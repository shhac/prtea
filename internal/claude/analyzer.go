package claude

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Analyzer spawns claude CLI to produce structured PR analysis.
type Analyzer struct {
	claudePath       string
	timeout          time.Duration
	promptsDir       string
	analysisMaxTurns int
}

// NewAnalyzer creates an Analyzer. claudePath is the path to the claude binary.
// timeout is the maximum time to wait for analysis to complete.
// promptsDir is the directory for custom per-repo prompts (may be empty).
// analysisMaxTurns is the max agentic turns for analysis (0 defaults to 30).
func NewAnalyzer(claudePath string, timeout time.Duration, promptsDir string, analysisMaxTurns int) *Analyzer {
	return &Analyzer{
		claudePath:       claudePath,
		timeout:          timeout,
		promptsDir:       promptsDir,
		analysisMaxTurns: analysisMaxTurns,
	}
}

// SetTimeout updates the command timeout for future analysis requests.
func (a *Analyzer) SetTimeout(d time.Duration) {
	a.timeout = d
}

// SetAnalysisMaxTurns updates the max agentic turns for future analysis requests.
func (a *Analyzer) SetAnalysisMaxTurns(n int) {
	a.analysisMaxTurns = n
}

// AnalyzeInput contains the parameters for a PR analysis.
type AnalyzeInput struct {
	RepoPath   string
	Owner      string
	Repo       string
	PRNumber   int
	PRTitle    string
	PRBody     string
	BaseBranch string
	HeadBranch string
}

// AnalyzeDiffInput contains the parameters for a diff-based analysis (no local repo needed).
type AnalyzeDiffInput struct {
	Owner       string
	Repo        string
	PRNumber    int
	PRTitle     string
	PRBody      string
	DiffContent string // unified diff patches for all changed files
}

// Analyze runs Claude CLI analysis on a PR and returns the structured result.
func (a *Analyzer) Analyze(ctx context.Context, input AnalyzeInput, onProgress ProgressFunc) (*AnalysisResult, error) {
	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()

	prompt := a.buildAnalysisPrompt(input)

	maxTurns := a.analysisMaxTurns
	if maxTurns == 0 {
		maxTurns = 30
	}

	args := []string{
		"-p", prompt,
		"--output-format", "stream-json",
		"--verbose",
		"--allowedTools", "Read,Glob,Grep,Bash",
		"--max-turns", fmt.Sprintf("%d", maxTurns),
	}

	cmd := exec.CommandContext(ctx, a.claudePath, args...)
	cmd.Dir = input.RepoPath
	cmd.Stdin = nil

	// Remove ANTHROPIC_API_KEY from env — let Claude CLI use its own auth
	cmd.Env = filterEnv(os.Environ(), "ANTHROPIC_API_KEY")

	return a.runAndParse(ctx, cmd, onProgress)
}

// ReviewInput contains the parameters for generating an AI review.
type ReviewInput struct {
	Owner       string
	Repo        string
	PRNumber    int
	PRTitle     string
	PRBody      string
	DiffContent string // unified diff patches for all changed files
}

// AnalyzeForReview runs Claude to generate a GitHub-ready review with inline comments.
func (a *Analyzer) AnalyzeForReview(ctx context.Context, input ReviewInput, onProgress ProgressFunc) (*ReviewAnalysis, error) {
	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()

	prompt := a.buildReviewPrompt(input)

	args := []string{
		"-p", prompt,
		"--output-format", "stream-json",
		"--verbose",
		"--max-turns", "1",
	}

	cmd := exec.CommandContext(ctx, a.claudePath, args...)
	cmd.Stdin = nil
	cmd.Env = filterEnv(os.Environ(), "ANTHROPIC_API_KEY")

	return a.runAndParseReview(ctx, cmd, onProgress)
}

// AnalyzeDiff runs analysis using inline diff content (no local repo needed).
func (a *Analyzer) AnalyzeDiff(ctx context.Context, input AnalyzeDiffInput, onProgress ProgressFunc) (*AnalysisResult, error) {
	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()

	prompt := a.buildDiffAnalysisPrompt(input)

	args := []string{
		"-p", prompt,
		"--output-format", "stream-json",
		"--verbose",
		"--max-turns", "1",
	}

	cmd := exec.CommandContext(ctx, a.claudePath, args...)
	cmd.Stdin = nil
	cmd.Env = filterEnv(os.Environ(), "ANTHROPIC_API_KEY")

	return a.runAndParse(ctx, cmd, onProgress)
}

