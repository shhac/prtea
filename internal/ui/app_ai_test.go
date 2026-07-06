package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/shhac/prtea/internal/ai"
	"github.com/shhac/prtea/internal/github"
)

// fakeGitHubService records the write operations the AI action path performs.
// Read methods return zero values; they are not under test here.
type fakeGitHubService struct {
	calls []string // e.g. "PostComment(hello)"
	err   error    // returned from every write

	inlineComments []github.InlineComment // returned from GetInlineComments
	resolution     map[int64]bool         // returned from GetReviewThreadResolution
	resolutionErr  error
}

func (f *fakeGitHubService) record(format string, args ...any) error {
	f.calls = append(f.calls, fmt.Sprintf(format, args...))
	return f.err
}

func (f *fakeGitHubService) GetUsername() string { return "tester" }
func (f *fakeGitHubService) GetPRsForReview(context.Context) ([]github.PRItem, error) {
	return nil, nil
}
func (f *fakeGitHubService) GetMyPRs(context.Context) ([]github.PRItem, error) { return nil, nil }
func (f *fakeGitHubService) GetPRDetail(context.Context, string, string, int) (*github.PRDetail, error) {
	return nil, nil
}
func (f *fakeGitHubService) GetPRFiles(context.Context, string, string, int) ([]github.PRFile, error) {
	return nil, nil
}
func (f *fakeGitHubService) GetComments(context.Context, string, string, int) ([]github.Comment, error) {
	return nil, nil
}
func (f *fakeGitHubService) GetInlineComments(context.Context, string, string, int) ([]github.InlineComment, error) {
	return f.inlineComments, nil
}
func (f *fakeGitHubService) GetReviewThreadResolution(context.Context, string, string, int) (map[int64]bool, error) {
	return f.resolution, f.resolutionErr
}
func (f *fakeGitHubService) GetCIStatus(context.Context, string, string, string, int) (*github.CIStatus, error) {
	return nil, nil
}
func (f *fakeGitHubService) GetReviews(context.Context, string, string, int) (*github.ReviewSummary, error) {
	return nil, nil
}
func (f *fakeGitHubService) ApprovePR(_ context.Context, _, _ string, _ int, body string) error {
	return f.record("ApprovePR(%s)", body)
}
func (f *fakeGitHubService) PostComment(_ context.Context, _, _ string, _ int, body string) error {
	return f.record("PostComment(%s)", body)
}
func (f *fakeGitHubService) ClosePR(context.Context, string, string, int) error {
	return f.record("ClosePR()")
}
func (f *fakeGitHubService) RequestChangesPR(_ context.Context, _, _ string, _ int, body string) error {
	return f.record("RequestChangesPR(%s)", body)
}
func (f *fakeGitHubService) CommentReviewPR(_ context.Context, _, _ string, _ int, body string) error {
	return f.record("CommentReviewPR(%s)", body)
}
func (f *fakeGitHubService) SubmitReviewWithComments(_ context.Context, _, _ string, _ int, event, body string, _ []github.ReviewCommentPayload) error {
	return f.record("SubmitReviewWithComments(%s, %s)", event, body)
}
func (f *fakeGitHubService) RerunWorkflow(context.Context, string, string, int64, bool) error {
	return f.record("RerunWorkflow()")
}
func (f *fakeGitHubService) ReplyToComment(_ context.Context, _, _ string, _ int, commentID int64, body string) error {
	return f.record("ReplyToComment(%d, %s)", commentID, body)
}
func (f *fakeGitHubService) GetReviewDecisions(context.Context, []github.PRItem) (map[string]string, error) {
	return nil, nil
}
func (f *fakeGitHubService) SetFetchLimit(int) {}

// runCmd executes a tea.Cmd and returns the message it produces.
func runCmd(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected non-nil cmd")
	}
	return cmd()
}

