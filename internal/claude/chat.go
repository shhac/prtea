package claude

import (
	"bytes"
	"context"
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
type ChatInput struct {
	RepoPath  string
	Owner     string
	Repo      string
	PRNumber  int
	PRContext string
	Message   string
}

// Chat sends a message to Claude about a PR and returns the response.
// Conversation history is maintained per PR.
func (cs *ChatService) Chat(ctx context.Context, input ChatInput) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, cs.timeout)
	defer cancel()

	session := cs.getOrCreateSession(input)
	prompt := buildChatPrompt(session, input.Message)

	args := []string{
		"-p", prompt,
		"--allowedTools", "Read,Glob,Grep,Bash",
		"--max-turns", "10",
	}

	cmd := exec.CommandContext(ctx, cs.claudePath, args...)
	cmd.Dir = input.RepoPath
	cmd.Stdin = nil

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		if isNotFound(err) {
			return "", fmt.Errorf("claude CLI not found at %s: ensure 'claude' is installed", cs.claudePath)
		}
		return "", fmt.Errorf("failed to start claude: %w", err)
	}

	if err := cmd.Wait(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("claude chat timed out after %s", cs.timeout)
		}
		errMsg := stderr.String()
		if len(errMsg) > 300 {
			errMsg = errMsg[:300]
		}
		return "", fmt.Errorf("claude chat exited with error: %w\nstderr: %s", err, errMsg)
	}

	response := strings.TrimSpace(stdout.String())

	// Append exchange to session history
	cs.mu.Lock()
	session.Messages = append(session.Messages,
		ChatMessage{Role: "user", Content: input.Message},
		ChatMessage{Role: "assistant", Content: response},
	)
	cs.mu.Unlock()

	return response, nil
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
	b.WriteString("\n\nYou have access to the repository files to answer questions. The repo is checked out on the main branch.\n")

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

func sessionKey(owner, repo string, prNumber int) string {
	return fmt.Sprintf("%s_%s_%d", owner, repo, prNumber)
}

// removeEnvKey returns env with the specified key removed.
// Unused but kept as a utility parallel to filterEnv in analyzer.go.
func removeEnvKey(key string) []string {
	return filterEnv(os.Environ(), key)
}
