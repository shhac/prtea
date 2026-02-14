package ui

import "testing"

func TestVisibleCount(t *testing.T) {
	tests := []struct {
		name    string
		visible [3]bool
		want    int
	}{
		{"none visible", [3]bool{false, false, false}, 0},
		{"all visible", [3]bool{true, true, true}, 3},
		{"left only", [3]bool{true, false, false}, 1},
		{"center+right", [3]bool{false, true, true}, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := visibleCount(tt.visible); got != tt.want {
				t.Errorf("visibleCount(%v) = %d, want %d", tt.visible, got, tt.want)
			}
		})
	}
}

func TestNextVisiblePanel(t *testing.T) {
	tests := []struct {
		name    string
		current Panel
		visible [3]bool
		want    Panel
	}{
		{"all visible, from left", PanelLeft, [3]bool{true, true, true}, PanelCenter},
		{"all visible, from center", PanelCenter, [3]bool{true, true, true}, PanelRight},
		{"all visible, from right wraps", PanelRight, [3]bool{true, true, true}, PanelLeft},
		{"skip hidden center", PanelLeft, [3]bool{true, false, true}, PanelRight},
		{"skip hidden right", PanelCenter, [3]bool{true, true, false}, PanelLeft},
		{"only one visible returns current", PanelLeft, [3]bool{true, false, false}, PanelLeft},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := nextVisiblePanel(tt.current, tt.visible); got != tt.want {
				t.Errorf("nextVisiblePanel(%v, %v) = %v, want %v", tt.current, tt.visible, got, tt.want)
			}
		})
	}
}

func TestPrevVisiblePanel(t *testing.T) {
	tests := []struct {
		name    string
		current Panel
		visible [3]bool
		want    Panel
	}{
		{"all visible, from right", PanelRight, [3]bool{true, true, true}, PanelCenter},
		{"all visible, from center", PanelCenter, [3]bool{true, true, true}, PanelLeft},
		{"all visible, from left wraps", PanelLeft, [3]bool{true, true, true}, PanelRight},
		{"skip hidden center", PanelRight, [3]bool{true, false, true}, PanelLeft},
		{"only one visible returns current", PanelCenter, [3]bool{false, true, false}, PanelCenter},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := prevVisiblePanel(tt.current, tt.visible); got != tt.want {
				t.Errorf("prevVisiblePanel(%v, %v) = %v, want %v", tt.current, tt.visible, got, tt.want)
			}
		})
	}
}

func TestPanelNextPrev(t *testing.T) {
	tests := []struct {
		name string
		p    Panel
		next Panel
		prev Panel
	}{
		{"left", PanelLeft, PanelCenter, PanelRight},
		{"center", PanelCenter, PanelRight, PanelLeft},
		{"right", PanelRight, PanelLeft, PanelCenter},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.p.Next(); got != tt.next {
				t.Errorf("%v.Next() = %v, want %v", tt.p, got, tt.next)
			}
			if got := tt.p.Prev(); got != tt.prev {
				t.Errorf("%v.Prev() = %v, want %v", tt.p, got, tt.prev)
			}
		})
	}
}

func TestPanelString(t *testing.T) {
	tests := []struct {
		p    Panel
		want string
	}{
		{PanelLeft, "PR List"},
		{PanelCenter, "Diff Viewer"},
		{PanelRight, "Chat"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.p.String(); got != tt.want {
				t.Errorf("Panel(%d).String() = %q, want %q", tt.p, got, tt.want)
			}
		})
	}
}

func TestCalculatePanelSizes_TooSmall(t *testing.T) {
	tests := []struct {
		name    string
		width   int
		height  int
		visible [3]bool
	}{
		{"zero width", 0, 50, [3]bool{true, true, true}},
		{"below minimum width", 79, 50, [3]bool{true, true, true}},
		{"zero visible panels", 120, 50, [3]bool{false, false, false}},
		{"tiny height", 120, 5, [3]bool{true, true, true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sizes := CalculatePanelSizes(tt.width, tt.height, tt.visible)
			if !sizes.TooSmall {
				t.Errorf("expected TooSmall=true for width=%d, height=%d, visible=%v", tt.width, tt.height, tt.visible)
			}
		})
	}
}

func TestCalculatePanelSizes_SinglePanel(t *testing.T) {
	tests := []struct {
		name    string
		visible [3]bool
		wantL   bool
		wantC   bool
		wantR   bool
	}{
		{"left only", [3]bool{true, false, false}, true, false, false},
		{"center only", [3]bool{false, true, false}, false, true, false},
		{"right only", [3]bool{false, false, true}, false, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sizes := CalculatePanelSizes(120, 50, tt.visible)
			if sizes.TooSmall {
				t.Fatal("unexpected TooSmall")
			}
			if (sizes.LeftWidth > 0) != tt.wantL {
				t.Errorf("LeftWidth=%d, wantVisible=%v", sizes.LeftWidth, tt.wantL)
			}
			if (sizes.CenterWidth > 0) != tt.wantC {
				t.Errorf("CenterWidth=%d, wantVisible=%v", sizes.CenterWidth, tt.wantC)
			}
			if (sizes.RightWidth > 0) != tt.wantR {
				t.Errorf("RightWidth=%d, wantVisible=%v", sizes.RightWidth, tt.wantR)
			}
			// Single panel gets full width
			total := sizes.LeftWidth + sizes.CenterWidth + sizes.RightWidth
			if total != 120 {
				t.Errorf("total width = %d, want 120", total)
			}
		})
	}
}

func TestCalculatePanelSizes_ThreePanels(t *testing.T) {
	visible := [3]bool{true, true, true}
	sizes := CalculatePanelSizes(200, 50, visible)
	if sizes.TooSmall {
		t.Fatal("unexpected TooSmall")
	}
	if sizes.LeftWidth < minLeftWidth {
		t.Errorf("LeftWidth=%d < minLeftWidth=%d", sizes.LeftWidth, minLeftWidth)
	}
	if sizes.CenterWidth < minCenterWidth {
		t.Errorf("CenterWidth=%d < minCenterWidth=%d", sizes.CenterWidth, minCenterWidth)
	}
	if sizes.RightWidth < minRightWidth {
		t.Errorf("RightWidth=%d < minRightWidth=%d", sizes.RightWidth, minRightWidth)
	}
	total := sizes.LeftWidth + sizes.CenterWidth + sizes.RightWidth
	if total != 200 {
		t.Errorf("total width = %d, want 200", total)
	}
	if sizes.PanelHeight != 49 {
		t.Errorf("PanelHeight = %d, want 49 (50 - statusBarHeight)", sizes.PanelHeight)
	}
}

func TestCalculatePanelSizes_TwoPanels(t *testing.T) {
	tests := []struct {
		name    string
		visible [3]bool
	}{
		{"left+center", [3]bool{true, true, false}},
		{"left+right", [3]bool{true, false, true}},
		{"center+right", [3]bool{false, true, true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sizes := CalculatePanelSizes(150, 40, tt.visible)
			if sizes.TooSmall {
				t.Fatal("unexpected TooSmall")
			}
			total := sizes.LeftWidth + sizes.CenterWidth + sizes.RightWidth
			if total != 150 {
				t.Errorf("total width = %d, want 150", total)
			}
		})
	}
}
