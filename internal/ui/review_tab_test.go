package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/shhac/prtea/internal/claude"
)

func TestReviewTab_ParseDefault(t *testing.T) {
	tests := []struct {
		input string
		want  ReviewAction
	}{
		{"approve", ReviewApprove},
		{"request_changes", ReviewRequestChanges},
		{"comment", ReviewComment},
		{"unknown", ReviewComment},
		{"", ReviewComment},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			tab := NewReviewTabModel()
			tab.parseDefault(tt.input)
			if tab.defaultAction != tt.want {
				t.Errorf("parseDefault(%q) → defaultAction=%d, want %d", tt.input, tab.defaultAction, tt.want)
			}
		})
	}
}

func TestReviewTab_SetDefaultAction(t *testing.T) {
	tab := NewReviewTabModel()
	tab.SetDefaultAction("approve")

	if tab.defaultAction != ReviewApprove {
		t.Errorf("defaultAction = %d, want %d", tab.defaultAction, ReviewApprove)
	}
	if tab.action != ReviewApprove {
		t.Errorf("action = %d, want %d (should match default)", tab.action, ReviewApprove)
	}
	if tab.radioFocus != int(ReviewApprove) {
		t.Errorf("radioFocus = %d, want %d", tab.radioFocus, int(ReviewApprove))
	}
}

func TestReviewTab_UpdateDefaultAction(t *testing.T) {
	tab := NewReviewTabModel()
	tab.SetDefaultAction("comment")

	// Change the current action manually
	tab.action = ReviewRequestChanges

	// UpdateDefaultAction should NOT change current state
	tab.UpdateDefaultAction("approve")

	if tab.defaultAction != ReviewApprove {
		t.Errorf("defaultAction = %d, want %d", tab.defaultAction, ReviewApprove)
	}
	if tab.action != ReviewRequestChanges {
		t.Errorf("action = %d, want %d (should be unchanged)", tab.action, ReviewRequestChanges)
	}
}

func TestReviewTab_Clear(t *testing.T) {
	tab := NewReviewTabModel()
	tab.SetDefaultAction("approve")

	// Put the tab into a messy state
	tab.action = ReviewRequestChanges
	tab.submitting = true
	tab.aiLoading = true
	tab.aiError = "some error"
	tab.aiResult = &claude.ReviewAnalysis{Body: "test"}
	tab.pendingCount = 5
	tab.textArea.SetValue("some text")

	tab.Clear()

	if tab.action != ReviewApprove {
		t.Errorf("action = %d, want %d (default)", tab.action, ReviewApprove)
	}
	if tab.submitting {
		t.Error("submitting should be false")
	}
	if tab.aiLoading {
		t.Error("aiLoading should be false")
	}
	if tab.aiError != "" {
		t.Errorf("aiError = %q", tab.aiError)
	}
	if tab.aiResult != nil {
		t.Error("aiResult should be nil")
	}
	if tab.pendingCount != 0 {
		t.Errorf("pendingCount = %d", tab.pendingCount)
	}
	if tab.textArea.Value() != "" {
		t.Errorf("textArea value = %q", tab.textArea.Value())
	}
}

func TestReviewTab_SetAIReviewResult(t *testing.T) {
	tab := NewReviewTabModel()

	// Start with loading state
	tab.SetAIReviewLoading()
	if !tab.aiLoading {
		t.Error("expected aiLoading=true")
	}

	// Set result
	result := &claude.ReviewAnalysis{
		Action: "request_changes",
		Body:   "Please fix the error handling",
	}
	tab.SetAIReviewResult(result)

	if tab.aiLoading {
		t.Error("aiLoading should be cleared")
	}
	if tab.aiResult != result {
		t.Error("aiResult not set")
	}
	if tab.textArea.Value() != "Please fix the error handling" {
		t.Errorf("textArea = %q", tab.textArea.Value())
	}
	if tab.action != ReviewRequestChanges {
		t.Errorf("action = %d, want %d", tab.action, ReviewRequestChanges)
	}
}

