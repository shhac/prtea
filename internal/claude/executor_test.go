package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"
)

// mockExecutor implements CommandExecutor for testing.
type mockExecutor struct {
	stdout  string // lines to write to stdout
	stderr  string // lines to write to stderr
	waitErr error  // error returned by Process.Wait

	// captured invocation (for assertions)
	lastArgs []string
	lastOpts ExecOptions
}

func (m *mockExecutor) Start(_ context.Context, args []string, opts ExecOptions) (*Process, error) {
	m.lastArgs = args
	m.lastOpts = opts
	return &Process{
		Stdout: io.NopCloser(strings.NewReader(m.stdout)),
		Stderr: io.NopCloser(strings.NewReader(m.stderr)),
		Wait:   func() error { return m.waitErr },
	}, nil
}

// mockStartError implements CommandExecutor but always fails to start.
type mockStartError struct{ err error }

func (m *mockStartError) Start(_ context.Context, _ []string, _ ExecOptions) (*Process, error) {
	return nil, m.err
}

// resultEvent builds a stream-json line for a result event carrying the given JSON result.
func resultEvent(result interface{}) string {
	data, _ := json.Marshal(map[string]interface{}{
		"type":     "result",
		"result":   result,
		"cost_usd": 0.01,
	})
	return string(data)
}

// textDeltaEvent builds a stream-json line for a token-level text delta.
func textDeltaEvent(text string) string {
	data, _ := json.Marshal(map[string]interface{}{
		"type": "stream_event",
		"event": map[string]interface{}{
			"type": "content_block_delta",
			"delta": map[string]interface{}{
				"type": "text_delta",
				"text": text,
			},
		},
	})
	return string(data)
}

// assistantEvent builds a stream-json line for an assistant turn with text blocks.
func assistantEvent(blocks ...string) string {
	content := make([]map[string]string, len(blocks))
	for i, b := range blocks {
		content[i] = map[string]string{"type": "text", "text": b}
	}
	data, _ := json.Marshal(map[string]interface{}{
		"type":    "assistant",
		"message": map[string]interface{}{"content": content},
	})
	return string(data)
}

// toolUseEvent builds a stream-json line for an assistant turn with a tool_use block.
func toolUseEvent(toolName string) string {
	data, _ := json.Marshal(map[string]interface{}{
		"type": "assistant",
		"message": map[string]interface{}{
			"content": []map[string]string{{"type": "tool_use", "name": toolName}},
		},
	})
	return string(data)
}

// --- runCLI tests ---

