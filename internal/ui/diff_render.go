package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

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

		m.fileOffsets[i] = len(lines)

		// File header
		lines = append(lines, diffFileHeaderStyle.Render(fileStatusLabel(f)))
		infos = append(infos, nonHunkInfo)

		// Separator
		lines = append(lines, strings.Repeat("─", min(innerWidth, 60)))
		infos = append(infos, nonHunkInfo)

		// Patch content
		if f.Patch == "" {
			lines = append(lines, dimItalicStyle.Render("  (diff not available)"))
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

// renderHunkLines renders a single hunk's styled output lines and parallel line info.
func (m *DiffViewerModel) renderHunkLines(hunkIdx int) ([]string, []lineInfo) {
	hunk := m.hunks[hunkIdx]
	selected := m.selectedHunks[hunkIdx]
	isFocused := hunkIdx == m.focusedHunkIdx
	hasInlineComments := len(m.aiCommentsByFileLine) > 0 || len(m.ghCommentThreads) > 0 || len(m.pendingCommentsByFileLine) > 0
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
		absPos := -1
		if hunkBase >= 0 {
			absPos = hunkBase + len(lines)
		}
		isCursorLine := absPos >= 0 && absPos == m.cursorLine
		isInSelection := absPos >= 0 && selLo >= 0 && absPos >= selLo && absPos <= selHi

		if line == "" {
			lines = append(lines, renderGutterOnly(isCursorLine, isInSelection, isFocused))
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

		gutter := renderGutter(isCursorLine, isInSelection, isFocused)
		style, displayLine := styleDiffLine(line, isFocused, selected)

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
		if commentable && hasInlineComments {
			lines, infos = m.injectInlineComments(lines, infos, hunkIdx, hunk.Filename, newLine, isFocused, cursorTargetKey)
		}

		// Advance new-side line counter for + and context lines
		if !strings.HasPrefix(line, "-") && !strings.HasPrefix(line, `\`) && !strings.HasPrefix(line, "@@") {
			newLine++
		}
	}

	return lines, infos
}

// renderGutterOnly returns just a gutter marker for empty lines.
func renderGutterOnly(isCursor, isSelected, isFocused bool) string {
	switch {
	case isCursor:
		return diffCursorGutterStyle.Render("▸")
	case isSelected:
		return diffSelectionGutterStyle.Render("▌")
	case isFocused:
		return diffFocusGutterStyle.Render("▎")
	default:
		return ""
	}
}

// renderGutter returns the gutter prefix string for a diff line.
func renderGutter(isCursor, isSelected, isFocused bool) string {
	switch {
	case isCursor:
		return diffCursorGutterStyle.Render("▸") + " "
	case isSelected:
		return diffSelectionGutterStyle.Render("▌") + " "
	case isFocused:
		return diffFocusGutterStyle.Render("▎") + " "
	default:
		return "  "
	}
}

// styleDiffLine returns the style and display text for a diff line based on its prefix.
func styleDiffLine(line string, isFocused, selected bool) (lipgloss.Style, string) {
	displayLine := line
	switch {
	case strings.HasPrefix(line, "@@"):
		if isFocused {
			if selected {
				return diffFocusedHunkStyle, "✓ " + line
			}
			return diffFocusedHunkStyle, "▶ " + line
		}
		if selected {
			return diffHunkHeaderStyle, "✓ " + line
		}
		return diffHunkHeaderStyle, displayLine
	case strings.HasPrefix(line, "+"):
		return diffAddedStyle, displayLine
	case strings.HasPrefix(line, "-"):
		return diffRemovedStyle, displayLine
	case strings.HasPrefix(line, `\`):
		return dimItalicStyle, displayLine
	default:
		return lipgloss.NewStyle(), displayLine
	}
}

// rerenderHunkInCache re-renders a single hunk's styled lines in the cache.
func (m *DiffViewerModel) rerenderHunkInCache(hunkIdx int) {
	if hunkIdx < 0 || hunkIdx >= len(m.hunkLineRanges) {
		return
	}
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
