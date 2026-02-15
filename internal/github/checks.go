package github

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// ghCheckRun is the JSON shape for statusCheckRollup items from gh pr view.
type ghCheckRun struct {
	Name       string `json:"name"`
	Status     string `json:"status"`     // "IN_PROGRESS", "COMPLETED", "QUEUED", etc.
	Conclusion string `json:"conclusion"` // "SUCCESS", "FAILURE", "NEUTRAL", etc.
	DetailsURL string `json:"detailsUrl"`
}

// ghPRChecks is the JSON shape from gh pr view --json statusCheckRollup.
type ghPRChecks struct {
	StatusCheckRollup []ghCheckRun `json:"statusCheckRollup"`
}

// GetCIStatus fetches check runs for a given ref and computes the overall CI status.
// Note: ref is unused when using gh pr view; we use the PR number directly.
// The ref parameter is kept for interface compatibility.
func (c *Client) GetCIStatus(ctx context.Context, owner, repo string, ref string, number int) (*CIStatus, error) {
	repoFlag := owner + "/" + repo

	var data ghPRChecks
	err := c.ghJSON(ctx, &data,
		"pr", "view", fmt.Sprintf("%d", number),
		"-R", repoFlag,
		"--json", "statusCheckRollup",
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list check runs for PR #%d: %w", number, err)
	}

	checks := make([]CICheck, 0, len(data.StatusCheckRollup))
	for _, cr := range data.StatusCheckRollup {
		checks = append(checks, CICheck{
			Name:          cr.Name,
			Status:        normalizeStatus(cr.Status),
			Conclusion:    normalizeConclusionStr(cr.Conclusion),
			HTMLURL:       cr.DetailsURL,
			WorkflowRunID: parseWorkflowRunID(cr.DetailsURL),
		})
	}

	overall := computeOverallStatus(checks)

	return &CIStatus{
		TotalCount:    len(checks),
		Checks:        checks,
		OverallStatus: overall,
	}, nil
}

// normalizeStatus converts gh CLI status values to our lowercase convention.
func normalizeStatus(s string) string {
	switch strings.ToUpper(s) {
	case "IN_PROGRESS":
		return "in_progress"
	case "COMPLETED":
		return "completed"
	case "QUEUED":
		return "queued"
	default:
		return strings.ToLower(s)
	}
}

// normalizeConclusionStr converts gh CLI conclusion values to our lowercase convention.
func normalizeConclusionStr(s string) string {
	return strings.ToLower(s)
}

// actionsRunIDRe matches GitHub Actions URLs like /actions/runs/12345 or /actions/runs/12345/job/67890
var actionsRunIDRe = regexp.MustCompile(`/actions/runs/(\d+)`)

// parseWorkflowRunID extracts the GitHub Actions workflow run ID from a detailsUrl.
// Returns 0 if the URL doesn't match the GitHub Actions pattern (e.g. external CI).
func parseWorkflowRunID(url string) int64 {
	m := actionsRunIDRe.FindStringSubmatch(url)
	if len(m) < 2 {
		return 0
	}
	id, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil {
		return 0
	}
	return id
}

// FailedRunIDs returns deduplicated workflow run IDs for failed checks.
// Only checks backed by GitHub Actions (WorkflowRunID > 0) are included.
func (s *CIStatus) FailedRunIDs() []int64 {
	if s == nil {
		return nil
	}
	seen := make(map[int64]bool)
	var ids []int64
	for _, c := range s.Checks {
		if c.Status == "completed" && c.Conclusion == "failure" && c.WorkflowRunID > 0 {
			if !seen[c.WorkflowRunID] {
				seen[c.WorkflowRunID] = true
				ids = append(ids, c.WorkflowRunID)
			}
		}
	}
	return ids
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
