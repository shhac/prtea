package ui

import (
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/shhac/prtea/internal/config"
)

// -- PR list domain handlers --

// handlePRListMsg handles PR list lifecycle: client init, fetching, polling, selection.
func (m App) handlePRListMsg(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case GHClientReadyMsg:
		m.ghClient = msg.Client
		m.ghClient.SetFetchLimit(m.appConfig.PRFetchLimit)
		return m, fetchPRsCmd(m.ghClient)

	case GHClientErrorMsg:
		m.prList.SetError(msg.Err.Error())
		return m, nil

	case PRsLoadedMsg:
		toReview := convertPRItems(msg.ToReview)
		myPRs := convertPRItems(msg.MyPRs)
		m.prList.SetItems(toReview, myPRs)
		if !m.initialLoadDone {
			m.initialLoadDone = true
			m.snapshotKnownPRs(msg.ToReview, msg.MyPRs)
		}
		var cmds []tea.Cmd
		if m.ghClient != nil {
			allPRs := append(msg.ToReview, msg.MyPRs...)
			cmds = append(cmds, fetchReviewDecisionsCmd(m.ghClient, allPRs))
		}
		if m.pollEnabled && m.pollInterval > 0 {
			cmds = append(cmds, pollTickCmd(m.pollInterval))
		}
		return m, tea.Batch(cmds...)

	case PRReviewDecisionsMsg:
		m.prList.UpdateReviewDecisions(msg.Decisions)
		return m, nil

	case PRsErrorMsg:
		m.prList.SetError(msg.Err.Error())
		return m, nil

	case pollTickMsg:
		if m.pollEnabled && m.ghClient != nil && m.prList.state == stateLoaded {
			return m, tea.Batch(
				pollFetchPRsCmd(m.ghClient),
				pollTickCmd(m.pollInterval),
			)
		}
		if m.pollEnabled && m.pollInterval > 0 {
			return m, pollTickCmd(m.pollInterval)
		}
		return m, nil

	case pollErrorMsg:
		clearCmd := m.statusBar.SetTemporaryMessage(
			"Poll error: "+formatUserError(msg.Err.Error()), 5*time.Second,
		)
		return m, clearCmd

	case pollPRsLoadedMsg:
		toReview := convertPRItems(msg.ToReview)
		myPRs := convertPRItems(msg.MyPRs)
		m.prList.MergeItems(toReview, myPRs)
		var cmds []tea.Cmd
		if m.ghClient != nil {
			allPRs := append(msg.ToReview, msg.MyPRs...)
			cmds = append(cmds, fetchReviewDecisionsCmd(m.ghClient, allPRs))
		}
		if m.notifyEnabled {
			newPRs := m.detectNewPRs(msg.ToReview)
			if len(newPRs) > 0 {
				cmds = append(cmds, notifyNewPRsCmd(newPRs, m.appConfig.NotificationThreshold))
			}
		}
		m.snapshotKnownPRs(msg.ToReview, msg.MyPRs)
		return m, tea.Batch(cmds...)

	case PRSelectedMsg:
		return m.selectPR(msg.Owner, msg.Repo, msg.Number, msg.HTMLURL, false)

	case PRSelectedAndAdvanceMsg:
		return m.selectPR(msg.Owner, msg.Repo, msg.Number, msg.HTMLURL, true)

	case list.FilterMatchesMsg:
		var cmd tea.Cmd
		m.prList, cmd = m.prList.Update(msg)
		return m, cmd
	}
	return m, nil
}

// -- Diff domain handlers --

