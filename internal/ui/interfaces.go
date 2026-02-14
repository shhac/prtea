package ui

import (
	"context"

	"github.com/shhac/prtea/internal/github"
)

// GitHubService defines the GitHub operations used by the UI layer.
// *github.Client satisfies this interface.
type GitHubService interface {
	GetUsername() string
	GetPRsForReview(ctx context.Context) ([]github.PRItem, error)
	GetMyPRs(ctx context.Context) ([]github.PRItem, error)
	GetPRDetail(ctx context.Context, owner, repo string, number int) (*github.PRDetail, error)
	GetPRFiles(ctx context.Context, owner, repo string, number int) ([]github.PRFile, error)
	GetComments(ctx context.Context, owner, repo string, number int) ([]github.Comment, error)
	GetInlineComments(ctx context.Context, owner, repo string, number int) ([]github.InlineComment, error)
	GetCIStatus(ctx context.Context, owner, repo string, ref string, number int) (*github.CIStatus, error)
	GetReviews(ctx context.Context, owner, repo string, number int) (*github.ReviewSummary, error)
	ApprovePR(ctx context.Context, owner, repo string, number int, body string) error
	PostComment(ctx context.Context, owner, repo string, number int, body string) error
	ClosePR(ctx context.Context, owner, repo string, number int) error
	RequestChangesPR(ctx context.Context, owner, repo string, number int, body string) error
	CommentReviewPR(ctx context.Context, owner, repo string, number int, body string) error
	SubmitReviewWithComments(ctx context.Context, owner, repo string, number int, event string, body string, comments []github.ReviewCommentPayload) error
}
