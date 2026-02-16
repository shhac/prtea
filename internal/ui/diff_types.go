package ui

import (
	"fmt"
	"strings"

	"github.com/shhac/prtea/internal/github"
)

// DiffViewerTab identifies which sub-tab is active in the diff viewer.
type DiffViewerTab int

const (
	TabDiff   DiffViewerTab = iota
	TabPRInfo
	TabCI
)

// DiffHunk represents a single hunk within a file's patch.
type DiffHunk struct {
	FileIndex int
	Filename  string
	Header    string   // the @@ line
	Lines     []string // all lines including the @@ header
}

// ghCommentThread groups a root GitHub inline comment with its replies.
type ghCommentThread struct {
	Root    github.InlineComment
	Replies []github.InlineComment
}

// commentKind identifies the type of inline comment a cached line represents.
type commentKind byte

const (
	commentNone    commentKind = iota
	commentAI                  // AI-generated inline comment
	commentGitHub              // GitHub review comment
	commentPending             // Pending user/AI draft
)

// lineInfo describes what a cached viewport line represents in the source diff.
type lineInfo struct {
	hunkIdx       int         // which hunk this line belongs to (-1 for file headers etc.)
	filename      string      // file path for this line
	newLineNum    int         // new-side file line number (0 = not a file line)
	isCommentable bool        // true for + and context lines (commentable on RIGHT side)
	isDiffLine    bool        // true for actual diff content lines (cursor can land here)
	comment       commentKind // non-zero for inline comment lines
}

// matchPos represents a single search match position within a line.
type matchPos struct {
	startCol int
	endCol   int
}

// searchMatch identifies a single search match globally across all hunks.
type searchMatch struct {
	hunkIdx    int
	lineInHunk int
	startCol   int
	endCol     int
}

// parsePatchHunks splits a file's patch string into individual hunks.
func parsePatchHunks(fileIndex int, filename string, patch string) []DiffHunk {
	lines := strings.Split(patch, "\n")
	var hunks []DiffHunk
	var current *DiffHunk

	for _, line := range lines {
		if strings.HasPrefix(line, "@@") {
			if current != nil {
				hunks = append(hunks, *current)
			}
			current = &DiffHunk{
				FileIndex: fileIndex,
				Filename:  filename,
				Header:    line,
				Lines:     []string{line},
			}
		} else if current != nil {
			current.Lines = append(current.Lines, line)
		}
	}
	if current != nil {
		hunks = append(hunks, *current)
	}

	return hunks
}

// parseHunkNewStart parses the new-side start line number from a @@ header.
// For "@@ -7,6 +12,8 @@" it returns 12.
func parseHunkNewStart(header string) int {
	idx := strings.Index(header, "+")
	if idx == -1 {
		return 0
	}
	rest := header[idx+1:]
	var n int
	fmt.Sscanf(rest, "%d", &n)
	return n
}

// parseAllHunks parses hunks from all files once and populates m.hunks.
func (m *DiffViewerModel) parseAllHunks() {
	m.hunks = nil
	for i, f := range m.files {
		if f.Patch == "" {
			continue
		}
		fileHunks := parsePatchHunks(i, f.Filename, f.Patch)
		m.hunks = append(m.hunks, fileHunks...)
	}
}

// fileStatusLabel formats a file header label with status and change counts.
func fileStatusLabel(f github.PRFile) string {
	switch f.Status {
	case "added":
		return fmt.Sprintf("%s (new file, +%d)", f.Filename, f.Additions)
	case "removed":
		return fmt.Sprintf("%s (deleted, -%d)", f.Filename, f.Deletions)
	case "renamed":
		return fmt.Sprintf("%s (renamed)", f.Filename)
	default:
		return fmt.Sprintf("%s (+%d/-%d)", f.Filename, f.Additions, f.Deletions)
	}
}
