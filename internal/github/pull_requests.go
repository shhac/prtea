package github

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// ghSearchPR is the JSON shape returned by gh search prs.
type ghSearchPR struct {
	Number     int    `json:"number"`
	Title      string `json:"title"`
	URL        string `json:"url"`
	CreatedAt  time.Time `json:"createdAt"`
	IsDraft    bool   `json:"isDraft"`
	Additions  int    `json:"additions"`
	Deletions  int    `json:"deletions"`
	ChangedFiles int  `json:"changedFiles"`
	Author     struct {
		Login string `json:"login"`
	} `json:"author"`
	Repository struct {
		Name          string `json:"name"`
		NameWithOwner string `json:"nameWithOwner"`
	} `json:"repository"`
	Labels []struct {
		Name  string `json:"name"`
		Color string `json:"color"`
	} `json:"labels"`
}

// ghPRView is the JSON shape returned by gh pr view.
type ghPRView struct {
	Number         int    `json:"number"`
	Title          string `json:"title"`
	Body           string `json:"body"`
	URL            string `json:"url"`
	Mergeable      string `json:"mergeable"` // "MERGEABLE", "CONFLICTING", "UNKNOWN"
	MergeStateStatus string `json:"mergeStateStatus"`
	BaseRefName    string `json:"baseRefName"`
	HeadRefName    string `json:"headRefName"`
	HeadRefOid     string `json:"headRefOid"`
	Author         struct {
		Login string `json:"login"`
	} `json:"author"`
}

// ghCompare is the JSON shape from the compare API.
type ghCompare struct {
	AheadBy  int `json:"ahead_by"`
	BehindBy int `json:"behind_by"`
}

// GetPRsForReview returns open PRs where the authenticated user is requested as a reviewer.
func (c *Client) GetPRsForReview(ctx context.Context) ([]PRItem, error) {
	var results []ghSearchPR
	err := ghJSON(ctx, &results,
		"search", "prs",
		"--review-requested=@me",
		"--state=open",
		"--limit", "100",
		"--json", "number,title,url,createdAt,isDraft,additions,deletions,changedFiles,author,repository,labels",
	)
	if err != nil {
		return nil, fmt.Errorf("failed to search PRs for review: %w", err)
	}
	return convertSearchResults(results), nil
}

// GetMyPRs returns open PRs authored by the authenticated user.
func (c *Client) GetMyPRs(ctx context.Context) ([]PRItem, error) {
	var results []ghSearchPR
	err := ghJSON(ctx, &results,
		"search", "prs",
		"--author=@me",
		"--state=open",
		"--limit", "100",
		"--json", "number,title,url,createdAt,isDraft,additions,deletions,changedFiles,author,repository,labels",
	)
	if err != nil {
		return nil, fmt.Errorf("failed to search my PRs: %w", err)
	}
	return convertSearchResults(results), nil
}

// GetPRDetail fetches full PR information including mergeable state and behind-by count.
func (c *Client) GetPRDetail(ctx context.Context, owner, repo string, number int) (*PRDetail, error) {
	repoFlag := owner + "/" + repo

	var pr ghPRView
	err := ghJSON(ctx, &pr,
		"pr", "view", fmt.Sprintf("%d", number),
		"-R", repoFlag,
		"--json", "number,title,body,url,mergeable,mergeStateStatus,baseRefName,headRefName,headRefOid,author",
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get PR #%d: %w", number, err)
	}

	// Get behind-by count via compare API
	behindBy := 0
	var cmp ghCompare
	endpoint := fmt.Sprintf("repos/%s/%s/compare/%s...%s", owner, repo, pr.HeadRefName, pr.BaseRefName)
	if err := ghJSON(ctx, &cmp, "api", endpoint); err != nil {
		behindBy = -1
	} else {
		behindBy = cmp.AheadBy
	}

	return &PRDetail{
		Number:         pr.Number,
		Title:          pr.Title,
		Body:           pr.Body,
		HTMLURL:        pr.URL,
		Author:         User{Login: pr.Author.Login},
		Repo:           Repo{Owner: owner, Name: repo, FullName: repoFlag},
		BaseBranch:     pr.BaseRefName,
		HeadBranch:     pr.HeadRefName,
		HeadSHA:        pr.HeadRefOid,
		Mergeable:      pr.Mergeable == "MERGEABLE",
		MergeableState: pr.MergeStateStatus,
		BehindBy:       behindBy,
	}, nil
}

func convertSearchResults(results []ghSearchPR) []PRItem {
	prs := make([]PRItem, 0, len(results))
	for _, r := range results {
		owner, name := parseNameWithOwner(r.Repository.NameWithOwner)

		labels := make([]Label, 0, len(r.Labels))
		for _, l := range r.Labels {
			labels = append(labels, Label{Name: l.Name, Color: l.Color})
		}

		prs = append(prs, PRItem{
			Number:       r.Number,
			Title:        r.Title,
			HTMLURL:      r.URL,
			Repo:         Repo{Owner: owner, Name: name, FullName: r.Repository.NameWithOwner},
			Author:       User{Login: r.Author.Login},
			Labels:       labels,
			Draft:        r.IsDraft,
			CreatedAt:    r.CreatedAt,
			Additions:    r.Additions,
			Deletions:    r.Deletions,
			ChangedFiles: r.ChangedFiles,
		})
	}
	return prs
}

// parseNameWithOwner splits "owner/repo" into owner and repo.
func parseNameWithOwner(nameWithOwner string) (string, string) {
	parts := strings.SplitN(nameWithOwner, "/", 2)
	if len(parts) != 2 {
		return "", ""
	}
	return parts[0], parts[1]
}