func TestReviewTab_SetAIReviewResult_Approve(t *testing.T) {
	tab := NewReviewTabModel()
	tab.SetAIReviewResult(&claude.ReviewAnalysis{
		Action: "approve",
		Body:   "LGTM",
	})
	if tab.action != ReviewApprove {
		t.Errorf("action = %d, want %d", tab.action, ReviewApprove)
	}
}

func TestReviewTab_SetAIReviewResult_DefaultComment(t *testing.T) {
	tab := NewReviewTabModel()
	tab.SetAIReviewResult(&claude.ReviewAnalysis{
		Action: "comment",
		Body:   "Some notes",
	})
	if tab.action != ReviewComment {
		t.Errorf("action = %d, want %d", tab.action, ReviewComment)
	}
}

func TestReviewTab_SetAIReviewError(t *testing.T) {
	tab := NewReviewTabModel()
	tab.SetAIReviewLoading()
	tab.SetAIReviewError("timeout")

	if tab.aiLoading {
		t.Error("aiLoading should be cleared")
	}
	if tab.aiError != "timeout" {
		t.Errorf("aiError = %q", tab.aiError)
	}
	if tab.aiResult != nil {
		t.Error("aiResult should be nil")
	}
}

func TestReviewTab_ClearAIReview(t *testing.T) {
	tab := NewReviewTabModel()
	tab.aiResult = &claude.ReviewAnalysis{Body: "test"}
	tab.aiLoading = true
	tab.aiError = "err"

	tab.ClearAIReview()

	if tab.aiResult != nil {
		t.Error("aiResult should be nil")
	}
	if tab.aiLoading {
		t.Error("aiLoading should be false")
	}
	if tab.aiError != "" {
		t.Error("aiError should be empty")
	}
}

func TestReviewTab_SetSubmitted_Success(t *testing.T) {
	tab := NewReviewTabModel()
	tab.SetDefaultAction("comment")
	tab.action = ReviewRequestChanges
	tab.textArea.SetValue("some review text")
	tab.submitting = true

	tab.SetSubmitted(nil)

	if tab.submitting {
		t.Error("submitting should be cleared")
	}
	if tab.action != ReviewComment {
		t.Errorf("action = %d, want %d (reset to default)", tab.action, ReviewComment)
	}
	if tab.textArea.Value() != "" {
		t.Errorf("textArea should be reset, got %q", tab.textArea.Value())
	}
}

func TestReviewTab_SetSubmitted_Error(t *testing.T) {
	tab := NewReviewTabModel()
	tab.action = ReviewRequestChanges
	tab.textArea.SetValue("my review")
	tab.submitting = true

	tab.SetSubmitted(errForTest("submit failed"))

	if tab.submitting {
		t.Error("submitting should be cleared even on error")
	}
	if tab.action != ReviewRequestChanges {
		t.Errorf("action = %d, want %d (preserved on error)", tab.action, ReviewRequestChanges)
	}
	if tab.textArea.Value() != "my review" {
		t.Errorf("textArea should be preserved on error, got %q", tab.textArea.Value())
	}
}

// --- Update / Validation tests ---

func TestReviewTab_Update_RequestChangesRequiresBody(t *testing.T) {
	tab := NewReviewTabModel()
	tab.action = ReviewRequestChanges
	tab.focus = ReviewFocusSubmit

	updated, cmd := tab.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("enter")})
	_ = updated

	if cmd == nil {
		t.Fatal("expected cmd for validation")
	}
	msg := cmd()
	if vm, ok := msg.(ReviewValidationMsg); ok {
		if vm.Message == "" {
			t.Error("expected validation message")
		}
	} else {
		t.Errorf("expected ReviewValidationMsg, got %T", msg)
	}
}

func TestReviewTab_Update_CommentRequiresBody(t *testing.T) {
	tab := NewReviewTabModel()
	tab.action = ReviewComment
	tab.focus = ReviewFocusSubmit

	_, cmd := tab.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("enter")})

	if cmd == nil {
		t.Fatal("expected cmd for validation")
	}
	msg := cmd()
	if _, ok := msg.(ReviewValidationMsg); !ok {
		t.Errorf("expected ReviewValidationMsg, got %T", msg)
	}
}

