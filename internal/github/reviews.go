package github

import (
	"context"
	"fmt"
	"time"
)

// ghReview is the JSON shape for reviews from gh pr view.
type ghReview struct {
	ID     string `json:"id"`
	Author struct {
		Login string `json:"login"`
	} `json:"author"`
	State       string    `json:"state"`
	Body        string    `json:"body"`
	SubmittedAt time.Time `json:"submittedAt"`
}

// ghLatestReview is used for the latestReviews field.
type ghLatestReview = ghReview

// ghReviewRequest is the JSON shape for reviewRequests from gh pr view.
type ghReviewRequest struct {
	TypeName string `json:"__typename"` // "User" or "Team"
	Login    string `json:"login"`      // for User
	Name     string `json:"name"`       // for Team
}

// ghPRReviews is the JSON shape from gh pr view --json reviews,latestReviews,reviewDecision,reviewRequests.
type ghPRReviews struct {
	Reviews        []ghReview        `json:"reviews"`
	LatestReviews  []ghLatestReview  `json:"latestReviews"`
	ReviewDecision string            `json:"reviewDecision"`
	ReviewRequests []ghReviewRequest `json:"reviewRequests"`
}

// GetReviews fetches all reviews for a PR, deduplicates to the latest per reviewer,
// and categorizes them into approved, changesRequested, and commented.
func (c *Client) GetReviews(ctx context.Context, owner, repo string, number int) (*ReviewSummary, error) {
	repoFlag := owner + "/" + repo

	var data ghPRReviews
	err := ghJSON(ctx, &data,
		"pr", "view", fmt.Sprintf("%d", number),
		"-R", repoFlag,
		"--json", "reviews,latestReviews,reviewDecision,reviewRequests",
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list reviews for PR #%d: %w", number, err)
	}

	// Use latestReviews for deduplication (gh gives us latest per user).
	// Fall back to manual deduplication from reviews if latestReviews is empty.
	var deduplicated []ghReview
	if len(data.LatestReviews) > 0 {
		deduplicated = data.LatestReviews
	} else {
		deduplicated = deduplicateReviews(data.Reviews)
	}

	summary := &ReviewSummary{
		ReviewDecision: data.ReviewDecision,
	}

	for _, r := range deduplicated {
		if r.State == "COMMENTED" {
			continue
		}
		review := Review{
			Author:      User{Login: r.Author.Login},
			State:       r.State,
			Body:        r.Body,
			SubmittedAt: r.SubmittedAt,
		}
		switch r.State {
		case "APPROVED":
			summary.Approved = append(summary.Approved, review)
		case "CHANGES_REQUESTED":
			summary.ChangesRequested = append(summary.ChangesRequested, review)
		}
	}

	for _, rr := range data.ReviewRequests {
		req := ReviewRequest{IsTeam: rr.TypeName == "Team"}
		if req.IsTeam {
			req.Login = rr.Name
		} else {
			req.Login = rr.Login
		}
		summary.PendingReviewers = append(summary.PendingReviewers, req)
	}

	return summary, nil
}

// deduplicateReviews keeps only the latest non-COMMENTED review per user.
func deduplicateReviews(reviews []ghReview) []ghReview {
	latest := make(map[string]ghReview)
	for _, r := range reviews {
		if r.State == "COMMENTED" {
			continue
		}
		latest[r.Author.Login] = r
	}
	result := make([]ghReview, 0, len(latest))
	for _, r := range latest {
		result = append(result, r)
	}
	return result
}
