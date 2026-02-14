package claude

import (
	"encoding/json"
	"testing"
)

func TestExtractAnalysisResult_DirectJSON(t *testing.T) {
	result := &AnalysisResult{
		Summary: "Adds frobnicate function to widget-factory",
		Risk:    RiskAssessment{Level: "low", Reasoning: "Small utility addition"},
	}
	data, _ := json.Marshal(result)

	event := &StreamEvent{
		Type:   "result",
		Result: string(data),
	}

	got, err := extractAnalysisResult(event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Summary != result.Summary {
		t.Errorf("Summary = %q, want %q", got.Summary, result.Summary)
	}
	if got.Risk.Level != "low" {
		t.Errorf("Risk.Level = %q, want %q", got.Risk.Level, "low")
	}
}

func TestExtractAnalysisResult_WrappedJSON(t *testing.T) {
	// Simulates Claude output with surrounding text
	jsonStr := `{"summary":"Fix widget alignment","risk":{"level":"medium","reasoning":"UI change"},"architectureImpact":{"hasImpact":false},"fileReviews":[],"testCoverage":{"assessment":"adequate"},"suggestions":[]}`
	wrapped := "Here is my analysis:\n\n" + jsonStr + "\n\nLet me know if you have questions."

	event := &StreamEvent{
		Type:   "result",
		Result: wrapped,
	}

	got, err := extractAnalysisResult(event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Summary != "Fix widget alignment" {
		t.Errorf("Summary = %q", got.Summary)
	}
	if got.Risk.Level != "medium" {
		t.Errorf("Risk.Level = %q", got.Risk.Level)
	}
}

func TestExtractAnalysisResult_NoJSON(t *testing.T) {
	event := &StreamEvent{
		Type:   "result",
		Result: "no json here at all",
	}
	_, err := extractAnalysisResult(event)
	if err == nil {
		t.Fatal("expected error for non-JSON result")
	}
}

func TestExtractAnalysisResult_NonStringResult(t *testing.T) {
	// Result comes as a map (already parsed JSON)
	result := map[string]interface{}{
		"summary": "Adds feature",
		"risk":    map[string]interface{}{"level": "low", "reasoning": "trivial"},
		"architectureImpact": map[string]interface{}{"hasImpact": false},
		"fileReviews":        []interface{}{},
		"testCoverage":       map[string]interface{}{"assessment": "ok"},
		"suggestions":        []interface{}{},
	}
	event := &StreamEvent{
		Type:   "result",
		Result: result,
	}

	got, err := extractAnalysisResult(event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Summary != "Adds feature" {
		t.Errorf("Summary = %q, want %q", got.Summary, "Adds feature")
	}
}

func TestFilterEnv(t *testing.T) {
	env := []string{
		"HOME=/home/alice",
		"ANTHROPIC_API_KEY=sk-secret-123",
		"PATH=/usr/bin",
		"ANTHROPIC_API_KEY_EXTRA=other",
	}
	got := filterEnv(env, "ANTHROPIC_API_KEY")
	if len(got) != 3 {
		t.Fatalf("got %d entries, want 3", len(got))
	}
	for _, e := range got {
		if e == "ANTHROPIC_API_KEY=sk-secret-123" {
			t.Error("ANTHROPIC_API_KEY should have been removed")
		}
	}
}

func TestFilterEnv_NoMatch(t *testing.T) {
	env := []string{"HOME=/home/bob", "PATH=/usr/bin"}
	got := filterEnv(env, "MISSING_KEY")
	if len(got) != 2 {
		t.Errorf("got %d entries, want 2", len(got))
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{"short string", "hello", 10, "hello"},
		{"exact length", "hello", 5, "hello"},
		{"truncated", "hello world", 5, "hello..."},
		{"empty", "", 5, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := truncate(tt.input, tt.maxLen); got != tt.want {
				t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
			}
		})
	}
}
