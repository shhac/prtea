package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/shhac/prtea/internal/github"
)

// CommentsTabModel manages the comments tab state and rendering.
type CommentsTabModel struct {
	comments       []github.Comment
	inlineComments []github.InlineComment
	loading        bool
	error          string
	posting        bool
	cache          string
	cacheWidth     int
}

// SetLoading puts the comments tab into loading state.
func (t *CommentsTabModel) SetLoading() {
	t.loading = true
	t.error = ""
	t.comments = nil
	t.inlineComments = nil
	t.cache = ""
}

// SetComments sets the comments data and clears loading state.
func (t *CommentsTabModel) SetComments(comments []github.Comment, inline []github.InlineComment) {
	t.comments = comments
	t.inlineComments = inline
	t.loading = false
	t.error = ""
	t.cache = ""
}

// SetError sets an error message on the comments tab.
func (t *CommentsTabModel) SetError(err string) {
	t.error = err
	t.loading = false
	t.cache = ""
}

// SetPosted clears the posting state after a comment post attempt.
func (t *CommentsTabModel) SetPosted(err error) {
	t.posting = false
	if err != nil {
		t.error = "Failed to post comment: " + err.Error()
	}
	t.cache = ""
}

// Clear resets all comments state.
func (t *CommentsTabModel) Clear() {
	t.comments = nil
	t.inlineComments = nil
	t.loading = false
	t.error = ""
	t.posting = false
	t.cache = ""
}

// IsPosting returns whether a comment is currently being posted.
func (t CommentsTabModel) IsPosting() bool {
	return t.posting
}

// SetPosting sets the posting state.
func (t *CommentsTabModel) SetPosting(posting bool) {
	t.posting = posting
}

// Render renders the comments tab content for the viewport.
func (t *CommentsTabModel) Render(width int, spinnerView string, md *MarkdownRenderer) string {
	if t.loading {
		return lipgloss.NewStyle().
			Foreground(lipgloss.Color("244")).
			Padding(1, 0).
			Render(spinnerView + " Loading comments...")
	}
	if t.error != "" {
		return renderErrorWithHint(formatUserError(t.error), "Press r to refresh")
	}
	if len(t.comments) == 0 && len(t.inlineComments) == 0 {
		return renderEmptyState("No comments on this PR", "Press Enter to be the first to comment")
	}

	// Return cached render if available and width hasn't changed
	if t.cache != "" && t.cacheWidth == width {
		return t.cache
	}

	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("33"))
	authorStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("220"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("244"))

	var b strings.Builder

	if len(t.comments) > 0 {
		b.WriteString(sectionStyle.Render(fmt.Sprintf("Conversation (%d)", len(t.comments))))
		b.WriteString("\n")
		for i, c := range t.comments {
			if i > 0 {
				b.WriteString("\n")
			}
			b.WriteString(authorStyle.Render(c.Author.Login))
			b.WriteString(dimStyle.Render(" · " + c.CreatedAt.Format("Jan 2 15:04")))
			b.WriteString("\n")
			b.WriteString(md.RenderMarkdown(c.Body, width))
			b.WriteString("\n")
		}
	}

	if len(t.inlineComments) > 0 {
		if len(t.comments) > 0 {
			b.WriteString("\n")
		}
		b.WriteString(dimStyle.Render(fmt.Sprintf("%d review comments shown inline in diff", len(t.inlineComments))))
		b.WriteString("\n")
	}

	result := b.String()
	t.cache = result
	t.cacheWidth = width
	return result
}
