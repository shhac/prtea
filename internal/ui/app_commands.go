package ui

// Command execution: the App-side behavior behind the command palette and
// the global keybindings that delegate to it. The registry itself lives in
// command_mode.go.

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// executeCommand runs a registry command by name. The registry is the single
// source of command behavior for both the palette and global keybindings.
func (m App) executeCommand(name string) (tea.Model, tea.Cmd) {
	if cmd := findCommand(name); cmd != nil {
		return cmd.Run(m)
	}
	return m, func() tea.Msg { return CommandNotFoundMsg{Input: name} }
}

// openSelectedPRInBrowser opens the selected PR's page, if any.
func (m App) openSelectedPRInBrowser() (tea.Model, tea.Cmd) {
	if m.session != nil && m.session.HTMLURL != "" {
		return m, openBrowserCmd(m.session.HTMLURL)
	}
	return m, nil
}

// openHelp shows the help overlay for the focused panel.
func (m App) openHelp() (tea.Model, tea.Cmd) {
	m.setMode(ModeOverlay)
	m.helpOverlay.SetSize(m.width, m.height)
	m.helpOverlay.Show(m.focused)
	return m, nil
}

// clearHunkSelection deselects all diff hunks.
func (m App) clearHunkSelection() (tea.Model, tea.Cmd) {
	if m.diffViewer.activeTab == TabDiff && len(m.diffViewer.selectedHunks) > 0 {
		for idx := range m.diffViewer.selectedHunks {
			m.diffViewer.markHunkDirty(idx)
		}
		m.diffViewer.selectedHunks = nil
		m.diffViewer.refreshContent()
	}
	return m, nil
}

// enterDiffCommentMode starts inline-comment input at the diff cursor.
func (m App) enterDiffCommentMode() (tea.Model, tea.Cmd) {
	if m.focused != PanelCenter || m.diffViewer.activeTab != TabDiff || len(m.diffViewer.hunks) == 0 {
		return m, m.statusBar.SetTemporaryMessage("Focus the diff viewer to add comments", 2*time.Second)
	}
	return m, m.diffViewer.EnterCommentMode()
}

// focusReviewTab jumps to the review submission form.
func (m App) focusReviewTab() (tea.Model, tea.Cmd) {
	m.chatPanel.SetActiveTab(ChatTabReview)
	m.showAndFocusPanel(PanelRight)
	return m, nil
}

// refreshFocused refreshes the PR list or the selected PR, whichever is focused.
func (m App) refreshFocused() (tea.Model, tea.Cmd) {
	if m.focused == PanelLeft {
		return m.refreshPRList()
	}
	return m.refreshSelectedPR()
}

// renderCommandOverlay composites the command palette at the bottom of the base view.
func (m App) renderCommandOverlay(base string) string {
	overlay := m.commandMode.View()
	if overlay == "" {
		return base
	}

	overlayLines := strings.Split(overlay, "\n")
	baseLines := strings.Split(base, "\n")

	overlayH := len(overlayLines)
	if overlayH > len(baseLines) {
		overlayH = len(baseLines)
	}

	start := len(baseLines) - overlayH
	for i := 0; i < len(overlayLines) && start+i < len(baseLines); i++ {
		line := overlayLines[i]
		// Pad to full width to cover base content underneath
		lineWidth := lipgloss.Width(line)
		if lineWidth < m.width {
			line += strings.Repeat(" ", m.width-lineWidth)
		}
		baseLines[start+i] = line
	}

	return strings.Join(baseLines, "\n")
}
