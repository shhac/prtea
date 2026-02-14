package claude

import (
	"testing"
	"time"
)

func TestAnalysisStore_PutAndGet(t *testing.T) {
	store := NewAnalysisStore(t.TempDir())

	result := &AnalysisResult{
		Summary: "Add frobnicate function",
		Risk:    RiskAssessment{Level: "low", Reasoning: "Simple addition"},
	}

	err := store.Put("alice", "widget-factory", 42, "abc123", result)
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
	if got.HeadSHA != "abc123" {
		t.Errorf("HeadSHA = %q, want %q", got.HeadSHA, "abc123")
	}
	if got.Result.Summary != result.Summary {
		t.Errorf("Summary = %q, want %q", got.Result.Summary, result.Summary)
	}
	if got.AnalyzedAt.IsZero() {
		t.Error("AnalyzedAt should not be zero")
	}
}

func TestAnalysisStore_GetNotFound(t *testing.T) {
	store := NewAnalysisStore(t.TempDir())

	got, err := store.Get("bob", "test-project", 99)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for non-existent cache, got %+v", got)
	}
}

func TestAnalysisStore_IsStale(t *testing.T) {
	store := NewAnalysisStore(t.TempDir())

	cached := &CachedAnalysis{
		HeadSHA:    "abc123",
		AnalyzedAt: time.Now(),
		Result:     &AnalysisResult{Summary: "test"},
	}

	t.Run("nil is stale", func(t *testing.T) {
		if !store.IsStale(nil, "abc123") {
			t.Error("nil should be stale")
		}
	})

	t.Run("matching SHA is not stale", func(t *testing.T) {
		if store.IsStale(cached, "abc123") {
			t.Error("matching SHA should not be stale")
		}
	})

	t.Run("different SHA is stale", func(t *testing.T) {
		if !store.IsStale(cached, "def456") {
			t.Error("different SHA should be stale")
		}
	})
}

func TestAnalysisStore_Overwrite(t *testing.T) {
	store := NewAnalysisStore(t.TempDir())

	r1 := &AnalysisResult{Summary: "first"}
	r2 := &AnalysisResult{Summary: "second"}

	if err := store.Put("alice", "widget-factory", 1, "sha1", r1); err != nil {
		t.Fatal(err)
	}
	if err := store.Put("alice", "widget-factory", 1, "sha2", r2); err != nil {
		t.Fatal(err)
	}

	got, err := store.Get("alice", "widget-factory", 1)
	if err != nil {
		t.Fatal(err)
	}
	if got.Result.Summary != "second" {
		t.Errorf("Summary = %q, want %q", got.Result.Summary, "second")
	}
	if got.HeadSHA != "sha2" {
		t.Errorf("HeadSHA = %q, want %q", got.HeadSHA, "sha2")
	}
}
