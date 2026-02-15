package ui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/shhac/prtea/internal/claude"
)

// AnalysisStreamRenderer incrementally parses streaming JSON from Claude's
// analysis output and renders completed fields with lipgloss styling. Unlike
// the generic StreamRenderer (used for chat markdown), this understands the
// AnalysisResult JSON schema and progressively renders sections as they
// become parseable — so users see styled output instead of raw JSON.
type AnalysisStreamRenderer struct {
	raw                string                // accumulated raw streaming text
	parsed             *claude.AnalysisResult // last successfully parsed partial result
	rendered           string                // cached rendered output for last parse
	parsedAt           time.Time             // when last parse happened
	CheckpointInterval time.Duration         // how often to attempt parsing (0 = default 300ms)
}

// Append adds a text chunk and attempts to parse the accumulated JSON if
// enough time has passed since the last parse attempt.
func (r *AnalysisStreamRenderer) Append(chunk string) {
	r.raw += chunk

	interval := r.CheckpointInterval
	if interval == 0 {
		interval = defaultCheckpointInterval
	}
	if time.Since(r.parsedAt) >= interval {
		if result := tryParsePartialAnalysis(r.raw); result != nil {
			r.parsed = result
			r.rendered = "" // invalidate cache
		}
		r.parsedAt = time.Now()
	}
}

// Reset clears all streaming state.
func (r *AnalysisStreamRenderer) Reset() {
	r.raw = ""
	r.parsed = nil
	r.rendered = ""
	r.parsedAt = time.Time{}
}

// HasContent returns true if any streaming text has been accumulated.
func (r *AnalysisStreamRenderer) HasContent() bool {
	return r.raw != ""
}

// View renders the partial analysis result with lipgloss styling. Returns
// an empty string if no fields have been successfully parsed yet.
func (r *AnalysisStreamRenderer) View(width int) string {
	if r.parsed == nil {
		return ""
	}
	if r.rendered == "" {
		r.rendered = renderPartialAnalysis(r.parsed, width)
	}
	return r.rendered
}

// tryParsePartialAnalysis attempts to parse an incomplete JSON buffer into
// an AnalysisResult by healing the JSON (closing open strings, brackets,
// and braces) and trying progressively shorter prefixes.
func tryParsePartialAnalysis(raw string) *claude.AnalysisResult {
	start := strings.Index(raw, "{")
	if start == -1 {
		return nil
	}
	buf := raw[start:]

	// Try healing at progressively shorter lengths. Each trim removes one
	// character from the end before healing, handling cases where the cut
	// falls mid-key or at a trailing comma.
	for trim := 0; trim <= 50 && trim < len(buf); trim++ {
		healed := healJSON(buf[:len(buf)-trim])
		var result claude.AnalysisResult
		if json.Unmarshal([]byte(healed), &result) == nil {
			return &result
		}
	}
	return nil
}

// healJSON closes any open strings, arrays, and objects in partial JSON,
// then removes trailing commas that would make it invalid.
func healJSON(partial string) string {
	var b strings.Builder
	b.Grow(len(partial) + 20)

	inString := false
	escaped := false
	var stack []byte

	for i := 0; i < len(partial); i++ {
		ch := partial[i]
		b.WriteByte(ch)

		if escaped {
			escaped = false
			continue
		}
		if ch == '\\' && inString {
			escaped = true
			continue
		}
		if ch == '"' {
			inString = !inString
			continue
		}
		if inString {
			continue
		}
		switch ch {
		case '{':
			stack = append(stack, '{')
		case '[':
			stack = append(stack, '[')
		case '}':
			if len(stack) > 0 && stack[len(stack)-1] == '{' {
				stack = stack[:len(stack)-1]
			}
		case ']':
			if len(stack) > 0 && stack[len(stack)-1] == '[' {
				stack = stack[:len(stack)-1]
			}
		}
	}

	// Close open string
	if inString {
		b.WriteByte('"')
	}

	// Close open constructs in reverse order
	for i := len(stack) - 1; i >= 0; i-- {
		if stack[i] == '[' {
			b.WriteByte(']')
		} else {
			b.WriteByte('}')
		}
	}

	result := b.String()

	// Remove trailing commas before closers: ,} → }  ,] → ]
	// Repeat until stable (nested cases like ,},})
	for {
		cleaned := strings.ReplaceAll(result, ",}", "}")
		cleaned = strings.ReplaceAll(cleaned, ",]", "]")
		if cleaned == result {
			break
		}
		result = cleaned
	}

	return result
}

// renderPartialAnalysis renders whatever fields of the AnalysisResult have
// been parsed so far, using the same lipgloss styling as renderAnalysisResult.
// Sections with zero values are skipped.
func renderPartialAnalysis(r *claude.AnalysisResult, width int) string {
	var b strings.Builder
	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("33"))

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
		b.WriteString(sectionStyle.Render("Summary"))
		b.WriteString("\n")
		b.WriteString(wordWrap(r.Summary, width))
		b.WriteString("\n\n")
	}

	// Architecture impact
	if r.ArchitectureImpact.HasImpact {
		b.WriteString(sectionStyle.Render("Architecture Impact"))
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
		b.WriteString(sectionStyle.Render(fmt.Sprintf("File Reviews (%d)", len(r.FileReviews))))
		b.WriteString("\n")
		fileStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("220"))
		for _, fr := range r.FileReviews {
			b.WriteString("\n")
			b.WriteString(fileStyle.Render(fr.File))
			b.WriteString("\n")
			if fr.Summary != "" {
				b.WriteString(wordWrap(fr.Summary, width))
				b.WriteString("\n")
			}
			for _, c := range fr.Comments {
				sevLabel := severityStyle(c.Severity).Render(c.Severity)
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
		b.WriteString(sectionStyle.Render("Test Coverage"))
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
		b.WriteString(sectionStyle.Render(fmt.Sprintf("Suggestions (%d)", len(r.Suggestions))))
		b.WriteString("\n")
		titleStyle := lipgloss.NewStyle().Bold(true)
		for _, s := range r.Suggestions {
			b.WriteString("\n  • ")
			b.WriteString(titleStyle.Render(s.Title))
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
