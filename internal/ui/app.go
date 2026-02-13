package ui

import (
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// App is the root Bubbletea model for the PR dashboard.
type App struct {
	// Panel models
	prList     PRListModel
	diffViewer DiffViewerModel
	chatPanel  ChatPanelModel
	statusBar  StatusBarModel

	// Overlays
	helpOverlay HelpOverlayModel

	// Layout state
	focused        Panel
	width          int
	height         int
	panelVisible   [3]bool // which panels are currently visible
	zoomed         bool    // zoom mode: only focused panel shown
	preZoomVisible [3]bool // saved visibility before zoom
	initialized    bool    // whether first WindowSizeMsg has been processed

	// Mode
	mode AppMode
}

// NewApp creates a new App model with default state.
func NewApp() App {
	return App{
		prList:       NewPRListModel(),
		diffViewer:   NewDiffViewerModel(),
		chatPanel:    NewChatPanelModel(),
		statusBar:    NewStatusBarModel(),
		helpOverlay:  NewHelpOverlayModel(),
		focused:      PanelLeft,
		panelVisible: [3]bool{true, true, true},
		mode:         ModeNavigation,
	}
}

func (m App) Init() tea.Cmd {
	return nil
}

func (m App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.helpOverlay.SetSize(m.width, m.height)
		// Auto-collapse right panel on first render if terminal is narrow
		if !m.initialized {
			m.initialized = true
			if m.width < collapseThreshold {
				m.panelVisible[PanelRight] = false
				if m.focused == PanelRight {
					m.focusPanel(nextVisiblePanel(m.focused, m.panelVisible))
				}
			}
		}
		m.recalcLayout()
		return m, nil

	case HelpClosedMsg:
		m.mode = ModeNavigation
		m.statusBar.SetState(m.focused, m.mode)
		return m, nil

	case ModeChangedMsg:
		if msg.Mode == ChatModeInsert {
			m.mode = ModeInsert
		} else {
			m.mode = ModeNavigation
		}
		m.statusBar.SetState(m.focused, m.mode)
		return m, nil

	case PRSelectedMsg:
		m.statusBar.SetSelectedPR(msg.Number)
		// TODO: trigger diff loading for the selected PR
		return m, nil

	case tea.KeyMsg:
		// Overlay mode captures all keys
		if m.mode == ModeOverlay {
			var cmd tea.Cmd
			m.helpOverlay, cmd = m.helpOverlay.Update(msg)
			return m, cmd
		}

		// In insert mode, only Esc is handled globally (via chat panel)
		if m.mode == ModeInsert {
			return m.updateChatPanel(msg)
		}

		// Global key handling in navigation mode
		switch {
		case key.Matches(msg, GlobalKeys.Help):
			m.mode = ModeOverlay
			m.helpOverlay.SetSize(m.width, m.height)
			m.helpOverlay.Show(m.focused)
			m.statusBar.SetState(m.focused, m.mode)
			return m, nil

		case key.Matches(msg, GlobalKeys.Quit):
			return m, tea.Quit

		case key.Matches(msg, GlobalKeys.Tab):
			if m.zoomed {
				m.exitZoom()
				m.recalcLayout()
			}
			m.focusPanel(nextVisiblePanel(m.focused, m.panelVisible))
			return m, nil

		case key.Matches(msg, GlobalKeys.ShiftTab):
			if m.zoomed {
				m.exitZoom()
				m.recalcLayout()
			}
			m.focusPanel(prevVisiblePanel(m.focused, m.panelVisible))
			return m, nil

		case key.Matches(msg, GlobalKeys.Panel1):
			m.showAndFocusPanel(PanelLeft)
			return m, nil

		case key.Matches(msg, GlobalKeys.Panel2):
			m.showAndFocusPanel(PanelCenter)
			return m, nil

		case key.Matches(msg, GlobalKeys.Panel3):
			m.showAndFocusPanel(PanelRight)
			return m, nil

		case key.Matches(msg, GlobalKeys.ToggleLeft):
			if m.zoomed {
				m.exitZoom()
			}
			m.togglePanel(PanelLeft)
			return m, nil

		case key.Matches(msg, GlobalKeys.ToggleRight):
			if m.zoomed {
				m.exitZoom()
			}
			m.togglePanel(PanelRight)
			return m, nil

		case key.Matches(msg, GlobalKeys.Zoom):
			m.toggleZoom()
			return m, nil
		}

		// Delegate to focused panel
		return m.updateFocusedPanel(msg)
	}

	return m, nil
}

