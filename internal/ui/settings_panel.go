package ui

import (
	"fmt"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/shhac/prtea/internal/config"
)

// settingKind describes the type of a setting entry.
type settingKind int

const (
	settingToggle settingKind = iota
	settingNumber
)

// settingItem describes a single configurable setting.
type settingItem struct {
	label   string
	desc    string
	kind    settingKind
	min     int // for settingNumber
	max     int // for settingNumber
	step    int // for settingNumber
	unitSec bool // display seconds (value stored as ms)
}

var settingsSchema = []settingItem{
	{label: "Polling", desc: "Auto-refresh PR list in the background", kind: settingToggle},
	{label: "Poll Interval", desc: "Seconds between background refreshes", kind: settingNumber, min: 10, max: 600, step: 10, unitSec: true},
	{label: "Notifications", desc: "Desktop notifications for new activity", kind: settingToggle},
	{label: "Claude Timeout", desc: "Seconds before Claude analysis times out", kind: settingNumber, min: 30, max: 600, step: 30, unitSec: true},
}

// SettingsModel manages the settings overlay.
type SettingsModel struct {
	cfg      *config.Config
	width    int
	height   int
	visible  bool
	cursor   int
	dirty    bool // whether settings have been modified
}

// NewSettingsModel creates a settings model.
func NewSettingsModel() SettingsModel {
	return SettingsModel{}
}

// Show makes the settings overlay visible with the given config.
func (m *SettingsModel) Show(cfg *config.Config) {
	m.visible = true
	m.cursor = 0
	m.dirty = false
	// Work on a copy so we can save atomically on close
	c := *cfg
	m.cfg = &c
}

// Hide dismisses the settings overlay.
func (m *SettingsModel) Hide() {
	m.visible = false
}

// IsVisible returns whether the settings overlay is currently shown.
func (m SettingsModel) IsVisible() bool {
	return m.visible
}

// SetSize updates the overlay dimensions.
func (m *SettingsModel) SetSize(termWidth, termHeight int) {
	m.width = termWidth
	m.height = termHeight
}

// Config returns the current (possibly modified) config.
func (m SettingsModel) Config() *config.Config {
	return m.cfg
}

// IsDirty returns whether settings have been modified.
func (m SettingsModel) IsDirty() bool {
	return m.dirty
}

// Update handles key events in the settings overlay.
func (m SettingsModel) Update(msg tea.Msg) (SettingsModel, tea.Cmd) {
	kmsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	switch {
	case kmsg.String() == "esc" || kmsg.String() == "q":
		m.Hide()
		var cmds []tea.Cmd
		cmds = append(cmds, func() tea.Msg { return SettingsClosedMsg{} })
		if m.dirty {
			cmds = append(cmds, func() tea.Msg { return ConfigChangedMsg{} })
		}
		return m, tea.Batch(cmds...)

	case key.Matches(kmsg, GlobalKeys.Help):
		m.Hide()
		var cmds []tea.Cmd
		cmds = append(cmds, func() tea.Msg { return SettingsClosedMsg{} })
		if m.dirty {
			cmds = append(cmds, func() tea.Msg { return ConfigChangedMsg{} })
		}
		return m, tea.Batch(cmds...)

	case kmsg.String() == "j" || kmsg.String() == "down":
		if m.cursor < len(settingsSchema)-1 {
			m.cursor++
		}
		return m, nil

	case kmsg.String() == "k" || kmsg.String() == "up":
		if m.cursor > 0 {
			m.cursor--
		}
		return m, nil

	case kmsg.String() == "enter" || kmsg.String() == " ":
		m.toggleOrIncrement()
		return m, nil

	case kmsg.String() == "l" || kmsg.String() == "right" || kmsg.String() == "+":
		m.adjustNumber(1)
		return m, nil

	case kmsg.String() == "h" || kmsg.String() == "left" || kmsg.String() == "-":
		m.adjustNumber(-1)
		return m, nil
	}

	return m, nil
}

// toggleOrIncrement toggles a boolean setting or increments a number setting.
func (m *SettingsModel) toggleOrIncrement() {
	item := settingsSchema[m.cursor]
	switch item.kind {
	case settingToggle:
		m.setToggle(m.cursor, !m.getToggle(m.cursor))
		m.dirty = true
	case settingNumber:
		m.adjustNumber(1)
	}
}

// adjustNumber changes a number setting by the given direction (-1 or +1).
func (m *SettingsModel) adjustNumber(dir int) {
	item := settingsSchema[m.cursor]
	if item.kind != settingNumber {
		return
	}
	val := m.getNumber(m.cursor)
	step := item.step
	if item.unitSec {
		step *= 1000
	}
	val += dir * step
	minVal := item.min
	maxVal := item.max
	if item.unitSec {
		minVal *= 1000
		maxVal *= 1000
	}
	if val < minVal {
		val = minVal
	}
	if val > maxVal {
		val = maxVal
	}
	m.setNumber(m.cursor, val)
	m.dirty = true
}

