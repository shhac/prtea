package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/shhac/prtea/internal/claude"
)

// CommentOverlayModel renders a centered overlay showing diff context,
// a scrollable comment thread, and a reply input.
type CommentOverlayModel struct {
	viewport viewport.Model
	textarea textarea.Model
	visible  bool
	composing bool // true when textarea is focused
	ready     bool

	// Submit mode
	postImmediately bool // true = post reply now; false = add to pending review

	// Terminal dimensions (for centering)
	width  int
	height int

	// Comment target
	targetPath string
	targetLine int
	diffCtx    string // pre-rendered diff context lines

	// Comment data
	ghThreads       []ghCommentThread
	aiComments      []claude.InlineReviewComment
	pendingComments []PendingInlineComment

	// Reply target: root comment ID for the first GitHub thread (0 if none)
	replyTargetID int64
}

func NewCommentOverlayModel() CommentOverlayModel {
	ta := textarea.New()
	ta.Placeholder = "Write a comment..."
	ta.CharLimit = 65535
	ta.SetHeight(3)
	ta.ShowLineNumbers = false
	ta.Blur()
	return CommentOverlayModel{textarea: ta}
}

// Show opens the overlay with the given comment context.
func (m *CommentOverlayModel) Show(msg ShowCommentOverlayMsg) tea.Cmd {
	m.visible = true
	m.composing = false
	m.targetPath = msg.Path
	m.targetLine = msg.Line
	m.ghThreads = msg.GHThreads
	m.aiComments = msg.AIComments
	m.pendingComments = msg.PendingComments
	m.textarea.SetValue("")

	// Determine reply target and default submit mode
	if len(msg.GHThreads) > 0 {
		m.replyTargetID = msg.GHThreads[0].Root.ID
		m.postImmediately = true
	} else {
		m.replyTargetID = 0
		m.postImmediately = false
	}

	// Build diff context
	m.diffCtx = m.renderDiffContext(msg.DiffLines, msg.TargetLineInCtx)

	// Rebuild thread content in viewport
	m.refreshContent()
	m.viewport.GotoTop()
	return nil
}

// Hide dismisses the overlay.
func (m *CommentOverlayModel) Hide() {
	m.visible = false
	m.composing = false
	m.textarea.Blur()
}

// IsVisible returns whether the overlay is currently shown.
func (m CommentOverlayModel) IsVisible() bool {
	return m.visible
}

// SetSize updates terminal dimensions for centering and viewport sizing.
func (m *CommentOverlayModel) SetSize(termWidth, termHeight int) {
	m.width = termWidth
	m.height = termHeight
	_, vpH := m.viewportDimensions()
	vpW := m.innerWidth()
	if !m.ready {
		m.viewport = viewport.New(vpW, vpH)
		m.ready = true
	} else {
		m.viewport.Width = vpW
		m.viewport.Height = vpH
	}
	if m.visible {
		m.refreshContent()
	}
}

func (m CommentOverlayModel) Update(msg tea.Msg) (CommentOverlayModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.composing {
			return m.updateComposing(msg)
		}
		return m.updateViewing(msg)
	}
	// Pass non-key messages to textarea (cursor blink, etc.)
	if m.composing {
		var cmd tea.Cmd
		m.textarea, cmd = m.textarea.Update(msg)
		return m, cmd
	}
	return m, nil
}

// updateViewing handles keys when scrolling the thread (textarea not focused).
func (m CommentOverlayModel) updateViewing(msg tea.KeyMsg) (CommentOverlayModel, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		m.Hide()
		return m, func() tea.Msg { return CommentOverlayClosedMsg{} }
	case "i", "enter":
		m.composing = true
		cmd := m.textarea.Focus()
		return m, cmd
	default:
		// Scroll the thread viewport
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd
	}
}

