package ui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/shhac/prtea/internal/claude"
	"github.com/shhac/prtea/internal/github"
)

// DiffViewerTab identifies which sub-tab is active in the diff viewer.
type DiffViewerTab int

const (
	TabDiff   DiffViewerTab = iota
	TabPRInfo
	TabCI
)

// DiffHunk represents a single hunk within a file's patch.
type DiffHunk struct {
	FileIndex int
	Filename  string
	Header    string   // the @@ line
	Lines     []string // all lines including the @@ header
}

// ghCommentThread groups a root GitHub inline comment with its replies.
type ghCommentThread struct {
	Root    github.InlineComment
	Replies []github.InlineComment
}

// parsePatchHunks splits a file's patch string into individual hunks.
func parsePatchHunks(fileIndex int, filename string, patch string) []DiffHunk {
	lines := strings.Split(patch, "\n")
	var hunks []DiffHunk
	var current *DiffHunk

	for _, line := range lines {
		if strings.HasPrefix(line, "@@") {
			if current != nil {
				hunks = append(hunks, *current)
			}
			current = &DiffHunk{
				FileIndex: fileIndex,
				Filename:  filename,
				Header:    line,
				Lines:     []string{line},
			}
		} else if current != nil {
			current.Lines = append(current.Lines, line)
		}
	}
	if current != nil {
		hunks = append(hunks, *current)
	}

	return hunks
}

// matchPos represents a single search match position within a line.
type matchPos struct {
	startCol int
	endCol   int
}

// searchMatch identifies a single search match globally across all hunks.
type searchMatch struct {
	hunkIdx    int
	lineInHunk int
	startCol   int
	endCol     int
}

// parseAllHunks parses hunks from all files once and populates m.hunks.
func (m *DiffViewerModel) parseAllHunks() {
	m.hunks = nil
	for i, f := range m.files {
		if f.Patch == "" {
			continue
		}
		fileHunks := parsePatchHunks(i, f.Filename, f.Patch)
		m.hunks = append(m.hunks, fileHunks...)
	}
}

// DiffViewerModel manages the diff viewer panel.
type DiffViewerModel struct {
	viewport  viewport.Model
	spinner   spinner.Model
	activeTab DiffViewerTab
	width     int
	height    int
	focused   bool
	ready     bool

	// Diff data
	files          []github.PRFile
	fileOffsets    []int // viewport line index where each file header starts
	currentFileIdx int
	loading        bool
	prNumber       int
	err            error

	// Hunk navigation and selection
	hunks          []DiffHunk   // all parsed hunks across all files
	hunkOffsets    []int        // viewport line offset where each hunk starts
	focusedHunkIdx int          // explicitly tracked focused hunk
	selectedHunks  map[int]bool // hunk index → selected

	// Cached rendering — avoids re-parsing and re-styling on every scroll.
	// On scroll, only the old and new focused hunks are re-rendered (O(hunk_size)
	// lipgloss calls instead of O(total_lines)).
	cachedLines       []string     // per-line styled output (nil = needs full rebuild)
	hunkLineRanges    [][2]int     // [start, end) line indices in cachedLines per hunk
	lastRenderedFocus int          // focusedHunkIdx at last cache update
	dirtyHunks        map[int]bool // hunk indices needing re-render in cache

	// AI inline comment state
	aiInlineComments      []claude.InlineReviewComment
	aiCommentsByFileLine  map[string][]claude.InlineReviewComment // "path:line" → comments

	// GitHub inline comment state
	ghCommentThreads map[string][]ghCommentThread // "path:line" → threaded comments

	// Pending inline comment state (user + AI drafts)
	pendingCommentsByFileLine map[string][]PendingInlineComment // "path:line" → comments

	// Comment input mode
	commentMode       bool
	commentInput      textinput.Model
	commentTargetFile string
	commentTargetLine int

	// Search state
	searchMode          bool
	searchInput         textinput.Model
	searchTerm          string
	searchMatches       []searchMatch
	searchMatchIdx      int
	searchMatchesByHunk map[int]map[int][]matchPos // hunkIdx → lineInHunk → match positions

	// PR info data (for PR Info tab)
	prTitle   string
	prBody    string
	prAuthor  string
	prURL     string
	prInfoErr string

	// CI status data
	ciStatus *github.CIStatus
	ciError  string

	// Review status data
	reviewSummary *github.ReviewSummary
	reviewError   string
}

func NewDiffViewerModel() DiffViewerModel {
	si := textinput.New()
	si.Prompt = ""
	si.CharLimit = 100

	ci := textinput.New()
	ci.Prompt = ""
	ci.CharLimit = 500

	return DiffViewerModel{
		spinner:      newLoadingSpinner(),
		searchInput:  si,
		commentInput: ci,
	}
}

