package claude

import "time"

// AnalysisResult is the structured output from Claude's PR analysis.
type AnalysisResult struct {
	Summary            string             `json:"summary"`
	Risk               RiskAssessment     `json:"risk"`
	ArchitectureImpact ArchitectureImpact `json:"architectureImpact"`
	FileReviews        []FileReview       `json:"fileReviews"`
	TestCoverage       TestCoverage       `json:"testCoverage"`
	Suggestions        []Suggestion       `json:"suggestions"`
}

// RiskAssessment describes the overall risk level of the PR.
type RiskAssessment struct {
	Level     string `json:"level"`     // "low", "medium", "high", "critical"
	Reasoning string `json:"reasoning"`
}

// ArchitectureImpact describes whether the PR affects system architecture.
type ArchitectureImpact struct {
	HasImpact       bool     `json:"hasImpact"`
	Description     string   `json:"description"`
	AffectedModules []string `json:"affectedModules"`
}

// FileReview is Claude's review of a single changed file.
type FileReview struct {
	File     string          `json:"file"`
	Summary  string          `json:"summary"`
	Comments []ReviewComment `json:"comments"`
}

// ReviewComment is a single comment on a file, optionally tied to a line.
type ReviewComment struct {
	Line     int    `json:"line,omitempty"`
	Severity string `json:"severity"` // "critical", "warning", "suggestion", "praise"
	Comment  string `json:"comment"`
}

// TestCoverage summarizes test coverage assessment for the PR.
type TestCoverage struct {
	Assessment string   `json:"assessment"`
	Gaps       []string `json:"gaps"`
}

// Suggestion is an actionable improvement suggestion.
type Suggestion struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	File        string `json:"file,omitempty"`
}

// ReviewAnalysis is the structured output from Claude's AI review generation.
// It produces a GitHub-ready review with inline comments.
type ReviewAnalysis struct {
	Action   string                `json:"action"`   // "approve", "comment", "request_changes"
	Body     string                `json:"body"`     // overall review comment
	Comments []InlineReviewComment `json:"comments"` // inline comments on specific lines
}

// InlineReviewComment is a single inline comment targeting a specific file/line.
type InlineReviewComment struct {
	Path string `json:"path"`           // relative file path
	Line int    `json:"line"`           // file line number (new side)
	Side string `json:"side,omitempty"` // "RIGHT" (default) or "LEFT"
	Body string `json:"body"`           // comment text
}

// ChatMessage represents a single message in a chat conversation.
type ChatMessage struct {
	Role    string `json:"role"` // "user", "assistant"
	Content string `json:"content"`
}

// ChatSession holds the conversation history for a PR chat.
type ChatSession struct {
	Messages  []ChatMessage
	PRContext string
}

// CachedAnalysis wraps an analysis result with cache metadata.
type CachedAnalysis struct {
	DiffContentHash string          `json:"diffContentHash"`
	AnalyzedAt time.Time       `json:"analyzedAt"`
	Result     *AnalysisResult `json:"result"`
}

// ProgressEvent reports analysis progress back to the TUI.
type ProgressEvent struct {
	Type    string // "tool_use", "thinking", "text"
	Message string
}

// ProgressFunc is a callback for receiving progress updates during analysis.
type ProgressFunc func(event ProgressEvent)

// StreamEvent represents a single event from Claude's stream-json output.
type StreamEvent struct {
	Type    string      `json:"type"`
	Result  interface{} `json:"result,omitempty"`
	CostUSD float64     `json:"cost_usd,omitempty"`
	Message *struct {
		Content []ContentBlock `json:"content,omitempty"`
	} `json:"message,omitempty"`
	Content []ContentBlock `json:"content,omitempty"`

	// Event holds the nested API event when type == "stream_event"
	// (emitted with --include-partial-messages). Contains content_block_delta
	// events with text_delta for token-level streaming.
	Event *StreamInnerEvent `json:"event,omitempty"`
}

// StreamInnerEvent is the nested event inside a stream_event envelope.
type StreamInnerEvent struct {
	Type  string       `json:"type"`
	Delta *StreamDelta `json:"delta,omitempty"`
}

// StreamDelta carries incremental content from content_block_delta events.
type StreamDelta struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// ContentBlock is a block within a stream event message.
type ContentBlock struct {
	Type      string      `json:"type"`
	Name      string      `json:"name,omitempty"`
	Text      string      `json:"text,omitempty"`
	Input     interface{} `json:"input,omitempty"`
	ToolUseID string      `json:"tool_use_id,omitempty"`
}
