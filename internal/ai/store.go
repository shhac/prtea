package ai

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Message roles stored in a thread transcript.
const (
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleActivity  = "activity" // command/thinking feed lines, kept for redisplay
)

// Message is a single transcript entry for redisplay. The real conversation
// state lives in the codex session; this is only what the chat panel shows.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// CachedThread persists a PR's thread ID and display transcript.
type CachedThread struct {
	ThreadID  string    `json:"threadId"`
	Messages  []Message `json:"messages"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// ThreadStore manages file-based persistence of PR threads.
type ThreadStore struct {
	cacheDir string
}

// NewThreadStore creates a store that persists threads in the given directory.
func NewThreadStore(cacheDir string) *ThreadStore {
	return &ThreadStore{cacheDir: cacheDir}
}

// Get loads a cached thread for a PR. Returns nil if not found.
func (s *ThreadStore) Get(owner, repo string, number int) (*CachedThread, error) {
	data, err := os.ReadFile(s.cachePath(owner, repo, number))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read thread cache: %w", err)
	}

	var cached CachedThread
	if err := json.Unmarshal(data, &cached); err != nil {
		return nil, fmt.Errorf("failed to parse thread cache: %w", err)
	}
	return &cached, nil
}

// Put saves a thread to disk.
func (s *ThreadStore) Put(owner, repo string, number int, threadID string, messages []Message) error {
	if threadID == "" && len(messages) == 0 {
		return nil
	}

	if err := os.MkdirAll(s.cacheDir, 0o755); err != nil {
		return fmt.Errorf("failed to create thread cache directory: %w", err)
	}

	data, err := json.MarshalIndent(CachedThread{
		ThreadID:  threadID,
		Messages:  messages,
		UpdatedAt: time.Now(),
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal thread: %w", err)
	}

	path := s.cachePath(owner, repo, number)
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return fmt.Errorf("failed to write temp thread cache: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to rename thread cache: %w", err)
	}
	return nil
}

// Delete removes a cached thread for a PR.
func (s *ThreadStore) Delete(owner, repo string, number int) error {
	err := os.Remove(s.cachePath(owner, repo, number))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete thread cache: %w", err)
	}
	return nil
}

func (s *ThreadStore) cachePath(owner, repo string, number int) string {
	return filepath.Join(s.cacheDir, fmt.Sprintf("%s_%s_%d.json", owner, repo, number))
}