func (m DiffViewerModel) Update(msg tea.Msg) (DiffViewerModel, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		if m.loading {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
		return m, nil
	case tea.KeyMsg:
		if !m.focused {
			return m, nil
		}

		// Comment mode: capture all keys for the comment input
		if m.commentMode {
			switch msg.String() {
			case "esc":
				m.commentMode = false
				m.commentInput.SetValue("")
				m.commentInput.Blur()
				m.refreshContent()
				return m, nil
			case "enter":
				body := strings.TrimSpace(m.commentInput.Value())
				path := m.commentTargetFile
				line := m.commentTargetLine
				m.commentMode = false
				m.commentInput.Blur()
				m.refreshContent()
				return m, func() tea.Msg {
					return InlineCommentAddMsg{Path: path, Line: line, Body: body}
				}
			default:
				var cmd tea.Cmd
				m.commentInput, cmd = m.commentInput.Update(msg)
				return m, cmd
			}
		}

		// Search mode: capture all keys for the search input
		if m.searchMode {
			switch msg.String() {
			case "esc":
				m.searchMode = false
				m.searchInput.Blur()
				if m.searchInput.Value() == "" {
					m.clearSearch()
				}
				m.cachedLines = nil
				m.refreshContent()
				return m, nil
			case "enter":
				m.searchMode = false
				m.searchInput.Blur()
				m.refreshContent()
				return m, nil
			default:
				var cmd tea.Cmd
				m.searchInput, cmd = m.searchInput.Update(msg)
				newTerm := m.searchInput.Value()
				if newTerm != m.searchTerm {
					m.searchTerm = newTerm
					m.computeSearchMatches()
					m.cachedLines = nil
					m.refreshContent()
				}
				return m, cmd
			}
		}

		// Active search (not typing): n/N navigate matches, Esc clears
		if m.activeTab == TabDiff && m.searchTerm != "" {
			switch {
			case key.Matches(msg, DiffViewerKeys.NextHunk):
				if len(m.searchMatches) > 0 {
					m.searchMatchIdx = (m.searchMatchIdx + 1) % len(m.searchMatches)
					m.scrollToCurrentMatch()
					m.cachedLines = nil
					m.refreshContent()
				}
				return m, nil
			case key.Matches(msg, DiffViewerKeys.PrevHunk):
				if len(m.searchMatches) > 0 {
					m.searchMatchIdx = (m.searchMatchIdx - 1 + len(m.searchMatches)) % len(m.searchMatches)
					m.scrollToCurrentMatch()
					m.cachedLines = nil
					m.refreshContent()
				}
				return m, nil
			}
			if msg.String() == "esc" {
				m.clearSearch()
				m.cachedLines = nil
				m.refreshContent()
				return m, nil
			}
		}

		// "/" enters search mode on diff tab
		if m.activeTab == TabDiff && key.Matches(msg, DiffViewerKeys.Search) {
			m.searchMode = true
			m.searchInput.SetValue(m.searchTerm)
			m.searchInput.CursorEnd()
			cmd := m.searchInput.Focus()
			m.refreshContent()
			return m, cmd
		}

		switch {
		case key.Matches(msg, DiffViewerKeys.PrevTab):
			if m.activeTab > TabDiff {
				m.activeTab--
				m.refreshContent()
			}
			return m, nil
		case key.Matches(msg, DiffViewerKeys.NextTab):
			if m.activeTab < TabCI {
				m.activeTab++
				m.refreshContent()
			}
			return m, nil
		case key.Matches(msg, DiffViewerKeys.NextHunk):
			if m.activeTab == TabDiff && len(m.hunks) > 0 {
				if m.focusedHunkIdx < len(m.hunks)-1 {
					m.focusedHunkIdx++
				}
				m.scrollToFocusedHunk()
				m.refreshContent()
			}
			return m, nil
		case key.Matches(msg, DiffViewerKeys.PrevHunk):
			if m.activeTab == TabDiff && len(m.hunks) > 0 {
				if m.focusedHunkIdx > 0 {
					m.focusedHunkIdx--
				}
				m.scrollToFocusedHunk()
				m.refreshContent()
			}
			return m, nil
		case key.Matches(msg, DiffViewerKeys.HalfDown):
			m.viewport.HalfViewDown()
			m.syncFocusToScroll()
			m.refreshContent()
			return m, nil
		case key.Matches(msg, DiffViewerKeys.HalfUp):
			m.viewport.HalfViewUp()
			m.syncFocusToScroll()
			m.refreshContent()
			return m, nil
		case key.Matches(msg, DiffViewerKeys.Top):
			m.viewport.GotoTop()
			m.syncFocusToScroll()
			m.refreshContent()
			return m, nil
		case key.Matches(msg, DiffViewerKeys.Bottom):
			m.viewport.GotoBottom()
			m.syncFocusToScroll()
			m.refreshContent()
			return m, nil
		case key.Matches(msg, DiffViewerKeys.Down):
			if m.activeTab == TabDiff && m.viewport.AtBottom() && len(m.hunks) > 0 && m.focusedHunkIdx < len(m.hunks)-1 {
				// At bottom of scroll: advance to next hunk
				m.focusedHunkIdx++
				m.scrollToFocusedHunk()
				m.refreshContent()
				return m, nil
			}
			var cmd tea.Cmd
			oldFocus := m.focusedHunkIdx
			m.viewport, cmd = m.viewport.Update(msg)
			if m.activeTab == TabDiff {
				m.syncFocusToScroll()
				// When scrolling down, don't allow focus to jump backward
				if m.focusedHunkIdx < oldFocus {
					m.focusedHunkIdx = oldFocus
				}
			}
			m.refreshContent()
			return m, cmd
		case key.Matches(msg, DiffViewerKeys.Up):
			var cmd tea.Cmd
			oldFocus := m.focusedHunkIdx
			m.viewport, cmd = m.viewport.Update(msg)
			if m.activeTab == TabDiff {
				m.syncFocusToScroll()
				// When scrolling up, don't allow focus to jump forward
				if m.focusedHunkIdx > oldFocus {
					m.focusedHunkIdx = oldFocus
				}
				// If natural scroll didn't shift focus backward but previous hunk
				// header is now visible, force focus shift without viewport jump
				if m.focusedHunkIdx == oldFocus && oldFocus > 0 {
					prevOffset := m.hunkOffsets[oldFocus-1]
					if prevOffset >= m.viewport.YOffset {
						m.focusedHunkIdx = oldFocus - 1
					}
				}
			}
			m.refreshContent()
			return m, cmd
		case key.Matches(msg, DiffViewerKeys.SelectHunkAndAdvance):
			if m.activeTab == TabDiff && len(m.hunks) > 0 {
				idx := m.focusedHunkIdx
				if idx >= 0 && idx < len(m.hunks) {
					if m.selectedHunks == nil {
						m.selectedHunks = make(map[int]bool)
					}
					if m.selectedHunks[idx] {
						delete(m.selectedHunks, idx)
					} else {
						m.selectedHunks[idx] = true
					}
					m.markHunkDirty(idx)
					m.refreshContent()
				}
				return m, func() tea.Msg { return HunkSelectedAndAdvanceMsg{} }
			}
		case key.Matches(msg, DiffViewerKeys.SelectHunk):
			if m.activeTab == TabDiff && len(m.hunks) > 0 {
				idx := m.focusedHunkIdx
				if idx >= 0 && idx < len(m.hunks) {
					if m.selectedHunks == nil {
						m.selectedHunks = make(map[int]bool)
					}
					if m.selectedHunks[idx] {
						delete(m.selectedHunks, idx)
					} else {
						m.selectedHunks[idx] = true
					}
					m.markHunkDirty(idx)
					m.refreshContent()
				}
				return m, nil
			}
			// Non-diff tabs: fall through to viewport (Space → page down)
		case key.Matches(msg, DiffViewerKeys.SelectFileHunks):
			if m.activeTab == TabDiff && len(m.hunks) > 0 {
				idx := m.focusedHunkIdx
				if idx >= 0 && idx < len(m.hunks) {
					if m.selectedHunks == nil {
						m.selectedHunks = make(map[int]bool)
					}
					fileIdx := m.hunks[idx].FileIndex
					allSelected := true
					for j, h := range m.hunks {
						if h.FileIndex == fileIdx && !m.selectedHunks[j] {
							allSelected = false
							break
						}
					}
					for j, h := range m.hunks {
						if h.FileIndex == fileIdx {
							if allSelected {
								delete(m.selectedHunks, j)
							} else {
								m.selectedHunks[j] = true
							}
							m.markHunkDirty(j)
						}
					}
					m.refreshContent()
				}
			}
			return m, nil
		case key.Matches(msg, DiffViewerKeys.ClearSelection):
			if m.activeTab == TabDiff && len(m.selectedHunks) > 0 {
				for idx := range m.selectedHunks {
					m.markHunkDirty(idx)
				}
				m.selectedHunks = nil
				m.refreshContent()
			}
			return m, nil
		}
	}

	var cmd tea.Cmd
	oldFocus := m.focusedHunkIdx
	m.viewport, cmd = m.viewport.Update(msg)
	m.syncFocusToScroll()
	if m.focusedHunkIdx != oldFocus {
		m.refreshContent()
	}
	return m, cmd
}

