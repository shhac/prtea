package ai

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
)

// Emit sends v on ch unless the context is cancelled (e.g. the consumer
// abandoned the turn and stopped draining). Returns false when the send was
// dropped. Shared by the codex stream, the demo engine, and the UI pump so
// cancellation semantics cannot drift.
func Emit[T any](ctx context.Context, ch chan<- T, v T) bool {
	select {
	case ch <- v:
		return true
	case <-ctx.Done():
		return false
	}
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

// streamCodexEvents parses JSONL from the codex process and emits normalized
// events.
//
// The most recent agent message is held back until another event follows it:
// only the final message may carry a prtea-action block, and it must be
// stripped before display. All mid-stream emissions flush the held message
// first, so the "only the final message carries actions" rule is structural.
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

	var heldMessage *string
	flushHeld := func() {
		if heldMessage != nil {
			Emit(ctx, events, Event{Kind: EventMessage, Text: *heldMessage})
			heldMessage = nil
		}
	}
	emitFlushing := func(ev Event) {
		flushHeld()
		Emit(ctx, events, ev)
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
			emitFlushing(Event{Kind: EventThreadStarted, ThreadID: ev.ThreadID})
		case "item.started":
			if ev.Item != nil && ev.Item.Type == "command_execution" {
				emitFlushing(Event{Kind: EventCommandStarted, Command: ev.Item.Command})
			}
		case "item.completed":
			if ev.Item == nil {
				continue
			}
			switch ev.Item.Type {
			case "command_execution":
				exitCode := 0
				if ev.Item.ExitCode != nil {
					exitCode = *ev.Item.ExitCode
				}
				emitFlushing(Event{Kind: EventCommandCompleted, Command: ev.Item.Command, ExitCode: exitCode})
			case "reasoning":
				if ev.Item.Text != "" {
					emitFlushing(Event{Kind: EventThinking, Text: ev.Item.Text})
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
			msg := ev.Message
			if ev.Error != nil && ev.Error.Message != "" {
				msg = ev.Error.Message
			}
			if msg == "" {
				msg = "codex turn failed"
			}
			emitFlushing(Event{Kind: EventError, Text: msg})
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
	if msg := finalErrorMessage(ctx, scanErr, waitErr, stderrBuf.String()); msg != "" {
		Emit(ctx, events, Event{Kind: EventError, Text: msg})
		return
	}

	// Final message: strip any trailing action block before display.
	if heldMessage != nil {
		remaining, actions := ExtractActions(*heldMessage)
		if strings.TrimSpace(remaining) != "" {
			Emit(ctx, events, Event{Kind: EventMessage, Text: remaining})
		}
		if len(actions) > 0 {
			Emit(ctx, events, Event{Kind: EventActionProposal, Actions: actions})
		}
	}
	Emit(ctx, events, Event{Kind: EventDone, Usage: usage})
}

// finalErrorMessage selects the error to surface when the stream ends without
// an explicit turn.failed event. Returns "" when the turn ended cleanly.
func finalErrorMessage(ctx context.Context, scanErr, waitErr error, stderr string) string {
	if scanErr != nil {
		return fmt.Sprintf("failed reading codex output: %v", scanErr)
	}
	if waitErr == nil {
		return ""
	}
	if ctx.Err() == context.DeadlineExceeded {
		return "codex timed out"
	}
	msg := strings.TrimSpace(stderr)
	if len(msg) > 500 {
		msg = msg[:500]
	}
	if msg == "" {
		msg = waitErr.Error()
	}
	return msg
}
