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

// SelectedPR tracks the currently selected PR's metadata for global actions.
type SelectedPR struct {
	Owner   string
	Repo    string
	Number  int
	Title   string
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

// -- Internal streaming --

// chatStreamChan carries streaming chunks and the final response from Claude chat.
type chatStreamChan chan tea.Msg
