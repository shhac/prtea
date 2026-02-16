package claude

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// CommandExecutor abstracts Claude CLI subprocess execution.
// The default implementation runs the real CLI binary;
// tests or demo modes can inject alternatives.
type CommandExecutor interface {
	Start(ctx context.Context, args []string, opts ExecOptions) (*Process, error)
}

// ExecOptions controls working directory and environment for the subprocess.
type ExecOptions struct {
	Dir string
	Env []string
}

// Process represents a running Claude CLI subprocess.
type Process struct {
	Stdout io.ReadCloser
	Stderr io.ReadCloser
	Wait   func() error
}

// CLIExecutor runs the real Claude CLI binary.
type CLIExecutor struct {
	Path string // path to the claude binary
}

// NewCLIExecutor creates an executor for the given Claude CLI binary path.
func NewCLIExecutor(claudePath string) *CLIExecutor {
	return &CLIExecutor{Path: claudePath}
}

// Start launches the Claude CLI with the given arguments and options.
func (e *CLIExecutor) Start(ctx context.Context, args []string, opts ExecOptions) (*Process, error) {
	cmd := exec.CommandContext(ctx, e.Path, args...)
	cmd.Dir = opts.Dir
	cmd.Env = opts.Env
	cmd.Stdin = nil

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
			return nil, fmt.Errorf("claude CLI not found at %s: ensure 'claude' is installed", e.Path)
		}
		return nil, fmt.Errorf("failed to start claude: %w", err)
	}

	return &Process{
		Stdout: stdout,
		Stderr: stderr,
		Wait:   cmd.Wait,
	}, nil
}

// EventVisitor is called for each parsed StreamEvent from the Claude CLI stdout.
type EventVisitor func(event *StreamEvent)

// runCLI starts the Claude CLI, parses stream-json events, and returns the
// final "result" event. The visitor is called for every event (including the
// result), allowing callers to handle progress, streaming deltas, etc.
func runCLI(ctx context.Context, executor CommandExecutor, args []string, opts ExecOptions, visitor EventVisitor) (*StreamEvent, error) {
	proc, err := executor.Start(ctx, args, opts)
	if err != nil {
		return nil, err
	}

	// Drain stderr in background
	var stderrBuf strings.Builder
	go func() {
		scanner := bufio.NewScanner(proc.Stderr)
		scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
		for scanner.Scan() {
			stderrBuf.WriteString(scanner.Text())
			stderrBuf.WriteByte('\n')
		}
	}()

	// Parse stream-json events from stdout
	var resultEvent *StreamEvent
	scanner := bufio.NewScanner(proc.Stdout)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}

		var event StreamEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}

		if visitor != nil {
			visitor(&event)
		}

		if event.Type == "result" {
			resultEvent = &event
		}
	}

	if err := proc.Wait(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("claude timed out")
		}
		errMsg := stderrBuf.String()
		if len(errMsg) > 500 {
			errMsg = errMsg[:500]
		}
		return nil, fmt.Errorf("claude exited with error: %w\nstderr: %s", err, errMsg)
	}

	if resultEvent == nil {
		return nil, fmt.Errorf("claude produced no result event")
	}

	return resultEvent, nil
}

// progressVisitor creates an EventVisitor that reports progress via onProgress.
func progressVisitor(onProgress ProgressFunc) EventVisitor {
	if onProgress == nil {
		return nil
	}
	return func(event *StreamEvent) {
		reportProgress(event, onProgress)
	}
}

// streamDeltaVisitor creates an EventVisitor that calls onChunk for text deltas
// from stream_event envelopes (used with --include-partial-messages).
func streamDeltaVisitor(onChunk func(string)) EventVisitor {
	return func(event *StreamEvent) {
		if event.Type == "stream_event" && event.Event != nil {
			if event.Event.Type == "content_block_delta" && event.Event.Delta != nil {
				if event.Event.Delta.Type == "text_delta" && event.Event.Delta.Text != "" {
					if onChunk != nil {
						onChunk(event.Event.Delta.Text)
					}
				}
			}
		}
	}
}

// reportProgress extracts progress information from stream events.
func reportProgress(event *StreamEvent, onProgress ProgressFunc) {
	switch event.Type {
	case "assistant":
		if event.Message == nil {
			return
		}
		for _, block := range event.Message.Content {
			switch block.Type {
			case "tool_use":
				onProgress(ProgressEvent{
					Type:    "tool_use",
					Message: fmt.Sprintf("Using %s...", block.Name),
				})
			case "text":
				if block.Text != "" {
					onProgress(ProgressEvent{
						Type:    "text",
						Message: truncate(block.Text, 100),
					})
				}
			}
		}
	}
}