func (m *DiffViewerModel) SetSize(width, height int) {
	m.width = width
	m.height = height
	// Account for borders (2) and header (1)
	innerWidth := width - 4
	innerHeight := height - 5
	if innerWidth < 1 {
		innerWidth = 1
	}
	if innerHeight < 1 {
		innerHeight = 1
	}

	if !m.ready {
		m.viewport = viewport.New(innerWidth, innerHeight)
		m.ready = true
	} else {
		m.viewport.Width = innerWidth
		m.viewport.Height = innerHeight
	}
	m.cachedLines = nil // width change invalidates styled cache
	m.refreshContent()
}

func (m *DiffViewerModel) SetFocused(focused bool) {
	m.focused = focused
}

// SetLoading puts the viewer into loading state for a given PR.
func (m *DiffViewerModel) SetLoading(prNumber int) {
	m.prNumber = prNumber
	m.loading = true
	m.files = nil
	m.fileOffsets = nil
	m.hunks = nil
	m.hunkOffsets = nil
	m.focusedHunkIdx = 0
	m.selectedHunks = nil
	m.cachedLines = nil
	m.hunkLineRanges = nil
	m.lastRenderedFocus = 0
	m.dirtyHunks = nil
	m.clearSearch()
	m.commentMode = false
	m.commentInput.SetValue("")
	m.commentInput.Blur()
	m.aiInlineComments = nil
	m.aiCommentsByFileLine = nil
	m.ghCommentThreads = nil
	m.pendingCommentsByFileLine = nil
	m.currentFileIdx = 0
	m.err = nil
	m.prTitle = ""
	m.prBody = ""
	m.prAuthor = ""
	m.prURL = ""
	m.prInfoErr = ""
	m.ciStatus = nil
	m.ciError = ""
	m.reviewSummary = nil
	m.reviewError = ""
	m.refreshContent()
}

// SetDiff displays the fetched diff files.
func (m *DiffViewerModel) SetDiff(files []github.PRFile) {
	m.loading = false
	m.files = files
	m.err = nil
	m.currentFileIdx = 0
	m.focusedHunkIdx = 0
	m.selectedHunks = nil
	m.clearSearch()
	m.parseAllHunks()
	m.cachedLines = nil
	m.refreshContent()
	m.viewport.GotoTop()
}

// SetError displays an error message.
func (m *DiffViewerModel) SetError(err error) {
	m.loading = false
	m.err = err
	m.files = nil
	m.fileOffsets = nil
	m.cachedLines = nil
	m.refreshContent()
}

// SetPRInfo sets PR metadata for the PR Info tab.
func (m *DiffViewerModel) SetPRInfo(title, body, author, url string) {
	m.prTitle = title
	m.prBody = body
	m.prAuthor = author
	m.prURL = url
	m.prInfoErr = ""
	m.refreshContent()
}

// SetPRInfoError sets an error message for the PR Info tab.
func (m *DiffViewerModel) SetPRInfoError(err string) {
	m.prInfoErr = err
	m.refreshContent()
}

// SetCIStatus sets CI check status data for the CI tab.
func (m *DiffViewerModel) SetCIStatus(status *github.CIStatus) {
	m.ciStatus = status
	m.refreshContent()
}

// SetReviewSummary sets review status data for the PR Info tab.
func (m *DiffViewerModel) SetReviewSummary(summary *github.ReviewSummary) {
	m.reviewSummary = summary
	m.refreshContent()
}

// SetCIError sets an error message for CI status loading.
func (m *DiffViewerModel) SetCIError(err string) {
	m.ciError = err
	m.refreshContent()
}

// SetReviewError sets an error message for review status loading.
func (m *DiffViewerModel) SetReviewError(err string) {
	m.reviewError = err
	m.refreshContent()
}

// SetAIInlineComments stores AI-generated inline comments and rebuilds the diff cache.
func (m *DiffViewerModel) SetAIInlineComments(comments []claude.InlineReviewComment) {
	m.aiInlineComments = comments
	m.aiCommentsByFileLine = make(map[string][]claude.InlineReviewComment)
	for _, c := range comments {
		key := fmt.Sprintf("%s:%d", c.Path, c.Line)
		m.aiCommentsByFileLine[key] = append(m.aiCommentsByFileLine[key], c)
	}
	// Full cache invalidation since comment lines change hunk sizes
	m.cachedLines = nil
	m.refreshContent()
}

// ClearAIInlineComments removes all AI inline comments.
func (m *DiffViewerModel) ClearAIInlineComments() {
	m.aiInlineComments = nil
	m.aiCommentsByFileLine = nil
	m.cachedLines = nil
	m.refreshContent()
}

// EnterCommentMode activates comment input mode targeting the focused hunk.
func (m *DiffViewerModel) EnterCommentMode() tea.Cmd {
	if len(m.hunks) == 0 || m.activeTab != TabDiff {
		return nil
	}
	hunk := m.hunks[m.focusedHunkIdx]
	m.commentTargetFile = hunk.Filename
	m.commentTargetLine = parseHunkNewStart(hunk.Header)
	m.commentMode = true

	// Pre-fill if editing existing comment at this location
	key := fmt.Sprintf("%s:%d", m.commentTargetFile, m.commentTargetLine)
	if comments, ok := m.pendingCommentsByFileLine[key]; ok && len(comments) > 0 {
		m.commentInput.SetValue(comments[0].Body)
		m.commentInput.CursorEnd()
	} else {
		m.commentInput.SetValue("")
	}

	m.refreshContent()
	return m.commentInput.Focus()
}

// IsCommenting returns true when the comment input is actively being typed into.
func (m DiffViewerModel) IsCommenting() bool {
	return m.commentMode
}

// SetPendingInlineComments stores pending comments and rebuilds the diff cache.
func (m *DiffViewerModel) SetPendingInlineComments(comments []PendingInlineComment) {
	m.pendingCommentsByFileLine = make(map[string][]PendingInlineComment)
	for _, c := range comments {
		key := fmt.Sprintf("%s:%d", c.Path, c.Line)
		m.pendingCommentsByFileLine[key] = append(m.pendingCommentsByFileLine[key], c)
	}
	m.cachedLines = nil
	m.refreshContent()
}