// getToggle returns the boolean value for a toggle setting by index.
func (m SettingsModel) getToggle(idx int) bool {
	switch settingsSchema[idx].label {
	case "Polling":
		return m.cfg.PollEnabled
	case "Notifications":
		return m.cfg.NotificationsEnabled
	}
	return false
}

// setToggle sets the boolean value for a toggle setting by index.
func (m *SettingsModel) setToggle(idx int, val bool) {
	switch settingsSchema[idx].label {
	case "Polling":
		m.cfg.PollEnabled = val
	case "Notifications":
		m.cfg.NotificationsEnabled = val
	}
}

// getNumber returns the numeric value for a number setting by index.
func (m SettingsModel) getNumber(idx int) int {
	switch settingsSchema[idx].label {
	case "Poll Interval":
		return m.cfg.PollInterval
	case "Claude Timeout":
		return m.cfg.ClaudeTimeout
	}
	return 0
}

// setNumber sets the numeric value for a number setting by index.
func (m *SettingsModel) setNumber(idx int, val int) {
	switch settingsSchema[idx].label {
	case "Poll Interval":
		m.cfg.PollInterval = val
	case "Claude Timeout":
		m.cfg.ClaudeTimeout = val
	}
}

// View renders the settings overlay.
func (m SettingsModel) View() string {
	if !m.visible {
		return ""
	}

	overlayW, overlayH := m.overlayDimensions()
	innerW := overlayW - 6 // border + padding
	if innerW < 1 {
		innerW = 1
	}

	// Title
	title := settingsTitleStyle.Render(" Settings ")
	titleLine := lipgloss.PlaceHorizontal(innerW, lipgloss.Center, title)

	// Footer
	footer := settingsFooterStyle.Render(" j/k navigate · Enter/Space toggle · h/l adjust · Esc close ")
	footerLine := lipgloss.PlaceHorizontal(innerW, lipgloss.Center, footer)

	// Setting rows
	var rows []string
	for i, item := range settingsSchema {
		rows = append(rows, m.renderSettingRow(i, item, innerW))
	}

	// Dirty indicator
	if m.dirty {
		rows = append(rows, "")
		rows = append(rows, settingsDirtyStyle.Render("  Changes will be saved on close"))
	}

	content := lipgloss.JoinVertical(lipgloss.Left, rows...)

	box := lipgloss.JoinVertical(lipgloss.Left, titleLine, "", content, "", footerLine)

	overlayStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Padding(0, 1).
		Width(overlayW - 2).
		Height(overlayH - 2)

	rendered := overlayStyle.Render(box)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, rendered)
}

// renderSettingRow renders a single setting row.
func (m SettingsModel) renderSettingRow(idx int, item settingItem, width int) string {
	isFocused := idx == m.cursor

	marker := "  "
	if isFocused {
		marker = settingsMarkerStyle.Render("▸ ")
	}

	labelStyle := settingsLabelStyle
	if isFocused {
		labelStyle = settingsLabelFocusedStyle
	}

	label := labelStyle.Render(padRight(item.label, 18))

	var value string
	switch item.kind {
	case settingToggle:
		on := m.getToggle(idx)
		if on {
			value = settingsOnStyle.Render("● ON ")
		} else {
			value = settingsOffStyle.Render("○ OFF")
		}
	case settingNumber:
		raw := m.getNumber(idx)
		display := raw
		unit := "ms"
		if item.unitSec {
			display = raw / 1000
			unit = "s"
		}
		numStr := fmt.Sprintf("%d%s", display, unit)
		if isFocused {
			value = settingsNumberFocusedStyle.Render(fmt.Sprintf("◂ %s ▸", numStr))
		} else {
			value = settingsNumberStyle.Render(fmt.Sprintf("  %s  ", numStr))
		}
	}

	desc := settingsDescStyle.Render(item.desc)

	row := marker + label + value + "  " + desc
	_ = width
	return row
}

// overlayDimensions returns the outer dimensions of the settings overlay box.
func (m SettingsModel) overlayDimensions() (width, height int) {
	width = int(float64(m.width) * 0.55)
	height = len(settingsSchema)*2 + 10 // rows + chrome
	if width < 60 {
		width = min(60, m.width)
	}
	if height < 12 {
		height = 12
	}
	if height > m.height-2 {
		height = m.height - 2
	}
	return width, height
}

// Settings overlay styles
var (
	settingsTitleStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("252")).
		Background(lipgloss.Color("62")).
		Padding(0, 1)

	settingsFooterStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("244")).
		Italic(true)

	settingsMarkerStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("42"))

	settingsLabelStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("252"))

	settingsLabelFocusedStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("42")).
		Bold(true)

	settingsOnStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("42")).
		Bold(true)

	settingsOffStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("244"))

	settingsNumberStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("214"))

	settingsNumberFocusedStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("214")).
		Bold(true)

	settingsDescStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("244")).
		Italic(true)

	settingsDirtyStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("214")).
		Italic(true)
)
