package claude

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// FindClaude locates the claude CLI binary.
// It checks PATH first, then common install locations.
func FindClaude() (string, error) {
	// Try PATH first
	if p, err := exec.LookPath("claude"); err == nil {
		return p, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}

	candidates := []string{
		filepath.Join(home, ".local", "bin", "claude"),
		"/usr/local/bin/claude",
		"/opt/homebrew/bin/claude",
	}

	for _, p := range candidates {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p, nil
		}
	}

	return "", fmt.Errorf("claude CLI not found: ensure 'claude' is installed and on your PATH")
}

// CheckVersion runs `claude --version` and returns the version string.
func CheckVersion(claudePath string) (string, error) {
	cmd := exec.Command(claudePath, "--version")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get claude version: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}
