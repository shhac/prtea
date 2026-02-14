package github

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// fakeRunner returns a CommandRunner that responds with canned output based on args.
func fakeRunner(responses map[string]string) CommandRunner {
	return func(ctx context.Context, args ...string) (string, error) {
		key := strings.Join(args, " ")
		for pattern, response := range responses {
			if strings.Contains(key, pattern) {
				return response, nil
			}
		}
		return "", fmt.Errorf("unexpected command: gh %s", key)
	}
}

// fakeErrorRunner returns a CommandRunner that always errors.
func fakeErrorRunner(errMsg string) CommandRunner {
	return func(ctx context.Context, args ...string) (string, error) {
		return "", fmt.Errorf("%s", errMsg)
	}
}

func TestGetPRsForReview(t *testing.T) {
	searchResult := []ghSearchPR{
		{
			Number: 42,
			Title:  "Add frobnicate function",
			URL:    "https://github.com/alice/widget-factory/pull/42",
			Author: struct {
				Login string `json:"login"`
			}{Login: "bob"},
			Repository: struct {
				Name          string `json:"name"`
				NameWithOwner string `json:"nameWithOwner"`
			}{Name: "widget-factory", NameWithOwner: "alice/widget-factory"},
		},
	}
	data, _ := json.Marshal(searchResult)

	client := NewTestClient("alice", fakeRunner(map[string]string{
		"search prs": string(data),
	}))

	items, err := client.GetPRsForReview(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
	if items[0].Number != 42 {
		t.Errorf("Number = %d, want 42", items[0].Number)
	}
	if items[0].Author.Login != "bob" {
		t.Errorf("Author = %q, want bob", items[0].Author.Login)
	}
}

func TestGetPRsForReview_Error(t *testing.T) {
	client := NewTestClient("alice", fakeErrorRunner("gh search prs failed: rate limit"))

	_, err := client.GetPRsForReview(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "search PRs for review") {
		t.Errorf("error = %q, expected to mention search PRs", err.Error())
	}
}

func TestGetPRFiles(t *testing.T) {
	files := []ghFile{
		{Filename: "main.go", Status: "modified", Additions: 10, Deletions: 2, Patch: "@@ -1,3 +1,4 @@\n+import \"fmt\""},
		{Filename: "README.md", Status: "modified", Additions: 1, Deletions: 0, Patch: "@@ -1 +1,2 @@\n+New line"},
	}
	data, _ := json.Marshal(files)

	client := NewTestClient("alice", fakeRunner(map[string]string{
		"api repos/alice/widget-factory/pulls/42/files": string(data),
	}))

	result, err := client.GetPRFiles(context.Background(), "alice", "widget-factory", 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("got %d files, want 2", len(result))
	}
	if result[0].Filename != "main.go" {
		t.Errorf("Filename = %q, want main.go", result[0].Filename)
	}
	if result[0].Additions != 10 {
		t.Errorf("Additions = %d, want 10", result[0].Additions)
	}
}

func TestGetCIStatus(t *testing.T) {
	checks := ghPRChecks{
		StatusCheckRollup: []ghCheckRun{
			{Name: "lint", Status: "COMPLETED", Conclusion: "SUCCESS"},
			{Name: "test", Status: "COMPLETED", Conclusion: "FAILURE"},
			{Name: "deploy", Status: "IN_PROGRESS", Conclusion: ""},
		},
	}
	data, _ := json.Marshal(checks)

	client := NewTestClient("alice", fakeRunner(map[string]string{
		"pr view 42": string(data),
	}))

	status, err := client.GetCIStatus(context.Background(), "alice", "widget-factory", "", 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status.TotalCount != 3 {
		t.Errorf("TotalCount = %d, want 3", status.TotalCount)
	}
	// Has pending (in_progress), so overall should be pending
	if status.OverallStatus != "pending" {
		t.Errorf("OverallStatus = %q, want pending", status.OverallStatus)
	}
}

func TestGetReviews(t *testing.T) {
	reviews := ghPRReviews{
		LatestReviews: []ghReview{
			{Author: struct {
				Login string `json:"login"`
			}{Login: "charlie"}, State: "APPROVED"},
		},
		ReviewDecision: "APPROVED",
		ReviewRequests: []ghReviewRequest{
			{TypeName: "User", Login: "bob"},
		},
	}
	data, _ := json.Marshal(reviews)

	client := NewTestClient("alice", fakeRunner(map[string]string{
		"pr view 42": string(data),
	}))

	summary, err := client.GetReviews(context.Background(), "alice", "widget-factory", 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if summary.ReviewDecision != "APPROVED" {
		t.Errorf("ReviewDecision = %q, want APPROVED", summary.ReviewDecision)
	}
	if len(summary.Approved) != 1 {
		t.Errorf("Approved count = %d, want 1", len(summary.Approved))
	}
	if len(summary.PendingReviewers) != 1 {
		t.Errorf("PendingReviewers count = %d, want 1", len(summary.PendingReviewers))
	}
	if summary.PendingReviewers[0].Login != "bob" {
		t.Errorf("PendingReviewers[0].Login = %q, want bob", summary.PendingReviewers[0].Login)
	}
}

func TestApprovePR(t *testing.T) {
	client := NewTestClient("alice", fakeRunner(map[string]string{
		"pr review": "",
	}))

	err := client.ApprovePR(context.Background(), "alice", "widget-factory", 42, "LGTM")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestApprovePR_Error(t *testing.T) {
	client := NewTestClient("alice", fakeErrorRunner("permission denied"))

	err := client.ApprovePR(context.Background(), "alice", "widget-factory", 42, "")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestClosePR(t *testing.T) {
	client := NewTestClient("alice", fakeRunner(map[string]string{
		"pr close": "",
	}))

	err := client.ClosePR(context.Background(), "bob", "test-project", 7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPostComment(t *testing.T) {
	client := NewTestClient("alice", fakeRunner(map[string]string{
		"pr comment": "",
	}))

	err := client.PostComment(context.Background(), "alice", "widget-factory", 42, "Nice work!")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGetComments(t *testing.T) {
	comments := ghPRComments{
		Comments: []ghComment{
			{
				ID: "c1",
				Author: struct {
					Login string `json:"login"`
				}{Login: "charlie"},
				Body: "Looks good to me!",
			},
		},
	}
	data, _ := json.Marshal(comments)

	client := NewTestClient("alice", fakeRunner(map[string]string{
		"pr view 42": string(data),
	}))

	result, err := client.GetComments(context.Background(), "alice", "widget-factory", 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("got %d comments, want 1", len(result))
	}
	if result[0].Author.Login != "charlie" {
		t.Errorf("Author = %q, want charlie", result[0].Author.Login)
	}
	if result[0].Body != "Looks good to me!" {
		t.Errorf("Body = %q", result[0].Body)
	}
}
