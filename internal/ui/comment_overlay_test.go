package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/shhac/prtea/internal/github"
)

// collectMsgs executes a tea.Cmd tree (Batch/Sequence) and returns every
// produced message.
func collectMsgs(t *testing.T, cmd tea.Cmd) []tea.Msg {
	t.Helper()
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, sub := range batch {
			out = append(out, collectMsgs(t, sub)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

// TestOverlaySaveAnnouncesClose pins the invariant behind a stuck-mode bug:
// every path that hides the overlay must emit CommentOverlayClosedMsg, or the
// app stays in overlay mode routing keys to an invisible overlay.
func TestOverlaySaveAnnouncesClose(t *testing.T) {
	newOverlay := func(threads []ghCommentThread) CommentOverlayModel {
		m := NewCommentOverlayModel()
		m.SetSize(120, 40)
		m.Show(ShowCommentOverlayMsg{Path: "a.go", Line: 5, GHThreads: threads})
		m.composing = true
		m.textarea.SetValue("a comment body")
		return m
	}

	t.Run("draft save", func(t *testing.T) {
		m := newOverlay(nil)
		m, cmd := m.updateComposing(tea.KeyMsg{Type: tea.KeyCtrlS})
		if m.IsVisible() {
			t.Error("overlay must hide on save")
		}
		msgs := collectMsgs(t, cmd)
		assertOverlayCloseAnd(t, msgs, func(msg tea.Msg) bool {
			_, ok := msg.(InlineCommentAddMsg)
			return ok
		}, "InlineCommentAddMsg")
	})

	t.Run("immediate reply", func(t *testing.T) {
		m := newOverlay([]ghCommentThread{{Root: github.InlineComment{ID: 42}}})
		m, cmd := m.updateComposing(tea.KeyMsg{Type: tea.KeyCtrlS})
		if m.IsVisible() {
			t.Error("overlay must hide on reply")
		}
		msgs := collectMsgs(t, cmd)
		assertOverlayCloseAnd(t, msgs, func(msg tea.Msg) bool {
			_, ok := msg.(InlineCommentReplyMsg)
			return ok
		}, "InlineCommentReplyMsg")
	})
}

func assertOverlayCloseAnd(t *testing.T, msgs []tea.Msg, isPayload func(tea.Msg) bool, payloadName string) {
	t.Helper()
	var sawClose, sawPayload bool
	for _, msg := range msgs {
		if _, ok := msg.(CommentOverlayClosedMsg); ok {
			sawClose = true
		}
		if isPayload(msg) {
			sawPayload = true
		}
	}
	if !sawClose {
		t.Errorf("save did not emit CommentOverlayClosedMsg (stuck-mode regression); msgs: %#v", msgs)
	}
	if !sawPayload {
		t.Errorf("save did not emit %s; msgs: %#v", payloadName, msgs)
	}
}
