package ui

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os/exec"
	"runtime"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/shhac/prtea/internal/claude"
	"github.com/shhac/prtea/internal/config"
	"github.com/shhac/prtea/internal/github"
)

// -- Async message types --

// GHClientReadyMsg is sent when the GitHub client has been created successfully.
type GHClientReadyMsg struct {
	Client *github.Client
}

// GHClientErrorMsg is sent when the GitHub client fails to initialize.
type GHClientErrorMsg struct {
	Err error
}

// PRsLoadedMsg is sent when PR data has been fetched successfully.
type PRsLoadedMsg struct {
	ToReview []github.PRItem
	MyPRs    []github.PRItem
}

// PRsErrorMsg is sent when PR fetching fails.
type PRsErrorMsg struct {
	Err error
}

// DiffLoadedMsg is sent when PR diff data has been fetched.
type DiffLoadedMsg struct {
	PRNumber int
	Files    []github.PRFile
	Err      error
}

// SelectedPR tracks the currently selected PR's metadata for global actions.
type SelectedPR struct {
	Owner   string
	Repo    string
	Number  int
	Title   string
	HTMLURL string
}

// PRDetailLoadedMsg is sent when PR detail data has been fetched.
type PRDetailLoadedMsg struct {
	PRNumber int
	Detail   *github.PRDetail
	Err      error
}

// AnalysisCompleteMsg is sent when Claude analysis finishes successfully.
type AnalysisCompleteMsg struct {
	PRNumber int
	DiffHash string
	Result   *claude.AnalysisResult
}

// AnalysisErrorMsg is sent when Claude analysis fails.
type AnalysisErrorMsg struct {
	Err error
}

// chatStreamChan carries streaming chunks and the final response from Claude chat.
type chatStreamChan chan tea.Msg

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
	ghClient *github.Client

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
	mode AppMode
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
	return initGHClientCmd
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

	case PRRefreshMsg:
		m.prList.SetLoading()
		if m.ghClient != nil {
			return m, fetchPRsCmd(m.ghClient)
		}
		// Client not ready yet — retry init
		return m, initGHClientCmd

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
		title := ""
		if item, ok := m.prList.list.SelectedItem().(PRItem); ok {
			title = item.title
		}
		m.selectedPR = &SelectedPR{
			Owner:   msg.Owner,
			Repo:    msg.Repo,
			Number:  msg.Number,
			Title:   title,
			HTMLURL: msg.HTMLURL,
		}
		m.streamChan = nil                 // stop listening to old stream
		m.diffFiles = nil                  // clear old diff data
		m.chatPanel.SetAnalysisResult(nil) // clear old analysis
		m.chatPanel.ClearChat()            // clear old chat
		if m.chatService != nil {
			m.chatService.ClearSession(msg.Owner, msg.Repo, msg.Number)
		}
		m.statusBar.SetSelectedPR(msg.Number)
		m.diffViewer.SetLoading(msg.Number)
		if m.ghClient != nil {
			return m, tea.Batch(
				fetchDiffCmd(m.ghClient, msg.Owner, msg.Repo, msg.Number),
				fetchPRDetailCmd(m.ghClient, msg.Owner, msg.Repo, msg.Number),
			)
		}
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
		if msg.Err == nil && msg.Detail != nil {
			m.diffViewer.SetPRInfo(
				msg.Detail.Title,
				msg.Detail.Body,
				msg.Detail.Author.Login,
				msg.Detail.HTMLURL,
			)
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

// -- Async commands --

// initGHClientCmd creates the GitHub client in a goroutine.
func initGHClientCmd() tea.Msg {
	client, err := github.NewClient()
	if err != nil {
		return GHClientErrorMsg{Err: err}
	}
	return GHClientReadyMsg{Client: client}
}

// fetchPRsCmd returns a command that fetches both PR lists.
func fetchPRsCmd(client *github.Client) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()

		toReview, err := client.GetPRsForReview(ctx)
		if err != nil {
			return PRsErrorMsg{Err: err}
		}

		myPRs, err := client.GetMyPRs(ctx)
		if err != nil {
			return PRsErrorMsg{Err: err}
		}

		return PRsLoadedMsg{
			ToReview: toReview,
			MyPRs:    myPRs,
		}
	}
}

