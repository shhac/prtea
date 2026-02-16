package ui

import (
	"testing"

	"github.com/shhac/prtea/internal/claude"
)

func TestAnalysisTab_SetLoading(t *testing.T) {
	tab := &AnalysisTabModel{}
	tab.result = &claude.AnalysisResult{Summary: "old"}
	tab.error = "old error"

	tab.SetLoading()

	if !tab.loading {
		t.Error("expected loading=true")
	}
	if tab.result != nil {
		t.Error("result should be nil")
	}
	if tab.error != "" {
		t.Errorf("error = %q", tab.error)
	}
}

func TestAnalysisTab_SetResult(t *testing.T) {
	tab := &AnalysisTabModel{}
	tab.SetLoading()

	result := &claude.AnalysisResult{
		Summary: "Adds helper function",
		Risk:    claude.RiskAssessment{Level: "low"},
	}
	tab.SetResult(result)

	if tab.loading {
		t.Error("loading should be false")
	}
	if tab.result != result {
		t.Error("result not set correctly")
	}
	if tab.error != "" {
		t.Errorf("error = %q", tab.error)
	}
}

func TestAnalysisTab_SetError(t *testing.T) {
	tab := &AnalysisTabModel{}
	tab.SetLoading()
	tab.SetError("analysis failed")

	if tab.loading {
		t.Error("loading should be false")
	}
	if tab.result != nil {
		t.Error("result should be nil")
	}
	if tab.error != "analysis failed" {
		t.Errorf("error = %q", tab.error)
	}
}

func TestAnalysisTab_AppendStreamChunk(t *testing.T) {
	tab := &AnalysisTabModel{}
	tab.SetLoading()

	tab.AppendStreamChunk(`{"summary":"partial`)
	if !tab.stream.HasContent() {
		t.Error("stream should have content after append")
	}

	tab.AppendStreamChunk(`","risk":{"level":"low"}}`)
	if !tab.stream.HasContent() {
		t.Error("stream should still have content")
	}
}

func TestAnalysisTab_CacheInvalidation(t *testing.T) {
	tab := &AnalysisTabModel{}
	tab.cache = "cached"

	tab.SetLoading()
	if tab.cache != "" {
		t.Error("SetLoading should clear cache")
	}

	tab.cache = "cached"
	tab.SetResult(&claude.AnalysisResult{})
	if tab.cache != "" {
		t.Error("SetResult should clear cache")
	}

	tab.cache = "cached"
	tab.SetError("err")
	if tab.cache != "" {
		t.Error("SetError should clear cache")
	}

	tab.cache = "cached"
	tab.AppendStreamChunk("x")
	if tab.cache != "" {
		t.Error("AppendStreamChunk should clear cache")
	}
}

func TestAnalysisTab_StateTransitions(t *testing.T) {
	tab := &AnalysisTabModel{}

	// Loading → Result
	tab.SetLoading()
	if !tab.loading {
		t.Error("expected loading")
	}
	tab.SetResult(&claude.AnalysisResult{Summary: "done"})
	if tab.loading {
		t.Error("loading should be cleared")
	}
	if tab.result.Summary != "done" {
		t.Errorf("Summary = %q", tab.result.Summary)
	}

	// Loading → Error
	tab.SetLoading()
	tab.SetError("timeout")
	if tab.loading {
		t.Error("loading should be cleared")
	}
	if tab.error != "timeout" {
		t.Errorf("error = %q", tab.error)
	}
	if tab.result != nil {
		t.Error("result should be nil after error")
	}
}

func TestRiskLevelColor(t *testing.T) {
	tests := []struct {
		level    string
		notEmpty bool
	}{
		{"low", true},
		{"medium", true},
		{"high", true},
		{"critical", true},
		{"unknown", true},
		{"", true},
	}
	for _, tt := range tests {
		t.Run(tt.level, func(t *testing.T) {
			color := riskLevelColor(tt.level)
			if string(color) == "" {
				t.Error("expected non-empty color")
			}
		})
	}

	// Verify specific colors differ
	low := riskLevelColor("low")
	high := riskLevelColor("high")
	if low == high {
		t.Error("low and high risk should have different colors")
	}
}