// AnalyzeDiffStream is like AnalyzeDiff but with token-level streaming.
// onChunk is called with each text delta as it arrives from the Claude CLI.
func (a *Analyzer) AnalyzeDiffStream(ctx context.Context, input AnalyzeDiffInput, onChunk func(string)) (*AnalysisResult, error) {
	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()

	prompt := a.buildDiffAnalysisPrompt(input)

	args := []string{
		"-p", prompt,
		"--output-format", "stream-json",
		"--verbose",
		"--include-partial-messages",
		"--max-turns", "1",
	}

	cmd := exec.CommandContext(ctx, a.claudePath, args...)
	cmd.Stdin = nil
	cmd.Env = filterEnv(os.Environ(), "ANTHROPIC_API_KEY")

	return a.runAndParseStream(ctx, cmd, onChunk)
}

// runAndParseStream starts the Claude CLI subprocess with token-level streaming,
// calling onChunk for each text delta, and extracting the final result.
func (a *Analyzer) runAndParseStream(ctx context.Context, cmd *exec.Cmd, onChunk func(string)) (*AnalysisResult, error) {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		if isNotFound(err) {
			return nil, fmt.Errorf("claude CLI not found at %s: ensure 'claude' is installed", a.claudePath)
		}
		return nil, fmt.Errorf("failed to start claude: %w", err)
	}

	// Drain stderr in background
	var stderrBuf strings.Builder
	go func() {
		scanner := bufio.NewScanner(stderr)
		scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
		for scanner.Scan() {
			stderrBuf.WriteString(scanner.Text())
			stderrBuf.WriteByte('\n')
		}
	}()

	// Parse stream-json events from stdout
	var resultEvent *StreamEvent
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}

		var event StreamEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}

		// Token-level streaming: stream_event with content_block_delta
		if event.Type == "stream_event" && event.Event != nil {
			if event.Event.Type == "content_block_delta" && event.Event.Delta != nil {
				if event.Event.Delta.Type == "text_delta" && event.Event.Delta.Text != "" {
					if onChunk != nil {
						onChunk(event.Event.Delta.Text)
					}
				}
			}
			continue
		}

		if event.Type == "result" {
			resultEvent = &event
		}
	}

	if err := cmd.Wait(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("claude analysis timed out after %s", a.timeout)
		}
		errMsg := stderrBuf.String()
		if len(errMsg) > 500 {
			errMsg = errMsg[:500]
		}
		return nil, fmt.Errorf("claude exited with error: %w\nstderr: %s", err, errMsg)
	}

	if resultEvent == nil {
		return nil, fmt.Errorf("claude produced no result event")
	}

	return extractAnalysisResult(resultEvent)
}

// runAndParse starts the Claude CLI subprocess, reads stream-json events, and extracts the result.
func (a *Analyzer) runAndParse(ctx context.Context, cmd *exec.Cmd, onProgress ProgressFunc) (*AnalysisResult, error) {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		if isNotFound(err) {
			return nil, fmt.Errorf("claude CLI not found at %s: ensure 'claude' is installed", a.claudePath)
		}
		return nil, fmt.Errorf("failed to start claude: %w", err)
	}

	// Drain stderr in background
	var stderrBuf strings.Builder
	go func() {
		scanner := bufio.NewScanner(stderr)
		scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
		for scanner.Scan() {
			stderrBuf.WriteString(scanner.Text())
			stderrBuf.WriteByte('\n')
		}
	}()

	// Parse stream-json events from stdout
	var resultEvent *StreamEvent
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}

		var event StreamEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue // skip unparseable lines
		}

		if onProgress != nil {
			reportProgress(&event, onProgress)
		}

		if event.Type == "result" {
			resultEvent = &event
		}
	}

	if err := cmd.Wait(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("claude analysis timed out after %s", a.timeout)
		}
		errMsg := stderrBuf.String()
		if len(errMsg) > 500 {
			errMsg = errMsg[:500]
		}
		return nil, fmt.Errorf("claude exited with error: %w\nstderr: %s", err, errMsg)
	}

	if resultEvent == nil {
		return nil, fmt.Errorf("claude produced no result event")
	}

	return extractAnalysisResult(resultEvent)
}

func (a *Analyzer) buildAnalysisPrompt(input AnalyzeInput) string {
	body := input.PRBody
	if body == "" {
		body = "No description provided."
	}

	customPrompt := a.loadCustomPrompt(input.Owner, input.Repo)

	return fmt.Sprintf(`You are reviewing PR #%d: "%s".

PR description:
%s

Instructions:
1. Run `+"`git diff origin/%s...origin/%s`"+` to see all changes in this PR.
2. For each changed file, read the full file on the %s branch to understand context — follow imports, check callers, understand the module's role.
3. Produce a thorough code review as structured JSON output.

Focus on: correctness, security, performance, maintainability, and test coverage. Be specific with line numbers when possible.
%s
IMPORTANT: Your final response must be ONLY valid JSON matching this schema (no markdown, no wrapping):
%s`,
		input.PRNumber, input.PRTitle,
		body,
		input.BaseBranch, input.HeadBranch,
		input.BaseBranch,
		customPrompt,
		analysisJSONSchema,
	)
}

