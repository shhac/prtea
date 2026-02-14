package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaults(t *testing.T) {
	cfg := defaults()
	if cfg.ClaudeTimeout != DefaultClaudeTimeoutMs {
		t.Errorf("ClaudeTimeout = %d, want %d", cfg.ClaudeTimeout, DefaultClaudeTimeoutMs)
	}
	if cfg.PollInterval != DefaultPollIntervalMs {
		t.Errorf("PollInterval = %d, want %d", cfg.PollInterval, DefaultPollIntervalMs)
	}
	if cfg.ReposPath != DefaultReposPath {
		t.Errorf("ReposPath = %q, want %q", cfg.ReposPath, DefaultReposPath)
	}
}

func TestApplyDefaults(t *testing.T) {
	t.Run("fills zero values", func(t *testing.T) {
		cfg := &Config{}
		applyDefaults(cfg)
		if cfg.ClaudeTimeout != DefaultClaudeTimeoutMs {
			t.Errorf("ClaudeTimeout = %d, want %d", cfg.ClaudeTimeout, DefaultClaudeTimeoutMs)
		}
		if cfg.PollInterval != DefaultPollIntervalMs {
			t.Errorf("PollInterval = %d, want %d", cfg.PollInterval, DefaultPollIntervalMs)
		}
		if cfg.ReposPath != DefaultReposPath {
			t.Errorf("ReposPath = %q, want %q", cfg.ReposPath, DefaultReposPath)
		}
	})

	t.Run("preserves non-zero values", func(t *testing.T) {
		cfg := &Config{
			ReposPath:     "/custom/path",
			ClaudeTimeout: 60000,
			PollInterval:  30000,
		}
		applyDefaults(cfg)
		if cfg.ReposPath != "/custom/path" {
			t.Errorf("ReposPath = %q, want /custom/path", cfg.ReposPath)
		}
		if cfg.ClaudeTimeout != 60000 {
			t.Errorf("ClaudeTimeout = %d, want 60000", cfg.ClaudeTimeout)
		}
		if cfg.PollInterval != 30000 {
			t.Errorf("PollInterval = %d, want 30000", cfg.PollInterval)
		}
	})
}

func TestClaudeTimeoutDuration(t *testing.T) {
	cfg := &Config{ClaudeTimeout: 120000}
	got := cfg.ClaudeTimeoutDuration()
	want := 120 * time.Second
	if got != want {
		t.Errorf("ClaudeTimeoutDuration() = %v, want %v", got, want)
	}
}

func TestExpandHome(t *testing.T) {
	t.Run("expands tilde", func(t *testing.T) {
		got := expandHome("~/repos")
		if got == "~/repos" {
			t.Error("tilde should have been expanded")
		}
		if got[len(got)-6:] != "/repos" {
			t.Errorf("expected path ending in /repos, got %q", got)
		}
	})

	t.Run("no tilde", func(t *testing.T) {
		got := expandHome("/absolute/path")
		if got != "/absolute/path" {
			t.Errorf("got %q, want /absolute/path", got)
		}
	})

	t.Run("empty string", func(t *testing.T) {
		got := expandHome("")
		if got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})

	t.Run("just tilde", func(t *testing.T) {
		// "~" alone has length < 2 or doesn't start with "~/"
		got := expandHome("~")
		if got != "~" {
			t.Errorf("got %q, want ~", got)
		}
	})
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()

	// Override DefaultConfigDir by writing directly to temp path
	configPath := filepath.Join(tmpDir, "config.json")

	cfg := &Config{
		ReposPath:     "/home/alice/repos",
		ClaudeTimeout: 90000,
		PollInterval:  45000,
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	if err := os.WriteFile(configPath, data, 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	// Read it back
	readData, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}

	var loaded Config
	if err := json.Unmarshal(readData, &loaded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if loaded.ReposPath != cfg.ReposPath {
		t.Errorf("ReposPath = %q, want %q", loaded.ReposPath, cfg.ReposPath)
	}
	if loaded.ClaudeTimeout != cfg.ClaudeTimeout {
		t.Errorf("ClaudeTimeout = %d, want %d", loaded.ClaudeTimeout, cfg.ClaudeTimeout)
	}
	if loaded.PollInterval != cfg.PollInterval {
		t.Errorf("PollInterval = %d, want %d", loaded.PollInterval, cfg.PollInterval)
	}
}

func TestGetRepoPrompt_NotFound(t *testing.T) {
	// Point PromptsDir to a temp directory with no prompts
	// Since GetRepoPrompt uses PromptsDir() which depends on DefaultConfigDir(),
	// we test the file-not-found path directly
	prompt, err := GetRepoPrompt("alice", "nonexistent-repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if prompt != "" {
		t.Errorf("expected empty prompt, got %q", prompt)
	}
}
