package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/shhac/prtea/internal/github"
)

// ReviewTabModel manages the review submission tab state and rendering.
type ReviewTabModel struct {
	textArea      textarea.Model
	action        ReviewAction
	radioFocus    int
	focus         ReviewFocus
	submitting    bool
	defaultAction ReviewAction

	// Pending inline comments queued for submission (set by app)
	pendingComments []github.ReviewCommentPayload
}

// NewReviewTabModel creates a ReviewTabModel with default state.
func NewReviewTabModel() ReviewTabModel {
	ta := textarea.New()
	ta.Placeholder = "Review body (optional for approve)..."
	ta.CharLimit = 65535
	ta.SetHeight(5)
	ta.ShowLineNumbers = false
	ta.Blur()

	return ReviewTabModel{
		textArea:   ta,
		action:     ReviewComment,
		radioFocus: 1, // matches ReviewComment default
	}
}

// SetWidth sets the textarea width.
func (t *ReviewTabModel) SetWidth(width int) {
	t.textArea.SetWidth(width)
}

// SetDefaultAction sets the default review action from config and
// applies it to the current review state. Use at initialization time.
func (t *ReviewTabModel) SetDefaultAction(action string) {
	t.parseDefault(action)
	t.action = t.defaultAction
	t.radioFocus = int(t.defaultAction)
}

func (t *ReviewTabModel) parseDefault(action string) {
	t.defaultAction = ParseReviewAction(action)
}

// Clear resets review state for a new PR.
func (t *ReviewTabModel) Clear() {
	t.resetForm()
	t.submitting = false
	t.pendingComments = nil
}

// resetForm restores the form to its default state.
func (t *ReviewTabModel) resetForm() {
	t.textArea.Reset()
	t.action = t.defaultAction
	t.radioFocus = int(t.defaultAction)
	t.focus = ReviewFocusTextArea
	t.textArea.Blur()
}

// SetPendingComments stores the draft inline comments queued for submission.
func (t *ReviewTabModel) SetPendingComments(comments []github.ReviewCommentPayload) {
	t.pendingComments = comments
}

// SetSubmitted clears the submitting state. On success, also resets the form.
func (t *ReviewTabModel) SetSubmitted(err error) {
	t.submitting = false
	if err == nil {
		t.resetForm()
	}
}

// IsFocused returns true when the textarea has focus (insert mode).
func (t ReviewTabModel) IsFocused() bool {
	return t.textArea.Focused()
}

// Blur removes focus from the textarea.
func (t *ReviewTabModel) Blur() {
	t.textArea.Blur()
}

// Update handles key events when the Review tab is active.
// Tab switching (h/l) is handled by the coordinator before delegation.
func (t ReviewTabModel) Update(msg tea.KeyMsg) (ReviewTabModel, tea.Cmd) {
	// When textarea is focused, it captures all keys except ESC and Tab
	if t.textArea.Focused() {
		switch msg.String() {
		case "esc":
			t.textArea.Blur()
			return t, func() tea.Msg { return ModeChangedMsg{Mode: ChatModeNormal} }
		case "tab":
			t.textArea.Blur()
			t.focus = ReviewFocusRadio
			return t, func() tea.Msg { return ModeChangedMsg{Mode: ChatModeNormal} }
		default:
			var cmd tea.Cmd
			t.textArea, cmd = t.textArea.Update(msg)
			return t, cmd
		}
	}

	// Normal mode within review tab
	switch t.focus {
	case ReviewFocusTextArea:
		switch msg.String() {
		case "enter":
			t.textArea.Focus()
			return t, func() tea.Msg { return ModeChangedMsg{Mode: ChatModeInsert} }
		case "tab", "j", "down":
			t.focus = ReviewFocusRadio
			t.radioFocus = int(t.action) // start focus on current selection
			return t, nil
		}

	case ReviewFocusRadio:
		switch msg.String() {
		case "j", "down":
			if t.radioFocus < int(ReviewRequestChanges) {
				t.radioFocus++
			} else {
				t.focus = ReviewFocusSubmit
			}
			return t, nil
		case "k", "up":
			if t.radioFocus > int(ReviewApprove) {
				t.radioFocus--
			} else {
				t.focus = ReviewFocusTextArea
			}
			return t, nil
		case "enter", " ":
			t.action = ReviewAction(t.radioFocus)
			return t, nil
		case "tab":
			t.focus = ReviewFocusSubmit
			return t, nil
		case "shift+tab":
			t.focus = ReviewFocusTextArea
			return t, nil
		}

	case ReviewFocusSubmit:
		switch msg.String() {
		case "enter":
			if t.submitting {
				return t, nil
			}
			body := strings.TrimSpace(t.textArea.Value())
			if t.action == ReviewRequestChanges && body == "" {
				return t, func() tea.Msg {
					return ReviewValidationMsg{Message: "Review body is required for Request Changes"}
				}
			}
			if t.action == ReviewComment && body == "" {
				return t, func() tea.Msg {
					return ReviewValidationMsg{Message: "Review body is required for Comment"}
				}
			}
			t.submitting = true
			action := t.action
			return t, func() tea.Msg {
				return ReviewSubmitMsg{Action: action, Body: body}
			}
		case "tab":
			t.focus = ReviewFocusTextArea
			return t, nil
		case "shift+tab":
			t.focus = ReviewFocusRadio
			t.radioFocus = int(ReviewRequestChanges)
			return t, nil
		case "k", "up":
			t.focus = ReviewFocusRadio
			t.radioFocus = int(ReviewRequestChanges)
			return t, nil
		}
	}

	return t, nil
}

