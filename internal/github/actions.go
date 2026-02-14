package github

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
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

// ReviewCommentPayload is a single inline comment in a review submission.
type ReviewCommentPayload struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Side string `json:"side,omitempty"`
	Body string `json:"body"`
}

// SubmitReviewWithComments submits a review with inline comments via the GitHub REST API.
// This uses `gh api` directly since `gh pr review` doesn't support inline comments.
func (c *Client) SubmitReviewWithComments(ctx context.Context, owner, repo string, number int, event string, body string, comments []ReviewCommentPayload) error {
	// Map event names to GitHub API values
	apiEvent := strings.ToUpper(event)
	switch apiEvent {
	case "APPROVE", "COMMENT", "REQUEST_CHANGES":
		// valid
	default:
		return fmt.Errorf("invalid review event: %s", event)
	}

	// Set default side for comments
	for i := range comments {
		if comments[i].Side == "" {
			comments[i].Side = "RIGHT"
		}
	}

	// Build JSON payload
	payload := struct {
		Event    string                 `json:"event"`
		Body     string                 `json:"body"`
		Comments []ReviewCommentPayload `json:"comments"`
	}{
		Event:    apiEvent,
		Body:     body,
		Comments: comments,
	}

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal review payload: %w", err)
	}

	endpoint := fmt.Sprintf("repos/%s/%s/pulls/%d/reviews", owner, repo, number)
	args := []string{"api", endpoint, "--method", "POST", "--input", "-"}

	// Use ghExec with stdin piped via a temporary approach:
	// gh api supports --input - for reading from stdin, but our ghExec doesn't support stdin.
	// Instead, use -f/--raw-field for each field. But for complex nested JSON, use --input.
	// Workaround: pass the payload as a raw field.
	args = []string{"api", endpoint, "--method", "POST",
		"-H", "Accept: application/vnd.github+json",
		"--input", "-",
	}

	// We need to pipe stdin, so use a custom approach
	if _, err := c.ghExecWithStdin(ctx, string(payloadJSON), args...); err != nil {
		return fmt.Errorf("failed to submit review with comments on PR #%d: %w", number, err)
	}
	return nil
}
