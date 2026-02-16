package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/shhac/prtea/internal/claude"
)

// chatMessage represents a single message in the chat history.
type chatMessage struct {
	role    string // "user" or "assistant"
	content string
}

// ChatTabModel manages the interactive chat tab state and rendering.
type ChatTabModel struct {
	messages   []chatMessage
	isWaiting  bool
	chatError  string
	chatStream StreamRenderer
	cache      string
	cacheWidth int
}

// MessageCount returns the number of messages in the chat history.
func (t ChatTabModel) MessageCount() int {
	return len(t.messages)
}

// IsWaiting returns whether the model is waiting for a Claude response.
func (t ChatTabModel) IsWaiting() bool {
	return t.isWaiting
}

// SetWaiting adds a user message and enters the waiting state.
func (t *ChatTabModel) SetWaiting(msg string) {
	t.messages = append(t.messages, chatMessage{role: "user", content: msg})
	t.isWaiting = true
	t.chatError = ""
	t.cache = ""
}

// AddResponse appends a Claude response and clears the waiting state.
func (t *ChatTabModel) AddResponse(content string) {
	t.messages = append(t.messages, chatMessage{role: "assistant", content: content})
	t.isWaiting = false
	t.chatError = ""
	t.chatStream.Reset()
	t.cache = ""
}

// SetChatError sets a chat error and clears the waiting state.
func (t *ChatTabModel) SetChatError(err string) {
	t.chatError = err
	t.isWaiting = false
	t.chatStream.Reset()
	t.cache = ""
}

// ClearChat resets all chat state.
func (t *ChatTabModel) ClearChat() {
	t.messages = nil
	t.isWaiting = false
	t.chatError = ""
	t.chatStream.Reset()
	t.cache = ""
}

// RestoreMessages restores chat history from a previous session.
func (t *ChatTabModel) RestoreMessages(msgs []claude.ChatMessage) {
	t.messages = make([]chatMessage, len(msgs))
	for i, msg := range msgs {
		t.messages[i] = chatMessage{role: msg.Role, content: msg.Content}
	}
	t.cache = ""
}

// Render renders the chat tab content for the viewport.
func (t *ChatTabModel) Render(width int, md *MarkdownRenderer) string {
	if len(t.messages) == 0 && !t.isWaiting && t.chatError == "" {
		return renderEmptyState("No messages yet", "Press Enter to start chatting")
	}

	// Don't cache during streaming — content changes rapidly
	isStreaming := t.isWaiting && t.chatStream.HasContent()

	// Return cached render if available and width hasn't changed
	if !isStreaming && t.cache != "" && t.cacheWidth == width {
		return t.cache
	}

	var b strings.Builder

	for i, msg := range t.messages {
		if i > 0 {
			b.WriteString("\n\n")
		}
		if msg.role == "user" {
			b.WriteString(chatUserStyle.Render("You:"))
		} else {
			b.WriteString(chatAssistantStyle.Render("Claude:"))
		}
		b.WriteString("\n")
		if msg.role == "assistant" {
			b.WriteString(md.RenderMarkdown(msg.content, width))
		} else {
			b.WriteString(wordWrap(msg.content, width))
		}
	}

	if t.isWaiting {
		if len(t.messages) > 0 {
			b.WriteString("\n\n")
		}
		if t.chatStream.HasContent() {
			b.WriteString(chatAssistantStyle.Render("Claude:"))
			b.WriteString("\n")
			b.WriteString(t.chatStream.View(wordWrap, width))
		} else {
			b.WriteString(lipgloss.NewStyle().
				Foreground(lipgloss.Color("244")).
				Italic(true).
				Render("Claude is thinking..."))
		}
	}

	if t.chatError != "" {
		if len(t.messages) > 0 || t.isWaiting {
			b.WriteString("\n\n")
		}
		b.WriteString(lipgloss.NewStyle().
			Foreground(lipgloss.Color("196")).
			Bold(true).
			Render(formatUserError(t.chatError)))
	}

	result := b.String()
	if !isStreaming {
		t.cache = result
		t.cacheWidth = width
	}
	return result
}
