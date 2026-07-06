package demo

import (
	"context"
	"strings"
	"time"

	"github.com/shhac/prtea/internal/ai"
)

// AIEngine is a scripted fake of the codex engine for demo mode.
// It emits a realistic event sequence (thinking, commands, message) without
// any external CLI, and proposes actions when the message asks for one.
type AIEngine struct{}

// NewAIEngine creates a demo AI engine.
func NewAIEngine() *AIEngine {
	return &AIEngine{}
}

// StartThread emits a scripted turn seeded by the first user message.
func (e *AIEngine) StartThread(ctx context.Context, input ai.ThreadInput) (<-chan ai.Event, error) {
	return e.script(ctx, input.Message, true), nil
}

// Send emits a scripted follow-up turn.
func (e *AIEngine) Send(ctx context.Context, _ string, input ai.MessageInput) (<-chan ai.Event, error) {
	return e.script(ctx, input.Message, false), nil
}

// SetTimeout is a no-op for the demo engine.
func (e *AIEngine) SetTimeout(time.Duration) {}

func (e *AIEngine) script(ctx context.Context, message string, newThread bool) <-chan ai.Event {
	events := make(chan ai.Event, 16)

	go func() {
		defer close(events)
		emit := func(ev ai.Event) bool {
			select {
			case events <- ev:
				return true
			case <-ctx.Done():
				return false
			}
		}
		pause := func(d time.Duration) bool {
			select {
			case <-time.After(d):
				return true
			case <-ctx.Done():
				return false
			}
		}

		if newThread {
			if !emit(ai.Event{Kind: ai.EventThreadStarted, ThreadID: "demo-thread"}) {
				return
			}
		}
		if !pause(400 * time.Millisecond) {
			return
		}
		if !emit(ai.Event{Kind: ai.EventThinking, Text: "Reading the PR diff and comments"}) {
			return
		}
		if !pause(500 * time.Millisecond) {
			return
		}
		if !emit(ai.Event{Kind: ai.EventCommandStarted, Command: "rg -n 'handleCheckout' internal/"}) {
			return
		}
		if !pause(700 * time.Millisecond) {
			return
		}
		if !emit(ai.Event{Kind: ai.EventCommandCompleted, Command: "rg -n 'handleCheckout' internal/", ExitCode: 0}) {
			return
		}
		if !pause(400 * time.Millisecond) {
			return
		}

		response, actions := demoResponse(message)
		if !emit(ai.Event{Kind: ai.EventMessage, Text: response}) {
			return
		}
		if len(actions) > 0 {
			if !emit(ai.Event{Kind: ai.EventActionProposal, Actions: actions}) {
				return
			}
		}
		emit(ai.Event{Kind: ai.EventDone, Usage: &ai.Usage{InputTokens: 12840, CachedInputTokens: 9600, OutputTokens: 310}})
	}()

	return events
}

// demoResponse picks a canned reply, proposing an action when asked.
func demoResponse(message string) (string, []ai.Action) {
	lower := strings.ToLower(message)

	switch {
	case strings.Contains(lower, "approve"):
		return "This PR looks solid — tests cover the new retry path and the error handling is defensive. I'll propose an approval for you to confirm.",
			[]ai.Action{{Type: ai.ActionSubmitReview, Event: "approve", Body: "LGTM — clean retry implementation with good test coverage."}}
	case strings.Contains(lower, "comment"):
		return "Happy to. Here's a comment summarizing the concern for the author.",
			[]ai.Action{{Type: ai.ActionPostComment, Body: "The retry loop looks good, but consider adding jitter to the backoff to avoid thundering-herd retries."}}
	case strings.Contains(lower, "orient"):
		return "**Gist:** This PR adds retry-with-backoff to the payment webhook handler so transient provider errors no longer drop events.\n\n" +
			"**Risky parts:**\n" +
			"- The backoff loop holds the request open — check the handler timeout budget\n" +
			"- Retries are not idempotency-guarded; a duplicate webhook could double-process\n\n" +
			"**Start reading at** `internal/webhooks/handler.go` — the `handleCheckout` change is the core of it; everything else is tests and plumbing.", nil
	default:
		return "This is demo mode, so I'm a scripted stand-in for the codex engine — but in real usage I'd answer that from the diff and repo context. " +
			"Try asking me to \"orient\" you, or to \"comment\" or \"approve\" to see action proposals.", nil
	}
}
