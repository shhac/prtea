package git

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// RepoExists checks if a git repository exists at the given path.
func RepoExists(path string) bool {
	info, err := os.Stat(filepath.Join(path, ".git"))
	return err == nil && info.IsDir()
}

// EnsureRepo clones a repository if it doesn't exist, or fetches if it does.
// Returns the path to the repository.
func EnsureRepo(reposPath, owner, repo, token string) (string, error) {
	repoPath := filepath.Join(reposPath, repo)

	if RepoExists(repoPath) {
		// Update remote URL with current token and fetch
		authURL := authRemoteURL(owner, repo, token)
		if err := runGit(repoPath, "remote", "set-url", "origin", authURL); err != nil {
			return "", fmt.Errorf("failed to update remote URL: %w", err)
		}
		if err := Fetch(repoPath); err != nil {
			return "", fmt.Errorf("failed to fetch: %w", err)
		}
		return repoPath, nil
	}

	// Clone
	if err := os.MkdirAll(reposPath, 0o755); err != nil {
		return "", fmt.Errorf("failed to create repos directory: %w", err)
	}

	authURL := authRemoteURL(owner, repo, token)
	cmd := exec.Command("git", "clone", authURL, repoPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("failed to clone %s/%s: %w\n%s", owner, repo, err, string(out))
	}

	return repoPath, nil
}

// Fetch runs git fetch origin in the given repo.
func Fetch(repoPath string) error {
	return runGit(repoPath, "fetch", "origin")
}

// GetHeadSHA returns the commit SHA for a branch reference.
func GetHeadSHA(repoPath, branch string) (string, error) {
	ref := "origin/" + branch
	cmd := exec.Command("git", "rev-parse", ref)
	cmd.Dir = repoPath
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get SHA for %s: %w", ref, err)
	}
	return strings.TrimSpace(string(out)), nil
}

func authRemoteURL(owner, repo, token string) string {
	return fmt.Sprintf("https://x-access-token:%s@github.com/%s/%s.git", token, owner, repo)
}

func runGit(repoPath string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = repoPath
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git %s failed: %w\n%s", strings.Join(args, " "), err, string(out))
	}
	return nil
}
