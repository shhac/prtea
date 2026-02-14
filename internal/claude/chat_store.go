package claude

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ChatStore manages file-based persistence of chat sessions.
type ChatStore struct {
	cacheDir string
}

// NewChatStore creates a store that persists chat sessions in the given directory.
func NewChatStore(cacheDir string) *ChatStore {
	return &ChatStore{cacheDir: cacheDir}
}

// CachedChatSession wraps a chat session with persistence metadata.
type CachedChatSession struct {
	Messages  []ChatMessage `json:"messages"`
	UpdatedAt time.Time     `json:"updatedAt"`
}

// Get loads a cached chat session for a PR. Returns nil if not found.
func (s *ChatStore) Get(owner, repo string, number int) (*CachedChatSession, error) {
	path := s.cachePath(owner, repo, number)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read chat cache: %w", err)
	}

	var cached CachedChatSession
	if err := json.Unmarshal(data, &cached); err != nil {
		return nil, fmt.Errorf("failed to parse chat cache: %w", err)
	}

	return &cached, nil
}

// Put saves a chat session to disk.
func (s *ChatStore) Put(owner, repo string, number int, messages []ChatMessage) error {
	if len(messages) == 0 {
		return nil // nothing to persist
	}

	if err := os.MkdirAll(s.cacheDir, 0o755); err != nil {
		return fmt.Errorf("failed to create chat cache directory: %w", err)
	}

	cached := CachedChatSession{
		Messages:  messages,
		UpdatedAt: time.Now(),
	}

	data, err := json.MarshalIndent(cached, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal chat session: %w", err)
	}

	path := s.cachePath(owner, repo, number)

	// Write atomically: temp file + rename
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return fmt.Errorf("failed to write temp chat cache: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to rename chat cache: %w", err)
	}

	return nil
}

// Delete removes a cached chat session for a PR.
func (s *ChatStore) Delete(owner, repo string, number int) error {
	path := s.cachePath(owner, repo, number)
	err := os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete chat cache: %w", err)
	}
	return nil
}

func (s *ChatStore) cachePath(owner, repo string, number int) string {
	filename := fmt.Sprintf("%s_%s_%d.json", owner, repo, number)
	return filepath.Join(s.cacheDir, filename)
}
