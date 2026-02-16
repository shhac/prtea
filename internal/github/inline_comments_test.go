package github

import (
	"context"
	"encoding/json"
	"testing"
)

func TestGetInlineComments_Basic(t *testing.T) {
	raw := []ghInlineComment{
		{
			ID: 1001,
			User: struct {
				Login     string `json:"login"`
				AvatarURL string `json:"avatar_url"`
			}{Login: "alice", AvatarURL: "https://example.com/alice.png"},
			Body:      "Nice change!",
			Path:      "main.go",
			Line:      10,
			Side:      "RIGHT",
			Position:  intPtr(5),
		},
	}
	data, _ := json.Marshal(raw)

	client := NewTestClient("alice", fakeRunner(map[string]string{
		"api repos/alice/widget/pulls/42/comments": string(data),
	}))

	comments, err := client.GetInlineComments(context.Background(), "alice", "widget", 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(comments) != 1 {
		t.Fatalf("got %d comments, want 1", len(comments))
	}
	c := comments[0]
	if c.ID != 1001 {
		t.Errorf("ID = %d", c.ID)
	}
	if c.Author.Login != "alice" {
		t.Errorf("Author = %q", c.Author.Login)
	}
	if c.Line != 10 {
		t.Errorf("Line = %d", c.Line)
	}
	if c.Outdated {
		t.Error("should not be outdated (position > 0)")
	}
}

func TestGetInlineComments_LineFallback(t *testing.T) {
	// When Line is 0, should fall back to OriginalLine
	raw := []ghInlineComment{
		{
			ID:            2001,
			User:          struct{ Login string `json:"login"`; AvatarURL string `json:"avatar_url"` }{Login: "bob"},
			Body:          "Outdated comment",
			Path:          "old.go",
			Line:          0,
			OriginalLine:  25,
			Side:          "RIGHT",
			Position:      nil, // outdated
		},
	}
	data, _ := json.Marshal(raw)

	client := NewTestClient("alice", fakeRunner(map[string]string{
		"api repos/": string(data),
	}))

	comments, err := client.GetInlineComments(context.Background(), "alice", "widget", 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if comments[0].Line != 25 {
		t.Errorf("Line = %d, want 25 (fallback from OriginalLine)", comments[0].Line)
	}
	if !comments[0].Outdated {
		t.Error("should be outdated (position is nil)")
	}
}

func TestGetInlineComments_NilPointers(t *testing.T) {
	// StartLine and InReplyToID are nil
	raw := []ghInlineComment{
		{
			ID:         3001,
			User:       struct{ Login string `json:"login"`; AvatarURL string `json:"avatar_url"` }{Login: "charlie"},
			Body:       "test",
			Path:       "test.go",
			Line:       5,
			Side:       "RIGHT",
			Position:   intPtr(3),
			// StartLine and InReplyToID intentionally nil
		},
	}
	data, _ := json.Marshal(raw)

	client := NewTestClient("alice", fakeRunner(map[string]string{
		"api repos/": string(data),
	}))

	comments, err := client.GetInlineComments(context.Background(), "alice", "widget", 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	c := comments[0]
	if c.StartLine != 0 {
		t.Errorf("StartLine = %d, want 0 (nil pointer)", c.StartLine)
	}
	if c.InReplyToID != 0 {
		t.Errorf("InReplyToID = %d, want 0 (nil pointer)", c.InReplyToID)
	}
}

func TestGetInlineComments_WithStartLineAndReply(t *testing.T) {
	startLine := 5
	replyTo := int64(999)
	raw := []ghInlineComment{
		{
			ID:          4001,
			User:        struct{ Login string `json:"login"`; AvatarURL string `json:"avatar_url"` }{Login: "dave"},
			Body:        "multi-line comment",
			Path:        "lib.go",
			Line:        10,
			StartLine:   &startLine,
			Side:        "RIGHT",
			InReplyToID: &replyTo,
			Position:    intPtr(8),
		},
	}
	data, _ := json.Marshal(raw)

	client := NewTestClient("alice", fakeRunner(map[string]string{
		"api repos/": string(data),
	}))

	comments, err := client.GetInlineComments(context.Background(), "alice", "widget", 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	c := comments[0]
	if c.StartLine != 5 {
		t.Errorf("StartLine = %d, want 5", c.StartLine)
	}
	if c.InReplyToID != 999 {
		t.Errorf("InReplyToID = %d, want 999", c.InReplyToID)
	}
}

func TestGetInlineComments_OutdatedPositionZero(t *testing.T) {
	pos := 0
	raw := []ghInlineComment{
		{
			ID:       5001,
			User:     struct{ Login string `json:"login"`; AvatarURL string `json:"avatar_url"` }{Login: "eve"},
			Body:     "test",
			Path:     "x.go",
			Line:     1,
			Side:     "RIGHT",
			Position: &pos,
		},
	}
	data, _ := json.Marshal(raw)

	client := NewTestClient("alice", fakeRunner(map[string]string{
		"api repos/": string(data),
	}))

	comments, err := client.GetInlineComments(context.Background(), "alice", "widget", 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !comments[0].Outdated {
		t.Error("should be outdated (position == 0)")
	}
}

func intPtr(n int) *int { return &n }
