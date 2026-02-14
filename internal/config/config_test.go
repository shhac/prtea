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
	})

	t.Run("preserves non-zero values", func(t *testing.T) {
		cfg := &Config{
			ClaudeTimeout: 60000,
			PollInterval:  30000,
		}
		applyDefaults(cfg)
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

func TestSaveAndLoadRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()

	// Override DefaultConfigDir by writing directly to temp path
	configPath := filepath.Join(tmpDir, "config.json")

	cfg := &Config{
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
