package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/shhac/prtea/internal/github"
)

// SetPRInfo sets PR metadata for the PR Info tab.
func (m *DiffViewerModel) SetPRInfo(title, body, author, url string) {
	m.prTitle = title
	m.prBody = body
	m.prAuthor = author
	m.prURL = url
	m.prInfoErr = ""
	m.refreshContent()
}

// SetPRInfoError sets an error message for the PR Info tab.
func (m *DiffViewerModel) SetPRInfoError(err string) {
	m.prInfoErr = err
	m.refreshContent()
}

// SetReviewSummary sets review status data for the PR Info tab.
func (m *DiffViewerModel) SetReviewSummary(summary *github.ReviewSummary) {
	m.reviewSummary = summary
	m.refreshContent()
}

// SetReviewError sets an error message for review status loading.
func (m *DiffViewerModel) SetReviewError(err string) {
	m.reviewError = err
	m.refreshContent()
}

// renderPRInfo renders the full PR info tab content.
func (m *DiffViewerModel) renderPRInfo() string {
	if m.prNumber == 0 {
		return renderEmptyState("Select a PR to view its details", "Use j/k to navigate, Enter to select")
	}

	if m.prInfoErr != "" {
		return renderErrorWithHint(
			formatUserError(m.prInfoErr),
			"Press r to refresh",
		)
	}

	if m.prTitle == "" {
		return lipgloss.NewStyle().
			Foreground(lipgloss.Color("244")).
			Padding(1, 2).
			Render(m.spinner.View() + fmt.Sprintf(" Loading PR #%d info...", m.prNumber))
	}

	innerWidth := m.viewport.Width
	if innerWidth < 10 {
		innerWidth = 10
	}

	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("33"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("244"))

	var b strings.Builder

	// Title
	b.WriteString(sectionStyle.Render(fmt.Sprintf("PR #%d", m.prNumber)))
	b.WriteString("\n")
	b.WriteString(lipgloss.NewStyle().Bold(true).Render(m.prTitle))
	b.WriteString("\n\n")

	// Author
	b.WriteString(dimStyle.Render("Author: "))
	b.WriteString(m.prAuthor)
	b.WriteString("\n")

	// URL
	if m.prURL != "" {
		b.WriteString(dimStyle.Render("URL: "))
		b.WriteString(m.prURL)
		b.WriteString("\n")
	}

	// Reviews
	if m.reviewError != "" {
		b.WriteString("\n")
		b.WriteString(sectionStyle.Render("Reviews"))
		b.WriteString("\n")
		errStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
		b.WriteString(errStyle.Render(formatUserError(m.reviewError)))
		b.WriteString("\n")
	} else if m.reviewSummary != nil {
		b.WriteString("\n")
		b.WriteString(sectionStyle.Render("Reviews"))
		b.WriteString("\n")

		// Overall decision badge
		if m.reviewSummary.ReviewDecision != "" {
			icon, color := reviewDecisionIconColor(m.reviewSummary.ReviewDecision)
			badge := lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render(icon)
			label := reviewDecisionLabel(m.reviewSummary.ReviewDecision)
			b.WriteString(fmt.Sprintf("%s %s\n", badge, label))
		}

		// Per-reviewer status
		for _, r := range m.reviewSummary.Approved {
			approvedIcon := lipgloss.NewStyle().Foreground(lipgloss.Color("76")).Render("✓")
			b.WriteString(fmt.Sprintf("  %s %s approved\n", approvedIcon, r.Author.Login))
		}
		for _, r := range m.reviewSummary.ChangesRequested {
			changesIcon := lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render("✗")
			b.WriteString(fmt.Sprintf("  %s %s requested changes\n", changesIcon, r.Author.Login))
		}

		// Pending reviewers
		for _, rr := range m.reviewSummary.PendingReviewers {
			pendingIcon := lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Render("○")
			name := rr.Login
			if rr.IsTeam {
				name += " (team)"
			}
			b.WriteString(fmt.Sprintf("  %s %s pending\n", pendingIcon, name))
		}

		if len(m.reviewSummary.Approved) == 0 && len(m.reviewSummary.ChangesRequested) == 0 &&
			len(m.reviewSummary.PendingReviewers) == 0 && m.reviewSummary.ReviewDecision == "" {
			b.WriteString(dimStyle.Render("No reviews yet"))
			b.WriteString("\n")
		}
	}

	// Description
	if m.prBody != "" {
		b.WriteString("\n")
		b.WriteString(sectionStyle.Render("Description"))
		b.WriteString("\n")
		b.WriteString(m.renderMarkdown(m.prBody, innerWidth))
	} else {
		b.WriteString("\n")
		b.WriteString(dimStyle.Render("No description provided."))
	}

	return b.String()
}

// reviewDecisionIconColor returns the icon and lipgloss color for a review decision.
func reviewDecisionIconColor(decision string) (string, string) {
	switch decision {
	case "APPROVED":
		return "✓", "76"
	case "CHANGES_REQUESTED":
		return "✗", "196"
	case "REVIEW_REQUIRED":
		return "○", "214"
	default:
		return "?", "244"
	}
}

// reviewDecisionLabel returns a display label for the review decision.
func reviewDecisionLabel(decision string) string {
	switch decision {
	case "APPROVED":
		return "Approved"
	case "CHANGES_REQUESTED":
		return "Changes Requested"
	case "REVIEW_REQUIRED":
		return "Review Required"
	default:
		return decision
	}
}
