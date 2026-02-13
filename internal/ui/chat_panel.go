package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/shhac/prtea/internal/claude"
	"github.com/shhac/prtea/internal/github"
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

// ChatSendMsg is emitted when the user sends a chat message.
type ChatSendMsg struct {
	Message string
}

// ChatResponseMsg is sent when Claude responds to a chat message.
type ChatResponseMsg struct {
	Content string
	Err     error
}

// ChatStreamChunkMsg carries a streaming text chunk from Claude.
type ChatStreamChunkMsg struct {
	Content string
}

// CommentPostMsg is emitted when the user wants to post a PR comment.
type CommentPostMsg struct {
	Body string
}

// CommentPostedMsg is sent after a comment has been posted (or failed).
type CommentPostedMsg struct {
	Err error
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

	// Chat state
	isWaiting        bool   // true while waiting for Claude response
	chatError        string // last chat error message
	streamingContent string // accumulated streaming text from Claude

	// Analysis state
	analysisResult  *claude.AnalysisResult
	analysisLoading bool
	analysisError   string

	// Comments state
	comments        []github.Comment
	inlineComments  []github.InlineComment
	commentsLoading bool
	commentsError   string
	commentPosting  bool // true while posting a comment
}

type chatMessage struct {
	role    string // "user" or "assistant"
	content string
}

