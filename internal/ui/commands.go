package ui

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/shhac/prtea/internal/ai"
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
func submitReviewCmd(client GitHubService, owner, repo string, number int, action ReviewAction, body string, inlineComments []github.ReviewCommentPayload) tea.Cmd {
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
				if c.Side == "" {
					c.Side = "RIGHT"
				}
				if c.StartLine > 0 && c.StartSide == "" {
					c.StartSide = c.Side
				}
				comments[i] = c
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

// replyToCommentCmd posts a reply to an existing GitHub review comment thread.
func replyToCommentCmd(client GitHubService, owner, repo string, prNumber int, commentID int64, body string) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		err := client.ReplyToComment(ctx, owner, repo, prNumber, commentID, body)
		return InlineCommentReplyDoneMsg{Err: err}
	}
}

// executeActionCmd executes a user-confirmed AI-proposed action via the
// GitHub client and reports the outcome.
func executeActionCmd(client GitHubService, owner, repo string, number int, action ai.Action) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		var err error
		switch action.Type {
		case ai.ActionPostComment:
			err = client.PostComment(ctx, owner, repo, number, action.Body)
		case ai.ActionReplyToComment:
			err = client.ReplyToComment(ctx, owner, repo, number, action.CommentID, action.Body)
		case ai.ActionSubmitReview:
			switch action.Event {
			case "approve":
				err = client.ApprovePR(ctx, owner, repo, number, action.Body)
			case "comment":
				err = client.CommentReviewPR(ctx, owner, repo, number, action.Body)
			case "request_changes":
				err = client.RequestChangesPR(ctx, owner, repo, number, action.Body)
			}
		default:
			err = fmt.Errorf("unknown action type %q", action.Type)
		}
		return AIActionResultMsg{Description: action.Describe(), Err: err}
	}
}

// listenForStream returns a tea.Cmd that reads the next message from a streaming channel.
func listenForStream(ch <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return nil
		}
		return msg
	}
}

// -- Context builders --

// maxContextDiffBytes caps the diff embedded in a thread's context so a huge
// PR cannot blow the model's context window. When exceeded, the diff is
// truncated with a notice; codex can still explore a local checkout.
const maxContextDiffBytes = 256 * 1024

// buildThreadContext assembles the PR context for a new AI thread:
// title, diff, comments (with IDs so the agent can propose replies),
// and any hunks the user has selected.
func buildThreadContext(title string, files []github.PRFile, comments []github.Comment, inline []github.InlineComment, selectedHunks string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Title: %s\n", title)

	if len(files) > 0 {
		b.WriteString("\nDiff:\n\n")
		diff := buildDiffContent(files)
		if len(diff) > maxContextDiffBytes {
			diff = diff[:maxContextDiffBytes] + "\n\n[... diff truncated — explore the local checkout for the rest ...]\n"
		}
		b.WriteString(diff)
	} else {
		b.WriteString("\n(Diff not yet loaded)\n")
	}

	if len(comments) > 0 {
		b.WriteString("\nPR comments:\n")
		for _, c := range comments {
			fmt.Fprintf(&b, "- %s: %s\n", c.Author.Login, c.Body)
		}
	}

	if len(inline) > 0 {
		b.WriteString("\nInline review comments (id — file:line — author):\n")
		for _, c := range inline {
			fmt.Fprintf(&b, "- %d — %s:%d — %s: %s\n", c.ID, c.Path, c.Line, c.Author.Login, c.Body)
		}
	}

	if selectedHunks != "" {
		b.WriteString("\nThe user has selected these hunks to focus on:\n\n")
		b.WriteString(selectedHunks)
	}

	return b.String()
}

// buildContextDelta assembles the refreshed-context block for a follow-up
// message on an existing thread. Empty when there is nothing new to add.
func buildContextDelta(selectedHunks string) string {
	if selectedHunks == "" {
		return ""
	}
	return "The user has selected these hunks to focus on:\n\n" + selectedHunks
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

// displayCommand compacts an agent shell command for the activity feed,
// stripping the `sh -lc "..."` wrapper codex uses.
func displayCommand(cmd string) string {
	if idx := strings.Index(cmd, " -lc "); idx != -1 {
		cmd = strings.Trim(strings.TrimSpace(cmd[idx+5:]), `"'`)
	}
	return firstLine(cmd)
}

// firstLine returns the first line of a possibly multi-line string.
func firstLine(s string) string {
	if idx := strings.IndexByte(s, '\n'); idx != -1 {
		return s[:idx] + " …"
	}
	return s
}
