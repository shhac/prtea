package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// Config holds application configuration.
type Config struct {
	AITimeout            int      `json:"aiTimeoutMs"`
	PollInterval         int      `json:"pollIntervalMs"`
	PollEnabled          bool     `json:"pollEnabled"`
	NotificationsEnabled bool     `json:"notificationsEnabled"`
	DefaultPRTab         string   `json:"defaultPRTab"`      // "review" (default) or "mine"
	StartCollapsed       []string `json:"startCollapsed"`    // panels to collapse on boot, e.g. ["right"]
	CollapseThreshold    int      `json:"collapseThreshold"` // terminal width below which panels auto-collapse

	// Fetch & notification tuning
	PRFetchLimit          int `json:"prFetchLimit"`          // max PRs to fetch per query
	NotificationThreshold int `json:"notificationThreshold"` // above this, batch notifications into summary

	// AI engine tuning
	CodexModel  string `json:"codexModel"`           // codex model, e.g. "gpt-5.5"
	CodexEffort string `json:"codexReasoningEffort"` // "low", "medium", "high"

	DefaultReviewAction string `json:"defaultReviewAction"` // "approve", "comment", or "request_changes"

	// LegacyClaudeTimeout maps the pre-codex claudeTimeoutMs field onto
	// AITimeout on load. Dropped from disk on next save.
	LegacyClaudeTimeout int `json:"claudeTimeoutMs,omitempty"`
}

// Defaults
const (
	DefaultAITimeoutMs           = 300000
	DefaultPollIntervalMs        = 60000
	DefaultCollapseThreshold     = 120
	DefaultPRFetchLimit          = 100
	DefaultNotificationThreshold = 3
	DefaultCodexModel            = "gpt-5.5"
	DefaultCodexEffort           = "medium"
)

// FilePath returns the location of the config file.
func FilePath() string {
	return filepath.Join(DefaultConfigDir(), "config.json")
}

// DefaultConfigDir returns the platform-appropriate config directory.
func DefaultConfigDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", ".config", "prtea")
	}

	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, ".config", "prtea")
	case "windows":
		if appData := os.Getenv("APPDATA"); appData != "" {
			return filepath.Join(appData, "prtea")
		}
		return filepath.Join(home, ".config", "prtea")
	default: // linux and others
		if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
			return filepath.Join(xdg, "prtea")
		}
		return filepath.Join(home, ".config", "prtea")
	}
}

// Load reads the config file, returning defaults for missing fields.
func Load() (*Config, error) {
	data, err := os.ReadFile(FilePath())
	if err != nil {
		if os.IsNotExist(err) {
			return Defaults(), nil
		}
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	applyDefaults(&cfg)
	return &cfg, nil
}

// Save writes the config to disk.
func Save(cfg *Config) error {
	dir := DefaultConfigDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	configPath := FilePath()
	tmpPath := configPath + ".tmp"

	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	if err := os.Rename(tmpPath, configPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to rename config: %w", err)
	}

	return nil
}

// ThreadsCacheDir returns the path to the AI thread cache directory.
func ThreadsCacheDir() string {
	return filepath.Join(DefaultConfigDir(), "threads")
}

// PromptsDir returns the path to the custom prompts directory.
func PromptsDir() string {
	return filepath.Join(DefaultConfigDir(), "prompts")
}

// GetRepoPrompt loads a custom prompt file for a repository, if it exists.
func GetRepoPrompt(owner, repo string) (string, error) {
	path := filepath.Join(PromptsDir(), fmt.Sprintf("%s_%s.md", owner, repo))
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("failed to read repo prompt: %w", err)
	}
	return string(data), nil
}

// AITimeoutDuration returns the configured AI turn timeout as a time.Duration.
func (c *Config) AITimeoutDuration() time.Duration {
	return time.Duration(c.AITimeout) * time.Millisecond
}

// PollIntervalDuration returns the configured poll interval as a time.Duration.
func (c *Config) PollIntervalDuration() time.Duration {
	return time.Duration(c.PollInterval) * time.Millisecond
}

// Defaults returns a config populated with default values.
func Defaults() *Config {
	return &Config{
		AITimeout:             DefaultAITimeoutMs,
		PollInterval:          DefaultPollIntervalMs,
		CollapseThreshold:     DefaultCollapseThreshold,
		PRFetchLimit:          DefaultPRFetchLimit,
		NotificationThreshold: DefaultNotificationThreshold,
		CodexModel:            DefaultCodexModel,
		CodexEffort:           DefaultCodexEffort,
	}
}

func applyDefaults(cfg *Config) {
	if cfg.AITimeout == 0 && cfg.LegacyClaudeTimeout > 0 {
		cfg.AITimeout = cfg.LegacyClaudeTimeout
	}
	cfg.LegacyClaudeTimeout = 0
	if cfg.AITimeout == 0 {
		cfg.AITimeout = DefaultAITimeoutMs
	}
	if cfg.PollInterval == 0 {
		cfg.PollInterval = DefaultPollIntervalMs
	}
	if cfg.CollapseThreshold == 0 {
		cfg.CollapseThreshold = DefaultCollapseThreshold
	}
	if cfg.PRFetchLimit == 0 {
		cfg.PRFetchLimit = DefaultPRFetchLimit
	}
	if cfg.NotificationThreshold == 0 {
		cfg.NotificationThreshold = DefaultNotificationThreshold
	}
	if cfg.CodexModel == "" {
		cfg.CodexModel = DefaultCodexModel
	}
	if cfg.CodexEffort == "" {
		cfg.CodexEffort = DefaultCodexEffort
	}
}
