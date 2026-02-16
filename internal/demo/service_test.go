package demo

import (
	"context"
	"errors"
	"testing"

	"github.com/shhac/prtea/internal/github"
)

func TestNewService(t *testing.T) {
	s := NewService()
	if s == nil {
		t.Fatal("NewService returned nil")
	}
	if s.username != demoUsername {
		t.Errorf("username = %q, want %q", s.username, demoUsername)
	}
}

func TestGetUsername(t *testing.T) {
	s := NewService()
	if got := s.GetUsername(); got != demoUsername {
		t.Errorf("GetUsername() = %q, want %q", got, demoUsername)
	}
}

func TestGetPRsForReview(t *testing.T) {
	s := NewService()
	prs, err := s.GetPRsForReview(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(prs) == 0 {
		t.Fatal("expected non-empty PRs for review")
	}
	if prs[0].Number != 101 {
		t.Errorf("first PR number = %d, want 101", prs[0].Number)
	}
}

func TestGetMyPRs(t *testing.T) {
	s := NewService()
	prs, err := s.GetMyPRs(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(prs) == 0 {
		t.Fatal("expected non-empty my PRs")
	}
	// My PRs should be authored by demo user
	for _, pr := range prs {
		if pr.Author.Login != demoUsername {
			t.Errorf("expected author %q, got %q", demoUsername, pr.Author.Login)
		}
	}
}

func TestGetPRDetail_Found(t *testing.T) {
	s := NewService()
	detail, err := s.GetPRDetail(context.Background(), "acme", "gateway", 101)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if detail.Title != "Add rate limiting middleware" {
		t.Errorf("Title = %q", detail.Title)
	}
}

func TestGetPRDetail_NotFound(t *testing.T) {
	s := NewService()
	_, err := s.GetPRDetail(context.Background(), "acme", "gateway", 999)
	if err == nil {
		t.Fatal("expected error for missing PR")
	}
}

func TestGetPRFiles_Found(t *testing.T) {
	s := NewService()
	files, err := s.GetPRFiles(context.Background(), "acme", "gateway", 101)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("expected non-empty files")
	}
}

func TestGetPRFiles_NotFound(t *testing.T) {
	s := NewService()
	_, err := s.GetPRFiles(context.Background(), "acme", "gateway", 999)
	if err == nil {
		t.Fatal("expected error for missing PR")
	}
}

func TestGetComments(t *testing.T) {
	s := NewService()
	// PR 101 has comments
	comments, err := s.GetComments(context.Background(), "acme", "gateway", 101)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(comments) == 0 {
		t.Fatal("expected comments for PR 101")
	}

	// PR with no comments returns empty
	comments, err = s.GetComments(context.Background(), "acme", "nexus", 303)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(comments) != 0 {
		t.Errorf("expected no comments for PR 303, got %d", len(comments))
	}
}

func TestGetInlineComments(t *testing.T) {
	s := NewService()
	inline, err := s.GetInlineComments(context.Background(), "acme", "platform", 404)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(inline) == 0 {
		t.Fatal("expected inline comments for PR 404")
	}
}

func TestGetCIStatus_Found(t *testing.T) {
	s := NewService()
	ci, err := s.GetCIStatus(context.Background(), "acme", "gateway", "abc", 101)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ci.OverallStatus != "passing" {
		t.Errorf("OverallStatus = %q", ci.OverallStatus)
	}
}

func TestGetCIStatus_NotFound(t *testing.T) {
	s := NewService()
	ci, err := s.GetCIStatus(context.Background(), "acme", "gateway", "abc", 999)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Returns empty CIStatus, not an error
	if ci.TotalCount != 0 {
		t.Errorf("TotalCount = %d, want 0", ci.TotalCount)
	}
}

func TestGetReviews_Found(t *testing.T) {
	s := NewService()
	reviews, err := s.GetReviews(context.Background(), "acme", "gateway", 101)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reviews.ReviewDecision != "APPROVED" {
		t.Errorf("ReviewDecision = %q", reviews.ReviewDecision)
	}
}

func TestGetReviews_NotFound(t *testing.T) {
	s := NewService()
	reviews, err := s.GetReviews(context.Background(), "acme", "gateway", 999)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reviews.ReviewDecision != "" {
		t.Errorf("ReviewDecision = %q, want empty", reviews.ReviewDecision)
	}
}

func TestSetFetchLimit(t *testing.T) {
	s := NewService()
	// Should not panic — it's a no-op
	s.SetFetchLimit(100)
}

// TestWriteOperationsReturnErrDemoMode verifies all write methods return ErrDemoMode.
func TestWriteOperationsReturnErrDemoMode(t *testing.T) {
	s := NewService()
	ctx := context.Background()

	tests := []struct {
		name string
		fn   func() error
	}{
		{"ApprovePR", func() error { return s.ApprovePR(ctx, "o", "r", 1, "lgtm") }},
		{"PostComment", func() error { return s.PostComment(ctx, "o", "r", 1, "comment") }},
		{"ClosePR", func() error { return s.ClosePR(ctx, "o", "r", 1) }},
		{"RequestChangesPR", func() error { return s.RequestChangesPR(ctx, "o", "r", 1, "changes") }},
		{"CommentReviewPR", func() error { return s.CommentReviewPR(ctx, "o", "r", 1, "note") }},
		{"SubmitReviewWithComments", func() error {
			return s.SubmitReviewWithComments(ctx, "o", "r", 1, "COMMENT", "body", []github.ReviewCommentPayload{})
		}},
		{"RerunWorkflow", func() error { return s.RerunWorkflow(ctx, "o", "r", 1, false) }},
		{"ReplyToComment", func() error { return s.ReplyToComment(ctx, "o", "r", 1, 123, "reply") }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.fn()
			if !errors.Is(err, ErrDemoMode) {
				t.Errorf("got error %v, want ErrDemoMode", err)
			}
		})
	}
}
