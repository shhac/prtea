package ui

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

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
	helpOverlay    HelpOverlayModel
	commandMode    CommandModeModel
	settingsPanel  SettingsModel
	commentOverlay CommentOverlayModel

	// GitHub client (nil until GHClientReadyMsg)
	ghClient GitHubService

	// Currently selected PR session (nil until a PR is selected)
	session *PRSession

	// Claude integration
	claudePath    string
	appConfig     *config.Config
	analyzer      AIAnalyzer
	chatService   AIChatService
	analysisStore *claude.AnalysisStore
	chatStore     *claude.ChatStore

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
}

// NewApp creates a new App model with default state.
func NewApp() App {
	cfg, cfgErr := config.Load()
	if cfg == nil {
		cfg = &config.Config{ClaudeTimeout: config.DefaultClaudeTimeoutMs}
	}
	if cfgErr != nil {
		log.Printf("warning: config load failed, using defaults: %v", cfgErr)
	}

	claudePath, _ := claude.FindClaude()

	chatStore := claude.NewChatStore(config.ChatCacheDir())

	var analyzer AIAnalyzer
	var chatSvc AIChatService
	if claudePath != "" {
		executor := claude.NewCLIExecutor(claudePath)
		analyzer = claude.NewAnalyzer(executor, cfg.ClaudeTimeoutDuration(), config.PromptsDir(), cfg.AnalysisMaxTurns)
		chatSvc = claude.NewChatService(executor, cfg.ClaudeTimeoutDuration(), chatStore, cfg.MaxPromptTokens, cfg.MaxChatHistory, cfg.ChatMaxTurns)
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

	chatPanel := NewChatPanelModel()
	chatPanel.SetStreamCheckpoint(time.Duration(cfg.StreamCheckpointMs) * time.Millisecond)
	chatPanel.SetDefaultReviewAction(cfg.DefaultReviewAction)

	return App{
		prList:            NewPRListModel(defaultTab),
		diffViewer:        NewDiffViewerModel(),
		chatPanel:         chatPanel,
		statusBar:         NewStatusBarModel(),
		helpOverlay:       NewHelpOverlayModel(),
		commandMode:       NewCommandModeModel(),
		settingsPanel:     NewSettingsModel(),
		commentOverlay:    NewCommentOverlayModel(),
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

// Update dispatches messages to domain-specific sub-handlers.
func (m App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg.(type) {
	// Window resize (handled inline — unique)
	case tea.WindowSizeMsg:
		return m.handleWindowSize(msg.(tea.WindowSizeMsg))

	// PR list domain: client init, fetching, polling, selection
	case GHClientReadyMsg, GHClientErrorMsg,
		PRsLoadedMsg, PRsErrorMsg,
		pollTickMsg, pollPRsLoadedMsg,
		PRSelectedMsg, PRSelectedAndAdvanceMsg:
		return m.handlePRListMsg(msg)

	// Diff domain: diff loading, PR detail, comments, CI, reviews
	case HunkSelectedAndAdvanceMsg,
		DiffLoadedMsg, PRDetailLoadedMsg,
		CommentsLoadedMsg, CIStatusLoadedMsg,
		CIRerunRequestMsg, CIRerunDoneMsg, CIRerunErrMsg,
		ReviewsLoadedMsg:
		return m.handleDiffMsg(msg)

	// Analysis domain: AI analysis and AI review
	case AnalysisStreamChunkMsg, AnalysisCompleteMsg, AnalysisErrorMsg,
		AIReviewCompleteMsg, AIReviewErrorMsg:
		return m.handleAnalysisMsg(msg)

	// Chat domain: chat streaming, comments, inline comments
	case ChatClearMsg, ChatSendMsg,
		ChatStreamChunkMsg, ChatResponseMsg,
		CommentPostMsg, CommentPostedMsg,
		InlineCommentAddMsg,
		InlineCommentReplyMsg, InlineCommentReplyDoneMsg:
		return m.handleChatMsg(msg)

	// Review domain: review submission, approval, PR close
	case ReviewValidationMsg, ReviewSubmitMsg,
		ReviewSubmitDoneMsg, ReviewSubmitErrMsg,
		PRApproveDoneMsg, PRApproveErrMsg,
		PRCloseDoneMsg, PRCloseErrMsg:
		return m.handleReviewMsg(msg)

	// Config domain: settings, overlays, mode changes, commands
	case ConfigChangedMsg, HelpClosedMsg, SettingsClosedMsg,
		ShowCommentOverlayMsg, CommentOverlayClosedMsg,
		CommandExecuteMsg, CommandModeExitMsg, CommandNotFoundMsg,
		ModeChangedMsg:
		return m.handleConfigMsg(msg)

	// Infrastructure: spinner ticks, status bar, filter matches
	case spinner.TickMsg:
		return m.handleSpinnerTick(msg.(spinner.TickMsg))

	case StatusBarClearMsg:
		m.statusBar.ClearIfSeqMatch(msg.(StatusBarClearMsg).Seq)
		return m, nil

	// Key input
	case tea.KeyMsg:
		return m.handleKeyMsg(msg.(tea.KeyMsg))
	}

	return m, nil
}

// handleWindowSize processes terminal resize events.
func (m App) handleWindowSize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	m.width = msg.Width
	m.height = msg.Height
	m.helpOverlay.SetSize(m.width, m.height)
	m.commandMode.SetSize(m.width, m.height)
	m.settingsPanel.SetSize(m.width, m.height)
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

// selectPR handles shared setup when a PR is selected: creates a fresh PRSession,
// resets panel state, kicks off data fetches, and optionally advances focus.
func (m App) selectPR(owner, repo string, number int, htmlURL string, advance bool) (tea.Model, tea.Cmd) {
	title := ""
	if item, ok := m.prList.list.SelectedItem().(PRItem); ok {
		title = item.title
	}
	// Save current chat session before switching PRs
	if m.chatService != nil && m.session != nil {
		m.chatService.SaveSession(m.session.Owner, m.session.Repo, m.session.Number)
	}

	// Cancel any active streams from the previous session
	if m.session != nil {
		m.session.CancelStreams()
	}

	// Create a fresh session for the new PR
	m.session = &PRSession{
		Owner:   owner,
		Repo:    repo,
		Number:  number,
		Title:   title,
		HTMLURL: htmlURL,
	}

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
	if m.session == nil {
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
	if m.session.Analyzing {
		return m, nil
	}
	if len(m.session.DiffFiles) == 0 {
		m.chatPanel.SetAnalysisError("No diff loaded. Select a PR to load its diff first.")
		m.chatPanel.activeTab = ChatTabAnalysis
		m.showAndFocusPanel(PanelRight)
		return m, nil
	}

	// Check cache
	hash := diffContentHash(m.session.DiffFiles)
	cached, _ := m.analysisStore.Get(m.session.Owner, m.session.Repo, m.session.Number)
	if cached != nil && !m.analysisStore.IsStale(cached, hash) {
		m.chatPanel.SetAnalysisResult(cached.Result)
		m.chatPanel.activeTab = ChatTabAnalysis
		m.showAndFocusPanel(PanelRight)
		return m, nil
	}

	// Cancel any previous analysis stream
	if m.session.AnalysisStreamCancel != nil {
		m.session.AnalysisStreamCancel()
	}

	// Start async streaming analysis
	m.session.Analyzing = true
	m.chatPanel.SetAnalysisLoading()
	m.chatPanel.activeTab = ChatTabAnalysis
	m.showAndFocusPanel(PanelRight)

	s := m.session
	files := s.DiffFiles
	analyzer := m.analyzer
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(analysisStreamChan)

	go func() {
		defer close(ch)
		diffContent := buildDiffContent(files)
		input := claude.AnalyzeDiffInput{
			Owner:       s.Owner,
			Repo:        s.Repo,
			PRNumber:    s.Number,
			PRTitle:     s.Title,
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
			case ch <- AnalysisErrorMsg{PRNumber: s.Number, Err: err}:
			case <-ctx.Done():
			}
		} else {
			select {
			case ch <- AnalysisCompleteMsg{PRNumber: s.Number, DiffHash: hash, Result: result}:
			case <-ctx.Done():
			}
		}
	}()

	m.session.AnalysisStreamCh = ch
	m.session.AnalysisStreamCancel = cancel
	return m, tea.Batch(listenForAnalysisStream(ch), m.chatPanel.spinner.Tick)
}

// startAIReview kicks off AI review generation and navigates to the Review tab.
func (m App) startAIReview() (tea.Model, tea.Cmd) {
	if m.session == nil {
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
	if len(m.session.DiffFiles) == 0 {
		m.chatPanel.SetAIReviewError("No diff loaded. Select a PR to load its diff first.")
		m.chatPanel.activeTab = ChatTabReview
		m.showAndFocusPanel(PanelRight)
		return m, nil
	}

	m.chatPanel.SetAIReviewLoading()
	m.chatPanel.activeTab = ChatTabReview
	m.showAndFocusPanel(PanelRight)

	return m, tea.Batch(aiReviewCmd(m.analyzer, m.session, m.session.DiffFiles), m.chatPanel.spinner.Tick)
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
	if m.session == nil {
		return m.refreshPRList()
	}

	s := m.session
	if m.ghClient == nil {
		return m, nil
	}

	// Track 5 pending fetches so we can show a success message when all complete.
	m.refreshPending = 5
	m.refreshPRNum = s.Number
	clearCmd := m.statusBar.SetTemporaryMessage(fmt.Sprintf("Refreshing PR #%d...", s.Number), 30*time.Second)

	return m, tea.Batch(
		clearCmd,
		fetchDiffCmd(m.ghClient, s.Owner, s.Repo, s.Number),
		fetchPRDetailCmd(m.ghClient, s.Owner, s.Repo, s.Number),
		fetchCommentsCmd(m.ghClient, s.Owner, s.Repo, s.Number),
		fetchCIStatusCmd(m.ghClient, s.Owner, s.Repo, s.Number),
		fetchReviewsCmd(m.ghClient, s.Owner, s.Repo, s.Number),
	)
}

// handleChatSend validates state and kicks off streaming Claude chat.
func (m App) handleChatSend(message string) (tea.Model, tea.Cmd) {
	if m.session == nil {
		m.chatPanel.SetChatError("No PR selected. Select a PR first.")
		return m, nil
	}
	if m.chatService == nil {
		m.chatPanel.SetChatError("Claude CLI not found.\nInstall from https://docs.anthropic.com/en/docs/claude-code")
		return m, nil
	}

	s := m.session
	var prContext string
	var hunksSelected bool
	if selected := m.diffViewer.GetSelectedHunkContent(); selected != "" {
		prContext = buildSelectedHunkContext(s, s.DiffFiles, selected)
		hunksSelected = true
	} else {
		prContext = buildChatContext(s, s.DiffFiles)
	}

	input := claude.ChatInput{
		Owner:         s.Owner,
		Repo:          s.Repo,
		PRNumber:      s.Number,
		PRContext:     prContext,
		HunksSelected: hunksSelected,
		Message:       message,
	}

	// Cancel any previous stream before starting a new one
	if s.StreamCancel != nil {
		s.StreamCancel()
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

	s.StreamChan = ch
	s.StreamCancel = cancel
	return m, listenForChatStream(ch)
}

// handleReviewSubmit validates state and dispatches the review action.
func (m App) handleReviewSubmit(msg ReviewSubmitMsg) (tea.Model, tea.Cmd) {
	if m.session == nil {
		m.chatPanel.SetReviewSubmitted(fmt.Errorf("no PR selected"))
		return m, nil
	}
	if m.ghClient == nil {
		m.chatPanel.SetReviewSubmitted(fmt.Errorf("GitHub client not ready"))
		return m, nil
	}

	s := m.session
	client := m.ghClient
	action := msg.Action
	body := msg.Body

	actionLabels := map[ReviewAction]string{
		ReviewApprove:        "Approving",
		ReviewComment:        "Submitting comment on",
		ReviewRequestChanges: "Requesting changes on",
	}
	clearCmd := m.statusBar.SetTemporaryMessage(fmt.Sprintf("%s PR #%d...", actionLabels[action], s.Number), 3*time.Second)

	// Use session's pending pool instead of msg.InlineComments
	var inlineComments []claude.InlineReviewComment
	for _, c := range s.PendingInlineComments {
		inlineComments = append(inlineComments, c.InlineReviewComment)
	}
	return m, tea.Batch(clearCmd, submitReviewCmd(client, s.Owner, s.Repo, s.Number, action, body, inlineComments))
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
	if m.session == nil {
		m.chatPanel.SetCommentPosted(fmt.Errorf("no PR selected"))
		return m, nil
	}
	if m.ghClient == nil {
		m.chatPanel.SetCommentPosted(fmt.Errorf("GitHub client not ready"))
		return m, nil
	}

	s := m.session
	client := m.ghClient
	return m, func() tea.Msg {
		err := client.PostComment(context.Background(), s.Owner, s.Repo, s.Number, body)
		return CommentPostedMsg{Err: err}
	}
}

// handleInlineCommentAdd manages the pending inline comment pool.
func (m App) handleInlineCommentAdd(msg InlineCommentAddMsg) (tea.Model, tea.Cmd) {
	if m.session == nil {
		return m, nil
	}

	if msg.Body == "" {
		// Delete: remove the first pending comment at this path:line
		removed := false
		for i, c := range m.session.PendingInlineComments {
			if c.Path == msg.Path && c.Line == msg.Line {
				m.session.PendingInlineComments = append(m.session.PendingInlineComments[:i], m.session.PendingInlineComments[i+1:]...)
				removed = true
				break
			}
		}
		m.diffViewer.SetPendingInlineComments(m.session.PendingInlineComments)
		m.chatPanel.SetPendingCommentCount(len(m.session.PendingInlineComments))
		if removed {
			clearCmd := m.statusBar.SetTemporaryMessage(
				fmt.Sprintf("Comment removed on %s:%d", msg.Path, msg.Line), 2*time.Second)
			return m, clearCmd
		}
		return m, nil
	}

	// Check if editing existing comment at this path:line
	found := false
	for i, c := range m.session.PendingInlineComments {
		if c.Path == msg.Path && c.Line == msg.Line {
			m.session.PendingInlineComments[i].Body = msg.Body
			m.session.PendingInlineComments[i].Source = "user"
			found = true
			break
		}
	}
	if !found {
		comment := PendingInlineComment{
			InlineReviewComment: claude.InlineReviewComment{
				Path: msg.Path,
				Line: msg.Line,
				Side: "RIGHT",
				Body: msg.Body,
			},
			Source: "user",
		}
		if msg.StartLine > 0 {
			comment.StartLine = msg.StartLine
			comment.StartSide = "RIGHT"
		}
		m.session.PendingInlineComments = append(m.session.PendingInlineComments, comment)
	}
	m.diffViewer.SetPendingInlineComments(m.session.PendingInlineComments)
	m.chatPanel.SetPendingCommentCount(len(m.session.PendingInlineComments))
	action := "added"
	if found {
		action = "updated"
	}
	var target string
	if msg.StartLine > 0 {
		target = fmt.Sprintf("%s:%d-%d", msg.Path, msg.StartLine, msg.Line)
	} else {
		target = fmt.Sprintf("%s:%d", msg.Path, msg.Line)
	}
	clearCmd := m.statusBar.SetTemporaryMessage(
		fmt.Sprintf("Comment %s on %s", action, target), 2*time.Second)
	return m, clearCmd
}

// mergeAIComments integrates AI review comments into the pending pool.
// Old AI-sourced comments are replaced; user-sourced comments are preserved.
func (m *App) mergeAIComments(aiComments []claude.InlineReviewComment) {
	if m.session == nil {
		return
	}

	// Remove old AI-sourced comments
	filtered := m.session.PendingInlineComments[:0]
	for _, c := range m.session.PendingInlineComments {
		if c.Source != "ai" {
			filtered = append(filtered, c)
		}
	}
	m.session.PendingInlineComments = filtered

	// Build set of lines with user comments
	userLines := make(map[string]bool)
	for _, c := range m.session.PendingInlineComments {
		key := fmt.Sprintf("%s:%d", c.Path, c.Line)
		userLines[key] = true
	}

	// Add new AI comments, skipping lines that already have user comments
	for _, c := range aiComments {
		key := fmt.Sprintf("%s:%d", c.Path, c.Line)
		if !userLines[key] {
			m.session.PendingInlineComments = append(m.session.PendingInlineComments, PendingInlineComment{
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
		if m.session != nil && m.session.HTMLURL != "" {
			return m, openBrowserCmd(m.session.HTMLURL)
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
	case "rerun ci":
		return m, func() tea.Msg { return CIRerunRequestMsg{} }
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
