package ai

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// FindCodex locates the codex CLI binary.
// It checks PATH first, then common install locations.
func FindCodex() (string, error) {
	if p, err := exec.LookPath("codex"); err == nil {
		return p, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}

	candidates := []string{
		filepath.Join(home, ".local", "bin", "codex"),
		"/usr/local/bin/codex",
		"/opt/homebrew/bin/codex",
	}

	for _, p := range candidates {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p, nil
		}
	}

	return "", fmt.Errorf("codex CLI not found: ensure 'codex' is installed and on your PATH")
}