// updateComposing handles keys when the textarea is focused.
func (m CommentOverlayModel) updateComposing(msg tea.KeyMsg) (CommentOverlayModel, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.composing = false
		m.textarea.Blur()
		return m, nil
	case "tab":
		if m.replyTargetID > 0 {
			m.postImmediately = !m.postImmediately
		}
		return m, nil
	case "ctrl+s":
		body := strings.TrimSpace(m.textarea.Value())
		if body == "" {
			return m, nil
		}
		m.Hide()
		if m.postImmediately && m.replyTargetID > 0 {
			commentID := m.replyTargetID
			return m, func() tea.Msg {
				return InlineCommentReplyMsg{CommentID: commentID, Body: body}
			}
		}
		path := m.targetPath
		line := m.targetLine
		return m, func() tea.Msg {
			return InlineCommentAddMsg{Path: path, Line: line, Body: body}
		}
	}
	var cmd tea.Cmd
	m.textarea, cmd = m.textarea.Update(msg)
	return m, cmd
}

func (m CommentOverlayModel) View() string {
	if !m.visible {
		return ""
	}

	overlayW, overlayH := m.overlayDimensions()
	innerW := m.innerWidth()

	// Title
	titleText := fmt.Sprintf(" 💬 %s:%d ", m.targetPath, m.targetLine)
	title := commentOverlayTitleStyle.Render(titleText)
	titleLine := lipgloss.PlaceHorizontal(innerW, lipgloss.Left, title)

	// Diff context
	ctx := m.diffCtx

	// Separator
	sep := commentOverlaySepStyle.Render(strings.Repeat("─", min(innerW, 50)))

	// Thread viewport
	thread := m.viewport.View()

	// Scroll indicator
	var scrollInd string
	if indicator := scrollIndicator(m.viewport, innerW); indicator != "" {
		scrollInd = indicator
	}

	// Textarea
	taView := m.textarea.View()

	// Footer
	footer := m.renderFooter(innerW)

	// Assemble parts
	parts := []string{titleLine, "", ctx, sep, thread}
	if scrollInd != "" {
		parts = append(parts, scrollInd)
	}
	parts = append(parts, sep, taView, "", footer)
	box := lipgloss.JoinVertical(lipgloss.Left, parts...)

	overlayStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Padding(0, 1).
		Width(overlayW - 2).
		Height(overlayH - 2)

	rendered := overlayStyle.Render(box)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, rendered)
}

// overlayDimensions returns the outer box dimensions.
func (m CommentOverlayModel) overlayDimensions() (width, height int) {
	width = int(float64(m.width) * 0.65)
	height = int(float64(m.height) * 0.80)
	if width < 50 {
		width = min(50, m.width)
	}
	if height < 15 {
		height = min(15, m.height)
	}
	return width, height
}

// innerWidth returns the usable content width inside the overlay box.
func (m CommentOverlayModel) innerWidth() int {
	ow, _ := m.overlayDimensions()
	w := ow - 6 // border (2) + padding (2) + margin (2)
	if w < 10 {
		w = 10
	}
	return w
}

// viewportDimensions returns the thread viewport dimensions.
func (m CommentOverlayModel) viewportDimensions() (width, height int) {
	_, oh := m.overlayDimensions()
	width = m.innerWidth()
	// Subtract: border(2) + title(2) + diffCtx(~7) + 2 separators(2) + textarea(5) + footer(2) + blanks(3)
	height = oh - 23
	if height < 3 {
		height = 3
	}
	return width, height
}

func (m *CommentOverlayModel) refreshContent() {
	if !m.ready {
		return
	}
	content := m.renderThreadContent()
	_, vpH := m.viewportDimensions()
	vpW := m.innerWidth()
	m.viewport.Width = vpW
	m.viewport.Height = vpH
	m.viewport.SetContent(content)
}

func (m CommentOverlayModel) renderDiffContext(diffLines []string, targetIdx int) string {
	if len(diffLines) == 0 {
		return ""
	}
	var b strings.Builder
	for i, line := range diffLines {
		if i > 0 {
			b.WriteString("\n")
		}
		var style lipgloss.Style
		switch {
		case strings.HasPrefix(line, "@@"):
			style = diffHunkHeaderStyle
		case strings.HasPrefix(line, "+"):
			style = diffAddedStyle
		case strings.HasPrefix(line, "-"):
			style = diffRemovedStyle
		default:
			style = lipgloss.NewStyle()
		}
		if i == targetIdx {
			style = style.Background(diffCursorBg)
		}
		b.WriteString(style.Render(line))
	}
	return b.String()
}

