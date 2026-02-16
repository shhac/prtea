package ui

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
	target := offset - 2
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

	newPos := m.cursorLine
	for {
		newPos += delta
		if newPos < 0 || newPos >= len(m.cachedLineInfo) {
			return
		}
		if m.cachedLineInfo[newPos].isDiffLine {
			break
		}
	}

	m.cursorLine = newPos

	newHunk := m.cachedLineInfo[m.cursorLine].hunkIdx
	if newHunk >= 0 {
		m.focusedHunkIdx = newHunk
	}

	if oldHunk >= 0 {
		m.markHunkDirty(oldHunk)
	}
	if newHunk >= 0 {
		m.markHunkDirty(newHunk)
	}

	m.ensureCursorVisible()
}

// extendSelection extends the multi-line selection by moving the cursor in the
// given direction while keeping the anchor fixed. Movement is clamped to the same hunk.
func (m *DiffViewerModel) extendSelection(delta int) {
	if len(m.cachedLineInfo) == 0 {
		return
	}

	if m.selectionAnchor < 0 {
		m.selectionAnchor = m.cursorLine
	}

	anchorHunk := -1
	if m.selectionAnchor >= 0 && m.selectionAnchor < len(m.cachedLineInfo) {
		anchorHunk = m.cachedLineInfo[m.selectionAnchor].hunkIdx
	}

	oldCursor := m.cursorLine
	m.moveCursor(delta)

	if m.cursorLine >= 0 && m.cursorLine < len(m.cachedLineInfo) {
		newHunk := m.cachedLineInfo[m.cursorLine].hunkIdx
		if newHunk != anchorHunk {
			m.cursorLine = oldCursor
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
	if !m.cachedLineInfo[m.cursorLine].isDiffLine {
		m.snapCursorToNearestDiffLine()
	}
}

// snapCursorToNearestDiffLine moves the cursor to the nearest diff line,
// searching forward first, then backward.
func (m *DiffViewerModel) snapCursorToNearestDiffLine() {
	for i := m.cursorLine; i < len(m.cachedLineInfo); i++ {
		if m.cachedLineInfo[i].isDiffLine {
			m.cursorLine = i
			return
		}
	}
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

	for i := m.viewport.YOffset; i < m.viewport.YOffset+m.viewport.Height && i < len(m.cachedLineInfo); i++ {
		if m.cachedLineInfo[i].isDiffLine {
			m.cursorLine = i
			break
		}
	}

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
