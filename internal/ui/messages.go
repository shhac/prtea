package ui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/shhac/prtea/internal/claude"
	"github.com/shhac/prtea/internal/github"
)

// -- GitHub client lifecycle --

// GHClientReadyMsg is sent when the GitHub client has been created successfully.
type GHClientReadyMsg struct {
	Client GitHubService
}

// GHClientErrorMsg is sent when the GitHub client fails to initialize.
type GHClientErrorMsg struct {
	Err error
}

// -- PR list data --

// PRsLoadedMsg is sent when PR data has been fetched successfully.
type PRsLoadedMsg struct {
	ToReview []github.PRItem
	MyPRs    []github.PRItem
}

// PRsErrorMsg is sent when PR fetching fails.
type PRsErrorMsg struct {
	Err error
}

// PRReviewDecisionsMsg delivers review decisions fetched asynchronously after PR list load.
type PRReviewDecisionsMsg struct {
	Decisions map[string]string // key: "owner/repo#number", value: review decision
}

// -- PR selection --

// PRSelectedMsg is sent when the user selects a PR.
type PRSelectedMsg struct {
	Owner   string
	Repo    string
	Number  int
	HTMLURL string
}

// PRSelectedAndAdvanceMsg is sent when ENTER selects a PR and should advance focus to the diff viewer.
type PRSelectedAndAdvanceMsg struct {
	Owner   string
	Repo    string
	Number  int
	HTMLURL string
}

// -- Diff / PR detail --

// DiffLoadedMsg is sent when PR diff data has been fetched.
type DiffLoadedMsg struct {
	PRNumber int
	Files    []github.PRFile
	Err      error
}

// PRDetailLoadedMsg is sent when PR detail data has been fetched.
type PRDetailLoadedMsg struct {
	PRNumber int
	Detail   *github.PRDetail
	Err      error
}

// -- Comments --

// CommentsLoadedMsg is sent when PR comments have been fetched.
type CommentsLoadedMsg struct {
	PRNumber       int
	Comments       []github.Comment
	InlineComments []github.InlineComment
	Err            error
}

// -- CI & reviews --

// CIStatusLoadedMsg is sent when CI check status has been fetched.
type CIStatusLoadedMsg struct {
	PRNumber int
	Status   *github.CIStatus
	Err      error
}

// ReviewsLoadedMsg is sent when review status has been fetched.
type ReviewsLoadedMsg struct {
	PRNumber int
	Summary  *github.ReviewSummary
	Err      error
}

// -- CI re-run --

// CIRerunRequestMsg is emitted when the user requests a CI re-run (x key or :rerun ci).
type CIRerunRequestMsg struct{}

// CIRerunDoneMsg is sent when CI workflow re-run succeeds.
type CIRerunDoneMsg struct {
	PRNumber int
	Count    int // number of workflows re-run
}

// CIRerunErrMsg is sent when CI workflow re-run fails.
type CIRerunErrMsg struct {
	PRNumber int
	Err      error
}

// -- Claude analysis --

// AnalysisCompleteMsg is sent when Claude analysis finishes successfully.
type AnalysisCompleteMsg struct {
	PRNumber int
	DiffHash string
	Result   *claude.AnalysisResult
}

// AnalysisErrorMsg is sent when Claude analysis fails.
type AnalysisErrorMsg struct {
	PRNumber int
	Err      error
}

// -- PR actions --

// PRApproveDoneMsg is sent when PR approval succeeds.
type PRApproveDoneMsg struct {
	PRNumber int
}

// PRApproveErrMsg is sent when PR approval fails.
type PRApproveErrMsg struct {
	PRNumber int
	Err      error
}

// PRCloseDoneMsg is sent when PR close succeeds.
type PRCloseDoneMsg struct {
	PRNumber int
}

// PRCloseErrMsg is sent when PR close fails.
type PRCloseErrMsg struct {
	PRNumber int
	Err      error
}

// -- Review submission --

// ReviewAction represents the type of PR review to submit.
type ReviewAction int

const (
	ReviewApprove        ReviewAction = iota
	ReviewComment
	ReviewRequestChanges
)

// ReviewSubmitMsg is emitted by the chat panel when the user submits a review.
type ReviewSubmitMsg struct {
	Action         ReviewAction
	Body           string
	InlineComments []claude.InlineReviewComment // optional inline comments from AI review
}

// ReviewSubmitDoneMsg is sent when review submission succeeds.
type ReviewSubmitDoneMsg struct {
	PRNumber int
	Action   ReviewAction
}

// ReviewSubmitErrMsg is sent when review submission fails.
type ReviewSubmitErrMsg struct {
	PRNumber int
	Err      error
}

// ReviewValidationMsg is emitted by the review tab when validation fails
// (e.g. empty body for Request Changes or Comment).
type ReviewValidationMsg struct {
	Message string
}

