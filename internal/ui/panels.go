package ui

// Panel identifies which panel has focus.
type Panel int

const (
	PanelLeft   Panel = iota // PR List
	PanelCenter              // Diff Viewer
	PanelRight               // Chat/Analysis
)

// AppMode represents the current input mode.
type AppMode int

const (
	ModeNavigation AppMode = iota
	ModeInsert
	ModeOverlay
)

// Layout constants
const (
	minLeftWidth   = 20
	minCenterWidth = 40
	minRightWidth  = 25
	minTotalWidth  = 80

	collapseThreshold = 120

	// 3-panel mode ratios
	leftRatio  = 0.20
	rightRatio = 0.30

	// 2-panel mode ratios
	twoLCLeftRatio   = 0.25 // Left + Center: left panel share
	twoLRLeftRatio   = 0.30 // Left + Right: left panel share
	twoCRCenterRatio = 0.60 // Center + Right: center panel share

	statusBarHeight = 1
)

// PanelSizes holds calculated panel dimensions.
type PanelSizes struct {
	LeftWidth   int
	CenterWidth int
	RightWidth  int
	PanelHeight int
	TooSmall    bool
}

// CalculatePanelSizes determines panel widths based on terminal dimensions
// and which panels are visible.
func CalculatePanelSizes(termWidth, termHeight int, visible [3]bool) PanelSizes {
	numVisible := visibleCount(visible)
	if numVisible == 0 || termWidth < minTotalWidth {
		return PanelSizes{TooSmall: true}
	}

	panelHeight := termHeight - statusBarHeight
	if panelHeight < 5 {
		return PanelSizes{TooSmall: true}
	}

	usableWidth := termWidth

	switch numVisible {
	case 1:
		sizes := PanelSizes{PanelHeight: panelHeight}
		if visible[PanelLeft] {
			sizes.LeftWidth = usableWidth
		} else if visible[PanelCenter] {
			sizes.CenterWidth = usableWidth
		} else {
			sizes.RightWidth = usableWidth
		}
		return sizes

	case 2:
		return calcTwoPanels(usableWidth, panelHeight, visible)

	case 3:
		return calcThreePanels(usableWidth, panelHeight)
	}

	return PanelSizes{TooSmall: true}
}

func calcTwoPanels(width, height int, visible [3]bool) PanelSizes {
	sizes := PanelSizes{PanelHeight: height}

	switch {
	case visible[PanelLeft] && visible[PanelCenter]:
		leftW := max(minLeftWidth, int(float64(width)*twoLCLeftRatio))
		centerW := width - leftW
		if centerW < minCenterWidth {
			centerW = minCenterWidth
			leftW = width - centerW
		}
		sizes.LeftWidth = leftW
		sizes.CenterWidth = centerW

	case visible[PanelLeft] && visible[PanelRight]:
		leftW := max(minLeftWidth, int(float64(width)*twoLRLeftRatio))
		rightW := width - leftW
		if rightW < minRightWidth {
			rightW = minRightWidth
			leftW = width - rightW
		}
		sizes.LeftWidth = leftW
		sizes.RightWidth = rightW

	case visible[PanelCenter] && visible[PanelRight]:
		centerW := max(minCenterWidth, int(float64(width)*twoCRCenterRatio))
		rightW := width - centerW
		if rightW < minRightWidth {
			rightW = minRightWidth
			centerW = width - rightW
		}
		sizes.CenterWidth = centerW
		sizes.RightWidth = rightW
	}

	return sizes
}

func calcThreePanels(width, height int) PanelSizes {
	leftW := max(minLeftWidth, int(float64(width)*leftRatio))
	rightW := max(minRightWidth, int(float64(width)*rightRatio))
	centerW := width - leftW - rightW
	if centerW < minCenterWidth {
		centerW = minCenterWidth
		rightW = width - leftW - centerW
		if rightW < 0 {
			rightW = 0
		}
	}
	return PanelSizes{
		LeftWidth:   leftW,
		CenterWidth: centerW,
		RightWidth:  rightW,
		PanelHeight: height,
	}
}

// visibleCount returns the number of visible panels.
func visibleCount(visible [3]bool) int {
	n := 0
	for _, v := range visible {
		if v {
			n++
		}
	}
	return n
}

// nextVisiblePanel returns the next visible panel after current, cycling forward.
func nextVisiblePanel(current Panel, visible [3]bool) Panel {
	for i := 1; i <= 3; i++ {
		candidate := Panel((int(current) + i) % 3)
		if visible[candidate] {
			return candidate
		}
	}
	return current
}

// prevVisiblePanel returns the previous visible panel before current, cycling backward.
func prevVisiblePanel(current Panel, visible [3]bool) Panel {
	for i := 1; i <= 3; i++ {
		candidate := Panel((int(current) - i + 3) % 3)
		if visible[candidate] {
			return candidate
		}
	}
	return current
}

func (p Panel) Next() Panel {
	switch p {
	case PanelLeft:
		return PanelCenter
	case PanelCenter:
		return PanelRight
	case PanelRight:
		return PanelLeft
	default:
		return PanelLeft
	}
}

func (p Panel) Prev() Panel {
	switch p {
	case PanelLeft:
		return PanelRight
	case PanelCenter:
		return PanelLeft
	case PanelRight:
		return PanelCenter
	default:
		return PanelLeft
	}
}

func (p Panel) String() string {
	switch p {
	case PanelLeft:
		return "PR List"
	case PanelCenter:
		return "Diff Viewer"
	case PanelRight:
		return "Chat"
	default:
		return "Unknown"
	}
}
