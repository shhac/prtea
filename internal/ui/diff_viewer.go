package ui

import (
	"fmt"
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
	cachedLines       []string     // per-line styled output (nil = needs full rebuild)
	cachedLineInfo    []lineInfo   // parallel to cachedLines
	hunkLineRanges    [][2]int     // [start, end) line indices in cachedLines per hunk
	lastRenderedFocus int          // focusedHunkIdx at last cache update
	dirtyHunks        map[int]bool // hunk indices needing re-render in cache

	// Line-level cursor for precise inline comment targeting.
	cursorLine int

	// Multi-line selection (visual mode) for range comments.
	selectionAnchor int // -1 means no active selection

	// AI inline comment state
	aiInlineComments      []claude.InlineReviewComment
	aiCommentsByFileLine  map[string][]claude.InlineReviewComment // "path:line" → comments

	// GitHub inline comment state
	ghCommentThreads map[string][]ghCommentThread // "path:line" → threaded comments

	// Pending inline comment state (user + AI drafts)
	pendingCommentsByFileLine map[string][]PendingInlineComment // "path:line" → comments

	// Comment input mode
	commentMode           bool
	commentInput          textinput.Model
	commentTargetFile     string
	commentTargetLine     int
	commentTargetStartLine int // non-zero for multi-line range comments

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

	// Shared markdown renderer (cached per width)
	md MarkdownRenderer

	// PR Info tab render cache
	prInfoCache      string
	prInfoCacheWidth int

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
		spinner:         newLoadingSpinner(),
		searchInput:     si,
		commentInput:    ci,
		selectionAnchor: -1,
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
			return m.handleCommentModeKey(msg)
		}

		// Search mode: capture all keys for the search input
		if m.searchMode {
			return m.handleSearchModeKey(msg)
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

		// "x" re-runs failed CI on CI tab
		if m.activeTab == TabCI && key.Matches(msg, DiffViewerKeys.RerunCI) {
			if m.ciStatus != nil && len(m.ciStatus.FailedRunIDs()) > 0 {
				return m, func() tea.Msg { return CIRerunRequestMsg{} }
			}
			return m, nil
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
				m.cancelSelection()
				if m.focusedHunkIdx < len(m.hunks)-1 {
					m.focusedHunkIdx++
				}
				m.scrollToFocusedHunk()
				m.syncCursorToFocusedHunk()
				m.refreshContent()
			}
			return m, nil
		case key.Matches(msg, DiffViewerKeys.PrevHunk):
			if m.activeTab == TabDiff && len(m.hunks) > 0 {
				m.cancelSelection()
				if m.focusedHunkIdx > 0 {
					m.focusedHunkIdx--
				}
				m.scrollToFocusedHunk()
				m.syncCursorToFocusedHunk()
				m.refreshContent()
			}
			return m, nil
		case key.Matches(msg, DiffViewerKeys.HalfDown):
			m.cancelSelection()
			m.viewport.HalfViewDown()
			m.syncFocusToScroll()
			m.syncCursorToScroll()
			m.refreshContent()
			return m, nil
		case key.Matches(msg, DiffViewerKeys.HalfUp):
			m.cancelSelection()
			m.viewport.HalfViewUp()
			m.syncFocusToScroll()
			m.syncCursorToScroll()
			m.refreshContent()
			return m, nil
		case key.Matches(msg, DiffViewerKeys.Top):
			m.cancelSelection()
			m.viewport.GotoTop()
			m.syncFocusToScroll()
			m.syncCursorToScroll()
			m.refreshContent()
			return m, nil
		case key.Matches(msg, DiffViewerKeys.Bottom):
			m.cancelSelection()
			m.viewport.GotoBottom()
			m.syncFocusToScroll()
			m.syncCursorToScroll()
			m.refreshContent()
			return m, nil
		case key.Matches(msg, DiffViewerKeys.SelectDown):
			if m.activeTab == TabDiff && len(m.cachedLineInfo) > 0 {
				m.extendSelection(1)
				m.refreshContent()
				return m, nil
			}
			return m, nil
		case key.Matches(msg, DiffViewerKeys.SelectUp):
			if m.activeTab == TabDiff && len(m.cachedLineInfo) > 0 {
				m.extendSelection(-1)
				m.refreshContent()
				return m, nil
			}
			return m, nil
		case key.Matches(msg, DiffViewerKeys.Down):
			if m.activeTab == TabDiff && len(m.cachedLineInfo) > 0 {
				m.cancelSelection()
				m.moveCursor(1)
				m.refreshContent()
				return m, nil
			}
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			m.refreshContent()
			return m, cmd
		case key.Matches(msg, DiffViewerKeys.Up):
			if m.activeTab == TabDiff && len(m.cachedLineInfo) > 0 {
				m.cancelSelection()
				m.moveCursor(-1)
				m.refreshContent()
				return m, nil
			}
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			m.refreshContent()
			return m, cmd
		case key.Matches(msg, DiffViewerKeys.SelectHunkAndAdvance):
			if m.activeTab == TabDiff && len(m.hunks) > 0 {
				m.toggleHunkSelection(m.focusedHunkIdx)
				return m, func() tea.Msg { return HunkSelectedAndAdvanceMsg{} }
			}
		case key.Matches(msg, DiffViewerKeys.SelectHunk):
			if m.activeTab == TabDiff && len(m.hunks) > 0 {
				m.toggleHunkSelection(m.focusedHunkIdx)
				return m, nil
			}
			// Non-diff tabs: fall through to viewport (Space → page down)
		case key.Matches(msg, DiffViewerKeys.SelectFileHunks):
			if m.activeTab == TabDiff && len(m.hunks) > 0 {
				m.toggleFileHunkSelection(m.focusedHunkIdx)
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

		// "c" opens comment overlay on Diff tab
		if m.activeTab == TabDiff && len(m.hunks) > 0 && msg.String() == "c" {
			overlayMsg := m.buildCommentOverlayMsg()
			if overlayMsg != nil {
				return m, func() tea.Msg { return *overlayMsg }
			}
		}
	}

	var cmd tea.Cmd
	oldFocus := m.focusedHunkIdx
	m.viewport, cmd = m.viewport.Update(msg)
	m.syncFocusToScroll()
	if m.focusedHunkIdx != oldFocus {
		if m.activeTab == TabDiff {
			m.syncCursorToScroll()
		}
		m.refreshContent()
	}
	return m, cmd
}

