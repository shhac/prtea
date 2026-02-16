package ui

import (
	"testing"

	"github.com/shhac/prtea/internal/claude"
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
	if tab.messages[0].role != "user" {
		t.Errorf("role = %q, want user", tab.messages[0].role)
	}
	if tab.messages[0].content != "hello" {
		t.Errorf("content = %q", tab.messages[0].content)
	}
}

func TestChatTab_AddResponse(t *testing.T) {
	tab := &ChatTabModel{}
	tab.SetWaiting("question")
	tab.AddResponse("answer")

	if tab.MessageCount() != 2 {
		t.Errorf("MessageCount = %d, want 2", tab.MessageCount())
	}
	if tab.IsWaiting() {
		t.Error("expected IsWaiting=false after response")
	}
	if tab.messages[1].role != "assistant" {
		t.Errorf("role = %q", tab.messages[1].role)
	}
	if tab.messages[1].content != "answer" {
		t.Errorf("content = %q", tab.messages[1].content)
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
}

func TestChatTab_RestoreMessages(t *testing.T) {
	tab := &ChatTabModel{}
	msgs := []claude.ChatMessage{
		{Role: "user", Content: "first"},
		{Role: "assistant", Content: "second"},
		{Role: "user", Content: "third"},
	}
	tab.RestoreMessages(msgs)

	if tab.MessageCount() != 3 {
		t.Fatalf("MessageCount = %d, want 3", tab.MessageCount())
	}
	if tab.messages[0].role != "user" || tab.messages[0].content != "first" {
		t.Errorf("messages[0] = %+v", tab.messages[0])
	}
	if tab.messages[1].role != "assistant" || tab.messages[1].content != "second" {
		t.Errorf("messages[1] = %+v", tab.messages[1])
	}
}

func TestChatTab_StateSequence(t *testing.T) {
	tab := &ChatTabModel{}

	// Send question, get response
	tab.SetWaiting("q1")
	if !tab.IsWaiting() {
		t.Error("should be waiting after SetWaiting")
	}
	tab.AddResponse("a1")
	if tab.IsWaiting() {
		t.Error("should not be waiting after AddResponse")
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
	tab.RestoreMessages([]claude.ChatMessage{{Role: "user", Content: "test"}})
	if tab.cache != "" {
		t.Error("RestoreMessages should invalidate cache")
	}
}