func TestRunCLI_Success(t *testing.T) {
	analysisJSON := `{"summary":"test PR","risk":{"level":"low","reasoning":"trivial"}}`
	mock := &mockExecutor{
		stdout: resultEvent(analysisJSON) + "\n",
	}

	event, err := runCLI(context.Background(), mock, []string{"-p", "test"}, ExecOptions{}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.Type != "result" {
		t.Errorf("event.Type = %q, want result", event.Type)
	}
}

func TestRunCLI_VisitorCalled(t *testing.T) {
	mock := &mockExecutor{
		stdout: assistantEvent("hello") + "\n" + resultEvent("done") + "\n",
	}

	var visited []string
	visitor := func(event *StreamEvent) { visited = append(visited, event.Type) }

	_, err := runCLI(context.Background(), mock, nil, ExecOptions{}, visitor)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(visited) != 2 {
		t.Fatalf("visitor called %d times, want 2", len(visited))
	}
	if visited[0] != "assistant" || visited[1] != "result" {
		t.Errorf("visited = %v, want [assistant result]", visited)
	}
}

func TestRunCLI_SkipsMalformedLines(t *testing.T) {
	mock := &mockExecutor{
		stdout: "not json\n" + resultEvent("ok") + "\n",
	}

	event, err := runCLI(context.Background(), mock, nil, ExecOptions{}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.Type != "result" {
		t.Errorf("event.Type = %q, want result", event.Type)
	}
}

func TestRunCLI_SkipsBlankLines(t *testing.T) {
	mock := &mockExecutor{
		stdout: "\n  \n" + resultEvent("ok") + "\n",
	}

	event, err := runCLI(context.Background(), mock, nil, ExecOptions{}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event == nil {
		t.Fatal("expected result event")
	}
}

func TestRunCLI_NoResultEvent(t *testing.T) {
	mock := &mockExecutor{
		stdout: assistantEvent("hello") + "\n",
	}

	_, err := runCLI(context.Background(), mock, nil, ExecOptions{}, nil)
	if err == nil {
		t.Fatal("expected error for missing result event")
	}
	if !strings.Contains(err.Error(), "no result event") {
		t.Errorf("error = %q, want mention of 'no result event'", err.Error())
	}
}

func TestRunCLI_ProcessError(t *testing.T) {
	mock := &mockExecutor{
		stdout:  "",
		stderr:  "something went wrong\n",
		waitErr: fmt.Errorf("exit status 1"),
	}

	_, err := runCLI(context.Background(), mock, nil, ExecOptions{}, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "something went wrong") {
		t.Errorf("error = %q, expected to contain stderr", err.Error())
	}
}

func TestRunCLI_StartError(t *testing.T) {
	mock := &mockStartError{err: fmt.Errorf("no such binary")}

	_, err := runCLI(context.Background(), mock, nil, ExecOptions{}, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "no such binary") {
		t.Errorf("error = %q", err.Error())
	}
}

// --- Visitor tests ---

func TestStreamDeltaVisitor(t *testing.T) {
	var chunks []string
	visitor := streamDeltaVisitor(func(s string) { chunks = append(chunks, s) })

	// Correct stream_event with text_delta
	visitor(&StreamEvent{
		Type: "stream_event",
		Event: &StreamInnerEvent{
			Type:  "content_block_delta",
			Delta: &StreamDelta{Type: "text_delta", Text: "hello"},
		},
	})
	// Wrong type — should be ignored
	visitor(&StreamEvent{Type: "assistant"})
	// stream_event without delta
	visitor(&StreamEvent{
		Type:  "stream_event",
		Event: &StreamInnerEvent{Type: "content_block_start"},
	})
	// Empty text — should be ignored
	visitor(&StreamEvent{
		Type: "stream_event",
		Event: &StreamInnerEvent{
			Type:  "content_block_delta",
			Delta: &StreamDelta{Type: "text_delta", Text: ""},
		},
	})
	// Another valid delta
	visitor(&StreamEvent{
		Type: "stream_event",
		Event: &StreamInnerEvent{
			Type:  "content_block_delta",
			Delta: &StreamDelta{Type: "text_delta", Text: " world"},
		},
	})

	if len(chunks) != 2 {
		t.Fatalf("got %d chunks, want 2", len(chunks))
	}
	if chunks[0] != "hello" || chunks[1] != " world" {
		t.Errorf("chunks = %v", chunks)
	}
}

func TestProgressVisitor_NilFunc(t *testing.T) {
	visitor := progressVisitor(nil)
	if visitor != nil {
		t.Error("progressVisitor(nil) should return nil")
	}
}

func TestReportProgress(t *testing.T) {
	var events []ProgressEvent
	onProgress := func(e ProgressEvent) { events = append(events, e) }

	// Tool use event
	reportProgress(&StreamEvent{
		Type: "assistant",
		Message: &struct {
			Content []ContentBlock `json:"content,omitempty"`
		}{
			Content: []ContentBlock{
				{Type: "tool_use", Name: "Read"},
				{Type: "text", Text: "Analyzing code..."},
			},
		},
	}, onProgress)

	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}
	if events[0].Type != "tool_use" || !strings.Contains(events[0].Message, "Read") {
		t.Errorf("event[0] = %+v", events[0])
	}
	if events[1].Type != "text" {
		t.Errorf("event[1].Type = %q, want text", events[1].Type)
	}
}

func TestReportProgress_IgnoresNonAssistant(t *testing.T) {
	var events []ProgressEvent
	onProgress := func(e ProgressEvent) { events = append(events, e) }

	reportProgress(&StreamEvent{Type: "result"}, onProgress)

	if len(events) != 0 {
		t.Errorf("got %d events for non-assistant type, want 0", len(events))
	}
}

// --- Analyzer integration tests using mock ---

func TestAnalyzer_AnalyzeDiff(t *testing.T) {
	analysisResult := AnalysisResult{
		Summary: "Adds a new helper function",
		Risk:    RiskAssessment{Level: "low", Reasoning: "Small change"},
	}
	resultJSON, _ := json.Marshal(analysisResult)

	mock := &mockExecutor{
		stdout: resultEvent(string(resultJSON)) + "\n",
	}

	analyzer := NewAnalyzer(mock, 30*time.Second, "", 0)

	result, err := analyzer.AnalyzeDiff(context.Background(), AnalyzeDiffInput{
		Owner:       "alice",
		Repo:        "widget",
		PRNumber:    42,
		PRTitle:     "Add helper",
		DiffContent: "+func helper() {}",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Summary != "Adds a new helper function" {
		t.Errorf("Summary = %q", result.Summary)
	}
	if result.Risk.Level != "low" {
		t.Errorf("Risk.Level = %q", result.Risk.Level)
	}

	// Verify CLI flags
	args := strings.Join(mock.lastArgs, " ")
	if !strings.Contains(args, "--output-format stream-json") {
		t.Error("missing --output-format stream-json flag")
	}
	if !strings.Contains(args, "--max-turns 1") {
		t.Error("missing --max-turns 1 flag")
	}
}

func TestAnalyzer_AnalyzeDiffStream(t *testing.T) {
	analysisResult := AnalysisResult{
		Summary: "Streaming result",
		Risk:    RiskAssessment{Level: "medium", Reasoning: "test"},
	}
	resultJSON, _ := json.Marshal(analysisResult)

	mock := &mockExecutor{
		stdout: strings.Join([]string{
			textDeltaEvent("chunk1"),
			textDeltaEvent("chunk2"),
			resultEvent(string(resultJSON)),
		}, "\n") + "\n",
	}

	analyzer := NewAnalyzer(mock, 30*time.Second, "", 0)

	var chunks []string
	result, err := analyzer.AnalyzeDiffStream(context.Background(), AnalyzeDiffInput{
		Owner:       "alice",
		Repo:        "widget",
		PRNumber:    42,
		DiffContent: "+new line",
	}, func(s string) { chunks = append(chunks, s) })

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Summary != "Streaming result" {
		t.Errorf("Summary = %q", result.Summary)
	}
	if len(chunks) != 2 {
		t.Fatalf("got %d chunks, want 2", len(chunks))
	}
	if chunks[0] != "chunk1" || chunks[1] != "chunk2" {
		t.Errorf("chunks = %v", chunks)
	}

	// Verify streaming-specific flags
	args := strings.Join(mock.lastArgs, " ")
	if !strings.Contains(args, "--include-partial-messages") {
		t.Error("missing --include-partial-messages flag")
	}
}

func TestAnalyzer_AnalyzeForReview(t *testing.T) {
	reviewResult := ReviewAnalysis{
		Action: "comment",
		Body:   "Looks good overall, minor suggestions",
		Comments: []InlineReviewComment{
			{Path: "main.go", Line: 10, Body: "Consider error handling here"},
		},
	}
	resultJSON, _ := json.Marshal(reviewResult)

	mock := &mockExecutor{
		stdout: resultEvent(string(resultJSON)) + "\n",
	}

	analyzer := NewAnalyzer(mock, 30*time.Second, "", 0)

	result, err := analyzer.AnalyzeForReview(context.Background(), ReviewInput{
		Owner:       "alice",
		Repo:        "widget",
		PRNumber:    42,
		DiffContent: "+new line",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Action != "comment" {
		t.Errorf("Action = %q", result.Action)
	}
	if len(result.Comments) != 1 {
		t.Fatalf("got %d comments, want 1", len(result.Comments))
	}
	if result.Comments[0].Path != "main.go" {
		t.Errorf("Comment.Path = %q", result.Comments[0].Path)
	}
}

func TestAnalyzer_AnalyzeDiff_Error(t *testing.T) {
	mock := &mockExecutor{
		stderr:  "timeout\n",
		waitErr: fmt.Errorf("exit status 1"),
	}

	analyzer := NewAnalyzer(mock, 30*time.Second, "", 0)

	_, err := analyzer.AnalyzeDiff(context.Background(), AnalyzeDiffInput{
		DiffContent: "+line",
	}, nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestAnalyzer_AnalyzeDiff_WithProgress(t *testing.T) {
	analysisResult := AnalysisResult{Summary: "test"}
	resultJSON, _ := json.Marshal(analysisResult)

	mock := &mockExecutor{
		stdout: strings.Join([]string{
			toolUseEvent("Read"),
			assistantEvent("Reading file..."),
			resultEvent(string(resultJSON)),
		}, "\n") + "\n",
	}

	analyzer := NewAnalyzer(mock, 30*time.Second, "", 0)

	var progress []ProgressEvent
	_, err := analyzer.AnalyzeDiff(context.Background(), AnalyzeDiffInput{
		DiffContent: "+line",
	}, func(e ProgressEvent) { progress = append(progress, e) })

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(progress) < 1 {
		t.Fatal("expected progress events")
	}
}

// --- ChatService.ChatStream integration test ---

func TestChatService_ChatStream_Success(t *testing.T) {
	resultJSON := "Here is my response about the PR"

	mock := &mockExecutor{
		stdout: strings.Join([]string{
			textDeltaEvent("Here is "),
			textDeltaEvent("my response"),
			textDeltaEvent(" about the PR"),
			resultEvent(resultJSON),
		}, "\n") + "\n",
	}

	store := NewChatStore(t.TempDir())
	svc := NewChatService(mock, 30*time.Second, store, 0, 0, 0)

	var chunks []string
	response, err := svc.ChatStream(context.Background(), ChatInput{
		Owner:     "alice",
		Repo:      "widget",
		PRNumber:  42,
		PRContext: "PR #42: test",
		Message:   "What does this do?",
	}, func(s string) { chunks = append(chunks, s) })

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should prefer streamed text over result text
	if response != "Here is my response about the PR" {
		t.Errorf("response = %q", response)
	}
	if len(chunks) != 3 {
		t.Fatalf("got %d chunks, want 3", len(chunks))
	}

	// Session should have 2 messages (user + assistant)
	msgs := svc.GetSessionMessages("alice", "widget", 42)
	if len(msgs) != 2 {
		t.Fatalf("got %d messages, want 2", len(msgs))
	}
	if msgs[0].Role != "user" || msgs[0].Content != "What does this do?" {
		t.Errorf("msgs[0] = %+v", msgs[0])
	}
	if msgs[1].Role != "assistant" {
		t.Errorf("msgs[1].Role = %q", msgs[1].Role)
	}

	// Verify CLI flags
	args := strings.Join(mock.lastArgs, " ")
	if !strings.Contains(args, "--include-partial-messages") {
		t.Error("missing --include-partial-messages flag")
	}
}

func TestChatService_ChatStream_AccumulatesHistory(t *testing.T) {
	call := 0
	responses := []string{
		"First response",
		"Second response",
	}

	// Custom mock that returns different results per call
	executor := &sequentialMockExecutor{
		calls: make([]mockCall, len(responses)),
	}
	for i, resp := range responses {
		resultJSON, _ := json.Marshal(resp)
		executor.calls[i] = mockCall{
			stdout: resultEvent(string(resultJSON)) + "\n",
		}
	}
	_ = call

	svc := NewChatService(executor, 30*time.Second, nil, 0, 0, 0)

	// First chat
	_, err := svc.ChatStream(context.Background(), ChatInput{
		Owner: "a", Repo: "b", PRNumber: 1, PRContext: "ctx", Message: "q1",
	}, func(string) {})
	if err != nil {
		t.Fatalf("first chat error: %v", err)
	}

	// Second chat
	_, err = svc.ChatStream(context.Background(), ChatInput{
		Owner: "a", Repo: "b", PRNumber: 1, PRContext: "ctx", Message: "q2",
	}, func(string) {})
	if err != nil {
		t.Fatalf("second chat error: %v", err)
	}

	// Should have 4 messages total
	msgs := svc.GetSessionMessages("a", "b", 1)
	if len(msgs) != 4 {
		t.Fatalf("got %d messages, want 4", len(msgs))
	}
}

// sequentialMockExecutor returns different responses for sequential calls.
type sequentialMockExecutor struct {
	calls []mockCall
	idx   int
}

type mockCall struct {
	stdout string
}

func (m *sequentialMockExecutor) Start(_ context.Context, _ []string, _ ExecOptions) (*Process, error) {
	if m.idx >= len(m.calls) {
		return nil, fmt.Errorf("no more mock calls configured")
	}
	call := m.calls[m.idx]
	m.idx++
	return &Process{
		Stdout: io.NopCloser(strings.NewReader(call.stdout)),
		Stderr: io.NopCloser(strings.NewReader("")),
		Wait:   func() error { return nil },
	}, nil
}

// --- extractReviewResult tests (extending existing coverage) ---

func TestExtractReviewResult_DirectJSON(t *testing.T) {
	review := ReviewAnalysis{
		Action: "approve",
		Body:   "LGTM",
		Comments: []InlineReviewComment{
			{Path: "main.go", Line: 5, Body: "Nice"},
		},
	}
	data, _ := json.Marshal(review)

	event := &StreamEvent{Type: "result", Result: string(data)}
	got, err := extractReviewResult(event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Action != "approve" {
		t.Errorf("Action = %q", got.Action)
	}
	if len(got.Comments) != 1 {
		t.Errorf("Comments count = %d", len(got.Comments))
	}
}

func TestExtractReviewResult_WrappedJSON(t *testing.T) {
	jsonStr := `{"action":"request_changes","body":"Please fix","comments":[]}`
	wrapped := "Here is my review:\n" + jsonStr + "\nEnd."

	event := &StreamEvent{Type: "result", Result: wrapped}
	got, err := extractReviewResult(event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Action != "request_changes" {
		t.Errorf("Action = %q", got.Action)
	}
}

func TestExtractReviewResult_NoJSON(t *testing.T) {
	event := &StreamEvent{Type: "result", Result: "no json"}
	_, err := extractReviewResult(event)
	if err == nil {
		t.Fatal("expected error")
	}
}