// -- AI Review --

// AIReviewCompleteMsg is sent when AI review generation finishes successfully.
type AIReviewCompleteMsg struct {
	PRNumber int
	Result   *claude.ReviewAnalysis
}

// AIReviewErrorMsg is sent when AI review generation fails.
type AIReviewErrorMsg struct {
	PRNumber int
	Err      error
}

// -- Chat panel --

// ModeChangedMsg is sent when the chat panel changes modes.
type ModeChangedMsg struct {
	Mode ChatMode
}

// ChatClearMsg is emitted when the user wants to start a new chat.
type ChatClearMsg struct{}

// ChatSendMsg is emitted when the user sends a chat message.
type ChatSendMsg struct {
	Message string
}

// ChatResponseMsg is sent when Claude responds to a chat message.
type ChatResponseMsg struct {
	Content string
	Err     error
}

// ChatStreamChunkMsg carries a streaming text chunk from Claude.
type ChatStreamChunkMsg struct {
	Content string
}

// CommentPostMsg is emitted when the user wants to post a PR comment.
type CommentPostMsg struct {
	Body string
}

// CommentPostedMsg is sent after a comment has been posted (or failed).
type CommentPostedMsg struct {
	Err error
}

// -- Navigation --

// HunkSelectedAndAdvanceMsg is sent when ENTER selects a hunk and should advance focus to the chat panel.
type HunkSelectedAndAdvanceMsg struct{}

// HelpClosedMsg is sent when the help overlay is dismissed.
type HelpClosedMsg struct{}

// StatusBarClearMsg is sent after a delay to clear the status bar temporary message.
type StatusBarClearMsg struct {
	// Seq is a monotonic counter to ensure only the latest clear fires.
	Seq int
}

// -- Command mode --

// CommandExecuteMsg is sent when a command should be executed.
type CommandExecuteMsg struct {
	Name string
}

// CommandModeExitMsg is sent when command mode is dismissed without executing.
type CommandModeExitMsg struct{}

// CommandNotFoundMsg is sent when an unrecognized command is entered.
type CommandNotFoundMsg struct {
	Input string
}

// -- Settings --

// ConfigChangedMsg is sent when the user changes settings in the settings panel.
type ConfigChangedMsg struct{}

// SettingsClosedMsg is sent when the settings overlay is dismissed.
type SettingsClosedMsg struct{}

// -- Background polling --

// pollTickMsg is sent by the periodic timer to trigger a background PR list fetch.
type pollTickMsg struct{}

// pollPRsLoadedMsg is sent when background polling fetches PR data successfully.
// Separate from PRsLoadedMsg to allow non-disruptive merging.
type pollPRsLoadedMsg struct {
	ToReview []github.PRItem
	MyPRs    []github.PRItem
}

// pollErrorMsg is sent when background polling fails, so transient issues
// (auth expiry, network errors) are visible to the user.
type pollErrorMsg struct {
	Err error
}

// -- Inline comment authoring --

// InlineCommentAddMsg is emitted by the diff viewer when the user saves an inline comment.
type InlineCommentAddMsg struct {
	Path      string
	Line      int
	Body      string
	StartLine int // non-zero for multi-line range comments
}

// PendingInlineComment wraps an inline review comment with source tracking
// to distinguish AI-generated comments from user-authored ones.
type PendingInlineComment struct {
	claude.InlineReviewComment
	Source string // "ai" or "user"
}

// -- Comment overlay --

// ShowCommentOverlayMsg requests opening the comment view overlay.
type ShowCommentOverlayMsg struct {
	Path            string
	Line            int
	StartLine       int      // non-zero for multi-line range comments
	DiffLines       []string // raw hunk lines for context display
	TargetLineInCtx int      // index of target line within DiffLines
	GHThreads       []ghCommentThread
	AIComments      []claude.InlineReviewComment
	PendingComments []PendingInlineComment
}

// CommentOverlayClosedMsg signals the comment overlay was dismissed.
type CommentOverlayClosedMsg struct{}

// InlineCommentReplyMsg posts an immediate reply to a GitHub thread.
type InlineCommentReplyMsg struct {
	CommentID int64
	Body      string
}

// InlineCommentReplyDoneMsg signals the reply was posted (or failed).
type InlineCommentReplyDoneMsg struct {
	Err error
}

// -- Internal streaming --

// chatStreamChan carries streaming chunks and the final response from Claude chat.
type chatStreamChan chan tea.Msg

// analysisStreamChan carries streaming chunks and the final result from Claude analysis.
type analysisStreamChan chan tea.Msg

// AnalysisStreamChunkMsg carries a streaming text chunk during analysis.
type AnalysisStreamChunkMsg struct {
	Content string
}
