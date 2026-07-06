package ai

import (
	"strings"
	"testing"
)

func TestBuildThreadPromptWithRepo(t *testing.T) {
	prompt := buildThreadPrompt(ThreadInput{
		Owner: "acme", Repo: "widgets", PRNumber: 12,
		PRContext:    "PR title and diff here",
		CustomPrompt: "Always check the migration files.",
		RepoPath:     "/repos/widgets",
		Message:      "what changed?",
	})

	for _, want := range []string{
		"read-only", "prtea-action", "post_comment", "reply_to_comment", "submit_review",
		"local checkout of this repository",
		"Always check the migration files.",
		"acme/widgets #12",
		"PR title and diff here",
		"what changed?",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
}

func TestBuildThreadPromptWithoutRepo(t *testing.T) {
	prompt := buildThreadPrompt(ThreadInput{
		Owner: "o", Repo: "r", PRNumber: 1,
		PRContext: "ctx", Message: "hi",
	})
	if !strings.Contains(prompt, "No local checkout is available") {
		t.Error("prompt missing no-checkout notice")
	}
	if strings.Contains(prompt, "Repository-specific instructions") {
		t.Error("prompt should omit custom-instructions header when empty")
	}
}

func TestBuildFollowUpPrompt(t *testing.T) {
	if got := buildFollowUpPrompt(MessageInput{Message: "just a question"}); got != "just a question" {
		t.Errorf("bare follow-up = %q", got)
	}

	got := buildFollowUpPrompt(MessageInput{Message: "explain this hunk", ContextDelta: "selected hunk: foo.go"})
	if !strings.Contains(got, "selected hunk: foo.go") || !strings.Contains(got, "explain this hunk") {
		t.Errorf("follow-up with delta = %q", got)
	}
	if !strings.Contains(got, "Updated context") {
		t.Errorf("follow-up missing delta header: %q", got)
	}
}
