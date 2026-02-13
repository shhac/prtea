package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// Client wraps the gh CLI and caches the authenticated username.
type Client struct {
	username string
}

// NewClient verifies the gh CLI is installed and authenticated, then caches the current user.
func NewClient() (*Client, error) {
	if _, err := exec.LookPath("gh"); err != nil {
		return nil, fmt.Errorf("gh CLI not found: install from https://cli.github.com")
	}

	// Verify authentication
	if _, err := ghExec(context.Background(), "auth", "status"); err != nil {
		return nil, fmt.Errorf("gh not authenticated: run 'gh auth login' first")
	}

	// Get authenticated username
	out, err := ghExec(context.Background(), "api", "user", "--jq", ".login")
	if err != nil {
		return nil, fmt.Errorf("failed to get authenticated user: %w", err)
	}

	return &Client{
		username: strings.TrimSpace(out),
	}, nil
}

// GetUsername returns the login of the authenticated user.
func (c *Client) GetUsername() string {
	return c.username
}

// ghExec runs a gh CLI command and returns its stdout.
func ghExec(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "gh", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("gh %s failed: %s", strings.Join(args, " "), strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

// ghJSON runs a gh CLI command and unmarshals the JSON output into dest.
func ghJSON(ctx context.Context, dest interface{}, args ...string) error {
	out, err := ghExec(ctx, args...)
	if err != nil {
		return err
	}
	if err := json.Unmarshal([]byte(out), dest); err != nil {
		return fmt.Errorf("failed to parse gh output: %w", err)
	}
	return nil
}
