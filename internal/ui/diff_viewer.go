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
	"github.com/charmbracelet/glamour"
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

// commentKind identifies the type of inline comment a cached line represents.
type commentKind byte

const (
	commentNone    commentKind = iota
	commentAI                  // AI-generated inline comment
	commentGitHub              // GitHub review comment
	commentPending             // Pending user/AI draft
)

// lineInfo describes what a cached viewport line represents in the source diff.
type lineInfo struct {
	hunkIdx       int         // which hunk this line belongs to (-1 for file headers etc.)
	filename      string      // file path for this line
	newLineNum    int         // new-side file line number (0 = not a file line)
	isCommentable bool        // true for + and context lines (commentable on RIGHT side)
	isDiffLine    bool        // true for actual diff content lines (cursor can land here)
	comment       commentKind // non-zero for inline comment lines
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
	cachedLineInfo    []lineInfo   // parallel to cachedLines — what each viewport line represents
	hunkLineRanges    [][2]int     // [start, end) line indices in cachedLines per hunk
	lastRenderedFocus int          // focusedHunkIdx at last cache update
	dirtyHunks        map[int]bool // hunk indices needing re-render in cache

	// Line-level cursor for precise inline comment targeting.
	// cursorLine indexes into cachedLines and cachedLineInfo.
	cursorLine int

	// Multi-line selection (visual mode) for range comments.
	// selectionAnchor is the cachedLineInfo index where selection started.
	// -1 means no active selection.
	selectionAnchor int

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

	// Glamour markdown renderer (cached per width)
	glamourRenderer *glamour.TermRenderer
	glamourWidth    int

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
			switch msg.String() {
			case "esc":
				m.commentMode = false
				m.commentInput.SetValue("")
				m.commentInput.Blur()
				m.cancelSelection()
				m.refreshContent()
				return m, nil
			case "enter":
				body := strings.TrimSpace(m.commentInput.Value())
				path := m.commentTargetFile
				line := m.commentTargetLine
				startLine := m.commentTargetStartLine
				m.commentMode = false
				m.commentInput.Blur()
				m.cancelSelection()
				m.refreshContent()
				return m, func() tea.Msg {
					return InlineCommentAddMsg{Path: path, Line: line, Body: body, StartLine: startLine}
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
			// Non-diff tabs: scroll viewport
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
			// Non-diff tabs: scroll viewport
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
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

func (m *DiffViewerModel) SetSize(width, height int) {
	m.width = width
	m.height = height
	// Account for borders (2), padding (2), and scrollbar gutter (1)
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
	m.cachedLines = nil // width change invalidates styled cache
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
	m.cachedLineInfo = nil
	m.refreshContent()
}

// ClearAIInlineComments removes all AI inline comments.
func (m *DiffViewerModel) ClearAIInlineComments() {
	m.aiInlineComments = nil
	m.aiCommentsByFileLine = nil
	m.cachedLines = nil
	m.cachedLineInfo = nil
	m.refreshContent()
}

// EnterCommentMode activates comment input mode targeting the cursor line.
// If the cursor is on a non-commentable line, it snaps to the nearest commentable
// line within the same hunk. Returns nil if no commentable line is found.
// When a multi-line selection is active, the comment targets the full range.
func (m *DiffViewerModel) EnterCommentMode() tea.Cmd {
	if len(m.hunks) == 0 || m.activeTab != TabDiff || len(m.cachedLineInfo) == 0 {
		return nil
	}

	// Find the comment target from cursor position
	targetLine, targetFile := m.commentTargetFromCursor()
	if targetLine == 0 || targetFile == "" {
		return nil
	}

	m.commentTargetFile = targetFile
	m.commentTargetLine = targetLine
	m.commentTargetStartLine = 0

	// If a multi-line selection is active, resolve the range
	if m.selectionAnchor >= 0 {
		startLine, endLine := m.resolveSelectionRange()
		if startLine > 0 && endLine > 0 && startLine != endLine {
			// GitHub API requires start_line < line
			if startLine > endLine {
				startLine, endLine = endLine, startLine
			}
			m.commentTargetStartLine = startLine
			m.commentTargetLine = endLine
		}
	}

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

// resolveSelectionRange finds the commentable new-side line numbers at the
// boundaries of the current multi-line selection. Returns (startLine, endLine)
// where both are new-side file line numbers, or (0, 0) if no valid range found.
func (m *DiffViewerModel) resolveSelectionRange() (int, int) {
	lo, hi := m.selectionRange()
	if lo < 0 {
		return 0, 0
	}

	// Find first commentable line from selection start (forward)
	startLine := 0
	for i := lo; i <= hi; i++ {
		if i < len(m.cachedLineInfo) && m.cachedLineInfo[i].isCommentable && m.cachedLineInfo[i].newLineNum > 0 {
			startLine = m.cachedLineInfo[i].newLineNum
			break
		}
	}

	// Find last commentable line from selection end (backward)
	endLine := 0
	for i := hi; i >= lo; i-- {
		if i < len(m.cachedLineInfo) && m.cachedLineInfo[i].isCommentable && m.cachedLineInfo[i].newLineNum > 0 {
			endLine = m.cachedLineInfo[i].newLineNum
			break
		}
	}

	return startLine, endLine
}

// commentTargetFromCursor returns the file path and line number for the cursor's
// current position. If the cursor is on a non-commentable line, searches nearby
// lines in the same hunk for the nearest commentable one.
func (m *DiffViewerModel) commentTargetFromCursor() (int, string) {
	if m.cursorLine < 0 || m.cursorLine >= len(m.cachedLineInfo) {
		return 0, ""
	}

	info := m.cachedLineInfo[m.cursorLine]
	if info.isCommentable && info.newLineNum > 0 {
		return info.newLineNum, info.filename
	}

	// Cursor is on a non-commentable line (@@, -, \, etc.)
	// Search forward then backward within the same hunk
	hunk := info.hunkIdx
	if hunk < 0 {
		return 0, ""
	}

	// Forward
	for i := m.cursorLine + 1; i < len(m.cachedLineInfo); i++ {
		ci := m.cachedLineInfo[i]
		if ci.hunkIdx != hunk {
			break
		}
		if ci.isCommentable && ci.newLineNum > 0 {
			return ci.newLineNum, ci.filename
		}
	}
	// Backward
	for i := m.cursorLine - 1; i >= 0; i-- {
		ci := m.cachedLineInfo[i]
		if ci.hunkIdx != hunk {
			break
		}
		if ci.isCommentable && ci.newLineNum > 0 {
			return ci.newLineNum, ci.filename
		}
	}

	return 0, ""
}

// buildCommentOverlayMsg gathers context from the current cursor position
// and returns a ShowCommentOverlayMsg, or nil if no commentable line is found.
func (m *DiffViewerModel) buildCommentOverlayMsg() *ShowCommentOverlayMsg {
	if len(m.cachedLineInfo) == 0 {
		return nil
	}
	targetLine, targetFile := m.commentTargetFromCursor()
	if targetLine == 0 || targetFile == "" {
		return nil
	}

	// Extract diff context lines from the hunk
	hunkIdx := m.cachedLineInfo[m.cursorLine].hunkIdx
	if hunkIdx < 0 || hunkIdx >= len(m.hunks) {
		return nil
	}
	hunk := m.hunks[hunkIdx]

	// Find target line index within hunk and extract a window around it
	targetIdx := -1
	newLine := 0
	for i, line := range hunk.Lines {
		if strings.HasPrefix(line, "@@") {
			// Parse start line from @@ header
			var n int
			if _, err := fmt.Sscanf(line, "@@ -%*d,%*d +%d", &n); err == nil {
				newLine = n - 1
			}
			continue
		}
		if !strings.HasPrefix(line, "-") && !strings.HasPrefix(line, `\`) {
			newLine++
		}
		if newLine == targetLine {
			targetIdx = i
			break
		}
	}
	if targetIdx < 0 {
		targetIdx = 0
	}

	ctxStart := max(0, targetIdx-2)
	ctxEnd := min(len(hunk.Lines), targetIdx+3)
	diffLines := hunk.Lines[ctxStart:ctxEnd]

	key := fmt.Sprintf("%s:%d", targetFile, targetLine)
	return &ShowCommentOverlayMsg{
		Path:            targetFile,
		Line:            targetLine,
		DiffLines:       diffLines,
		TargetLineInCtx: targetIdx - ctxStart,
		GHThreads:       m.ghCommentThreads[key],
		AIComments:      m.aiCommentsByFileLine[key],
		PendingComments: m.pendingCommentsByFileLine[key],
	}
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
	m.cachedLineInfo = nil
	m.refreshContent()
}

// renderCommentBar renders the comment input bar shown during comment mode.
func (m DiffViewerModel) renderCommentBar() string {
	var target string
	if m.commentTargetStartLine > 0 {
		target = fmt.Sprintf("%s:%d-%d", m.commentTargetFile, m.commentTargetStartLine, m.commentTargetLine)
	} else {
		target = fmt.Sprintf("%s:%d", m.commentTargetFile, m.commentTargetLine)
	}
	promptStyle := lipgloss.NewStyle().Foreground(commentBoxPendingBorder).Bold(true)
	prompt := promptStyle.Render("📝 " + target + " > ")
	return prompt + m.commentInput.View()
}

// SetGitHubInlineComments stores GitHub review comments, groups them into threads,
// and rebuilds the diff cache so they render at their line positions.
func (m *DiffViewerModel) SetGitHubInlineComments(comments []github.InlineComment) {
	if len(comments) == 0 {
		m.ghCommentThreads = nil
		m.cachedLines = nil
		m.cachedLineInfo = nil
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
	m.cachedLineInfo = nil
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
		// Attach vertical scrollbar column to the right edge of viewport content
		if m.viewport.TotalLineCount() > m.viewport.Height {
			content = lipgloss.JoinHorizontal(lipgloss.Top, content, m.renderScrollbar())
		} else {
			// Reserve the scrollbar column space even when not scrollable
			content = lipgloss.JoinHorizontal(lipgloss.Top, content, strings.Repeat(" \n", m.viewport.Height-1)+" ")
		}
	} else {
		content = "Loading..."
	}

	innerWidth := m.width - 4 // viewport + scrollbar column
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

// renderScrollbar builds a 1-char-wide vertical scrollbar column with comment markers.
// Each row maps proportionally to the total content; the thumb shows the visible portion
// and colored markers indicate where inline comments live.
func (m DiffViewerModel) renderScrollbar() string {
	height := m.viewport.Height
	totalLines := m.viewport.TotalLineCount()
	if totalLines <= 0 || height <= 0 {
		return strings.Repeat(" \n", height-1) + " "
	}

	// Thumb position and size
	thumbSize := max(1, height*height/totalLines)
	thumbStart := m.viewport.YOffset * height / totalLines
	if thumbStart+thumbSize > height {
		thumbStart = height - thumbSize
	}

	// Collect comment marker positions in scrollbar space.
	// Track the highest-priority comment kind per scrollbar row.
	commentMarkers := make([]commentKind, height)
	if m.activeTab == TabDiff && m.cachedLineInfo != nil {
		for i, info := range m.cachedLineInfo {
			if info.comment == commentNone {
				continue
			}
			row := i * height / totalLines
			if row >= height {
				row = height - 1
			}
			// Priority: pending > GitHub > AI (higher commentKind value wins)
			if info.comment > commentMarkers[row] {
				commentMarkers[row] = info.comment
			}
		}
	}

	// Render each scrollbar row
	rows := make([]string, height)
	for i := 0; i < height; i++ {
		inThumb := i >= thumbStart && i < thumbStart+thumbSize
		marker := commentMarkers[i]

		switch {
		case inThumb && marker != commentNone:
			// Thumb with comment: colored thumb character
			rows[i] = scrollbarCommentStyle(marker).Render("┃")
		case inThumb:
			rows[i] = scrollbarThumbStyle.Render("┃")
		case marker != commentNone:
			// Comment marker on track
			rows[i] = scrollbarCommentStyle(marker).Render("●")
		default:
			rows[i] = scrollbarTrackStyle.Render("│")
		}
	}
	return strings.Join(rows, "\n")
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

func (m *DiffViewerModel) renderPRInfo() string {
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
		b.WriteString(m.renderMarkdown(m.prBody, innerWidth))
	} else {
		b.WriteString("\n")
		b.WriteString(dimStyle.Render("No description provided."))
	}

	return b.String()
}

// getOrCreateRenderer returns a cached glamour renderer for the given width,
// creating a new one only when the width changes.
func (m *DiffViewerModel) getOrCreateRenderer(width int) *glamour.TermRenderer {
	if m.glamourRenderer != nil && m.glamourWidth == width {
		return m.glamourRenderer
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return nil
	}
	m.glamourRenderer = r
	m.glamourWidth = width
	return r
}

// renderMarkdown renders markdown text with glamour for terminal display.
// Falls back to plain wordWrap if glamour fails.
func (m *DiffViewerModel) renderMarkdown(markdown string, width int) string {
	if width < 10 {
		width = 10
	}
	r := m.getOrCreateRenderer(width)
	if r == nil {
		return wordWrap(markdown, width)
	}
	out, err := r.Render(markdown)
	if err != nil {
		return wordWrap(markdown, width)
	}
	return strings.TrimSpace(out)
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

	// When all checks share one status, show a flat list without group headers.
	nonEmpty := 0
	for _, g := range groups {
		if len(g.checks) > 0 {
			nonEmpty++
		}
	}

	for _, group := range groups {
		if len(group.checks) == 0 {
			continue
		}
		if nonEmpty > 1 {
			b.WriteString(dimStyle.Render(fmt.Sprintf("── %s (%d) ", group.title, len(group.checks))))
			b.WriteString("\n")
		}
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

	// Show re-run hint when there are failed checks with rerunnable workflows
	if failedIDs := m.ciStatus.FailedRunIDs(); len(failedIDs) > 0 {
		hintStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Italic(true)
		b.WriteString(hintStyle.Render("Press x to re-run failed checks"))
		b.WriteString("\n")
	}

	return b.String()
}

// buildCachedLines renders all diff content into the cachedLines slice.
// It computes fileOffsets, hunkOffsets, hunkLineRanges, and cachedLineInfo.
func (m *DiffViewerModel) buildCachedLines() {
	if len(m.files) == 0 {
		m.cachedLines = []string{renderEmptyState("No files changed in this PR", "")}
		m.cachedLineInfo = []lineInfo{{hunkIdx: -1}}
		m.hunkLineRanges = nil
		return
	}

	innerWidth := m.viewport.Width
	lines := make([]string, 0, 256)
	infos := make([]lineInfo, 0, 256)
	m.fileOffsets = make([]int, len(m.files))
	m.hunkOffsets = make([]int, len(m.hunks))
	m.hunkLineRanges = make([][2]int, len(m.hunks))
	globalHunkIdx := 0

	nonHunkInfo := lineInfo{hunkIdx: -1}

	for i, f := range m.files {
		if i > 0 {
			lines = append(lines, "")
			infos = append(infos, nonHunkInfo)
		}

		// Track line offset where this file header starts
		m.fileOffsets[i] = len(lines)

		// File header
		lines = append(lines, diffFileHeaderStyle.Render(fileStatusLabel(f)))
		infos = append(infos, nonHunkInfo)

		// Separator
		lines = append(lines, strings.Repeat("─", min(innerWidth, 60)))
		infos = append(infos, nonHunkInfo)

		// Patch content
		if f.Patch == "" {
			lines = append(lines, lipgloss.NewStyle().
				Foreground(lipgloss.Color("244")).
				Italic(true).
				Render("  (diff not available)"))
			infos = append(infos, nonHunkInfo)
			continue
		}

		lines = append(lines, "") // blank before hunks
		infos = append(infos, nonHunkInfo)

		// Render pre-parsed hunks
		for globalHunkIdx < len(m.hunks) && m.hunks[globalHunkIdx].FileIndex == i {
			m.hunkOffsets[globalHunkIdx] = len(lines)
			start := len(lines)
			hunkLines, hunkInfos := m.renderHunkLines(globalHunkIdx)
			lines = append(lines, hunkLines...)
			infos = append(infos, hunkInfos...)
			m.hunkLineRanges[globalHunkIdx] = [2]int{start, len(lines)}
			globalHunkIdx++
		}
	}

	m.cachedLines = lines
	m.cachedLineInfo = infos
	m.lastRenderedFocus = m.focusedHunkIdx
	m.dirtyHunks = nil

	// Full cache rebuild invalidates selection indices
	m.selectionAnchor = -1

	// Clamp cursor and snap to first diff line if needed
	m.clampCursor()
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

// renderHunkLines renders a single hunk's styled output lines and parallel line info.
func (m *DiffViewerModel) renderHunkLines(hunkIdx int) ([]string, []lineInfo) {
	hunk := m.hunks[hunkIdx]
	selected := m.selectedHunks[hunkIdx]
	isFocused := hunkIdx == m.focusedHunkIdx
	hasAIComments := len(m.aiCommentsByFileLine) > 0
	hasGHComments := len(m.ghCommentThreads) > 0
	hasPendingComments := len(m.pendingCommentsByFileLine) > 0
	lines := make([]string, 0, len(hunk.Lines))
	infos := make([]lineInfo, 0, len(hunk.Lines))

	// Compute cursor's comment target key so we can highlight the targeted comment box.
	cursorTargetKey := ""
	if targetLine, targetFile := m.commentTargetFromCursor(); targetLine > 0 {
		cursorTargetKey = fmt.Sprintf("%s:%d", targetFile, targetLine)
	}

	// Base offset of this hunk in cachedLines (for cursor comparison).
	hunkBase := -1
	if hunkIdx < len(m.hunkOffsets) {
		hunkBase = m.hunkOffsets[hunkIdx]
	}

	// Multi-line selection range (if active and in this hunk)
	selLo, selHi := m.selectionRange()

	// Track new-side line number for inline comment matching
	newLine := 0

	for lineIdx, line := range hunk.Lines {
		// Compute absolute position in cachedLines for cursor/selection check
		absPos := -1
		if hunkBase >= 0 {
			absPos = hunkBase + len(lines)
		}
		isCursorLine := absPos >= 0 && absPos == m.cursorLine
		isInSelection := absPos >= 0 && selLo >= 0 && absPos >= selLo && absPos <= selHi

		if line == "" {
			if isCursorLine {
				lines = append(lines, diffCursorGutterStyle.Render("▸"))
			} else if isInSelection {
				lines = append(lines, diffSelectionGutterStyle.Render("▌"))
			} else if isFocused {
				lines = append(lines, diffFocusGutterStyle.Render("▎"))
			} else {
				lines = append(lines, "")
			}
			infos = append(infos, lineInfo{hunkIdx: hunkIdx, filename: hunk.Filename})
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

		commentable := newLine > 0 && !strings.HasPrefix(line, "-") && !strings.HasPrefix(line, `\`) && !strings.HasPrefix(line, "@@")

		// Gutter marker: ▸ for cursor, ▌ for selection, ▎ for focused hunk, space for others
		var gutter string
		if isCursorLine {
			gutter = diffCursorGutterStyle.Render("▸") + " "
		} else if isInSelection {
			gutter = diffSelectionGutterStyle.Render("▌") + " "
		} else if isFocused {
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
		if isInSelection {
			style = style.Background(diffSelectionBg)
		}
		if isCursorLine {
			style = style.Background(diffCursorBg)
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
		infos = append(infos, lineInfo{
			hunkIdx:       hunkIdx,
			filename:      hunk.Filename,
			newLineNum:    newLine,
			isCommentable: commentable,
			isDiffLine:    true,
		})

		// Inject inline comments after matching lines (+ or context lines)
		if commentable {
			key := fmt.Sprintf("%s:%d", hunk.Filename, newLine)
			boxInnerWidth := m.viewport.Width - 2 - 2 - 2 // gutter, border, padding
			if boxInnerWidth < 10 {
				boxInnerWidth = 10
			}
			isTargeted := cursorTargetKey != "" && key == cursorTargetKey

			// Gutter for comment lines: continue the focused hunk indicator
			commentGutter := "  "
			if isFocused {
				commentGutter = diffFocusGutterStyle.Render("▎") + " "
			}

			// AI inline comments
			if hasAIComments {
				if comments, ok := m.aiCommentsByFileLine[key]; ok {
					for _, c := range comments {
						header := commentBoxHeaderStyle.Render("💬 Claude AI")
						body := m.renderMarkdown(c.Body, boxInnerWidth)
						borderColor := commentBoxAIBorder
						if isTargeted {
							borderColor = commentBoxAIBorderHi
						}
						boxLines := m.renderCommentBox(header, body, borderColor, isTargeted, commentGutter)
						for range boxLines {
							infos = append(infos, lineInfo{hunkIdx: hunkIdx, filename: hunk.Filename, comment: commentAI})
						}
						lines = append(lines, boxLines...)
					}
				}
			}

			// GitHub inline comments (threaded)
			if hasGHComments {
				if threads, ok := m.ghCommentThreads[key]; ok {
					for _, t := range threads {
						threadLines := m.renderGHCommentThread(t, isTargeted, commentGutter)
						for range threadLines {
							infos = append(infos, lineInfo{hunkIdx: hunkIdx, filename: hunk.Filename, comment: commentGitHub})
						}
						lines = append(lines, threadLines...)
					}
				}
			}

			// Pending inline comments (user + AI drafts)
			if hasPendingComments {
				if comments, ok := m.pendingCommentsByFileLine[key]; ok {
					for _, c := range comments {
						source := "Draft"
						if c.Source == "ai" {
							source = "Draft (AI)"
						}
						header := commentBoxHeaderStyle.Render("📝 " + source)
						body := m.renderMarkdown(c.Body, boxInnerWidth)
						borderColor := commentBoxPendingBorder
						if isTargeted {
							borderColor = commentBoxPendingBorderHi
						}
						boxLines := m.renderCommentBox(header, body, borderColor, isTargeted, commentGutter)
						for range boxLines {
							infos = append(infos, lineInfo{hunkIdx: hunkIdx, filename: hunk.Filename, comment: commentPending})
						}
						lines = append(lines, boxLines...)
					}
				}
			}
		}

		// Advance new-side line counter for + and context lines
		if !strings.HasPrefix(line, "-") && !strings.HasPrefix(line, `\`) && !strings.HasPrefix(line, "@@") {
			newLine++
		}
	}

	return lines, infos
}

// commentBoxMaxPreviewLines is the maximum body lines shown in the inline preview.
const commentBoxMaxPreviewLines = 3

// renderCommentBox renders content inside a bordered box, split into viewport lines.
// header is the first line inside the box (e.g. "💬 Claude AI").
// body is the pre-rendered content (already glamour-processed or plain text).
// borderColor is the lipgloss color for the rounded border.
// highlighted uses a thick border and brighter color to indicate cursor targeting.
// gutter is the left margin prefix for each line (e.g. "▎ " for focused hunk).
func (m *DiffViewerModel) renderCommentBox(header, body string, borderColor lipgloss.Color, highlighted bool, gutter string) []string {
	boxWidth := m.viewport.Width - 2 // 2-char gutter
	if boxWidth < 14 {
		boxWidth = 14
	}

	// Assemble content: header + body
	var content strings.Builder
	content.WriteString(header)
	if body != "" {
		content.WriteString("\n")
		// Trim and apply preview limit
		bodyLines := strings.Split(body, "\n")
		// Remove trailing empty lines from glamour output
		for len(bodyLines) > 0 && strings.TrimSpace(bodyLines[len(bodyLines)-1]) == "" {
			bodyLines = bodyLines[:len(bodyLines)-1]
		}
		if len(bodyLines) > commentBoxMaxPreviewLines {
			remaining := len(bodyLines) - commentBoxMaxPreviewLines
			bodyLines = bodyLines[:commentBoxMaxPreviewLines]
			bodyLines = append(bodyLines, commentBoxTrimStyle.Render(fmt.Sprintf("[+%d lines]", remaining)))
		}
		content.WriteString(strings.Join(bodyLines, "\n"))
	}

	// Add [c] hint on last line of content
	hintStyle := commentBoxHintStyle
	if highlighted {
		hintStyle = commentBoxHintHiStyle
	}
	content.WriteString("  " + hintStyle.Render("[c]"))

	border := lipgloss.RoundedBorder()
	if highlighted {
		border = lipgloss.ThickBorder()
	}

	boxStyle := lipgloss.NewStyle().
		Border(border).
		BorderForeground(borderColor).
		Width(boxWidth - 2). // -2 for border chars
		PaddingLeft(1).PaddingRight(1)

	rendered := boxStyle.Render(content.String())

	// Split into viewport lines and prepend gutter
	result := strings.Split(rendered, "\n")
	for i, line := range result {
		result[i] = gutter + line
	}
	return result
}

// renderGHCommentThread renders a single GitHub comment thread inside a bordered box.
func (m *DiffViewerModel) renderGHCommentThread(t ghCommentThread, highlighted bool, gutter string) []string {
	boxInnerWidth := m.viewport.Width - 2 - 2 - 2 // gutter, border, padding
	if boxInnerWidth < 10 {
		boxInnerWidth = 10
	}

	// Header: 💬 @author · Jan 2 15:04
	header := commentBoxHeaderStyle.Render("💬 @"+t.Root.Author.Login) +
		commentBoxMetaStyle.Render(" · "+t.Root.CreatedAt.Format("Jan 2 15:04"))

	// Build body: root body + replies
	var body strings.Builder
	body.WriteString(m.renderMarkdown(t.Root.Body, boxInnerWidth))

	for i, r := range t.Replies {
		if i >= 1 {
			// Trim after first reply
			remaining := len(t.Replies) - 1
			body.WriteString("\n")
			body.WriteString(commentBoxTrimStyle.Render(fmt.Sprintf("[+%d more replies]", remaining)))
			break
		}
		body.WriteString("\n")
		replyHeader := commentBoxReplyStyle.Render("↳ ") +
			commentBoxHeaderStyle.Render("@"+r.Author.Login) +
			commentBoxMetaStyle.Render(" · "+r.CreatedAt.Format("Jan 2 15:04"))
		body.WriteString(replyHeader)
		body.WriteString("\n")
		body.WriteString(m.renderMarkdown(r.Body, boxInnerWidth))
	}

	borderColor := commentBoxGitHubBorder
	if highlighted {
		borderColor = commentBoxGitHubBorderHi
	}
	return m.renderCommentBox(header, body.String(), borderColor, highlighted, gutter)
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
	newLines, newInfos := m.renderHunkLines(hunkIdx)
	for i, line := range newLines {
		m.cachedLines[r[0]+i] = line
		if r[0]+i < len(m.cachedLineInfo) {
			m.cachedLineInfo[r[0]+i] = newInfos[i]
		}
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

// moveCursor moves the line cursor by delta positions, skipping non-diff lines.
// It also updates focusedHunkIdx and marks affected hunks dirty.
func (m *DiffViewerModel) moveCursor(delta int) {
	if len(m.cachedLineInfo) == 0 {
		return
	}

	oldHunk := -1
	if m.cursorLine >= 0 && m.cursorLine < len(m.cachedLineInfo) {
		oldHunk = m.cachedLineInfo[m.cursorLine].hunkIdx
	}

	// Step in the given direction, skipping non-diff lines
	newPos := m.cursorLine
	for {
		newPos += delta
		if newPos < 0 || newPos >= len(m.cachedLineInfo) {
			// Hit boundary — stay put
			return
		}
		if m.cachedLineInfo[newPos].isDiffLine {
			break
		}
	}

	m.cursorLine = newPos

	// Sync focused hunk to cursor
	newHunk := m.cachedLineInfo[m.cursorLine].hunkIdx
	if newHunk >= 0 {
		m.focusedHunkIdx = newHunk
	}

	// Mark affected hunks dirty so they re-render with updated cursor indicator
	if oldHunk >= 0 {
		m.markHunkDirty(oldHunk)
	}
	if newHunk >= 0 {
		m.markHunkDirty(newHunk)
	}

	m.ensureCursorVisible()
}

// extendSelection extends the multi-line selection by moving the cursor in the
// given direction while keeping the anchor fixed. If no selection is active, the
// anchor is set to the current cursor position. Movement is clamped to the same hunk.
func (m *DiffViewerModel) extendSelection(delta int) {
	if len(m.cachedLineInfo) == 0 {
		return
	}

	// Start selection if not active
	if m.selectionAnchor < 0 {
		m.selectionAnchor = m.cursorLine
	}

	anchorHunk := -1
	if m.selectionAnchor >= 0 && m.selectionAnchor < len(m.cachedLineInfo) {
		anchorHunk = m.cachedLineInfo[m.selectionAnchor].hunkIdx
	}

	oldCursor := m.cursorLine
	m.moveCursor(delta)

	// If cursor moved to a different hunk, undo the move
	if m.cursorLine >= 0 && m.cursorLine < len(m.cachedLineInfo) {
		newHunk := m.cachedLineInfo[m.cursorLine].hunkIdx
		if newHunk != anchorHunk {
			m.cursorLine = oldCursor
			// Restore focused hunk
			if anchorHunk >= 0 {
				m.focusedHunkIdx = anchorHunk
			}
		}
	}
}

// cancelSelection clears any active multi-line selection and marks affected hunks dirty.
func (m *DiffViewerModel) cancelSelection() {
	if m.selectionAnchor < 0 {
		return
	}
	// Mark the hunk containing the selection dirty so it re-renders without highlight
	if m.selectionAnchor >= 0 && m.selectionAnchor < len(m.cachedLineInfo) {
		hunk := m.cachedLineInfo[m.selectionAnchor].hunkIdx
		if hunk >= 0 {
			m.markHunkDirty(hunk)
		}
	}
	m.selectionAnchor = -1
}

// HasSelection returns true when a multi-line selection is active.
func (m DiffViewerModel) HasSelection() bool {
	return m.selectionAnchor >= 0
}

// selectionRange returns the ordered start/end indices in cachedLineInfo
// for the current selection. Returns (-1, -1) if no selection is active.
func (m DiffViewerModel) selectionRange() (int, int) {
	if m.selectionAnchor < 0 {
		return -1, -1
	}
	lo, hi := m.selectionAnchor, m.cursorLine
	if lo > hi {
		lo, hi = hi, lo
	}
	return lo, hi
}

// ensureCursorVisible scrolls the viewport so the cursor line is on-screen.
func (m *DiffViewerModel) ensureCursorVisible() {
	if m.cursorLine < m.viewport.YOffset {
		m.viewport.SetYOffset(m.cursorLine)
	} else if m.cursorLine >= m.viewport.YOffset+m.viewport.Height {
		m.viewport.SetYOffset(m.cursorLine - m.viewport.Height + 1)
	}
}

// clampCursor ensures cursorLine is within bounds and on a diff line.
// Called after cache rebuilds when absolute positions may have shifted.
func (m *DiffViewerModel) clampCursor() {
	if len(m.cachedLineInfo) == 0 {
		m.cursorLine = 0
		return
	}
	if m.cursorLine >= len(m.cachedLineInfo) {
		m.cursorLine = len(m.cachedLineInfo) - 1
	}
	if m.cursorLine < 0 {
		m.cursorLine = 0
	}
	// If not on a diff line, find the nearest one forward then backward
	if !m.cachedLineInfo[m.cursorLine].isDiffLine {
		m.snapCursorToNearestDiffLine()
	}
}

// snapCursorToNearestDiffLine moves the cursor to the nearest diff line,
// searching forward first, then backward.
func (m *DiffViewerModel) snapCursorToNearestDiffLine() {
	// Search forward
	for i := m.cursorLine; i < len(m.cachedLineInfo); i++ {
		if m.cachedLineInfo[i].isDiffLine {
			m.cursorLine = i
			return
		}
	}
	// Search backward
	for i := m.cursorLine - 1; i >= 0; i-- {
		if m.cachedLineInfo[i].isDiffLine {
			m.cursorLine = i
			return
		}
	}
}

// syncCursorToScroll snaps the cursor to a visible diff line after viewport jumps.
func (m *DiffViewerModel) syncCursorToScroll() {
	if len(m.cachedLineInfo) == 0 {
		return
	}

	oldHunk := -1
	if m.cursorLine >= 0 && m.cursorLine < len(m.cachedLineInfo) {
		oldHunk = m.cachedLineInfo[m.cursorLine].hunkIdx
	}

	// Place cursor at first visible diff line
	for i := m.viewport.YOffset; i < m.viewport.YOffset+m.viewport.Height && i < len(m.cachedLineInfo); i++ {
		if m.cachedLineInfo[i].isDiffLine {
			m.cursorLine = i
			break
		}
	}

	// Mark old and new hunks dirty
	if oldHunk >= 0 {
		m.markHunkDirty(oldHunk)
	}
	if m.cursorLine >= 0 && m.cursorLine < len(m.cachedLineInfo) {
		newHunk := m.cachedLineInfo[m.cursorLine].hunkIdx
		if newHunk >= 0 {
			m.markHunkDirty(newHunk)
		}
	}
}

// syncCursorToFocusedHunk moves the cursor to the first diff line of the focused hunk.
func (m *DiffViewerModel) syncCursorToFocusedHunk() {
	if m.focusedHunkIdx < 0 || m.focusedHunkIdx >= len(m.hunkOffsets) || len(m.cachedLineInfo) == 0 {
		return
	}

	oldHunk := -1
	if m.cursorLine >= 0 && m.cursorLine < len(m.cachedLineInfo) {
		oldHunk = m.cachedLineInfo[m.cursorLine].hunkIdx
	}

	start := m.hunkOffsets[m.focusedHunkIdx]
	for i := start; i < len(m.cachedLineInfo); i++ {
		if m.cachedLineInfo[i].isDiffLine && m.cachedLineInfo[i].hunkIdx == m.focusedHunkIdx {
			m.cursorLine = i
			break
		}
	}

	if oldHunk >= 0 {
		m.markHunkDirty(oldHunk)
	}
	m.markHunkDirty(m.focusedHunkIdx)
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
