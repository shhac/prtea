package github

import (
	"context"
	"fmt"
	"sort"
	"time"
)

// ghComment is the JSON shape for issue-level comments from gh pr view.
type ghComment struct {
	ID     string `json:"id"`
	Author struct {
		Login string `json:"login"`
	} `json:"author"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"createdAt"`
	URL       string    `json:"url"`
}

// ghPRComments is the JSON shape from gh pr view --json comments.
type ghPRComments struct {
	Comments []ghComment `json:"comments"`
}

// ghInlineComment is the JSON shape from the pulls comments API.
type ghInlineComment struct {
	ID     int64 `json:"id"`
	User   struct {
		Login     string `json:"login"`
		AvatarURL string `json:"avatar_url"`
	} `json:"user"`
	Body        string    `json:"body"`
	CreatedAt   time.Time `json:"created_at"`
	Path        string    `json:"path"`
	Line        int       `json:"line"`
	StartLine   *int      `json:"start_line"`
	OriginalLine int      `json:"original_line"`
	Side        string    `json:"side"`
	InReplyToID *int64    `json:"in_reply_to_id"`
	Position    *int      `json:"position"`
}

// GetComments fetches issue-level comments on a PR (general conversation).
func (c *Client) GetComments(ctx context.Context, owner, repo string, number int) ([]Comment, error) {
	repoFlag := owner + "/" + repo

	var data ghPRComments
	err := c.ghJSON(ctx, &data,
		"pr", "view", fmt.Sprintf("%d", number),
		"-R", repoFlag,
		"--json", "comments",
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list comments for PR #%d: %w", number, err)
	}

	comments := make([]Comment, 0, len(data.Comments))
	for _, c := range data.Comments {
		comments = append(comments, Comment{
			Author:    User{Login: c.Author.Login},
			Body:      c.Body,
			CreatedAt: c.CreatedAt,
		})
	}

	sort.Slice(comments, func(i, j int) bool {
		return comments[i].CreatedAt.Before(comments[j].CreatedAt)
	})

	return comments, nil
}

// GetInlineComments fetches review comments attached to specific file lines.
func (c *Client) GetInlineComments(ctx context.Context, owner, repo string, number int) ([]InlineComment, error) {
	var raw []ghInlineComment
	endpoint := fmt.Sprintf("repos/%s/%s/pulls/%d/comments", owner, repo, number)
	if err := c.ghJSON(ctx, &raw, "api", endpoint, "--paginate"); err != nil {
		return nil, fmt.Errorf("failed to list inline comments for PR #%d: %w", number, err)
	}

	comments := make([]InlineComment, 0, len(raw))
	for _, c := range raw {
		line := c.Line
		if line == 0 {
			line = c.OriginalLine
		}

		var inReplyToID int64
		if c.InReplyToID != nil {
			inReplyToID = *c.InReplyToID
		}

		var startLine int
		if c.StartLine != nil {
			startLine = *c.StartLine
		}

		outdated := c.Position == nil || *c.Position == 0

		comments = append(comments, InlineComment{
			ID:          c.ID,
			Author:      User{Login: c.User.Login, AvatarURL: c.User.AvatarURL},
			Body:        c.Body,
			CreatedAt:   c.CreatedAt,
			Path:        c.Path,
			Line:        line,
			StartLine:   startLine,
			Side:        c.Side,
			InReplyToID: inReplyToID,
			Outdated:    outdated,
		})
	}

	return comments, nil
}
