package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Command represents a registered command in the palette.
type Command struct {
	Name        string   // primary name (e.g., "review")
	Aliases     []string // short aliases (e.g., ["rev"])
	QuickKey    string   // single key for quick mode, empty if not in quick palette
	Description string   // human-readable description
}

// commandRegistry is the canonical list of all commands.
// Quick-key commands are listed first, full-mode-only commands follow.
var commandRegistry = []Command{
	// Actions with quick keys
	{Name: "analyze", Aliases: []string{"an"}, QuickKey: "a", Description: "Analyze PR with Claude"},
	{Name: "open", Aliases: []string{"op"}, QuickKey: "o", Description: "Open PR in browser"},
	{Name: "new", Aliases: nil, QuickKey: "n", Description: "New chat (clear)"},
	{Name: "quit", Aliases: []string{"q"}, QuickKey: "q", Description: "Quit prtea"},
	{Name: "help", Aliases: []string{"h", "?"}, QuickKey: "?", Description: "Show help"},
	{Name: "zoom", Aliases: []string{"z"}, QuickKey: "z", Description: "Zoom focused panel"},
	{Name: "comment", Aliases: []string{"cm"}, QuickKey: "c", Description: "Add inline comment"},
	// Panel toggles with quick keys
	{Name: "toggle left", Aliases: []string{"tl"}, QuickKey: "1", Description: "Toggle left panel"},
	{Name: "toggle center", Aliases: []string{"tc"}, QuickKey: "2", Description: "Toggle center panel"},
	{Name: "toggle right", Aliases: []string{"tr"}, QuickKey: "3", Description: "Toggle right panel"},
	// Full mode only
	{Name: "config", Aliases: []string{"settings", "cfg"}, QuickKey: "s", Description: "Open settings"},
	{Name: "clear selection", Aliases: []string{"cs"}, Description: "Clear hunk selection"},
	{Name: "review", Aliases: []string{"rev"}, Description: "Generate AI review"},
	{Name: "approve", Aliases: []string{"ap"}, Description: "Quick-approve PR"},
	{Name: "rerun ci", Aliases: []string{"rerun"}, Description: "Re-run failed CI checks"},
	{Name: "refresh", Aliases: []string{"ref"}, Description: "Refresh current view"},
	{Name: "diff", Aliases: []string{"d"}, Description: "Focus diff panel"},
	{Name: "chat", Aliases: []string{"ch"}, Description: "Focus chat panel"},
	{Name: "prs", Aliases: nil, Description: "Focus PR list"},
}

// CommandModeModel manages the command palette overlay.
type CommandModeModel struct {
	quickMode bool
	input     textinput.Model
	filtered  []Command
	selected  int
	width     int
	height    int
	active    bool
}

// NewCommandModeModel creates a command palette model.
func NewCommandModeModel() CommandModeModel {
	ti := textinput.New()
	ti.Prompt = ": "
	ti.PromptStyle = cmdPalettePromptStyle
	ti.TextStyle = cmdPaletteInputTextStyle
	ti.Placeholder = "type a command..."
	ti.PlaceholderStyle = cmdPaletteHintStyle
	ti.CharLimit = 64
	return CommandModeModel{
		input: ti,
	}
}

// SetSize updates the overlay dimensions.
func (m *CommandModeModel) SetSize(width, height int) {
	m.width = width
	m.height = height
}

// Open activates command mode. quick=true for Ctrl+P, quick=false for :.
func (m *CommandModeModel) Open(quick bool) tea.Cmd {
	m.quickMode = quick
	m.active = true
	m.selected = 0
	if !quick {
		m.input.SetValue("")
		m.filtered = commandRegistry
		return m.input.Focus()
	}
	return nil
}

// Close deactivates command mode.
func (m *CommandModeModel) Close() {
	m.active = false
	m.input.Blur()
}

// IsActive returns whether command mode is currently active.
func (m CommandModeModel) IsActive() bool {
	return m.active
}

