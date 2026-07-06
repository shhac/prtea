package ui

// Pending inline comments: the pool of user-authored draft comments queued
// for the next review submission.

import (
	"fmt"

	"github.com/shhac/prtea/internal/github"
)

// syncPendingComments propagates the session's pending comment pool to the
// diff viewer and the review tab counter.
func (m *App) syncPendingComments() {
	m.diffViewer.SetPendingInlineComments(m.session.PendingInlineComments)
	m.chatPanel.SetPendingComments(m.session.PendingInlineComments)
}

// findPendingComment returns the index of the pending comment at
// path:line:startLine, or -1.
func findPendingComment(comments []github.ReviewCommentPayload, path string, line, startLine int) int {
	for i, c := range comments {
		if c.Path == path && c.Line == line && c.StartLine == startLine {
			return i
		}
	}
	return -1
}

// removePendingComment removes the pending comment at path:line:startLine,
// reporting whether one was found.
func removePendingComment(comments []github.ReviewCommentPayload, path string, line, startLine int) ([]github.ReviewCommentPayload, bool) {
	i := findPendingComment(comments, path, line, startLine)
	if i == -1 {
		return comments, false
	}
	return append(comments[:i], comments[i+1:]...), true
}

// upsertPendingComment updates the body of an existing pending comment at the
// message's target, or appends a new one. Reports whether an existing comment
// was updated.
func upsertPendingComment(comments []github.ReviewCommentPayload, msg InlineCommentAddMsg) ([]github.ReviewCommentPayload, bool) {
	if i := findPendingComment(comments, msg.Path, msg.Line, msg.StartLine); i != -1 {
		comments[i].Body = msg.Body
		return comments, true
	}

	comment := github.ReviewCommentPayload{
		Path: msg.Path,
		Line: msg.Line,
		Side: "RIGHT",
		Body: msg.Body,
	}
	if msg.StartLine > 0 {
		comment.StartLine = msg.StartLine
		comment.StartSide = "RIGHT"
	}
	return append(comments, comment), false
}

// formatCommentTarget renders a comment location as path:line, or
// path:start-end for multi-line ranges.
func formatCommentTarget(path string, startLine, line int) string {
	if startLine > 0 {
		return fmt.Sprintf("%s:%d-%d", path, startLine, line)
	}
	return fmt.Sprintf("%s:%d", path, line)
}
