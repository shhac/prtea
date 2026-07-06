package ai

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// CommandExecutor abstracts codex CLI subprocess execution.
// The default implementation runs the real CLI binary;
// tests and demo mode inject alternatives.
type CommandExecutor interface {
	Start(ctx context.Context, args []string, opts ExecOptions) (*Process, error)
}

// ExecOptions controls working directory, environment, and stdin for the subprocess.
type ExecOptions struct {
	Dir   string
	Env   []string
	Stdin io.Reader
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

// Start launches the codex CLI with the given arguments and options.
func (e *CLIExecutor) Start(ctx context.Context, args []string, opts ExecOptions) (*Process, error) {
	cmd := exec.CommandContext(ctx, e.Path, args...)
	cmd.Dir = opts.Dir
	cmd.Env = opts.Env
	cmd.Stdin = opts.Stdin

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		if isNotFound(err) {
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

func isNotFound(err error) bool {
	return strings.Contains(err.Error(), "executable file not found")
}