// handleDiffMsg handles diff loading, PR detail, comments, CI, and reviews.
func (m App) handleDiffMsg(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case HunkSelectedAndAdvanceMsg:
		m.showAndFocusPanel(PanelRight)
		return m, nil

	case DiffLoadedMsg:
		if msg.PRNumber != m.diffViewer.prNumber {
			return m, nil
		}
		if msg.Err != nil {
			m.diffViewer.SetError(msg.Err)
		} else {
			m.diffViewer.SetDiff(msg.Files)
			if m.session != nil {
				m.session.DiffFiles = msg.Files
			}
		}
		return m, m.refreshFetchDone(msg.PRNumber)

	case PRDetailLoadedMsg:
		if !m.session.MatchesPR(msg.PRNumber) {
			return m, nil
		}
		if msg.Err != nil {
			m.diffViewer.SetPRInfoError(msg.Err.Error())
		} else if msg.Detail != nil {
			m.diffViewer.SetPRInfo(
				msg.Detail.Title,
				msg.Detail.Body,
				msg.Detail.Author.Login,
				msg.Detail.HTMLURL,
			)
		}
		return m, m.refreshFetchDone(msg.PRNumber)

	case CommentsLoadedMsg:
		if !m.session.MatchesPR(msg.PRNumber) {
			return m, nil
		}
		if msg.Err != nil {
			m.chatPanel.SetCommentsError(msg.Err.Error())
		} else {
			m.chatPanel.SetComments(msg.Comments, msg.InlineComments)
			m.diffViewer.SetGitHubInlineComments(msg.InlineComments)
		}
		return m, m.refreshFetchDone(msg.PRNumber)

	case CIStatusLoadedMsg:
		if !m.session.MatchesPR(msg.PRNumber) {
			return m, nil
		}
		if msg.Err != nil {
			m.diffViewer.SetCIError(msg.Err.Error())
		} else if msg.Status != nil {
			m.diffViewer.SetCIStatus(msg.Status)
			m.prList.SetCIStatus(msg.Status.OverallStatus)
		}
		return m, m.refreshFetchDone(msg.PRNumber)

	case CIRerunRequestMsg:
		if m.session == nil || m.ghClient == nil {
			return m, nil
		}
		runIDs := m.diffViewer.ciStatus.FailedRunIDs()
		if len(runIDs) == 0 {
			clearCmd := m.statusBar.SetTemporaryMessage("No re-runnable failed checks", 2*time.Second)
			return m, clearCmd
		}
		clearCmd := m.statusBar.SetTemporaryMessage(
			fmt.Sprintf("Re-running %d failed workflow(s)...", len(runIDs)), 15*time.Second,
		)
		return m, tea.Batch(clearCmd, rerunFailedCICmd(m.ghClient, m.session.Owner, m.session.Repo, m.session.Number, runIDs))

	case CIRerunDoneMsg:
		clearCmd := m.statusBar.SetTemporaryMessage(
			fmt.Sprintf("Re-ran %d workflow(s) — refreshing CI...", msg.Count), 3*time.Second,
		)
		var fetchCmd tea.Cmd
		if m.session.MatchesPR(msg.PRNumber) && m.ghClient != nil {
			fetchCmd = fetchCIStatusCmd(m.ghClient, m.session.Owner, m.session.Repo, m.session.Number)
		}
		return m, tea.Batch(clearCmd, fetchCmd)

	case CIRerunErrMsg:
		clearCmd := m.statusBar.SetTemporaryMessage(
			fmt.Sprintf("CI re-run failed: %s", formatUserError(msg.Err.Error())), 5*time.Second,
		)
		return m, clearCmd

	case ReviewsLoadedMsg:
		if !m.session.MatchesPR(msg.PRNumber) {
			return m, nil
		}
		if msg.Err != nil {
			m.diffViewer.SetReviewError(msg.Err.Error())
		} else if msg.Summary != nil {
			m.diffViewer.SetReviewSummary(msg.Summary)
			m.prList.SetReviewDecision(msg.Summary.ReviewDecision)
		}
		return m, m.refreshFetchDone(msg.PRNumber)
	}
	return m, nil
}

// -- Analysis domain handlers --

