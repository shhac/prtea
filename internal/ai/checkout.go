package ai

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// FindLocalCheckout returns the root of the current working directory's git
// repository if its origin remote matches owner/repo, otherwise "".
// Used to give codex filesystem context when reviewing a PR for the repo
// the user launched prtea from.
func FindLocalCheckout(owner, repo string) string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}

	root, err := exec.Command("git", "-C", cwd, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return ""
	}

	origin, err := exec.Command("git", "-C", cwd, "remote", "get-url", "origin").Output()
	if err != nil {
		return ""
	}

	if !remoteMatches(strings.TrimSpace(string(origin)), owner, repo) {
		return ""
	}
	return strings.TrimSpace(string(root))
}

// remoteMatches reports whether a git remote URL points at owner/repo.
// Handles https://github.com/owner/repo(.git) and git@github.com:owner/repo(.git).
func remoteMatches(remoteURL, owner, repo string) bool {
	url := strings.ToLower(strings.TrimSuffix(remoteURL, ".git"))
	want := strings.ToLower(fmt.Sprintf("%s/%s", owner, repo))
	return strings.HasSuffix(url, "/"+want) || strings.HasSuffix(url, ":"+want)
}
