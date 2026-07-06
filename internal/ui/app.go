package ui

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/shhac/prtea/internal/ai"
	"github.com/shhac/prtea/internal/config"
	"github.com/shhac/prtea/internal/demo"
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
	helpOverlay    HelpOverlayModel
	commandMode    CommandModeModel
	commentOverlay CommentOverlayModel

	// GitHub client (nil until GHClientReadyMsg)
	ghClient GitHubService

	// Currently selected PR session (nil until a PR is selected)
	session *PRSession

	// AI integration
	appConfig   *config.Config
	aiEngine    AIEngine
	threadStore *ai.ThreadStore

	// Layout state
	focused           Panel
	width             int
	height            int
	panelVisible      [3]bool // which panels are currently visible
	zoomed            bool    // zoom mode: only focused panel shown
	preZoomVisible    [3]bool // saved visibility before zoom
	initialized       bool    // whether first WindowSizeMsg has been processed
	collapseThreshold int     // terminal width below which panels auto-collapse

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
	notifyEnabled   bool            // whether OS notifications are enabled
	initialLoadDone bool            // true after first successful PR fetch
	knownPRs        map[string]bool // PR keys seen since boot (for new-PR detection)

	// Demo mode
	demoMode bool
}

// AppOption configures the App during construction.
type AppOption func(*App)

// WithDemo enables demo mode with mock GitHub data.
func WithDemo() AppOption {
	return func(a *App) { a.demoMode = true }
}

// NewApp creates a new App model with default state.
func NewApp(opts ...AppOption) App {
	cfg, cfgErr := config.Load()
	if cfg == nil {
		cfg = config.Defaults()
	}
	if cfgErr != nil {
		log.Printf("warning: config load failed, using defaults: %v", cfgErr)
	}

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

	chatPanel := NewChatPanelModel()
	chatPanel.SetDefaultReviewAction(cfg.DefaultReviewAction)

	app := App{
		prList:            NewPRListModel(defaultTab),
		diffViewer:        NewDiffViewerModel(),
		chatPanel:         chatPanel,
		statusBar:         NewStatusBarModel(),
		helpOverlay:       NewHelpOverlayModel(),
		commandMode:       NewCommandModeModel(),
		commentOverlay:    NewCommentOverlayModel(),
		focused:           PanelLeft,
		panelVisible:      panelVisible,
		mode:              ModeNavigation,
		collapseThreshold: cfg.CollapseThreshold,
		appConfig:         cfg,
		pollInterval:      cfg.PollIntervalDuration(),
		pollEnabled:       cfg.PollEnabled,
		notifyEnabled:     cfg.NotificationsEnabled,
		knownPRs:          make(map[string]bool),
	}
	for _, opt := range opts {
		opt(&app)
	}

	// Wire the AI engine and thread store after options so demo mode can
	// inject its fake and keep demo transcripts out of the real cache.
	if app.demoMode {
		app.aiEngine = demo.NewAIEngine()
		app.threadStore = ai.NewThreadStore(filepath.Join(os.TempDir(), "prtea-demo-threads"))
	} else {
		app.threadStore = ai.NewThreadStore(config.ThreadsCacheDir())
		if codexPath, err := ai.FindCodex(); err == nil {
			executor := ai.NewCLIExecutor(codexPath)
			app.aiEngine = ai.NewCodexEngine(executor, cfg.CodexModel, cfg.CodexEffort)
		}
	}
	return app
}

func (m App) Init() tea.Cmd {
	initCmd := initGHClientCmd
	if m.demoMode {
		initCmd = initDemoClientCmd
	}
	return tea.Batch(initCmd, m.prList.spinner.Tick)
}

// initDemoClientCmd creates a demo GitHubService with fake data.
func initDemoClientCmd() tea.Msg {
	return GHClientReadyMsg{Client: demo.NewService()}
}