// handleAnalysisMsg handles AI analysis and AI review streaming.
func (m App) handleAnalysisMsg(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case AnalysisStreamChunkMsg:
		if m.session == nil || m.session.AnalysisStreamCh == nil {
			return m, nil
		}
		m.chatPanel.AppendAnalysisStreamChunk(msg.Content)
		return m, listenForStream(m.session.AnalysisStreamCh)

	case AnalysisCompleteMsg:
		if m.session != nil {
			m.session.Analyzing = false
			m.session.AnalysisStreamCh = nil
		}
		if m.session.MatchesPR(msg.PRNumber) {
			m.chatPanel.SetAnalysisResult(msg.Result)
			_ = m.analysisStore.Put(
				m.session.Owner, m.session.Repo, m.session.Number,
				msg.DiffHash, msg.Result,
			)
		}
		return m, nil

	case AnalysisErrorMsg:
		if m.session != nil {
			m.session.Analyzing = false
			m.session.AnalysisStreamCh = nil
		}
		if m.session.MatchesPR(msg.PRNumber) {
			m.chatPanel.SetAnalysisError(msg.Err.Error())
		}
		return m, nil

	case AIReviewCompleteMsg:
		if m.session.MatchesPR(msg.PRNumber) {
			m.chatPanel.SetAIReviewResult(msg.Result)
			m.mergeAIComments(msg.Result.Comments)
			m.diffViewer.ClearAIInlineComments()
			m.diffViewer.SetPendingInlineComments(m.session.PendingInlineComments)
			m.chatPanel.SetPendingCommentCount(len(m.session.PendingInlineComments))
			clearCmd := m.statusBar.SetTemporaryMessage(
				fmt.Sprintf("AI review ready: %d inline comments", len(msg.Result.Comments)),
				3*time.Second,
			)
			return m, clearCmd
		}
		return m, nil

	case AIReviewErrorMsg:
		if m.session.MatchesPR(msg.PRNumber) {
			m.chatPanel.SetAIReviewError(msg.Err.Error())
			clearCmd := m.statusBar.SetTemporaryMessage(
				"AI review failed: "+formatUserError(msg.Err.Error()),
				5*time.Second,
			)
			return m, clearCmd
		}
		return m, nil
	}
	return m, nil
}

// -- Chat domain handlers --

// handleChatMsg handles chat streaming, comments, and inline comment management.
func (m App) handleChatMsg(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case ChatClearMsg:
		m.chatPanel.ClearChat()
		if m.session != nil {
			if m.session.StreamCancel != nil {
				m.session.StreamCancel()
				m.session.StreamCancel = nil
			}
			m.session.StreamChan = nil
		}
		if m.chatService != nil && m.session != nil {
			m.chatService.ClearSession(m.session.Owner, m.session.Repo, m.session.Number)
		}
		clearCmd := m.statusBar.SetTemporaryMessage("Chat cleared", 2*time.Second)
		return m, clearCmd

	case ChatSendMsg:
		return m.handleChatSend(msg.Message)

	case ChatStreamChunkMsg:
		if m.session == nil || m.session.StreamChan == nil {
			return m, nil
		}
		m.chatPanel.AppendStreamChunk(msg.Content)
		return m, listenForStream(m.session.StreamChan)

	case ChatResponseMsg:
		if m.session == nil || m.session.StreamChan == nil {
			return m, nil
		}
		m.session.StreamChan = nil
		if msg.Err != nil {
			m.chatPanel.SetChatError(msg.Err.Error())
		} else {
			m.chatPanel.AddResponse(msg.Content)
		}
		return m, nil

	case CommentPostMsg:
		return m.handleCommentPost(msg.Body)

	case CommentPostedMsg:
		m.chatPanel.SetCommentPosted(msg.Err)
		if msg.Err == nil && m.ghClient != nil && m.session != nil {
			return m, fetchCommentsCmd(m.ghClient, m.session.Owner, m.session.Repo, m.session.Number)
		}
		return m, nil

	case InlineCommentAddMsg:
		return m.handleInlineCommentAdd(msg)

	case InlineCommentReplyMsg:
		if m.session == nil || m.ghClient == nil {
			return m, nil
		}
		clearCmd := m.statusBar.SetTemporaryMessage("Posting reply...", 2*time.Second)
		return m, tea.Batch(clearCmd, replyToCommentCmd(m.ghClient, m.session.Owner, m.session.Repo, m.session.Number, msg.CommentID, msg.Body))

	case InlineCommentReplyDoneMsg:
		if msg.Err != nil {
			clearCmd := m.statusBar.SetTemporaryMessage(
				fmt.Sprintf("Reply failed: %v", msg.Err), 3*time.Second)
			return m, clearCmd
		}
		clearCmd := m.statusBar.SetTemporaryMessage("Reply posted", 2*time.Second)
		var refreshCmd tea.Cmd
		if m.session != nil && m.ghClient != nil {
			refreshCmd = fetchCommentsCmd(m.ghClient, m.session.Owner, m.session.Repo, m.session.Number)
		}
		return m, tea.Batch(clearCmd, refreshCmd)
	}
	return m, nil
}

