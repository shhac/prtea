package ui

import (
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

// MarkdownRenderer provides cached glamour markdown rendering.
// Used by ChatPanelModel and DiffViewerModel to avoid duplicating
// the renderer lifecycle and fallback logic.
type MarkdownRenderer struct {
	renderer *glamour.TermRenderer
	width    int
}

// RenderMarkdown renders markdown text with glamour for terminal display.
// Uses a cached renderer per width to avoid re-creating it on every call.
// Falls back to plain wordWrap if glamour fails.
func (mr *MarkdownRenderer) RenderMarkdown(markdown string, width int) string {
	if width < 10 {
		width = 10
	}
	r := mr.getOrCreate(width)
	if r == nil {
		return wordWrap(markdown, width)
	}
	out, err := r.Render(markdown)
	if err != nil {
		return wordWrap(markdown, width)
	}
	return strings.TrimSpace(out)
}

func (mr *MarkdownRenderer) getOrCreate(width int) *glamour.TermRenderer {
	if mr.renderer != nil && mr.width == width {
		return mr.renderer
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return nil
	}
	mr.renderer = r
	mr.width = width
	return r
}

// wordWrap wraps text to fit within the given width.
func wordWrap(s string, width int) string {
	if width <= 0 {
		return s
	}

	var result strings.Builder
	for _, line := range strings.Split(s, "\n") {
		if lipgloss.Width(line) <= width {
			if result.Len() > 0 {
				result.WriteString("\n")
			}
			result.WriteString(line)
			continue
		}

		words := strings.Fields(line)
		currentLine := ""
		for _, word := range words {
			if currentLine == "" {
				currentLine = word
			} else if lipgloss.Width(currentLine+" "+word) <= width {
				currentLine += " " + word
			} else {
				if result.Len() > 0 {
					result.WriteString("\n")
				}
				result.WriteString(currentLine)
				currentLine = word
			}
		}
		if currentLine != "" {
			if result.Len() > 0 {
				result.WriteString("\n")
			}
			result.WriteString(currentLine)
		}
	}
	return result.String()
}
