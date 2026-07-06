package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/shhac/prtea/internal/ai"
)

// ChatTabModel manages the interactive chat tab state and rendering.
// The transcript mixes user messages, assistant markdown, and dim activity
// lines (commands the agent ran, thinking summaries).
type ChatTabModel struct {
	messages       []ai.Message
	isWaiting      bool
	chatError      string
	pendingActions []ai.Action
	cache          string
	cacheWidth     int
}

// MessageCount returns the number of user/assistant messages in the transcript.
func (t ChatTabModel) MessageCount() int {
	n := 0
	for _, m := range t.messages {
		if m.Role != ai.RoleActivity {
			n++
		}
	}
	return n
}

// Messages returns the full transcript for persistence.
func (t ChatTabModel) Messages() []ai.Message {
	return t.messages
}

// IsWaiting returns whether a turn is in flight.
func (t ChatTabModel) IsWaiting() bool {
	return t.isWaiting
}

// HasPendingActions returns whether proposed actions await confirmation.
func (t ChatTabModel) HasPendingActions() bool {
	return len(t.pendingActions) > 0
}

// PendingActions returns the proposed actions awaiting confirmation.
func (t ChatTabModel) PendingActions() []ai.Action {
	return t.pendingActions
}

// SetWaiting adds a user message and enters the waiting state.
func (t *ChatTabModel) SetWaiting(msg string) {
	t.messages = append(t.messages, ai.Message{Role: ai.RoleUser, Content: msg})
	t.isWaiting = true
	t.chatError = ""
	t.cache = ""
}

// AddActivity appends a dim activity line (command run, thinking note).
func (t *ChatTabModel) AddActivity(line string) {
	t.messages = append(t.messages, ai.Message{Role: ai.RoleActivity, Content: line})
	t.cache = ""
}

// AddResponse appends an assistant message.
func (t *ChatTabModel) AddResponse(content string) {
	t.messages = append(t.messages, ai.Message{Role: ai.RoleAssistant, Content: content})
	t.chatError = ""
	t.cache = ""
}

// SetTurnDone clears the waiting state at the end of a turn.
func (t *ChatTabModel) SetTurnDone() {
	t.isWaiting = false
	t.cache = ""
}

// SetPendingActions stores proposed actions awaiting user confirmation.
func (t *ChatTabModel) SetPendingActions(actions []ai.Action) {
	t.pendingActions = actions
	t.cache = ""
}

// ClearPendingActions removes the pending action prompt.
func (t *ChatTabModel) ClearPendingActions() {
	t.pendingActions = nil
	t.cache = ""
}

// SetChatError sets a chat error and clears the waiting state.
func (t *ChatTabModel) SetChatError(err string) {
	t.chatError = err
	t.isWaiting = false
	t.cache = ""
}

// ClearChat resets all chat state.
func (t *ChatTabModel) ClearChat() {
	t.messages = nil
	t.isWaiting = false
	t.chatError = ""
	t.pendingActions = nil
	t.cache = ""
}

// RestoreMessages restores a transcript from a previous session.
func (t *ChatTabModel) RestoreMessages(msgs []ai.Message) {
	t.messages = append([]ai.Message(nil), msgs...)
	t.cache = ""
}

var chatActivityStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

// Render renders the chat tab content for the viewport.
func (t *ChatTabModel) Render(width int, md *MarkdownRenderer) string {
	if len(t.messages) == 0 && !t.isWaiting && t.chatError == "" {
		return renderEmptyState("No messages yet", "Press Enter to chat, or 'a' for a PR overview")
	}

	if t.cache != "" && t.cacheWidth == width {
		return t.cache
	}

	var b strings.Builder

	prevRole := ""
	for _, msg := range t.messages {
		if b.Len() > 0 {
			if msg.Role == ai.RoleActivity && prevRole == ai.RoleActivity {
				b.WriteString("\n")
			} else {
				b.WriteString("\n\n")
			}
		}
		switch msg.Role {
		case ai.RoleUser:
			b.WriteString(chatUserStyle.Render("You:"))
			b.WriteString("\n")
			b.WriteString(wordWrap(msg.Content, width))
		case ai.RoleActivity:
			b.WriteString(chatActivityStyle.Render(truncateLine(msg.Content, width)))
		default:
			b.WriteString(chatAssistantStyle.Render("AI:"))
			b.WriteString("\n")
			b.WriteString(md.RenderMarkdown(msg.Content, width))
		}
		prevRole = msg.Role
	}

	if t.isWaiting {
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(lipgloss.NewStyle().
			Foreground(lipgloss.Color("244")).
			Italic(true).
			Render("Working..."))
	}

	for _, action := range t.pendingActions {
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(renderActionPrompt(action, width))
	}

	if t.chatError != "" {
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(lipgloss.NewStyle().
			Foreground(lipgloss.Color("196")).
			Bold(true).
			Render(formatUserError(t.chatError)))
	}

	result := b.String()
	if !t.isWaiting {
		t.cache = result
		t.cacheWidth = width
	}
	return result
}

// renderActionPrompt renders a proposed action awaiting confirmation.
func renderActionPrompt(action ai.Action, width int) string {
	title := lipgloss.NewStyle().
		Foreground(lipgloss.Color("214")).
		Bold(true).
		Render("Proposed: " + action.Describe())

	var body string
	if action.Body != "" {
		body = wordWrap(action.Body, width-4)
	}

	hint := lipgloss.NewStyle().
		Foreground(lipgloss.Color("244")).
		Italic(true).
		Render("y to confirm · n to dismiss")

	parts := []string{title}
	if body != "" {
		parts = append(parts, body)
	}
	parts = append(parts, hint)

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("214")).
		Padding(0, 1).
		Width(width - 2).
		Render(strings.Join(parts, "\n"))
}

// truncateLine shortens a single line to fit the given width.
func truncateLine(s string, width int) string {
	if width < 4 || len(s) <= width {
		return s
	}
	return s[:width-1] + "…"
}