func TestReviewTab_Update_ApproveAllowsEmptyBody(t *testing.T) {
	tab := NewReviewTabModel()
	tab.action = ReviewApprove
	tab.focus = ReviewFocusSubmit

	_, cmd := tab.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("enter")})

	if cmd == nil {
		t.Fatal("expected cmd")
	}
	msg := cmd()
	if _, ok := msg.(ReviewSubmitMsg); !ok {
		t.Errorf("expected ReviewSubmitMsg, got %T", msg)
	}
}

func TestReviewTab_Update_FocusNavigation(t *testing.T) {
	tab := NewReviewTabModel()
	tab.focus = ReviewFocusTextArea

	// Tab from text area to radio
	tab, _ = tab.Update(tea.KeyMsg{Type: tea.KeyTab})
	if tab.focus != ReviewFocusRadio {
		t.Errorf("focus = %d, want %d (radio)", tab.focus, ReviewFocusRadio)
	}

	// Tab from radio to submit
	tab, _ = tab.Update(tea.KeyMsg{Type: tea.KeyTab})
	if tab.focus != ReviewFocusSubmit {
		t.Errorf("focus = %d, want %d (submit)", tab.focus, ReviewFocusSubmit)
	}

	// Tab from submit wraps to text area
	tab, _ = tab.Update(tea.KeyMsg{Type: tea.KeyTab})
	if tab.focus != ReviewFocusTextArea {
		t.Errorf("focus = %d, want %d (text area)", tab.focus, ReviewFocusTextArea)
	}
}

func TestReviewTab_Update_RadioNavigation(t *testing.T) {
	tab := NewReviewTabModel()
	tab.focus = ReviewFocusRadio
	tab.radioFocus = int(ReviewApprove)

	// j moves down
	tab, _ = tab.Update(keyMsg("j"))
	if tab.radioFocus != int(ReviewComment) {
		t.Errorf("radioFocus = %d, want %d", tab.radioFocus, int(ReviewComment))
	}

	// Enter selects
	tab, _ = tab.Update(keyMsg("enter"))
	if tab.action != ReviewComment {
		t.Errorf("action = %d, want %d", tab.action, ReviewComment)
	}

	// k moves up
	tab, _ = tab.Update(keyMsg("k"))
	if tab.radioFocus != int(ReviewApprove) {
		t.Errorf("radioFocus = %d, want %d", tab.radioFocus, int(ReviewApprove))
	}

	// k at top moves to textarea
	tab, _ = tab.Update(keyMsg("k"))
	if tab.focus != ReviewFocusTextArea {
		t.Errorf("focus = %d, want %d (should move to textarea)", tab.focus, ReviewFocusTextArea)
	}
}

func TestReviewTab_Update_SubmitSetsSubmitting(t *testing.T) {
	tab := NewReviewTabModel()
	tab.focus = ReviewFocusSubmit
	tab.action = ReviewApprove

	tab, cmd := tab.Update(keyMsg("enter"))
	if !tab.submitting {
		t.Error("expected submitting=true")
	}
	if cmd == nil {
		t.Fatal("expected cmd")
	}
	msg := cmd()
	if sm, ok := msg.(ReviewSubmitMsg); ok {
		if sm.Action != ReviewApprove {
			t.Errorf("Action = %d, want %d", sm.Action, ReviewApprove)
		}
	} else {
		t.Errorf("expected ReviewSubmitMsg, got %T", msg)
	}
}

func TestReviewTab_Update_SubmitIgnoredWhileSubmitting(t *testing.T) {
	tab := NewReviewTabModel()
	tab.focus = ReviewFocusSubmit
	tab.action = ReviewApprove
	tab.submitting = true

	_, cmd := tab.Update(keyMsg("enter"))
	if cmd != nil {
		t.Error("expected nil cmd when already submitting")
	}
}

// keyMsg creates a tea.KeyMsg from a key string.
func keyMsg(key string) tea.KeyMsg {
	switch key {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	}
}

type testError string

func (e testError) Error() string { return string(e) }
func errForTest(msg string) error { return testError(msg) }
