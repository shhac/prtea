package github

import "testing"

func TestComputeOverallStatus(t *testing.T) {
	tests := []struct {
		name   string
		checks []CICheck
		want   string
	}{
		{
			name:   "no checks returns pending",
			checks: nil,
			want:   "pending",
		},
		{
			name: "all passing",
			checks: []CICheck{
				{Name: "lint", Status: "completed", Conclusion: "success"},
				{Name: "test", Status: "completed", Conclusion: "success"},
			},
			want: "passing",
		},
		{
			name: "all failing",
			checks: []CICheck{
				{Name: "lint", Status: "completed", Conclusion: "failure"},
				{Name: "test", Status: "completed", Conclusion: "failure"},
			},
			want: "failing",
		},
		{
			name: "mixed success and failure",
			checks: []CICheck{
				{Name: "lint", Status: "completed", Conclusion: "success"},
				{Name: "test", Status: "completed", Conclusion: "failure"},
			},
			want: "mixed",
		},
		{
			name: "pending overrides everything",
			checks: []CICheck{
				{Name: "lint", Status: "completed", Conclusion: "success"},
				{Name: "test", Status: "in_progress", Conclusion: ""},
			},
			want: "pending",
		},
		{
			name: "queued is pending",
			checks: []CICheck{
				{Name: "deploy", Status: "queued", Conclusion: ""},
			},
			want: "pending",
		},
		{
			name: "skipped counts as success",
			checks: []CICheck{
				{Name: "optional", Status: "completed", Conclusion: "skipped"},
			},
			want: "passing",
		},
		{
			name: "neutral counts as success",
			checks: []CICheck{
				{Name: "info", Status: "completed", Conclusion: "neutral"},
			},
			want: "passing",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := computeOverallStatus(tt.checks); got != tt.want {
				t.Errorf("computeOverallStatus() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNormalizeStatus(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"IN_PROGRESS", "in_progress"},
		{"COMPLETED", "completed"},
		{"QUEUED", "queued"},
		{"in_progress", "in_progress"},
		{"Something_Else", "something_else"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := normalizeStatus(tt.input); got != tt.want {
				t.Errorf("normalizeStatus(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestNormalizeConclusionStr(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"SUCCESS", "success"},
		{"FAILURE", "failure"},
		{"NEUTRAL", "neutral"},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := normalizeConclusionStr(tt.input); got != tt.want {
				t.Errorf("normalizeConclusionStr(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
