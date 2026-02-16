package github

import "time"

// Repo identifies a GitHub repository.
type Repo struct {
	Owner    string
	Name     string
	FullName string
}

// User represents a GitHub user.
type User struct {
	Login     string
	AvatarURL string
}

// Label represents a PR label.
type Label struct {
	Name  string
	Color string
}

// PRItem is a lightweight PR representation for list views.
type PRItem struct {
	ID           int64
	Number       int
	Title        string
	HTMLURL      string
	Repo         Repo
	Author       User
	Labels       []Label
	Draft        bool
	CreatedAt    time.Time
	Additions    int
	Deletions    int
	ChangedFiles int
}

// PRDetail is the full PR representation including merge state.
type PRDetail struct {
	Number         int
	Title          string
	Body           string
	HTMLURL        string
	Author         User
	Repo           Repo
	BaseBranch     string
	HeadBranch     string
	HeadSHA        string
	Mergeable      bool
	MergeableState string
	BehindBy       int
}

// PRFile represents a single changed file in a PR.
type PRFile struct {
	Filename  string
	Status    string // "added", "removed", "modified", "renamed"
	Additions int
	Deletions int
	Patch     string
}

// CICheck represents an individual CI check run.
type CICheck struct {
	ID            int64
	Name          string
	Status        string // "queued", "in_progress", "completed"
	Conclusion    string // "success", "failure", "neutral", "cancelled", "skipped", "timed_out", "action_required"
	HTMLURL       string
	WorkflowRunID int64 // extracted from detailsUrl for GitHub Actions checks; 0 if not available
}

// CIStatus is the aggregate CI status for a commit.
type CIStatus struct {
	TotalCount    int
	Checks        []CICheck
	OverallStatus string // "passing", "failing", "pending", "mixed"
}

// Review represents an individual PR review.
type Review struct {
	Author      User
	State       string // "APPROVED", "CHANGES_REQUESTED", "COMMENTED", "DISMISSED", "PENDING"
	Body        string
	SubmittedAt time.Time
}

// ReviewRequest represents a pending review request (user or team).
type ReviewRequest struct {
	Login  string // user login or team name
	IsTeam bool
}

// ReviewSummary categorizes reviews by state, deduplicated per user.
type ReviewSummary struct {
	Approved          []Review
	ChangesRequested  []Review
	Commented         []Review
	ReviewDecision    string // "APPROVED", "CHANGES_REQUESTED", "REVIEW_REQUIRED", ""
	PendingReviewers  []ReviewRequest
}

// Comment represents an issue-level PR comment.
type Comment struct {
	Author    User
	Body      string
	CreatedAt time.Time
}

// InlineComment represents a review comment on a specific file/line.
type InlineComment struct {
	ID          int64
	Author      User
	Body        string
	CreatedAt   time.Time
	Path        string
	Line        int
	StartLine   int    // non-zero for multi-line range comments
	Side        string // "LEFT", "RIGHT"
	InReplyToID int64
	Outdated    bool
}
