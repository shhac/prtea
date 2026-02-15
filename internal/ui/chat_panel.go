package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
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
	ChatTabReview
)

// ReviewFocus tracks which component has focus within the Review tab.
type ReviewFocus int

const (
	ReviewFocusTextArea ReviewFocus = iota
	ReviewFocusRadio
	ReviewFocusSubmit
)

// ChatPanelModel manages the chat/analysis panel.
type ChatPanelModel struct {
	viewport  viewport.Model
	spinner   spinner.Model
	textInput textinput.Model
	chatMode  ChatMode
	activeTab ChatTab
	messages  []chatMessage
	width     int
	height    int
	focused   bool
	ready     bool

	// Chat state
	isWaiting bool   // true while waiting for Claude response
	chatError string // last chat error message
	chatStream StreamRenderer // progressive streaming for chat

	// Analysis state
	analysisResult  *claude.AnalysisResult
	analysisLoading bool
	analysisError   string
	analysisStream  StreamRenderer // progressive streaming for analysis

	// Comments state
	comments        []github.Comment
	inlineComments  []github.InlineComment
	commentsLoading bool
	commentsError   string
	commentPosting  bool // true while posting a comment

	// Review tab state
	reviewTextArea      textarea.Model
	reviewAction        ReviewAction // the confirmed selection (what gets submitted)
	reviewRadioFocus    int          // which radio option has focus (0=Approve, 1=Comment, 2=RequestChanges)
	reviewFocus         ReviewFocus
	reviewSubmitting    bool
	defaultReviewAction ReviewAction // configured default from settings

	// AI review state
	aiReviewResult   *claude.ReviewAnalysis
	aiReviewLoading  bool
	aiReviewError    string

	// Pending inline comment count (set by app)
	pendingCommentCount int

	// Cached glamour renderer (recreated when width changes)
	glamourRenderer *glamour.TermRenderer
	glamourWidth    int
}

type chatMessage struct {
	role    string // "user" or "assistant"
	content string
}

func NewChatPanelModel() ChatPanelModel {
	ti := textinput.New()
	ti.Placeholder = "Ask about this PR..."
	ti.CharLimit = 500

	ta := textarea.New()
	ta.Placeholder = "Review body (optional for approve)..."
	ta.CharLimit = 65535
	ta.SetHeight(5)
	ta.ShowLineNumbers = false
	ta.Blur()

	return ChatPanelModel{
		spinner:      newLoadingSpinner(),
		textInput:    ti,
		chatMode:     ChatModeNormal,
		activeTab:    ChatTabChat,
		reviewTextArea:   ta,
		reviewAction:     ReviewComment,
		reviewRadioFocus: 1, // matches ReviewComment default
	}
}

// SetStreamCheckpoint sets the checkpoint interval for streaming renderers.
func (m *ChatPanelModel) SetStreamCheckpoint(d time.Duration) {
	m.chatStream.CheckpointInterval = d
	m.analysisStream.CheckpointInterval = d
}

// SetDefaultReviewAction sets the default review action from config and
// applies it to the current review state. Use at initialization time.
func (m *ChatPanelModel) SetDefaultReviewAction(action string) {
	m.parseDefaultReviewAction(action)
	m.reviewAction = m.defaultReviewAction
	m.reviewRadioFocus = int(m.defaultReviewAction)
}

// UpdateDefaultReviewAction updates the stored default without touching the
// current review state. Use when config changes mid-session so an in-progress
// review is not disrupted.
func (m *ChatPanelModel) UpdateDefaultReviewAction(action string) {
	m.parseDefaultReviewAction(action)
}

func (m *ChatPanelModel) parseDefaultReviewAction(action string) {
	switch action {
	case "approve":
		m.defaultReviewAction = ReviewApprove
	case "request_changes":
		m.defaultReviewAction = ReviewRequestChanges
	default:
		m.defaultReviewAction = ReviewComment
	}
}

