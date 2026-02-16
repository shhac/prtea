package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
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

// ChatPanelModel manages the chat/analysis panel as a thin coordinator
// that delegates state and rendering to per-tab models.
type ChatPanelModel struct {
	// Shared UI components
	viewport  viewport.Model
	spinner   spinner.Model
	textInput textinput.Model
	md        MarkdownRenderer

	// Panel state
	chatMode  ChatMode
	activeTab ChatTab
	width     int
	height    int
	focused   bool
	ready     bool

	// Per-tab models
	chat     ChatTabModel
	analysis AnalysisTabModel
	comments CommentsTabModel
	review   ReviewTabModel
}

func NewChatPanelModel() ChatPanelModel {
	ti := textinput.New()
	ti.Placeholder = "Ask about this PR..."
	ti.CharLimit = 500

	return ChatPanelModel{
		spinner:   newLoadingSpinner(),
		textInput: ti,
		chatMode:  ChatModeNormal,
		activeTab: ChatTabChat,
		review:    NewReviewTabModel(),
	}
}

// contentWidth returns the inner content width used by tab renders.
func (m ChatPanelModel) contentWidth() int {
	w := m.width - 6
	if w < 10 {
		return 10
	}
	return w
}

// -- Public API (coordinator delegates to tab models) --

// SetActiveTab switches the active tab.
func (m *ChatPanelModel) SetActiveTab(tab ChatTab) {
	m.activeTab = tab
}

// IsAIReviewLoading returns whether the AI review is in progress.
func (m ChatPanelModel) IsAIReviewLoading() bool {
	return m.review.IsAIReviewLoading()
}

// SetStreamCheckpoint sets the checkpoint interval for streaming renderers.
func (m *ChatPanelModel) SetStreamCheckpoint(d time.Duration) {
	m.chat.chatStream.CheckpointInterval = d
	m.analysis.stream.CheckpointInterval = d
}

// SetDefaultReviewAction sets the default review action from config.
func (m *ChatPanelModel) SetDefaultReviewAction(action string) {
	m.review.SetDefaultAction(action)
}

// UpdateDefaultReviewAction updates the stored default without touching current state.
func (m *ChatPanelModel) UpdateDefaultReviewAction(action string) {
	m.review.UpdateDefaultAction(action)
}

// -- Analysis delegation --

// SetAnalysisLoading puts the analysis tab into loading state.
func (m *ChatPanelModel) SetAnalysisLoading() {
	m.analysis.SetLoading()
	m.refreshViewport()
}

// SetAnalysisResult sets the analysis result and clears loading state.
func (m *ChatPanelModel) SetAnalysisResult(result *claude.AnalysisResult) {
	m.analysis.SetResult(result)
	m.refreshViewport()
}

// SetAnalysisError sets an error message on the analysis tab.
func (m *ChatPanelModel) SetAnalysisError(err string) {
	m.analysis.SetError(err)
	m.refreshViewport()
}

// AppendAnalysisStreamChunk appends a text chunk during analysis streaming.
func (m *ChatPanelModel) AppendAnalysisStreamChunk(chunk string) {
	m.analysis.AppendStreamChunk(chunk)
	wasAtBottom := m.viewport.AtBottom()
	m.refreshViewport()
	if wasAtBottom {
		m.viewport.GotoBottom()
	}
}

// -- Chat delegation --

// AppendStreamChunk appends a text chunk during chat streaming.
// Only auto-scrolls if the user was already at the bottom.
func (m *ChatPanelModel) AppendStreamChunk(chunk string) {
	w := m.contentWidth()
	m.chat.chatStream.Append(chunk, func(s string) string {
		return m.md.RenderMarkdown(s, w)
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
	m.chat.AddResponse(content)
	wasAtBottom := m.viewport.AtBottom()
	m.refreshViewport()
	if wasAtBottom {
		m.viewport.GotoBottom()
	}
}

// SetChatError sets a chat error and clears the waiting state.
// Only auto-scrolls if the user was already at the bottom.
func (m *ChatPanelModel) SetChatError(err string) {
	m.chat.SetChatError(err)
	wasAtBottom := m.viewport.AtBottom()
	m.refreshViewport()
	if wasAtBottom {
		m.viewport.GotoBottom()
	}
}

// ClearChat resets chat messages and state for a new PR.
func (m *ChatPanelModel) ClearChat() {
	m.chat.ClearChat()
	m.refreshViewport()
}

// RestoreMessages restores chat history from a previous session.
func (m *ChatPanelModel) RestoreMessages(msgs []claude.ChatMessage) {
	m.chat.RestoreMessages(msgs)
	m.refreshViewport()
}

// -- Comments delegation --

// SetCommentsLoading puts the comments tab into loading state.
func (m *ChatPanelModel) SetCommentsLoading() {
	m.comments.SetLoading()
	m.refreshViewport()
}

// SetComments sets the comments data and clears loading state.
func (m *ChatPanelModel) SetComments(comments []github.Comment, inline []github.InlineComment) {
	m.comments.SetComments(comments, inline)
	m.refreshViewport()
}

// SetCommentsError sets an error message on the comments tab.
func (m *ChatPanelModel) SetCommentsError(err string) {
	m.comments.SetError(err)
	m.refreshViewport()
}

// SetCommentPosted clears the posting state after a comment post attempt.
func (m *ChatPanelModel) SetCommentPosted(err error) {
	m.comments.SetPosted(err)
	m.refreshViewport()
	m.viewport.GotoBottom()
}

// ClearComments resets comments state for a new PR.
func (m *ChatPanelModel) ClearComments() {
	m.comments.Clear()
	m.refreshViewport()
}

// -- Review delegation --

// ClearReview resets review state for a new PR.
func (m *ChatPanelModel) ClearReview() {
	m.review.Clear()
}

// SetAIReviewLoading puts the review tab into AI review loading state.
func (m *ChatPanelModel) SetAIReviewLoading() {
	m.review.SetAIReviewLoading()
}

// SetAIReviewResult pre-populates the review form with AI-generated content.
func (m *ChatPanelModel) SetAIReviewResult(result *claude.ReviewAnalysis) {
	m.review.SetAIReviewResult(result)
}

// SetAIReviewError sets an error message for AI review generation.
func (m *ChatPanelModel) SetAIReviewError(err string) {
	m.review.SetAIReviewError(err)
}

// ClearAIReview resets AI review state.
func (m *ChatPanelModel) ClearAIReview() {
	m.review.ClearAIReview()
}

// SetPendingCommentCount sets the number of pending inline comments.
func (m *ChatPanelModel) SetPendingCommentCount(n int) {
	m.review.SetPendingCommentCount(n)
}

// SetReviewSubmitted clears the submitting state. On success, also resets the form.
func (m *ChatPanelModel) SetReviewSubmitted(err error) {
	m.review.SetSubmitted(err)
}

// -- Layout --

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
	m.review.SetWidth(innerWidth)

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
		m.review.Blur()
	}
}

