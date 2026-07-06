package ai

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

// CodexEngine runs conversations through the codex CLI (`codex exec --json`).
// Threads are codex sessions: the first turn creates one, follow-ups resume it,
// so conversation state lives with codex rather than being replayed in prompts.
type CodexEngine struct {
	executor CommandExecutor

	mu      sync.RWMutex
	model   string
	effort  string
	timeout time.Duration
}

// NewCodexEngine creates an engine. executor handles subprocess spawning.
func NewCodexEngine(executor CommandExecutor, model, effort string, timeout time.Duration) *CodexEngine {
	return &CodexEngine{
		executor: executor,
		model:    model,
		effort:   effort,
		timeout:  timeout,
	}
}

// SetTimeout updates the per-turn timeout for future requests.
func (e *CodexEngine) SetTimeout(d time.Duration) {
	e.mu.Lock()
	e.timeout = d
	e.mu.Unlock()
}

func (e *CodexEngine) config() (model, effort string, timeout time.Duration) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.model, e.effort, e.timeout
}

// ThreadInput seeds a new conversation thread about a PR.
type ThreadInput struct {
	Owner        string
	Repo         string
	PRNumber     int
	PRContext    string // assembled PR metadata + diff + comments
	CustomPrompt string // per-repo review instructions (may be empty)
	RepoPath     string // local checkout matching the PR's repo; empty = no filesystem context
	Message      string // first user message
}

// MessageInput is a follow-up message on an existing thread.
type MessageInput struct {
	Message      string
	ContextDelta string // optional refreshed context (e.g. selected hunks, new comments)
}

// StartThread begins a new codex session seeded with PR context and the first
// user message. Events stream on the returned channel until the turn ends;
// the channel is closed after EventDone or EventError.
func (e *CodexEngine) StartThread(ctx context.Context, input ThreadInput) (<-chan Event, error) {
	model, effort, timeout := e.config()

	args := []string{
		"exec",
		"--json",
		"--sandbox", "read-only",
		"-m", model,
		"-c", fmt.Sprintf("model_reasoning_effort=%q", effort),
	}
	if input.RepoPath != "" {
		args = append(args, "-C", input.RepoPath)
	} else {
		args = append(args, "--skip-git-repo-check")
	}
	args = append(args, "-") // prompt on stdin

	prompt := buildThreadPrompt(input)
	return e.run(ctx, args, ExecOptions{Stdin: strings.NewReader(prompt)}, timeout)
}

// Send resumes an existing thread with a follow-up message.
func (e *CodexEngine) Send(ctx context.Context, threadID string, input MessageInput) (<-chan Event, error) {
	model, _, timeout := e.config()

	// resume does not accept --sandbox; the mode carries over from creation.
	args := []string{
		"exec", "resume", threadID,
		"--json",
		"-m", model,
		"-",
	}

	prompt := buildFollowUpPrompt(input)
	return e.run(ctx, args, ExecOptions{Stdin: strings.NewReader(prompt)}, timeout)
}

func (e *CodexEngine) run(ctx context.Context, args []string, opts ExecOptions, timeout time.Duration) (<-chan Event, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)

	proc, err := e.executor.Start(ctx, args, opts)
	if err != nil {
		cancel()
		return nil, err
	}

	events := make(chan Event, 16)
	go func() {
		defer close(events)
		defer cancel()
		streamCodexEvents(ctx, proc, events)
	}()

	return events, nil
}

// codexEvent mirrors the JSONL lines emitted by `codex exec --json`.
type codexEvent struct {
	Type     string      `json:"type"`
	ThreadID string      `json:"thread_id,omitempty"`
	Item     *codexItem  `json:"item,omitempty"`
	Usage    *Usage      `json:"usage,omitempty"`
	Error    *codexError `json:"error,omitempty"`
	Message  string      `json:"message,omitempty"`
}

type codexItem struct {
	ID       string `json:"id"`
	Type     string `json:"type"` // agent_message | command_execution | reasoning | ...
	Text     string `json:"text,omitempty"`
	Command  string `json:"command,omitempty"`
	ExitCode *int   `json:"exit_code,omitempty"`
	Status   string `json:"status,omitempty"`
}

type codexError struct {
	Message string `json:"message"`
}

