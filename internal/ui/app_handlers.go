package ui

import (
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

// -- PR list handlers --

func (m App) handlePRsLoaded(msg PRsLoadedMsg) (tea.Model, tea.Cmd) {
	m.prList.SetItems(convertPRItems(msg.ToReview), convertPRItems(msg.MyPRs))
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
}

func (m App) handlePollTick() (tea.Model, tea.Cmd) {
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
}

func (m App) handlePollPRsLoaded(msg pollPRsLoadedMsg) (tea.Model, tea.Cmd) {
	m.prList.MergeItems(convertPRItems(msg.ToReview), convertPRItems(msg.MyPRs))
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
}

// -- PR data load handlers --

func (m App) handleDiffLoaded(msg DiffLoadedMsg) (tea.Model, tea.Cmd) {
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
}

func (m App) handlePRDetailLoaded(msg PRDetailLoadedMsg) (tea.Model, tea.Cmd) {
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
}

func (m App) handleCommentsLoaded(msg CommentsLoadedMsg) (tea.Model, tea.Cmd) {
	if !m.session.MatchesPR(msg.PRNumber) {
		return m, nil
	}
	if msg.Err != nil {
		m.chatPanel.SetCommentsError(msg.Err.Error())
	} else {
		m.chatPanel.SetComments(msg.Comments, msg.InlineComments)
		m.diffViewer.SetGitHubInlineComments(msg.InlineComments)
		m.session.Comments = msg.Comments
		m.session.InlineComments = msg.InlineComments
	}
	return m, m.refreshFetchDone(msg.PRNumber)
}

func (m App) handleCIStatusLoaded(msg CIStatusLoadedMsg) (tea.Model, tea.Cmd) {
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
}

func (m App) handleReviewsLoaded(msg ReviewsLoadedMsg) (tea.Model, tea.Cmd) {
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

// -- CI re-run handlers --

func (m App) handleCIRerunRequest() (tea.Model, tea.Cmd) {
	if m.session == nil || m.ghClient == nil {
		return m, nil
	}
	runIDs := m.diffViewer.ciStatus.FailedRunIDs()
	if len(runIDs) == 0 {
		return m, m.statusBar.SetTemporaryMessage("No re-runnable failed checks", 2*time.Second)
	}
	clearCmd := m.statusBar.SetTemporaryMessage(
		fmt.Sprintf("Re-running %d failed workflow(s)...", len(runIDs)), 15*time.Second,
	)
	return m, tea.Batch(clearCmd, rerunFailedCICmd(m.ghClient, m.session.Owner, m.session.Repo, m.session.Number, runIDs))
}

func (m App) handleCIRerunDone(msg CIRerunDoneMsg) (tea.Model, tea.Cmd) {
	clearCmd := m.statusBar.SetTemporaryMessage(
		fmt.Sprintf("Re-ran %d workflow(s) — refreshing CI...", msg.Count), 3*time.Second,
	)
	var fetchCmd tea.Cmd
	if m.session.MatchesPR(msg.PRNumber) && m.ghClient != nil {
		fetchCmd = fetchCIStatusCmd(m.ghClient, m.session.Owner, m.session.Repo, m.session.Number)
	}
	return m, tea.Batch(clearCmd, fetchCmd)
}

// -- Chat & comment handlers --

func (m App) handleChatClear() (tea.Model, tea.Cmd) {
	m.chatPanel.ClearChat()
	if m.session != nil {
		m.session.CancelAITurn()
		m.session.ThreadID = ""
		_ = m.threadStore.Delete(m.session.Owner, m.session.Repo, m.session.Number)
	}
	return m, m.statusBar.SetTemporaryMessage("Chat cleared", 2*time.Second)
}

func (m App) handleCommentPosted(msg CommentPostedMsg) (tea.Model, tea.Cmd) {
	m.chatPanel.SetCommentPosted(msg.Err)
	if msg.Err == nil && m.ghClient != nil && m.session != nil {
		return m, fetchCommentsCmd(m.ghClient, m.session.Owner, m.session.Repo, m.session.Number)
	}
	return m, nil
}

func (m App) handleInlineCommentReply(msg InlineCommentReplyMsg) (tea.Model, tea.Cmd) {
	if m.session == nil || m.ghClient == nil {
		return m, nil
	}
	clearCmd := m.statusBar.SetTemporaryMessage("Posting reply...", 2*time.Second)
	return m, tea.Batch(clearCmd, replyToCommentCmd(m.ghClient, m.session.Owner, m.session.Repo, m.session.Number, msg.CommentID, msg.Body))
}

func (m App) handleInlineCommentReplyDone(msg InlineCommentReplyDoneMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		return m, m.statusBar.SetTemporaryMessage(
			fmt.Sprintf("Reply failed: %v", msg.Err), 3*time.Second)
	}
	clearCmd := m.statusBar.SetTemporaryMessage("Reply posted", 2*time.Second)
	var refreshCmd tea.Cmd
	if m.session != nil && m.ghClient != nil {
		refreshCmd = fetchCommentsCmd(m.ghClient, m.session.Owner, m.session.Repo, m.session.Number)
	}
	return m, tea.Batch(clearCmd, refreshCmd)
}

// -- Review submission handlers --

func (m App) handleReviewSubmitDone(msg ReviewSubmitDoneMsg) (tea.Model, tea.Cmd) {
	if !m.session.MatchesPR(msg.PRNumber) {
		return m, nil
	}
	clearCmd := m.statusBar.SetTemporaryMessage(fmt.Sprintf("✓ %s PR #%d", msg.Action.PastLabel(), msg.PRNumber), 3*time.Second)
	m.chatPanel.SetReviewSubmitted(nil)
	// Clear pending comments — they've been submitted
	m.session.PendingInlineComments = nil
	m.syncPendingComments()
	return m, tea.Batch(clearCmd, fetchReviewsCmd(m.ghClient, m.session.Owner, m.session.Repo, m.session.Number))
}

func (m App) handleReviewSubmitErr(msg ReviewSubmitErrMsg) (tea.Model, tea.Cmd) {
	if m.session.MatchesPR(msg.PRNumber) {
		m.chatPanel.SetReviewSubmitted(msg.Err)
	}
	return m, m.statusBar.SetTemporaryMessage(fmt.Sprintf("✗ Review failed: %s", msg.Err), 5*time.Second)
}

// -- Key handling --

// handleKeyMsg dispatches keyboard input by mode. Global bindings that have a
// palette equivalent delegate to executeCommand so the two cannot drift.
func (m App) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Overlay mode captures all keys
	if m.mode == ModeOverlay {
		if m.commentOverlay.IsVisible() {
			var cmd tea.Cmd
			m.commentOverlay, cmd = m.commentOverlay.Update(msg)
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

	// While filtering, searching, or commenting, route all keys to that panel
	if m.focused == PanelLeft && m.prList.IsFiltering() {
		return m.updateFocusedPanel(msg)
	}
	if m.focused == PanelCenter && (m.diffViewer.IsSearching() || m.diffViewer.IsCommenting()) {
		return m.updateFocusedPanel(msg)
	}

	// Global key handling in navigation mode
	switch {
	case key.Matches(msg, GlobalKeys.Help):
		return m.executeCommand("help")
	case key.Matches(msg, GlobalKeys.Quit):
		return m, tea.Quit
	case key.Matches(msg, GlobalKeys.Tab):
		m.focusAdjacentPanel(nextVisiblePanel)
		return m, nil
	case key.Matches(msg, GlobalKeys.ShiftTab):
		m.focusAdjacentPanel(prevVisiblePanel)
		return m, nil
	case key.Matches(msg, GlobalKeys.Panel1):
		return m.executeCommand("prs")
	case key.Matches(msg, GlobalKeys.Panel2):
		return m.executeCommand("diff")
	case key.Matches(msg, GlobalKeys.Panel3):
		return m.executeCommand("chat")
	case key.Matches(msg, GlobalKeys.ToggleLeft):
		return m.executeCommand("toggle left")
	case key.Matches(msg, GlobalKeys.ToggleCenter):
		return m.executeCommand("toggle center")
	case key.Matches(msg, GlobalKeys.ToggleRight):
		return m.executeCommand("toggle right")
	case key.Matches(msg, GlobalKeys.Zoom):
		return m.executeCommand("zoom")
	case key.Matches(msg, GlobalKeys.OpenBrowser):
		return m.executeCommand("open")
	case key.Matches(msg, GlobalKeys.Analyze):
		return m.executeCommand("analyze")
	case key.Matches(msg, GlobalKeys.Refresh):
		return m.executeCommand("refresh")
	case key.Matches(msg, GlobalKeys.CommandMode):
		return m.openCommandPalette(true)
	case key.Matches(msg, GlobalKeys.ExCommand):
		return m.openCommandPalette(false)
	}

	// Delegate to focused panel
	return m.updateFocusedPanel(msg)
}

// focusAdjacentPanel exits zoom and moves focus using the given selector.
func (m *App) focusAdjacentPanel(pick func(Panel, [3]bool) Panel) {
	if m.zoomed {
		m.exitZoom()
		m.recalcLayout()
	}
	m.focusPanel(pick(m.focused, m.panelVisible))
}

// openCommandPalette enters command mode in quick or full mode.
func (m App) openCommandPalette(quick bool) (tea.Model, tea.Cmd) {
	m.setMode(ModeCommand)
	m.commandMode.SetSize(m.width, m.height)
	return m, m.commandMode.Open(quick)
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
