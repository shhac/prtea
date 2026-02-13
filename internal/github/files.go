package github

import (
	"context"
	"fmt"

	gh "github.com/google/go-github/v68/github"
)

// GetPRFiles returns all changed files in a PR with their patches.
func (c *Client) GetPRFiles(ctx context.Context, owner, repo string, number int) ([]PRFile, error) {
	var allFiles []PRFile
	opts := &gh.ListOptions{PerPage: 100}

	for {
		files, resp, err := c.gh.PullRequests.ListFiles(ctx, owner, repo, number, opts)
		if err != nil {
			return nil, fmt.Errorf("failed to list files for PR #%d: %w", number, err)
		}

		for _, f := range files {
			allFiles = append(allFiles, PRFile{
				Filename:  f.GetFilename(),
				Status:    f.GetStatus(),
				Additions: f.GetAdditions(),
				Deletions: f.GetDeletions(),
				Patch:     f.GetPatch(),
			})
		}

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return allFiles, nil
}