// emit sends an event unless the context is cancelled (e.g. the UI abandoned
// the turn and stopped draining). Returns false when the send was dropped.
func emit(ctx context.Context, events chan<- Event, ev Event) bool {
	select {
	case events <- ev:
		return true
	case <-ctx.Done():
		return false
	}
}

// streamCodexEvents parses JSONL from the codex process and emits normalized
// events. The final agent message is scanned for prtea-action blocks.
func streamCodexEvents(ctx context.Context, proc *Process, events chan<- Event) {
	var stderrBuf strings.Builder
	var stderrWg sync.WaitGroup
	stderrWg.Add(1)
	go func() {
		defer stderrWg.Done()
		scanner := bufio.NewScanner(proc.Stderr)
		scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
		for scanner.Scan() {
			stderrBuf.WriteString(scanner.Text())
			stderrBuf.WriteByte('\n')
		}
	}()

	// The most recent agent message is held back until the next event arrives:
	// only the final message may carry a prtea-action block, and it must be
	// stripped before display. Intermediate messages flush as soon as another
	// item follows them.
	var heldMessage *string
	flushHeld := func() {
		if heldMessage != nil {
			emit(ctx, events, Event{Kind: EventMessage, Text: *heldMessage})
			heldMessage = nil
		}
	}

	var usage *Usage
	failed := false

	scanner := bufio.NewScanner(proc.Stdout)
	scanner.Buffer(make([]byte, 1024*1024), 10*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var ev codexEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}

		switch ev.Type {
		case "thread.started":
			emit(ctx, events, Event{Kind: EventThreadStarted, ThreadID: ev.ThreadID})
		case "item.started":
			if ev.Item != nil && ev.Item.Type == "command_execution" {
				flushHeld()
				emit(ctx, events, Event{Kind: EventCommandStarted, Command: ev.Item.Command})
			}
		case "item.completed":
			if ev.Item == nil {
				continue
			}
			switch ev.Item.Type {
			case "command_execution":
				flushHeld()
				exitCode := 0
				if ev.Item.ExitCode != nil {
					exitCode = *ev.Item.ExitCode
				}
				emit(ctx, events, Event{Kind: EventCommandCompleted, Command: ev.Item.Command, ExitCode: exitCode})
			case "reasoning":
				if ev.Item.Text != "" {
					flushHeld()
					emit(ctx, events, Event{Kind: EventThinking, Text: ev.Item.Text})
				}
			case "agent_message":
				flushHeld()
				text := ev.Item.Text
				heldMessage = &text
			}
		case "turn.completed":
			usage = ev.Usage
		case "turn.failed", "error":
			failed = true
			flushHeld()
			msg := ev.Message
			if ev.Error != nil && ev.Error.Message != "" {
				msg = ev.Error.Message
			}
			if msg == "" {
				msg = "codex turn failed"
			}
			emit(ctx, events, Event{Kind: EventError, Text: msg})
		}
	}
	scanErr := scanner.Err()

	// Drain remaining output so the process can exit, then reap it.
	_, _ = io.Copy(io.Discard, proc.Stdout)
	stderrWg.Wait()
	waitErr := proc.Wait()

	if failed {
		return
	}
	if scanErr != nil {
		emit(ctx, events, Event{Kind: EventError, Text: fmt.Sprintf("failed reading codex output: %v", scanErr)})
		return
	}
	if waitErr != nil {
		msg := stderrBuf.String()
		if len(msg) > 500 {
			msg = msg[:500]
		}
		if ctx.Err() == context.DeadlineExceeded {
			msg = "codex timed out"
		} else if strings.TrimSpace(msg) == "" {
			msg = waitErr.Error()
		}
		emit(ctx, events, Event{Kind: EventError, Text: strings.TrimSpace(msg)})
		return
	}

	// Final message: strip any trailing action block before display.
	if heldMessage != nil {
		remaining, actions := ExtractActions(*heldMessage)
		if strings.TrimSpace(remaining) != "" {
			emit(ctx, events, Event{Kind: EventMessage, Text: remaining})
		}
		if len(actions) > 0 {
			emit(ctx, events, Event{Kind: EventActionProposal, Actions: actions})
		}
	}
	emit(ctx, events, Event{Kind: EventDone, Usage: usage})
}
