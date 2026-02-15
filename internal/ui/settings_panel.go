package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/shhac/prtea/internal/config"
)

// settingKind describes the type of a setting entry.
type settingKind int

const (
	settingToggle  settingKind = iota
	settingNumber              // numeric with min/max/step
	settingSelect              // cycles through string options
	settingSection             // non-interactive section header
)

// settingItem describes a single configurable setting.
type settingItem struct {
	label   string
	desc    string
	kind    settingKind
	min     int      // for settingNumber
	max     int      // for settingNumber
	step    int      // for settingNumber
	unitSec bool     // display seconds (value stored as ms)
	unitMs  bool     // display milliseconds
	options []string // for settingSelect: display labels
	values  []string // for settingSelect: stored config values
}

// settingsSchema defines all settings grouped into sections.
var settingsSchema = []settingItem{
	// Layout
	{label: "Layout", kind: settingSection},
	{label: "Default PR Tab", desc: "Which tab to show on startup", kind: settingSelect,
		options: []string{"To Review", "My PRs"}, values: []string{"review", "mine"}},
	{label: "Collapse Right", desc: "Hide right panel on startup", kind: settingToggle},
	{label: "Auto-collapse Width", desc: "Terminal width to auto-hide panels", kind: settingNumber, min: 80, max: 200, step: 10},

	// Polling
	{label: "Polling", kind: settingSection},
	{label: "Enabled", desc: "Auto-refresh PR list in the background", kind: settingToggle},
	{label: "Interval", desc: "Seconds between background refreshes", kind: settingNumber, min: 10, max: 600, step: 10, unitSec: true},

	// Notifications
	{label: "Notifications", kind: settingSection},
	{label: "Enabled", desc: "Desktop notifications for new activity", kind: settingToggle},
	{label: "Batch Threshold", desc: "Summarize when more than N new PRs", kind: settingNumber, min: 1, max: 20, step: 1},

	// Fetching
	{label: "Fetching", kind: settingSection},
	{label: "PR Fetch Limit", desc: "Max PRs to fetch per query", kind: settingNumber, min: 10, max: 500, step: 10},

	// AI
	{label: "AI", kind: settingSection},
	{label: "Claude Timeout", desc: "Seconds before analysis times out", kind: settingNumber, min: 30, max: 600, step: 30, unitSec: true},
	{label: "Chat History", desc: "Max messages kept in chat context", kind: settingNumber, min: 4, max: 64, step: 4},
	{label: "Prompt Token Limit", desc: "Max tokens for prompt context", kind: settingNumber, min: 10000, max: 500000, step: 10000},
	{label: "Chat Max Turns", desc: "Max agentic turns per chat message", kind: settingNumber, min: 1, max: 10, step: 1},
	{label: "Analysis Max Turns", desc: "Max turns for full PR analysis", kind: settingNumber, min: 5, max: 100, step: 5},

	// Display
	{label: "Display", kind: settingSection},
	{label: "Render Refresh", desc: "Stream rendering interval", kind: settingNumber, min: 50, max: 1000, step: 50, unitMs: true},

	// Review
	{label: "Review", kind: settingSection},
	{label: "Default Action", desc: "Pre-selected review action", kind: settingSelect,
		options: []string{"Approve", "Comment", "Request Changes"}, values: []string{"approve", "comment", "request_changes"}},
}

// navigableItems returns indices of items that are not section headers.
func navigableItems() []int {
	var indices []int
	for i, item := range settingsSchema {
		if item.kind != settingSection {
			indices = append(indices, i)
		}
	}
	return indices
}