// -- Update --

func (m ChatPanelModel) Update(msg tea.Msg) (ChatPanelModel, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		if m.analysis.loading || m.comments.loading || m.review.aiLoading {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
		return m, nil
	case tea.KeyMsg:
		if m.activeTab == ChatTabReview {
			return m.updateReviewTab(msg)
		}
		if m.chatMode == ChatModeInsert {
			return m.updateInsertMode(msg)
		}
		return m.updateNormalMode(msg)
	}

	if m.chatMode == ChatModeNormal && m.ready {
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m ChatPanelModel) updateInsertMode(msg tea.KeyMsg) (ChatPanelModel, tea.Cmd) {
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
			if !m.comments.IsPosting() {
				m.comments.SetPosting(true)
				m.refreshViewport()
				m.viewport.GotoBottom()
				return m, func() tea.Msg { return CommentPostMsg{Body: userMsg} }
			}
			return m, nil
		}

		// Chat tab send
		if !m.chat.IsWaiting() {
			m.chat.SetWaiting(userMsg)
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

func (m ChatPanelModel) updateNormalMode(msg tea.KeyMsg) (ChatPanelModel, tea.Cmd) {
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
	return m, nil
}

// updateReviewTab handles key events when the Review tab is active.
// Tab switching is intercepted here; other keys are delegated to the ReviewTabModel.
func (m ChatPanelModel) updateReviewTab(msg tea.KeyMsg) (ChatPanelModel, tea.Cmd) {
	// Tab switching in normal mode (not when textarea is focused)
	if !m.review.IsFocused() {
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
	}

	var cmd tea.Cmd
	m.review, cmd = m.review.Update(msg)
	return m, cmd
}

// -- Viewport refresh --

func (m *ChatPanelModel) refreshViewport() {
	if !m.ready || m.activeTab == ChatTabReview {
		return
	}
	w := m.contentWidth()
	sv := m.spinner.View()
	var content string
	switch m.activeTab {
	case ChatTabAnalysis:
		content = m.analysis.Render(w, sv)
	case ChatTabComments:
		content = m.comments.Render(w, sv, &m.md)
	default:
		content = m.chat.Render(w, &m.md)
	}
	m.viewport.SetContent(content)
}

// -- View --

func (m ChatPanelModel) View() string {
	header := m.renderHeader()

	if m.activeTab == ChatTabReview {
		reviewContent := m.review.Render(m.contentWidth(), m.spinner.View())
		inner := lipgloss.JoinVertical(lipgloss.Left, header, reviewContent)
		isInsert := m.review.IsFocused()
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

	chatLabel := "Chat"
	if n := m.chat.MessageCount(); n > 0 {
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

func (m ChatPanelModel) renderInputSeparator() string {
	w := m.width - 6
	if w < 1 {
		w = 1
	}
	sepColor := lipgloss.Color("238")
	if m.chatMode == ChatModeInsert {
		sepColor = lipgloss.Color("42")
	}
	return lipgloss.NewStyle().
		Foreground(sepColor).
		Render(strings.Repeat("─", w))
}

func (m ChatPanelModel) renderInput() string {
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

		if m.activeTab == ChatTabComments && m.comments.IsPosting() {
			return prefix + lipgloss.NewStyle().
				Foreground(lipgloss.Color("244")).
				Italic(true).
				Render("posting comment...")
		}
		if m.activeTab == ChatTabChat && m.chat.IsWaiting() {
			return prefix + lipgloss.NewStyle().
				Foreground(lipgloss.Color("244")).
				Italic(true).
				Render("waiting for response...")
		}
		return prefix + m.textInput.View()
	}

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
