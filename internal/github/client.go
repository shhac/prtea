package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// CommandRunner executes a CLI command and returns its stdout.
// The default implementation runs the gh CLI via exec.Command.
// Tests can inject a mock implementation.
type CommandRunner func(ctx context.Context, args ...string) (string, error)

// Client wraps the gh CLI and caches the authenticated username.
type Client struct {
	username string
	run      CommandRunner
}

// NewClient verifies the gh CLI is installed and authenticated, then caches the current user.
func NewClient() (*Client, error) {
	if _, err := exec.LookPath("gh"); err != nil {
		return nil, fmt.Errorf("gh CLI not found: install from https://cli.github.com")
	}

	c := &Client{run: defaultRunner}

	// Verify authentication
	if _, err := c.ghExec(context.Background(), "auth", "status"); err != nil {
		return nil, fmt.Errorf("gh not authenticated: run 'gh auth login' first")
	}

	// Get authenticated username
	out, err := c.ghExec(context.Background(), "api", "user", "--jq", ".login")
	if err != nil {
		return nil, fmt.Errorf("failed to get authenticated user: %w", err)
	}

	c.username = strings.TrimSpace(out)
	return c, nil
}

// NewTestClient creates a Client with a custom CommandRunner for testing.
func NewTestClient(username string, runner CommandRunner) *Client {
	return &Client{username: username, run: runner}
}

// GetUsername returns the login of the authenticated user.
func (c *Client) GetUsername() string {
	return c.username
}

// defaultRunner executes the gh CLI via exec.Command.
func defaultRunner(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "gh", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("gh %s failed: %s", strings.Join(args, " "), strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

// ghExec runs a gh CLI command via the client's CommandRunner.
func (c *Client) ghExec(ctx context.Context, args ...string) (string, error) {
	return c.run(ctx, args...)
}

// ghExecWithStdin runs a gh CLI command with the given string piped to stdin.
func (c *Client) ghExecWithStdin(ctx context.Context, stdin string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "gh", args...)
	cmd.Stdin = strings.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("gh %s failed: %s", strings.Join(args, " "), strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

// ghJSON runs a gh CLI command and unmarshals the JSON output into dest.
func (c *Client) ghJSON(ctx context.Context, dest interface{}, args ...string) error {
	out, err := c.ghExec(ctx, args...)
	if err != nil {
		return err
	}
	if err := json.Unmarshal([]byte(out), dest); err != nil {
		return fmt.Errorf("failed to parse gh output: %w", err)
	}
	return nil
}
