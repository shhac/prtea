package ui

import (
	"context"
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/key"
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
	helpOverlay HelpOverlayModel

	// GitHub client (nil until GHClientReadyMsg)
	ghClient GitHubService

	// Currently selected PR (nil until a PR is selected)
	selectedPR *SelectedPR
	diffFiles  []github.PRFile // stored for analysis context

	// Claude integration
	claudePath    string
	appConfig     *config.Config
	analyzer      *claude.Analyzer
	chatService   *claude.ChatService
	analysisStore *claude.AnalysisStore
	analyzing     bool
	streamChan    chatStreamChan // active chat streaming channel

	// Layout state
	focused        Panel
	width          int
	height         int
	panelVisible   [3]bool // which panels are currently visible
	zoomed         bool    // zoom mode: only focused panel shown
	preZoomVisible [3]bool // saved visibility before zoom
	initialized    bool    // whether first WindowSizeMsg has been processed

	// Mode
	mode          AppMode
	pendingAction ConfirmAction
}

// NewApp creates a new App model with default state.
func NewApp() App {
	cfg, _ := config.Load()
	if cfg == nil {
		cfg = &config.Config{ClaudeTimeout: config.DefaultClaudeTimeoutMs}
	}

	claudePath, _ := claude.FindClaude()

	var analyzer *claude.Analyzer
	var chatSvc *claude.ChatService
	if claudePath != "" {
		analyzer = claude.NewAnalyzer(claudePath, cfg.ClaudeTimeoutDuration(), config.PromptsDir())
		chatSvc = claude.NewChatService(claudePath, cfg.ClaudeTimeoutDuration())
	}

	store := claude.NewAnalysisStore(config.AnalysesCacheDir())

	return App{
		prList:        NewPRListModel(),
		diffViewer:    NewDiffViewerModel(),
		chatPanel:     NewChatPanelModel(),
		statusBar:     NewStatusBarModel(),
		helpOverlay:   NewHelpOverlayModel(),
		focused:       PanelLeft,
		panelVisible:  [3]bool{true, true, true},
		mode:          ModeNavigation,
		claudePath:    claudePath,
		appConfig:     cfg,
		analyzer:      analyzer,
		chatService:   chatSvc,
		analysisStore: store,
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
		// Auto-collapse right panel on first render if terminal is narrow
		if !m.initialized {
			m.initialized = true
			if m.width < collapseThreshold {
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
		return m, nil

	case PRsErrorMsg:
		m.prList.SetError(msg.Err.Error())
		return m, nil

	case HelpClosedMsg:
		m.mode = ModeNavigation
		m.statusBar.SetState(m.focused, m.mode)
		return m, nil

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
		return m, nil

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
		return m, nil

	case CommentsLoadedMsg:
		// Race guard: only apply if this is for the currently selected PR
		if m.selectedPR == nil || msg.PRNumber != m.selectedPR.Number {
			return m, nil
		}
		if msg.Err != nil {
			m.chatPanel.SetCommentsError(msg.Err.Error())
		} else {
			m.chatPanel.SetComments(msg.Comments, msg.InlineComments)
		}
		return m, nil

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
		return m, nil

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
		return m, nil

	case AnalysisCompleteMsg:
		m.analyzing = false
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
		m.chatPanel.SetAnalysisError(msg.Err.Error())
		return m, nil

	case CommentPostMsg:
		return m.handleCommentPost(msg.Body)

	case CommentPostedMsg:
		m.chatPanel.SetCommentPosted(msg.Err)
		if msg.Err == nil && m.ghClient != nil && m.selectedPR != nil {
			// Refresh comments after successful post
			return m, fetchCommentsCmd(m.ghClient, m.selectedPR.Owner, m.selectedPR.Repo, m.selectedPR.Number)
		}
		return m, nil

	case PRApproveDoneMsg:
		if m.selectedPR == nil || msg.PRNumber != m.selectedPR.Number {
			return m, nil
		}
		m.statusBar.SetTemporaryMessage(fmt.Sprintf("✓ Approved PR #%d", msg.PRNumber), 3*time.Second)
		return m, fetchReviewsCmd(m.ghClient, m.selectedPR.Owner, m.selectedPR.Repo, m.selectedPR.Number)

	case PRApproveErrMsg:
		m.statusBar.SetTemporaryMessage(fmt.Sprintf("✗ Approve failed: %s", msg.Err), 5*time.Second)
		return m, nil

	case PRCloseDoneMsg:
		if m.selectedPR == nil || msg.PRNumber != m.selectedPR.Number {
			return m, nil
		}
		m.statusBar.SetTemporaryMessage(fmt.Sprintf("✓ Closed PR #%d", msg.PRNumber), 3*time.Second)
		if m.ghClient != nil {
			return m, fetchPRsCmd(m.ghClient)
		}
		return m, nil

	case PRCloseErrMsg:
		m.statusBar.SetTemporaryMessage(fmt.Sprintf("✗ Close failed: %s", msg.Err), 5*time.Second)
		return m, nil

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
			var cmd tea.Cmd
			m.helpOverlay, cmd = m.helpOverlay.Update(msg)
			return m, cmd
		}

		// In insert mode, only Esc is handled globally (via chat panel)
		if m.mode == ModeInsert {
			return m.updateChatPanel(msg)
		}

		// Confirmation prompt captures y/n/Esc
		if m.pendingAction != ConfirmNone {
			return m.handleConfirmKey(msg)
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

		case key.Matches(msg, GlobalKeys.Approve):
			return m.promptConfirm(ConfirmApprove)

		case key.Matches(msg, GlobalKeys.Close):
			return m.promptConfirm(ConfirmClose)
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
	bar := m.statusBar.View()

	base := lipgloss.JoinVertical(lipgloss.Left, panels, bar)

	// Render help overlay on top if active
	if m.helpOverlay.IsVisible() {
		return m.helpOverlay.View()
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
	m.selectedPR = &SelectedPR{
		Owner:   owner,
		Repo:    repo,
		Number:  number,
		Title:   title,
		HTMLURL: htmlURL,
	}
	m.streamChan = nil                 // stop listening to old stream
	m.diffFiles = nil                  // clear old diff data
	m.chatPanel.SetAnalysisResult(nil) // clear old analysis
	m.chatPanel.ClearChat()            // clear old chat
	m.chatPanel.ClearComments()        // clear old comments
	if m.chatService != nil {
		m.chatService.ClearSession(owner, repo, number)
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
// and focuses the panel.
func (m *App) showAndFocusPanel(p Panel) {
	if m.zoomed {
		m.exitZoom()
	}
	if !m.panelVisible[p] {
		m.panelVisible[p] = true
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

	// Start async analysis
	m.analyzing = true
	m.chatPanel.SetAnalysisLoading()
	m.chatPanel.activeTab = ChatTabAnalysis
	m.showAndFocusPanel(PanelRight)

	return m, tea.Batch(analyzeDiffCmd(m.analyzer, m.selectedPR, m.diffFiles, hash), m.chatPanel.spinner.Tick)
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

	m.statusBar.SetTemporaryMessage(fmt.Sprintf("Refreshing PR #%d...", pr.Number), 2*time.Second)

	return m, tea.Batch(
		fetchDiffCmd(m.ghClient, pr.Owner, pr.Repo, pr.Number),
		fetchPRDetailCmd(m.ghClient, pr.Owner, pr.Repo, pr.Number),
		fetchCommentsCmd(m.ghClient, pr.Owner, pr.Repo, pr.Number),
		fetchCIStatusCmd(m.ghClient, pr.Owner, pr.Repo, pr.Number),
		fetchReviewsCmd(m.ghClient, pr.Owner, pr.Repo, pr.Number),
	)
}

// promptConfirm enters the confirmation state for approve/close actions.
func (m App) promptConfirm(action ConfirmAction) (tea.Model, tea.Cmd) {
	if m.selectedPR == nil {
		m.statusBar.SetTemporaryMessage("No PR selected", 2*time.Second)
		return m, nil
	}
	if m.ghClient == nil {
		m.statusBar.SetTemporaryMessage("GitHub client not ready", 2*time.Second)
		return m, nil
	}
	m.pendingAction = action
	m.statusBar.SetPendingAction(action)
	return m, nil
}

// handleConfirmKey processes y/n/Esc during a pending confirmation.
func (m App) handleConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	action := m.pendingAction
	m.pendingAction = ConfirmNone
	m.statusBar.SetPendingAction(ConfirmNone)

	switch msg.String() {
	case "y", "Y":
		pr := m.selectedPR
		client := m.ghClient
		switch action {
		case ConfirmApprove:
			m.statusBar.SetTemporaryMessage(fmt.Sprintf("Approving PR #%d...", pr.Number), 3*time.Second)
			return m, approvePRCmd(client, pr.Owner, pr.Repo, pr.Number)
		case ConfirmClose:
			m.statusBar.SetTemporaryMessage(fmt.Sprintf("Closing PR #%d...", pr.Number), 3*time.Second)
			return m, closePRCmd(client, pr.Owner, pr.Repo, pr.Number)
		}
	case "n", "N", "esc":
		m.statusBar.SetTemporaryMessage("Cancelled", 1*time.Second)
	default:
		// Any other key cancels silently
	}
	return m, nil
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
	if selected := m.diffViewer.GetSelectedHunkContent(); selected != "" {
		prContext = buildSelectedHunkContext(m.selectedPR, selected)
	} else {
		prContext = buildChatContext(m.selectedPR, m.diffFiles)
	}

	input := claude.ChatInput{
		Owner:     m.selectedPR.Owner,
		Repo:      m.selectedPR.Repo,
		PRNumber:  m.selectedPR.Number,
		PRContext: prContext,
		Message:   message,
	}

	ch := make(chatStreamChan)
	go func() {
		defer close(ch)
		response, err := m.chatService.ChatStream(context.Background(), input, func(text string) {
			ch <- ChatStreamChunkMsg{Content: text}
		})
		if err != nil {
			ch <- ChatResponseMsg{Err: err}
		} else {
			ch <- ChatResponseMsg{Content: response}
		}
	}()

	m.streamChan = ch
	return m, listenForChatStream(ch)
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