// Update handles messages in command mode.
func (m CommandModeModel) Update(msg tea.Msg) (CommandModeModel, tea.Cmd) {
	if kmsg, ok := msg.(tea.KeyMsg); ok {
		if m.quickMode {
			return m.updateQuick(kmsg)
		}
		return m.updateFull(kmsg)
	}
	// Pass non-key messages to textinput (cursor blink, etc.)
	if !m.quickMode {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m CommandModeModel) updateQuick(msg tea.KeyMsg) (CommandModeModel, tea.Cmd) {
	keyStr := msg.String()

	// Esc or Ctrl+P toggle exits
	if keyStr == "esc" || keyStr == "ctrl+p" {
		m.Close()
		return m, func() tea.Msg { return CommandModeExitMsg{} }
	}

	// Match against quick keys
	for _, cmd := range commandRegistry {
		if cmd.QuickKey != "" && cmd.QuickKey == keyStr {
			m.Close()
			name := cmd.Name
			return m, func() tea.Msg { return CommandExecuteMsg{Name: name} }
		}
	}

	// Unknown key: stay in palette
	return m, nil
}

func (m CommandModeModel) updateFull(msg tea.KeyMsg) (CommandModeModel, tea.Cmd) {
	keyStr := msg.String()

	switch keyStr {
	case "esc", "ctrl+p":
		m.Close()
		return m, func() tea.Msg { return CommandModeExitMsg{} }

	case "enter":
		input := strings.TrimSpace(m.input.Value())
		if input == "" {
			m.Close()
			return m, func() tea.Msg { return CommandModeExitMsg{} }
		}
		// Execute the highlighted suggestion if available
		if len(m.filtered) > 0 && m.selected < len(m.filtered) {
			name := m.filtered[m.selected].Name
			m.Close()
			return m, func() tea.Msg { return CommandExecuteMsg{Name: name} }
		}
		// No filtered results — try to resolve typed input
		name := m.resolveCommand(input)
		if name == "" {
			name = input
		}
		m.Close()
		return m, func() tea.Msg { return CommandExecuteMsg{Name: name} }

	case "tab":
		if len(m.filtered) > 0 && m.selected < len(m.filtered) {
			m.input.SetValue(m.filtered[m.selected].Name)
			m.input.CursorEnd()
		}
		return m, nil

	case "up":
		if m.selected > 0 {
			m.selected--
		}
		return m, nil

	case "down":
		if m.selected < len(m.filtered)-1 {
			m.selected++
		}
		return m, nil

	default:
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		m.filterCommands()
		return m, cmd
	}
}

// resolveCommand maps user input to a command name.
func (m CommandModeModel) resolveCommand(input string) string {
	lower := strings.ToLower(input)

	// Exact name match
	for _, cmd := range commandRegistry {
		if strings.ToLower(cmd.Name) == lower {
			return cmd.Name
		}
	}

	// Exact alias match
	for _, cmd := range commandRegistry {
		for _, alias := range cmd.Aliases {
			if strings.ToLower(alias) == lower {
				return cmd.Name
			}
		}
	}

	// Unambiguous prefix match
	var matches []Command
	for _, cmd := range commandRegistry {
		if strings.HasPrefix(strings.ToLower(cmd.Name), lower) {
			matches = append(matches, cmd)
		}
	}
	if len(matches) == 1 {
		return matches[0].Name
	}

	return ""
}

func (m *CommandModeModel) filterCommands() {
	input := strings.ToLower(strings.TrimSpace(m.input.Value()))
	if input == "" {
		m.filtered = commandRegistry
		m.selected = 0
		return
	}

	var filtered []Command
	for _, cmd := range commandRegistry {
		if strings.HasPrefix(strings.ToLower(cmd.Name), input) {
			filtered = append(filtered, cmd)
			continue
		}
		for _, alias := range cmd.Aliases {
			if strings.HasPrefix(strings.ToLower(alias), input) {
				filtered = append(filtered, cmd)
				break
			}
		}
	}

	m.filtered = filtered
	if m.selected >= len(m.filtered) {
		m.selected = max(0, len(m.filtered)-1)
	}
}

// quickCommands returns only commands that have a quick key.
func quickCommands() []Command {
	var cmds []Command
	for _, cmd := range commandRegistry {
		if cmd.QuickKey != "" {
			cmds = append(cmds, cmd)
		}
	}
	return cmds
}

// View renders the command palette overlay.
func (m CommandModeModel) View() string {
	if !m.active {
		return ""
	}
	if m.quickMode {
		return m.viewQuick()
	}
	return m.viewFull()
}

func (m CommandModeModel) viewQuick() string {
	cmds := quickCommands()

	var b strings.Builder

	// Title divider
	title := cmdPaletteTitleStyle.Render(" Ctrl+P ")
	titleW := lipgloss.Width(title)
	remaining := m.width - titleW - 1
	if remaining < 0 {
		remaining = 0
	}
	b.WriteString(cmdPaletteDividerStyle.Render("─") + title + cmdPaletteDividerStyle.Render(strings.Repeat("─", remaining)))
	b.WriteString("\n")

	// 2-column grid
	colWidth := m.width / 2
	if colWidth < 20 {
		colWidth = 20
	}

	half := (len(cmds) + 1) / 2
	for i := 0; i < half; i++ {
		left := formatQuickEntry(cmds[i], colWidth)
		right := ""
		if i+half < len(cmds) {
			right = formatQuickEntry(cmds[i+half], colWidth)
		}
		b.WriteString(left + right + "\n")
	}

	// Footer hint
	b.WriteString(cmdPaletteHintStyle.Render(" Press a key · Esc to cancel"))

	return b.String()
}

func formatQuickEntry(cmd Command, colWidth int) string {
	keyStr := cmdPaletteKeyStyle.Render(padRight(cmd.QuickKey, 3))
	desc := cmdPaletteDescStyle.Render(cmd.Description)
	entry := " " + keyStr + " " + desc
	entryWidth := lipgloss.Width(entry)
	if entryWidth < colWidth {
		entry += strings.Repeat(" ", colWidth-entryWidth)
	}
	return entry
}

func (m CommandModeModel) viewFull() string {
	var b strings.Builder

	// Title divider
	title := cmdPaletteTitleStyle.Render(" : ")
	titleW := lipgloss.Width(title)
	remaining := m.width - titleW - 1
	if remaining < 0 {
		remaining = 0
	}
	b.WriteString(cmdPaletteDividerStyle.Render("─") + title + cmdPaletteDividerStyle.Render(strings.Repeat("─", remaining)))
	b.WriteString("\n")

	// Suggestions (max 8 visible)
	maxShow := min(8, len(m.filtered))
	for i := 0; i < maxShow; i++ {
		cmd := m.filtered[i]
		marker := "  "
		nameStyle := cmdPaletteDescStyle
		if i == m.selected {
			marker = cmdPaletteMarkerStyle.Render("▸ ")
			nameStyle = cmdPaletteSelectedStyle
		}

		name := nameStyle.Render(padRight(cmd.Name, 18))
		desc := cmdPaletteHintStyle.Render(cmd.Description)

		aliasStr := ""
		if len(cmd.Aliases) > 0 {
			aliasStr = cmdPaletteAliasStyle.Render(" (" + strings.Join(cmd.Aliases, ", ") + ")")
		}

		b.WriteString(marker + name + aliasStr + " " + desc + "\n")
	}

	if len(m.filtered) == 0 && m.input.Value() != "" {
		b.WriteString(cmdPaletteErrorStyle.Render("  No matching commands") + "\n")
	}

	// Input line
	b.WriteString(m.input.View())

	return b.String()
}
