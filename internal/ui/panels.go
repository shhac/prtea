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

	leftRatio   = 0.20
	centerRatio = 0.50
	rightRatio  = 0.30

	// 2-panel mode ratios
	twoLeftRatio   = 0.25
	twoCenterRatio = 0.75

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
// and whether the right panel is collapsed.
func CalculatePanelSizes(termWidth, termHeight int, rightCollapsed bool) PanelSizes {
	if termWidth < minTotalWidth {
		return PanelSizes{TooSmall: true}
	}

	panelHeight := termHeight - statusBarHeight
	if panelHeight < 5 {
		return PanelSizes{TooSmall: true}
	}

	// Account for borders (2 chars per panel for left+right border)
	usableWidth := termWidth

	autoCollapse := termWidth < collapseThreshold
	collapsed := rightCollapsed || autoCollapse

	if collapsed {
		leftW := max(minLeftWidth, int(float64(usableWidth)*twoLeftRatio))
		centerW := usableWidth - leftW
		if centerW < minCenterWidth {
			centerW = minCenterWidth
			leftW = usableWidth - centerW
		}
		return PanelSizes{
			LeftWidth:   leftW,
			CenterWidth: centerW,
			RightWidth:  0,
			PanelHeight: panelHeight,
		}
	}

	leftW := max(minLeftWidth, int(float64(usableWidth)*leftRatio))
	rightW := max(minRightWidth, int(float64(usableWidth)*rightRatio))
	centerW := usableWidth - leftW - rightW
	if centerW < minCenterWidth {
		centerW = minCenterWidth
		rightW = usableWidth - leftW - centerW
		if rightW < minRightWidth {
			// Fall back to 2-panel mode
			rightW = 0
			centerW = usableWidth - leftW
		}
	}

	return PanelSizes{
		LeftWidth:   leftW,
		CenterWidth: centerW,
		RightWidth:  rightW,
		PanelHeight: panelHeight,
	}
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