func TestExecuteActionCmd(t *testing.T) {
	cases := []struct {
		name     string
		action   ai.Action
		wantCall string // substring of the recorded client call; "" = no call
		wantErr  bool
	}{
		{"post comment", ai.Action{Type: ai.ActionPostComment, Body: "hi"}, "PostComment(hi)", false},
		{"reply", ai.Action{Type: ai.ActionReplyToComment, CommentID: 7, Body: "re"}, "ReplyToComment(7, re)", false},
		{"approve", ai.Action{Type: ai.ActionSubmitReview, Event: "approve", Body: "lgtm"}, "ApprovePR(lgtm)", false},
		{"review comment", ai.Action{Type: ai.ActionSubmitReview, Event: "comment", Body: "note"}, "CommentReviewPR(note)", false},
		{"request changes", ai.Action{Type: ai.ActionSubmitReview, Event: "request_changes", Body: "fix"}, "RequestChangesPR(fix)", false},
		{"unknown review event", ai.Action{Type: ai.ActionSubmitReview, Event: "merge", Body: "x"}, "", true},
		{"unknown action type", ai.Action{Type: "delete_repo", Body: "x"}, "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := &fakeGitHubService{}
			msg := runCmd(t, executeActionCmd(client, "o", "r", 1, tc.action))

			result, ok := msg.(AIActionResultMsg)
			if !ok {
				t.Fatalf("msg = %T, want AIActionResultMsg", msg)
			}
			if (result.Err != nil) != tc.wantErr {
				t.Errorf("Err = %v, wantErr %v", result.Err, tc.wantErr)
			}
			if tc.wantCall == "" {
				if len(client.calls) != 0 {
					t.Errorf("unexpected client calls: %v", client.calls)
				}
			} else if len(client.calls) != 1 || client.calls[0] != tc.wantCall {
				t.Errorf("calls = %v, want [%s]", client.calls, tc.wantCall)
			}
		})
	}
}

// TestExecuteActionCmdCoversAllValidatedEvents locks the contract between
// ai.Action.Validate and executeActionCmd: every review event Validate
// accepts must dispatch a client call, never silently no-op.
func TestExecuteActionCmdCoversAllValidatedEvents(t *testing.T) {
	for _, event := range []string{"approve", "comment", "request_changes"} {
		action := ai.Action{Type: ai.ActionSubmitReview, Event: event, Body: "b"}
		if err := action.Validate(); err != nil {
			t.Fatalf("Validate(%s): %v", event, err)
		}
		client := &fakeGitHubService{}
		msg := runCmd(t, executeActionCmd(client, "o", "r", 1, action))
		result := msg.(AIActionResultMsg)
		if result.Err != nil {
			t.Errorf("event %s: unexpected error %v", event, result.Err)
		}
		if len(client.calls) != 1 {
			t.Errorf("event %s: validated event produced no client call", event)
		}
	}
}

func TestExecuteActionCmdPropagatesClientError(t *testing.T) {
	client := &fakeGitHubService{err: errors.New("boom")}
	msg := runCmd(t, executeActionCmd(client, "o", "r", 1, ai.Action{Type: ai.ActionPostComment, Body: "hi"}))
	result := msg.(AIActionResultMsg)
	if result.Err == nil || !strings.Contains(result.Err.Error(), "boom") {
		t.Errorf("Err = %v, want client error propagated", result.Err)
	}
}

// newTestApp builds a minimal App with a session, thread store in a temp dir,
// and an in-flight AI turn channel.
func newTestApp(t *testing.T) App {
	t.Helper()
	app := App{
		chatPanel:   NewChatPanelModel(),
		threadStore: ai.NewThreadStore(t.TempDir()),
		session: &PRSession{
			Owner: "o", Repo: "r", Number: 1, Title: "Test PR",
			AIEventCh: make(aiEventChan),
			AICancel:  func() {},
		},
	}
	app.chatPanel.SetSize(80, 40)
	return app
}

func aiEvent(kind ai.EventKind) AIEventMsg {
	return AIEventMsg{Owner: "o", Repo: "r", PRNumber: 1, Event: ai.Event{Kind: kind}}
}