func (a *Analyzer) loadCustomPrompt(owner, repo string) string {
	if a.promptsDir == "" {
		return ""
	}
	path := fmt.Sprintf("%s/%s_%s.md", a.promptsDir, owner, repo)
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return "\nAdditional review instructions:\n" + string(data)
}

func (a *Analyzer) buildDiffAnalysisPrompt(input AnalyzeDiffInput) string {
	body := input.PRBody
	if body == "" {
		body = "No description provided."
	}

	customPrompt := a.loadCustomPrompt(input.Owner, input.Repo)

	return fmt.Sprintf(`You are reviewing PR #%d in %s/%s: "%s".

PR description:
%s

Here is the complete diff for this PR:

%s

Instructions:
1. Review all changes shown in the diff above.
2. Produce a thorough code review as structured JSON output.

Focus on: correctness, security, performance, maintainability, and test coverage. Be specific with line numbers when possible.
%s
IMPORTANT: Your final response must be ONLY valid JSON matching this schema (no markdown, no wrapping):
%s`,
		input.PRNumber, input.Owner, input.Repo, input.PRTitle,
		body,
		input.DiffContent,
		customPrompt,
		analysisJSONSchema,
	)
}

func extractAnalysisResult(event *StreamEvent) (*AnalysisResult, error) {
	var resultText string

	switch v := event.Result.(type) {
	case string:
		resultText = v
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal result: %w", err)
		}
		resultText = string(data)
	}

	// Try direct parse
	var result AnalysisResult
	if err := json.Unmarshal([]byte(resultText), &result); err == nil {
		return &result, nil
	}

	// Fallback: extract JSON between first { and last }
	start := strings.Index(resultText, "{")
	end := strings.LastIndex(resultText, "}")
	if start == -1 || end == -1 || end <= start {
		return nil, fmt.Errorf("no JSON object found in claude result")
	}

	if err := json.Unmarshal([]byte(resultText[start:end+1]), &result); err != nil {
		return nil, fmt.Errorf("failed to parse analysis JSON: %w\nraw: %s", err, truncate(resultText, 500))
	}

	return &result, nil
}

func reportProgress(event *StreamEvent, onProgress ProgressFunc) {
	switch event.Type {
	case "assistant":
		if event.Message == nil {
			return
		}
		for _, block := range event.Message.Content {
			switch block.Type {
			case "tool_use":
				onProgress(ProgressEvent{
					Type:    "tool_use",
					Message: fmt.Sprintf("Using %s...", block.Name),
				})
			case "text":
				if block.Text != "" {
					onProgress(ProgressEvent{
						Type:    "text",
						Message: truncate(block.Text, 100),
					})
				}
			}
		}
	}
}

func filterEnv(env []string, remove string) []string {
	prefix := remove + "="
	filtered := make([]string, 0, len(env))
	for _, e := range env {
		if !strings.HasPrefix(e, prefix) {
			filtered = append(filtered, e)
		}
	}
	return filtered
}

func isNotFound(err error) bool {
	return strings.Contains(err.Error(), exec.ErrNotFound.Error())
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// runAndParseReview starts the Claude CLI subprocess and extracts a ReviewAnalysis result.
func (a *Analyzer) runAndParseReview(ctx context.Context, cmd *exec.Cmd, onProgress ProgressFunc) (*ReviewAnalysis, error) {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		if isNotFound(err) {
			return nil, fmt.Errorf("claude CLI not found at %s: ensure 'claude' is installed", a.claudePath)
		}
		return nil, fmt.Errorf("failed to start claude: %w", err)
	}

	// Drain stderr in background
	var stderrBuf strings.Builder
	go func() {
		scanner := bufio.NewScanner(stderr)
		scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
		for scanner.Scan() {
			stderrBuf.WriteString(scanner.Text())
			stderrBuf.WriteByte('\n')
		}
	}()

	// Parse stream-json events from stdout
	var resultEvent *StreamEvent
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}

		var event StreamEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}

		if onProgress != nil {
			reportProgress(&event, onProgress)
		}

		if event.Type == "result" {
			resultEvent = &event
		}
	}

	if err := cmd.Wait(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("claude review timed out after %s", a.timeout)
		}
		errMsg := stderrBuf.String()
		if len(errMsg) > 500 {
			errMsg = errMsg[:500]
		}
		return nil, fmt.Errorf("claude exited with error: %w\nstderr: %s", err, errMsg)
	}

	if resultEvent == nil {
		return nil, fmt.Errorf("claude produced no result event")
	}

	return extractReviewResult(resultEvent)
}

