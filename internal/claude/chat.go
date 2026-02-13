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
	claudePath string
	timeout    time.Duration
	mu         sync.Mutex
	sessions   map[string]*ChatSession
}

// NewChatService creates a ChatService.
func NewChatService(claudePath string, timeout time.Duration) *ChatService {
	return &ChatService{
		claudePath: claudePath,
		timeout:    timeout,
		sessions:   make(map[string]*ChatSession),
	}
}

// ChatInput contains the parameters for a chat request.
// RepoPath is optional — if empty, Claude runs without filesystem access (diff-as-context mode).
type ChatInput struct {
	Owner     string
	Repo      string
	PRNumber  int
	PRContext string // PR metadata + diff content embedded as text
	Message   string
}

// ClearSession removes the chat history for a PR.
func (cs *ChatService) ClearSession(owner, repo string, prNumber int) {
	key := sessionKey(owner, repo, prNumber)
	cs.mu.Lock()
	delete(cs.sessions, key)
	cs.mu.Unlock()
}

func (cs *ChatService) getOrCreateSession(input ChatInput) *ChatSession {
	key := sessionKey(input.Owner, input.Repo, input.PRNumber)
	cs.mu.Lock()
	defer cs.mu.Unlock()

	session, ok := cs.sessions[key]
	if !ok {
		session = &ChatSession{
			PRContext: input.PRContext,
		}
		cs.sessions[key] = session
	}
	return session
}

func buildChatPrompt(session *ChatSession, userMessage string) string {
	var b strings.Builder

	b.WriteString("You are helping review a pull request. Here is the context:\n\n")
	b.WriteString(session.PRContext)
	b.WriteString("\n\nAnswer questions about this PR based on the diff and metadata provided above.\n")

	for _, msg := range session.Messages {
		if msg.Role == "user" {
			fmt.Fprintf(&b, "\nUser: %s", msg.Content)
		} else {
			fmt.Fprintf(&b, "\nAssistant: %s", msg.Content)
		}
	}

	fmt.Fprintf(&b, "\nUser: %s\n\nRespond helpfully and concisely.", userMessage)

	return b.String()
}

// ChatStream sends a message to Claude with streaming JSON output.
// onChunk is called with each text chunk as it arrives.
// Returns the complete response text.
func (cs *ChatService) ChatStream(ctx context.Context, input ChatInput, onChunk func(text string)) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, cs.timeout)
	defer cancel()

	session := cs.getOrCreateSession(input)
	prompt := buildChatPrompt(session, input.Message)

	args := []string{
		"-p", prompt,
		"--output-format", "stream-json",
		"--verbose",
		"--include-partial-messages",
		"--max-turns", "3",
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
			return "", fmt.Errorf("claude chat timed out after %s", cs.timeout)
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

func sessionKey(owner, repo string, prNumber int) string {
	return fmt.Sprintf("%s_%s_%d", owner, repo, prNumber)
}