func TestHandleAIEventLifecycle(t *testing.T) {
	app := newTestApp(t)
	app.chatPanel.StartChatWaiting("orient me")

	// Thinking, command, and message events keep the stream armed.
	msg := aiEvent(ai.EventThinking)
	msg.Event.Text = "reading the diff"
	model, cmd := app.handleAIEvent(msg)
	app = model.(App)
	if cmd == nil {
		t.Fatal("non-terminal event must re-arm listenForStream")
	}

	msg = aiEvent(ai.EventMessage)
	msg.Event.Text = "here is the answer"
	model, cmd = app.handleAIEvent(msg)
	app = model.(App)
	if cmd == nil {
		t.Fatal("message event must re-arm listenForStream")
	}

	// Done clears the turn, persists the thread, and stops listening.
	model, cmd = app.handleAIEvent(aiEvent(ai.EventDone))
	app = model.(App)
	if cmd != nil {
		t.Error("EventDone must not re-arm listenForStream")
	}
	if app.session.AIEventCh != nil {
		t.Error("EventDone must clear the event channel")
	}
	if app.chatPanel.IsChatWaiting() {
		t.Error("EventDone must clear the waiting state")
	}

	cached, err := app.threadStore.Get("o", "r", 1)
	if err != nil || cached == nil {
		t.Fatalf("thread not persisted after done: %v", err)
	}
	var roles []string
	for _, m := range cached.Messages {
		roles = append(roles, m.Role)
	}
	want := []string{ai.RoleUser, ai.RoleActivity, ai.RoleAssistant}
	if strings.Join(roles, ",") != strings.Join(want, ",") {
		t.Errorf("persisted roles = %v, want %v", roles, want)
	}
}

func TestHandleAIEventErrorEndsTurn(t *testing.T) {
	app := newTestApp(t)
	app.chatPanel.StartChatWaiting("hi")

	msg := aiEvent(ai.EventError)
	msg.Event.Text = "quota exceeded"
	model, cmd := app.handleAIEvent(msg)
	app = model.(App)

	if cmd != nil {
		t.Error("EventError must not re-arm listenForStream")
	}
	if app.session.AIEventCh != nil {
		t.Error("EventError must clear the event channel")
	}
	if app.chatPanel.IsChatWaiting() {
		t.Error("EventError must clear the waiting state")
	}
}

func TestHandleAIEventMismatchedPRDropped(t *testing.T) {
	app := newTestApp(t)

	msg := AIEventMsg{Owner: "o", Repo: "r", PRNumber: 99, Event: ai.Event{Kind: ai.EventMessage, Text: "late"}}
	model, cmd := app.handleAIEvent(msg)
	app = model.(App)

	if cmd != nil {
		t.Error("mismatched-PR event must not re-arm the listener")
	}
	if len(app.chatPanel.ChatMessages()) != 0 {
		t.Errorf("mismatched-PR message leaked into transcript: %v", app.chatPanel.ChatMessages())
	}
}

func TestHandleAIEventThreadStartedForOtherPRStillPersists(t *testing.T) {
	app := newTestApp(t)

	// Pre-existing transcript for the other PR must survive the ID update.
	if err := app.threadStore.Put("o", "r", 99, "", []ai.Message{{Role: ai.RoleUser, Content: "old"}}); err != nil {
		t.Fatal(err)
	}

	msg := AIEventMsg{Owner: "o", Repo: "r", PRNumber: 99, Event: ai.Event{Kind: ai.EventThreadStarted, ThreadID: "t-late"}}
	if _, cmd := app.handleAIEvent(msg); cmd != nil {
		t.Error("mismatched-PR thread-started must not re-arm the listener")
	}

	cached, _ := app.threadStore.Get("o", "r", 99)
	if cached == nil || cached.ThreadID != "t-late" {
		t.Fatalf("thread ID not persisted for navigated-away PR: %+v", cached)
	}
	if len(cached.Messages) != 1 || cached.Messages[0].Content != "old" {
		t.Errorf("existing transcript lost on thread ID update: %+v", cached.Messages)
	}
}

func TestRecordThreadIDMatchingSession(t *testing.T) {
	app := newTestApp(t)
	app.recordThreadID("o", "r", 1, "t-123")

	if app.session.ThreadID != "t-123" {
		t.Errorf("session.ThreadID = %q", app.session.ThreadID)
	}
	cached, _ := app.threadStore.Get("o", "r", 1)
	if cached == nil || cached.ThreadID != "t-123" {
		t.Errorf("thread not persisted: %+v", cached)
	}
}

