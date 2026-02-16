package github

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// fakeStdinRunner returns a StdinCommandRunner that captures stdin and
// pattern-matches args against canned responses.
func fakeStdinRunner(responses map[string]string, capturedStdin *string) StdinCommandRunner {
	return func(ctx context.Context, stdin string, args ...string) (string, error) {
		if capturedStdin != nil {
			*capturedStdin = stdin
		}
		key := strings.Join(args, " ")
		for pattern, response := range responses {
			if strings.Contains(key, pattern) {
				return response, nil
			}
		}
		return "", nil
	}
}

func TestSubmitReviewWithComments_Success(t *testing.T) {
	var capturedStdin string
	client := &Client{
		username: "alice",
		run:      fakeRunner(map[string]string{}),
		runStdin: fakeStdinRunner(map[string]string{"api repos/": ""}, &capturedStdin),
	}

	comments := []ReviewCommentPayload{
		{Path: "main.go", Line: 10, Body: "Consider error handling"},
	}

	err := client.SubmitReviewWithComments(context.Background(), "alice", "widget", 42, "COMMENT", "Looks good", comments)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify stdin payload
	if capturedStdin == "" {
		t.Fatal("expected stdin payload")
	}
	var payload struct {
		Event    string                 `json:"event"`
		Body     string                 `json:"body"`
		Comments []ReviewCommentPayload `json:"comments"`
	}
	if err := json.Unmarshal([]byte(capturedStdin), &payload); err != nil {
		t.Fatalf("failed to parse stdin: %v", err)
	}
	if payload.Event != "COMMENT" {
		t.Errorf("Event = %q, want COMMENT", payload.Event)
	}
	if payload.Body != "Looks good" {
		t.Errorf("Body = %q", payload.Body)
	}
	if len(payload.Comments) != 1 {
		t.Fatalf("got %d comments, want 1", len(payload.Comments))
	}
}

func TestSubmitReviewWithComments_DefaultSide(t *testing.T) {
	var capturedStdin string
	client := &Client{
		username: "alice",
		run:      fakeRunner(map[string]string{}),
		runStdin: fakeStdinRunner(map[string]string{"api repos/": ""}, &capturedStdin),
	}

	comments := []ReviewCommentPayload{
		{Path: "main.go", Line: 10, Body: "test"}, // no Side set
		{Path: "main.go", Line: 20, Body: "test2", Side: "LEFT"},
	}

	err := client.SubmitReviewWithComments(context.Background(), "alice", "widget", 42, "APPROVE", "", comments)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var payload struct {
		Comments []ReviewCommentPayload `json:"comments"`
	}
	json.Unmarshal([]byte(capturedStdin), &payload)

	if payload.Comments[0].Side != "RIGHT" {
		t.Errorf("comment[0].Side = %q, want RIGHT (default)", payload.Comments[0].Side)
	}
	if payload.Comments[1].Side != "LEFT" {
		t.Errorf("comment[1].Side = %q, want LEFT (preserved)", payload.Comments[1].Side)
	}
}

func TestSubmitReviewWithComments_InvalidEvent(t *testing.T) {
	client := &Client{
		username: "alice",
		run:      fakeRunner(map[string]string{}),
		runStdin: fakeStdinRunner(map[string]string{}, nil),
	}

	err := client.SubmitReviewWithComments(context.Background(), "alice", "widget", 42, "INVALID", "", nil)
	if err == nil {
		t.Fatal("expected error for invalid event")
	}
	if !strings.Contains(err.Error(), "invalid review event") {
		t.Errorf("error = %q", err.Error())
	}
}

func TestSubmitReviewWithComments_CaseInsensitive(t *testing.T) {
	var capturedStdin string
	client := &Client{
		username: "alice",
		run:      fakeRunner(map[string]string{}),
		runStdin: fakeStdinRunner(map[string]string{"api repos/": ""}, &capturedStdin),
	}

	err := client.SubmitReviewWithComments(context.Background(), "a", "b", 1, "approve", "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var payload struct{ Event string `json:"event"` }
	json.Unmarshal([]byte(capturedStdin), &payload)
	if payload.Event != "APPROVE" {
		t.Errorf("Event = %q, want APPROVE (uppercased)", payload.Event)
	}
}

func TestReplyToComment_Success(t *testing.T) {
	var capturedStdin string
	client := &Client{
		username: "alice",
		run:      fakeRunner(map[string]string{}),
		runStdin: fakeStdinRunner(map[string]string{"api repos/": ""}, &capturedStdin),
	}

	err := client.ReplyToComment(context.Background(), "alice", "widget", 42, 12345, "Thanks for the feedback!")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var payload struct{ Body string `json:"body"` }
	if err := json.Unmarshal([]byte(capturedStdin), &payload); err != nil {
		t.Fatalf("failed to parse stdin: %v", err)
	}
	if payload.Body != "Thanks for the feedback!" {
		t.Errorf("Body = %q", payload.Body)
	}
}

func TestReplyToComment_Error(t *testing.T) {
	client := &Client{
		username: "alice",
		run:      fakeErrorRunner("api error"),
		runStdin: func(ctx context.Context, stdin string, args ...string) (string, error) {
			return "", errorf("api call failed")
		},
	}

	err := client.ReplyToComment(context.Background(), "alice", "widget", 42, 999, "test")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRerunWorkflow_Success(t *testing.T) {
	client := NewTestClient("alice", fakeRunner(map[string]string{
		"run rerun": "",
	}))

	err := client.RerunWorkflow(context.Background(), "alice", "widget", 12345, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRerunWorkflow_FailedOnly(t *testing.T) {
	called := false
	client := NewTestClient("alice", func(ctx context.Context, args ...string) (string, error) {
		key := strings.Join(args, " ")
		if strings.Contains(key, "--failed") {
			called = true
		}
		return "", nil
	})

	err := client.RerunWorkflow(context.Background(), "alice", "widget", 12345, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("expected --failed flag")
	}
}

func TestRequestChangesPR(t *testing.T) {
	client := NewTestClient("alice", fakeRunner(map[string]string{
		"pr review": "",
	}))

	err := client.RequestChangesPR(context.Background(), "alice", "widget", 42, "Please fix the bug")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCommentReviewPR(t *testing.T) {
	client := NewTestClient("alice", fakeRunner(map[string]string{
		"pr review": "",
	}))

	err := client.CommentReviewPR(context.Background(), "alice", "widget", 42, "Some notes")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// errorf is a helper to produce consistent errors in test fakes.
func errorf(format string, args ...interface{}) error {
	return fmt.Errorf(format, args...)
}