// renderCommentBar renders the comment input bar shown during comment mode.
func (m DiffViewerModel) renderCommentBar() string {
	target := fmt.Sprintf("%s:%d", m.commentTargetFile, m.commentTargetLine)
	prompt := pendingCommentPrefixStyle.Render("📝 " + target + " > ")
	return prompt + m.commentInput.View()
}

// SetGitHubInlineComments stores GitHub review comments, groups them into threads,
// and rebuilds the diff cache so they render at their line positions.
func (m *DiffViewerModel) SetGitHubInlineComments(comments []github.InlineComment) {
	if len(comments) == 0 {
		m.ghCommentThreads = nil
		m.cachedLines = nil
		m.refreshContent()
		return
	}

	// Separate root comments from replies; index roots by ID for thread building.
	rootByID := make(map[int64]*ghCommentThread)
	var rootOrder []int64 // preserve insertion order
	var replies []github.InlineComment

	for _, c := range comments {
		if c.Outdated {
			continue // outdated comments stay in Comments tab only
		}
		if c.InReplyToID != 0 {
			replies = append(replies, c)
		} else {
			t := ghCommentThread{Root: c}
			rootByID[c.ID] = &t
			rootOrder = append(rootOrder, c.ID)
		}
	}

	// Attach replies to their root threads, sorted chronologically.
	sort.Slice(replies, func(i, j int) bool {
		return replies[i].CreatedAt.Before(replies[j].CreatedAt)
	})
	for _, r := range replies {
		if t, ok := rootByID[r.InReplyToID]; ok {
			t.Replies = append(t.Replies, r)
		}
		// Orphan replies (root not found) are silently dropped — they
		// still appear in the Comments tab flat list.
	}

	// Build the "path:line" → threads map.
	m.ghCommentThreads = make(map[string][]ghCommentThread)
	for _, id := range rootOrder {
		t := rootByID[id]
		key := fmt.Sprintf("%s:%d", t.Root.Path, t.Root.Line)
		m.ghCommentThreads[key] = append(m.ghCommentThreads[key], *t)
	}

	m.cachedLines = nil
	m.refreshContent()
}

func (m *DiffViewerModel) refreshContent() {
	if !m.ready {
		return
	}

	// Adjust viewport height for search bar / comment bar
	innerHeight := m.height - 5
	if m.searchBarVisible() {
		innerHeight--
	}
	if m.commentMode {
		innerHeight--
	}
	if innerHeight < 1 {
		innerHeight = 1
	}
	m.viewport.Height = innerHeight

	// PR Info tab has its own content path
	if m.activeTab == TabPRInfo {
		m.viewport.SetContent(m.renderPRInfo())
		return
	}

	// CI tab has its own content path
	if m.activeTab == TabCI {
		m.viewport.SetContent(m.renderCITab())
		return
	}

	// Diff tab
	if m.loading {
		m.viewport.SetContent(
			lipgloss.NewStyle().
				Foreground(lipgloss.Color("244")).
				Padding(1, 2).
				Render(m.spinner.View() + fmt.Sprintf(" Loading diff for PR #%d...", m.prNumber)),
		)
		return
	}
	if m.err != nil {
		m.viewport.SetContent(renderErrorWithHint(
			formatUserError(fmt.Sprintf("%v", m.err)),
			"Press r to refresh",
		))
		return
	}
	if m.files != nil {
		if m.cachedLines == nil {
			// Full rebuild needed (new diff, resize, etc.)
			m.buildCachedLines()
		} else {
			// Incremental update: only re-render hunks whose visual state changed
			if m.focusedHunkIdx != m.lastRenderedFocus {
				m.markHunkDirty(m.lastRenderedFocus)
				m.markHunkDirty(m.focusedHunkIdx)
				m.lastRenderedFocus = m.focusedHunkIdx
			}
			for idx := range m.dirtyHunks {
				m.rerenderHunkInCache(idx)
			}
			m.dirtyHunks = nil
			// If a rerender invalidated the cache (e.g. inline comments changed
			// line counts), do the full rebuild now.
			if m.cachedLines == nil {
				m.buildCachedLines()
			}
		}
		m.viewport.SetContent(strings.Join(m.cachedLines, "\n"))
		return
	}
	// No PR selected yet
	m.viewport.SetContent(renderEmptyState("Select a PR to view its diff", "Use j/k to navigate, Enter to select"))
}

func (m DiffViewerModel) View() string {
	header := m.renderTabs()

	var content string
	if m.ready {
		content = m.viewport.View()
	} else {
		content = "Loading..."
	}

	parts := []string{header, content}
	if indicator := scrollIndicator(m.viewport, m.width-4); indicator != "" {
		parts = append(parts, indicator)
	}

	if m.searchMode {
		parts = append(parts, m.renderSearchBar())
	} else if m.searchTerm != "" {
		parts = append(parts, m.renderSearchInfo())
	}

	if m.commentMode {
		parts = append(parts, m.renderCommentBar())
	}

	inner := lipgloss.JoinVertical(lipgloss.Left, parts...)
	style := panelStyle(m.focused, false, m.width-2, m.height-2)
	return style.Render(inner)
}

func (m DiffViewerModel) renderTabs() string {
	var tabs []string

	diffLabel := "Diff"
	if m.prNumber > 0 && m.files != nil {
		diffLabel = fmt.Sprintf("Diff (%d files)", len(m.files))
	}
	if len(m.selectedHunks) > 0 {
		diffLabel += fmt.Sprintf(" [%d/%d hunks]", len(m.selectedHunks), len(m.hunks))
	}
	prInfoLabel := "PR Info"
	ciLabel := m.ciTabLabel()

	tabNames := []struct {
		tab   DiffViewerTab
		label string
	}{
		{TabDiff, diffLabel},
		{TabPRInfo, prInfoLabel},
		{TabCI, ciLabel},
	}

	for _, t := range tabNames {
		if m.activeTab == t.tab {
			tabs = append(tabs, activeTabStyle().Render(t.label))
		} else {
			tabs = append(tabs, inactiveTabStyle().Render(t.label))
		}
	}

	return strings.Join(tabs, " ")
}

