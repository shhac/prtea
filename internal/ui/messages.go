package ui

import (
	"github.com/shhac/prtea/internal/ai"
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

// PRSelectedMsg is sent when the user selects a PR. Advance requests moving
// focus to the diff viewer (the Enter binding).
type PRSelectedMsg struct {
	Owner   string
	Repo    string
	Number  int
	HTMLURL string
	Advance bool
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

// -- Review submission --

// ReviewAction represents the type of PR review to submit.
type ReviewAction int

const (
	ReviewApprove ReviewAction = iota
	ReviewComment
	ReviewRequestChanges
)

// reviewActionMeta is the single source of truth for the three-valued review
// concept: config/AI event strings, GitHub API events, and user-facing labels.
var reviewActionMeta = map[ReviewAction]struct {
	value    string // config value and ai.Action event string
	apiEvent string // GitHub REST review event
	label    string
	progress string
	past     string
}{
	ReviewApprove:        {"approve", "APPROVE", "Approve", "Approving", "Approved"},
	ReviewComment:        {"comment", "COMMENT", "Comment", "Submitting comment on", "Commented on"},
	ReviewRequestChanges: {"request_changes", "REQUEST_CHANGES", "Request Changes", "Requesting changes on", "Requested changes on"},
}

// Label returns the short user-facing name ("Approve").
func (a ReviewAction) Label() string { return reviewActionMeta[a].label }

// ProgressLabel returns the in-flight status prefix ("Approving").
func (a ReviewAction) ProgressLabel() string { return reviewActionMeta[a].progress }

// PastLabel returns the completed status prefix ("Approved").
func (a ReviewAction) PastLabel() string { return reviewActionMeta[a].past }

// APIEvent returns the GitHub REST review event ("APPROVE").
func (a ReviewAction) APIEvent() string { return reviewActionMeta[a].apiEvent }

// reviewActionFromValue converts a config/AI event string ("approve") to a
// ReviewAction, reporting whether the value was recognized.
func reviewActionFromValue(value string) (ReviewAction, bool) {
	for action, meta := range reviewActionMeta {
		if meta.value == value {
			return action, true
		}
	}
	return ReviewComment, false
}

// ParseReviewAction converts a config string to a ReviewAction, defaulting to
// ReviewComment for unrecognized values.
func ParseReviewAction(value string) ReviewAction {
	action, _ := reviewActionFromValue(value)
	return action
}

// ReviewSubmitMsg is emitted by the chat panel when the user submits a review.
type ReviewSubmitMsg struct {
	Action ReviewAction
	Body   string
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

// AIEventMsg carries one normalized engine event for the active turn.
// Owner/Repo identify the PR so late events can still be attributed after
// the user navigates away.
type AIEventMsg struct {
	Owner    string
	Repo     string
	PRNumber int
	Event    ai.Event
}

// AIActionRespondMsg is emitted by the chat panel when the user confirms or
// dismisses its pending proposed actions. The panel is the single owner of
// pending-action state, so the actions travel with the message.
type AIActionRespondMsg struct {
	Approve bool
	Actions []ai.Action
}

// AIActionResultMsg reports the outcome of executing a confirmed action.
type AIActionResultMsg struct {
	Description string
	Err         error
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

// -- Comment overlay --

// ShowCommentOverlayMsg requests opening the comment view overlay.
type ShowCommentOverlayMsg struct {
	Path            string
	Line            int
	StartLine       int      // non-zero for multi-line range comments
	DiffLines       []string // raw hunk lines for context display
	TargetLineInCtx int      // index of target line within DiffLines
	GHThreads       []ghCommentThread
	PendingComments []github.ReviewCommentPayload
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

// aiEventChan carries AIEventMsg values from the engine goroutine to the UI.
type aiEventChan chan AIEventMsg
