package github

import (
	"context"
	"fmt"

	gh "github.com/google/go-github/v68/github"
)

// ApprovePR submits an approval review on a PR.
func (c *Client) ApprovePR(ctx context.Context, owner, repo string, number int, body string) error {
	review := &gh.PullRequestReviewRequest{
		Event: gh.Ptr("APPROVE"),
	}
	if body != "" {
		review.Body = &body
	}

	_, _, err := c.gh.PullRequests.CreateReview(ctx, owner, repo, number, review)
	if err != nil {
		return fmt.Errorf("failed to approve PR #%d: %w", number, err)
	}
	return nil
}

// ClosePR closes a PR without merging.
func (c *Client) ClosePR(ctx context.Context, owner, repo string, number int) error {
	state := "closed"
	_, _, err := c.gh.PullRequests.Edit(ctx, owner, repo, number, &gh.PullRequest{
		State: &state,
	})
	if err != nil {
		return fmt.Errorf("failed to close PR #%d: %w", number, err)
	}
	return nil
}