func (m DiffViewerModel) renderPRInfo() string {
	if m.prNumber == 0 {
		return renderEmptyState("Select a PR to view its details", "Use j/k to navigate, Enter to select")
	}

	if m.prInfoErr != "" {
		return renderErrorWithHint(
			formatUserError(m.prInfoErr),
			"Press r to refresh",
		)
	}

	if m.prTitle == "" {
		return lipgloss.NewStyle().
			Foreground(lipgloss.Color("244")).
			Padding(1, 2).
			Render(m.spinner.View() + fmt.Sprintf(" Loading PR #%d info...", m.prNumber))
	}

	innerWidth := m.viewport.Width
	if innerWidth < 10 {
		innerWidth = 10
	}

	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("33"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("244"))

	var b strings.Builder

	// Title
	b.WriteString(sectionStyle.Render(fmt.Sprintf("PR #%d", m.prNumber)))
	b.WriteString("\n")
	b.WriteString(lipgloss.NewStyle().Bold(true).Render(m.prTitle))
	b.WriteString("\n\n")

	// Author
	b.WriteString(dimStyle.Render("Author: "))
	b.WriteString(m.prAuthor)
	b.WriteString("\n")

	// URL
	if m.prURL != "" {
		b.WriteString(dimStyle.Render("URL: "))
		b.WriteString(m.prURL)
		b.WriteString("\n")
	}

	// Reviews
	if m.reviewError != "" {
		b.WriteString("\n")
		b.WriteString(sectionStyle.Render("Reviews"))
		b.WriteString("\n")
		errStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
		b.WriteString(errStyle.Render(formatUserError(m.reviewError)))
		b.WriteString("\n")
	} else if m.reviewSummary != nil {
		b.WriteString("\n")
		b.WriteString(sectionStyle.Render("Reviews"))
		b.WriteString("\n")

		// Overall decision badge
		if m.reviewSummary.ReviewDecision != "" {
			icon, color := reviewDecisionIconColor(m.reviewSummary.ReviewDecision)
			badge := lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render(icon)
			label := reviewDecisionLabel(m.reviewSummary.ReviewDecision)
			b.WriteString(fmt.Sprintf("%s %s\n", badge, label))
		}

		// Per-reviewer status
		for _, r := range m.reviewSummary.Approved {
			approvedIcon := lipgloss.NewStyle().Foreground(lipgloss.Color("76")).Render("✓")
			b.WriteString(fmt.Sprintf("  %s %s approved\n", approvedIcon, r.Author.Login))
		}
		for _, r := range m.reviewSummary.ChangesRequested {
			changesIcon := lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render("✗")
			b.WriteString(fmt.Sprintf("  %s %s requested changes\n", changesIcon, r.Author.Login))
		}

		// Pending reviewers
		for _, rr := range m.reviewSummary.PendingReviewers {
			pendingIcon := lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Render("○")
			name := rr.Login
			if rr.IsTeam {
				name += " (team)"
			}
			b.WriteString(fmt.Sprintf("  %s %s pending\n", pendingIcon, name))
		}

		if len(m.reviewSummary.Approved) == 0 && len(m.reviewSummary.ChangesRequested) == 0 &&
			len(m.reviewSummary.PendingReviewers) == 0 && m.reviewSummary.ReviewDecision == "" {
			b.WriteString(dimStyle.Render("No reviews yet"))
			b.WriteString("\n")
		}
	}

	// Description
	if m.prBody != "" {
		b.WriteString("\n")
		b.WriteString(sectionStyle.Render("Description"))
		b.WriteString("\n")
		b.WriteString(wordWrap(m.prBody, innerWidth))
	} else {
		b.WriteString("\n")
		b.WriteString(dimStyle.Render("No description provided."))
	}

	return b.String()
}

// ciTabLabel returns a dynamic label for the CI tab header showing at-a-glance status.
func (m DiffViewerModel) ciTabLabel() string {
	if m.ciStatus == nil || m.prNumber == 0 {
		return "CI"
	}
	if m.ciStatus.TotalCount == 0 {
		return "CI"
	}
	icon, _ := ciStatusIconColor(m.ciStatus.OverallStatus)
	passCount := ciPassingCount(m.ciStatus.Checks)
	switch m.ciStatus.OverallStatus {
	case "passing":
		return fmt.Sprintf("CI (%s %d/%d)", icon, passCount, m.ciStatus.TotalCount)
	case "failing":
		failCount := m.ciStatus.TotalCount - passCount
		return fmt.Sprintf("CI (%s %d/%d)", icon, failCount, m.ciStatus.TotalCount)
	case "pending":
		completedCount := 0
		for _, c := range m.ciStatus.Checks {
			if c.Status == "completed" {
				completedCount++
			}
		}
		return fmt.Sprintf("CI (%s %d/%d)", icon, completedCount, m.ciStatus.TotalCount)
	case "mixed":
		return fmt.Sprintf("CI (%s %d/%d)", icon, passCount, m.ciStatus.TotalCount)
	default:
		return "CI"
	}
}

// renderCITab renders the full CI status view for the dedicated CI tab.
func (m DiffViewerModel) renderCITab() string {
	if m.prNumber == 0 {
		return renderEmptyState("Select a PR to view CI status", "Use j/k to navigate, Enter to select")
	}

	if m.ciError != "" {
		return renderErrorWithHint(formatUserError(m.ciError), "Press r to refresh")
	}

	if m.ciStatus == nil {
		return lipgloss.NewStyle().
			Foreground(lipgloss.Color("244")).
			Padding(1, 2).
			Render(m.spinner.View() + fmt.Sprintf(" Loading CI status for PR #%d...", m.prNumber))
	}

	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("33"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("244"))

	var b strings.Builder

	b.WriteString(sectionStyle.Render(fmt.Sprintf("CI Status — PR #%d", m.prNumber)))
	b.WriteString("\n\n")

	if m.ciStatus.TotalCount == 0 {
		b.WriteString(dimStyle.Render("No CI checks configured"))
		b.WriteString("\n")
		return b.String()
	}

	// Summary badge
	icon, color := ciStatusIconColor(m.ciStatus.OverallStatus)
	badge := lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render(icon)
	passCount := ciPassingCount(m.ciStatus.Checks)
	label := ciStatusLabel(m.ciStatus.OverallStatus)
	b.WriteString(fmt.Sprintf("%s %s — %d/%d checks passing\n\n", badge, label, passCount, m.ciStatus.TotalCount))

	// Sort checks: failures first, then pending, then passing/skipped
	type checkGroup struct {
		title  string
		checks []github.CICheck
	}
	var failing, pending, passing []github.CICheck
	for _, check := range m.ciStatus.Checks {
		switch {
		case check.Status == "completed" && check.Conclusion == "failure":
			failing = append(failing, check)
		case check.Status == "queued" || check.Status == "in_progress":
			pending = append(pending, check)
		default:
			passing = append(passing, check)
		}
	}

	groups := []checkGroup{
		{"Failing", failing},
		{"In Progress", pending},
		{"Passing", passing},
	}

	for _, group := range groups {
		if len(group.checks) == 0 {
			continue
		}
		b.WriteString(dimStyle.Render(fmt.Sprintf("── %s (%d) ", group.title, len(group.checks))))
		b.WriteString("\n")
		for _, check := range group.checks {
			ci, cc := ciCheckIconColor(check)
			checkIcon := lipgloss.NewStyle().Foreground(lipgloss.Color(cc)).Render(ci)
			conclusion := ""
			if check.Status == "completed" && check.Conclusion != "" {
				conclusion = dimStyle.Render(fmt.Sprintf(" (%s)", check.Conclusion))
			} else if check.Status != "completed" {
				conclusion = dimStyle.Render(fmt.Sprintf(" (%s)", check.Status))
			}
			b.WriteString(fmt.Sprintf("  %s %s%s\n", checkIcon, check.Name, conclusion))
		}
		b.WriteString("\n")
	}

	return b.String()
}

