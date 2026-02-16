// Package demo provides a mock GitHubService for demo mode.
// All read operations return realistic fake data; all write operations
// return ErrDemoMode so the UI surfaces a friendly read-only message.
package demo

import (
	"context"
	"fmt"

	"github.com/shhac/prtea/internal/github"
)

// ErrDemoMode is returned by all write operations in demo mode.
var ErrDemoMode = fmt.Errorf("Demo mode: submissions disabled")

// Service implements ui.GitHubService with in-memory fake data.
type Service struct {
	username string
	toReview []github.PRItem
	myPRs    []github.PRItem
	details  map[int]*github.PRDetail
	files    map[int][]github.PRFile
	comments map[int][]github.Comment
	inline   map[int][]github.InlineComment
	ci       map[int]*github.CIStatus
	reviews  map[int]*github.ReviewSummary
}

// NewService creates a DemoService populated with fake PR data.
func NewService() *Service {
	return &Service{
		username: demoUsername,
		toReview: prsForReview,
		myPRs:    myPRs,
		details:  prDetails,
		files:    prFiles,
		comments: issueComments,
		inline:   inlineComments,
		ci:       ciStatuses,
		reviews:  reviewSummaries,
	}
}

// -- Read operations --

func (s *Service) GetUsername() string { return s.username }

func (s *Service) GetPRsForReview(_ context.Context) ([]github.PRItem, error) {
	return s.toReview, nil
}

func (s *Service) GetMyPRs(_ context.Context) ([]github.PRItem, error) {
	return s.myPRs, nil
}

func (s *Service) GetPRDetail(_ context.Context, _, _ string, number int) (*github.PRDetail, error) {
	if d, ok := s.details[number]; ok {
		return d, nil
	}
	return nil, fmt.Errorf("demo: PR #%d not found", number)
}

func (s *Service) GetPRFiles(_ context.Context, _, _ string, number int) ([]github.PRFile, error) {
	if f, ok := s.files[number]; ok {
		return f, nil
	}
	return nil, fmt.Errorf("demo: PR #%d not found", number)
}

func (s *Service) GetComments(_ context.Context, _, _ string, number int) ([]github.Comment, error) {
	return s.comments[number], nil
}

func (s *Service) GetInlineComments(_ context.Context, _, _ string, number int) ([]github.InlineComment, error) {
	return s.inline[number], nil
}

func (s *Service) GetCIStatus(_ context.Context, _, _ string, _ string, number int) (*github.CIStatus, error) {
	if ci, ok := s.ci[number]; ok {
		return ci, nil
	}
	return &github.CIStatus{}, nil
}

func (s *Service) GetReviews(_ context.Context, _, _ string, number int) (*github.ReviewSummary, error) {
	if r, ok := s.reviews[number]; ok {
		return r, nil
	}
	return &github.ReviewSummary{}, nil
}

// -- Configuration (no-op) --

func (s *Service) SetFetchLimit(_ int) {}

// -- Write operations (all blocked) --

func (s *Service) ApprovePR(_ context.Context, _, _ string, _ int, _ string) error {
	return ErrDemoMode
}

func (s *Service) PostComment(_ context.Context, _, _ string, _ int, _ string) error {
	return ErrDemoMode
}

func (s *Service) ClosePR(_ context.Context, _, _ string, _ int) error {
	return ErrDemoMode
}

func (s *Service) RequestChangesPR(_ context.Context, _, _ string, _ int, _ string) error {
	return ErrDemoMode
}

func (s *Service) CommentReviewPR(_ context.Context, _, _ string, _ int, _ string) error {
	return ErrDemoMode
}

func (s *Service) SubmitReviewWithComments(_ context.Context, _, _ string, _ int, _ string, _ string, _ []github.ReviewCommentPayload) error {
	return ErrDemoMode
}

func (s *Service) RerunWorkflow(_ context.Context, _, _ string, _ int64, _ bool) error {
	return ErrDemoMode
}

func (s *Service) ReplyToComment(_ context.Context, _, _ string, _ int, _ int64, _ string) error {
	return ErrDemoMode
}
