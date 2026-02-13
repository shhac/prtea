package github

import (
	"context"
	"fmt"
	"strings"

	gh "github.com/google/go-github/v68/github"
)

// GetPRsForReview returns open PRs where the authenticated user is requested as a reviewer.
func (c *Client) GetPRsForReview(ctx context.Context) ([]PRItem, error) {
	query := fmt.Sprintf("is:open is:pr review-requested:%s archived:false", c.username)
	return c.searchPRs(ctx, query)
}

// GetMyPRs returns open PRs authored by the authenticated user.
func (c *Client) GetMyPRs(ctx context.Context) ([]PRItem, error) {
	query := fmt.Sprintf("is:open is:pr author:%s archived:false", c.username)
	return c.searchPRs(ctx, query)
}

// GetPRDetail fetches full PR information including mergeable state and behind-by count.
func (c *Client) GetPRDetail(ctx context.Context, owner, repo string, number int) (*PRDetail, error) {
	pr, _, err := c.gh.PullRequests.Get(ctx, owner, repo, number)
	if err != nil {
		return nil, fmt.Errorf("failed to get PR #%d: %w", number, err)
	}

	behindBy := 0
	comparison, _, err := c.gh.Repositories.CompareCommits(ctx, owner, repo, pr.GetHead().GetRef(), pr.GetBase().GetRef(), nil)
	if err != nil {
		behindBy = -1
	} else {
		behindBy = comparison.GetAheadBy()
	}

	return &PRDetail{
		Number:         pr.GetNumber(),
		Title:          pr.GetTitle(),
		Body:           pr.GetBody(),
		HTMLURL:        pr.GetHTMLURL(),
		Author:         userFromGH(pr.GetUser()),
		Repo:           Repo{Owner: owner, Name: repo, FullName: owner + "/" + repo},
		BaseBranch:     pr.GetBase().GetRef(),
		HeadBranch:     pr.GetHead().GetRef(),
		HeadSHA:        pr.GetHead().GetSHA(),
		Mergeable:      pr.GetMergeable(),
		MergeableState: pr.GetMergeableState(),
		BehindBy:       behindBy,
	}, nil
}

// searchPRs performs a search query and enriches each result with full PR data.
func (c *Client) searchPRs(ctx context.Context, query string) ([]PRItem, error) {
	opts := &gh.SearchOptions{
		Sort:  "created",
		Order: "asc",
		ListOptions: gh.ListOptions{
			PerPage: 100,
		},
	}

	result, _, err := c.gh.Search.Issues(ctx, query, opts)
	if err != nil {
		return nil, fmt.Errorf("search failed for query %q: %w", query, err)
	}

	prs := make([]PRItem, 0, len(result.Issues))
	for _, issue := range result.Issues {
		owner, repo := parseRepoURL(issue.GetRepositoryURL())
		if owner == "" {
			continue
		}

		prData, _, err := c.gh.PullRequests.Get(ctx, owner, repo, issue.GetNumber())
		if err != nil {
			continue
		}

		labels := make([]Label, 0, len(issue.Labels))
		for _, l := range issue.Labels {
			labels = append(labels, Label{
				Name:  l.GetName(),
				Color: l.GetColor(),
			})
		}

		prs = append(prs, PRItem{
			ID:           issue.GetID(),
			Number:       issue.GetNumber(),
			Title:        issue.GetTitle(),
			HTMLURL:      issue.GetHTMLURL(),
			Repo:         Repo{Owner: owner, Name: repo, FullName: owner + "/" + repo},
			Author:       userFromGH(issue.GetUser()),
			Labels:       labels,
			Draft:        prData.GetDraft(),
			CreatedAt:    issue.GetCreatedAt().Time,
			Additions:    prData.GetAdditions(),
			Deletions:    prData.GetDeletions(),
			ChangedFiles: prData.GetChangedFiles(),
		})
	}

	return prs, nil
}

// parseRepoURL extracts owner and repo from a repository_url like
// "https://api.github.com/repos/owner/repo".
func parseRepoURL(repoURL string) (string, string) {
	parts := strings.Split(repoURL, "/repos/")
	if len(parts) != 2 {
		return "", ""
	}
	segments := strings.SplitN(parts[1], "/", 2)
	if len(segments) != 2 {
		return "", ""
	}
	return segments[0], segments[1]
}

// userFromGH converts a go-github User to our User type.
func userFromGH(u *gh.User) User {
	if u == nil {
		return User{Login: "unknown"}
	}
	return User{
		Login:     u.GetLogin(),
		AvatarURL: u.GetAvatarURL(),
	}
}
