package ai

import (
	"context"
	"fmt"
	"strings"
)

// CodexEngine runs conversations through the codex CLI (`codex exec --json`).
// Threads are codex sessions: the first turn creates one, follow-ups resume it,
// so conversation state lives with codex rather than being replayed in prompts.
//
// The engine is an immutable value; per-turn deadlines belong to the caller's
// context.
type CodexEngine struct {
	executor CommandExecutor
	model    string
	effort   string
}

// NewCodexEngine creates an engine. executor handles subprocess spawning.
func NewCodexEngine(executor CommandExecutor, model, effort string) *CodexEngine {
	return &CodexEngine{
		executor: executor,
		model:    model,
		effort:   effort,
	}
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
// the channel is closed after EventDone or EventError. Cancel the context to
// abandon the turn (the caller owns any deadline).
func (e *CodexEngine) StartThread(ctx context.Context, input ThreadInput) (<-chan Event, error) {
	args := []string{
		"exec",
		"--json",
		"--sandbox", "read-only",
		"-m", e.model,
		"-c", fmt.Sprintf("model_reasoning_effort=%q", e.effort),
	}
	if input.RepoPath != "" {
		args = append(args, "-C", input.RepoPath)
	} else {
		args = append(args, "--skip-git-repo-check")
	}
	args = append(args, "-") // prompt on stdin

	return e.run(ctx, args, buildThreadPrompt(input))
}

// Send resumes an existing thread with a follow-up message.
func (e *CodexEngine) Send(ctx context.Context, threadID string, input MessageInput) (<-chan Event, error) {
	// resume does not accept --sandbox; the mode carries over from creation.
	args := []string{
		"exec", "resume", threadID,
		"--json",
		"-m", e.model,
		"-",
	}

	return e.run(ctx, args, buildFollowUpPrompt(input))
}

func (e *CodexEngine) run(ctx context.Context, args []string, prompt string) (<-chan Event, error) {
	proc, err := e.executor.Start(ctx, args, strings.NewReader(prompt))
	if err != nil {
		return nil, err
	}

	events := make(chan Event, 16)
	go func() {
		defer close(events)
		streamCodexEvents(ctx, proc, events)
	}()

	return events, nil
}
