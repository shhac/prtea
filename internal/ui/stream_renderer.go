package ui

import "time"

// StreamRenderer manages progressive markdown rendering for streaming content.
// It accumulates raw text and periodically renders with glamour at checkpoint
// intervals, showing raw tail text between checkpoints. Used by both chat and
// analysis streaming.
type StreamRenderer struct {
	Content       string    // accumulated raw streaming text
	Rendered      string    // last successful glamour-rendered content
	RenderedLen   int       // byte length of Content when last rendered
	RenderedAt    time.Time // when the last render happened
}

// checkpointInterval controls how often glamour re-renders are triggered.
const checkpointInterval = 300 * time.Millisecond

// Append adds a text chunk and re-renders with glamour if enough time has passed.
// renderFn is called to perform the actual glamour rendering.
func (sr *StreamRenderer) Append(chunk string, renderFn func(string) string) {
	sr.Content += chunk

	if time.Since(sr.RenderedAt) >= checkpointInterval {
		sr.Rendered = renderFn(sr.Content)
		sr.RenderedLen = len(sr.Content)
		sr.RenderedAt = time.Now()
	}
}

// Reset clears all streaming state.
func (sr *StreamRenderer) Reset() {
	sr.Content = ""
	sr.Rendered = ""
	sr.RenderedLen = 0
	sr.RenderedAt = time.Time{}
}

// HasContent returns true if any streaming text has been accumulated.
func (sr *StreamRenderer) HasContent() bool {
	return sr.Content != ""
}

// View returns the current display content: the last glamour-rendered
// checkpoint plus any raw tail text received since that checkpoint.
func (sr *StreamRenderer) View(wrapFn func(string, int) string, width int) string {
	if sr.Rendered != "" {
		result := sr.Rendered
		// Append raw tail (tokens received since last glamour render)
		if len(sr.Content) > sr.RenderedLen {
			tail := sr.Content[sr.RenderedLen:]
			result += "\n" + wrapFn(tail, width)
		}
		return result
	}
	// No render yet (first checkpoint interval) — show raw text
	return wrapFn(sr.Content, width)
}
