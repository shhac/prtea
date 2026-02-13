package github

import (
	"context"
	"fmt"

	gh "github.com/google/go-github/v68/github"
)

// GetReviews fetches all reviews for a PR, deduplicates to the latest per reviewer,
// and categorizes them into approved, changesRequested, and commented.
func (c *Client) GetReviews(ctx context.Context, owner, repo string, number int) (*ReviewSummary, error) {
	var allReviews []*gh.PullRequestReview
	opts := &gh.ListOptions{PerPage: 100}

	for {
		reviews, resp, err := c.gh.PullRequests.ListReviews(ctx, owner, repo, number, opts)
		if err != nil {
			return nil, fmt.Errorf("failed to list reviews for PR #%d: %w", number, err)
		}
		allReviews = append(allReviews, reviews...)
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	// Deduplicate: keep only the latest non-COMMENTED review per user.
	latestByUser := make(map[string]Review)
	for _, r := range allReviews {
		state := r.GetState()
		if state == "COMMENTED" {
			continue
		}

		login := "unknown"
		if r.GetUser() != nil {
			login = r.GetUser().GetLogin()
		}

		latestByUser[login] = Review{
			ID:          r.GetID(),
			Author:      userFromGH(r.GetUser()),
			State:       state,
			Body:        r.GetBody(),
			SubmittedAt: r.GetSubmittedAt().Time,
		}
	}

	summary := &ReviewSummary{}
	for _, review := range latestByUser {
		switch review.State {
		case "APPROVED":
			summary.Approved = append(summary.Approved, review)
		case "CHANGES_REQUESTED":
			summary.ChangesRequested = append(summary.ChangesRequested, review)
		}
	}

	return summary, nil
}
