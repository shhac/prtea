package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/shhac/prtea/internal/github"
)

// SetCIStatus sets CI check status data for the CI tab.
func (m *DiffViewerModel) SetCIStatus(status *github.CIStatus) {
	m.ciStatus = status
	m.refreshContent()
}

// SetCIError sets an error message for CI status loading.
func (m *DiffViewerModel) SetCIError(err string) {
	m.ciError = err
	m.refreshContent()
}

// ciTabLabel returns a dynamic label for the CI tab header showing at-a-glance status.
func (m DiffViewerModel) ciTabLabel() string {
	if m.ciStatus == nil || m.prNumber == 0 {
		return "CI"
	}
	if m.ciStatus.TotalCount == 0 {
		return "CI"
	}
	icon, _ := ciStatusIconColor(m.ciStatus.OverallStatus)
	passCount := ciPassingCount(m.ciStatus.Checks)
	switch m.ciStatus.OverallStatus {
	case "passing":
		return fmt.Sprintf("CI (%s %d/%d)", icon, passCount, m.ciStatus.TotalCount)
	case "failing":
		failCount := m.ciStatus.TotalCount - passCount
		return fmt.Sprintf("CI (%s %d/%d)", icon, failCount, m.ciStatus.TotalCount)
	case "pending":
		completedCount := 0
		for _, c := range m.ciStatus.Checks {
			if c.Status == "completed" {
				completedCount++
			}
		}
		return fmt.Sprintf("CI (%s %d/%d)", icon, completedCount, m.ciStatus.TotalCount)
	case "mixed":
		return fmt.Sprintf("CI (%s %d/%d)", icon, passCount, m.ciStatus.TotalCount)
	default:
		return "CI"
	}
}

// renderCITab renders the full CI status view for the dedicated CI tab.
func (m DiffViewerModel) renderCITab() string {
	if m.prNumber == 0 {
		return renderEmptyState("Select a PR to view CI status", "Use j/k to navigate, Enter to select")
	}

	if m.ciError != "" {
		return renderErrorWithHint(formatUserError(m.ciError), "Press r to refresh")
	}

	if m.ciStatus == nil {
		return lipgloss.NewStyle().
			Foreground(lipgloss.Color("244")).
			Padding(1, 2).
			Render(m.spinner.View() + fmt.Sprintf(" Loading CI status for PR #%d...", m.prNumber))
	}

	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("33"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("244"))

	var b strings.Builder

	b.WriteString(sectionStyle.Render(fmt.Sprintf("CI Status — PR #%d", m.prNumber)))
	b.WriteString("\n\n")

	if m.ciStatus.TotalCount == 0 {
		b.WriteString(dimStyle.Render("No CI checks configured"))
		b.WriteString("\n")
		return b.String()
	}

	// Summary badge
	icon, color := ciStatusIconColor(m.ciStatus.OverallStatus)
	badge := lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render(icon)
	passCount := ciPassingCount(m.ciStatus.Checks)
	label := ciStatusLabel(m.ciStatus.OverallStatus)
	b.WriteString(fmt.Sprintf("%s %s — %d/%d checks passing\n\n", badge, label, passCount, m.ciStatus.TotalCount))

	// Sort checks: failures first, then pending, then passing/skipped
	type checkGroup struct {
		title  string
		checks []github.CICheck
	}
	var failing, pending, passing []github.CICheck
	for _, check := range m.ciStatus.Checks {
		switch {
		case check.Status == "completed" && check.Conclusion == "failure":
			failing = append(failing, check)
		case check.Status == "queued" || check.Status == "in_progress":
			pending = append(pending, check)
		default:
			passing = append(passing, check)
		}
	}

	groups := []checkGroup{
		{"Failing", failing},
		{"In Progress", pending},
		{"Passing", passing},
	}

	// When all checks share one status, show a flat list without group headers.
	nonEmpty := 0
	for _, g := range groups {
		if len(g.checks) > 0 {
			nonEmpty++
		}
	}

	for _, group := range groups {
		if len(group.checks) == 0 {
			continue
		}
		if nonEmpty > 1 {
			b.WriteString(dimStyle.Render(fmt.Sprintf("── %s (%d) ", group.title, len(group.checks))))
			b.WriteString("\n")
		}
		for _, check := range group.checks {
			ci, cc := ciCheckIconColor(check)
			checkIcon := lipgloss.NewStyle().Foreground(lipgloss.Color(cc)).Render(ci)
			conclusion := ""
			if check.Status == "completed" && check.Conclusion != "" {
				conclusion = dimStyle.Render(fmt.Sprintf(" (%s)", check.Conclusion))
			} else if check.Status != "completed" {
				conclusion = dimStyle.Render(fmt.Sprintf(" (%s)", check.Status))
			}
			b.WriteString(fmt.Sprintf("  %s %s%s\n", checkIcon, check.Name, conclusion))
		}
		b.WriteString("\n")
	}

	// Show re-run hint when there are failed checks with rerunnable workflows
	if failedIDs := m.ciStatus.FailedRunIDs(); len(failedIDs) > 0 {
		hintStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Italic(true)
		b.WriteString(hintStyle.Render("Press x to re-run failed checks"))
		b.WriteString("\n")
	}

	return b.String()
}

// ciStatusIconColor returns the icon and lipgloss color for an overall CI status.
func ciStatusIconColor(status string) (string, string) {
	switch status {
	case "passing":
		return "✓", "42"
	case "failing":
		return "✗", "196"
	case "pending":
		return "●", "226"
	case "mixed":
		return "⚠", "208"
	default:
		return "?", "244"
	}
}

// ciStatusLabel returns a display label for the overall CI status.
func ciStatusLabel(status string) string {
	switch status {
	case "passing":
		return "Passing"
	case "failing":
		return "Failing"
	case "pending":
		return "Pending"
	case "mixed":
		return "Mixed"
	default:
		return status
	}
}

// ciCheckIconColor returns the icon and color for an individual CI check.
func ciCheckIconColor(check github.CICheck) (string, string) {
	switch {
	case check.Status == "completed" && check.Conclusion == "success":
		return "✓", "42"
	case check.Status == "completed" && (check.Conclusion == "skipped" || check.Conclusion == "neutral"):
		return "−", "244"
	case check.Status == "completed" && check.Conclusion == "failure":
		return "✗", "196"
	case check.Status == "queued" || check.Status == "in_progress":
		return "●", "226"
	default:
		return "?", "244"
	}
}

// ciPassingCount counts checks that completed successfully (including skipped/neutral).
func ciPassingCount(checks []github.CICheck) int {
	count := 0
	for _, c := range checks {
		if c.Status == "completed" && (c.Conclusion == "success" || c.Conclusion == "skipped" || c.Conclusion == "neutral") {
			count++
		}
	}
	return count
}
