package ui

import (
	"testing"

	"github.com/shhac/prtea/internal/ai"
)

func TestChatTab_SetWaiting(t *testing.T) {
	tab := &ChatTabModel{}
	tab.SetWaiting("hello")

	if tab.MessageCount() != 1 {
		t.Errorf("MessageCount = %d, want 1", tab.MessageCount())
	}
	if !tab.IsWaiting() {
		t.Error("expected IsWaiting=true")
	}
	if tab.chatError != "" {
		t.Errorf("chatError = %q", tab.chatError)
	}
	if tab.messages[0].Role != ai.RoleUser {
		t.Errorf("role = %q, want user", tab.messages[0].Role)
	}
	if tab.messages[0].Content != "hello" {
		t.Errorf("content = %q", tab.messages[0].Content)
	}
}

func TestChatTab_AddResponseAndTurnDone(t *testing.T) {
	tab := &ChatTabModel{}
	tab.SetWaiting("question")
	tab.AddResponse("answer")

	if tab.MessageCount() != 2 {
		t.Errorf("MessageCount = %d, want 2", tab.MessageCount())
	}
	// Waiting persists until the turn completes — an agentic turn can emit
	// several messages before it is done.
	if !tab.IsWaiting() {
		t.Error("expected IsWaiting=true until SetTurnDone")
	}
	tab.SetTurnDone()
	if tab.IsWaiting() {
		t.Error("expected IsWaiting=false after SetTurnDone")
	}
	if tab.messages[1].Role != ai.RoleAssistant {
		t.Errorf("role = %q", tab.messages[1].Role)
	}
	if tab.messages[1].Content != "answer" {
		t.Errorf("content = %q", tab.messages[1].Content)
	}
}

func TestChatTab_ActivityLinesExcludedFromCount(t *testing.T) {
	tab := &ChatTabModel{}
	tab.SetWaiting("question")
	tab.AddActivity("▸ rg foo")
	tab.AddActivity("· thinking")
	tab.AddResponse("answer")

	if tab.MessageCount() != 2 {
		t.Errorf("MessageCount = %d, want 2 (activity lines excluded)", tab.MessageCount())
	}
	if len(tab.Messages()) != 4 {
		t.Errorf("Messages() len = %d, want 4 (activity lines included)", len(tab.Messages()))
	}
}

func TestChatTab_SetChatError(t *testing.T) {
	tab := &ChatTabModel{}
	tab.SetWaiting("question")
	tab.SetChatError("timeout")

	if tab.IsWaiting() {
		t.Error("expected IsWaiting=false after error")
	}
	if tab.chatError != "timeout" {
		t.Errorf("chatError = %q", tab.chatError)
	}
}

func TestChatTab_ClearChat(t *testing.T) {
	tab := &ChatTabModel{}
	tab.SetWaiting("q1")
	tab.AddResponse("a1")
	tab.SetWaiting("q2")
	tab.SetPendingActions([]ai.Action{{Type: ai.ActionPostComment, Body: "x"}})

	tab.ClearChat()

	if tab.MessageCount() != 0 {
		t.Errorf("MessageCount = %d, want 0", tab.MessageCount())
	}
	if tab.IsWaiting() {
		t.Error("expected IsWaiting=false")
	}
	if tab.chatError != "" {
		t.Errorf("chatError = %q", tab.chatError)
	}
	if tab.HasPendingActions() {
		t.Error("expected pending actions cleared")
	}
}

func TestChatTab_PendingActions(t *testing.T) {
	tab := &ChatTabModel{}
	if tab.HasPendingActions() {
		t.Error("new tab should have no pending actions")
	}
	tab.SetPendingActions([]ai.Action{{Type: ai.ActionPostComment, Body: "hi"}})
	if !tab.HasPendingActions() {
		t.Error("expected pending actions")
	}
	tab.ClearPendingActions()
	if tab.HasPendingActions() {
		t.Error("expected no pending actions after clear")
	}
}

func TestChatTab_RestoreMessages(t *testing.T) {
	tab := &ChatTabModel{}
	msgs := []ai.Message{
		{Role: ai.RoleUser, Content: "first"},
		{Role: ai.RoleActivity, Content: "▸ ls"},
		{Role: ai.RoleAssistant, Content: "second"},
	}
	tab.RestoreMessages(msgs)

	if tab.MessageCount() != 2 {
		t.Fatalf("MessageCount = %d, want 2", tab.MessageCount())
	}
	if tab.messages[0].Role != ai.RoleUser || tab.messages[0].Content != "first" {
		t.Errorf("messages[0] = %+v", tab.messages[0])
	}
	if tab.messages[2].Role != ai.RoleAssistant || tab.messages[2].Content != "second" {
		t.Errorf("messages[2] = %+v", tab.messages[2])
	}
}

func TestChatTab_StateSequence(t *testing.T) {
	tab := &ChatTabModel{}

	// Send question, get response, turn completes
	tab.SetWaiting("q1")
	if !tab.IsWaiting() {
		t.Error("should be waiting after SetWaiting")
	}
	tab.AddResponse("a1")
	tab.SetTurnDone()
	if tab.IsWaiting() {
		t.Error("should not be waiting after SetTurnDone")
	}

	// Send question, get error
	tab.SetWaiting("q2")
	tab.SetChatError("network error")
	if tab.IsWaiting() {
		t.Error("should not be waiting after SetChatError")
	}
	if tab.chatError != "network error" {
		t.Errorf("chatError = %q", tab.chatError)
	}
	if tab.MessageCount() != 3 {
		t.Errorf("MessageCount = %d, want 3 (q1, a1, q2)", tab.MessageCount())
	}

	// Send question, get response (error should be cleared)
	tab.SetWaiting("q3")
	if tab.chatError != "" {
		t.Error("chatError should be cleared by SetWaiting")
	}
	tab.AddResponse("a3")
	if tab.chatError != "" {
		t.Error("chatError should be cleared by AddResponse")
	}

	// Clear everything
	tab.ClearChat()
	if tab.MessageCount() != 0 {
		t.Errorf("MessageCount = %d after ClearChat", tab.MessageCount())
	}
}

func TestChatTab_CacheInvalidation(t *testing.T) {
	tab := &ChatTabModel{}
	tab.cache = "cached content"
	tab.cacheWidth = 80

	tab.SetWaiting("test")
	if tab.cache != "" {
		t.Error("SetWaiting should invalidate cache")
	}

	tab.cache = "cached"
	tab.AddResponse("response")
	if tab.cache != "" {
		t.Error("AddResponse should invalidate cache")
	}

	tab.cache = "cached"
	tab.AddActivity("▸ ls")
	if tab.cache != "" {
		t.Error("AddActivity should invalidate cache")
	}

	tab.cache = "cached"
	tab.SetChatError("err")
	if tab.cache != "" {
		t.Error("SetChatError should invalidate cache")
	}

	tab.cache = "cached"
	tab.ClearChat()
	if tab.cache != "" {
		t.Error("ClearChat should invalidate cache")
	}

	tab.cache = "cached"
	tab.RestoreMessages([]ai.Message{{Role: ai.RoleUser, Content: "test"}})
	if tab.cache != "" {
		t.Error("RestoreMessages should invalidate cache")
	}
}
