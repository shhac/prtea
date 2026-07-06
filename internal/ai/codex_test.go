package ai

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

// fakeExecutor returns a scripted Process and records the invocation.
type fakeExecutor struct {
	stdout  string
	stderr  string
	waitErr error

	gotArgs []string
	gotOpts ExecOptions
}

func (f *fakeExecutor) Start(_ context.Context, args []string, opts ExecOptions) (*Process, error) {
	f.gotArgs = args
	f.gotOpts = opts
	return &Process{
		Stdout: io.NopCloser(strings.NewReader(f.stdout)),
		Stderr: io.NopCloser(strings.NewReader(f.stderr)),
		Wait:   func() error { return f.waitErr },
	}, nil
}

func collectEvents(t *testing.T, ch <-chan Event) []Event {
	t.Helper()
	var events []Event
	timeout := time.After(5 * time.Second)
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return events
			}
			events = append(events, ev)
		case <-timeout:
			t.Fatal("timed out waiting for events channel to close")
		}
	}
}

// Golden lines captured from a real `codex exec --json` run (codex-cli 0.138.0).
const goldenStream = `{"type":"thread.started","thread_id":"019f382c-19a5-7e52-b7a7-0b82ea0e7271"}
{"type":"turn.started"}
{"type":"item.completed","item":{"id":"item_0","type":"agent_message","text":"I'll read the README title line."}}
{"type":"item.started","item":{"id":"item_1","type":"command_execution","command":"sed -n '1p' README.md","aggregated_output":"","exit_code":null,"status":"in_progress"}}
{"type":"item.completed","item":{"id":"item_1","type":"command_execution","command":"sed -n '1p' README.md","aggregated_output":"# prtea\n","exit_code":0,"status":"completed"}}
{"type":"item.completed","item":{"id":"item_2","type":"agent_message","text":"The project is called prtea."}}
{"type":"turn.completed","usage":{"input_tokens":59081,"cached_input_tokens":50432,"output_tokens":96}}
`

func TestStartThreadParsesGoldenStream(t *testing.T) {
	exec := &fakeExecutor{stdout: goldenStream}
	engine := NewCodexEngine(exec, "gpt-5.5", "medium", time.Minute)

	ch, err := engine.StartThread(context.Background(), ThreadInput{
		Owner: "o", Repo: "r", PRNumber: 1,
		PRContext: "diff", Message: "hi", RepoPath: "/tmp/repo",
	})
	if err != nil {
		t.Fatalf("StartThread: %v", err)
	}
	events := collectEvents(t, ch)

	wantKinds := []EventKind{
		EventThreadStarted, EventMessage, EventCommandStarted,
		EventCommandCompleted, EventMessage, EventDone,
	}
	if len(events) != len(wantKinds) {
		t.Fatalf("got %d events, want %d: %+v", len(events), len(wantKinds), events)
	}
	for i, kind := range wantKinds {
		if events[i].Kind != kind {
			t.Errorf("event %d kind = %s, want %s", i, events[i].Kind, kind)
		}
	}

	if events[0].ThreadID != "019f382c-19a5-7e52-b7a7-0b82ea0e7271" {
		t.Errorf("thread ID = %q", events[0].ThreadID)
	}
	if events[3].ExitCode != 0 || events[3].Command != "sed -n '1p' README.md" {
		t.Errorf("command completed event = %+v", events[3])
	}
	done := events[len(events)-1]
	if done.Usage == nil || done.Usage.OutputTokens != 96 {
		t.Errorf("done usage = %+v", done.Usage)
	}
}

func TestStartThreadArgsAndStdin(t *testing.T) {
	exec := &fakeExecutor{stdout: goldenStream}
	engine := NewCodexEngine(exec, "gpt-5.5", "low", time.Minute)

	ch, err := engine.StartThread(context.Background(), ThreadInput{
		Owner: "o", Repo: "r", PRNumber: 7,
		PRContext: "THE DIFF", Message: "what is going on",
		RepoPath: "/tmp/repo",
	})
	if err != nil {
		t.Fatalf("StartThread: %v", err)
	}
	collectEvents(t, ch)

	args := strings.Join(exec.gotArgs, " ")
	for _, want := range []string{
		"exec", "--json", "--sandbox read-only",
		"-m gpt-5.5", `model_reasoning_effort="low"`, "-C /tmp/repo",
	} {
		if !strings.Contains(args, want) {
			t.Errorf("args missing %q: %s", want, args)
		}
	}
	if exec.gotArgs[len(exec.gotArgs)-1] != "-" {
		t.Errorf("last arg should be stdin sentinel, got %q", exec.gotArgs[len(exec.gotArgs)-1])
	}

	prompt, _ := io.ReadAll(exec.gotOpts.Stdin)
	for _, want := range []string{"THE DIFF", "what is going on", "prtea-action", "o/r #7"} {
		if !strings.Contains(string(prompt), want) {
			t.Errorf("stdin prompt missing %q", want)
		}
	}
}