// -- Review domain handlers --

// handleReviewMsg handles review submission, approval, and PR close.
func (m App) handleReviewMsg(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case ReviewValidationMsg:
		clearCmd := m.statusBar.SetTemporaryMessage(msg.Message, 3*time.Second)
		return m, clearCmd

	case ReviewSubmitMsg:
		return m.handleReviewSubmit(msg)

	case ReviewSubmitDoneMsg:
		if !m.session.MatchesPR(msg.PRNumber) {
			return m, nil
		}
		actionLabels := map[ReviewAction]string{
			ReviewApprove:        "Approved",
			ReviewComment:        "Commented on",
			ReviewRequestChanges: "Requested changes on",
		}
		label := actionLabels[msg.Action]
		clearCmd := m.statusBar.SetTemporaryMessage(fmt.Sprintf("✓ %s PR #%d", label, msg.PRNumber), 3*time.Second)
		m.chatPanel.SetReviewSubmitted(nil)
		// Clear pending comments — they've been submitted
		m.session.PendingInlineComments = nil
		m.diffViewer.SetPendingInlineComments(nil)
		m.chatPanel.SetPendingCommentCount(0)
		return m, tea.Batch(clearCmd, fetchReviewsCmd(m.ghClient, m.session.Owner, m.session.Repo, m.session.Number))

	case ReviewSubmitErrMsg:
		if m.session.MatchesPR(msg.PRNumber) {
			m.chatPanel.SetReviewSubmitted(msg.Err)
		}
		clearCmd := m.statusBar.SetTemporaryMessage(fmt.Sprintf("✗ Review failed: %s", msg.Err), 5*time.Second)
		return m, clearCmd

	case PRApproveDoneMsg:
		if !m.session.MatchesPR(msg.PRNumber) {
			return m, nil
		}
		clearCmd := m.statusBar.SetTemporaryMessage(fmt.Sprintf("✓ Approved PR #%d", msg.PRNumber), 3*time.Second)
		return m, tea.Batch(clearCmd, fetchReviewsCmd(m.ghClient, m.session.Owner, m.session.Repo, m.session.Number))

	case PRApproveErrMsg:
		clearCmd := m.statusBar.SetTemporaryMessage(fmt.Sprintf("✗ Approve failed: %s", msg.Err), 5*time.Second)
		return m, clearCmd

	case PRCloseDoneMsg:
		if !m.session.MatchesPR(msg.PRNumber) {
			return m, nil
		}
		clearCmd := m.statusBar.SetTemporaryMessage(fmt.Sprintf("✓ Closed PR #%d", msg.PRNumber), 3*time.Second)
		if m.ghClient != nil {
			return m, tea.Batch(clearCmd, fetchPRsCmd(m.ghClient))
		}
		return m, clearCmd

	case PRCloseErrMsg:
		clearCmd := m.statusBar.SetTemporaryMessage(fmt.Sprintf("✗ Close failed: %s", msg.Err), 5*time.Second)
		return m, clearCmd
	}
	return m, nil
}

// -- Config domain handlers --

