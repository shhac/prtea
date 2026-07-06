package ai

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
)

// CommandExecutor abstracts codex CLI subprocess execution so tests can
// inject a scripted fake.
type CommandExecutor interface {
	Start(ctx context.Context, args []string, stdin io.Reader) (*Process, error)
}

// Process represents a running codex CLI subprocess.
type Process struct {
	Stdout io.ReadCloser
	Stderr io.ReadCloser
	Wait   func() error
}

// CLIExecutor runs the real codex CLI binary.
type CLIExecutor struct {
	Path string // path to the codex binary
}

// NewCLIExecutor creates an executor for the given codex CLI binary path.
func NewCLIExecutor(codexPath string) *CLIExecutor {
	return &CLIExecutor{Path: codexPath}
}

// Start launches the codex CLI with the given arguments and stdin.
func (e *CLIExecutor) Start(ctx context.Context, args []string, stdin io.Reader) (*Process, error) {
	cmd := exec.CommandContext(ctx, e.Path, args...)
	cmd.Stdin = stdin

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		if errors.Is(err, exec.ErrNotFound) || errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("codex CLI not found at %s: ensure 'codex' is installed", e.Path)
		}
		return nil, fmt.Errorf("failed to start codex: %w", err)
	}

	return &Process{
		Stdout: stdout,
		Stderr: stderr,
		Wait:   cmd.Wait,
	}, nil
}