func TestStartThreadWithoutRepoSkipsGitCheck(t *testing.T) {
	exec := &fakeExecutor{stdout: goldenStream}
	engine := NewCodexEngine(exec, "gpt-5.5", "medium", time.Minute)

	ch, _ := engine.StartThread(context.Background(), ThreadInput{Message: "hi"})
	collectEvents(t, ch)

	args := strings.Join(exec.gotArgs, " ")
	if !strings.Contains(args, "--skip-git-repo-check") {
		t.Errorf("args missing --skip-git-repo-check: %s", args)
	}
	if strings.Contains(args, "-C ") {
		t.Errorf("args should not contain -C without a repo path: %s", args)
	}
}

func TestSendResumesThread(t *testing.T) {
	exec := &fakeExecutor{stdout: goldenStream}
	engine := NewCodexEngine(exec, "gpt-5.5", "medium", time.Minute)

	ch, err := engine.Send(context.Background(), "thread-123", MessageInput{Message: "follow up"})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	collectEvents(t, ch)

	args := strings.Join(exec.gotArgs, " ")
	if !strings.Contains(args, "exec resume thread-123") {
		t.Errorf("args missing resume: %s", args)
	}
	if strings.Contains(args, "--sandbox") {
		t.Errorf("resume must not pass --sandbox: %s", args)
	}

	prompt, _ := io.ReadAll(exec.gotOpts.Stdin)
	if string(prompt) != "follow up" {
		t.Errorf("stdin = %q, want bare message", string(prompt))
	}
}

func TestActionProposalEmittedFromFinalMessage(t *testing.T) {
	stream := `{"type":"thread.started","thread_id":"t1"}
{"type":"item.completed","item":{"id":"i1","type":"agent_message","text":"Posting for you.\n\n` +
		"```prtea-action\\n{\\\"type\\\":\\\"post_comment\\\",\\\"body\\\":\\\"LGTM\\\"}\\n```" + `"}}
{"type":"turn.completed","usage":{"input_tokens":1,"cached_input_tokens":0,"output_tokens":2}}
`
	exec := &fakeExecutor{stdout: stream}
	engine := NewCodexEngine(exec, "gpt-5.5", "medium", time.Minute)

	ch, _ := engine.StartThread(context.Background(), ThreadInput{Message: "post lgtm"})
	events := collectEvents(t, ch)

	var proposal *Event
	for i := range events {
		if events[i].Kind == EventActionProposal {
			proposal = &events[i]
		}
		if events[i].Kind == EventMessage && strings.Contains(events[i].Text, "prtea-action") {
			t.Errorf("displayed message leaks raw action block: %q", events[i].Text)
		}
	}
	if proposal == nil {
		t.Fatalf("no action proposal in events: %+v", events)
	}
	if len(proposal.Actions) != 1 || proposal.Actions[0].Type != ActionPostComment || proposal.Actions[0].Body != "LGTM" {
		t.Errorf("actions = %+v", proposal.Actions)
	}
}

func TestTurnFailedEmitsError(t *testing.T) {
	stream := `{"type":"thread.started","thread_id":"t1"}
{"type":"turn.failed","error":{"message":"quota exceeded"}}
`
	exec := &fakeExecutor{stdout: stream, waitErr: errors.New("exit status 1")}
	engine := NewCodexEngine(exec, "gpt-5.5", "medium", time.Minute)

	ch, _ := engine.StartThread(context.Background(), ThreadInput{Message: "hi"})
	events := collectEvents(t, ch)

	last := events[len(events)-1]
	if last.Kind != EventError || !strings.Contains(last.Text, "quota exceeded") {
		t.Errorf("last event = %+v, want error with message", last)
	}
	for _, ev := range events {
		if ev.Kind == EventDone {
			t.Error("EventDone must not be emitted after failure")
		}
	}
}

func TestProcessErrorSurfacesStderr(t *testing.T) {
	exec := &fakeExecutor{stdout: "", stderr: "codex: something broke\n", waitErr: errors.New("exit status 2")}
	engine := NewCodexEngine(exec, "gpt-5.5", "medium", time.Minute)

	ch, _ := engine.StartThread(context.Background(), ThreadInput{Message: "hi"})
	events := collectEvents(t, ch)

	if len(events) != 1 || events[0].Kind != EventError {
		t.Fatalf("events = %+v, want single error", events)
	}
	if !strings.Contains(events[0].Text, "something broke") {
		t.Errorf("error text = %q", events[0].Text)
	}
}
