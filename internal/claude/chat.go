package claude

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// ChatService manages Claude chat sessions for PR discussions.
type ChatService struct {
	claudePath         string
	timeout            time.Duration
	maxPromptTokens    int
	maxHistoryMessages int
	maxTurns           int
	mu                 sync.Mutex
	sessions           map[string]*ChatSession
	store              *ChatStore // optional persistent store
}

// NewChatService creates a ChatService with optional persistent storage.
func NewChatService(claudePath string, timeout time.Duration, store *ChatStore, maxPromptTokens, maxHistory, maxTurns int) *ChatService {
	return &ChatService{
		claudePath:         claudePath,
		timeout:            timeout,
		maxPromptTokens:    maxPromptTokens,
		maxHistoryMessages: maxHistory,
		maxTurns:           maxTurns,
		sessions:           make(map[string]*ChatSession),
		store:              store,
	}
}

// ChatInput contains the parameters for a chat request.
// RepoPath is optional — if empty, Claude runs without filesystem access (diff-as-context mode).
type ChatInput struct {
	Owner        string
	Repo         string
	PRNumber     int
	PRContext    string // PR metadata + diff content embedded as text
	HunksSelected bool   // true when the user has selected specific hunks
	Message      string
}

// ClearSession removes the chat history for a PR (in memory and on disk).
func (cs *ChatService) ClearSession(owner, repo string, prNumber int) {
	key := sessionKey(owner, repo, prNumber)
	cs.mu.Lock()
	delete(cs.sessions, key)
	cs.mu.Unlock()
	if cs.store != nil {
		_ = cs.store.Delete(owner, repo, prNumber)
	}
}

// SaveSession persists the current session for a PR to disk.
// Called when switching PRs so the conversation can be restored later.
func (cs *ChatService) SaveSession(owner, repo string, prNumber int) {
	if cs.store == nil {
		return
	}
	key := sessionKey(owner, repo, prNumber)
	cs.mu.Lock()
	session, ok := cs.sessions[key]
	cs.mu.Unlock()
	if ok && len(session.Messages) > 0 {
		_ = cs.store.Put(owner, repo, prNumber, session.Messages)
	}
}

// GetSessionMessages returns the messages for a PR session (from memory or disk).
// Used by the UI to restore chat history when returning to a PR.
func (cs *ChatService) GetSessionMessages(owner, repo string, prNumber int) []ChatMessage {
	key := sessionKey(owner, repo, prNumber)
	cs.mu.Lock()
	session, ok := cs.sessions[key]
	cs.mu.Unlock()
	if ok {
		return session.Messages
	}
	// Try loading from disk
	if cs.store != nil {
		if cached, err := cs.store.Get(owner, repo, prNumber); err == nil && cached != nil {
			// Restore into memory
			cs.mu.Lock()
			cs.sessions[key] = &ChatSession{Messages: cached.Messages}
			cs.mu.Unlock()
			return cached.Messages
		}
	}
	return nil
}

func (cs *ChatService) getOrCreateSession(input ChatInput) *ChatSession {
	key := sessionKey(input.Owner, input.Repo, input.PRNumber)
	cs.mu.Lock()
	defer cs.mu.Unlock()

	session, ok := cs.sessions[key]
	if !ok {
		// Try loading from disk
		if cs.store != nil {
			if cached, err := cs.store.Get(input.Owner, input.Repo, input.PRNumber); err == nil && cached != nil {
				session = &ChatSession{
					PRContext: input.PRContext,
					Messages:  cached.Messages,
				}
				cs.sessions[key] = session
				return session
			}
		}
		session = &ChatSession{
			PRContext: input.PRContext,
		}
		cs.sessions[key] = session
	}
	return session
}

// Token budget defaults (used when service values are zero).
const (
	defaultMaxPromptTokens    = 100_000
	defaultMaxHistoryMessages = 16
	defaultChatMaxTurns       = 3
)

// estimateTokens returns a rough token count for a string.
// Code and diffs average ~3 chars per token; prose ~4 chars.
// We use 3 as a conservative estimate (overestimates slightly for prose).
func estimateTokens(s string) int {
	return len(s) / 3
}

func buildChatPrompt(session *ChatSession, input ChatInput, maxTokens, maxHistory int) string {
	var b strings.Builder

	// System instruction (always included)
	systemPrefix := "You are helping review a pull request. Here is the context:\n\n"
	b.WriteString(systemPrefix)

	var instruction string
	if input.HunksSelected {
		instruction = "\n\nThe user has selected specific code hunks from the diff above. " +
			"Focus your answer primarily on these selected hunks. " +
			"Explain what the selected code does, flag potential issues, and suggest improvements.\n"
	} else {
		instruction = "\n\nAnswer questions about this PR based on the diff and metadata provided above.\n"
	}

	currentMsg := fmt.Sprintf("\nUser: %s\n\nRespond helpfully and concisely.", input.Message)

	// Calculate fixed token costs
	fixedTokens := estimateTokens(systemPrefix) + estimateTokens(instruction) + estimateTokens(currentMsg)
	contextTokens := estimateTokens(input.PRContext)

	// Determine which messages to include (most recent first, up to budget)
	messages := session.Messages
	if len(messages) > maxHistory {
		messages = messages[len(messages)-maxHistory:]
	}

	// Further trim messages if total exceeds token budget
	historyTokens := 0
	for _, msg := range messages {
		historyTokens += estimateTokens(msg.Content) + 10 // 10 for "User: " / "Assistant: " prefix
	}

	totalTokens := fixedTokens + contextTokens + historyTokens
	if totalTokens > maxTokens && len(messages) > 2 {
		// Drop oldest messages until we fit (keep at least the last 2 messages)
		for totalTokens > maxTokens && len(messages) > 2 {
			dropped := messages[0]
			messages = messages[1:]
			totalTokens -= estimateTokens(dropped.Content) + 10
		}
	}

	// If still over budget after trimming history, truncate the diff context
	prContext := input.PRContext
	if totalTokens > maxTokens {
		// Calculate how many tokens we can afford for the context
		availableContextTokens := maxTokens - fixedTokens - historyTokens
		if availableContextTokens < 0 {
			availableContextTokens = 0
		}
		maxContextChars := availableContextTokens * 3 // reverse the estimation
		if maxContextChars > 0 && maxContextChars < len(prContext) {
			prContext = prContext[:maxContextChars] + "\n\n[... diff truncated to fit context window ...]"
		}
	}

	b.WriteString(prContext)
	b.WriteString(instruction)

	for _, msg := range messages {
		if msg.Role == "user" {
			fmt.Fprintf(&b, "\nUser: %s", msg.Content)
		} else {
			fmt.Fprintf(&b, "\nAssistant: %s", msg.Content)
		}
	}

	b.WriteString(currentMsg)

	return b.String()
}

