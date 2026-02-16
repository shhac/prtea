package ui

import (
	"context"
	"time"

	"github.com/shhac/prtea/internal/claude"
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
	RerunWorkflow(ctx context.Context, owner, repo string, runID int64, failedOnly bool) error
	ReplyToComment(ctx context.Context, owner, repo string, prNumber int, commentID int64, body string) error
}

// AIAnalyzer defines the analysis operations used by the UI layer.
// *claude.Analyzer satisfies this interface.
type AIAnalyzer interface {
	Analyze(ctx context.Context, input claude.AnalyzeInput, onProgress claude.ProgressFunc) (*claude.AnalysisResult, error)
	AnalyzeDiff(ctx context.Context, input claude.AnalyzeDiffInput, onProgress claude.ProgressFunc) (*claude.AnalysisResult, error)
	AnalyzeDiffStream(ctx context.Context, input claude.AnalyzeDiffInput, onChunk func(string)) (*claude.AnalysisResult, error)
	AnalyzeForReview(ctx context.Context, input claude.ReviewInput, onProgress claude.ProgressFunc) (*claude.ReviewAnalysis, error)
	SetTimeout(d time.Duration)
	SetAnalysisMaxTurns(n int)
}

// AIChatService defines the chat operations used by the UI layer.
// *claude.ChatService satisfies this interface.
type AIChatService interface {
	ChatStream(ctx context.Context, input claude.ChatInput, onChunk func(text string)) (string, error)
	ClearSession(owner, repo string, prNumber int)
	SaveSession(owner, repo string, prNumber int)
	GetSessionMessages(owner, repo string, prNumber int) []claude.ChatMessage
	SetTimeout(d time.Duration)
	SetMaxPromptTokens(n int)
	SetMaxHistoryMessages(n int)
	SetMaxTurns(n int)
}
