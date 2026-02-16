package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// Analyzer spawns claude CLI to produce structured PR analysis.
type Analyzer struct {
	executor   CommandExecutor
	promptsDir string

	mu               sync.RWMutex
	timeout          time.Duration
	analysisMaxTurns int
}

// NewAnalyzer creates an Analyzer. executor handles subprocess spawning.
// timeout is the maximum time to wait for analysis to complete.
// promptsDir is the directory for custom per-repo prompts (may be empty).
// analysisMaxTurns is the max agentic turns for analysis (0 defaults to 30).
func NewAnalyzer(executor CommandExecutor, timeout time.Duration, promptsDir string, analysisMaxTurns int) *Analyzer {
	return &Analyzer{
		executor:         executor,
		timeout:          timeout,
		promptsDir:       promptsDir,
		analysisMaxTurns: analysisMaxTurns,
	}
}

// SetTimeout updates the command timeout for future analysis requests.
func (a *Analyzer) SetTimeout(d time.Duration) {
	a.mu.Lock()
	a.timeout = d
	a.mu.Unlock()
}

// SetAnalysisMaxTurns updates the max agentic turns for future analysis requests.
func (a *Analyzer) SetAnalysisMaxTurns(n int) {
	a.mu.Lock()
	a.analysisMaxTurns = n
	a.mu.Unlock()
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

// config returns a snapshot of mutable config fields under read lock.
func (a *Analyzer) config() (timeout time.Duration, maxTurns int) {
	a.mu.RLock()
	timeout = a.timeout
	maxTurns = a.analysisMaxTurns
	a.mu.RUnlock()
	return
}

// Analyze runs Claude CLI analysis on a PR and returns the structured result.
func (a *Analyzer) Analyze(ctx context.Context, input AnalyzeInput, onProgress ProgressFunc) (*AnalysisResult, error) {
	timeout, maxTurns := a.config()
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	prompt := buildAnalysisPrompt(a.promptsDir, input)

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

	opts := ExecOptions{
		Dir: input.RepoPath,
		Env: filterEnv(os.Environ(), "ANTHROPIC_API_KEY"),
	}

	resultEvent, err := runCLI(ctx, a.executor, args, opts, progressVisitor(onProgress))
	if err != nil {
		return nil, err
	}

	return extractAnalysisResult(resultEvent)
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
	timeout, _ := a.config()
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	prompt := buildReviewPrompt(a.promptsDir, input)

	args := []string{
		"-p", prompt,
		"--output-format", "stream-json",
		"--verbose",
		"--max-turns", "1",
	}

	opts := ExecOptions{
		Env: filterEnv(os.Environ(), "ANTHROPIC_API_KEY"),
	}

	resultEvent, err := runCLI(ctx, a.executor, args, opts, progressVisitor(onProgress))
	if err != nil {
		return nil, err
	}

	return extractReviewResult(resultEvent)
}

// AnalyzeDiff runs analysis using inline diff content (no local repo needed).
func (a *Analyzer) AnalyzeDiff(ctx context.Context, input AnalyzeDiffInput, onProgress ProgressFunc) (*AnalysisResult, error) {
	timeout, _ := a.config()
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	prompt := buildDiffAnalysisPrompt(a.promptsDir, input)

	args := []string{
		"-p", prompt,
		"--output-format", "stream-json",
		"--verbose",
		"--max-turns", "1",
	}

	opts := ExecOptions{
		Env: filterEnv(os.Environ(), "ANTHROPIC_API_KEY"),
	}

	resultEvent, err := runCLI(ctx, a.executor, args, opts, progressVisitor(onProgress))
	if err != nil {
		return nil, err
	}

	return extractAnalysisResult(resultEvent)
}

// AnalyzeDiffStream is like AnalyzeDiff but with token-level streaming.
// onChunk is called with each text delta as it arrives from the Claude CLI.
func (a *Analyzer) AnalyzeDiffStream(ctx context.Context, input AnalyzeDiffInput, onChunk func(string)) (*AnalysisResult, error) {
	timeout, _ := a.config()
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	prompt := buildDiffAnalysisPrompt(a.promptsDir, input)

	args := []string{
		"-p", prompt,
		"--output-format", "stream-json",
		"--verbose",
		"--include-partial-messages",
		"--max-turns", "1",
	}

	opts := ExecOptions{
		Env: filterEnv(os.Environ(), "ANTHROPIC_API_KEY"),
	}

	resultEvent, err := runCLI(ctx, a.executor, args, opts, streamDeltaVisitor(onChunk))
	if err != nil {
		return nil, err
	}

	return extractAnalysisResult(resultEvent)
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
	return strings.Contains(err.Error(), "executable file not found")
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
