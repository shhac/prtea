package claude

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// AnalysisStore manages file-based caching of PR analysis results.
type AnalysisStore struct {
	cacheDir string
}

// NewAnalysisStore creates a store that caches analyses in the given directory.
func NewAnalysisStore(cacheDir string) *AnalysisStore {
	return &AnalysisStore{cacheDir: cacheDir}
}

// Get loads a cached analysis for a PR. Returns nil if not found.
func (s *AnalysisStore) Get(owner, repo string, number int) (*CachedAnalysis, error) {
	path := s.cachePath(owner, repo, number)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read cache file: %w", err)
	}

	var cached CachedAnalysis
	if err := json.Unmarshal(data, &cached); err != nil {
		return nil, fmt.Errorf("failed to parse cache file: %w", err)
	}

	return &cached, nil
}

// Put saves an analysis result to the cache.
func (s *AnalysisStore) Put(owner, repo string, number int, headSHA string, result *AnalysisResult) error {
	if err := os.MkdirAll(s.cacheDir, 0o755); err != nil {
		return fmt.Errorf("failed to create cache directory: %w", err)
	}

	cached := CachedAnalysis{
		HeadSHA:    headSHA,
		AnalyzedAt: time.Now(),
		Result:     result,
	}

	data, err := json.MarshalIndent(cached, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal analysis: %w", err)
	}

	path := s.cachePath(owner, repo, number)

	// Write atomically: temp file + rename
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return fmt.Errorf("failed to write temp cache file: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to rename cache file: %w", err)
	}

	return nil
}

// IsStale returns true if the cached analysis doesn't match the current head SHA.
func (s *AnalysisStore) IsStale(cached *CachedAnalysis, currentHeadSHA string) bool {
	if cached == nil {
		return true
	}
	return cached.HeadSHA != currentHeadSHA
}

func (s *AnalysisStore) cachePath(owner, repo string, number int) string {
	filename := fmt.Sprintf("%s_%s_%d.json", owner, repo, number)
	return filepath.Join(s.cacheDir, filename)
}
