package ui

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/shhac/prtea/internal/claude"
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

// fetchPRsCmd returns a command that fetches both PR lists.
func fetchPRsCmd(client GitHubService) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()

		toReview, err := client.GetPRsForReview(ctx)
		if err != nil {
			return PRsErrorMsg{Err: err}
		}

		myPRs, err := client.GetMyPRs(ctx)
		if err != nil {
			return PRsErrorMsg{Err: err}
		}

		return PRsLoadedMsg{
			ToReview: toReview,
			MyPRs:    myPRs,
		}
	}
}

// convertPRItems converts github.PRItem slice to list.Item slice.
func convertPRItems(prs []github.PRItem) []list.Item {
	items := make([]list.Item, len(prs))
	for i, pr := range prs {
		items[i] = PRItem{
			number:   pr.Number,
			title:    pr.Title,
			repo:     pr.Repo.Name,
			owner:    pr.Repo.Owner,
			repoFull: pr.Repo.FullName,
			author:   pr.Author.Login,
			htmlURL:  pr.HTMLURL,
		}
	}
	return items
}

// pollTickCmd returns a command that fires after the given interval to trigger background polling.
func pollTickCmd(interval time.Duration) tea.Cmd {
	return tea.Tick(interval, func(t time.Time) tea.Msg {
		return pollTickMsg{}
	})
}

// pollFetchPRsCmd returns a command that fetches PR lists for background polling.
// Errors are silently ignored — the next tick will retry.
func pollFetchPRsCmd(client GitHubService) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		toReview, err := client.GetPRsForReview(ctx)
		if err != nil {
			return nil
		}
		myPRs, err := client.GetMyPRs(ctx)
		if err != nil {
			return nil
		}
		return pollPRsLoadedMsg{
			ToReview: toReview,
			MyPRs:    myPRs,
		}
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
		_ = cmd.Start()
		return nil
	}
}

// approvePRCmd returns a command that approves a PR.
func approvePRCmd(client GitHubService, owner, repo string, number int) tea.Cmd {
	return func() tea.Msg {
		err := client.ApprovePR(context.Background(), owner, repo, number, "")
		if err != nil {
			return PRApproveErrMsg{PRNumber: number, Err: err}
		}
		return PRApproveDoneMsg{PRNumber: number}
	}
}

// closePRCmd returns a command that closes a PR without merging.
func closePRCmd(client GitHubService, owner, repo string, number int) tea.Cmd {
	return func() tea.Msg {
		err := client.ClosePR(context.Background(), owner, repo, number)
		if err != nil {
			return PRCloseErrMsg{PRNumber: number, Err: err}
		}
		return PRCloseDoneMsg{PRNumber: number}
	}
}

// submitReviewCmd returns a command that submits a PR review, optionally with inline comments.
func submitReviewCmd(client GitHubService, owner, repo string, number int, action ReviewAction, body string, inlineComments []claude.InlineReviewComment) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		var err error

		// If there are inline comments, use the REST API for the full review
		if len(inlineComments) > 0 {
			eventMap := map[ReviewAction]string{
				ReviewApprove:        "APPROVE",
				ReviewComment:        "COMMENT",
				ReviewRequestChanges: "REQUEST_CHANGES",
			}
			comments := make([]github.ReviewCommentPayload, len(inlineComments))
			for i, c := range inlineComments {
				side := c.Side
				if side == "" {
					side = "RIGHT"
				}
				payload := github.ReviewCommentPayload{
					Path: c.Path,
					Line: c.Line,
					Side: side,
					Body: c.Body,
				}
				if c.StartLine > 0 {
					payload.StartLine = c.StartLine
					startSide := c.StartSide
					if startSide == "" {
						startSide = side
					}
					payload.StartSide = startSide
				}
				comments[i] = payload
			}
			err = client.SubmitReviewWithComments(ctx, owner, repo, number, eventMap[action], body, comments)
		} else {
			// No inline comments — use simple gh pr review
			switch action {
			case ReviewApprove:
				err = client.ApprovePR(ctx, owner, repo, number, body)
			case ReviewComment:
				err = client.CommentReviewPR(ctx, owner, repo, number, body)
			case ReviewRequestChanges:
				err = client.RequestChangesPR(ctx, owner, repo, number, body)
			}
		}

		if err != nil {
			return ReviewSubmitErrMsg{PRNumber: number, Err: err}
		}
		return ReviewSubmitDoneMsg{PRNumber: number, Action: action}
	}
}

