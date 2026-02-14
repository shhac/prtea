package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// HelpOverlayModel renders a centered help overlay with keybinding reference.
type HelpOverlayModel struct {
	viewport viewport.Model
	width    int
	height   int
	visible  bool
	context  Panel // which panel was focused when help opened
	ready    bool
}

// HelpClosedMsg is sent when the help overlay is dismissed.
type HelpClosedMsg struct{}

func NewHelpOverlayModel() HelpOverlayModel {
	return HelpOverlayModel{}
}

// Show makes the overlay visible and sets the context panel.
func (m *HelpOverlayModel) Show(context Panel) {
	m.visible = true
	m.context = context
	m.refreshContent()
}

// Hide dismisses the overlay.
func (m *HelpOverlayModel) Hide() {
	m.visible = false
}

// IsVisible returns whether the overlay is currently shown.
func (m HelpOverlayModel) IsVisible() bool {
	return m.visible
}

// SetSize updates the overlay dimensions and rebuilds the viewport.
func (m *HelpOverlayModel) SetSize(termWidth, termHeight int) {
	m.width = termWidth
	m.height = termHeight

	innerW, innerH := m.innerDimensions()
	if !m.ready {
		m.viewport = viewport.New(innerW, innerH)
		m.ready = true
	} else {
		m.viewport.Width = innerW
		m.viewport.Height = innerH
	}
	m.refreshContent()
}

func (m HelpOverlayModel) Update(msg tea.Msg) (HelpOverlayModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, GlobalKeys.Help):
			m.Hide()
			return m, func() tea.Msg { return HelpClosedMsg{} }
		case msg.String() == "esc":
			m.Hide()
			return m, func() tea.Msg { return HelpClosedMsg{} }
		case msg.String() == "q":
			m.Hide()
			return m, func() tea.Msg { return HelpClosedMsg{} }
		default:
			// Scroll the viewport with j/k/arrows
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd
		}
	}
	return m, nil
}

func (m HelpOverlayModel) View() string {
	if !m.visible {
		return ""
	}

	overlayW, overlayH := m.overlayDimensions()

	var content string
	if m.ready {
		content = m.viewport.View()
	}

	// Build the overlay box
	title := helpTitleStyle.Render(" Keyboard Shortcuts ")
	footer := helpFooterStyle.Render(" ? / Esc to close ")

	innerW := overlayW - 4 // account for border + padding
	if innerW < 1 {
		innerW = 1
	}

	// Center the title and footer
	titleLine := lipgloss.PlaceHorizontal(innerW, lipgloss.Center, title)
	footerLine := lipgloss.PlaceHorizontal(innerW, lipgloss.Center, footer)

	boxParts := []string{titleLine, "", content}
	if indicator := scrollIndicator(m.viewport, innerW); indicator != "" {
		boxParts = append(boxParts, indicator)
	} else {
		boxParts = append(boxParts, "")
	}
	boxParts = append(boxParts, footerLine)
	box := lipgloss.JoinVertical(lipgloss.Left, boxParts...)

	overlayStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Padding(0, 1).
		Width(overlayW - 2).   // account for border
		Height(overlayH - 2)

	rendered := overlayStyle.Render(box)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, rendered)
}

// overlayDimensions returns the outer dimensions of the overlay box.
func (m HelpOverlayModel) overlayDimensions() (width, height int) {
	width = int(float64(m.width) * 0.65)
	height = int(float64(m.height) * 0.75)
	if width < 50 {
		width = min(50, m.width)
	}
	if height < 15 {
		height = min(15, m.height)
	}
	return width, height
}

// innerDimensions returns the viewport dimensions inside the overlay box.
func (m HelpOverlayModel) innerDimensions() (width, height int) {
	ow, oh := m.overlayDimensions()
	// Subtract border (2), padding (2), title line (2), footer line (2), blank lines (2)
	width = ow - 6
	height = oh - 10
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}
	return width, height
}