func NewChatPanelModel() ChatPanelModel {
	ti := textinput.New()
	ti.Placeholder = "Ask about this PR..."
	ti.CharLimit = 500

	return ChatPanelModel{
		textInput: ti,
		chatMode:  ChatModeNormal,
		activeTab: ChatTabChat,
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
				if m.textInput.Value() == "" {
					return m, nil
				}
				userMsg := m.textInput.Value()
				m.textInput.Reset()

				if m.activeTab == ChatTabComments {
					if !m.commentPosting {
						m.commentPosting = true
						m.refreshViewport()
						m.viewport.GotoBottom()
						return m, func() tea.Msg { return CommentPostMsg{Body: userMsg} }
					}
					return m, nil
				}

				// Chat tab send
				if !m.isWaiting {
					m.messages = append(m.messages, chatMessage{
						role:    "user",
						content: userMsg,
					})
					m.isWaiting = true
					m.chatError = ""
					m.refreshViewport()
					m.viewport.GotoBottom()
					return m, func() tea.Msg { return ChatSendMsg{Message: userMsg} }
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
			if m.activeTab == ChatTabAnalysis {
				return m, nil
			}
			m.chatMode = ChatModeInsert
			if m.activeTab == ChatTabComments {
				m.textInput.Placeholder = "Write a comment..."
			} else {
				m.textInput.Placeholder = "Ask about this PR..."
			}
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

// SetAnalysisLoading puts the analysis tab into loading state.
func (m *ChatPanelModel) SetAnalysisLoading() {
	m.analysisLoading = true
	m.analysisError = ""
	m.analysisResult = nil
	m.refreshViewport()
}

// SetAnalysisResult sets the analysis result and clears loading state.
func (m *ChatPanelModel) SetAnalysisResult(result *claude.AnalysisResult) {
	m.analysisResult = result
	m.analysisLoading = false
	m.analysisError = ""
	m.refreshViewport()
}

// SetAnalysisError sets an error message on the analysis tab.
func (m *ChatPanelModel) SetAnalysisError(err string) {
	m.analysisError = err
	m.analysisLoading = false
	m.analysisResult = nil
	m.refreshViewport()
}

// AppendStreamChunk appends a text chunk during streaming and refreshes the viewport.
func (m *ChatPanelModel) AppendStreamChunk(chunk string) {
	m.streamingContent += chunk
	m.refreshViewport()
	m.viewport.GotoBottom()
}

// AddResponse appends a Claude response and clears the waiting state.
func (m *ChatPanelModel) AddResponse(content string) {
	m.messages = append(m.messages, chatMessage{
		role:    "assistant",
		content: content,
	})
	m.isWaiting = false
	m.chatError = ""
	m.streamingContent = ""
	m.refreshViewport()
	m.viewport.GotoBottom()
}

// SetChatError sets a chat error and clears the waiting state.
func (m *ChatPanelModel) SetChatError(err string) {
	m.chatError = err
	m.isWaiting = false
	m.streamingContent = ""
	m.refreshViewport()
	m.viewport.GotoBottom()
}

// ClearChat resets chat messages and state for a new PR.
func (m *ChatPanelModel) ClearChat() {
	m.messages = nil
	m.isWaiting = false
	m.chatError = ""
	m.streamingContent = ""
	m.refreshViewport()
}

// SetCommentsLoading puts the comments tab into loading state.
func (m *ChatPanelModel) SetCommentsLoading() {
	m.commentsLoading = true
	m.commentsError = ""
	m.comments = nil
	m.inlineComments = nil
	m.refreshViewport()
}

// SetComments sets the comments data and clears loading state.
func (m *ChatPanelModel) SetComments(comments []github.Comment, inline []github.InlineComment) {
	m.comments = comments
	m.inlineComments = inline
	m.commentsLoading = false
	m.commentsError = ""
	m.refreshViewport()
}

// SetCommentsError sets an error message on the comments tab.
func (m *ChatPanelModel) SetCommentsError(err string) {
	m.commentsError = err
	m.commentsLoading = false
	m.refreshViewport()
}

// SetCommentPosted clears the posting state after a comment post attempt.
func (m *ChatPanelModel) SetCommentPosted(err error) {
	m.commentPosting = false
	if err != nil {
		m.commentsError = "Failed to post comment: " + err.Error()
	}
	m.refreshViewport()
	m.viewport.GotoBottom()
}

// ClearComments resets comments state for a new PR.
func (m *ChatPanelModel) ClearComments() {
	m.comments = nil
	m.inlineComments = nil
	m.commentsLoading = false
	m.commentsError = ""
	m.commentPosting = false
	m.refreshViewport()
}

func (m *ChatPanelModel) refreshViewport() {
	if !m.ready {
		return
	}
	var content string
	switch m.activeTab {
	case ChatTabAnalysis:
		content = m.renderAnalysis()
	case ChatTabComments:
		content = m.renderComments()
	default:
		content = m.renderMessages()
	}
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
	if len(m.messages) == 0 && !m.isWaiting && m.chatError == "" {
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

		if msg.role == "assistant" {
			b.WriteString(renderMarkdown(msg.content, innerWidth))
		} else {
			b.WriteString(wordWrap(msg.content, innerWidth))
		}
	}

	if m.isWaiting {
		if len(m.messages) > 0 {
			b.WriteString("\n\n")
		}
		if m.streamingContent != "" {
			b.WriteString(chatAssistantStyle.Render("Claude:"))
			b.WriteString("\n")
			wrapped := wordWrap(m.streamingContent, innerWidth)
			b.WriteString(wrapped)
		} else {
			b.WriteString(lipgloss.NewStyle().
				Foreground(lipgloss.Color("244")).
				Italic(true).
				Render("Claude is thinking..."))
		}
	}

	if m.chatError != "" {
		if len(m.messages) > 0 || m.isWaiting {
			b.WriteString("\n\n")
		}
		b.WriteString(lipgloss.NewStyle().
			Foreground(lipgloss.Color("196")).
			Bold(true).
			Render("Error: " + m.chatError))
	}

	return b.String()
}

func (m ChatPanelModel) renderAnalysis() string {
	if m.analysisLoading {
		return lipgloss.NewStyle().
			Foreground(lipgloss.Color("244")).
			Padding(1, 0).
			Render("Analyzing PR with Claude...\n\nThis may take a minute.")
	}
	if m.analysisError != "" {
		return lipgloss.NewStyle().
			Foreground(lipgloss.Color("196")).
			Bold(true).
			Padding(1, 0).
			Render(m.analysisError)
	}
	if m.analysisResult == nil {
		return lipgloss.NewStyle().
			Foreground(lipgloss.Color("244")).
			Padding(1, 0).
			Render("Press 'a' to analyze this PR with Claude.")
	}
	return m.renderAnalysisResult()
}

func (m ChatPanelModel) renderComments() string {
	if m.commentsLoading {
		return lipgloss.NewStyle().
			Foreground(lipgloss.Color("244")).
			Padding(1, 0).
			Render("Loading comments...")
	}
	if m.commentsError != "" {
		return lipgloss.NewStyle().
			Foreground(lipgloss.Color("196")).
			Bold(true).
			Padding(1, 0).
			Render(m.commentsError)
	}
	if len(m.comments) == 0 && len(m.inlineComments) == 0 {
		return lipgloss.NewStyle().
			Foreground(lipgloss.Color("244")).
			Padding(1, 0).
			Render("No comments on this PR.")
	}

	innerWidth := m.width - 6
	if innerWidth < 10 {
		innerWidth = 10
	}

	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("33"))
	authorStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("220"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	fileStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("42"))

	var b strings.Builder

	// Issue-level comments
	if len(m.comments) > 0 {
		b.WriteString(sectionStyle.Render(fmt.Sprintf("Conversation (%d)", len(m.comments))))
		b.WriteString("\n")
		for i, c := range m.comments {
			if i > 0 {
				b.WriteString("\n")
			}
			b.WriteString(authorStyle.Render(c.Author.Login))
			b.WriteString(dimStyle.Render(" · " + c.CreatedAt.Format("Jan 2 15:04")))
			b.WriteString("\n")
			b.WriteString(renderMarkdown(c.Body, innerWidth))
			b.WriteString("\n")
		}
	}

	// Inline review comments
	if len(m.inlineComments) > 0 {
		if len(m.comments) > 0 {
			b.WriteString("\n")
		}
		b.WriteString(sectionStyle.Render(fmt.Sprintf("Review Comments (%d)", len(m.inlineComments))))
		b.WriteString("\n")
		for i, c := range m.inlineComments {
			if i > 0 {
				b.WriteString("\n")
			}
			b.WriteString(authorStyle.Render(c.Author.Login))
			b.WriteString(dimStyle.Render(" · " + c.CreatedAt.Format("Jan 2 15:04")))
			if c.Path != "" {
				b.WriteString(" ")
				label := c.Path
				if c.Line > 0 {
					label = fmt.Sprintf("%s:%d", c.Path, c.Line)
				}
				b.WriteString(fileStyle.Render(label))
			}
			if c.Outdated {
				b.WriteString(dimStyle.Render(" (outdated)"))
			}
			b.WriteString("\n")
			b.WriteString(renderMarkdown(c.Body, innerWidth))
			b.WriteString("\n")
		}
	}

	return b.String()
}

func (m ChatPanelModel) renderAnalysisResult() string {
	r := m.analysisResult
	innerWidth := m.width - 6
	if innerWidth < 10 {
		innerWidth = 10
	}

	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("33"))
	var b strings.Builder

	// Risk badge
	riskBadge := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("0")).
		Background(riskLevelColor(r.Risk.Level)).
		Padding(0, 1).
		Render(strings.ToUpper(r.Risk.Level) + " RISK")
	b.WriteString(riskBadge)
	b.WriteString("\n")
	b.WriteString(wordWrap(r.Risk.Reasoning, innerWidth))
	b.WriteString("\n\n")

	// Summary
	b.WriteString(sectionStyle.Render("Summary"))
	b.WriteString("\n")
	b.WriteString(wordWrap(r.Summary, innerWidth))
	b.WriteString("\n\n")

	// Architecture impact
	if r.ArchitectureImpact.HasImpact {
		b.WriteString(sectionStyle.Render("Architecture Impact"))
		b.WriteString("\n")
		b.WriteString(wordWrap(r.ArchitectureImpact.Description, innerWidth))
		if len(r.ArchitectureImpact.AffectedModules) > 0 {
			b.WriteString("\nAffected: ")
			b.WriteString(strings.Join(r.ArchitectureImpact.AffectedModules, ", "))
		}
		b.WriteString("\n\n")
	}

	// File reviews
	if len(r.FileReviews) > 0 {
		b.WriteString(sectionStyle.Render(fmt.Sprintf("File Reviews (%d)", len(r.FileReviews))))
		b.WriteString("\n")
		fileStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("220"))
		for _, fr := range r.FileReviews {
			b.WriteString("\n")
			b.WriteString(fileStyle.Render(fr.File))
			b.WriteString("\n")
			b.WriteString(wordWrap(fr.Summary, innerWidth))
			b.WriteString("\n")
			for _, c := range fr.Comments {
				sevLabel := severityStyle(c.Severity).Render(c.Severity)
				if c.Line > 0 {
					sevLabel += fmt.Sprintf(" L%d", c.Line)
				}
				b.WriteString("  ")
				b.WriteString(sevLabel)
				b.WriteString(" ")
				b.WriteString(wordWrap(c.Comment, innerWidth-4))
				b.WriteString("\n")
			}
		}
		b.WriteString("\n")
	}

	// Test coverage
	b.WriteString(sectionStyle.Render("Test Coverage"))
	b.WriteString("\n")
	b.WriteString(wordWrap(r.TestCoverage.Assessment, innerWidth))
	if len(r.TestCoverage.Gaps) > 0 {
		b.WriteString("\nGaps:")
		for _, gap := range r.TestCoverage.Gaps {
			b.WriteString("\n  • ")
			b.WriteString(wordWrap(gap, innerWidth-4))
		}
	}
	b.WriteString("\n\n")

	// Suggestions
	if len(r.Suggestions) > 0 {
		b.WriteString(sectionStyle.Render(fmt.Sprintf("Suggestions (%d)", len(r.Suggestions))))
		b.WriteString("\n")
		titleStyle := lipgloss.NewStyle().Bold(true)
		for _, s := range r.Suggestions {
			b.WriteString("\n  • ")
			b.WriteString(titleStyle.Render(s.Title))
			b.WriteString("\n    ")
			b.WriteString(wordWrap(s.Description, innerWidth-4))
			if s.File != "" {
				b.WriteString(fmt.Sprintf("\n    File: %s", s.File))
			}
			b.WriteString("\n")
		}
	}

	return b.String()
}