// convertPRItems converts github.PRItem slice to list.Item slice.
func convertPRItems(prs []github.PRItem) []list.Item {
	items := make([]list.Item, len(prs))
	for i, pr := range prs {
		items[i] = PRItem{
			number:   pr.Number,
			title:    pr.Title,
			repo:     pr.Repo.Name,
			owner:    pr.Repo.Owner,
			repoFull: pr.Repo.FullName,
			author:   pr.Author.Login,
			htmlURL:  pr.HTMLURL,
		}
	}
	return items
}

// fetchDiffCmd returns a command that fetches PR file diffs.
func fetchDiffCmd(client *github.Client, owner, repo string, number int) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		files, err := client.GetPRFiles(ctx, owner, repo, number)
		if err != nil {
			return DiffLoadedMsg{PRNumber: number, Err: err}
		}
		return DiffLoadedMsg{PRNumber: number, Files: files}
	}
}

// fetchPRDetailCmd returns a command that fetches PR detail (title, body, etc.).
func fetchPRDetailCmd(client *github.Client, owner, repo string, number int) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		detail, err := client.GetPRDetail(ctx, owner, repo, number)
		if err != nil {
			return PRDetailLoadedMsg{PRNumber: number, Err: err}
		}
		return PRDetailLoadedMsg{PRNumber: number, Detail: detail}
	}
}

// openBrowserCmd returns a command that opens a URL in the default browser.
func openBrowserCmd(url string) tea.Cmd {
	return func() tea.Msg {
		var cmd *exec.Cmd
		switch runtime.GOOS {
		case "darwin":
			cmd = exec.Command("open", url)
		case "windows":
			cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
		default: // linux, freebsd, etc.
			cmd = exec.Command("xdg-open", url)
		}
		_ = cmd.Start()
		return nil
	}
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

	return m, analyzeDiffCmd(m.analyzer, m.selectedPR, m.diffFiles, hash)
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

// listenForChatStream returns a tea.Cmd that reads the next message from the streaming channel.
func listenForChatStream(ch chatStreamChan) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return nil
		}
		return msg
	}
}

// buildChatContext constructs the PR context string for chat from metadata + diff.
func buildChatContext(pr *SelectedPR, files []github.PRFile) string {
	var b strings.Builder
	fmt.Fprintf(&b, "PR #%d: \"%s\" in %s/%s\n", pr.Number, pr.Title, pr.Owner, pr.Repo)
	if len(files) > 0 {
		b.WriteString("\nChanges in this PR:\n\n")
		b.WriteString(buildDiffContent(files))
	} else {
		b.WriteString("\n(Diff not yet loaded)")
	}
	return b.String()
}

// analyzeDiffCmd returns a command that runs Claude analysis with inline diff content.
func analyzeDiffCmd(analyzer *claude.Analyzer, pr *SelectedPR, files []github.PRFile, diffHash string) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()

		diffContent := buildDiffContent(files)

		input := claude.AnalyzeDiffInput{
			Owner:       pr.Owner,
			Repo:        pr.Repo,
			PRNumber:    pr.Number,
			PRTitle:     pr.Title,
			DiffContent: diffContent,
		}

		result, err := analyzer.AnalyzeDiff(ctx, input, nil)
		if err != nil {
			return AnalysisErrorMsg{Err: err}
		}

		return AnalysisCompleteMsg{
			PRNumber: pr.Number,
			DiffHash: diffHash,
			Result:   result,
		}
	}
}

// buildSelectedHunkContext constructs PR context using only selected hunks.
func buildSelectedHunkContext(pr *SelectedPR, selectedDiff string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "PR #%d: \"%s\" in %s/%s\n", pr.Number, pr.Title, pr.Owner, pr.Repo)
	b.WriteString("\nSelected hunks from this PR:\n\n")
	b.WriteString(selectedDiff)
	return b.String()
}

// buildDiffContent constructs a unified diff string from PR files.
func buildDiffContent(files []github.PRFile) string {
	var b strings.Builder
	for _, f := range files {
		b.WriteString(fmt.Sprintf("--- a/%s\n", f.Filename))
		b.WriteString(fmt.Sprintf("+++ b/%s\n", f.Filename))
		if f.Patch != "" {
			b.WriteString(f.Patch)
			b.WriteString("\n")
		} else {
			b.WriteString("(binary or too large to display)\n")
		}
		b.WriteString("\n")
	}
	return b.String()
}

// diffContentHash computes a short hash of the diff content for cache staleness checks.
func diffContentHash(files []github.PRFile) string {
	h := sha256.New()
	for _, f := range files {
		h.Write([]byte(f.Filename))
		h.Write([]byte(f.Patch))
	}
	return fmt.Sprintf("%x", h.Sum(nil))[:16]
}