func (m App) View() string {
	sizes := CalculatePanelSizes(m.width, m.height, m.panelVisible)

	if sizes.TooSmall {
		msg := lipgloss.NewStyle().
			Foreground(lipgloss.Color("196")).
			Bold(true).
			Render("Terminal too small. Please resize to at least 80×10.")
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, msg)
	}

	var panelViews []string
	if sizes.LeftWidth > 0 {
		panelViews = append(panelViews, m.prList.View())
	}
	if sizes.CenterWidth > 0 {
		panelViews = append(panelViews, m.diffViewer.View())
	}
	if sizes.RightWidth > 0 {
		panelViews = append(panelViews, m.chatPanel.View())
	}

	panels := lipgloss.JoinHorizontal(lipgloss.Top, panelViews...)
	bar := m.statusBar.View()

	base := lipgloss.JoinVertical(lipgloss.Left, panels, bar)

	// Render help overlay on top if active
	if m.helpOverlay.IsVisible() {
		return m.helpOverlay.View()
	}

	return base
}

// focusPanel sets focus to the given panel. If the panel is hidden,
// focuses the next visible panel instead.
func (m *App) focusPanel(p Panel) {
	if !m.panelVisible[p] {
		p = nextVisiblePanel(p, m.panelVisible)
	}
	m.focused = p
	m.prList.SetFocused(p == PanelLeft)
	m.diffViewer.SetFocused(p == PanelCenter)
	m.chatPanel.SetFocused(p == PanelRight)
	m.statusBar.SetState(m.focused, m.mode)
}

func (m *App) recalcLayout() {
	sizes := CalculatePanelSizes(m.width, m.height, m.panelVisible)
	if sizes.TooSmall {
		return
	}

	if sizes.LeftWidth > 0 {
		m.prList.SetSize(sizes.LeftWidth, sizes.PanelHeight)
	}
	if sizes.CenterWidth > 0 {
		m.diffViewer.SetSize(sizes.CenterWidth, sizes.PanelHeight)
	}
	if sizes.RightWidth > 0 {
		m.chatPanel.SetSize(sizes.RightWidth, sizes.PanelHeight)
	}
	m.statusBar.SetWidth(m.width)
	m.statusBar.SetState(m.focused, m.mode)
}

// togglePanel shows or hides a panel. Prevents hiding the last visible panel.
func (m *App) togglePanel(p Panel) {
	if m.panelVisible[p] && visibleCount(m.panelVisible) <= 1 {
		return // can't hide the last visible panel
	}
	m.panelVisible[p] = !m.panelVisible[p]
	if !m.panelVisible[m.focused] {
		m.focusPanel(nextVisiblePanel(m.focused, m.panelVisible))
	}
	m.recalcLayout()
}

// toggleZoom enters or exits zoom mode. When zoomed, only the focused panel
// is visible at full width.
func (m *App) toggleZoom() {
	if m.zoomed {
		m.exitZoom()
	} else {
		m.preZoomVisible = m.panelVisible
		m.panelVisible = [3]bool{}
		m.panelVisible[m.focused] = true
		m.zoomed = true
	}
	m.recalcLayout()
}

// exitZoom restores the pre-zoom panel visibility.
func (m *App) exitZoom() {
	if !m.zoomed {
		return
	}
	m.panelVisible = m.preZoomVisible
	m.zoomed = false
}

// showAndFocusPanel ensures a panel is visible, exits zoom if active,
// and focuses the panel.
func (m *App) showAndFocusPanel(p Panel) {
	if m.zoomed {
		m.exitZoom()
	}
	if !m.panelVisible[p] {
		m.panelVisible[p] = true
	}
	m.focusPanel(p)
	m.recalcLayout()
}

func (m App) updateFocusedPanel(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch m.focused {
	case PanelLeft:
		m.prList, cmd = m.prList.Update(msg)
	case PanelCenter:
		m.diffViewer, cmd = m.diffViewer.Update(msg)
	case PanelRight:
		m.chatPanel, cmd = m.chatPanel.Update(msg)
	}
	return m, cmd
}

func (m App) updateChatPanel(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.chatPanel, cmd = m.chatPanel.Update(msg)
	return m, cmd
}