// handleConfigMsg handles settings changes and overlay lifecycle.
func (m App) handleConfigMsg(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case ConfigChangedMsg:
		if m.settingsPanel.IsDirty() {
			cfg := m.settingsPanel.Config()
			m.appConfig = cfg
			_ = config.Save(cfg)
			var cmds []tea.Cmd
			wasEnabled := m.pollEnabled
			m.pollEnabled = cfg.PollEnabled
			m.pollInterval = cfg.PollIntervalDuration()
			m.notifyEnabled = cfg.NotificationsEnabled
			if !wasEnabled && m.pollEnabled && m.pollInterval > 0 && m.prList.state == stateLoaded {
				cmds = append(cmds, pollTickCmd(m.pollInterval))
			}
			m.chatPanel.SetStreamCheckpoint(time.Duration(cfg.StreamCheckpointMs) * time.Millisecond)
			m.chatPanel.UpdateDefaultReviewAction(cfg.DefaultReviewAction)
			m.collapseThreshold = cfg.CollapseThreshold
			if m.ghClient != nil {
				m.ghClient.SetFetchLimit(cfg.PRFetchLimit)
			}
			if m.analyzer != nil {
				m.analyzer.SetTimeout(cfg.ClaudeTimeoutDuration())
				m.analyzer.SetAnalysisMaxTurns(cfg.AnalysisMaxTurns)
			}
			if m.chatService != nil {
				m.chatService.SetTimeout(cfg.ClaudeTimeoutDuration())
				m.chatService.SetMaxPromptTokens(cfg.MaxPromptTokens)
				m.chatService.SetMaxHistoryMessages(cfg.MaxChatHistory)
				m.chatService.SetMaxTurns(cfg.ChatMaxTurns)
			}
			return m, tea.Batch(cmds...)
		}
		return m, nil

	case HelpClosedMsg:
		m.setMode(ModeNavigation)
		return m, nil

	case SettingsClosedMsg:
		m.setMode(ModeNavigation)
		return m, nil

	case ShowCommentOverlayMsg:
		m.commentOverlay.SetSize(m.width, m.height)
		cmd := m.commentOverlay.Show(msg)
		m.setMode(ModeOverlay)
		return m, cmd

	case CommentOverlayClosedMsg:
		m.setMode(ModeNavigation)
		return m, nil

	case CommandExecuteMsg:
		m.setMode(ModeNavigation)
		return m.executeCommand(msg.Name)

	case CommandModeExitMsg:
		m.setMode(ModeNavigation)
		return m, nil

	case CommandNotFoundMsg:
		clearCmd := m.statusBar.SetTemporaryMessage(fmt.Sprintf("Unknown command: %s", msg.Input), 2*time.Second)
		return m, clearCmd

	case ModeChangedMsg:
		if msg.Mode == ChatModeInsert {
			m.setMode(ModeInsert)
		} else {
			m.setMode(ModeNavigation)
		}
		return m, nil
	}
	return m, nil
}

// -- Key handling --

