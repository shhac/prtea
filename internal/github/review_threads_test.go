package github

import (
	"context"
	"testing"
)

func TestGetReviewThreadResolution(t *testing.T) {
	response := `{
		"data": {
			"repository": {
				"pullRequest": {
					"reviewThreads": {
						"nodes": [
							{
								"isResolved": true,
								"comments": {"nodes": [{"databaseId": 100}, {"databaseId": 101}]}
							},
							{
								"isResolved": false,
								"comments": {"nodes": [{"databaseId": 200}]}
							}
						]
					}
				}
			}
		}
	}`

	client := NewTestClient("alice", fakeRunner(map[string]string{
		"api graphql": response,
	}))

	resolution, err := client.GetReviewThreadResolution(context.Background(), "alice", "widget", 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cases := []struct {
		id   int64
		want bool
	}{
		{100, true},
		{101, true},
		{200, false},
	}
	for _, tc := range cases {
		if got := resolution[tc.id]; got != tc.want {
			t.Errorf("resolution[%d] = %v, want %v", tc.id, got, tc.want)
		}
	}
	if _, ok := resolution[999]; ok {
		t.Error("unknown comment ID should be absent from the map")
	}
}

func TestGetReviewThreadResolution_Error(t *testing.T) {
	client := NewTestClient("alice", fakeErrorRunner("graphql unavailable"))
	if _, err := client.GetReviewThreadResolution(context.Background(), "alice", "widget", 42); err == nil {
		t.Fatal("expected error")
	}
}