// SettingsModel manages the settings overlay.
type SettingsModel struct {
	cfg       *config.Config
	width     int
	height    int
	visible   bool
	cursor    int  // index into navigableItems
	dirty     bool // whether settings have been modified
	viewport  viewport.Model
	vpReady   bool
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
	m.refreshViewport()
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
	_, innerH := m.innerDimensions()
	innerW, _ := m.innerDimensions()
	if !m.vpReady {
		m.viewport = viewport.New(innerW, innerH)
		m.vpReady = true
	} else {
		m.viewport.Width = innerW
		m.viewport.Height = innerH
	}
	m.refreshViewport()
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

	nav := navigableItems()

	switch {
	case kmsg.String() == "esc" || kmsg.String() == "q":
		return m.close()

	case key.Matches(kmsg, GlobalKeys.Help):
		return m.close()

	case kmsg.String() == "j" || kmsg.String() == "down":
		if m.cursor < len(nav)-1 {
			m.cursor++
			m.ensureVisible()
		}
		m.refreshViewport()
		return m, nil

	case kmsg.String() == "k" || kmsg.String() == "up":
		if m.cursor > 0 {
			m.cursor--
			m.ensureVisible()
		}
		m.refreshViewport()
		return m, nil

	case kmsg.String() == "enter" || kmsg.String() == " ":
		m.toggleOrCycle()
		m.refreshViewport()
		return m, nil

	case kmsg.String() == "l" || kmsg.String() == "right" || kmsg.String() == "+":
		m.adjust(1)
		m.refreshViewport()
		return m, nil

	case kmsg.String() == "h" || kmsg.String() == "left" || kmsg.String() == "-":
		m.adjust(-1)
		m.refreshViewport()
		return m, nil
	}

	return m, nil
}

// close hides the panel and emits appropriate messages.
func (m SettingsModel) close() (SettingsModel, tea.Cmd) {
	m.Hide()
	var cmds []tea.Cmd
	cmds = append(cmds, func() tea.Msg { return SettingsClosedMsg{} })
	if m.dirty {
		cmds = append(cmds, func() tea.Msg { return ConfigChangedMsg{} })
	}
	return m, tea.Batch(cmds...)
}

// schemaIdx returns the settingsSchema index for the current cursor position.
func (m SettingsModel) schemaIdx() int {
	nav := navigableItems()
	if m.cursor < 0 || m.cursor >= len(nav) {
		return 0
	}
	return nav[m.cursor]
}

// toggleOrCycle toggles a bool, cycles a select, or increments a number.
func (m *SettingsModel) toggleOrCycle() {
	idx := m.schemaIdx()
	item := settingsSchema[idx]
	switch item.kind {
	case settingToggle:
		m.setToggle(idx, !m.getToggle(idx))
		m.dirty = true
	case settingSelect:
		m.cycleSelect(idx, 1)
		m.dirty = true
	case settingNumber:
		m.adjustNumber(idx, 1)
	}
}

// adjust changes the current setting by direction.
func (m *SettingsModel) adjust(dir int) {
	idx := m.schemaIdx()
	item := settingsSchema[idx]
	switch item.kind {
	case settingNumber:
		m.adjustNumber(idx, dir)
	case settingSelect:
		m.cycleSelect(idx, dir)
		m.dirty = true
	case settingToggle:
		// h/l also toggles booleans
		m.setToggle(idx, dir > 0)
		m.dirty = true
	}
}

// adjustNumber changes a number setting by the given direction (-1 or +1).
func (m *SettingsModel) adjustNumber(idx, dir int) {
	item := settingsSchema[idx]
	if item.kind != settingNumber {
		return
	}
	val := m.getNumber(idx)
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
	m.setNumber(idx, val)
	m.dirty = true
}

// cycleSelect cycles a select setting by the given direction.
func (m *SettingsModel) cycleSelect(idx, dir int) {
	item := settingsSchema[idx]
	if item.kind != settingSelect || len(item.values) == 0 {
		return
	}
	cur := m.getSelect(idx)
	curIdx := 0
	for i, v := range item.values {
		if v == cur {
			curIdx = i
			break
		}
	}
	curIdx = (curIdx + dir + len(item.values)) % len(item.values)
	m.setSelect(idx, item.values[curIdx])
}

// getToggle returns the boolean value for a toggle setting.
func (m SettingsModel) getToggle(idx int) bool {
	switch settingsSchema[idx].label {
	case "Enabled":
		// Disambiguate by finding the section this belongs to
		section := m.sectionFor(idx)
		switch section {
		case "Polling":
			return m.cfg.PollEnabled
		case "Notifications":
			return m.cfg.NotificationsEnabled
		}
	case "Collapse Right":
		for _, s := range m.cfg.StartCollapsed {
			if s == "right" {
				return true
			}
		}
		return false
	}
	return false
}

