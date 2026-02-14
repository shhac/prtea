package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"
)

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

// Focused hunk gutter marker (▎ in accent color)
var diffFocusGutterStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("62"))

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

// newLoadingSpinner creates a consistently styled spinner for loading states.
func newLoadingSpinner() spinner.Model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("62"))
	return s
}

// renderEmptyState renders a consistent empty state message with optional action hint.
func renderEmptyState(message, hint string) string {
	msg := lipgloss.NewStyle().
		Foreground(lipgloss.Color("244")).
		Padding(1, 2).
		Render("— " + message)
	if hint == "" {
		return msg
	}
	h := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		Italic(true).
		Padding(0, 2).
		Render(hint)
	return lipgloss.JoinVertical(lipgloss.Left, msg, h)
}

// renderErrorWithHint renders a consistent error message with retry hint.
func renderErrorWithHint(errMsg, hint string) string {
	msg := lipgloss.NewStyle().
		Foreground(lipgloss.Color("196")).
		Bold(true).
		Padding(1, 2).
		Render(errMsg)
	if hint == "" {
		return msg
	}
	h := lipgloss.NewStyle().
		Foreground(lipgloss.Color("244")).
		Padding(0, 2).
		Render(hint)
	return lipgloss.JoinVertical(lipgloss.Left, msg, h)
}

// formatUserError converts raw error strings into user-friendly messages.
func formatUserError(err string) string {
	lower := strings.ToLower(err)
	switch {
	case strings.Contains(lower, "gh cli not found"):
		return "GitHub CLI (gh) not found.\nInstall from https://cli.github.com"
	case strings.Contains(lower, "not authenticated") || strings.Contains(lower, "auth login"):
		return "Not authenticated with GitHub.\nRun 'gh auth login' in your terminal."
	case strings.Contains(lower, "rate limit"):
		return "GitHub rate limit reached.\nWait a moment and try again."
	case strings.Contains(lower, "timeout") || strings.Contains(lower, "deadline exceeded"):
		return "Request timed out.\nCheck your connection and try again."
	case strings.Contains(lower, "no such host") || strings.Contains(lower, "connection refused"):
		return "Network error.\nCheck your internet connection."
	default:
		return err
	}
}

// Scroll indicator style
var scrollIndicatorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))

// scrollIndicator returns a scroll position line for a viewport.
// Returns "" if all content fits within the viewport (no scrolling needed).
func scrollIndicator(vp viewport.Model, width int) string {
	if vp.TotalLineCount() <= vp.Height {
		return ""
	}
	pct := int(vp.ScrollPercent() * 100)
	var label string
	switch {
	case vp.AtTop():
		label = fmt.Sprintf("%d%% ▼", pct)
	case vp.AtBottom():
		label = fmt.Sprintf("▲ %d%%", pct)
	default:
		label = fmt.Sprintf("▲ %d%% ▼", pct)
	}
	return scrollIndicatorStyle.Render(
		lipgloss.PlaceHorizontal(width, lipgloss.Right, label),
	)
}
