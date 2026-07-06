package ui

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/shhac/prtea/internal/github"
	"github.com/shhac/prtea/internal/notify"
)

// initGHClientCmd creates the GitHub client in a goroutine.
func initGHClientCmd() tea.Msg {
	client, err := github.NewClient()
	if err != nil {
		return GHClientErrorMsg{Err: err}
	}
	return GHClientReadyMsg{Client: client}
}

// fetchPRData fetches both PR lists from GitHub. Shared by foreground and poll fetchers.
func fetchPRData(client GitHubService) ([]github.PRItem, []github.PRItem, error) {
	ctx := context.Background()
	toReview, err := client.GetPRsForReview(ctx)
	if err != nil {
		return nil, nil, err
	}
	myPRs, err := client.GetMyPRs(ctx)
	if err != nil {
		return nil, nil, err
	}
	return toReview, myPRs, nil
}

// fetchPRsCmd returns a command that fetches both PR lists.
func fetchPRsCmd(client GitHubService) tea.Cmd {
	return func() tea.Msg {
		toReview, myPRs, err := fetchPRData(client)
		if err != nil {
			return PRsErrorMsg{Err: err}
		}
		return PRsLoadedMsg{ToReview: toReview, MyPRs: myPRs}
	}
}

// convertPRItems converts github.PRItem slice to list.Item slice.
func convertPRItems(prs []github.PRItem) []list.Item {
	items := make([]list.Item, len(prs))
	for i, pr := range prs {
		items[i] = PRItem{
			number:         pr.Number,
			title:          pr.Title,
			repo:           pr.Repo.Name,
			owner:          pr.Repo.Owner,
			repoFull:       pr.Repo.FullName,
			author:         pr.Author.Login,
			htmlURL:        pr.HTMLURL,
			reviewDecision: pr.ReviewDecision,
			isDraft:        pr.Draft,
		}
	}
	return items
}

// fetchReviewDecisionsCmd fetches review decisions for a batch of PRs asynchronously.
// This runs in the background after the PR list loads — it does not block UI interactivity.
func fetchReviewDecisionsCmd(client GitHubService, prs []github.PRItem) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		decisions, _ := client.GetReviewDecisions(ctx, prs)
		if len(decisions) == 0 {
			return nil
		}
		return PRReviewDecisionsMsg{Decisions: decisions}
	}
}

// pollTickCmd returns a command that fires after the given interval to trigger background polling.
func pollTickCmd(interval time.Duration) tea.Cmd {
	return tea.Tick(interval, func(t time.Time) tea.Msg {
		return pollTickMsg{}
	})
}

// pollFetchPRsCmd returns a command that fetches PR lists for background polling.
// Errors are surfaced as pollErrorMsg so the user sees transient issues.
func pollFetchPRsCmd(client GitHubService) tea.Cmd {
	return func() tea.Msg {
		toReview, myPRs, err := fetchPRData(client)
		if err != nil {
			return pollErrorMsg{Err: err}
		}
		return pollPRsLoadedMsg{ToReview: toReview, MyPRs: myPRs}
	}
}

// prKey returns a unique string key for a PR across repos (owner/repo#number).
func prKey(owner, repo string, number int) string {
	return fmt.Sprintf("%s/%s#%d", owner, repo, number)
}

// notifyNewPRsCmd sends OS notifications for newly detected PRs.
// If more than threshold new PRs arrived at once, sends a single summary notification.
func notifyNewPRsCmd(newPRs []github.PRItem, threshold int) tea.Cmd {
	return func() tea.Msg {
		if len(newPRs) > threshold {
			_ = notify.Send(
				"prtea",
				fmt.Sprintf("%d new PRs for review", len(newPRs)),
			)
		} else {
			for _, pr := range newPRs {
				_ = notify.Send(
					"prtea: New PR for review",
					fmt.Sprintf("#%d %s by %s in %s", pr.Number, pr.Title, pr.Author.Login, pr.Repo.Name),
				)
			}
		}
		return nil
	}
}

// fetchAllPRDataCmds returns the full batch of fetches that load a PR:
// diff, detail, comments, CI status, and reviews. refreshSelectedPR counts
// this batch to know when a refresh has completed.
func fetchAllPRDataCmds(client GitHubService, owner, repo string, number int) []tea.Cmd {
	return []tea.Cmd{
		fetchDiffCmd(client, owner, repo, number),
		fetchPRDetailCmd(client, owner, repo, number),
		fetchCommentsCmd(client, owner, repo, number),
		fetchCIStatusCmd(client, owner, repo, number),
		fetchReviewsCmd(client, owner, repo, number),
	}
}