// aiReviewCmd returns a command that runs Claude to generate an AI review with inline comments.
func aiReviewCmd(analyzer *claude.Analyzer, pr *SelectedPR, files []github.PRFile) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()

		diffContent := buildDiffContent(files)

		input := claude.ReviewInput{
			Owner:       pr.Owner,
			Repo:        pr.Repo,
			PRNumber:    pr.Number,
			PRTitle:     pr.Title,
			PRBody:      "", // TODO: include PR body when available
			DiffContent: diffContent,
		}

		result, err := analyzer.AnalyzeForReview(ctx, input, nil)
		if err != nil {
			return AIReviewErrorMsg{PRNumber: pr.Number, Err: err}
		}

		return AIReviewCompleteMsg{
			PRNumber: pr.Number,
			Result:   result,
		}
	}
}

// listenForChatStream returns a tea.Cmd that reads the next message from the streaming channel.
func listenForChatStream(ch chatStreamChan) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return nil
		}
		return msg
	}
}

// listenForAnalysisStream returns a tea.Cmd that reads the next message from the analysis streaming channel.
func listenForAnalysisStream(ch analysisStreamChan) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return nil
		}
		return msg
	}
}

// -- Context builders --

// buildChatContext constructs the PR context string for chat from metadata + diff.
func buildChatContext(pr *SelectedPR, files []github.PRFile) string {
	var b strings.Builder
	fmt.Fprintf(&b, "PR #%d: \"%s\" in %s/%s\n", pr.Number, pr.Title, pr.Owner, pr.Repo)
	if len(files) > 0 {
		b.WriteString("\nChanges in this PR:\n\n")
		b.WriteString(buildDiffContent(files))
	} else {
		b.WriteString("\n(Diff not yet loaded)")
	}
	return b.String()
}

// buildSelectedHunkContext constructs PR context with selected hunks as the primary
// focus, plus a brief file list for broader context.
func buildSelectedHunkContext(pr *SelectedPR, files []github.PRFile, selectedDiff string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "PR #%d: \"%s\" in %s/%s\n", pr.Number, pr.Title, pr.Owner, pr.Repo)

	// Include file list for broader context
	if len(files) > 0 {
		b.WriteString("\nFiles changed in this PR: ")
		for i, f := range files {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(f.Filename)
		}
		b.WriteString("\n")
	}

	b.WriteString("\nThe user selected the following hunks to discuss:\n\n")
	b.WriteString(selectedDiff)
	return b.String()
}

// buildDiffContent constructs a unified diff string from PR files.
func buildDiffContent(files []github.PRFile) string {
	var b strings.Builder
	for _, f := range files {
		b.WriteString(fmt.Sprintf("--- a/%s\n", f.Filename))
		b.WriteString(fmt.Sprintf("+++ b/%s\n", f.Filename))
		if f.Patch != "" {
			b.WriteString(f.Patch)
			b.WriteString("\n")
		} else {
			b.WriteString("(binary or too large to display)\n")
		}
		b.WriteString("\n")
	}
	return b.String()
}

// diffContentHash computes a short hash of the diff content for cache staleness checks.
func diffContentHash(files []github.PRFile) string {
	h := sha256.New()
	for _, f := range files {
		h.Write([]byte(f.Filename))
		h.Write([]byte(f.Patch))
	}
	return fmt.Sprintf("%x", h.Sum(nil))[:16]
}
