package claude

import (
	"testing"
)

func TestChatStore_PutAndGet(t *testing.T) {
	store := NewChatStore(t.TempDir())

	messages := []ChatMessage{
		{Role: "user", Content: "What does this PR do?"},
		{Role: "assistant", Content: "It adds a frobnicate function."},
	}

	err := store.Put("alice", "widget-factory", 42, messages)
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	got, err := store.Get("alice", "widget-factory", 42)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got == nil {
		t.Fatal("Get returned nil")
	}
	if len(got.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(got.Messages))
	}
	if got.Messages[0].Role != "user" || got.Messages[0].Content != "What does this PR do?" {
		t.Errorf("unexpected first message: %+v", got.Messages[0])
	}
	if got.Messages[1].Role != "assistant" || got.Messages[1].Content != "It adds a frobnicate function." {
		t.Errorf("unexpected second message: %+v", got.Messages[1])
	}
	if got.UpdatedAt.IsZero() {
		t.Error("UpdatedAt should not be zero")
	}
}

func TestChatStore_GetNotFound(t *testing.T) {
	store := NewChatStore(t.TempDir())

	got, err := store.Get("bob", "test-project", 99)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for non-existent cache, got %+v", got)
	}
}

func TestChatStore_PutEmpty(t *testing.T) {
	store := NewChatStore(t.TempDir())

	// Putting empty messages should be a no-op
	err := store.Put("alice", "widget-factory", 42, nil)
	if err != nil {
		t.Fatalf("Put with nil messages failed: %v", err)
	}

	got, err := store.Get("alice", "widget-factory", 42)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil after putting empty messages, got %+v", got)
	}
}

func TestChatStore_Delete(t *testing.T) {
	store := NewChatStore(t.TempDir())

	messages := []ChatMessage{
		{Role: "user", Content: "hello"},
	}
	if err := store.Put("alice", "widget-factory", 42, messages); err != nil {
		t.Fatal(err)
	}

	if err := store.Delete("alice", "widget-factory", 42); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	got, err := store.Get("alice", "widget-factory", 42)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("expected nil after delete, got %+v", got)
	}
}

func TestChatStore_DeleteNonExistent(t *testing.T) {
	store := NewChatStore(t.TempDir())

	// Deleting a non-existent session should not error
	if err := store.Delete("alice", "widget-factory", 99); err != nil {
		t.Fatalf("Delete non-existent failed: %v", err)
	}
}

func TestChatStore_Overwrite(t *testing.T) {
	store := NewChatStore(t.TempDir())

	m1 := []ChatMessage{{Role: "user", Content: "first"}}
	m2 := []ChatMessage{
		{Role: "user", Content: "first"},
		{Role: "assistant", Content: "response"},
		{Role: "user", Content: "second"},
	}

	if err := store.Put("alice", "widget-factory", 1, m1); err != nil {
		t.Fatal(err)
	}
	if err := store.Put("alice", "widget-factory", 1, m2); err != nil {
		t.Fatal(err)
	}

	got, err := store.Get("alice", "widget-factory", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Messages) != 3 {
		t.Errorf("expected 3 messages after overwrite, got %d", len(got.Messages))
	}
}
