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
	DiffFiles            []github.PRFile        // stored for analysis context
	PendingInlineComments []PendingInlineComment // unified pool of pending comments

	// Streaming state
	StreamChan           chatStreamChan     // active chat streaming channel
	StreamCancel         context.CancelFunc // cancels active stream goroutine
	AnalysisStreamCh     analysisStreamChan // active analysis streaming channel
	AnalysisStreamCancel context.CancelFunc // cancels active analysis stream

	// Analysis state
	Analyzing bool
}

// CancelStreams cancels any active chat and analysis stream goroutines.
func (s *PRSession) CancelStreams() {
	if s.StreamCancel != nil {
		s.StreamCancel()
		s.StreamCancel = nil
	}
	s.StreamChan = nil
	if s.AnalysisStreamCancel != nil {
		s.AnalysisStreamCancel()
		s.AnalysisStreamCancel = nil
	}
	s.AnalysisStreamCh = nil
	s.Analyzing = false
}

// MatchesPR returns true if this session is for the given PR number.
func (s *PRSession) MatchesPR(prNumber int) bool {
	return s != nil && s.Number == prNumber
}