// handleKeyMsg dispatches keyboard input by mode.
func (m App) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Overlay mode captures all keys
	if m.mode == ModeOverlay {
		if m.commentOverlay.IsVisible() {
			var cmd tea.Cmd
			m.commentOverlay, cmd = m.commentOverlay.Update(msg)
			return m, cmd
		}
		if m.settingsPanel.IsVisible() {
			var cmd tea.Cmd
			m.settingsPanel, cmd = m.settingsPanel.Update(msg)
			return m, cmd
		}
		var cmd tea.Cmd
		m.helpOverlay, cmd = m.helpOverlay.Update(msg)
		return m, cmd
	}

	// In insert mode, only Esc is handled globally (via chat panel)
	if m.mode == ModeInsert {
		return m.updateChatPanel(msg)
	}

	// Command mode captures all keys
	if m.mode == ModeCommand {
		var cmd tea.Cmd
		m.commandMode, cmd = m.commandMode.Update(msg)
		return m, cmd
	}

	// While filtering the PR list, route all keys to the list
	if m.focused == PanelLeft && m.prList.IsFiltering() {
		return m.updateFocusedPanel(msg)
	}

	// While searching in the diff viewer, route all keys to the diff viewer
	if m.focused == PanelCenter && m.diffViewer.IsSearching() {
		return m.updateFocusedPanel(msg)
	}

	// While commenting in the diff viewer, route all keys to the diff viewer
	if m.focused == PanelCenter && m.diffViewer.IsCommenting() {
		return m.updateFocusedPanel(msg)
	}

	// Global key handling in navigation mode
	switch {
	case key.Matches(msg, GlobalKeys.Help):
		m.setMode(ModeOverlay)
		m.helpOverlay.SetSize(m.width, m.height)
		m.helpOverlay.Show(m.focused)
		return m, nil

	case key.Matches(msg, GlobalKeys.Quit):
		return m, tea.Quit

	case key.Matches(msg, GlobalKeys.Tab):
		if m.zoomed {
			m.exitZoom()
			m.recalcLayout()
		}
		m.focusPanel(nextVisiblePanel(m.focused, m.panelVisible))
		return m, nil

	case key.Matches(msg, GlobalKeys.ShiftTab):
		if m.zoomed {
			m.exitZoom()
			m.recalcLayout()
		}
		m.focusPanel(prevVisiblePanel(m.focused, m.panelVisible))
		return m, nil

	case key.Matches(msg, GlobalKeys.Panel1):
		m.showAndFocusPanel(PanelLeft)
		return m, nil

	case key.Matches(msg, GlobalKeys.Panel2):
		m.showAndFocusPanel(PanelCenter)
		return m, nil

	case key.Matches(msg, GlobalKeys.Panel3):
		m.showAndFocusPanel(PanelRight)
		return m, nil

	case key.Matches(msg, GlobalKeys.ToggleLeft):
		if m.zoomed {
			m.exitZoom()
		}
		m.togglePanel(PanelLeft)
		return m, nil

	case key.Matches(msg, GlobalKeys.ToggleCenter):
		if m.zoomed {
			m.exitZoom()
		}
		m.togglePanel(PanelCenter)
		return m, nil

	case key.Matches(msg, GlobalKeys.ToggleRight):
		if m.zoomed {
			m.exitZoom()
		}
		m.togglePanel(PanelRight)
		return m, nil

	case key.Matches(msg, GlobalKeys.Zoom):
		m.toggleZoom()
		return m, nil

	case key.Matches(msg, GlobalKeys.OpenBrowser):
		if m.session != nil && m.session.HTMLURL != "" {
			return m, openBrowserCmd(m.session.HTMLURL)
		}
		return m, nil

	case key.Matches(msg, GlobalKeys.Analyze):
		return m.startAnalysis()

	case key.Matches(msg, GlobalKeys.Refresh):
		if m.focused == PanelLeft {
			return m.refreshPRList()
		}
		return m.refreshSelectedPR()

	case key.Matches(msg, GlobalKeys.ReviewPanel):
		m.showAndFocusPanel(PanelRight)
		m.chatPanel.SetActiveTab(ChatTabReview)
		return m, nil

	case key.Matches(msg, GlobalKeys.CommandMode):
		m.setMode(ModeCommand)
		m.commandMode.SetSize(m.width, m.height)
		cmd := m.commandMode.Open(true)
		return m, cmd

	case key.Matches(msg, GlobalKeys.ExCommand):
		m.setMode(ModeCommand)
		m.commandMode.SetSize(m.width, m.height)
		cmd := m.commandMode.Open(false)
		return m, cmd
	}

	// Delegate to focused panel
	return m.updateFocusedPanel(msg)
}

// -- Infrastructure handlers --

// handleSpinnerTick routes spinner ticks to all panels.
func (m App) handleSpinnerTick(msg spinner.TickMsg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	var cmd tea.Cmd
	m.prList, cmd = m.prList.Update(msg)
	cmds = append(cmds, cmd)
	m.diffViewer, cmd = m.diffViewer.Update(msg)
	cmds = append(cmds, cmd)
	m.chatPanel, cmd = m.chatPanel.Update(msg)
	cmds = append(cmds, cmd)
	return m, tea.Batch(cmds...)
}
