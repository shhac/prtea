package ai

import (
	"strings"
	"testing"
)

func TestExtractActionsSingleTrailingBlock(t *testing.T) {
	msg := "I'll post that comment.\n\n```prtea-action\n{\"type\":\"post_comment\",\"body\":\"Nice work!\"}\n```"

	remaining, actions := ExtractActions(msg)

	if len(actions) != 1 {
		t.Fatalf("got %d actions, want 1", len(actions))
	}
	if actions[0].Type != ActionPostComment || actions[0].Body != "Nice work!" {
		t.Errorf("action = %+v", actions[0])
	}
	if strings.Contains(remaining, "prtea-action") {
		t.Errorf("remaining still contains block: %q", remaining)
	}
	if !strings.Contains(remaining, "I'll post that comment.") {
		t.Errorf("remaining lost message text: %q", remaining)
	}
}

func TestExtractActionsMultipleBlocks(t *testing.T) {
	msg := "Doing both.\n\n" +
		"```prtea-action\n{\"type\":\"post_comment\",\"body\":\"first\"}\n```\n\n" +
		"```prtea-action\n{\"type\":\"submit_review\",\"event\":\"approve\",\"body\":\"ship it\"}\n```"

	remaining, actions := ExtractActions(msg)

	if len(actions) != 2 {
		t.Fatalf("got %d actions, want 2: %+v", len(actions), actions)
	}
	if actions[0].Type != ActionPostComment || actions[1].Type != ActionSubmitReview {
		t.Errorf("action order wrong: %+v", actions)
	}
	if strings.Contains(remaining, "```") {
		t.Errorf("remaining still contains fence: %q", remaining)
	}
}

func TestExtractActionsNoBlock(t *testing.T) {
	msg := "Just a normal answer with some `code` in it."
	remaining, actions := ExtractActions(msg)
	if len(actions) != 0 || remaining != msg {
		t.Errorf("remaining=%q actions=%+v", remaining, actions)
	}
}

func TestExtractActionsMalformedJSONLeftInPlace(t *testing.T) {
	msg := "Try this.\n\n```prtea-action\nnot json at all\n```"
	remaining, actions := ExtractActions(msg)
	if len(actions) != 0 {
		t.Errorf("malformed block parsed: %+v", actions)
	}
	if remaining != msg {
		t.Errorf("malformed block should be left in message: %q", remaining)
	}
}

func TestExtractActionsNonTrailingBlockIgnored(t *testing.T) {
	msg := "```prtea-action\n{\"type\":\"post_comment\",\"body\":\"x\"}\n```\n\nMore prose after the block."
	remaining, actions := ExtractActions(msg)
	if len(actions) != 0 {
		t.Errorf("non-trailing block should not be extracted: %+v", actions)
	}
	if remaining != msg {
		t.Errorf("message should be unchanged: %q", remaining)
	}
}

func TestExtractActionsBodyContainingCodeFence(t *testing.T) {
	// Comment bodies routinely contain ``` as escaped text inside the JSON —
	// the closing fence must be line-anchored, not first-match.
	msg := "Suggesting a fix.\n\n```prtea-action\n" +
		`{"type":"post_comment","body":"Use this instead:\n` + "```go\\nfoo()\\n```" + `\nmuch cleaner"}` +
		"\n```"

	remaining, actions := ExtractActions(msg)

	if len(actions) != 1 {
		t.Fatalf("got %d actions, want 1 (remaining=%q)", len(actions), remaining)
	}
	if !strings.Contains(actions[0].Body, "```go") {
		t.Errorf("body lost inner fence: %q", actions[0].Body)
	}
	if remaining != "Suggesting a fix." {
		t.Errorf("remaining = %q", remaining)
	}
}

func TestExtractActionsInvalidActionRejected(t *testing.T) {
	msg := "ok\n\n```prtea-action\n{\"type\":\"submit_review\",\"event\":\"merge\",\"body\":\"x\"}\n```"
	_, actions := ExtractActions(msg)
	if len(actions) != 0 {
		t.Errorf("invalid event should fail validation: %+v", actions)
	}
}

func TestActionValidate(t *testing.T) {
	cases := []struct {
		name    string
		action  Action
		wantErr bool
	}{
		{"post comment ok", Action{Type: ActionPostComment, Body: "hi"}, false},
		{"post comment empty body", Action{Type: ActionPostComment, Body: "  "}, true},
		{"reply ok", Action{Type: ActionReplyToComment, CommentID: 5, Body: "hi"}, false},
		{"reply missing id", Action{Type: ActionReplyToComment, Body: "hi"}, true},
		{"review approve ok", Action{Type: ActionSubmitReview, Event: "approve"}, false},
		{"review bad event", Action{Type: ActionSubmitReview, Event: "merge"}, true},
		{"unknown type", Action{Type: "delete_repo", Body: "x"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.action.Validate()
			if (err != nil) != tc.wantErr {
				t.Errorf("Validate() err = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}
