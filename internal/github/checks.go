package github

import (
	"context"
	"fmt"

	gh "github.com/google/go-github/v68/github"
)

// GetCIStatus fetches check runs for a given ref and computes the overall CI status.
func (c *Client) GetCIStatus(ctx context.Context, owner, repo string, ref string) (*CIStatus, error) {
	var allChecks []CICheck
	opts := &gh.ListCheckRunsOptions{
		ListOptions: gh.ListOptions{PerPage: 100},
	}

	for {
		result, resp, err := c.gh.Checks.ListCheckRunsForRef(ctx, owner, repo, ref, opts)
		if err != nil {
			return nil, fmt.Errorf("failed to list check runs for %s: %w", ref, err)
		}

		for _, cr := range result.CheckRuns {
			conclusion := ""
			if cr.Conclusion != nil {
				conclusion = cr.GetConclusion()
			}
			allChecks = append(allChecks, CICheck{
				ID:         cr.GetID(),
				Name:       cr.GetName(),
				Status:     cr.GetStatus(),
				Conclusion: conclusion,
				HTMLURL:    cr.GetHTMLURL(),
			})
		}

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	overall := computeOverallStatus(allChecks)

	return &CIStatus{
		TotalCount:    len(allChecks),
		Checks:        allChecks,
		OverallStatus: overall,
	}, nil
}

// computeOverallStatus determines the aggregate CI status from individual checks.
func computeOverallStatus(checks []CICheck) string {
	if len(checks) == 0 {
		return "pending"
	}

	hasFailure := false
	hasPending := false
	hasSuccess := false

	for _, check := range checks {
		switch {
		case check.Status == "queued" || check.Status == "in_progress":
			hasPending = true
		case check.Status == "completed" && check.Conclusion == "failure":
			hasFailure = true
		case check.Status == "completed" && (check.Conclusion == "success" || check.Conclusion == "skipped" || check.Conclusion == "neutral"):
			hasSuccess = true
		}
	}

	switch {
	case hasPending:
		return "pending"
	case hasFailure && hasSuccess:
		return "mixed"
	case hasFailure:
		return "failing"
	default:
		return "passing"
	}
}
