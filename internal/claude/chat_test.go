package claude

import (
	"fmt"
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
		prompt := buildChatPrompt(session, input, defaultMaxPromptTokens, defaultMaxHistoryMessages)
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
		prompt := buildChatPrompt(session, input, defaultMaxPromptTokens, defaultMaxHistoryMessages)
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
		prompt := buildChatPrompt(session, input, defaultMaxPromptTokens, defaultMaxHistoryMessages)
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

func TestEstimateTokens(t *testing.T) {
	// 300 chars of code ≈ 100 tokens
	code := strings.Repeat("x", 300)
	tokens := estimateTokens(code)
	if tokens != 100 {
		t.Errorf("estimateTokens(%d chars) = %d, want 100", len(code), tokens)
	}

	if estimateTokens("") != 0 {
		t.Error("empty string should be 0 tokens")
	}
}

func TestBuildChatPrompt_TokenBudget_DropsOldMessages(t *testing.T) {
	// Create a session with many messages
	var messages []ChatMessage
	for i := 0; i < 30; i++ {
		messages = append(messages,
			ChatMessage{Role: "user", Content: fmt.Sprintf("question %d", i)},
			ChatMessage{Role: "assistant", Content: fmt.Sprintf("answer %d", i)},
		)
	}
	session := &ChatSession{
		PRContext: "small context",
		Messages:  messages,
	}

	input := ChatInput{
		PRContext: "small context",
		Message:   "final question",
	}

	prompt := buildChatPrompt(session, input, defaultMaxPromptTokens, defaultMaxHistoryMessages)

	// Should contain the most recent messages but not the earliest ones
	if !strings.Contains(prompt, "final question") {
		t.Error("prompt should contain the current message")
	}
	// With maxHistoryMessages=16, messages 0-21 (indices) should be dropped
	if strings.Contains(prompt, "question 0\n") {
		t.Error("prompt should have dropped oldest messages due to maxHistoryMessages limit")
	}
	// Recent messages should still be present
	if !strings.Contains(prompt, "question 29") {
		t.Error("prompt should contain the most recent messages")
	}
}

func TestBuildChatPrompt_TokenBudget_TruncatesDiff(t *testing.T) {
	// Create a very large PR context (simulate huge diff)
	largeDiff := strings.Repeat("+ added line\n", 100000) // ~1.3MB
	session := &ChatSession{
		Messages: []ChatMessage{
			{Role: "user", Content: "what does this do?"},
			{Role: "assistant", Content: "it does things"},
		},
	}

	input := ChatInput{
		PRContext: largeDiff,
		Message:   "explain more",
	}

	prompt := buildChatPrompt(session, input, defaultMaxPromptTokens, defaultMaxHistoryMessages)

	// The prompt should be truncated
	if !strings.Contains(prompt, "[... diff truncated to fit context window ...]") {
		t.Error("large diff should be truncated")
	}

	// Should still contain the user message and history
	if !strings.Contains(prompt, "explain more") {
		t.Error("prompt should still contain user message after truncation")
	}
	if !strings.Contains(prompt, "it does things") {
		t.Error("prompt should still contain conversation history after truncation")
	}
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
	svc := NewChatService(nil, 0, nil, 0, 0, 0)

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

func TestChatService_SaveAndGetSession(t *testing.T) {
	store := NewChatStore(t.TempDir())
	svc := NewChatService(nil, 0, store, 0, 0, 0)

	// Create a session manually
	svc.mu.Lock()
	svc.sessions["alice_widget-factory_42"] = &ChatSession{
		PRContext: "test",
		Messages: []ChatMessage{
			{Role: "user", Content: "what does this do?"},
			{Role: "assistant", Content: "it frobnicates"},
		},
	}
	svc.mu.Unlock()

	// Save to disk
	svc.SaveSession("alice", "widget-factory", 42)

	// Clear in-memory session
	svc.mu.Lock()
	delete(svc.sessions, "alice_widget-factory_42")
	svc.mu.Unlock()

	// GetSessionMessages should restore from disk
	msgs := svc.GetSessionMessages("alice", "widget-factory", 42)
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].Content != "what does this do?" {
		t.Errorf("unexpected first message: %q", msgs[0].Content)
	}

	// Should now also be in memory
	svc.mu.Lock()
	_, exists := svc.sessions["alice_widget-factory_42"]
	svc.mu.Unlock()
	if !exists {
		t.Error("session should be restored in memory after GetSessionMessages")
	}
}

func TestChatService_GetSessionMessages_Empty(t *testing.T) {
	svc := NewChatService(nil, 0, nil, 0, 0, 0)
	msgs := svc.GetSessionMessages("alice", "widget-factory", 99)
	if msgs != nil {
		t.Errorf("expected nil for non-existent session, got %+v", msgs)
	}
}

func TestChatService_ClearSession_WithStore(t *testing.T) {
	store := NewChatStore(t.TempDir())
	svc := NewChatService(nil, 0, store, 0, 0, 0)

	// Put a session in memory and on disk
	svc.mu.Lock()
	svc.sessions["alice_widget-factory_42"] = &ChatSession{
		Messages: []ChatMessage{{Role: "user", Content: "hello"}},
	}
	svc.mu.Unlock()
	svc.SaveSession("alice", "widget-factory", 42)

	// Clear should remove from both
	svc.ClearSession("alice", "widget-factory", 42)

	// Memory should be empty
	svc.mu.Lock()
	_, exists := svc.sessions["alice_widget-factory_42"]
	svc.mu.Unlock()
	if exists {
		t.Error("session should be cleared from memory")
	}

	// Disk should be empty
	cached, _ := store.Get("alice", "widget-factory", 42)
	if cached != nil {
		t.Error("session should be cleared from disk")
	}
}
