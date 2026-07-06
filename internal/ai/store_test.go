package ai

import (
	"testing"
)

func TestThreadStoreRoundTrip(t *testing.T) {
	store := NewThreadStore(t.TempDir())

	messages := []Message{
		{Role: RoleUser, Content: "orient me"},
		{Role: RoleActivity, Content: "ran `git log`"},
		{Role: RoleAssistant, Content: "This PR does X."},
	}
	if err := store.Put("owner", "repo", 42, "thread-abc", messages); err != nil {
		t.Fatalf("Put: %v", err)
	}

	cached, err := store.Get("owner", "repo", 42)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if cached == nil {
		t.Fatal("Get returned nil")
	}
	if cached.ThreadID != "thread-abc" {
		t.Errorf("ThreadID = %q", cached.ThreadID)
	}
	if len(cached.Messages) != 3 || cached.Messages[2].Content != "This PR does X." {
		t.Errorf("Messages = %+v", cached.Messages)
	}
	if cached.UpdatedAt.IsZero() {
		t.Error("UpdatedAt not set")
	}
}

func TestThreadStoreGetMissing(t *testing.T) {
	store := NewThreadStore(t.TempDir())
	cached, err := store.Get("o", "r", 1)
	if err != nil || cached != nil {
		t.Errorf("Get missing = %+v, %v; want nil, nil", cached, err)
	}
}

func TestThreadStoreDelete(t *testing.T) {
	store := NewThreadStore(t.TempDir())
	if err := store.Put("o", "r", 1, "t1", []Message{{Role: RoleUser, Content: "x"}}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := store.Delete("o", "r", 1); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	cached, _ := store.Get("o", "r", 1)
	if cached != nil {
		t.Errorf("thread still present after delete: %+v", cached)
	}
	// Deleting again is not an error.
	if err := store.Delete("o", "r", 1); err != nil {
		t.Errorf("second Delete: %v", err)
	}
}

func TestThreadStorePutEmptyIsNoop(t *testing.T) {
	store := NewThreadStore(t.TempDir())
	if err := store.Put("o", "r", 1, "", nil); err != nil {
		t.Fatalf("Put empty: %v", err)
	}
	cached, _ := store.Get("o", "r", 1)
	if cached != nil {
		t.Errorf("empty Put should not persist: %+v", cached)
	}
}