// Render renders the Review tab content (textarea, radio options, submit button).
func (t ReviewTabModel) Render(width int, spinnerView string) string {
	var b strings.Builder

	// Pending inline comment basket
	if n := len(t.pendingComments); n > 0 {
		countText := fmt.Sprintf("📝 %d pending inline comment", n)
		if n != 1 {
			countText += "s"
		}
		countText += " will be submitted"
		b.WriteString(lipgloss.NewStyle().
			Foreground(lipgloss.Color("214")).
			Render(countText))
		b.WriteString("\n")
		for _, c := range t.pendingComments {
			target := formatCommentTarget(c.Path, c.StartLine, c.Line)
			body := firstLine(c.Body)
			entry := fmt.Sprintf("  • %s — %s", target, body)
			b.WriteString(lipgloss.NewStyle().
				Foreground(lipgloss.Color("244")).
				Render(truncateLine(entry, width)))
			b.WriteString("\n")
		}
		b.WriteString(lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")).
			Italic(true).
			Render("  edit or delete with 'c' on the diff line"))
		b.WriteString("\n\n")
	}

	// 1. Review body textarea
	label := reviewLabelStyle.Render("Review Body")
	if t.focus == ReviewFocusTextArea && !t.textArea.Focused() {
		label += lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Italic(true).Render("  press Enter to edit")
	}
	b.WriteString(label)
	b.WriteString("\n")
	b.WriteString(t.textArea.View())
	b.WriteString("\n\n")

	// 2. Review action radio group
	b.WriteString(reviewLabelStyle.Render("Action"))
	b.WriteString("\n")

	actions := []struct {
		action ReviewAction
		active lipgloss.Style
	}{
		{ReviewApprove, reviewApproveStyle},
		{ReviewComment, reviewCommentStyle},
		{ReviewRequestChanges, reviewRequestChangesStyle},
	}

	for i, a := range actions {
		indicator := "  ( ) "
		if t.action == a.action {
			indicator = "  (●) "
		}
		isFocused := t.focus == ReviewFocusRadio && t.radioFocus == i
		if isFocused {
			indicator = "▸ " + indicator[2:]
		}

		var line string
		if t.action == a.action {
			line = indicator + a.active.Render(a.action.Label())
		} else {
			line = indicator + reviewOptionDimStyle.Render(a.action.Label())
		}
		if isFocused {
			line = lipgloss.NewStyle().Bold(true).Render(line)
		}

		b.WriteString(line)
		b.WriteString("\n")
	}
	b.WriteString("\n")

	// 3. Submit button
	buttonText := fmt.Sprintf("[ Submit: %s ]", t.action.Label())
	if t.submitting {
		buttonText = "[ Submitting... ]"
	}

	if t.focus == ReviewFocusSubmit && !t.submitting {
		var style lipgloss.Style
		switch t.action {
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