// buildCachedLines renders all diff content into the cachedLines slice.
// It computes fileOffsets, hunkOffsets, and hunkLineRanges from pre-parsed hunks.
func (m *DiffViewerModel) buildCachedLines() {
	if len(m.files) == 0 {
		m.cachedLines = []string{renderEmptyState("No files changed in this PR", "")}
		m.hunkLineRanges = nil
		return
	}

	innerWidth := m.viewport.Width
	lines := make([]string, 0, 256)
	m.fileOffsets = make([]int, len(m.files))
	m.hunkOffsets = make([]int, len(m.hunks))
	m.hunkLineRanges = make([][2]int, len(m.hunks))
	globalHunkIdx := 0

	for i, f := range m.files {
		if i > 0 {
			lines = append(lines, "")
		}

		// Track line offset where this file header starts
		m.fileOffsets[i] = len(lines)

		// File header
		lines = append(lines, diffFileHeaderStyle.Render(fileStatusLabel(f)))

		// Separator
		lines = append(lines, strings.Repeat("─", min(innerWidth, 60)))

		// Patch content
		if f.Patch == "" {
			lines = append(lines, lipgloss.NewStyle().
				Foreground(lipgloss.Color("244")).
				Italic(true).
				Render("  (diff not available)"))
			continue
		}

		lines = append(lines, "") // blank before hunks

		// Render pre-parsed hunks
		for globalHunkIdx < len(m.hunks) && m.hunks[globalHunkIdx].FileIndex == i {
			m.hunkOffsets[globalHunkIdx] = len(lines)
			start := len(lines)
			hunkLines := m.renderHunkLines(globalHunkIdx)
			lines = append(lines, hunkLines...)
			m.hunkLineRanges[globalHunkIdx] = [2]int{start, len(lines)}
			globalHunkIdx++
		}
	}

	m.cachedLines = lines
	m.lastRenderedFocus = m.focusedHunkIdx
	m.dirtyHunks = nil
}

// parseHunkNewStart parses the new-side start line number from a @@ header.
// For "@@ -7,6 +12,8 @@" it returns 12.
func parseHunkNewStart(header string) int {
	// Find the "+N" part in the @@ header
	idx := strings.Index(header, "+")
	if idx == -1 {
		return 0
	}
	rest := header[idx+1:]
	var n int
	fmt.Sscanf(rest, "%d", &n)
	return n
}