// Update is the single message dispatch: every message type routes directly
// to its handler, so an unhandled message is structurally impossible to hide.
func (m App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	// Window & input
	case tea.WindowSizeMsg:
		return m.handleWindowSize(msg)
	case tea.KeyMsg:
		return m.handleKeyMsg(msg)
	case spinner.TickMsg:
		return m.handleSpinnerTick(msg)
	case StatusBarClearMsg:
		m.statusBar.ClearIfSeqMatch(msg.Seq)
		return m, nil

	// GitHub client & PR list
	case GHClientReadyMsg:
		return m.handleGHClientReady(msg)
	case GHClientErrorMsg:
		m.prList.SetError(msg.Err.Error())
		return m, nil
	case PRsLoadedMsg:
		return m.handlePRsLoaded(msg)
	case PRsErrorMsg:
		m.prList.SetError(msg.Err.Error())
		return m, nil
	case PRReviewDecisionsMsg:
		m.prList.UpdateReviewDecisions(msg.Decisions)
		return m, nil
	case pollTickMsg:
		return m.handlePollTick()
	case pollPRsLoadedMsg:
		return m.handlePollPRsLoaded(msg)
	case pollErrorMsg:
		return m, m.statusBar.SetTemporaryMessage("Poll error: "+formatUserError(msg.Err.Error()), 5*time.Second)
	case PRSelectedMsg:
		return m.selectPR(msg.Owner, msg.Repo, msg.Number, msg.HTMLURL, msg.Advance)
	case list.FilterMatchesMsg:
		var cmd tea.Cmd
		m.prList, cmd = m.prList.Update(msg)
		return m, cmd

	// PR data loads
	case DiffLoadedMsg:
		return m.handleDiffLoaded(msg)
	case PRDetailLoadedMsg:
		return m.handlePRDetailLoaded(msg)
	case CommentsLoadedMsg:
		return m.handleCommentsLoaded(msg)
	case CIStatusLoadedMsg:
		return m.handleCIStatusLoaded(msg)
	case ReviewsLoadedMsg:
		return m.handleReviewsLoaded(msg)

	// CI re-runs
	case CIRerunRequestMsg:
		return m.handleCIRerunRequest()
	case CIRerunDoneMsg:
		return m.handleCIRerunDone(msg)
	case CIRerunErrMsg:
		return m, m.statusBar.SetTemporaryMessage(fmt.Sprintf("CI re-run failed: %s", formatUserError(msg.Err.Error())), 5*time.Second)

	// AI turns & chat
	case ChatClearMsg:
		return m.handleChatClear()
	case ChatSendMsg:
		return m.handleChatSend(msg.Message)
	case AIEventMsg:
		return m.handleAIEvent(msg)
	case AIActionRespondMsg:
		return m.handleActionRespond(msg)
	case AIActionResultMsg:
		return m.handleActionResult(msg)

	// Comments
	case CommentPostMsg:
		return m.handleCommentPost(msg.Body)
	case CommentPostedMsg:
		return m.handleCommentPosted(msg)
	case InlineCommentAddMsg:
		return m.handleInlineCommentAdd(msg)
	case InlineCommentReplyMsg:
		return m.handleInlineCommentReply(msg)
	case InlineCommentReplyDoneMsg:
		return m.handleInlineCommentReplyDone(msg)

	// Review submission
	case ReviewValidationMsg:
		return m, m.statusBar.SetTemporaryMessage(msg.Message, 3*time.Second)
	case ReviewSubmitMsg:
		return m.handleReviewSubmit(msg)
	case ReviewSubmitDoneMsg:
		return m.handleReviewSubmitDone(msg)
	case ReviewSubmitErrMsg:
		return m.handleReviewSubmitErr(msg)

	// Overlays, palette, modes
	case HunkSelectedAndAdvanceMsg:
		m.showAndFocusPanel(PanelRight)
		return m, nil
	case HelpClosedMsg, CommentOverlayClosedMsg, CommandModeExitMsg:
		m.setMode(ModeNavigation)
		return m, nil
	case ShowCommentOverlayMsg:
		return m.handleShowCommentOverlay(msg)
	case CommandExecuteMsg:
		m.setMode(ModeNavigation)
		return m.executeCommand(msg.Name)
	case CommandNotFoundMsg:
		return m, m.statusBar.SetTemporaryMessage(fmt.Sprintf("Unknown command: %s", msg.Input), 2*time.Second)
	case ConfigOpenedMsg:
		if msg.Err != nil {
			return m, m.statusBar.SetTemporaryMessage(fmt.Sprintf("Could not open config: %s", formatUserError(msg.Err.Error())), 5*time.Second)
		}
		return m, m.statusBar.SetTemporaryMessage("Opened "+msg.Path+" — restart to apply changes", 5*time.Second)
	case ModeChangedMsg:
		return m.handleChatModeChanged(msg)
	}

	return m, nil
}