func (m CommentOverlayModel) renderThreadContent() string {
	var b strings.Builder
	innerW := m.innerWidth()

	hasContent := false

	// AI comments
	for _, c := range m.aiComments {
		if hasContent {
			b.WriteString("\n\n")
		}
		header := commentBoxHeaderStyle.Render("🤖 Claude AI")
		b.WriteString(header)
		b.WriteString("\n")
		b.WriteString(wordWrapPlain(c.Body, innerW))
		hasContent = true
	}

	// GitHub threads
	for _, t := range m.ghThreads {
		if hasContent {
			b.WriteString("\n\n")
		}
		// Root
		header := commentBoxHeaderStyle.Render("💬 @"+t.Root.Author.Login) +
			commentBoxMetaStyle.Render(" · "+t.Root.CreatedAt.Format("Jan 2 15:04"))
		b.WriteString(header)
		b.WriteString("\n")
		b.WriteString(wordWrapPlain(t.Root.Body, innerW))

		// All replies (no trimming in overlay — show full thread)
		for _, r := range t.Replies {
			b.WriteString("\n\n")
			replyHeader := commentBoxReplyStyle.Render("  ↳ ") +
				commentBoxHeaderStyle.Render("@"+r.Author.Login) +
				commentBoxMetaStyle.Render(" · "+r.CreatedAt.Format("Jan 2 15:04"))
			b.WriteString(replyHeader)
			b.WriteString("\n")
			b.WriteString(wordWrapPlain(r.Body, innerW))
		}
		hasContent = true
	}

	// Pending comments
	for _, c := range m.pendingComments {
		if hasContent {
			b.WriteString("\n\n")
		}
		source := "Draft"
		if c.Source == "ai" {
			source = "Draft (AI)"
		}
		header := commentBoxHeaderStyle.Render("📝 " + source)
		b.WriteString(header)
		b.WriteString("\n")
		b.WriteString(wordWrapPlain(c.Body, innerW))
		hasContent = true
	}

	if !hasContent {
		return lipgloss.NewStyle().
			Foreground(lipgloss.Color("244")).
			Italic(true).
			Render("No comments yet. Press i to write one.")
	}

	return b.String()
}

func (m CommentOverlayModel) renderFooter(innerW int) string {
	var parts []string

	if m.replyTargetID > 0 {
		if m.postImmediately {
			parts = append(parts, commentOverlayActiveToggle.Render("● post now"))
			parts = append(parts, commentOverlayInactiveToggle.Render("○ add to review"))
		} else {
			parts = append(parts, commentOverlayInactiveToggle.Render("○ post now"))
			parts = append(parts, commentOverlayActiveToggle.Render("● add to review"))
		}
		parts = append(parts, commentOverlayHintStyle.Render("  Tab: toggle"))
	} else {
		parts = append(parts, commentOverlayActiveToggle.Render("● add to review"))
	}

	left := strings.Join(parts, " ")

	var right string
	if m.composing {
		right = commentOverlayHintStyle.Render("Ctrl+S: submit  Esc: cancel")
	} else {
		right = commentOverlayHintStyle.Render("i: reply  Esc: close")
	}

	gap := innerW - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

// wordWrapPlain wraps text at the given width without any styling.
func wordWrapPlain(text string, width int) string {
	if width <= 0 {
		return text
	}
	var result strings.Builder
	for i, line := range strings.Split(text, "\n") {
		if i > 0 {
			result.WriteString("\n")
		}
		for len(line) > width {
			// Find last space within width
			cut := strings.LastIndex(line[:width], " ")
			if cut <= 0 {
				cut = width
			}
			result.WriteString(line[:cut])
			result.WriteString("\n")
			line = strings.TrimLeft(line[cut:], " ")
		}
		result.WriteString(line)
	}
	return result.String()
}
