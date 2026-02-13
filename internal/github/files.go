package github

import (
	"context"
	"fmt"
)

// ghFile is the JSON shape returned by the pulls files API.
type ghFile struct {
	Filename  string `json:"filename"`
	Status    string `json:"status"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Patch     string `json:"patch"`
}

// GetPRFiles returns all changed files in a PR with their patches.
func (c *Client) GetPRFiles(ctx context.Context, owner, repo string, number int) ([]PRFile, error) {
	var files []ghFile
	endpoint := fmt.Sprintf("repos/%s/%s/pulls/%d/files", owner, repo, number)
	if err := ghJSON(ctx, &files, "api", endpoint, "--paginate"); err != nil {
		return nil, fmt.Errorf("failed to list files for PR #%d: %w", number, err)
	}

	result := make([]PRFile, 0, len(files))
	for _, f := range files {
		result = append(result, PRFile{
			Filename:  f.Filename,
			Status:    f.Status,
			Additions: f.Additions,
			Deletions: f.Deletions,
			Patch:     f.Patch,
		})
	}
	return result, nil
}