// handleWindowSize processes terminal resize events.
func (m App) handleWindowSize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	m.width = msg.Width
	m.height = msg.Height
	m.helpOverlay.SetSize(m.width, m.height)
	m.commandMode.SetSize(m.width, m.height)
	m.commentOverlay.SetSize(m.width, m.height)
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

	// Render comment overlay on top if active
	if m.commentOverlay.IsVisible() {
		return m.commentOverlay.View()
	}

	// Render help overlay on top if active
	if m.helpOverlay.IsVisible() {
		return m.helpOverlay.View()
	}

	// Render command palette at the bottom if active
	if m.commandMode.IsActive() {
		return m.renderCommandOverlay(base)
	}

	return base
}

// selectPR handles shared setup when a PR is selected: creates a fresh PRSession,
// resets panel state, kicks off data fetches, and optionally advances focus.
func (m App) selectPR(owner, repo string, number int, htmlURL string, advance bool) (tea.Model, tea.Cmd) {
	title := ""
	if item, ok := m.prList.list.SelectedItem().(PRItem); ok {
		title = item.title
	}
	// Persist the outgoing PR's thread and cancel any in-flight turn
	if m.session != nil {
		m.persistThread()
		m.session.CancelAITurn()
	}

	// Create a fresh session for the new PR
	m.session = &PRSession{
		Owner:   owner,
		Repo:    repo,
		Number:  number,
		Title:   title,
		HTMLURL: htmlURL,
	}

	m.chatPanel.ClearComments() // clear old comments
	m.chatPanel.ClearReview()   // clear old review

	// Restore the PR's thread (transcript + thread ID) from disk
	m.chatPanel.ClearChat()
	if cached, err := m.threadStore.Get(owner, repo, number); err == nil && cached != nil {
		m.session.ThreadID = cached.ThreadID
		if len(cached.Messages) > 0 {
			m.chatPanel.RestoreMessages(cached.Messages)
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
		cmds := fetchAllPRDataCmds(m.ghClient, owner, repo, number)
		cmds = append(cmds, m.diffViewer.spinner.Tick, m.chatPanel.spinner.Tick)
		return m, tea.Batch(cmds...)
	}
	return m, nil
}

// setMode updates the app mode and synchronises the status bar.
func (m *App) setMode(mode AppMode) {
	m.mode = mode
	m.statusBar.SetState(m.focused, m.mode)
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

// togglePanel shows or hides a panel, exiting zoom first. Prevents hiding
// the last visible panel.
func (m *App) togglePanel(p Panel) {
	if m.zoomed {
		m.exitZoom()
	}
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

// refreshPRList re-fetches the PR lists (To Review + My PRs).
func (m App) refreshPRList() (tea.Model, tea.Cmd) {
	m.prList.SetLoading()
	if m.ghClient != nil {
		return m, tea.Batch(fetchPRsCmd(m.ghClient), m.prList.spinner.Tick)
	}
	return m, tea.Batch(initGHClientCmd, m.prList.spinner.Tick)
}

// refreshSelectedPR re-fetches all data for the currently selected PR
// without clearing the chat thread.
func (m App) refreshSelectedPR() (tea.Model, tea.Cmd) {
	if m.session == nil {
		return m.refreshPRList()
	}

	s := m.session
	if m.ghClient == nil {
		return m, nil
	}

	// Track pending fetches so we can show a success message when all complete.
	fetchCmds := fetchAllPRDataCmds(m.ghClient, s.Owner, s.Repo, s.Number)
	m.refreshPending = len(fetchCmds)
	m.refreshPRNum = s.Number
	clearCmd := m.statusBar.SetTemporaryMessage(fmt.Sprintf("Refreshing PR #%d...", s.Number), 30*time.Second)

	return m, tea.Batch(append(fetchCmds, clearCmd)...)
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