// setToggle sets the boolean value for a toggle setting.
func (m *SettingsModel) setToggle(idx int, val bool) {
	switch settingsSchema[idx].label {
	case "Enabled":
		section := m.sectionFor(idx)
		switch section {
		case "Polling":
			m.cfg.PollEnabled = val
		case "Notifications":
			m.cfg.NotificationsEnabled = val
		}
	case "Collapse Right":
		if val {
			// Add "right" if not present
			found := false
			for _, s := range m.cfg.StartCollapsed {
				if s == "right" {
					found = true
					break
				}
			}
			if !found {
				m.cfg.StartCollapsed = append(m.cfg.StartCollapsed, "right")
			}
		} else {
			// Remove "right"
			var filtered []string
			for _, s := range m.cfg.StartCollapsed {
				if s != "right" {
					filtered = append(filtered, s)
				}
			}
			m.cfg.StartCollapsed = filtered
		}
	}
}

// getNumber returns the numeric value for a number setting.
func (m SettingsModel) getNumber(idx int) int {
	switch settingsSchema[idx].label {
	case "Interval":
		return m.cfg.PollInterval
	case "Claude Timeout":
		return m.cfg.ClaudeTimeout
	case "Auto-collapse Width":
		return m.cfg.CollapseThreshold
	case "PR Fetch Limit":
		return m.cfg.PRFetchLimit
	case "Batch Threshold":
		return m.cfg.NotificationThreshold
	case "Chat History":
		return m.cfg.MaxChatHistory
	case "Prompt Token Limit":
		return m.cfg.MaxPromptTokens
	case "Chat Max Turns":
		return m.cfg.ChatMaxTurns
	case "Analysis Max Turns":
		return m.cfg.AnalysisMaxTurns
	case "Render Refresh":
		return m.cfg.StreamCheckpointMs
	}
	return 0
}

// setNumber sets the numeric value for a number setting.
func (m *SettingsModel) setNumber(idx int, val int) {
	switch settingsSchema[idx].label {
	case "Interval":
		m.cfg.PollInterval = val
	case "Claude Timeout":
		m.cfg.ClaudeTimeout = val
	case "Auto-collapse Width":
		m.cfg.CollapseThreshold = val
	case "PR Fetch Limit":
		m.cfg.PRFetchLimit = val
	case "Batch Threshold":
		m.cfg.NotificationThreshold = val
	case "Chat History":
		m.cfg.MaxChatHistory = val
	case "Prompt Token Limit":
		m.cfg.MaxPromptTokens = val
	case "Chat Max Turns":
		m.cfg.ChatMaxTurns = val
	case "Analysis Max Turns":
		m.cfg.AnalysisMaxTurns = val
	case "Render Refresh":
		m.cfg.StreamCheckpointMs = val
	}
}

// getSelect returns the current string value for a select setting.
func (m SettingsModel) getSelect(idx int) string {
	switch settingsSchema[idx].label {
	case "Default PR Tab":
		if m.cfg.DefaultPRTab == "" {
			return "review"
		}
		return m.cfg.DefaultPRTab
	case "Default Action":
		if m.cfg.DefaultReviewAction == "" {
			return "comment"
		}
		return m.cfg.DefaultReviewAction
	}
	return ""
}

// setSelect sets the string value for a select setting.
func (m *SettingsModel) setSelect(idx int, val string) {
	switch settingsSchema[idx].label {
	case "Default PR Tab":
		m.cfg.DefaultPRTab = val
	case "Default Action":
		m.cfg.DefaultReviewAction = val
	}
}

// sectionFor walks backwards from idx to find the nearest section header label.
func (m SettingsModel) sectionFor(idx int) string {
	for i := idx - 1; i >= 0; i-- {
		if settingsSchema[i].kind == settingSection {
			return settingsSchema[i].label
		}
	}
	return ""
}

// ensureVisible scrolls the viewport so the focused row is visible.
func (m *SettingsModel) ensureVisible() {
	if !m.vpReady {
		return
	}
	// Estimate row position: each schema item is 1 line, sections get 1 extra blank line above
	linePos := 0
	nav := navigableItems()
	targetSchemaIdx := nav[m.cursor]
	for i := 0; i <= targetSchemaIdx; i++ {
		if settingsSchema[i].kind == settingSection {
			if i > 0 {
				linePos++ // blank line before section
			}
			linePos++ // section header line
		} else {
			linePos++ // setting row
		}
	}
	// linePos is 1-indexed line count; viewport uses 0-indexed offset
	row := linePos - 1
	vpTop := m.viewport.YOffset
	vpBottom := vpTop + m.viewport.Height - 1
	if row < vpTop {
		m.viewport.SetYOffset(row)
	} else if row > vpBottom {
		m.viewport.SetYOffset(row - m.viewport.Height + 1)
	}
}

