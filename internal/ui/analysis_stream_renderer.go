package ui

import (
	"encoding/json"
	"strings"
	"time"

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
		r.rendered = renderAnalysisContent(r.parsed, width)
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
