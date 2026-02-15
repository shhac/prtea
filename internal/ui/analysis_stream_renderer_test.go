package ui

import (
	"testing"
)

func TestHealJSON(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string // expected to be valid JSON (or at least structurally closed)
	}{
		{
			name:  "already valid",
			input: `{"summary":"hello"}`,
			want:  `{"summary":"hello"}`,
		},
		{
			name:  "open string in value",
			input: `{"summary":"hel`,
			want:  `{"summary":"hel"}`,
		},
		{
			name:  "open object",
			input: `{"summary":"hello","risk":{"level":"med`,
			want:  `{"summary":"hello","risk":{"level":"med"}}`,
		},
		{
			name:  "open array",
			input: `{"items":["a","b`,
			want:  `{"items":["a","b"]}`,
		},
		{
			name:  "trailing comma in object",
			input: `{"a":"b",}`,
			want:  `{"a":"b"}`,
		},
		{
			name:  "trailing comma in array",
			input: `{"items":["a",]}`,
			want:  `{"items":["a"]}`,
		},
		{
			name:  "escaped quote in string",
			input: `{"msg":"say \"hi`,
			want:  `{"msg":"say \"hi"}`,
		},
		{
			name:  "nested objects and arrays",
			input: `{"a":{"b":[{"c":"d`,
			want:  `{"a":{"b":[{"c":"d"}]}}`,
		},
		{
			name:  "empty input",
			input: ``,
			want:  ``,
		},
		{
			name:  "just opening brace",
			input: `{`,
			want:  `{}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := healJSON(tt.input)
			if got != tt.want {
				t.Errorf("healJSON(%q)\n  got  %q\n  want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestTryParsePartialAnalysis(t *testing.T) {
	tests := []struct {
		name           string
		input          string
		wantNil        bool
		wantSummary    string
		wantRiskLevel  string
		wantFileCount  int
	}{
		{
			name:    "no JSON",
			input:   "just some text",
			wantNil: true,
		},
		{
			name:    "empty object",
			input:   "{}",
			wantNil: false, // parses but all fields empty
		},
		{
			name:        "complete summary",
			input:       `{"summary":"This PR adds auth"}`,
			wantSummary: "This PR adds auth",
		},
		{
			name:        "partial summary (mid-value)",
			input:       `{"summary":"This PR ad`,
			wantSummary: "This PR ad",
		},
		{
			name:          "summary + partial risk",
			input:         `{"summary":"hello","risk":{"level":"medium","reasoning":"It chan`,
			wantSummary:   "hello",
			wantRiskLevel: "medium",
		},
		{
			name:        "mid-key after complete field",
			input:       `{"summary":"hello","ri`,
			wantSummary: "hello",
		},
		{
			name:        "colon after key, no value yet",
			input:       `{"summary":"hello","risk":`,
			wantSummary: "hello",
		},
		{
			name:          "with file reviews",
			input:         `{"summary":"ok","risk":{"level":"low","reasoning":"safe"},"architectureImpact":{"hasImpact":false},"fileReviews":[{"file":"main.go","summary":"looks good","comments":[]}`,
			wantSummary:   "ok",
			wantRiskLevel: "low",
			wantFileCount: 1,
		},
		{
			name:          "with preamble text before JSON",
			input:         `Here is my analysis: {"summary":"found it","risk":{"level":"high","reasoning":"dangerous"}}`,
			wantSummary:   "found it",
			wantRiskLevel: "high",
		},
		{
			name:          "realistic partial mid-file-review",
			input:         `{"summary":"Adds logging","risk":{"level":"low","reasoning":"Minor change"},"architectureImpact":{"hasImpact":false,"description":"","affectedModules":[]},"fileReviews":[{"file":"cmd/main.go","summary":"Adds log import","comments":[{"line":5,"severity":"suggestion","comment":"Consider using structured log`,
			wantSummary:   "Adds logging",
			wantRiskLevel: "low",
			wantFileCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tryParsePartialAnalysis(tt.input)
			if tt.wantNil {
				if result != nil {
					t.Fatalf("expected nil, got %+v", result)
				}
				return
			}
			if result == nil {
				t.Fatal("expected non-nil result")
			}
			if tt.wantSummary != "" && result.Summary != tt.wantSummary {
				t.Errorf("summary: got %q, want %q", result.Summary, tt.wantSummary)
			}
			if tt.wantRiskLevel != "" && result.Risk.Level != tt.wantRiskLevel {
				t.Errorf("risk.level: got %q, want %q", result.Risk.Level, tt.wantRiskLevel)
			}
			if tt.wantFileCount > 0 && len(result.FileReviews) != tt.wantFileCount {
				t.Errorf("fileReviews count: got %d, want %d", len(result.FileReviews), tt.wantFileCount)
			}
		})
	}
}

func TestAnalysisStreamRenderer(t *testing.T) {
	r := &AnalysisStreamRenderer{}

	// Initially empty
	if r.HasContent() {
		t.Fatal("expected no content initially")
	}
	if v := r.View(80); v != "" {
		t.Fatalf("expected empty view, got %q", v)
	}

	// Force immediate parsing by setting interval to 0 (uses default 300ms)
	// We'll set parsedAt to zero time so the first append triggers parsing
	r.CheckpointInterval = 1 // 1ns — always triggers

	// Append a complete summary
	r.Append(`{"summary":"Test summary","risk":{"level":"low","reasoning":"Safe change"}`)

	if !r.HasContent() {
		t.Fatal("expected content after append")
	}

	view := r.View(80)
	if view == "" {
		t.Fatal("expected non-empty view after parsing")
	}

	// Should contain styled content, not raw JSON
	if contains(view, `"summary"`) {
		t.Error("view should not contain raw JSON key \"summary\"")
	}
	if !contains(view, "Test summary") {
		t.Error("view should contain the summary text")
	}
	if !contains(view, "LOW RISK") {
		t.Error("view should contain the risk badge")
	}

	// Reset should clear everything
	r.Reset()
	if r.HasContent() {
		t.Fatal("expected no content after reset")
	}
	if v := r.View(80); v != "" {
		t.Fatalf("expected empty view after reset, got %q", v)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
