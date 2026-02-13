package ui

import "github.com/charmbracelet/lipgloss"

// Panel border colors
var (
	focusedBorderColor   = lipgloss.Color("62")  // bright purple/blue
	unfocusedBorderColor = lipgloss.Color("240") // dim gray
	insertModeBorderColor = lipgloss.Color("42") // green
)

// Diff colors
var (
	diffAddedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	diffRemovedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	diffHunkHeaderStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("33")).Bold(true)
	diffFileHeaderStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("220")).
		Bold(true)
)

// Status bar
var (
	statusBarStyle = lipgloss.NewStyle().
		Background(lipgloss.Color("236")).
		Foreground(lipgloss.Color("252"))
	statusBarAccentStyle = lipgloss.NewStyle().
		Background(lipgloss.Color("236")).
		Foreground(lipgloss.Color("62")).
		Bold(true)
)

// Chat styles
var (
	chatUserStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("33")).
		Bold(true)
	chatAssistantStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("42")).
		Bold(true)
)

// Selected hunk highlight
var diffSelectedBg = lipgloss.Color("236")

// Focused hunk indicator
var diffFocusedHunkStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("99")).Bold(true)

// PR list styles
var (
	prTitleStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	prMetaStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
)

// Panel style builders
func panelStyle(focused bool, insertMode bool, width, height int) lipgloss.Style {
	borderColor := unfocusedBorderColor
	if focused {
		borderColor = focusedBorderColor
		if insertMode {
			borderColor = insertModeBorderColor
		}
	}

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Width(width).
		Height(height)
}

func panelHeaderStyle(focused bool) lipgloss.Style {
	if focused {
		return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("252"))
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
}

// Tab styles
func activeTabStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("252")).
		Background(lipgloss.Color("62")).
		Padding(0, 1)
}

func inactiveTabStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("244")).
		Padding(0, 1)
}

// Mode badge styles
func normalModeBadge() string {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("244")).
		Background(lipgloss.Color("238")).
		Padding(0, 1).
		Render("NORMAL")
}

func insertModeBadge() string {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("0")).
		Background(lipgloss.Color("42")).
		Padding(0, 1).
		Render("INSERT")
}
