package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// handleSearchModeKey processes key events while search input mode is active.
func (m *DiffViewerModel) handleSearchModeKey(msg tea.KeyMsg) (DiffViewerModel, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.searchMode = false
		m.searchInput.Blur()
		if m.searchInput.Value() == "" {
			m.clearSearch()
		}
		m.cachedLines = nil
		m.refreshContent()
		return *m, nil
	case "enter":
		m.searchMode = false
		m.searchInput.Blur()
		m.refreshContent()
		return *m, nil
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
		return *m, cmd
	}
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