// toggleHunkSelection toggles selection state for a single hunk.
func (m *DiffViewerModel) toggleHunkSelection(idx int) {
	if idx < 0 || idx >= len(m.hunks) {
		return
	}
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

// toggleFileHunkSelection toggles selection for all hunks in the same file.
func (m *DiffViewerModel) toggleFileHunkSelection(idx int) {
	if idx < 0 || idx >= len(m.hunks) {
		return
	}
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

func (m *DiffViewerModel) SetSize(width, height int) {
	m.width = width
	m.height = height
	innerWidth := width - 5
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
	m.cachedLines = nil
	m.cachedLineInfo = nil
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
	m.cursorLine = 0
	m.selectionAnchor = -1
	m.selectedHunks = nil
	m.cachedLines = nil
	m.cachedLineInfo = nil
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
	m.cursorLine = 0
	m.selectionAnchor = -1
	m.selectedHunks = nil
	m.clearSearch()
	m.parseAllHunks()
	m.cachedLines = nil
	m.cachedLineInfo = nil
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
	m.cachedLineInfo = nil
	m.refreshContent()
}

// renderMarkdown renders markdown text with glamour for terminal display.
func (m *DiffViewerModel) renderMarkdown(markdown string, width int) string {
	return m.md.RenderMarkdown(markdown, width)
}

func (m *DiffViewerModel) refreshContent() {
	if !m.ready {
		return
	}

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

	if m.activeTab == TabPRInfo {
		m.viewport.SetContent(m.renderPRInfo())
		return
	}

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
			m.buildCachedLines()
		} else {
			if m.focusedHunkIdx != m.lastRenderedFocus {
				m.markHunkDirty(m.lastRenderedFocus)
				m.markHunkDirty(m.focusedHunkIdx)
				m.lastRenderedFocus = m.focusedHunkIdx
			}
			for idx := range m.dirtyHunks {
				m.rerenderHunkInCache(idx)
			}
			m.dirtyHunks = nil
			if m.cachedLines == nil {
				m.buildCachedLines()
			}
		}
		m.viewport.SetContent(strings.Join(m.cachedLines, "\n"))
		return
	}
	m.viewport.SetContent(renderEmptyState("Select a PR to view its diff", "Use j/k to navigate, Enter to select"))
}

func (m DiffViewerModel) View() string {
	header := m.renderTabs()

	var content string
	if m.ready {
		content = m.viewport.View()
		if m.viewport.TotalLineCount() > m.viewport.Height {
			content = lipgloss.JoinHorizontal(lipgloss.Top, content, m.renderScrollbar())
		} else {
			content = lipgloss.JoinHorizontal(lipgloss.Top, content, strings.Repeat(" \n", m.viewport.Height-1)+" ")
		}
	} else {
		content = "Loading..."
	}

	innerWidth := m.width - 4
	parts := []string{header, content}
	if indicator := scrollIndicator(m.viewport, innerWidth); indicator != "" {
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

// GetSelectedHunkContent returns formatted diff content for only the selected hunks.
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
