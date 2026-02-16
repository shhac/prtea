package github

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestGetPRDetail_Success(t *testing.T) {
	prView := ghPRView{
		Number:      42,
		Title:       "Add feature",
		Body:        "Description",
		URL:         "https://github.com/alice/widget/pull/42",
		Mergeable:   "MERGEABLE",
		BaseRefName: "main",
		HeadRefName: "feature",
		HeadRefOid:  "abc123",
		Author: struct {
			Login string `json:"login"`
		}{Login: "bob"},
	}
	prData, _ := json.Marshal(prView)

	compare := ghCompare{AheadBy: 3, BehindBy: 1}
	cmpData, _ := json.Marshal(compare)

	client := NewTestClient("alice", fakeRunner(map[string]string{
		"pr view 42": string(prData),
		"api repos/":  string(cmpData),
	}))

	detail, err := client.GetPRDetail(context.Background(), "alice", "widget", 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if detail.Number != 42 {
		t.Errorf("Number = %d", detail.Number)
	}
	if detail.Title != "Add feature" {
		t.Errorf("Title = %q", detail.Title)
	}
	if !detail.Mergeable {
		t.Error("expected Mergeable=true for MERGEABLE state")
	}
	if detail.BehindBy != 3 {
		t.Errorf("BehindBy = %d, want 3 (AheadBy from compare)", detail.BehindBy)
	}
	if detail.Author.Login != "bob" {
		t.Errorf("Author = %q", detail.Author.Login)
	}
	if detail.BaseBranch != "main" {
		t.Errorf("BaseBranch = %q", detail.BaseBranch)
	}
}

func TestGetPRDetail_CompareAPIFailure(t *testing.T) {
	prView := ghPRView{
		Number:      7,
		Title:       "Fix bug",
		Mergeable:   "CONFLICTING",
		BaseRefName: "main",
		HeadRefName: "fix",
		HeadRefOid:  "def456",
	}
	prData, _ := json.Marshal(prView)

	// fakeRunner that succeeds for pr view but fails for api repos/
	client := NewTestClient("alice", func(ctx context.Context, args ...string) (string, error) {
		key := strings.Join(args, " ")
		if strings.Contains(key, "pr view") {
			return string(prData), nil
		}
		return "", fmt.Errorf("compare API error")
	})

	detail, err := client.GetPRDetail(context.Background(), "alice", "widget", 7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if detail.Mergeable {
		t.Error("expected Mergeable=false for CONFLICTING")
	}
	if detail.BehindBy != -1 {
		t.Errorf("BehindBy = %d, want -1 (compare API failed)", detail.BehindBy)
	}
}

func TestGetPRDetail_PRViewError(t *testing.T) {
	client := NewTestClient("alice", fakeErrorRunner("not found"))

	_, err := client.GetPRDetail(context.Background(), "alice", "widget", 999)
	if err == nil {
		t.Fatal("expected error")
	}
}
