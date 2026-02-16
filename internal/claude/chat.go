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

// ChatService manages Claude chat sessions for PR discussions.
type ChatService struct {
	executor           CommandExecutor
	timeout            time.Duration
	maxPromptTokens    int
	maxHistoryMessages int
	maxTurns           int
	mu                 sync.Mutex
	sessions           map[string]*ChatSession
	store              *ChatStore // optional persistent store
}

// NewChatService creates a ChatService with optional persistent storage.
func NewChatService(executor CommandExecutor, timeout time.Duration, store *ChatStore, maxPromptTokens, maxHistory, maxTurns int) *ChatService {
	return &ChatService{
		executor:           executor,
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
	Owner         string
	Repo          string
	PRNumber      int
	PRContext     string // PR metadata + diff content embedded as text
	HunksSelected bool   // true when the user has selected specific hunks
	Message       string
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

	opts := ExecOptions{
		Env: filterEnv(os.Environ(), "ANTHROPIC_API_KEY"),
	}

	var streamedText strings.Builder
	visitor := func(event *StreamEvent) {
		// Token-level streaming: stream_event with content_block_delta
		if event.Type == "stream_event" && event.Event != nil {
			if event.Event.Type == "content_block_delta" && event.Event.Delta != nil {
				if event.Event.Delta.Type == "text_delta" && event.Event.Delta.Text != "" {
					onChunk(event.Event.Delta.Text)
					streamedText.WriteString(event.Event.Delta.Text)
				}
			}
			return
		}

		// Fallback: complete assistant turn (without --include-partial-messages)
		if event.Type == "assistant" && event.Message != nil {
			for _, block := range event.Message.Content {
				if block.Type == "text" && block.Text != "" {
					onChunk(block.Text)
				}
			}
		}
	}

	resultEvent, err := runCLI(ctx, cs.executor, args, opts, visitor)
	if err != nil {
		return "", err
	}

	// Prefer streamed text if available (token-level), fall back to result event
	finalText := extractResultText(resultEvent)
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