func TestHandleActionRespondDismiss(t *testing.T) {
	app := newTestApp(t)
	client := &fakeGitHubService{}
	app.ghClient = client

	msg := AIActionRespondMsg{Approve: false, Actions: []ai.Action{{Type: ai.ActionPostComment, Body: "x"}}}
	model, _ := app.handleActionRespond(msg)
	app = model.(App)

	if len(client.calls) != 0 {
		t.Errorf("dismiss must not call the client: %v", client.calls)
	}
	msgs := app.chatPanel.ChatMessages()
	if len(msgs) != 1 || !strings.Contains(msgs[0].Content, "dismissed") {
		t.Errorf("expected dismissal activity line, got %v", msgs)
	}
}

func TestHandleActionRespondApproveExecutes(t *testing.T) {
	app := newTestApp(t)
	client := &fakeGitHubService{}
	app.ghClient = client

	msg := AIActionRespondMsg{Approve: true, Actions: []ai.Action{
		{Type: ai.ActionPostComment, Body: "first"},
		{Type: ai.ActionSubmitReview, Event: "approve", Body: "lgtm"},
	}}
	_, cmd := app.handleActionRespond(msg)
	if cmd == nil {
		t.Fatal("approve must return execution commands")
	}
	// Execute the batch; each sub-command records a client call.
	collectBatchMsgs(t, cmd)

	if len(client.calls) != 2 {
		t.Fatalf("calls = %v, want 2", client.calls)
	}
	if client.calls[0] != "PostComment(first)" || client.calls[1] != "ApprovePR(lgtm)" {
		t.Errorf("calls = %v", client.calls)
	}
}

// collectBatchMsgs executes a possibly-batched tea.Cmd tree.
func collectBatchMsgs(t *testing.T, cmd tea.Cmd) {
	t.Helper()
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, sub := range batch {
			if sub != nil {
				collectBatchMsgs(t, sub)
			}
		}
	}
}

func TestBuildThreadContext(t *testing.T) {
	files := []github.PRFile{{Filename: "a.go", Patch: "+hello"}}
	comments := []github.Comment{{Body: "top-level note"}}
	inline := []github.InlineComment{
		{ID: 4242, Path: "a.go", Line: 3, Body: "inline note"},
		{ID: 4243, Path: "a.go", Line: 9, Body: "settled note", Resolved: true},
	}

	ctx := buildThreadContext("My PR", files, comments, inline, "selected hunk text")

	for _, want := range []string{"My PR", "+hello", "top-level note", "4242", "inline note", "selected hunk text"} {
		if !strings.Contains(ctx, want) {
			t.Errorf("context missing %q", want)
		}
	}
	if !strings.Contains(ctx, "4243 — a.go:9") || !strings.Contains(ctx, "[resolved]") {
		t.Error("context missing resolved-thread tag")
	}
	if strings.Contains(ctx, "4242 — a.go:3 — "+""+" [resolved]") {
		t.Error("unresolved comment wrongly tagged")
	}
}

func TestBuildThreadContextTruncatesHugeDiff(t *testing.T) {
	files := []github.PRFile{{Filename: "big.go", Patch: strings.Repeat("x", maxContextDiffBytes+1)}}
	ctx := buildThreadContext("t", files, nil, nil, "")
	if !strings.Contains(ctx, "diff truncated") {
		t.Error("huge diff not truncated")
	}

	small := []github.PRFile{{Filename: "small.go", Patch: "tiny"}}
	ctx = buildThreadContext("t", small, nil, nil, "")
	if strings.Contains(ctx, "diff truncated") {
		t.Error("small diff wrongly truncated")
	}
}

func TestBuildThreadContextEmptyDiff(t *testing.T) {
	ctx := buildThreadContext("t", nil, nil, nil, "")
	if !strings.Contains(ctx, "Diff not yet loaded") {
		t.Error("missing empty-diff notice")
	}
}

func TestBuildContextDelta(t *testing.T) {
	if got := buildContextDelta(""); got != "" {
		t.Errorf("empty delta = %q", got)
	}
	if got := buildContextDelta("hunk"); !strings.Contains(got, "hunk") {
		t.Errorf("delta = %q", got)
	}
}