func (m ChatPanelModel) Update(msg tea.Msg) (ChatPanelModel, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		if m.analysisLoading || m.commentsLoading || m.aiReviewLoading {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
		return m, nil
	case tea.KeyMsg:
		// Review tab has its own input handling
		if m.activeTab == ChatTabReview {
			return m.updateReviewTab(msg)
		}

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
			if m.activeTab < ChatTabReview {
				m.activeTab++
			}
			m.refreshViewport()
			return m, nil
		case key.Matches(msg, ChatKeys.NewChat):
			if m.activeTab == ChatTabChat {
				return m, func() tea.Msg { return ChatClearMsg{} }
			}
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
	// Account for borders (2), header (2), separator (1), input line (1), padding
	innerWidth := width - 4
	innerHeight := height - 8
	if innerWidth < 1 {
		innerWidth = 1
	}
	if innerHeight < 1 {
		innerHeight = 1
	}

	m.textInput.Width = innerWidth - 4
	m.reviewTextArea.SetWidth(innerWidth)

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
	if !focused {
		m.reviewTextArea.Blur()
	}
}

// SetAnalysisLoading puts the analysis tab into loading state.
func (m *ChatPanelModel) SetAnalysisLoading() {
	m.analysisLoading = true
	m.analysisError = ""
	m.analysisResult = nil
	m.analysisStream.Reset()
	m.refreshViewport()
}

// SetAnalysisResult sets the analysis result and clears loading state.
func (m *ChatPanelModel) SetAnalysisResult(result *claude.AnalysisResult) {
	m.analysisResult = result
	m.analysisLoading = false
	m.analysisError = ""
	m.analysisStream.Reset()
	m.refreshViewport()
}

// SetAnalysisError sets an error message on the analysis tab.
func (m *ChatPanelModel) SetAnalysisError(err string) {
	m.analysisError = err
	m.analysisLoading = false
	m.analysisResult = nil
	m.analysisStream.Reset()
	m.refreshViewport()
}

// AppendStreamChunk appends a text chunk during chat streaming and refreshes the viewport.
// Only auto-scrolls if the user was already at the bottom, so scrolling up
// to read earlier messages is not disrupted by incoming tokens.
func (m *ChatPanelModel) AppendStreamChunk(chunk string) {
	innerWidth := m.width - 6
	if innerWidth < 10 {
		innerWidth = 10
	}
	m.chatStream.Append(chunk, func(s string) string {
		return m.renderMarkdown(s, innerWidth)
	})
	wasAtBottom := m.viewport.AtBottom()
	m.refreshViewport()
	if wasAtBottom {
		m.viewport.GotoBottom()
	}
}

// AppendAnalysisStreamChunk appends a text chunk during analysis streaming.
func (m *ChatPanelModel) AppendAnalysisStreamChunk(chunk string) {
	innerWidth := m.width - 6
	if innerWidth < 10 {
		innerWidth = 10
	}
	m.analysisStream.Append(chunk, func(s string) string {
		return m.renderMarkdown(s, innerWidth)
	})
	wasAtBottom := m.viewport.AtBottom()
	m.refreshViewport()
	if wasAtBottom {
		m.viewport.GotoBottom()
	}
}

// AddResponse appends a Claude response and clears the waiting state.
// Only auto-scrolls if the user was already at the bottom.
func (m *ChatPanelModel) AddResponse(content string) {
	m.messages = append(m.messages, chatMessage{
		role:    "assistant",
		content: content,
	})
	m.isWaiting = false
	m.chatError = ""
	m.chatStream.Reset()
	wasAtBottom := m.viewport.AtBottom()
	m.refreshViewport()
	if wasAtBottom {
		m.viewport.GotoBottom()
	}
}

// SetChatError sets a chat error and clears the waiting state.
// Only auto-scrolls if the user was already at the bottom.
func (m *ChatPanelModel) SetChatError(err string) {
	m.chatError = err
	m.isWaiting = false
	m.chatStream.Reset()
	wasAtBottom := m.viewport.AtBottom()
	m.refreshViewport()
	if wasAtBottom {
		m.viewport.GotoBottom()
	}
}

// ClearChat resets chat messages and state for a new PR.
func (m *ChatPanelModel) ClearChat() {
	m.messages = nil
	m.isWaiting = false
	m.chatError = ""
	m.chatStream.Reset()
	m.refreshViewport()
}

// RestoreMessages restores chat history from a previous session (e.g., loaded from disk).
func (m *ChatPanelModel) RestoreMessages(msgs []claude.ChatMessage) {
	m.messages = make([]chatMessage, len(msgs))
	for i, msg := range msgs {
		m.messages[i] = chatMessage{role: msg.Role, content: msg.Content}
	}
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

// ClearReview resets review state for a new PR.
func (m *ChatPanelModel) ClearReview() {
	m.reviewTextArea.Reset()
	m.reviewAction = m.defaultReviewAction
	m.reviewRadioFocus = int(m.defaultReviewAction)
	m.reviewFocus = ReviewFocusTextArea
	m.reviewSubmitting = false
	m.reviewTextArea.Blur()
	m.aiReviewResult = nil
	m.aiReviewLoading = false
	m.aiReviewError = ""
	m.pendingCommentCount = 0
}

// SetAIReviewLoading puts the review tab into AI review loading state.
func (m *ChatPanelModel) SetAIReviewLoading() {
	m.aiReviewLoading = true
	m.aiReviewError = ""
	m.aiReviewResult = nil
}

// SetAIReviewResult pre-populates the review form with AI-generated content.
func (m *ChatPanelModel) SetAIReviewResult(result *claude.ReviewAnalysis) {
	m.aiReviewLoading = false
	m.aiReviewError = ""
	m.aiReviewResult = result

	// Pre-populate the review form
	m.reviewTextArea.SetValue(result.Body)

	switch result.Action {
	case "approve":
		m.reviewAction = ReviewApprove
		m.reviewRadioFocus = int(ReviewApprove)
	case "request_changes":
		m.reviewAction = ReviewRequestChanges
		m.reviewRadioFocus = int(ReviewRequestChanges)
	default:
		m.reviewAction = ReviewComment
		m.reviewRadioFocus = int(ReviewComment)
	}

	m.reviewFocus = ReviewFocusTextArea
}

// SetAIReviewError sets an error message for AI review generation.
func (m *ChatPanelModel) SetAIReviewError(err string) {
	m.aiReviewLoading = false
	m.aiReviewError = err
	m.aiReviewResult = nil
}

// ClearAIReview resets AI review state.
func (m *ChatPanelModel) ClearAIReview() {
	m.aiReviewResult = nil
	m.aiReviewLoading = false
	m.aiReviewError = ""
}

// SetPendingCommentCount sets the number of pending inline comments for display in the review tab.
func (m *ChatPanelModel) SetPendingCommentCount(n int) {
	m.pendingCommentCount = n
}

// SetReviewSubmitted clears the submitting state. On success, also resets the form.
func (m *ChatPanelModel) SetReviewSubmitted(err error) {
	m.reviewSubmitting = false
	if err == nil {
		m.reviewTextArea.Reset()
		m.reviewAction = m.defaultReviewAction
		m.reviewRadioFocus = int(m.defaultReviewAction)
		m.reviewFocus = ReviewFocusTextArea
		m.reviewTextArea.Blur()
		m.aiReviewResult = nil
		m.aiReviewLoading = false
		m.aiReviewError = ""
	}
}

func (m *ChatPanelModel) refreshViewport() {
	if !m.ready {
		return
	}
	if m.activeTab == ChatTabReview {
		// Review tab doesn't use viewport for content
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

	if m.activeTab == ChatTabReview {
		reviewContent := m.renderReview()
		inner := lipgloss.JoinVertical(lipgloss.Left, header, reviewContent)
		isInsert := m.reviewTextArea.Focused()
		style := panelStyle(m.focused, isInsert, m.width-2, m.height-2)
		return style.Render(inner)
	}

	var content string
	if m.ready {
		content = m.viewport.View()
	} else {
		content = "Loading..."
	}

	separator := m.renderInputSeparator()
	input := m.renderInput()
	parts := []string{header, content}
	if indicator := scrollIndicator(m.viewport, m.width-4); indicator != "" {
		parts = append(parts, indicator)
	}
	parts = append(parts, separator, input)
	inner := lipgloss.JoinVertical(lipgloss.Left, parts...)

	isInsert := m.chatMode == ChatModeInsert
	style := panelStyle(m.focused, isInsert, m.width-2, m.height-2)
	return style.Render(inner)
}

func (m ChatPanelModel) renderHeader() string {
	var tabs []string

	// Show message count on Chat tab when there are messages
	chatLabel := "Chat"
	if n := len(m.messages); n > 0 {
		chatLabel = fmt.Sprintf("Chat (%d)", n)
	}

	tabNames := []struct {
		tab  ChatTab
		name string
	}{
		{ChatTabChat, chatLabel},
		{ChatTabAnalysis, "Analysis"},
		{ChatTabComments, "Comments"},
		{ChatTabReview, "Review"},
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

func (m *ChatPanelModel) renderMessages() string {
	if len(m.messages) == 0 && !m.isWaiting && m.chatError == "" {
		return renderEmptyState("No messages yet", "Press Enter to start chatting")
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
			b.WriteString(m.renderMarkdown(msg.content, innerWidth))
		} else {
			b.WriteString(wordWrap(msg.content, innerWidth))
		}
	}

	if m.isWaiting {
		if len(m.messages) > 0 {
			b.WriteString("\n\n")
		}
		if m.chatStream.HasContent() {
			b.WriteString(chatAssistantStyle.Render("Claude:"))
			b.WriteString("\n")
			b.WriteString(m.chatStream.View(wordWrap, innerWidth))
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
			Render(formatUserError(m.chatError)))
	}

	return b.String()
}

func (m ChatPanelModel) renderAnalysis() string {
	if m.analysisLoading {
		innerWidth := m.width - 6
		if innerWidth < 10 {
			innerWidth = 10
		}
		if m.analysisStream.HasContent() {
			var b strings.Builder
			b.WriteString(lipgloss.NewStyle().
				Foreground(lipgloss.Color("244")).
				Render(m.spinner.View() + " Analyzing PR with Claude..."))
			b.WriteString("\n\n")
			b.WriteString(m.analysisStream.View(wordWrap, innerWidth))
			return b.String()
		}
		return lipgloss.NewStyle().
			Foreground(lipgloss.Color("244")).
			Padding(1, 0).
			Render(m.spinner.View() + " Analyzing PR with Claude...\n\nThis may take a minute.")
	}
	if m.analysisError != "" {
		return renderErrorWithHint(formatUserError(m.analysisError), "Press 'a' to try again")
	}
	if m.analysisResult == nil {
		return renderEmptyState("No analysis yet", "Press 'a' to analyze this PR with Claude")
	}
	return m.renderAnalysisResult()
}

func (m *ChatPanelModel) renderComments() string {
	if m.commentsLoading {
		return lipgloss.NewStyle().
			Foreground(lipgloss.Color("244")).
			Padding(1, 0).
			Render(m.spinner.View() + " Loading comments...")
	}
	if m.commentsError != "" {
		return renderErrorWithHint(formatUserError(m.commentsError), "Press r to refresh")
	}
	if len(m.comments) == 0 && len(m.inlineComments) == 0 {
		return renderEmptyState("No comments on this PR", "Press Enter to be the first to comment")
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
			b.WriteString(m.renderMarkdown(c.Body, innerWidth))
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
			b.WriteString(m.renderMarkdown(c.Body, innerWidth))
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

func (m ChatPanelModel) renderInputSeparator() string {
	innerWidth := m.width - 6
	if innerWidth < 1 {
		innerWidth = 1
	}
	sepColor := lipgloss.Color("238")
	if m.chatMode == ChatModeInsert {
		sepColor = lipgloss.Color("42")
	}
	return lipgloss.NewStyle().
		Foreground(sepColor).
		Render(strings.Repeat("─", innerWidth))
}

func (m ChatPanelModel) renderInput() string {
	// Review and Analysis tabs don't use the shared text input
	if m.activeTab == ChatTabReview {
		return ""
	}
	if m.activeTab == ChatTabAnalysis {
		dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Italic(true)
		return dimStyle.Render("> press 'a' to analyze")
	}

	if m.chatMode == ChatModeInsert {
		prefix := lipgloss.NewStyle().
			Foreground(lipgloss.Color("42")).
			Bold(true).
			Render("> ")

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

	// Normal mode — show hint with dimmed styling
	prefix := lipgloss.NewStyle().
		Foreground(lipgloss.Color("245")).
		Render("> ")
	hint := "Enter to chat"
	if m.activeTab == ChatTabComments {
		hint = "Enter to comment"
	}
	return prefix + lipgloss.NewStyle().
		Foreground(lipgloss.Color("245")).
		Italic(true).
		Render(hint)
}

// updateReviewTab handles all key events when the Review tab is active.
func (m ChatPanelModel) updateReviewTab(msg tea.KeyMsg) (ChatPanelModel, tea.Cmd) {
	// When textarea is focused, it captures all keys except ESC and Tab
	if m.reviewTextArea.Focused() {
		switch msg.String() {
		case "esc":
			m.reviewTextArea.Blur()
			return m, func() tea.Msg { return ModeChangedMsg{Mode: ChatModeNormal} }
		case "tab":
			m.reviewTextArea.Blur()
			m.reviewFocus = ReviewFocusRadio
			return m, func() tea.Msg { return ModeChangedMsg{Mode: ChatModeNormal} }
		default:
			var cmd tea.Cmd
			m.reviewTextArea, cmd = m.reviewTextArea.Update(msg)
			return m, cmd
		}
	}

	// Normal mode within review tab
	switch {
	case key.Matches(msg, ChatKeys.PrevTab):
		if m.activeTab > ChatTabChat {
			m.activeTab--
		}
		m.refreshViewport()
		return m, nil
	case key.Matches(msg, ChatKeys.NextTab):
		if m.activeTab < ChatTabReview {
			m.activeTab++
		}
		m.refreshViewport()
		return m, nil
	}

	switch m.reviewFocus {
	case ReviewFocusTextArea:
		switch msg.String() {
		case "enter":
			m.reviewTextArea.Focus()
			return m, func() tea.Msg { return ModeChangedMsg{Mode: ChatModeInsert} }
		case "tab", "j", "down":
			m.reviewFocus = ReviewFocusRadio
			m.reviewRadioFocus = int(m.reviewAction) // start focus on current selection
			return m, nil
		}

	case ReviewFocusRadio:
		switch msg.String() {
		case "j", "down":
			if m.reviewRadioFocus < int(ReviewRequestChanges) {
				m.reviewRadioFocus++
			} else {
				m.reviewFocus = ReviewFocusSubmit
			}
			return m, nil
		case "k", "up":
			if m.reviewRadioFocus > int(ReviewApprove) {
				m.reviewRadioFocus--
			} else {
				m.reviewFocus = ReviewFocusTextArea
			}
			return m, nil
		case "enter", " ":
			m.reviewAction = ReviewAction(m.reviewRadioFocus)
			return m, nil
		case "tab":
			m.reviewFocus = ReviewFocusSubmit
			return m, nil
		case "shift+tab":
			m.reviewFocus = ReviewFocusTextArea
			return m, nil
		}

	case ReviewFocusSubmit:
		switch msg.String() {
		case "enter":
			if m.reviewSubmitting {
				return m, nil
			}
			body := strings.TrimSpace(m.reviewTextArea.Value())
			// Validate: request changes and comment require a body
			if m.reviewAction == ReviewRequestChanges && body == "" {
				return m, func() tea.Msg {
					return ReviewValidationMsg{Message: "Review body is required for Request Changes"}
				}
			}
			if m.reviewAction == ReviewComment && body == "" {
				return m, func() tea.Msg {
					return ReviewValidationMsg{Message: "Review body is required for Comment"}
				}
			}
			m.reviewSubmitting = true
			action := m.reviewAction
			// Inline comments are managed by app.pendingInlineComments
			return m, func() tea.Msg {
				return ReviewSubmitMsg{Action: action, Body: body}
			}
		case "tab":
			m.reviewFocus = ReviewFocusTextArea
			return m, nil
		case "shift+tab":
			m.reviewFocus = ReviewFocusRadio
			m.reviewRadioFocus = int(ReviewRequestChanges) // focus last radio option
			return m, nil
		case "k", "up":
			m.reviewFocus = ReviewFocusRadio
			m.reviewRadioFocus = int(ReviewRequestChanges) // focus last radio option
			return m, nil
		}
	}

	return m, nil
}

// renderReview renders the Review tab content: textarea, radio options, submit button.
func (m ChatPanelModel) renderReview() string {
	innerWidth := m.width - 6
	if innerWidth < 10 {
		innerWidth = 10
	}

	var b strings.Builder

	// AI review status banner
	if m.aiReviewLoading {
		b.WriteString(lipgloss.NewStyle().
			Foreground(lipgloss.Color("244")).
			Render(m.spinner.View() + " Generating AI review..."))
		b.WriteString("\n\n")
	} else if m.aiReviewError != "" {
		b.WriteString(lipgloss.NewStyle().
			Foreground(lipgloss.Color("196")).
			Bold(true).
			Render("AI review failed: " + formatUserError(m.aiReviewError)))
		b.WriteString("\n")
		b.WriteString(lipgloss.NewStyle().
			Foreground(lipgloss.Color("244")).
			Italic(true).
			Render("Press R to retry"))
		b.WriteString("\n\n")
	} else if m.aiReviewResult != nil {
		badge := lipgloss.NewStyle().
			Foreground(lipgloss.Color("0")).
			Background(lipgloss.Color("75")).
			Bold(true).
			Padding(0, 1).
			Render("AI REVIEW")
		b.WriteString(badge)
		b.WriteString("\n\n")
	}

	// Pending inline comment count
	if m.pendingCommentCount > 0 {
		countText := fmt.Sprintf("📝 %d pending inline comment", m.pendingCommentCount)
		if m.pendingCommentCount != 1 {
			countText += "s"
		}
		countText += " will be submitted"
		b.WriteString(lipgloss.NewStyle().
			Foreground(lipgloss.Color("214")).
			Render(countText))
		b.WriteString("\n\n")
	}

	// 1. Review body textarea
	label := reviewLabelStyle.Render("Review Body")
	if m.reviewFocus == ReviewFocusTextArea && !m.reviewTextArea.Focused() {
		label += lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Italic(true).Render("  press Enter to edit")
	}
	b.WriteString(label)
	b.WriteString("\n")
	b.WriteString(m.reviewTextArea.View())
	b.WriteString("\n\n")

	// 2. Review action radio group
	b.WriteString(reviewLabelStyle.Render("Action"))
	b.WriteString("\n")

	actions := []struct {
		action ReviewAction
		label  string
		active lipgloss.Style
	}{
		{ReviewApprove, "Approve", reviewApproveStyle},
		{ReviewComment, "Comment", reviewCommentStyle},
		{ReviewRequestChanges, "Request Changes", reviewRequestChangesStyle},
	}

	for i, a := range actions {
		// Selection indicator: (●) for selected action, ( ) otherwise
		indicator := "  ( ) "
		if m.reviewAction == a.action {
			indicator = "  (●) "
		}

		// Focus cursor: ▸ prefix when this option has focus
		isFocused := m.reviewFocus == ReviewFocusRadio && m.reviewRadioFocus == i
		if isFocused {
			indicator = "▸ " + indicator[2:] // replace leading spaces with cursor
		}

		var line string
		if m.reviewAction == a.action {
			line = indicator + a.active.Render(a.label)
		} else {
			line = indicator + reviewOptionDimStyle.Render(a.label)
		}

		// Bold the focused option
		if isFocused {
			line = lipgloss.NewStyle().Bold(true).Render(line)
		}

		b.WriteString(line)
		b.WriteString("\n")
	}
	b.WriteString("\n")

	// 3. Submit button
	actionLabels := map[ReviewAction]string{
		ReviewApprove:        "Approve",
		ReviewComment:        "Comment",
		ReviewRequestChanges: "Request Changes",
	}

	buttonText := fmt.Sprintf("[ Submit: %s ]", actionLabels[m.reviewAction])
	if m.reviewSubmitting {
		buttonText = "[ Submitting... ]"
	}

	if m.reviewFocus == ReviewFocusSubmit && !m.reviewSubmitting {
		// Focused submit button gets the action's color
		var style lipgloss.Style
		switch m.reviewAction {
		case ReviewApprove:
			style = reviewSubmitFocusedStyle.
				Foreground(lipgloss.Color("0")).
				Background(lipgloss.Color("42"))
		case ReviewRequestChanges:
			style = reviewSubmitFocusedStyle.
				Foreground(lipgloss.Color("255")).
				Background(lipgloss.Color("196"))
		default:
			style = reviewSubmitFocusedStyle.
				Foreground(lipgloss.Color("252")).
				Background(lipgloss.Color("62"))
		}
		b.WriteString("  " + style.Render(buttonText))
	} else {
		b.WriteString("  " + reviewSubmitDimStyle.Render(buttonText))
	}

	return b.String()
}

// getOrCreateRenderer returns a cached glamour renderer for the given width,
// creating a new one only when the width changes.
func (m *ChatPanelModel) getOrCreateRenderer(width int) *glamour.TermRenderer {
	if m.glamourRenderer != nil && m.glamourWidth == width {
		return m.glamourRenderer
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return nil
	}
	m.glamourRenderer = r
	m.glamourWidth = width
	return r
}

// renderMarkdown renders markdown text with glamour for terminal display.
// Uses a cached renderer per width to avoid re-creating it on every call.
// Falls back to plain wordWrap if glamour fails.
func (m *ChatPanelModel) renderMarkdown(markdown string, width int) string {
	if width < 10 {
		width = 10
	}
	r := m.getOrCreateRenderer(width)
	if r == nil {
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
