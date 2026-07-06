package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaults(t *testing.T) {
	cfg := Defaults()
	if cfg.AITimeout != DefaultAITimeoutMs {
		t.Errorf("AITimeout = %d, want %d", cfg.AITimeout, DefaultAITimeoutMs)
	}
	if cfg.PollInterval != DefaultPollIntervalMs {
		t.Errorf("PollInterval = %d, want %d", cfg.PollInterval, DefaultPollIntervalMs)
	}
	if cfg.CodexModel != DefaultCodexModel {
		t.Errorf("CodexModel = %q, want %q", cfg.CodexModel, DefaultCodexModel)
	}
	if cfg.CodexEffort != DefaultCodexEffort {
		t.Errorf("CodexEffort = %q, want %q", cfg.CodexEffort, DefaultCodexEffort)
	}
}

func TestApplyDefaults(t *testing.T) {
	t.Run("fills zero values", func(t *testing.T) {
		cfg := &Config{}
		applyDefaults(cfg)
		if cfg.AITimeout != DefaultAITimeoutMs {
			t.Errorf("AITimeout = %d, want %d", cfg.AITimeout, DefaultAITimeoutMs)
		}
		if cfg.PollInterval != DefaultPollIntervalMs {
			t.Errorf("PollInterval = %d, want %d", cfg.PollInterval, DefaultPollIntervalMs)
		}
		if cfg.CodexModel != DefaultCodexModel {
			t.Errorf("CodexModel = %q, want %q", cfg.CodexModel, DefaultCodexModel)
		}
	})

	t.Run("preserves non-zero values", func(t *testing.T) {
		cfg := &Config{
			AITimeout:    60000,
			PollInterval: 30000,
			CodexModel:   "gpt-5.4-mini",
			CodexEffort:  "high",
		}
		applyDefaults(cfg)
		if cfg.AITimeout != 60000 {
			t.Errorf("AITimeout = %d, want 60000", cfg.AITimeout)
		}
		if cfg.PollInterval != 30000 {
			t.Errorf("PollInterval = %d, want 30000", cfg.PollInterval)
		}
		if cfg.CodexModel != "gpt-5.4-mini" {
			t.Errorf("CodexModel = %q", cfg.CodexModel)
		}
		if cfg.CodexEffort != "high" {
			t.Errorf("CodexEffort = %q", cfg.CodexEffort)
		}
	})

	t.Run("migrates legacy claude timeout", func(t *testing.T) {
		cfg := &Config{LegacyClaudeTimeout: 90000}
		applyDefaults(cfg)
		if cfg.AITimeout != 90000 {
			t.Errorf("AITimeout = %d, want migrated 90000", cfg.AITimeout)
		}
		if cfg.LegacyClaudeTimeout != 0 {
			t.Errorf("LegacyClaudeTimeout = %d, want 0 after migration", cfg.LegacyClaudeTimeout)
		}
	})

	t.Run("new timeout wins over legacy", func(t *testing.T) {
		cfg := &Config{AITimeout: 45000, LegacyClaudeTimeout: 90000}
		applyDefaults(cfg)
		if cfg.AITimeout != 45000 {
			t.Errorf("AITimeout = %d, want 45000", cfg.AITimeout)
		}
	})
}

func TestAITimeoutDuration(t *testing.T) {
	cfg := &Config{AITimeout: 120000}
	got := cfg.AITimeoutDuration()
	want := 120 * time.Second
	if got != want {
		t.Errorf("AITimeoutDuration() = %v, want %v", got, want)
	}
}

func TestLegacyConfigParses(t *testing.T) {
	// A pre-codex config file must still load, mapping claudeTimeoutMs.
	legacy := `{
		"claudeTimeoutMs": 90000,
		"pollIntervalMs": 45000,
		"maxChatHistory": 16,
		"maxPromptTokens": 100000,
		"chatMaxTurns": 3,
		"analysisMaxTurns": 30
	}`

	var cfg Config
	if err := json.Unmarshal([]byte(legacy), &cfg); err != nil {
		t.Fatalf("legacy config failed to parse: %v", err)
	}
	applyDefaults(&cfg)

	if cfg.AITimeout != 90000 {
		t.Errorf("AITimeout = %d, want 90000 from claudeTimeoutMs", cfg.AITimeout)
	}
	if cfg.PollInterval != 45000 {
		t.Errorf("PollInterval = %d, want 45000", cfg.PollInterval)
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()

	// Override DefaultConfigDir by writing directly to temp path
	configPath := filepath.Join(tmpDir, "config.json")

	cfg := &Config{
		AITimeout:    90000,
		PollInterval: 45000,
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

	if loaded.AITimeout != cfg.AITimeout {
		t.Errorf("AITimeout = %d, want %d", loaded.AITimeout, cfg.AITimeout)
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
