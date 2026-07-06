package ui

import (
	"context"

	"github.com/shhac/prtea/internal/github"
)

// PRSession holds all state related to the currently selected PR.
// When no PR is selected, the App's session field is nil.
type PRSession struct {
	// PR identity
	Owner   string
	Repo    string
	Number  int
	Title   string
	HTMLURL string

	// PR data
	DiffFiles             []github.PRFile               // stored for AI context
	PendingInlineComments []github.ReviewCommentPayload // pending inline comments for the next review

	// AI thread state. The display transcript and any pending action
	// proposals live in the chat panel; the transcript is persisted alongside
	// the thread ID via the thread store.
	ThreadID string // codex session ID ("" until the first turn starts one)

	// Data cached for AI context
	Comments       []github.Comment
	InlineComments []github.InlineComment

	// Active AI turn
	AIEventCh aiEventChan        // event channel for the in-flight turn
	AICancel  context.CancelFunc // cancels the in-flight turn
}

// CancelAITurn cancels any in-flight AI turn.
func (s *PRSession) CancelAITurn() {
	if s.AICancel != nil {
		s.AICancel()
		s.AICancel = nil
	}
	s.AIEventCh = nil
}

// MatchesPR returns true if this session is for the given PR number.
func (s *PRSession) MatchesPR(prNumber int) bool {
	return s != nil && s.Number == prNumber
}
