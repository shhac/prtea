package ui

import (
	"strings"
	"testing"
)

func TestFormatUserError(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		contains string
	}{
		{"gh cli not found", "gh CLI not found in PATH", "GitHub CLI (gh) not found"},
		{"not authenticated", "not authenticated with github", "Not authenticated"},
		{"auth login variant", "run gh auth login first", "Not authenticated"},
		{"rate limit", "rate limit exceeded", "rate limit reached"},
		{"timeout", "context deadline exceeded", "timed out"},
		{"generic timeout", "request timeout after 30s", "timed out"},
		{"no such host", "dial tcp: no such host", "Network error"},
		{"connection refused", "connection refused", "Network error"},
		{"unknown error passthrough", "something weird happened", "something weird happened"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatUserError(tt.input)
			if !strings.Contains(got, tt.contains) {
				t.Errorf("formatUserError(%q) = %q, want to contain %q", tt.input, got, tt.contains)
			}
		})
	}
}

func TestRenderEmptyState(t *testing.T) {
	t.Run("message only", func(t *testing.T) {
		got := renderEmptyState("No items found", "")
		if !strings.Contains(got, "No items found") {
			t.Errorf("expected output to contain 'No items found', got %q", got)
		}
	})

	t.Run("message with hint", func(t *testing.T) {
		got := renderEmptyState("No PRs", "Press r to refresh")
		if !strings.Contains(got, "No PRs") {
			t.Errorf("expected output to contain 'No PRs', got %q", got)
		}
		if !strings.Contains(got, "Press r to refresh") {
			t.Errorf("expected output to contain hint, got %q", got)
		}
	})
}

func TestRenderErrorWithHint(t *testing.T) {
	t.Run("error only", func(t *testing.T) {
		got := renderErrorWithHint("Something failed", "")
		if !strings.Contains(got, "Something failed") {
			t.Errorf("expected output to contain error, got %q", got)
		}
	})

	t.Run("error with hint", func(t *testing.T) {
		got := renderErrorWithHint("API error", "Press r to retry")
		if !strings.Contains(got, "API error") {
			t.Errorf("expected output to contain error, got %q", got)
		}
		if !strings.Contains(got, "Press r to retry") {
			t.Errorf("expected output to contain hint, got %q", got)
		}
	})
}
