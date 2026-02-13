package github

import (
	"context"
	"fmt"
)

// ApprovePR submits an approval review on a PR.
func (c *Client) ApprovePR(ctx context.Context, owner, repo string, number int, body string) error {
	repoFlag := owner + "/" + repo
	args := []string{"pr", "review", fmt.Sprintf("%d", number), "-R", repoFlag, "--approve"}
	if body != "" {
		args = append(args, "-b", body)
	}

	if _, err := ghExec(ctx, args...); err != nil {
		return fmt.Errorf("failed to approve PR #%d: %w", number, err)
	}
	return nil
}

// ClosePR closes a PR without merging.
func (c *Client) ClosePR(ctx context.Context, owner, repo string, number int) error {
	repoFlag := owner + "/" + repo
	if _, err := ghExec(ctx, "pr", "close", fmt.Sprintf("%d", number), "-R", repoFlag); err != nil {
		return fmt.Errorf("failed to close PR #%d: %w", number, err)
	}
	return nil
}
