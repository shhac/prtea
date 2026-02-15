package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/shhac/prtea/internal/claude"
	"github.com/shhac/prtea/internal/config"
	"github.com/shhac/prtea/internal/github"
)

// App is the root Bubbletea model for the PR dashboard.
type App struct {
	// Panel models
	prList     PRListModel
	diffViewer DiffViewerModel
	chatPanel  ChatPanelModel
	statusBar  StatusBarModel

	// Overlays
	helpOverlay   HelpOverlayModel
	commandMode   CommandModeModel
	settingsPanel SettingsModel

	// GitHub client (nil until GHClientReadyMsg)
	ghClient GitHubService

	// Currently selected PR (nil until a PR is selected)
	selectedPR *SelectedPR
	diffFiles             []github.PRFile          // stored for analysis context
	pendingInlineComments []PendingInlineComment   // unified pool of pending comments

	// Claude integration
	claudePath    string
	appConfig     *config.Config
	analyzer      *claude.Analyzer
	chatService   *claude.ChatService
	analysisStore *claude.AnalysisStore
	chatStore     *claude.ChatStore
	analyzing            bool
	streamChan           chatStreamChan      // active chat streaming channel
	streamCancel         context.CancelFunc  // cancels active stream goroutine
	analysisStreamCh     analysisStreamChan  // active analysis streaming channel
	analysisStreamCancel context.CancelFunc  // cancels active analysis stream

	// Layout state
	focused            Panel
	width              int
	height             int
	panelVisible       [3]bool // which panels are currently visible
	zoomed             bool    // zoom mode: only focused panel shown
	preZoomVisible     [3]bool // saved visibility before zoom
	initialized        bool    // whether first WindowSizeMsg has been processed
	collapseThreshold  int     // terminal width below which panels auto-collapse

	// Mode
	mode AppMode

	// Refresh tracking: counts remaining fetches for a PR refresh.
	// When it reaches 0, we show a success message.
	refreshPending int
	refreshPRNum   int // PR number being refreshed

	// Background polling
	pollInterval time.Duration // current poll interval from config
	pollEnabled  bool          // whether polling is enabled

	// Notification state
	notifyEnabled    bool            // whether OS notifications are enabled
	initialLoadDone  bool            // true after first successful PR fetch
	knownPRs         map[string]bool // PR keys seen since boot (for new-PR detection)
}

// NewApp creates a new App model with default state.
func NewApp() App {
	cfg, _ := config.Load()
	if cfg == nil {
		cfg = &config.Config{ClaudeTimeout: config.DefaultClaudeTimeoutMs}
	}

	claudePath, _ := claude.FindClaude()

	chatStore := claude.NewChatStore(config.ChatCacheDir())

	var analyzer *claude.Analyzer
	var chatSvc *claude.ChatService
	if claudePath != "" {
		analyzer = claude.NewAnalyzer(claudePath, cfg.ClaudeTimeoutDuration(), config.PromptsDir())
		chatSvc = claude.NewChatService(claudePath, cfg.ClaudeTimeoutDuration(), chatStore)
	}

	store := claude.NewAnalysisStore(config.AnalysesCacheDir())

	// Map config default PR tab to constant
	defaultTab := TabToReview
	if cfg.DefaultPRTab == "mine" {
		defaultTab = TabMyPRs
	}

	// Determine initial panel visibility from StartCollapsed config
	panelVisible := [3]bool{true, true, true}
	for _, name := range cfg.StartCollapsed {
		switch name {
		case "left":
			panelVisible[PanelLeft] = false
		case "center":
			panelVisible[PanelCenter] = false
		case "right":
			panelVisible[PanelRight] = false
		}
	}
	// Ensure at least one panel is visible
	if !panelVisible[PanelLeft] && !panelVisible[PanelCenter] && !panelVisible[PanelRight] {
		panelVisible = [3]bool{true, true, true}
	}

	return App{
		prList:            NewPRListModel(defaultTab),
		diffViewer:        NewDiffViewerModel(),
		chatPanel:         NewChatPanelModel(),
		statusBar:         NewStatusBarModel(),
		helpOverlay:       NewHelpOverlayModel(),
		commandMode:       NewCommandModeModel(),
		settingsPanel:     NewSettingsModel(),
		focused:           PanelLeft,
		panelVisible:      panelVisible,
		mode:              ModeNavigation,
		collapseThreshold: cfg.CollapseThreshold,
		claudePath:        claudePath,
		appConfig:         cfg,
		analyzer:          analyzer,
		chatService:       chatSvc,
		analysisStore:     store,
		chatStore:         chatStore,
		pollInterval:      cfg.PollIntervalDuration(),
		pollEnabled:       cfg.PollEnabled,
		notifyEnabled:     cfg.NotificationsEnabled,
		knownPRs:          make(map[string]bool),
	}
}

func (m App) Init() tea.Cmd {
	return tea.Batch(initGHClientCmd, m.prList.spinner.Tick)
}