// refreshViewport rebuilds the viewport content.
func (m *SettingsModel) refreshViewport() {
	if !m.vpReady || m.cfg == nil {
		return
	}
	content := m.renderContent()
	m.viewport.SetContent(content)
}

// renderContent builds all setting rows with section headers.
func (m SettingsModel) renderContent() string {
	nav := navigableItems()
	// Build a map of schema index → cursor index for matching
	cursorSchemaIdx := -1
	if m.cursor >= 0 && m.cursor < len(nav) {
		cursorSchemaIdx = nav[m.cursor]
	}

	var rows []string
	for i, item := range settingsSchema {
		if item.kind == settingSection {
			if i > 0 {
				rows = append(rows, "") // blank line before section
			}
			rows = append(rows, settingsSectionStyle.Render("  "+item.label))
			continue
		}
		isFocused := i == cursorSchemaIdx
		rows = append(rows, m.renderSettingRow(i, item, isFocused))
	}

	// Dirty indicator
	if m.dirty {
		rows = append(rows, "")
		rows = append(rows, settingsDirtyStyle.Render("  Changes will be saved on close"))
	}

	return strings.Join(rows, "\n")
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

	var content string
	if m.vpReady {
		content = m.viewport.View()
	}

	boxParts := []string{titleLine, "", content}
	if m.vpReady {
		if indicator := scrollIndicator(m.viewport, innerW); indicator != "" {
			boxParts = append(boxParts, indicator)
		} else {
			boxParts = append(boxParts, "")
		}
	}
	boxParts = append(boxParts, footerLine)
	box := lipgloss.JoinVertical(lipgloss.Left, boxParts...)

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
func (m SettingsModel) renderSettingRow(idx int, item settingItem, isFocused bool) string {
	marker := "  "
	if isFocused {
		marker = settingsMarkerStyle.Render("▸ ")
	}

	labelStyle := settingsLabelStyle
	if isFocused {
		labelStyle = settingsLabelFocusedStyle
	}

	label := labelStyle.Render(padRight(item.label, 22))

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
		unit := ""
		if item.unitSec {
			display = raw / 1000
			unit = "s"
		} else if item.unitMs {
			unit = "ms"
		}
		numStr := fmt.Sprintf("%d%s", display, unit)
		if isFocused {
			value = settingsNumberFocusedStyle.Render(fmt.Sprintf("◂ %s ▸", numStr))
		} else {
			value = settingsNumberStyle.Render(fmt.Sprintf("  %s  ", numStr))
		}
	case settingSelect:
		cur := m.getSelect(idx)
		// Find display label for current value
		displayLabel := cur
		for i, v := range item.values {
			if v == cur {
				displayLabel = item.options[i]
				break
			}
		}
		if isFocused {
			value = settingsSelectFocusedStyle.Render(fmt.Sprintf("◂ %s ▸", displayLabel))
		} else {
			value = settingsSelectStyle.Render(fmt.Sprintf("  %s  ", displayLabel))
		}
	}

	desc := settingsDescStyle.Render(item.desc)

	return marker + label + value + "  " + desc
}

// overlayDimensions returns the outer dimensions of the settings overlay box.
func (m SettingsModel) overlayDimensions() (width, height int) {
	width = int(float64(m.width) * 0.70)
	height = int(float64(m.height) * 0.80)
	if width < 70 {
		width = min(70, m.width)
	}
	if height < 20 {
		height = min(20, m.height)
	}
	if height > m.height-2 {
		height = m.height - 2
	}
	return width, height
}

// innerDimensions returns the viewport dimensions inside the overlay box.
func (m SettingsModel) innerDimensions() (width, height int) {
	ow, oh := m.overlayDimensions()
	// Subtract border (2), padding (2), title (2), footer (2), scroll indicator (1), blank lines (2)
	width = ow - 6
	height = oh - 11
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
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

	settingsSectionStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("33"))

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

	settingsSelectStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("33"))

	settingsSelectFocusedStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("33")).
		Bold(true)

	settingsDescStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("244")).
		Italic(true)

	settingsDirtyStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("214")).
		Italic(true)
)
