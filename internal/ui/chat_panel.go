package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ChatMode represents normal/insert mode for the chat panel.
type ChatMode int

const (
	ChatModeNormal ChatMode = iota
	ChatModeInsert
)

// ChatTab identifies which sub-tab is active in the chat panel.
type ChatTab int

const (
	ChatTabChat ChatTab = iota
	ChatTabAnalysis
	ChatTabComments
)

// ModeChangedMsg is sent when the chat panel changes modes.
type ModeChangedMsg struct {
	Mode ChatMode
}

// ChatPanelModel manages the chat/analysis panel.
type ChatPanelModel struct {
	viewport  viewport.Model
	textInput textinput.Model
	chatMode  ChatMode
	activeTab ChatTab
	messages  []chatMessage
	width     int
	height    int
	focused   bool
	ready     bool
}

type chatMessage struct {
	role    string // "user" or "assistant"
	content string
}

func NewChatPanelModel() ChatPanelModel {
	ti := textinput.New()
	ti.Placeholder = "Ask about this PR..."
	ti.CharLimit = 500

	messages := []chatMessage{
		{role: "user", content: "What does this PR change?"},
		{role: "assistant", content: "This PR adds authentication timeout handling to the login flow. The key changes are:\n\n1. Added a `authenticateWithTimeout` wrapper that uses context deadlines\n2. Improved error wrapping with fmt.Errorf\n3. Set token expiration to 24 hours\n\nThe changes look solid — the timeout prevents hanging auth requests."},
		{role: "user", content: "Are there any potential issues?"},
		{role: "assistant", content: "A few things to consider:\n\n1. The 30-second default timeout is hardcoded — might want to make it configurable\n2. The goroutine in authenticateWithTimeout could leak if the context is cancelled but authenticate() never returns\n3. No unit tests for the new timeout path"},
	}

	return ChatPanelModel{
		textInput: ti,
		chatMode:  ChatModeNormal,
		activeTab: ChatTabChat,
		messages:  messages,
	}
}

func (m ChatPanelModel) Update(msg tea.Msg) (ChatPanelModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.chatMode == ChatModeInsert {
			switch {
			case key.Matches(msg, ChatKeys.ExitInsert):
				m.chatMode = ChatModeNormal
				m.textInput.Blur()
				return m, func() tea.Msg { return ModeChangedMsg{Mode: ChatModeNormal} }
			case key.Matches(msg, ChatKeys.Send):
				if m.textInput.Value() != "" {
					m.messages = append(m.messages, chatMessage{
						role:    "user",
						content: m.textInput.Value(),
					})
					m.textInput.Reset()
					m.refreshViewport()
					m.viewport.GotoBottom()
				}
				return m, nil
			default:
				var cmd tea.Cmd
				m.textInput, cmd = m.textInput.Update(msg)
				return m, cmd
			}
		}

		// Normal mode
		switch {
		case key.Matches(msg, ChatKeys.PrevTab):
			if m.activeTab > ChatTabChat {
				m.activeTab--
			}
			m.refreshViewport()
			return m, nil
		case key.Matches(msg, ChatKeys.NextTab):
			if m.activeTab < ChatTabComments {
				m.activeTab++
			}
			m.refreshViewport()
			return m, nil
		case msg.String() == "enter":
			m.chatMode = ChatModeInsert
			m.textInput.Focus()
			return m, func() tea.Msg { return ModeChangedMsg{Mode: ChatModeInsert} }
		}
	}

	if m.chatMode == ChatModeNormal && m.ready {
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m *ChatPanelModel) SetSize(width, height int) {
	m.width = width
	m.height = height
	// Account for borders (2), header (2), input line (1), padding
	innerWidth := width - 4
	innerHeight := height - 7
	if innerWidth < 1 {
		innerWidth = 1
	}
	if innerHeight < 1 {
		innerHeight = 1
	}

	m.textInput.Width = innerWidth - 4

	if !m.ready {
		m.viewport = viewport.New(innerWidth, innerHeight)
		m.ready = true
	} else {
		m.viewport.Width = innerWidth
		m.viewport.Height = innerHeight
	}
	m.refreshViewport()
}

