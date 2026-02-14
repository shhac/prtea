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
	ClaudeTimeout int `json:"claudeTimeoutMs"`
	PollInterval  int `json:"pollIntervalMs"`
}

// Defaults
const (
	DefaultClaudeTimeoutMs = 120000
	DefaultPollIntervalMs  = 60000
)

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
	configPath := filepath.Join(DefaultConfigDir(), "config.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return defaults(), nil
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

	configPath := filepath.Join(dir, "config.json")
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

// AnalysesCacheDir returns the path to the analysis cache directory.
func AnalysesCacheDir() string {
	return filepath.Join(DefaultConfigDir(), "analyses")
}

// ChatCacheDir returns the path to the chat session cache directory.
func ChatCacheDir() string {
	return filepath.Join(DefaultConfigDir(), "chats")
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

// ClaudeTimeoutDuration returns the configured claude timeout as a time.Duration.
func (c *Config) ClaudeTimeoutDuration() time.Duration {
	return time.Duration(c.ClaudeTimeout) * time.Millisecond
}

func defaults() *Config {
	return &Config{
		ClaudeTimeout: DefaultClaudeTimeoutMs,
		PollInterval:  DefaultPollIntervalMs,
	}
}

func applyDefaults(cfg *Config) {
	if cfg.ClaudeTimeout == 0 {
		cfg.ClaudeTimeout = DefaultClaudeTimeoutMs
	}
	if cfg.PollInterval == 0 {
		cfg.PollInterval = DefaultPollIntervalMs
	}
}