func (m *HelpOverlayModel) refreshContent() {
	if !m.ready {
		return
	}
	content := m.renderHelpContent()
	m.viewport.SetContent(content)
	m.viewport.GotoTop()
}

func (m HelpOverlayModel) renderHelpContent() string {
	innerW, _ := m.innerDimensions()

	var b strings.Builder

	sections := []struct {
		title string
		panel Panel
		match bool // whether this section matches current context
		keys  []helpEntry
	}{
		{
			title: "Global",
			panel: -1,
			match: false,
			keys: []helpEntry{
				{"Tab / Shift+Tab", "Switch panels"},
				{"1 / 2 / 3", "Jump to panel"},
				{"[ / \\ / ]", "Toggle left/center/right panel"},
				{"z", "Zoom focused panel"},
				{"r", "Refresh (PR list / selected PR)"},
				{"a", "Analyze PR"},
				{"A", "Approve PR"},
				{"X", "Close PR"},
				{"o", "Open in browser"},
				{"?", "Toggle this help"},
				{"q", "Quit"},
			},
		},
		{
			title: "PR List",
			panel: PanelLeft,
			match: m.context == PanelLeft,
			keys: []helpEntry{
				{"h / l", "Prev/next tab"},
				{"j / k", "Move up/down"},
				{"Space", "Select PR"},
				{"Enter", "Select PR + focus diff"},
				{"/", "Filter"},
			},
		},
		{
			title: "Diff Viewer",
			panel: PanelCenter,
			match: m.context == PanelCenter,
			keys: []helpEntry{
				{"h / l", "Prev/next tab"},
				{"j / k", "Scroll up/down"},
				{"Ctrl+d / Ctrl+u", "Half page down/up"},
				{"n / N", "Next/prev hunk"},
				{"g / G", "Jump to top/bottom"},
				{"s / Space", "Select/deselect hunk"},
				{"Enter", "Select hunk + focus chat"},
				{"S", "Select/deselect file hunks"},
				{"c", "Clear selection"},
			},
		},
		{
			title: "Chat (Normal)",
			panel: PanelRight,
			match: m.context == PanelRight,
			keys: []helpEntry{
				{"h / l", "Prev/next tab"},
				{"j / k", "Scroll history"},
				{"Enter", "Enter insert mode"},
			},
		},
		{
			title: "Chat (Insert)",
			panel: PanelRight,
			match: false,
			keys: []helpEntry{
				{"Enter", "Send message"},
				{"Esc", "Exit insert mode"},
			},
		},
	}

	for i, section := range sections {
		if i > 0 {
			b.WriteString("\n\n")
		}

		titleStr := section.title
		if section.match {
			titleStr += " (current)"
		}

		if section.match {
			b.WriteString(helpSectionActiveStyle.Render(titleStr))
		} else {
			b.WriteString(helpSectionStyle.Render(titleStr))
		}
		b.WriteString("\n")

		// Divider line under the section title
		divLen := min(lipgloss.Width(titleStr)+2, innerW)
		if section.match {
			b.WriteString(helpSectionActiveStyle.Render(strings.Repeat("─", divLen)))
		} else {
			b.WriteString(helpDividerStyle.Render(strings.Repeat("─", divLen)))
		}
		b.WriteString("\n")

		for _, entry := range section.keys {
			keyCol := helpKeyStyle.Render(padRight(entry.key, 20))
			descCol := helpDescStyle.Render(entry.desc)
			b.WriteString(keyCol + descCol + "\n")
		}
	}

	return b.String()
}

type helpEntry struct {
	key  string
	desc string
}

func padRight(s string, width int) string {
	if lipgloss.Width(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-lipgloss.Width(s))
}

// Help overlay styles
var (
	helpTitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("252")).
			Background(lipgloss.Color("62")).
			Padding(0, 1)

	helpFooterStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("244")).
			Italic(true)

	helpSectionStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("33"))

	helpSectionActiveStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("42"))

	helpDividerStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("240"))

	helpKeyStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("214"))

	helpDescStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("252"))
)
