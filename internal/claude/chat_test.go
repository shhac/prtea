package claude

import (
	"strings"
	"testing"
)

func TestSessionKey(t *testing.T) {
	tests := []struct {
		owner    string
		repo     string
		prNumber int
		want     string
	}{
		{"alice", "widget-factory", 42, "alice_widget-factory_42"},
		{"bob", "test-project", 7, "bob_test-project_7"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := sessionKey(tt.owner, tt.repo, tt.prNumber); got != tt.want {
				t.Errorf("sessionKey(%q, %q, %d) = %q, want %q",
					tt.owner, tt.repo, tt.prNumber, got, tt.want)
			}
		})
	}
}

func TestBuildChatPrompt(t *testing.T) {
	session := &ChatSession{
		PRContext: "PR #42: \"Add frobnicate function\" in alice/widget-factory",
	}

	t.Run("first message", func(t *testing.T) {
		input := ChatInput{
			PRContext: session.PRContext,
			Message:   "What does this PR do?",
		}
		prompt := buildChatPrompt(session, input)
		if !strings.Contains(prompt, "PR #42") {
			t.Error("prompt should contain PR context")
		}
		if !strings.Contains(prompt, "What does this PR do?") {
			t.Error("prompt should contain user message")
		}
		if !strings.Contains(prompt, "Answer questions about this PR") {
			t.Error("prompt should contain full-PR instruction")
		}
	})

	t.Run("with hunks selected", func(t *testing.T) {
		input := ChatInput{
			PRContext:     session.PRContext,
			HunksSelected: true,
			Message:       "What does this do?",
		}
		prompt := buildChatPrompt(session, input)
		if !strings.Contains(prompt, "selected specific code hunks") {
			t.Error("prompt should contain hunk-focused instruction")
		}
		if strings.Contains(prompt, "Answer questions about this PR") {
			t.Error("prompt should NOT contain full-PR instruction when hunks are selected")
		}
	})

	t.Run("with history", func(t *testing.T) {
		session.Messages = []ChatMessage{
			{Role: "user", Content: "What does this do?"},
			{Role: "assistant", Content: "It adds a frobnicate function."},
		}
		input := ChatInput{
			PRContext: session.PRContext,
			Message:   "Is it safe?",
		}
		prompt := buildChatPrompt(session, input)
		if !strings.Contains(prompt, "What does this do?") {
			t.Error("prompt should contain previous user message")
		}
		if !strings.Contains(prompt, "It adds a frobnicate function.") {
			t.Error("prompt should contain previous assistant message")
		}
		if !strings.Contains(prompt, "Is it safe?") {
			t.Error("prompt should contain new user message")
		}
	})
}

func TestExtractResultText(t *testing.T) {
	t.Run("string result", func(t *testing.T) {
		event := &StreamEvent{Type: "result", Result: "The answer is 42"}
		got := extractResultText(event)
		if got != "The answer is 42" {
			t.Errorf("got %q, want %q", got, "The answer is 42")
		}
	})

	t.Run("nil result", func(t *testing.T) {
		event := &StreamEvent{Type: "result", Result: nil}
		got := extractResultText(event)
		if got != "" {
			t.Errorf("got %q, want empty string", got)
		}
	})

	t.Run("map result", func(t *testing.T) {
		event := &StreamEvent{Type: "result", Result: map[string]interface{}{"key": "value"}}
		got := extractResultText(event)
		if !strings.Contains(got, "key") {
			t.Errorf("expected JSON containing 'key', got %q", got)
		}
	})
}

func TestChatService_ClearSession(t *testing.T) {
	svc := NewChatService("/usr/local/bin/claude", 0)

	// Create a session manually
	svc.mu.Lock()
	svc.sessions["alice_widget-factory_42"] = &ChatSession{
		PRContext: "test",
		Messages:  []ChatMessage{{Role: "user", Content: "hello"}},
	}
	svc.mu.Unlock()

	svc.ClearSession("alice", "widget-factory", 42)

	svc.mu.Lock()
	_, exists := svc.sessions["alice_widget-factory_42"]
	svc.mu.Unlock()

	if exists {
		t.Error("session should have been cleared")
	}
}