func (m App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.helpOverlay.SetSize(m.width, m.height)
		m.commandMode.SetSize(m.width, m.height)
		m.settingsPanel.SetSize(m.width, m.height)
		// Auto-collapse panels on first render if terminal is narrow
		if !m.initialized {
			m.initialized = true
			if m.width < m.collapseThreshold {
				m.panelVisible[PanelRight] = false
				if m.focused == PanelRight {
					m.focusPanel(nextVisiblePanel(m.focused, m.panelVisible))
				}
			}
		}
		m.recalcLayout()
		return m, nil

	case GHClientReadyMsg:
		m.ghClient = msg.Client
		return m, fetchPRsCmd(m.ghClient)

	case GHClientErrorMsg:
		m.prList.SetError(msg.Err.Error())
		return m, nil

	case PRsLoadedMsg:
		toReview := convertPRItems(msg.ToReview)
		myPRs := convertPRItems(msg.MyPRs)
		m.prList.SetItems(toReview, myPRs)
		// Snapshot known PRs on initial load (boot set for notification detection)
		if !m.initialLoadDone {
			m.initialLoadDone = true
			m.snapshotKnownPRs(msg.ToReview, msg.MyPRs)
		}
		// Start background polling after the first successful load
		if m.pollEnabled && m.pollInterval > 0 {
			return m, pollTickCmd(m.pollInterval)
		}
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
		// Not ready or disabled — reschedule so polling resumes if re-enabled
		if m.pollEnabled && m.pollInterval > 0 {
			return m, pollTickCmd(m.pollInterval)
		}
		return m, nil

	case pollPRsLoadedMsg:
		toReview := convertPRItems(msg.ToReview)
		myPRs := convertPRItems(msg.MyPRs)
		m.prList.MergeItems(toReview, myPRs)
		// Detect new PRs and send notifications
		var cmds []tea.Cmd
		if m.notifyEnabled {
			newPRs := m.detectNewPRs(msg.ToReview)
			if len(newPRs) > 0 {
				cmds = append(cmds, notifyNewPRsCmd(newPRs))
			}
		}
		// Always update the known set (even if notifications disabled)
		m.snapshotKnownPRs(msg.ToReview, msg.MyPRs)
		return m, tea.Batch(cmds...)

	case HelpClosedMsg:
		m.mode = ModeNavigation
		m.statusBar.SetState(m.focused, m.mode)
		return m, nil

	case SettingsClosedMsg:
		m.mode = ModeNavigation
		m.statusBar.SetState(m.focused, m.mode)
		return m, nil

	case ConfigChangedMsg:
		if m.settingsPanel.IsDirty() {
			cfg := m.settingsPanel.Config()
			m.appConfig = cfg
			_ = config.Save(cfg)
			// Update polling state from new config
			wasEnabled := m.pollEnabled
			m.pollEnabled = cfg.PollEnabled
			m.pollInterval = cfg.PollIntervalDuration()
			m.notifyEnabled = cfg.NotificationsEnabled
			// If polling was just enabled, start the tick
			if !wasEnabled && m.pollEnabled && m.pollInterval > 0 && m.prList.state == stateLoaded {
				return m, pollTickCmd(m.pollInterval)
			}
		}
		return m, nil

	case CommandExecuteMsg:
		m.mode = ModeNavigation
		m.statusBar.SetState(m.focused, m.mode)
		return m.executeCommand(msg.Name)

	case CommandModeExitMsg:
		m.mode = ModeNavigation
		m.statusBar.SetState(m.focused, m.mode)
		return m, nil

	case CommandNotFoundMsg:
		clearCmd := m.statusBar.SetTemporaryMessage(fmt.Sprintf("Unknown command: %s", msg.Input), 2*time.Second)
		return m, clearCmd

	case ModeChangedMsg:
		if msg.Mode == ChatModeInsert {
			m.mode = ModeInsert
		} else {
			m.mode = ModeNavigation
		}
		m.statusBar.SetState(m.focused, m.mode)
		return m, nil

	case PRSelectedMsg:
		return m.selectPR(msg.Owner, msg.Repo, msg.Number, msg.HTMLURL, false)

	case PRSelectedAndAdvanceMsg:
		return m.selectPR(msg.Owner, msg.Repo, msg.Number, msg.HTMLURL, true)

	case HunkSelectedAndAdvanceMsg:
		m.showAndFocusPanel(PanelRight)
		return m, nil

	case DiffLoadedMsg:
		// Race guard: only apply if this is for the currently displayed PR
		if msg.PRNumber != m.diffViewer.prNumber {
			return m, nil
		}
		if msg.Err != nil {
			m.diffViewer.SetError(msg.Err)
		} else {
			m.diffViewer.SetDiff(msg.Files)
			m.diffFiles = msg.Files
		}
		return m, m.refreshFetchDone(msg.PRNumber)

	case PRDetailLoadedMsg:
		// Race guard: only apply if this is for the currently selected PR
		if m.selectedPR == nil || msg.PRNumber != m.selectedPR.Number {
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
		// Race guard: only apply if this is for the currently selected PR
		if m.selectedPR == nil || msg.PRNumber != m.selectedPR.Number {
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
		// Race guard: only apply if this is for the currently selected PR
		if m.selectedPR == nil || msg.PRNumber != m.selectedPR.Number {
			return m, nil
		}
		if msg.Err != nil {
			m.diffViewer.SetCIError(msg.Err.Error())
		} else if msg.Status != nil {
			m.diffViewer.SetCIStatus(msg.Status)
			m.prList.SetCIStatus(msg.Status.OverallStatus)
		}
		return m, m.refreshFetchDone(msg.PRNumber)

	case ReviewsLoadedMsg:
		// Race guard: only apply if this is for the currently selected PR
		if m.selectedPR == nil || msg.PRNumber != m.selectedPR.Number {
			return m, nil
		}
		if msg.Err != nil {
			m.diffViewer.SetReviewError(msg.Err.Error())
		} else if msg.Summary != nil {
			m.diffViewer.SetReviewSummary(msg.Summary)
			m.prList.SetReviewDecision(msg.Summary.ReviewDecision)
		}
		return m, m.refreshFetchDone(msg.PRNumber)

	case AnalysisStreamChunkMsg:
		if m.analysisStreamCh == nil {
			return m, nil
		}
		m.chatPanel.AppendAnalysisStreamChunk(msg.Content)
		return m, listenForAnalysisStream(m.analysisStreamCh)

	case AnalysisCompleteMsg:
		m.analyzing = false
		m.analysisStreamCh = nil
		if m.selectedPR != nil && msg.PRNumber == m.selectedPR.Number {
			m.chatPanel.SetAnalysisResult(msg.Result)
			// Cache the result
			_ = m.analysisStore.Put(
				m.selectedPR.Owner, m.selectedPR.Repo, m.selectedPR.Number,
				msg.DiffHash, msg.Result,
			)
		}
		return m, nil

	case AnalysisErrorMsg:
		m.analyzing = false
		m.analysisStreamCh = nil
		if m.selectedPR != nil && msg.PRNumber == m.selectedPR.Number {
			m.chatPanel.SetAnalysisError(msg.Err.Error())
		}
		return m, nil

	case AIReviewCompleteMsg:
		if m.selectedPR != nil && msg.PRNumber == m.selectedPR.Number {
			m.chatPanel.SetAIReviewResult(msg.Result)
			// Merge AI comments into pending pool (replaces old AI comments, preserves user comments)
			m.mergeAIComments(msg.Result.Comments)
			m.diffViewer.ClearAIInlineComments()
			m.diffViewer.SetPendingInlineComments(m.pendingInlineComments)
			m.chatPanel.SetPendingCommentCount(len(m.pendingInlineComments))
			clearCmd := m.statusBar.SetTemporaryMessage(
				fmt.Sprintf("AI review ready: %d inline comments", len(msg.Result.Comments)),
				3*time.Second,
			)
			return m, clearCmd
		}
		return m, nil

	case AIReviewErrorMsg:
		if m.selectedPR != nil && msg.PRNumber == m.selectedPR.Number {
			m.chatPanel.SetAIReviewError(msg.Err.Error())
			clearCmd := m.statusBar.SetTemporaryMessage(
				"AI review failed: "+formatUserError(msg.Err.Error()),
				5*time.Second,
			)
			return m, clearCmd
		}
		return m, nil

	case InlineCommentAddMsg:
		return m.handleInlineCommentAdd(msg)

	case CommentPostMsg:
		return m.handleCommentPost(msg.Body)

	case CommentPostedMsg:
		m.chatPanel.SetCommentPosted(msg.Err)
		if msg.Err == nil && m.ghClient != nil && m.selectedPR != nil {
			// Refresh comments after successful post
			return m, fetchCommentsCmd(m.ghClient, m.selectedPR.Owner, m.selectedPR.Repo, m.selectedPR.Number)
		}
		return m, nil

	case ReviewValidationMsg:
		clearCmd := m.statusBar.SetTemporaryMessage(msg.Message, 3*time.Second)
		return m, clearCmd

	case ReviewSubmitMsg:
		return m.handleReviewSubmit(msg)

	case ReviewSubmitDoneMsg:
		if m.selectedPR == nil || msg.PRNumber != m.selectedPR.Number {
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
		m.pendingInlineComments = nil
		m.diffViewer.SetPendingInlineComments(nil)
		m.chatPanel.SetPendingCommentCount(0)
		return m, tea.Batch(clearCmd, fetchReviewsCmd(m.ghClient, m.selectedPR.Owner, m.selectedPR.Repo, m.selectedPR.Number))

	case ReviewSubmitErrMsg:
		if m.selectedPR != nil && msg.PRNumber == m.selectedPR.Number {
			m.chatPanel.SetReviewSubmitted(msg.Err)
		}
		clearCmd := m.statusBar.SetTemporaryMessage(fmt.Sprintf("✗ Review failed: %s", msg.Err), 5*time.Second)
		return m, clearCmd

	case PRApproveDoneMsg:
		if m.selectedPR == nil || msg.PRNumber != m.selectedPR.Number {
			return m, nil
		}
		clearCmd := m.statusBar.SetTemporaryMessage(fmt.Sprintf("✓ Approved PR #%d", msg.PRNumber), 3*time.Second)
		return m, tea.Batch(clearCmd, fetchReviewsCmd(m.ghClient, m.selectedPR.Owner, m.selectedPR.Repo, m.selectedPR.Number))

	case PRApproveErrMsg:
		clearCmd := m.statusBar.SetTemporaryMessage(fmt.Sprintf("✗ Approve failed: %s", msg.Err), 5*time.Second)
		return m, clearCmd

	case PRCloseDoneMsg:
		if m.selectedPR == nil || msg.PRNumber != m.selectedPR.Number {
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

	case StatusBarClearMsg:
		m.statusBar.ClearIfSeqMatch(msg.Seq)
		return m, nil

	case list.FilterMatchesMsg:
		// Route filter match results back to the PR list so filtering actually works.
		var cmd tea.Cmd
		m.prList, cmd = m.prList.Update(msg)
		return m, cmd

	case spinner.TickMsg:
		// Route spinner ticks to all panels (each panel only processes its own spinner)
		var cmds []tea.Cmd
		var cmd tea.Cmd
		m.prList, cmd = m.prList.Update(msg)
		cmds = append(cmds, cmd)
		m.diffViewer, cmd = m.diffViewer.Update(msg)
		cmds = append(cmds, cmd)
		m.chatPanel, cmd = m.chatPanel.Update(msg)
		cmds = append(cmds, cmd)
		return m, tea.Batch(cmds...)

	case ChatClearMsg:
		m.chatPanel.ClearChat()
		if m.streamCancel != nil { // cancel active stream goroutine
			m.streamCancel()
			m.streamCancel = nil
		}
		m.streamChan = nil // stop any active stream
		if m.chatService != nil && m.selectedPR != nil {
			m.chatService.ClearSession(m.selectedPR.Owner, m.selectedPR.Repo, m.selectedPR.Number)
		}
		clearCmd := m.statusBar.SetTemporaryMessage("Chat cleared", 2*time.Second)
		return m, clearCmd

	case ChatSendMsg:
		return m.handleChatSend(msg.Message)

	case ChatStreamChunkMsg:
		// Ignore stale chunks from a previous PR's stream
		if m.streamChan == nil {
			return m, nil
		}
		m.chatPanel.AppendStreamChunk(msg.Content)
		return m, listenForChatStream(m.streamChan)

	case ChatResponseMsg:
		// Ignore stale responses from a previous PR's stream
		if m.streamChan == nil {
			return m, nil
		}
		m.streamChan = nil
		if msg.Err != nil {
			m.chatPanel.SetChatError(msg.Err.Error())
		} else {
			m.chatPanel.AddResponse(msg.Content)
		}
		return m, nil

	case tea.KeyMsg:
		// Overlay mode captures all keys
		if m.mode == ModeOverlay {
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
			m.mode = ModeOverlay
			m.helpOverlay.SetSize(m.width, m.height)
			m.helpOverlay.Show(m.focused)
			m.statusBar.SetState(m.focused, m.mode)
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
			if m.selectedPR != nil && m.selectedPR.HTMLURL != "" {
				return m, openBrowserCmd(m.selectedPR.HTMLURL)
			}
			return m, nil

		case key.Matches(msg, GlobalKeys.Analyze):
			return m.startAnalysis()

		case key.Matches(msg, GlobalKeys.Refresh):
			if m.focused == PanelLeft {
				return m.refreshPRList()
			}
			return m.refreshSelectedPR()

		case key.Matches(msg, GlobalKeys.CommandMode):
			m.mode = ModeCommand
			m.commandMode.SetSize(m.width, m.height)
			cmd := m.commandMode.Open(true)
			m.statusBar.SetState(m.focused, m.mode)
			return m, cmd

		case key.Matches(msg, GlobalKeys.ExCommand):
			m.mode = ModeCommand
			m.commandMode.SetSize(m.width, m.height)
			cmd := m.commandMode.Open(false)
			m.statusBar.SetState(m.focused, m.mode)
			return m, cmd

		}

		// Delegate to focused panel
		return m.updateFocusedPanel(msg)
	}

	return m, nil
}

func (m App) View() string {
	sizes := CalculatePanelSizes(m.width, m.height, m.panelVisible)

	if sizes.TooSmall {
		msg := lipgloss.NewStyle().
			Foreground(lipgloss.Color("196")).
			Bold(true).
			Render("Terminal too small. Please resize to at least 80×10.")
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, msg)
	}

	var panelViews []string
	if sizes.LeftWidth > 0 {
		panelViews = append(panelViews, m.prList.View())
	}
	if sizes.CenterWidth > 0 {
		panelViews = append(panelViews, m.diffViewer.View())
	}
	if sizes.RightWidth > 0 {
		panelViews = append(panelViews, m.chatPanel.View())
	}

	panels := lipgloss.JoinHorizontal(lipgloss.Top, panelViews...)
	m.statusBar.SetFiltering(m.focused == PanelLeft && m.prList.IsFiltering())
	m.statusBar.SetDiffSearching(m.focused == PanelCenter && m.diffViewer.IsSearching())
	m.statusBar.SetDiffSearchInfo(m.diffViewer.SearchInfo())
	bar := m.statusBar.View()

	base := lipgloss.JoinVertical(lipgloss.Left, panels, bar)

	// Render help overlay on top if active
	if m.helpOverlay.IsVisible() {
		return m.helpOverlay.View()
	}

	// Render settings overlay on top if active
	if m.settingsPanel.IsVisible() {
		return m.settingsPanel.View()
	}

	// Render command palette at the bottom if active
	if m.commandMode.IsActive() {
		return m.renderCommandOverlay(base)
	}

	return base
}

// selectPR handles shared setup when a PR is selected: resets panel state,
// kicks off data fetches, and optionally advances focus to the diff viewer.
func (m App) selectPR(owner, repo string, number int, htmlURL string, advance bool) (tea.Model, tea.Cmd) {
	title := ""
	if item, ok := m.prList.list.SelectedItem().(PRItem); ok {
		title = item.title
	}
	// Save current chat session before switching PRs
	if m.chatService != nil && m.selectedPR != nil {
		m.chatService.SaveSession(m.selectedPR.Owner, m.selectedPR.Repo, m.selectedPR.Number)
	}

	m.selectedPR = &SelectedPR{
		Owner:   owner,
		Repo:    repo,
		Number:  number,
		Title:   title,
		HTMLURL: htmlURL,
	}
	if m.streamCancel != nil { // cancel active chat stream goroutine
		m.streamCancel()
		m.streamCancel = nil
	}
	m.streamChan = nil // stop listening to old chat stream
	if m.analysisStreamCancel != nil { // cancel active analysis stream
		m.analysisStreamCancel()
		m.analysisStreamCancel = nil
	}
	m.analysisStreamCh = nil           // stop listening to old analysis stream
	m.analyzing = false
	m.diffFiles = nil                  // clear old diff data
	m.pendingInlineComments = nil      // clear old pending comments
	m.chatPanel.SetAnalysisResult(nil) // clear old analysis
	m.chatPanel.ClearComments()        // clear old comments
	m.chatPanel.ClearReview()          // clear old review

	// Restore chat from previous session (memory or disk) instead of clearing
	m.chatPanel.ClearChat()
	if m.chatService != nil {
		if msgs := m.chatService.GetSessionMessages(owner, repo, number); len(msgs) > 0 {
			m.chatPanel.RestoreMessages(msgs)
		}
	}
	m.statusBar.SetSelectedPR(number)
	m.prList.SetSelectedPR(number)
	m.prList.SetCIStatus("")
	m.prList.SetReviewDecision("")
	m.diffViewer.SetLoading(number)
	if advance {
		m.showAndFocusPanel(PanelCenter)
	}
	if m.ghClient != nil {
		m.chatPanel.SetCommentsLoading()
		return m, tea.Batch(
			fetchDiffCmd(m.ghClient, owner, repo, number),
			fetchPRDetailCmd(m.ghClient, owner, repo, number),
			fetchCommentsCmd(m.ghClient, owner, repo, number),
			fetchCIStatusCmd(m.ghClient, owner, repo, number),
			fetchReviewsCmd(m.ghClient, owner, repo, number),
			m.diffViewer.spinner.Tick,
			m.chatPanel.spinner.Tick,
		)
	}
	return m, nil
}

// -- Layout & panel helpers --

// focusPanel sets focus to the given panel. If the panel is hidden,
// focuses the next visible panel instead.
func (m *App) focusPanel(p Panel) {
	if !m.panelVisible[p] {
		p = nextVisiblePanel(p, m.panelVisible)
	}
	m.focused = p
	m.prList.SetFocused(p == PanelLeft)
	m.diffViewer.SetFocused(p == PanelCenter)
	m.chatPanel.SetFocused(p == PanelRight)
	m.statusBar.SetState(m.focused, m.mode)
}

func (m *App) recalcLayout() {
	sizes := CalculatePanelSizes(m.width, m.height, m.panelVisible)
	if sizes.TooSmall {
		return
	}

	if sizes.LeftWidth > 0 {
		m.prList.SetSize(sizes.LeftWidth, sizes.PanelHeight)
	}
	if sizes.CenterWidth > 0 {
		m.diffViewer.SetSize(sizes.CenterWidth, sizes.PanelHeight)
	}
	if sizes.RightWidth > 0 {
		m.chatPanel.SetSize(sizes.RightWidth, sizes.PanelHeight)
	}
	m.statusBar.SetWidth(m.width)
	m.statusBar.SetState(m.focused, m.mode)
}

// togglePanel shows or hides a panel. Prevents hiding the last visible panel.
func (m *App) togglePanel(p Panel) {
	if m.panelVisible[p] && visibleCount(m.panelVisible) <= 1 {
		return // can't hide the last visible panel
	}
	m.panelVisible[p] = !m.panelVisible[p]
	if !m.panelVisible[m.focused] {
		m.focusPanel(nextVisiblePanel(m.focused, m.panelVisible))
	}
	m.recalcLayout()
}

// toggleZoom enters or exits zoom mode. When zoomed, only the focused panel
// is visible at full width.
func (m *App) toggleZoom() {
	if m.zoomed {
		m.exitZoom()
	} else {
		m.preZoomVisible = m.panelVisible
		m.panelVisible = [3]bool{}
		m.panelVisible[m.focused] = true
		m.zoomed = true
	}
	m.recalcLayout()
}

// exitZoom restores the pre-zoom panel visibility.
func (m *App) exitZoom() {
	if !m.zoomed {
		return
	}
	m.panelVisible = m.preZoomVisible
	m.zoomed = false
}

// showAndFocusPanel ensures a panel is visible, exits zoom if active,
// and focuses the panel. On narrow terminals (below collapse threshold),
// showing center or right automatically hides the other to keep at most
// 2 panels visible.
func (m *App) showAndFocusPanel(p Panel) {
	if m.zoomed {
		m.exitZoom()
	}
	if !m.panelVisible[p] {
		m.panelVisible[p] = true
	}
	// On small screens, auto-swap center↔right to avoid cramped 3-panel layout
	if m.width > 0 && m.width < m.collapseThreshold {
		switch p {
		case PanelCenter:
			m.panelVisible[PanelRight] = false
		case PanelRight:
			m.panelVisible[PanelCenter] = false
		}
	}
	m.focusPanel(p)
	m.recalcLayout()
}

func (m App) updateFocusedPanel(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch m.focused {
	case PanelLeft:
		m.prList, cmd = m.prList.Update(msg)
	case PanelCenter:
		m.diffViewer, cmd = m.diffViewer.Update(msg)
	case PanelRight:
		m.chatPanel, cmd = m.chatPanel.Update(msg)
	}
	return m, cmd
}

func (m App) updateChatPanel(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.chatPanel, cmd = m.chatPanel.Update(msg)
	return m, cmd
}

// startAnalysis validates state and kicks off Claude analysis.
func (m App) startAnalysis() (tea.Model, tea.Cmd) {
	if m.selectedPR == nil {
		m.chatPanel.SetAnalysisError("No PR selected. Select a PR first.")
		m.chatPanel.activeTab = ChatTabAnalysis
		m.showAndFocusPanel(PanelRight)
		return m, nil
	}
	if m.claudePath == "" {
		m.chatPanel.SetAnalysisError("Claude CLI not found.\nInstall from https://docs.anthropic.com/en/docs/claude-code")
		m.chatPanel.activeTab = ChatTabAnalysis
		m.showAndFocusPanel(PanelRight)
		return m, nil
	}
	if m.analyzing {
		return m, nil
	}
	if len(m.diffFiles) == 0 {
		m.chatPanel.SetAnalysisError("No diff loaded. Select a PR to load its diff first.")
		m.chatPanel.activeTab = ChatTabAnalysis
		m.showAndFocusPanel(PanelRight)
		return m, nil
	}

	// Check cache
	hash := diffContentHash(m.diffFiles)
	cached, _ := m.analysisStore.Get(m.selectedPR.Owner, m.selectedPR.Repo, m.selectedPR.Number)
	if cached != nil && !m.analysisStore.IsStale(cached, hash) {
		m.chatPanel.SetAnalysisResult(cached.Result)
		m.chatPanel.activeTab = ChatTabAnalysis
		m.showAndFocusPanel(PanelRight)
		return m, nil
	}

	// Cancel any previous analysis stream
	if m.analysisStreamCancel != nil {
		m.analysisStreamCancel()
	}

	// Start async streaming analysis
	m.analyzing = true
	m.chatPanel.SetAnalysisLoading()
	m.chatPanel.activeTab = ChatTabAnalysis
	m.showAndFocusPanel(PanelRight)

	pr := m.selectedPR
	files := m.diffFiles
	analyzer := m.analyzer
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(analysisStreamChan)

	go func() {
		defer close(ch)
		diffContent := buildDiffContent(files)
		input := claude.AnalyzeDiffInput{
			Owner:       pr.Owner,
			Repo:        pr.Repo,
			PRNumber:    pr.Number,
			PRTitle:     pr.Title,
			DiffContent: diffContent,
		}

		result, err := analyzer.AnalyzeDiffStream(ctx, input, func(text string) {
			select {
			case ch <- AnalysisStreamChunkMsg{Content: text}:
			case <-ctx.Done():
			}
		})
		if err != nil {
			select {
			case ch <- AnalysisErrorMsg{PRNumber: pr.Number, Err: err}:
			case <-ctx.Done():
			}
		} else {
			select {
			case ch <- AnalysisCompleteMsg{PRNumber: pr.Number, DiffHash: hash, Result: result}:
			case <-ctx.Done():
			}
		}
	}()

	m.analysisStreamCh = ch
	m.analysisStreamCancel = cancel
	return m, tea.Batch(listenForAnalysisStream(ch), m.chatPanel.spinner.Tick)
}

// startAIReview kicks off AI review generation and navigates to the Review tab.
func (m App) startAIReview() (tea.Model, tea.Cmd) {
	if m.selectedPR == nil {
		m.chatPanel.SetAIReviewError("No PR selected. Select a PR first.")
		m.chatPanel.activeTab = ChatTabReview
		m.showAndFocusPanel(PanelRight)
		return m, nil
	}
	if m.claudePath == "" {
		m.chatPanel.SetAIReviewError("Claude CLI not found.\nInstall from https://docs.anthropic.com/en/docs/claude-code")
		m.chatPanel.activeTab = ChatTabReview
		m.showAndFocusPanel(PanelRight)
		return m, nil
	}
	if m.chatPanel.aiReviewLoading {
		return m, nil
	}
	if len(m.diffFiles) == 0 {
		m.chatPanel.SetAIReviewError("No diff loaded. Select a PR to load its diff first.")
		m.chatPanel.activeTab = ChatTabReview
		m.showAndFocusPanel(PanelRight)
		return m, nil
	}

	m.chatPanel.SetAIReviewLoading()
	m.chatPanel.activeTab = ChatTabReview
	m.showAndFocusPanel(PanelRight)

	return m, tea.Batch(aiReviewCmd(m.analyzer, m.selectedPR, m.diffFiles), m.chatPanel.spinner.Tick)
}

// refreshPRList re-fetches the PR lists (To Review + My PRs).
func (m App) refreshPRList() (tea.Model, tea.Cmd) {
	m.prList.SetLoading()
	if m.ghClient != nil {
		return m, tea.Batch(fetchPRsCmd(m.ghClient), m.prList.spinner.Tick)
	}
	return m, tea.Batch(initGHClientCmd, m.prList.spinner.Tick)
}

// refreshSelectedPR re-fetches all data for the currently selected PR
// without clearing chat history, Claude session, or analysis results.
func (m App) refreshSelectedPR() (tea.Model, tea.Cmd) {
	if m.selectedPR == nil {
		return m.refreshPRList()
	}

	pr := m.selectedPR
	if m.ghClient == nil {
		return m, nil
	}

	// Track 5 pending fetches so we can show a success message when all complete.
	m.refreshPending = 5
	m.refreshPRNum = pr.Number
	// Show "Refreshing..." with a long safety-net timeout; it will be replaced
	// by the success message once all fetches finish.
	clearCmd := m.statusBar.SetTemporaryMessage(fmt.Sprintf("Refreshing PR #%d...", pr.Number), 30*time.Second)

	return m, tea.Batch(
		clearCmd,
		fetchDiffCmd(m.ghClient, pr.Owner, pr.Repo, pr.Number),
		fetchPRDetailCmd(m.ghClient, pr.Owner, pr.Repo, pr.Number),
		fetchCommentsCmd(m.ghClient, pr.Owner, pr.Repo, pr.Number),
		fetchCIStatusCmd(m.ghClient, pr.Owner, pr.Repo, pr.Number),
		fetchReviewsCmd(m.ghClient, pr.Owner, pr.Repo, pr.Number),
	)
}

// handleChatSend validates state and kicks off streaming Claude chat.
func (m App) handleChatSend(message string) (tea.Model, tea.Cmd) {
	if m.selectedPR == nil {
		m.chatPanel.SetChatError("No PR selected. Select a PR first.")
		return m, nil
	}
	if m.chatService == nil {
		m.chatPanel.SetChatError("Claude CLI not found.\nInstall from https://docs.anthropic.com/en/docs/claude-code")
		return m, nil
	}

	var prContext string
	var hunksSelected bool
	if selected := m.diffViewer.GetSelectedHunkContent(); selected != "" {
		prContext = buildSelectedHunkContext(m.selectedPR, m.diffFiles, selected)
		hunksSelected = true
	} else {
		prContext = buildChatContext(m.selectedPR, m.diffFiles)
	}

	input := claude.ChatInput{
		Owner:         m.selectedPR.Owner,
		Repo:          m.selectedPR.Repo,
		PRNumber:      m.selectedPR.Number,
		PRContext:     prContext,
		HunksSelected: hunksSelected,
		Message:       message,
	}

	// Cancel any previous stream before starting a new one
	if m.streamCancel != nil {
		m.streamCancel()
	}
	ctx, cancel := context.WithCancel(context.Background())

	ch := make(chatStreamChan)
	go func() {
		defer close(ch)
		response, err := m.chatService.ChatStream(ctx, input, func(text string) {
			select {
			case ch <- ChatStreamChunkMsg{Content: text}:
			case <-ctx.Done():
			}
		})
		if err != nil {
			select {
			case ch <- ChatResponseMsg{Err: err}:
			case <-ctx.Done():
			}
		} else {
			select {
			case ch <- ChatResponseMsg{Content: response}:
			case <-ctx.Done():
			}
		}
	}()

	m.streamChan = ch
	m.streamCancel = cancel
	return m, listenForChatStream(ch)
}

// handleReviewSubmit validates state and dispatches the review action.
func (m App) handleReviewSubmit(msg ReviewSubmitMsg) (tea.Model, tea.Cmd) {
	if m.selectedPR == nil {
		m.chatPanel.SetReviewSubmitted(fmt.Errorf("no PR selected"))
		return m, nil
	}
	if m.ghClient == nil {
		m.chatPanel.SetReviewSubmitted(fmt.Errorf("GitHub client not ready"))
		return m, nil
	}

	pr := m.selectedPR
	client := m.ghClient
	action := msg.Action
	body := msg.Body

	actionLabels := map[ReviewAction]string{
		ReviewApprove:        "Approving",
		ReviewComment:        "Submitting comment on",
		ReviewRequestChanges: "Requesting changes on",
	}
	clearCmd := m.statusBar.SetTemporaryMessage(fmt.Sprintf("%s PR #%d...", actionLabels[action], pr.Number), 3*time.Second)

	// Use app's pending pool instead of msg.InlineComments
	var inlineComments []claude.InlineReviewComment
	for _, c := range m.pendingInlineComments {
		inlineComments = append(inlineComments, c.InlineReviewComment)
	}
	return m, tea.Batch(clearCmd, submitReviewCmd(client, pr.Owner, pr.Repo, pr.Number, action, body, inlineComments))
}

// refreshFetchDone decrements the pending refresh counter and, when all
// fetches have completed, shows a brief success message in the status bar.
func (m *App) refreshFetchDone(prNumber int) tea.Cmd {
	if m.refreshPending <= 0 || prNumber != m.refreshPRNum {
		return nil
	}
	m.refreshPending--
	if m.refreshPending == 0 {
		return m.statusBar.SetTemporaryMessage(fmt.Sprintf("Refreshed PR #%d", prNumber), 3*time.Second)
	}
	return nil
}

// handleCommentPost validates state and posts a comment on the selected PR.
func (m App) handleCommentPost(body string) (tea.Model, tea.Cmd) {
	if m.selectedPR == nil {
		m.chatPanel.SetCommentPosted(fmt.Errorf("no PR selected"))
		return m, nil
	}
	if m.ghClient == nil {
		m.chatPanel.SetCommentPosted(fmt.Errorf("GitHub client not ready"))
		return m, nil
	}

	pr := m.selectedPR
	client := m.ghClient
	return m, func() tea.Msg {
		err := client.PostComment(context.Background(), pr.Owner, pr.Repo, pr.Number, body)
		return CommentPostedMsg{Err: err}
	}
}

// handleInlineCommentAdd manages the pending inline comment pool.
func (m App) handleInlineCommentAdd(msg InlineCommentAddMsg) (tea.Model, tea.Cmd) {
	if msg.Body == "" {
		// Delete: remove the first pending comment at this path:line
		removed := false
		for i, c := range m.pendingInlineComments {
			if c.Path == msg.Path && c.Line == msg.Line {
				m.pendingInlineComments = append(m.pendingInlineComments[:i], m.pendingInlineComments[i+1:]...)
				removed = true
				break
			}
		}
		m.diffViewer.SetPendingInlineComments(m.pendingInlineComments)
		m.chatPanel.SetPendingCommentCount(len(m.pendingInlineComments))
		if removed {
			clearCmd := m.statusBar.SetTemporaryMessage(
				fmt.Sprintf("Comment removed on %s:%d", msg.Path, msg.Line), 2*time.Second)
			return m, clearCmd
		}
		return m, nil
	}

	// Check if editing existing comment at this path:line
	found := false
	for i, c := range m.pendingInlineComments {
		if c.Path == msg.Path && c.Line == msg.Line {
			m.pendingInlineComments[i].Body = msg.Body
			m.pendingInlineComments[i].Source = "user"
			found = true
			break
		}
	}
	if !found {
		m.pendingInlineComments = append(m.pendingInlineComments, PendingInlineComment{
			InlineReviewComment: claude.InlineReviewComment{
				Path: msg.Path,
				Line: msg.Line,
				Side: "RIGHT",
				Body: msg.Body,
			},
			Source: "user",
		})
	}
	m.diffViewer.SetPendingInlineComments(m.pendingInlineComments)
	m.chatPanel.SetPendingCommentCount(len(m.pendingInlineComments))
	action := "added"
	if found {
		action = "updated"
	}
	clearCmd := m.statusBar.SetTemporaryMessage(
		fmt.Sprintf("Comment %s on %s:%d", action, msg.Path, msg.Line), 2*time.Second)
	return m, clearCmd
}

// mergeAIComments integrates AI review comments into the pending pool.
// Old AI-sourced comments are replaced; user-sourced comments are preserved.
func (m *App) mergeAIComments(aiComments []claude.InlineReviewComment) {
	// Remove old AI-sourced comments
	filtered := m.pendingInlineComments[:0]
	for _, c := range m.pendingInlineComments {
		if c.Source != "ai" {
			filtered = append(filtered, c)
		}
	}
	m.pendingInlineComments = filtered

	// Build set of lines with user comments
	userLines := make(map[string]bool)
	for _, c := range m.pendingInlineComments {
		key := fmt.Sprintf("%s:%d", c.Path, c.Line)
		userLines[key] = true
	}

	// Add new AI comments, skipping lines that already have user comments
	for _, c := range aiComments {
		key := fmt.Sprintf("%s:%d", c.Path, c.Line)
		if !userLines[key] {
			m.pendingInlineComments = append(m.pendingInlineComments, PendingInlineComment{
				InlineReviewComment: c,
				Source:              "ai",
			})
		}
	}
}

// snapshotKnownPRs records all current PR keys in the known set.
func (m *App) snapshotKnownPRs(toReview, myPRs []github.PRItem) {
	for _, pr := range toReview {
		m.knownPRs[prKey(pr.Repo.Owner, pr.Repo.Name, pr.Number)] = true
	}
	for _, pr := range myPRs {
		m.knownPRs[prKey(pr.Repo.Owner, pr.Repo.Name, pr.Number)] = true
	}
}

// detectNewPRs returns PRs from the "To Review" list that are not in the known set.
// Only "To Review" is checked — the user generally doesn't need notifications for their own PRs.
func (m *App) detectNewPRs(toReview []github.PRItem) []github.PRItem {
	var newPRs []github.PRItem
	for _, pr := range toReview {
		if !m.knownPRs[prKey(pr.Repo.Owner, pr.Repo.Name, pr.Number)] {
			newPRs = append(newPRs, pr)
		}
	}
	return newPRs
}

// executeCommand dispatches a named command from the command palette.
func (m App) executeCommand(name string) (tea.Model, tea.Cmd) {
	switch name {
	case "analyze":
		return m.startAnalysis()
	case "review":
		return m.startAIReview()
	case "open":
		if m.selectedPR != nil && m.selectedPR.HTMLURL != "" {
			return m, openBrowserCmd(m.selectedPR.HTMLURL)
		}
		return m, nil
	case "clear selection":
		if m.diffViewer.activeTab == TabDiff && len(m.diffViewer.selectedHunks) > 0 {
			for idx := range m.diffViewer.selectedHunks {
				m.diffViewer.markHunkDirty(idx)
			}
			m.diffViewer.selectedHunks = nil
			m.diffViewer.refreshContent()
		}
		return m, nil
	case "comment":
		if m.focused != PanelCenter || m.diffViewer.activeTab != TabDiff || len(m.diffViewer.hunks) == 0 {
			clearCmd := m.statusBar.SetTemporaryMessage("Focus the diff viewer to add comments", 2*time.Second)
			return m, clearCmd
		}
		cmd := m.diffViewer.EnterCommentMode()
		return m, cmd
	case "approve":
		m.chatPanel.activeTab = ChatTabReview
		m.showAndFocusPanel(PanelRight)
		return m, nil
	case "refresh":
		if m.focused == PanelLeft {
			return m.refreshPRList()
		}
		return m.refreshSelectedPR()
	case "new":
		return m, func() tea.Msg { return ChatClearMsg{} }
	case "quit":
		return m, tea.Quit
	case "help":
		m.mode = ModeOverlay
		m.helpOverlay.SetSize(m.width, m.height)
		m.helpOverlay.Show(m.focused)
		m.statusBar.SetState(m.focused, m.mode)
		return m, nil
	case "config":
		m.mode = ModeOverlay
		m.settingsPanel.SetSize(m.width, m.height)
		m.settingsPanel.Show(m.appConfig)
		m.statusBar.SetState(m.focused, m.mode)
		return m, nil
	case "zoom":
		m.toggleZoom()
		return m, nil
	case "prs":
		m.showAndFocusPanel(PanelLeft)
		return m, nil
	case "diff":
		m.showAndFocusPanel(PanelCenter)
		return m, nil
	case "chat":
		m.showAndFocusPanel(PanelRight)
		return m, nil
	case "toggle left":
		if m.zoomed {
			m.exitZoom()
		}
		m.togglePanel(PanelLeft)
		return m, nil
	case "toggle center":
		if m.zoomed {
			m.exitZoom()
		}
		m.togglePanel(PanelCenter)
		return m, nil
	case "toggle right":
		if m.zoomed {
			m.exitZoom()
		}
		m.togglePanel(PanelRight)
		return m, nil
	default:
		input := name
		return m, func() tea.Msg { return CommandNotFoundMsg{Input: input} }
	}
}

// renderCommandOverlay composites the command palette at the bottom of the base view.
func (m App) renderCommandOverlay(base string) string {
	overlay := m.commandMode.View()
	if overlay == "" {
		return base
	}

	overlayLines := strings.Split(overlay, "\n")
	baseLines := strings.Split(base, "\n")

	overlayH := len(overlayLines)
	if overlayH > len(baseLines) {
		overlayH = len(baseLines)
	}

	start := len(baseLines) - overlayH
	for i := 0; i < len(overlayLines) && start+i < len(baseLines); i++ {
		line := overlayLines[i]
		// Pad to full width to cover base content underneath
		lineWidth := lipgloss.Width(line)
		if lineWidth < m.width {
			line += strings.Repeat(" ", m.width-lineWidth)
		}
		baseLines[start+i] = line
	}

	return strings.Join(baseLines, "\n")
}