func (a *Analyzer) buildReviewPrompt(input ReviewInput) string {
	body := input.PRBody
	if body == "" {
		body = "No description provided."
	}

	customPrompt := a.loadCustomPrompt(input.Owner, input.Repo)

	return fmt.Sprintf(`You are generating a GitHub pull request review for PR #%d in %s/%s: "%s".

PR description:
%s

Here is the complete diff for this PR:

%s

Instructions:
1. Review all changes shown in the diff above.
2. Decide whether to approve, comment, or request changes.
3. Write an overall review body summarizing your assessment.
4. For specific issues, add inline comments targeting the exact file path and line number.
   - Use the NEW file line number (right side of the diff) for added/modified lines.
   - Only comment on lines that actually appear in the diff.
   - Each comment should be actionable and specific.
   - Focus on bugs, security issues, and significant improvements. Skip trivial style nits.
%s
IMPORTANT: Your final response must be ONLY valid JSON matching this schema (no markdown, no wrapping):
%s`,
		input.PRNumber, input.Owner, input.Repo, input.PRTitle,
		body,
		input.DiffContent,
		customPrompt,
		reviewJSONSchema,
	)
}

func extractReviewResult(event *StreamEvent) (*ReviewAnalysis, error) {
	var resultText string

	switch v := event.Result.(type) {
	case string:
		resultText = v
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal result: %w", err)
		}
		resultText = string(data)
	}

	// Try direct parse
	var result ReviewAnalysis
	if err := json.Unmarshal([]byte(resultText), &result); err == nil {
		return &result, nil
	}

	// Fallback: extract JSON between first { and last }
	start := strings.Index(resultText, "{")
	end := strings.LastIndex(resultText, "}")
	if start == -1 || end == -1 || end <= start {
		return nil, fmt.Errorf("no JSON object found in claude review result")
	}

	if err := json.Unmarshal([]byte(resultText[start:end+1]), &result); err != nil {
		return nil, fmt.Errorf("failed to parse review JSON: %w\nraw: %s", err, truncate(resultText, 500))
	}

	return &result, nil
}

// reviewJSONSchema is the JSON schema for AI-generated reviews.
var reviewJSONSchema = `{
  "type": "object",
  "required": ["action", "body", "comments"],
  "properties": {
    "action": { "type": "string", "enum": ["approve", "comment", "request_changes"] },
    "body": { "type": "string", "description": "Overall review comment summarizing the assessment" },
    "comments": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["path", "line", "body"],
        "properties": {
          "path": { "type": "string", "description": "Relative file path" },
          "line": { "type": "number", "description": "Line number in the new file (right side)" },
          "body": { "type": "string", "description": "Inline comment text" }
        }
      }
    }
  }
}`

// analysisJSONSchema is the JSON schema that Claude must produce.
var analysisJSONSchema = `{
  "type": "object",
  "required": ["summary", "risk", "architectureImpact", "fileReviews", "testCoverage", "suggestions"],
  "properties": {
    "summary": { "type": "string" },
    "risk": {
      "type": "object",
      "required": ["level", "reasoning"],
      "properties": {
        "level": { "type": "string", "enum": ["low", "medium", "high", "critical"] },
        "reasoning": { "type": "string" }
      }
    },
    "architectureImpact": {
      "type": "object",
      "required": ["hasImpact", "description", "affectedModules"],
      "properties": {
        "hasImpact": { "type": "boolean" },
        "description": { "type": "string" },
        "affectedModules": { "type": "array", "items": { "type": "string" } }
      }
    },
    "fileReviews": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["file", "summary", "comments"],
        "properties": {
          "file": { "type": "string" },
          "summary": { "type": "string" },
          "comments": {
            "type": "array",
            "items": {
              "type": "object",
              "required": ["severity", "comment"],
              "properties": {
                "line": { "type": "number" },
                "severity": { "type": "string", "enum": ["critical", "warning", "suggestion", "praise"] },
                "comment": { "type": "string" }
              }
            }
          }
        }
      }
    },
    "testCoverage": {
      "type": "object",
      "required": ["assessment", "gaps"],
      "properties": {
        "assessment": { "type": "string" },
        "gaps": { "type": "array", "items": { "type": "string" } }
      }
    },
    "suggestions": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["title", "description"],
        "properties": {
          "title": { "type": "string" },
          "description": { "type": "string" },
          "file": { "type": "string" }
        }
      }
    }
  }
}`
