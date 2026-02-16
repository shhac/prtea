package ui

import "strings"

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
