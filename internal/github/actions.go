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

	if _, err := c.ghExec(ctx, args...); err != nil {
		return fmt.Errorf("failed to approve PR #%d: %w", number, err)
	}
	return nil
}

// PostComment posts an issue-level comment on a PR.
func (c *Client) PostComment(ctx context.Context, owner, repo string, number int, body string) error {
	repoFlag := owner + "/" + repo
	if _, err := c.ghExec(ctx, "pr", "comment", fmt.Sprintf("%d", number), "-R", repoFlag, "--body", body); err != nil {
		return fmt.Errorf("failed to post comment on PR #%d: %w", number, err)
	}
	return nil
}

// ClosePR closes a PR without merging.
func (c *Client) ClosePR(ctx context.Context, owner, repo string, number int) error {
	repoFlag := owner + "/" + repo
	if _, err := c.ghExec(ctx, "pr", "close", fmt.Sprintf("%d", number), "-R", repoFlag); err != nil {
		return fmt.Errorf("failed to close PR #%d: %w", number, err)
	}
	return nil
}

// RequestChangesPR submits a "request changes" review on a PR.
// The body is required by the GitHub API for this review type.
func (c *Client) RequestChangesPR(ctx context.Context, owner, repo string, number int, body string) error {
	repoFlag := owner + "/" + repo
	args := []string{"pr", "review", fmt.Sprintf("%d", number), "-R", repoFlag, "--request-changes", "-b", body}
	if _, err := c.ghExec(ctx, args...); err != nil {
		return fmt.Errorf("failed to request changes on PR #%d: %w", number, err)
	}
	return nil
}

// CommentReviewPR submits a review-level comment on a PR (not an issue comment).
func (c *Client) CommentReviewPR(ctx context.Context, owner, repo string, number int, body string) error {
	repoFlag := owner + "/" + repo
	args := []string{"pr", "review", fmt.Sprintf("%d", number), "-R", repoFlag, "--comment", "-b", body}
	if _, err := c.ghExec(ctx, args...); err != nil {
		return fmt.Errorf("failed to submit review comment on PR #%d: %w", number, err)
	}
	return nil
}
