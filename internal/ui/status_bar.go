package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// StatusBarModel renders the bottom status bar.
type StatusBarModel struct {
	width   int
	focused Panel
	mode    AppMode
}

func NewStatusBarModel() StatusBarModel {
	return StatusBarModel{}
}

func (m *StatusBarModel) SetWidth(width int) {
	m.width = width
}

func (m *StatusBarModel) SetState(focused Panel, mode AppMode) {
	m.focused = focused
	m.mode = mode
}

func (m StatusBarModel) View() string {
	leftHints := m.keyHints()
	rightInfo := m.contextInfo()

	leftRendered := statusBarAccentStyle.Render(leftHints)
	rightRendered := statusBarStyle.Render(rightInfo)

	leftWidth := lipgloss.Width(leftRendered)
	rightWidth := lipgloss.Width(rightRendered)
	padding := m.width - leftWidth - rightWidth
	if padding < 0 {
		padding = 0
	}

	bar := leftRendered +
		statusBarStyle.Render(strings.Repeat(" ", padding)) +
		rightRendered

	return statusBarStyle.Width(m.width).Render(bar)
}

func (m StatusBarModel) keyHints() string {
	if m.mode == ModeInsert {
		return " [Enter]send [Esc]exit insert"
	}

	switch m.focused {
	case PanelLeft:
		return " [j/k]move [Enter]select [/]filter [Tab]panel [?]help [q]quit"
	case PanelCenter:
		return " [j/k]scroll [n/N]file [Ctrl+d/u]page [Tab]panel [?]help [q]quit"
	case PanelRight:
		return " [j/k]scroll [i]insert [Tab]panel [?]help [q]quit"
	default:
		return " [Tab]panel [?]help [q]quit"
	}
}

func (m StatusBarModel) contextInfo() string {
	modeStr := ""
	switch m.mode {
	case ModeInsert:
		modeStr = " INSERT "
	case ModeOverlay:
		modeStr = " OVERLAY "
	default:
		modeStr = " NAV "
	}

	return modeStr + "PR #1234 main "
}
