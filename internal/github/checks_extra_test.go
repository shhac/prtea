package github

import "testing"

func TestParseWorkflowRunID(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want int64
	}{
		{"standard URL", "https://github.com/owner/repo/actions/runs/12345", 12345},
		{"URL with job", "https://github.com/owner/repo/actions/runs/99999/job/67890", 99999},
		{"external CI", "https://circleci.com/gh/owner/repo/123", 0},
		{"empty string", "", 0},
		{"no match", "https://github.com/owner/repo/pull/42", 0},
		{"only path fragment", "/actions/runs/777", 777},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseWorkflowRunID(tt.url)
			if got != tt.want {
				t.Errorf("parseWorkflowRunID(%q) = %d, want %d", tt.url, got, tt.want)
			}
		})
	}
}

func TestFailedRunIDs(t *testing.T) {
	t.Run("nil receiver", func(t *testing.T) {
		var s *CIStatus
		ids := s.FailedRunIDs()
		if ids != nil {
			t.Errorf("got %v, want nil", ids)
		}
	})

	t.Run("no checks", func(t *testing.T) {
		s := &CIStatus{Checks: []CICheck{}}
		ids := s.FailedRunIDs()
		if len(ids) != 0 {
			t.Errorf("got %v, want empty", ids)
		}
	})

	t.Run("all passing", func(t *testing.T) {
		s := &CIStatus{Checks: []CICheck{
			{Status: "completed", Conclusion: "success", WorkflowRunID: 100},
			{Status: "completed", Conclusion: "success", WorkflowRunID: 200},
		}}
		ids := s.FailedRunIDs()
		if len(ids) != 0 {
			t.Errorf("got %v, want empty", ids)
		}
	})

	t.Run("failed checks deduplicated", func(t *testing.T) {
		s := &CIStatus{Checks: []CICheck{
			{Status: "completed", Conclusion: "failure", WorkflowRunID: 100},
			{Status: "completed", Conclusion: "failure", WorkflowRunID: 100}, // duplicate
			{Status: "completed", Conclusion: "failure", WorkflowRunID: 200},
			{Status: "completed", Conclusion: "success", WorkflowRunID: 300},
		}}
		ids := s.FailedRunIDs()
		if len(ids) != 2 {
			t.Fatalf("got %d IDs, want 2", len(ids))
		}
	})

	t.Run("external CI excluded", func(t *testing.T) {
		s := &CIStatus{Checks: []CICheck{
			{Status: "completed", Conclusion: "failure", WorkflowRunID: 0}, // external CI
			{Status: "completed", Conclusion: "failure", WorkflowRunID: 100},
		}}
		ids := s.FailedRunIDs()
		if len(ids) != 1 {
			t.Fatalf("got %d IDs, want 1", len(ids))
		}
		if ids[0] != 100 {
			t.Errorf("ids[0] = %d, want 100", ids[0])
		}
	})
}