func (m *ChatPanelModel) SetFocused(focused bool) {
	m.focused = focused
	if !focused && m.chatMode == ChatModeInsert {
		m.chatMode = ChatModeNormal
		m.textInput.Blur()
	}
}

func (m *ChatPanelModel) refreshViewport() {
	if !m.ready {
		return
	}
	content := m.renderMessages()
	m.viewport.SetContent(content)
}

func (m ChatPanelModel) View() string {
	header := m.renderHeader()

	var content string
	if m.ready {
		content = m.viewport.View()
	} else {
		content = "Loading..."
	}

	input := m.renderInput()
	inner := lipgloss.JoinVertical(lipgloss.Left, header, content, input)

	isInsert := m.chatMode == ChatModeInsert
	style := panelStyle(m.focused, isInsert, m.width-2, m.height-2)
	return style.Render(inner)
}

func (m ChatPanelModel) renderHeader() string {
	var tabs []string

	tabNames := []struct {
		tab  ChatTab
		name string
	}{
		{ChatTabChat, "Chat"},
		{ChatTabAnalysis, "Analysis"},
		{ChatTabComments, "Comments"},
	}

	for _, t := range tabNames {
		if m.activeTab == t.tab {
			tabs = append(tabs, activeTabStyle().Render(t.name))
		} else {
			tabs = append(tabs, inactiveTabStyle().Render(t.name))
		}
	}

	tabRow := strings.Join(tabs, " ")

	var badge string
	if m.chatMode == ChatModeInsert {
		badge = insertModeBadge()
	} else {
		badge = normalModeBadge()
	}

	headerWidth := m.width - 6
	if headerWidth < 1 {
		headerWidth = 1
	}
	padding := headerWidth - lipgloss.Width(tabRow) - lipgloss.Width(badge)
	if padding < 1 {
		padding = 1
	}

	return tabRow + strings.Repeat(" ", padding) + badge
}

func (m ChatPanelModel) renderMessages() string {
	if len(m.messages) == 0 {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("244")).
			Render("No messages yet. Press Enter to start chatting.")
	}

	var b strings.Builder
	innerWidth := m.width - 6
	if innerWidth < 10 {
		innerWidth = 10
	}

	for i, msg := range m.messages {
		if i > 0 {
			b.WriteString("\n\n")
		}

		var roleLabel string
		if msg.role == "user" {
			roleLabel = chatUserStyle.Render("You:")
		} else {
			roleLabel = chatAssistantStyle.Render("Claude:")
		}

		b.WriteString(roleLabel)
		b.WriteString("\n")

		// Word wrap the content
		wrapped := wordWrap(msg.content, innerWidth)
		b.WriteString(wrapped)
	}

	return b.String()
}

func (m ChatPanelModel) renderInput() string {
	var prefix string
	if m.chatMode == ChatModeInsert {
		prefix = lipgloss.NewStyle().
			Foreground(lipgloss.Color("42")).
			Bold(true).
			Render("> ")
	} else {
		prefix = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")).
			Render("> ")
	}

	if m.chatMode == ChatModeInsert {
		return prefix + m.textInput.View()
	}
	return prefix + lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		Italic(true).
		Render("press Enter to chat")
}

// wordWrap wraps text to fit within the given width.
func wordWrap(s string, width int) string {
	if width <= 0 {
		return s
	}

	var result strings.Builder
	for _, line := range strings.Split(s, "\n") {
		if lipgloss.Width(line) <= width {
			if result.Len() > 0 {
				result.WriteString("\n")
			}
			result.WriteString(line)
			continue
		}

		words := strings.Fields(line)
		currentLine := ""
		for _, word := range words {
			if currentLine == "" {
				currentLine = word
			} else if lipgloss.Width(currentLine+" "+word) <= width {
				currentLine += " " + word
			} else {
				if result.Len() > 0 {
					result.WriteString("\n")
				}
				result.WriteString(currentLine)
				currentLine = word
			}
		}
		if currentLine != "" {
			if result.Len() > 0 {
				result.WriteString("\n")
			}
			result.WriteString(currentLine)
		}
	}
	return result.String()
}
