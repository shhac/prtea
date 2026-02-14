package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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

	// Cached rendering — avoids re-parsing and re-styling on every scroll
	cachedLines    []string // per-line styled output (nil = needs full rebuild)
	hunkLineRanges [][2]int // [start, end) line indices in cachedLines per hunk

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
	return DiffViewerModel{
		spinner: newLoadingSpinner(),
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
			if m.activeTab == TabDiff && len(m.hunks) > 0 && m.focusedHunkIdx > 0 {
				prevOffset := m.hunkOffsets[m.focusedHunkIdx-1]
				// If scrolling up 1 line would make previous hunk's header visible
				if prevOffset >= m.viewport.YOffset-1 {
					m.focusedHunkIdx--
					m.scrollToFocusedHunk()
					m.refreshContent()
					return m, nil
				}
			}
			var cmd tea.Cmd
			oldFocus := m.focusedHunkIdx
			m.viewport, cmd = m.viewport.Update(msg)
			if m.activeTab == TabDiff {
				m.syncFocusToScroll()
				// When scrolling up, don't allow focus to jump forward
				if m.focusedHunkIdx > oldFocus {
					m.focusedHunkIdx = oldFocus
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
						}
					}
					m.refreshContent()
				}
			}
			return m, nil
		case key.Matches(msg, DiffViewerKeys.ClearSelection):
			if m.activeTab == TabDiff && len(m.selectedHunks) > 0 {
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

func (m *DiffViewerModel) refreshContent() {
	if !m.ready {
		return
	}

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
		m.buildCachedLines()
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
}

// renderHunkLines renders a single hunk's styled output lines.
func (m *DiffViewerModel) renderHunkLines(hunkIdx int) []string {
	hunk := m.hunks[hunkIdx]
	selected := m.selectedHunks[hunkIdx]
	isFocused := hunkIdx == m.focusedHunkIdx
	lines := make([]string, 0, len(hunk.Lines))

	for _, line := range hunk.Lines {
		if line == "" {
			if isFocused {
				lines = append(lines, diffFocusGutterStyle.Render("▎"))
			} else {
				lines = append(lines, "")
			}
			continue
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

		lines = append(lines, gutter+style.Render(displayLine))
	}

	return lines
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
