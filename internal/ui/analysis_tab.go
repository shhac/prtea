package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/shhac/prtea/internal/claude"
)

// AnalysisTabModel manages the analysis tab state and rendering.
type AnalysisTabModel struct {
	result     *claude.AnalysisResult
	loading    bool
	error      string
	stream     AnalysisStreamRenderer
	cache      string
	cacheWidth int
}

// SetLoading puts the analysis tab into loading state.
func (t *AnalysisTabModel) SetLoading() {
	t.loading = true
	t.error = ""
	t.result = nil
	t.stream.Reset()
	t.cache = ""
}

// SetResult sets the analysis result and clears loading state.
func (t *AnalysisTabModel) SetResult(result *claude.AnalysisResult) {
	t.result = result
	t.loading = false
	t.error = ""
	t.stream.Reset()
	t.cache = ""
}

// SetError sets an error message on the analysis tab.
func (t *AnalysisTabModel) SetError(err string) {
	t.error = err
	t.loading = false
	t.result = nil
	t.stream.Reset()
	t.cache = ""
}

// AppendStreamChunk appends a text chunk during analysis streaming.
func (t *AnalysisTabModel) AppendStreamChunk(chunk string) {
	t.stream.Append(chunk)
	t.cache = ""
}

// Render renders the analysis tab content for the viewport.
func (t *AnalysisTabModel) Render(width int, spinnerView string) string {
	if t.loading {
		// Don't cache during streaming — content changes rapidly
		if t.stream.HasContent() {
			var b strings.Builder
			b.WriteString(lipgloss.NewStyle().
				Foreground(lipgloss.Color("244")).
				Render(spinnerView + " Analyzing PR with Claude..."))
			b.WriteString("\n\n")
			streamView := t.stream.View(width)
			if streamView != "" {
				b.WriteString(streamView)
			} else {
				b.WriteString(lipgloss.NewStyle().
					Foreground(lipgloss.Color("244")).
					Render("Waiting for analysis data..."))
			}
			return b.String()
		}
		return lipgloss.NewStyle().
			Foreground(lipgloss.Color("244")).
			Padding(1, 0).
			Render(spinnerView + " Analyzing PR with Claude...\n\nThis may take a minute.")
	}
	if t.error != "" {
		return renderErrorWithHint(formatUserError(t.error), "Press 'a' to try again")
	}
	if t.result == nil {
		return renderEmptyState("No analysis yet", "Press 'a' to analyze this PR with Claude")
	}

	// Return cached render if available and width hasn't changed
	if t.cache != "" && t.cacheWidth == width {
		return t.cache
	}

	result := renderAnalysisContent(t.result, width)
	t.cache = result
	t.cacheWidth = width
	return result
}

// renderAnalysisContent renders an AnalysisResult with lipgloss styling.
// Sections with zero values are skipped, making this suitable for both
// complete results and partial (streaming) results.
func renderAnalysisContent(r *claude.AnalysisResult, width int) string {
	var b strings.Builder

	// Risk badge
	if r.Risk.Level != "" {
		riskBadge := lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("0")).
			Background(riskLevelColor(r.Risk.Level)).
			Padding(0, 1).
			Render(strings.ToUpper(r.Risk.Level) + " RISK")
		b.WriteString(riskBadge)
		b.WriteString("\n")
		if r.Risk.Reasoning != "" {
			b.WriteString(wordWrap(r.Risk.Reasoning, width))
		}
		b.WriteString("\n\n")
	}

	// Summary
	if r.Summary != "" {
		b.WriteString(sectionHeaderStyle.Render("Summary"))
		b.WriteString("\n")
		b.WriteString(wordWrap(r.Summary, width))
		b.WriteString("\n\n")
	}

	// Architecture impact
	if r.ArchitectureImpact.HasImpact {
		b.WriteString(sectionHeaderStyle.Render("Architecture Impact"))
		b.WriteString("\n")
		if r.ArchitectureImpact.Description != "" {
			b.WriteString(wordWrap(r.ArchitectureImpact.Description, width))
		}
		if len(r.ArchitectureImpact.AffectedModules) > 0 {
			b.WriteString("\nAffected: ")
			b.WriteString(strings.Join(r.ArchitectureImpact.AffectedModules, ", "))
		}
		b.WriteString("\n\n")
	}

	// File reviews
	if len(r.FileReviews) > 0 {
		b.WriteString(sectionHeaderStyle.Render(fmt.Sprintf("File Reviews (%d)", len(r.FileReviews))))
		b.WriteString("\n")
		for _, fr := range r.FileReviews {
			b.WriteString("\n")
			b.WriteString(contentAuthorStyle.Render(fr.File))
			b.WriteString("\n")
			if fr.Summary != "" {
				b.WriteString(wordWrap(fr.Summary, width))
				b.WriteString("\n")
			}
			for _, c := range fr.Comments {
				sev, ok := severityStyles[c.Severity]
				if !ok {
					sev = defaultSeverityStyle
				}
				sevLabel := sev.Render(c.Severity)
				if c.Line > 0 {
					sevLabel += fmt.Sprintf(" L%d", c.Line)
				}
				b.WriteString("  ")
				b.WriteString(sevLabel)
				b.WriteString(" ")
				b.WriteString(wordWrap(c.Comment, width-4))
				b.WriteString("\n")
			}
		}
		b.WriteString("\n")
	}

	// Test coverage
	if r.TestCoverage.Assessment != "" {
		b.WriteString(sectionHeaderStyle.Render("Test Coverage"))
		b.WriteString("\n")
		b.WriteString(wordWrap(r.TestCoverage.Assessment, width))
		if len(r.TestCoverage.Gaps) > 0 {
			b.WriteString("\nGaps:")
			for _, gap := range r.TestCoverage.Gaps {
				b.WriteString("\n  • ")
				b.WriteString(wordWrap(gap, width-4))
			}
		}
		b.WriteString("\n\n")
	}

	// Suggestions
	if len(r.Suggestions) > 0 {
		b.WriteString(sectionHeaderStyle.Render(fmt.Sprintf("Suggestions (%d)", len(r.Suggestions))))
		b.WriteString("\n")
		for _, s := range r.Suggestions {
			b.WriteString("\n  • ")
			b.WriteString(boldStyle.Render(s.Title))
			if s.Description != "" {
				b.WriteString("\n    ")
				b.WriteString(wordWrap(s.Description, width-4))
			}
			if s.File != "" {
				b.WriteString(fmt.Sprintf("\n    File: %s", s.File))
			}
			b.WriteString("\n")
		}
	}

	return b.String()
}

func riskLevelColor(level string) lipgloss.Color {
	switch level {
	case "low":
		return lipgloss.Color("42") // green
	case "medium":
		return lipgloss.Color("214") // orange
	case "high", "critical":
		return lipgloss.Color("196") // red
	default:
		return lipgloss.Color("244") // gray
	}
}

