package ui

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os/exec"
	"runtime"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/shhac/prtea/internal/claude"
	"github.com/shhac/prtea/internal/github"
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

// submitReviewCmd returns a command that submits a PR review.
func submitReviewCmd(client GitHubService, owner, repo string, number int, action ReviewAction, body string) tea.Cmd {
	return func() tea.Msg {
		var err error
		switch action {
		case ReviewApprove:
			err = client.ApprovePR(context.Background(), owner, repo, number, body)
		case ReviewComment:
			err = client.CommentReviewPR(context.Background(), owner, repo, number, body)
		case ReviewRequestChanges:
			err = client.RequestChangesPR(context.Background(), owner, repo, number, body)
		}
		if err != nil {
			return ReviewSubmitErrMsg{PRNumber: number, Err: err}
		}
		return ReviewSubmitDoneMsg{PRNumber: number, Action: action}
	}
}

// analyzeDiffCmd returns a command that runs Claude analysis with inline diff content.
func analyzeDiffCmd(analyzer *claude.Analyzer, pr *SelectedPR, files []github.PRFile, diffHash string) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()

		diffContent := buildDiffContent(files)

		input := claude.AnalyzeDiffInput{
			Owner:       pr.Owner,
			Repo:        pr.Repo,
			PRNumber:    pr.Number,
			PRTitle:     pr.Title,
			DiffContent: diffContent,
		}

		result, err := analyzer.AnalyzeDiff(ctx, input, nil)
		if err != nil {
			return AnalysisErrorMsg{Err: err}
		}

		return AnalysisCompleteMsg{
			PRNumber: pr.Number,
			DiffHash: diffHash,
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
