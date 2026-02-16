package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

const markdownCacheMaxSize = 100

// MarkdownRenderer provides cached glamour markdown rendering.
// Used by ChatPanelModel and DiffViewerModel to avoid duplicating
// the renderer lifecycle and fallback logic.
type MarkdownRenderer struct {
	renderer *glamour.TermRenderer
	width    int
	cache    map[string]string // content-level LRU (key: "width:content")
}

// RenderMarkdown renders markdown text with glamour for terminal display.
// Uses a cached renderer per width to avoid re-creating it on every call.
// Falls back to plain wordWrap if glamour fails.
func (mr *MarkdownRenderer) RenderMarkdown(markdown string, width int) string {
	if width < 10 {
		width = 10
	}

	key := fmt.Sprintf("%d:%s", width, markdown)
	if cached, ok := mr.cache[key]; ok {
		return cached
	}

	r := mr.getOrCreate(width)
	var result string
	if r == nil {
		result = wordWrap(markdown, width)
	} else if out, err := r.Render(markdown); err != nil {
		result = wordWrap(markdown, width)
	} else {
		result = strings.TrimSpace(out)
	}

	if mr.cache == nil {
		mr.cache = make(map[string]string)
	}
	if len(mr.cache) >= markdownCacheMaxSize {
		// Simple eviction: clear all on overflow
		mr.cache = make(map[string]string)
	}
	mr.cache[key] = result
	return result
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
