package github

import (
	"context"
	"fmt"
	"sort"

	gh "github.com/google/go-github/v68/github"
)

// GetComments fetches issue-level comments on a PR (general conversation).
func (c *Client) GetComments(ctx context.Context, owner, repo string, number int) ([]Comment, error) {
	var allComments []Comment
	opts := &gh.IssueListCommentsOptions{
		ListOptions: gh.ListOptions{PerPage: 100},
	}

	for {
		comments, resp, err := c.gh.Issues.ListComments(ctx, owner, repo, number, opts)
		if err != nil {
			return nil, fmt.Errorf("failed to list comments for PR #%d: %w", number, err)
		}

		for _, c := range comments {
			allComments = append(allComments, Comment{
				ID:        c.GetID(),
				Author:    userFromGH(c.GetUser()),
				Body:      c.GetBody(),
				CreatedAt: c.GetCreatedAt().Time,
				UpdatedAt: c.GetUpdatedAt().Time,
			})
		}

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	sort.Slice(allComments, func(i, j int) bool {
		return allComments[i].CreatedAt.Before(allComments[j].CreatedAt)
	})

	return allComments, nil
}

// GetInlineComments fetches review comments attached to specific file lines.
func (c *Client) GetInlineComments(ctx context.Context, owner, repo string, number int) ([]InlineComment, error) {
	var allComments []InlineComment
	opts := &gh.PullRequestListCommentsOptions{
		ListOptions: gh.ListOptions{PerPage: 100},
	}

	for {
		comments, resp, err := c.gh.PullRequests.ListComments(ctx, owner, repo, number, opts)
		if err != nil {
			return nil, fmt.Errorf("failed to list inline comments for PR #%d: %w", number, err)
		}

		for _, c := range comments {
			line := c.GetLine()
			if line == 0 {
				line = c.GetOriginalLine()
			}

			var inReplyToID int64
			if c.InReplyTo != nil {
				inReplyToID = c.GetInReplyTo()
			}

			allComments = append(allComments, InlineComment{
				ID:          c.GetID(),
				Author:      userFromGH(c.GetUser()),
				Body:        c.GetBody(),
				CreatedAt:   c.GetCreatedAt().Time,
				Path:        c.GetPath(),
				Line:        line,
				Side:        c.GetSide(),
				InReplyToID: inReplyToID,
				Outdated:    c.GetPosition() == 0,
			})
		}

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return allComments, nil
}
