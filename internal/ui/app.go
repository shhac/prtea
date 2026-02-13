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

	// Layout state
	focused        Panel
	width          int
	height         int
	rightCollapsed bool

	// Mode
	mode AppMode
}

// NewApp creates a new App model with default state.
func NewApp() App {
	return App{
		prList:     NewPRListModel(),
		diffViewer: NewDiffViewerModel(),
		chatPanel:  NewChatPanelModel(),
		statusBar:  NewStatusBarModel(),
		focused:    PanelLeft,
		mode:       ModeNavigation,
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
		m.recalcLayout()
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
		// Future: trigger diff loading
		return m, nil

	case tea.KeyMsg:
		// In insert mode, only Esc is handled globally (via chat panel)
		if m.mode == ModeInsert {
			return m.updateChatPanel(msg)
		}

		// Global key handling in navigation mode
		switch {
		case key.Matches(msg, GlobalKeys.Quit):
			return m, tea.Quit

		case key.Matches(msg, GlobalKeys.Tab):
			m.focusPanel(m.focused.Next())
			return m, nil

		case key.Matches(msg, GlobalKeys.ShiftTab):
			m.focusPanel(m.focused.Prev())
			return m, nil

		case key.Matches(msg, GlobalKeys.Panel1):
			m.focusPanel(PanelLeft)
			return m, nil

		case key.Matches(msg, GlobalKeys.Panel2):
			m.focusPanel(PanelCenter)
			return m, nil

		case key.Matches(msg, GlobalKeys.Panel3):
			if m.rightCollapsed {
				m.rightCollapsed = false
				m.recalcLayout()
			}
			m.focusPanel(PanelRight)
			return m, nil

		case key.Matches(msg, GlobalKeys.ToggleRight):
			m.rightCollapsed = !m.rightCollapsed
			if m.rightCollapsed && m.focused == PanelRight {
				m.focusPanel(PanelCenter)
			}
			m.recalcLayout()
			return m, nil
		}

		// Delegate to focused panel
		return m.updateFocusedPanel(msg)
	}

	return m, nil
}

func (m App) View() string {
	sizes := CalculatePanelSizes(m.width, m.height, m.rightCollapsed)

	if sizes.TooSmall {
		msg := lipgloss.NewStyle().
			Foreground(lipgloss.Color("196")).
			Bold(true).
			Render("Terminal too small. Please resize to at least 80×10.")
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, msg)
	}

	// Render panels
	left := m.prList.View()
	center := m.diffViewer.View()

	var panels string
	if sizes.RightWidth > 0 {
		right := m.chatPanel.View()
		panels = lipgloss.JoinHorizontal(lipgloss.Top, left, center, right)
	} else {
		panels = lipgloss.JoinHorizontal(lipgloss.Top, left, center)
	}

	// Render status bar
	bar := m.statusBar.View()

	return lipgloss.JoinVertical(lipgloss.Left, panels, bar)
}

func (m *App) focusPanel(p Panel) {
	// Skip right panel if collapsed
	if p == PanelRight && m.rightCollapsed {
		p = p.Next()
	}
	m.focused = p
	m.prList.SetFocused(p == PanelLeft)
	m.diffViewer.SetFocused(p == PanelCenter)
	m.chatPanel.SetFocused(p == PanelRight)
	m.statusBar.SetState(m.focused, m.mode)
}

func (m *App) recalcLayout() {
	sizes := CalculatePanelSizes(m.width, m.height, m.rightCollapsed)
	if sizes.TooSmall {
		return
	}

	m.prList.SetSize(sizes.LeftWidth, sizes.PanelHeight)
	m.diffViewer.SetSize(sizes.CenterWidth, sizes.PanelHeight)
	if sizes.RightWidth > 0 {
		m.chatPanel.SetSize(sizes.RightWidth, sizes.PanelHeight)
	}
	m.statusBar.SetWidth(m.width)
	m.statusBar.SetState(m.focused, m.mode)
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