// fetchDiffCmd returns a command that fetches PR file diffs.
func fetchDiffCmd(client GitHubService, owner, repo string, number int) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		files, err := client.GetPRFiles(ctx, owner, repo, number)
		if err != nil {
			return DiffLoadedMsg{PRNumber: number, Err: err}
		}
		return DiffLoadedMsg{PRNumber: number, Files: files}
	}
}

// fetchPRDetailCmd returns a command that fetches PR detail (title, body, etc.).
func fetchPRDetailCmd(client GitHubService, owner, repo string, number int) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		detail, err := client.GetPRDetail(ctx, owner, repo, number)
		if err != nil {
			return PRDetailLoadedMsg{PRNumber: number, Err: err}
		}
		return PRDetailLoadedMsg{PRNumber: number, Detail: detail}
	}
}

// fetchCommentsCmd returns a command that fetches PR comments (issue-level + inline).
func fetchCommentsCmd(client GitHubService, owner, repo string, number int) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()

		comments, commErr := client.GetComments(ctx, owner, repo, number)
		inline, inlineErr := client.GetInlineComments(ctx, owner, repo, number)

		// Report first error if any
		if commErr != nil {
			return CommentsLoadedMsg{PRNumber: number, Err: commErr}
		}
		if inlineErr != nil {
			return CommentsLoadedMsg{PRNumber: number, Err: inlineErr}
		}

		return CommentsLoadedMsg{
			PRNumber:       number,
			Comments:       comments,
			InlineComments: inline,
		}
	}
}

// fetchCIStatusCmd returns a command that fetches CI check status for a PR.
func fetchCIStatusCmd(client GitHubService, owner, repo string, number int) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		status, err := client.GetCIStatus(ctx, owner, repo, "", number)
		return CIStatusLoadedMsg{PRNumber: number, Status: status, Err: err}
	}
}

// fetchReviewsCmd returns a command that fetches review status for a PR.
func fetchReviewsCmd(client GitHubService, owner, repo string, number int) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		summary, err := client.GetReviews(ctx, owner, repo, number)
		return ReviewsLoadedMsg{PRNumber: number, Summary: summary, Err: err}
	}
}

// rerunFailedCICmd returns a command that re-runs failed GitHub Actions workflows.
func rerunFailedCICmd(client GitHubService, owner, repo string, number int, runIDs []int64) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		for _, id := range runIDs {
			if err := client.RerunWorkflow(ctx, owner, repo, id, true); err != nil {
				return CIRerunErrMsg{PRNumber: number, Err: err}
			}
		}
		return CIRerunDoneMsg{PRNumber: number, Count: len(runIDs)}
	}
}

// openBrowserCmd returns a command that opens a URL in the default browser.
func openBrowserCmd(url string) tea.Cmd {
	return func() tea.Msg {
		var cmd *exec.Cmd
		switch runtime.GOOS {
		case "darwin":
			cmd = exec.Command("open", url)
		case "windows":
			cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
		default: // linux, freebsd, etc.
			cmd = exec.Command("xdg-open", url)
		}
		if err := cmd.Start(); err == nil {
			go cmd.Wait() // reap the child process to avoid zombies
		}
		return nil
	}
}

// submitSimpleReview dispatches a body-only review (no inline comments) to
// the matching gh pr review operation. Shared by user-submitted reviews and
// AI-proposed submit_review actions so the two paths cannot drift.
func submitSimpleReview(ctx context.Context, client GitHubService, owner, repo string, number int, action ReviewAction, body string) error {
	switch action {
	case ReviewApprove:
		return client.ApprovePR(ctx, owner, repo, number, body)
	case ReviewComment:
		return client.CommentReviewPR(ctx, owner, repo, number, body)
	case ReviewRequestChanges:
		return client.RequestChangesPR(ctx, owner, repo, number, body)
	default:
		return fmt.Errorf("unknown review action %d", action)
	}
}

// submitReviewCmd returns a command that submits a PR review, optionally with inline comments.
func submitReviewCmd(client GitHubService, owner, repo string, number int, action ReviewAction, body string, inlineComments []github.ReviewCommentPayload) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		var err error

		// Inline comments require the REST API for the full review
		if len(inlineComments) > 0 {
			err = client.SubmitReviewWithComments(ctx, owner, repo, number, action.APIEvent(), body, inlineComments)
		} else {
			err = submitSimpleReview(ctx, client, owner, repo, number, action, body)
		}

		if err != nil {
			return ReviewSubmitErrMsg{PRNumber: number, Err: err}
		}
		return ReviewSubmitDoneMsg{PRNumber: number, Action: action}
	}
}

// replyToCommentCmd posts a reply to an existing GitHub review comment thread.
func replyToCommentCmd(client GitHubService, owner, repo string, prNumber int, commentID int64, body string) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		err := client.ReplyToComment(ctx, owner, repo, prNumber, commentID, body)
		return InlineCommentReplyDoneMsg{Err: err}
	}
}