// renderHunkLines renders a single hunk's styled output lines.
func (m *DiffViewerModel) renderHunkLines(hunkIdx int) []string {
	hunk := m.hunks[hunkIdx]
	selected := m.selectedHunks[hunkIdx]
	isFocused := hunkIdx == m.focusedHunkIdx
	hasAIComments := len(m.aiCommentsByFileLine) > 0
	hasGHComments := len(m.ghCommentThreads) > 0
	hasPendingComments := len(m.pendingCommentsByFileLine) > 0
	lines := make([]string, 0, len(hunk.Lines))

	// Track new-side line number for inline comment matching
	newLine := 0

	for lineIdx, line := range hunk.Lines {
		if line == "" {
			if isFocused {
				lines = append(lines, diffFocusGutterStyle.Render("▎"))
			} else {
				lines = append(lines, "")
			}
			continue
		}

		// Update line number tracking
		switch {
		case strings.HasPrefix(line, "@@"):
			newLine = parseHunkNewStart(line)
		case strings.HasPrefix(line, "+"):
			// newLine is consumed after rendering
		case strings.HasPrefix(line, "-"):
			// Removed lines don't advance new-side counter
		case strings.HasPrefix(line, `\`):
			// "\ No newline" — no counter change
		default:
			// Context line — advances new-side counter
		}

		// Gutter marker: ▎ for focused hunk, space for others
		var gutter string
		if isFocused {
			gutter = diffFocusGutterStyle.Render("▎") + " "
		} else {
			gutter = "  "
		}

		var style lipgloss.Style
		displayLine := line
		switch {
		case strings.HasPrefix(line, "@@"):
			if isFocused {
				style = diffFocusedHunkStyle
				if selected {
					displayLine = "✓ " + line
				} else {
					displayLine = "▶ " + line
				}
			} else {
				style = diffHunkHeaderStyle
				if selected {
					displayLine = "✓ " + line
				}
			}
		case strings.HasPrefix(line, "+"):
			style = diffAddedStyle
		case strings.HasPrefix(line, "-"):
			style = diffRemovedStyle
		case strings.HasPrefix(line, `\`):
			style = lipgloss.NewStyle().
				Foreground(lipgloss.Color("244")).
				Italic(true)
		default:
			style = lipgloss.NewStyle()
		}

		if selected {
			style = style.Background(diffSelectedBg)
		}

		// Apply search highlights if matches exist on this line
		if lineMatches := m.getLineSearchMatches(hunkIdx, lineIdx); len(lineMatches) > 0 {
			prefixLen := len(displayLine) - len(line)
			var currentMatchPos *matchPos
			if len(m.searchMatches) > 0 && m.searchMatchIdx < len(m.searchMatches) {
				cm := m.searchMatches[m.searchMatchIdx]
				if cm.hunkIdx == hunkIdx && cm.lineInHunk == lineIdx {
					currentMatchPos = &matchPos{startCol: cm.startCol, endCol: cm.endCol}
				}
			}
			lines = append(lines, gutter+renderLineWithHighlights(displayLine, lineMatches, prefixLen, style, currentMatchPos))
		} else {
			lines = append(lines, gutter+style.Render(displayLine))
		}

		// Inject inline comments after matching lines (+ or context lines)
		if newLine > 0 && !strings.HasPrefix(line, "-") && !strings.HasPrefix(line, `\`) && !strings.HasPrefix(line, "@@") {
			key := fmt.Sprintf("%s:%d", hunk.Filename, newLine)

			// AI inline comments
			if hasAIComments {
				if comments, ok := m.aiCommentsByFileLine[key]; ok {
					for _, c := range comments {
						prefix := aiCommentPrefixStyle.Render("  💬 ")
						body := aiCommentStyle.Render(c.Body)
						lines = append(lines, prefix+body)
					}
				}
			}

			// GitHub inline comments (threaded)
			if hasGHComments {
				if threads, ok := m.ghCommentThreads[key]; ok {
					for _, t := range threads {
						lines = append(lines, m.renderGHCommentThread(t)...)
					}
				}
			}

			// Pending inline comments (user + AI drafts)
			if hasPendingComments {
				if comments, ok := m.pendingCommentsByFileLine[key]; ok {
					for _, c := range comments {
						prefix := pendingCommentPrefixStyle.Render("  📝 ")
						body := pendingCommentStyle.Render(c.Body)
						lines = append(lines, prefix+body)
					}
				}
			}
		}

		// Advance new-side line counter for + and context lines
		if !strings.HasPrefix(line, "-") && !strings.HasPrefix(line, `\`) && !strings.HasPrefix(line, "@@") {
			newLine++
		}
	}

	return lines
}

// renderGHCommentThread renders a single GitHub comment thread (root + replies).
func (m *DiffViewerModel) renderGHCommentThread(t ghCommentThread) []string {
	var lines []string

	// Root comment: 💬 @author · Jan 2 15:04
	header := ghCommentAuthorStyle.Render("  💬 @"+t.Root.Author.Login) +
		ghCommentMetaStyle.Render(" · "+t.Root.CreatedAt.Format("Jan 2 15:04"))
	lines = append(lines, header)
	lines = append(lines, ghCommentBodyStyle.Render(t.Root.Body))

	// Replies: ↳ @author · Jan 2 15:04
	for _, r := range t.Replies {
		replyHeader := ghCommentMetaStyle.Render("    ↳ ") +
			ghCommentAuthorStyle.Render("@"+r.Author.Login) +
			ghCommentMetaStyle.Render(" · "+r.CreatedAt.Format("Jan 2 15:04"))
		lines = append(lines, replyHeader)
		lines = append(lines, ghCommentReplyStyle.Render(r.Body))
	}

	return lines
}

// rerenderHunkInCache re-renders a single hunk's styled lines in the cache.
// When inline comments are present, line counts may differ from the source,
// so we fall back to a full cache rebuild instead of in-place replacement.
func (m *DiffViewerModel) rerenderHunkInCache(hunkIdx int) {
	if hunkIdx < 0 || hunkIdx >= len(m.hunkLineRanges) {
		return
	}
	// If any inline comments are active, hunk line counts are unstable — force full rebuild
	if len(m.aiCommentsByFileLine) > 0 || len(m.ghCommentThreads) > 0 || len(m.pendingCommentsByFileLine) > 0 {
		m.cachedLines = nil
		return
	}
	r := m.hunkLineRanges[hunkIdx]
	newLines := m.renderHunkLines(hunkIdx)
	for i, line := range newLines {
		m.cachedLines[r[0]+i] = line
	}
}

// markHunkDirty marks a hunk for re-rendering on the next refreshContent call.
func (m *DiffViewerModel) markHunkDirty(idx int) {
	if idx < 0 || idx >= len(m.hunks) {
		return
	}
	if m.dirtyHunks == nil {
		m.dirtyHunks = make(map[int]bool)
	}
	m.dirtyHunks[idx] = true
}

func fileStatusLabel(f github.PRFile) string {
	switch f.Status {
	case "added":
		return fmt.Sprintf("%s (new file, +%d)", f.Filename, f.Additions)
	case "removed":
		return fmt.Sprintf("%s (deleted, -%d)", f.Filename, f.Deletions)
	case "renamed":
		return fmt.Sprintf("%s (renamed)", f.Filename)
	default:
		return fmt.Sprintf("%s (+%d/-%d)", f.Filename, f.Additions, f.Deletions)
	}
}

// syncFocusToScroll updates focusedHunkIdx to match the current viewport scroll position.
// It picks the last hunk whose header is in the top third of the viewport.
func (m *DiffViewerModel) syncFocusToScroll() {
	if len(m.hunkOffsets) == 0 {
		return
	}
	ref := m.viewport.YOffset + m.viewport.Height/3
	idx := 0
	for i, offset := range m.hunkOffsets {
		if offset <= ref {
			idx = i
		} else {
			break
		}
	}
	m.focusedHunkIdx = idx
}

// scrollToFocusedHunk scrolls the viewport so the focused hunk's header is visible
// near the top of the viewport.
func (m *DiffViewerModel) scrollToFocusedHunk() {
	if m.focusedHunkIdx < 0 || m.focusedHunkIdx >= len(m.hunkOffsets) {
		return
	}
	offset := m.hunkOffsets[m.focusedHunkIdx]
	target := offset - 2 // small margin above the @@ header
	if target < 0 {
		target = 0
	}
	m.viewport.SetYOffset(target)
}

// ciStatusIconColor returns the icon and lipgloss color for an overall CI status.
func ciStatusIconColor(status string) (string, string) {
	switch status {
	case "passing":
		return "✓", "42"
	case "failing":
		return "✗", "196"
	case "pending":
		return "●", "226"
	case "mixed":
		return "⚠", "208"
	default:
		return "?", "244"
	}
}

// ciStatusLabel returns a display label for the overall CI status.
func ciStatusLabel(status string) string {
	switch status {
	case "passing":
		return "Passing"
	case "failing":
		return "Failing"
	case "pending":
		return "Pending"
	case "mixed":
		return "Mixed"
	default:
		return status
	}
}

// ciCheckIconColor returns the icon and color for an individual CI check.
func ciCheckIconColor(check github.CICheck) (string, string) {
	switch {
	case check.Status == "completed" && check.Conclusion == "success":
		return "✓", "42"
	case check.Status == "completed" && (check.Conclusion == "skipped" || check.Conclusion == "neutral"):
		return "−", "244"
	case check.Status == "completed" && check.Conclusion == "failure":
		return "✗", "196"
	case check.Status == "queued" || check.Status == "in_progress":
		return "●", "226"
	default:
		return "?", "244"
	}
}

// reviewDecisionIconColor returns the icon and lipgloss color for a review decision.
func reviewDecisionIconColor(decision string) (string, string) {
	switch decision {
	case "APPROVED":
		return "✓", "76"
	case "CHANGES_REQUESTED":
		return "✗", "196"
	case "REVIEW_REQUIRED":
		return "○", "214"
	default:
		return "?", "244"
	}
}

// reviewDecisionLabel returns a display label for the review decision.
func reviewDecisionLabel(decision string) string {
	switch decision {
	case "APPROVED":
		return "Approved"
	case "CHANGES_REQUESTED":
		return "Changes Requested"
	case "REVIEW_REQUIRED":
		return "Review Required"
	default:
		return decision
	}
}

// ciPassingCount counts checks that completed successfully (including skipped/neutral).
func ciPassingCount(checks []github.CICheck) int {
	count := 0
	for _, c := range checks {
		if c.Status == "completed" && (c.Conclusion == "success" || c.Conclusion == "skipped" || c.Conclusion == "neutral") {
			count++
		}
	}
	return count
}

// GetSelectedHunkContent returns formatted diff content for only the selected hunks.
// Returns empty string if no hunks are selected.
func (m DiffViewerModel) GetSelectedHunkContent() string {
	if len(m.selectedHunks) == 0 {
		return ""
	}

	var b strings.Builder
	lastFileIdx := -1

	for i, hunk := range m.hunks {
		if !m.selectedHunks[i] {
			continue
		}

		if hunk.FileIndex != lastFileIdx {
			if lastFileIdx >= 0 {
				b.WriteString("\n")
			}
			b.WriteString(fmt.Sprintf("--- a/%s\n", hunk.Filename))
			b.WriteString(fmt.Sprintf("+++ b/%s\n", hunk.Filename))
			lastFileIdx = hunk.FileIndex
		}

		for _, line := range hunk.Lines {
			b.WriteString(line)
			b.WriteString("\n")
		}
	}

	return b.String()
}

// -- Search methods --

// IsSearching returns true when the search input is actively being typed into.
func (m DiffViewerModel) IsSearching() bool {
	return m.searchMode
}

// SearchInfo returns a string like "3/17" indicating current match position,
// or "No matches" if the search term has no results, or "" if no search is active.
func (m DiffViewerModel) SearchInfo() string {
	if m.searchTerm == "" {
		return ""
	}
	if len(m.searchMatches) == 0 {
		return "No matches"
	}
	return fmt.Sprintf("%d/%d", m.searchMatchIdx+1, len(m.searchMatches))
}

// HasActiveSearch returns true when a search term is active (matches may be navigated).
func (m DiffViewerModel) HasActiveSearch() bool {
	return m.searchTerm != ""
}

// clearSearch resets all search state.
func (m *DiffViewerModel) clearSearch() {
	m.searchMode = false
	m.searchTerm = ""
	m.searchMatches = nil
	m.searchMatchesByHunk = nil
	m.searchMatchIdx = 0
	m.searchInput.SetValue("")
	m.searchInput.Blur()
}

// searchBarVisible returns true when the search bar or info line should be shown.
func (m DiffViewerModel) searchBarVisible() bool {
	return m.searchMode || m.searchTerm != ""
}

// computeSearchMatches scans all hunks for case-insensitive matches of the search term.
func (m *DiffViewerModel) computeSearchMatches() {
	m.searchMatches = nil
	m.searchMatchesByHunk = nil
	m.searchMatchIdx = 0

	if m.searchTerm == "" {
		return
	}

	lowerTerm := strings.ToLower(m.searchTerm)
	m.searchMatchesByHunk = make(map[int]map[int][]matchPos)

	for hunkIdx, hunk := range m.hunks {
		for lineIdx, line := range hunk.Lines {
			lower := strings.ToLower(line)
			start := 0
			for {
				idx := strings.Index(lower[start:], lowerTerm)
				if idx == -1 {
					break
				}
				absStart := start + idx
				absEnd := absStart + len(lowerTerm)

				m.searchMatches = append(m.searchMatches, searchMatch{
					hunkIdx:    hunkIdx,
					lineInHunk: lineIdx,
					startCol:   absStart,
					endCol:     absEnd,
				})

				if m.searchMatchesByHunk[hunkIdx] == nil {
					m.searchMatchesByHunk[hunkIdx] = make(map[int][]matchPos)
				}
				m.searchMatchesByHunk[hunkIdx][lineIdx] = append(
					m.searchMatchesByHunk[hunkIdx][lineIdx],
					matchPos{startCol: absStart, endCol: absEnd},
				)

				start = absEnd
			}
		}
	}
}

// scrollToCurrentMatch scrolls the viewport so the current search match is visible.
func (m *DiffViewerModel) scrollToCurrentMatch() {
	if len(m.searchMatches) == 0 || m.searchMatchIdx >= len(m.searchMatches) {
		return
	}
	match := m.searchMatches[m.searchMatchIdx]
	m.focusedHunkIdx = match.hunkIdx
	m.scrollToFocusedHunk()
}

// getLineSearchMatches returns match positions for a specific line in a hunk.
func (m *DiffViewerModel) getLineSearchMatches(hunkIdx, lineIdx int) []matchPos {
	if m.searchMatchesByHunk == nil {
		return nil
	}
	if lineMap, ok := m.searchMatchesByHunk[hunkIdx]; ok {
		return lineMap[lineIdx]
	}
	return nil
}

// renderSearchBar renders the search input bar (shown during active search mode).
func (m DiffViewerModel) renderSearchBar() string {
	prompt := diffSearchInfoStyle.Render("/")
	return prompt + m.searchInput.View()
}

// renderSearchInfo renders the search term and match count (shown when search is active but not typing).
func (m DiffViewerModel) renderSearchInfo() string {
	info := m.SearchInfo()
	return diffSearchInfoStyle.Render(fmt.Sprintf(" /%s  %s ", m.searchTerm, info))
}

// renderLineWithHighlights renders a display line with search match highlights applied.
// prefixLen is the number of bytes prepended to the raw line for display (e.g., "✓ ").
// Match positions refer to the raw line; they are offset by prefixLen for the display line.
func renderLineWithHighlights(displayLine string, matches []matchPos, prefixLen int, baseStyle lipgloss.Style, currentMatch *matchPos) string {
	var b strings.Builder
	lastEnd := 0

	for _, mp := range matches {
		start := mp.startCol + prefixLen
		end := mp.endCol + prefixLen

		if start > len(displayLine) {
			continue
		}
		if end > len(displayLine) {
			end = len(displayLine)
		}

		// Render text before this match
		if start > lastEnd {
			b.WriteString(baseStyle.Render(displayLine[lastEnd:start]))
		}

		// Determine highlight color
		highlightBg := diffSearchMatchBg
		if currentMatch != nil && mp.startCol == currentMatch.startCol && mp.endCol == currentMatch.endCol {
			highlightBg = diffSearchCurrentMatchBg
		}

		highlightStyle := baseStyle.Background(highlightBg)
		b.WriteString(highlightStyle.Render(displayLine[start:end]))
		lastEnd = end
	}

	// Render remaining text after last match
	if lastEnd < len(displayLine) {
		b.WriteString(baseStyle.Render(displayLine[lastEnd:]))
	}

	return b.String()
}