func riskLevelColor(level string) lipgloss.Color {
	switch level {
	case "low":
		return lipgloss.Color("42") // green
	case "medium":
		return lipgloss.Color("214") // orange
	case "high", "critical":
		return lipgloss.Color("196") // red
	default:
		return lipgloss.Color("244") // gray
	}
}

func severityStyle(severity string) lipgloss.Style {
	switch severity {
	case "critical":
		return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("196"))
	case "warning":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	case "suggestion":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("33"))
	case "praise":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	default:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	}
}

func (m ChatPanelModel) renderInput() string {
	// Analysis tab doesn't have text input
	if m.activeTab == ChatTabAnalysis {
		return lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")).
			Render("> ") + lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")).
			Italic(true).
			Render("press 'a' to analyze")
	}

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
		if m.activeTab == ChatTabComments && m.commentPosting {
			return prefix + lipgloss.NewStyle().
				Foreground(lipgloss.Color("244")).
				Italic(true).
				Render("posting comment...")
		}
		if m.activeTab == ChatTabChat && m.isWaiting {
			return prefix + lipgloss.NewStyle().
				Foreground(lipgloss.Color("244")).
				Italic(true).
				Render("waiting for response...")
		}
		return prefix + m.textInput.View()
	}

	hint := "press Enter to chat"
	if m.activeTab == ChatTabComments {
		hint = "press Enter to comment"
	}
	return prefix + lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		Italic(true).
		Render(hint)
}

// renderMarkdown renders markdown text with glamour for terminal display.
// Falls back to plain wordWrap if glamour fails.
func renderMarkdown(markdown string, width int) string {
	if width < 10 {
		width = 10
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return wordWrap(markdown, width)
	}
	out, err := r.Render(markdown)
	if err != nil {
		return wordWrap(markdown, width)
	}
	return strings.TrimSpace(out)
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