// ChatStream sends a message to Claude with streaming JSON output.
// onChunk is called with each text chunk as it arrives.
// Returns the complete response text.
func (cs *ChatService) ChatStream(ctx context.Context, input ChatInput, onChunk func(text string)) (string, error) {
	// Snapshot config under lock to avoid races with Set* methods.
	cs.mu.Lock()
	timeout := cs.timeout
	maxTokens := cs.maxPromptTokens
	maxHistory := cs.maxHistoryMessages
	turns := cs.maxTurns
	cs.mu.Unlock()

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	session := cs.getOrCreateSession(input)

	if maxTokens == 0 {
		maxTokens = defaultMaxPromptTokens
	}
	if maxHistory == 0 {
		maxHistory = defaultMaxHistoryMessages
	}
	if turns == 0 {
		turns = defaultChatMaxTurns
	}

	prompt := buildChatPrompt(session, input, maxTokens, maxHistory)

	args := []string{
		"-p", prompt,
		"--output-format", "stream-json",
		"--verbose",
		"--include-partial-messages",
		"--max-turns", fmt.Sprintf("%d", turns),
	}

	cmd := exec.CommandContext(ctx, cs.claudePath, args...)
	cmd.Stdin = nil
	cmd.Env = filterEnv(os.Environ(), "ANTHROPIC_API_KEY")

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "", fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		if isNotFound(err) {
			return "", fmt.Errorf("claude CLI not found at %s: ensure 'claude' is installed", cs.claudePath)
		}
		return "", fmt.Errorf("failed to start claude: %w", err)
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

	// Parse stream-json events from stdout.
	// With --include-partial-messages, Claude emits "stream_event" envelopes
	// containing content_block_delta events with text_delta for token-level streaming.
	// We also keep the "assistant" handler as a fallback for complete turn events.
	var streamedText strings.Builder
	var resultText string
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
					onChunk(event.Event.Delta.Text)
					streamedText.WriteString(event.Event.Delta.Text)
				}
			}
			continue
		}

		// Fallback: complete assistant turn (without --include-partial-messages)
		if event.Type == "assistant" && event.Message != nil {
			for _, block := range event.Message.Content {
				if block.Type == "text" && block.Text != "" {
					onChunk(block.Text)
				}
			}
		}

		// Capture final result
		if event.Type == "result" {
			resultText = extractResultText(&event)
		}
	}

	if err := cmd.Wait(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("claude chat timed out after %s", timeout)
		}
		errMsg := stderrBuf.String()
		if len(errMsg) > 300 {
			errMsg = errMsg[:300]
		}
		return "", fmt.Errorf("claude chat exited with error: %w\nstderr: %s", err, errMsg)
	}

	// Prefer streamed text if available (token-level), fall back to result event
	finalText := resultText
	if streamedText.Len() > 0 {
		finalText = streamedText.String()
	}

	// Append exchange to session history
	cs.mu.Lock()
	session.Messages = append(session.Messages,
		ChatMessage{Role: "user", Content: input.Message},
		ChatMessage{Role: "assistant", Content: finalText},
	)
	cs.mu.Unlock()

	// Persist to disk after each exchange
	if cs.store != nil {
		_ = cs.store.Put(input.Owner, input.Repo, input.PRNumber, session.Messages)
	}

	return finalText, nil
}

// extractResultText pulls the text content from a result stream event.
func extractResultText(event *StreamEvent) string {
	switch v := event.Result.(type) {
	case string:
		return v
	default:
		if v == nil {
			return ""
		}
		data, err := json.Marshal(v)
		if err != nil {
			return ""
		}
		return string(data)
	}
}

// SetTimeout updates the command timeout for future chat requests.
func (cs *ChatService) SetTimeout(d time.Duration) {
	cs.mu.Lock()
	cs.timeout = d
	cs.mu.Unlock()
}

// SetMaxPromptTokens updates the max prompt token budget for future chat requests.
func (cs *ChatService) SetMaxPromptTokens(n int) {
	cs.mu.Lock()
	cs.maxPromptTokens = n
	cs.mu.Unlock()
}

// SetMaxHistoryMessages updates the max history messages for future chat requests.
func (cs *ChatService) SetMaxHistoryMessages(n int) {
	cs.mu.Lock()
	cs.maxHistoryMessages = n
	cs.mu.Unlock()
}

// SetMaxTurns updates the max agentic turns for future chat requests.
func (cs *ChatService) SetMaxTurns(n int) {
	cs.mu.Lock()
	cs.maxTurns = n
	cs.mu.Unlock()
}

func sessionKey(owner, repo string, prNumber int) string {
	return fmt.Sprintf("%s_%s_%d", owner, repo, prNumber)
}
