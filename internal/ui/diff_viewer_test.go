package ui

import (
	"testing"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/shhac/prtea/internal/github"
)

func TestParsePatchHunks_SingleHunk(t *testing.T) {
	patch := `@@ -1,3 +1,4 @@
 package main
+import "fmt"

 func main() {`

	hunks := parsePatchHunks(0, "main.go", patch)
	if len(hunks) != 1 {
		t.Fatalf("got %d hunks, want 1", len(hunks))
	}
	h := hunks[0]
	if h.FileIndex != 0 {
		t.Errorf("FileIndex = %d, want 0", h.FileIndex)
	}
	if h.Filename != "main.go" {
		t.Errorf("Filename = %q, want %q", h.Filename, "main.go")
	}
	if h.Header != "@@ -1,3 +1,4 @@" {
		t.Errorf("Header = %q, want %q", h.Header, "@@ -1,3 +1,4 @@")
	}
	if len(h.Lines) != 5 {
		t.Errorf("got %d lines, want 5", len(h.Lines))
	}
}

func TestParsePatchHunks_MultipleHunks(t *testing.T) {
	patch := `@@ -1,3 +1,3 @@
-old line
+new line
 context
@@ -10,4 +10,5 @@
 unchanged
+added line
 more context`

	hunks := parsePatchHunks(2, "widget.go", patch)
	if len(hunks) != 2 {
		t.Fatalf("got %d hunks, want 2", len(hunks))
	}

	if hunks[0].Header != "@@ -1,3 +1,3 @@" {
		t.Errorf("first hunk header = %q", hunks[0].Header)
	}
	if len(hunks[0].Lines) != 4 {
		t.Errorf("first hunk has %d lines, want 4", len(hunks[0].Lines))
	}

	if hunks[1].Header != "@@ -10,4 +10,5 @@" {
		t.Errorf("second hunk header = %q", hunks[1].Header)
	}
	if len(hunks[1].Lines) != 4 {
		t.Errorf("second hunk has %d lines, want 4", len(hunks[1].Lines))
	}

	// Both hunks should carry the same file metadata
	for i, h := range hunks {
		if h.FileIndex != 2 {
			t.Errorf("hunk[%d].FileIndex = %d, want 2", i, h.FileIndex)
		}
		if h.Filename != "widget.go" {
			t.Errorf("hunk[%d].Filename = %q, want %q", i, h.Filename, "widget.go")
		}
	}
}

func TestParsePatchHunks_EmptyPatch(t *testing.T) {
	hunks := parsePatchHunks(0, "empty.go", "")
	if len(hunks) != 0 {
		t.Errorf("got %d hunks for empty patch, want 0", len(hunks))
	}
}

func TestParsePatchHunks_NoHunkHeaders(t *testing.T) {
	// Lines without @@ prefix are ignored until a hunk header is seen
	hunks := parsePatchHunks(0, "no_headers.go", "just some text\nno hunks here")
	if len(hunks) != 0 {
		t.Errorf("got %d hunks, want 0", len(hunks))
	}
}

// newTestDiffViewer creates a DiffViewerModel with a ready viewport for testing.
func newTestDiffViewer(width, height int) DiffViewerModel {
	m := NewDiffViewerModel()
	m.viewport = viewport.New(width, height)
	m.ready = true
	m.width = width + 4
	m.height = height + 5
	return m
}

func TestParseAllHunks(t *testing.T) {
	m := newTestDiffViewer(80, 24)
	m.files = []github.PRFile{
		{
			Filename: "a.go", Status: "modified", Additions: 1, Deletions: 1,
			Patch: "@@ -1,3 +1,3 @@\n-old\n+new\n ctx",
		},
		{
			Filename: "b.go", Status: "added", Additions: 2, Deletions: 0,
			Patch: "@@ -0,0 +1,2 @@\n+line1\n+line2",
		},
		{
			Filename: "c.bin", Status: "modified", Additions: 0, Deletions: 0,
			Patch: "", // binary — no patch
		},
	}
	m.parseAllHunks()

	if len(m.hunks) != 2 {
		t.Fatalf("got %d hunks, want 2", len(m.hunks))
	}
	if m.hunks[0].Filename != "a.go" {
		t.Errorf("hunks[0].Filename = %q, want %q", m.hunks[0].Filename, "a.go")
	}
	if m.hunks[0].FileIndex != 0 {
		t.Errorf("hunks[0].FileIndex = %d, want 0", m.hunks[0].FileIndex)
	}
	if m.hunks[1].Filename != "b.go" {
		t.Errorf("hunks[1].Filename = %q, want %q", m.hunks[1].Filename, "b.go")
	}
	if m.hunks[1].FileIndex != 1 {
		t.Errorf("hunks[1].FileIndex = %d, want 1", m.hunks[1].FileIndex)
	}
}

func TestBuildCachedLines_ComputesOffsetsAndRanges(t *testing.T) {
	m := newTestDiffViewer(80, 24)
	m.files = []github.PRFile{
		{
			Filename: "a.go", Status: "modified", Additions: 1, Deletions: 1,
			Patch: "@@ -1,3 +1,3 @@\n-old\n+new\n ctx",
		},
		{
			Filename: "b.go", Status: "added", Additions: 2, Deletions: 0,
			Patch: "@@ -0,0 +1,2 @@\n+line1\n+line2",
		},
	}
	m.parseAllHunks()
	m.buildCachedLines()

	if m.cachedLines == nil {
		t.Fatal("cachedLines should not be nil")
	}
	if len(m.fileOffsets) != 2 {
		t.Fatalf("fileOffsets len = %d, want 2", len(m.fileOffsets))
	}
	if len(m.hunkOffsets) != 2 {
		t.Fatalf("hunkOffsets len = %d, want 2", len(m.hunkOffsets))
	}
	if len(m.hunkLineRanges) != 2 {
		t.Fatalf("hunkLineRanges len = %d, want 2", len(m.hunkLineRanges))
	}

	// File offsets should be ordered
	if m.fileOffsets[0] >= m.fileOffsets[1] {
		t.Errorf("fileOffsets[0]=%d should be < fileOffsets[1]=%d", m.fileOffsets[0], m.fileOffsets[1])
	}
	// Hunk offsets should be ordered
	if m.hunkOffsets[0] >= m.hunkOffsets[1] {
		t.Errorf("hunkOffsets[0]=%d should be < hunkOffsets[1]=%d", m.hunkOffsets[0], m.hunkOffsets[1])
	}

	// Each hunk range should cover exactly as many lines as hunk.Lines
	for i, hr := range m.hunkLineRanges {
		rangeSize := hr[1] - hr[0]
		wantSize := len(m.hunks[i].Lines)
		if rangeSize != wantSize {
			t.Errorf("hunkLineRanges[%d] range=%d, want %d (Lines count)", i, rangeSize, wantSize)
		}
	}
}

func TestBuildCachedLines_EmptyFiles(t *testing.T) {
	m := newTestDiffViewer(80, 24)
	m.files = []github.PRFile{}
	m.parseAllHunks()
	m.buildCachedLines()

	if m.cachedLines == nil {
		t.Fatal("cachedLines should not be nil for empty files")
	}
	if len(m.hunks) != 0 {
		t.Errorf("hunks should be empty, got %d", len(m.hunks))
	}
}

func TestBuildCachedLines_NoPatchFile(t *testing.T) {
	m := newTestDiffViewer(80, 24)
	m.files = []github.PRFile{
		{Filename: "binary.dat", Status: "modified", Patch: ""},
	}
	m.parseAllHunks()
	m.buildCachedLines()

	if len(m.hunks) != 0 {
		t.Errorf("hunks should be empty for no-patch file, got %d", len(m.hunks))
	}
	// Should have file header, separator, and "(diff not available)" lines
	if len(m.cachedLines) < 3 {
		t.Errorf("cachedLines should have at least 3 lines, got %d", len(m.cachedLines))
	}
}

func TestRenderHunkLines_FocusAndSelection(t *testing.T) {
	m := newTestDiffViewer(80, 24)
	m.files = []github.PRFile{
		{
			Filename: "a.go", Status: "modified", Additions: 1, Deletions: 1,
			Patch: "@@ -1,2 +1,2 @@\n-old\n+new",
		},
	}
	m.parseAllHunks()

	// Unfocused, unselected
	m.focusedHunkIdx = 99
	lines, infos := m.renderHunkLines(0)
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3", len(lines))
	}
	if len(infos) != 3 {
		t.Fatalf("got %d infos, want 3", len(infos))
	}

	// Focused hunk — should have gutter marker
	m.focusedHunkIdx = 0
	lines, _ = m.renderHunkLines(0)
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3", len(lines))
	}

	// Selected hunk
	m.selectedHunks = map[int]bool{0: true}
	lines, _ = m.renderHunkLines(0)
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3", len(lines))
	}
}

func TestIncrementalFocusUpdate(t *testing.T) {
	m := newTestDiffViewer(80, 24)
	m.files = []github.PRFile{
		{
			Filename: "a.go", Status: "modified", Additions: 1, Deletions: 1,
			Patch: "@@ -1,2 +1,2 @@\n-old\n+new",
		},
		{
			Filename: "b.go", Status: "modified", Additions: 1, Deletions: 0,
			Patch: "@@ -1,2 +1,3 @@\n ctx\n+added",
		},
	}
	m.parseAllHunks()
	m.focusedHunkIdx = 0
	m.buildCachedLines()

	// Snapshot the initial cached lines
	initialLines := make([]string, len(m.cachedLines))
	copy(initialLines, m.cachedLines)

	// Simulate focus moving to hunk 1
	m.focusedHunkIdx = 1
	m.refreshContent()

	// The cache should still exist (not rebuilt from scratch)
	if m.cachedLines == nil {
		t.Fatal("cachedLines should not be nil after incremental update")
	}

	// Lines outside both hunks should be unchanged
	// File header for a.go is at fileOffsets[0], which is before any hunk
	fileHeaderIdx := m.fileOffsets[0]
	if m.cachedLines[fileHeaderIdx] != initialLines[fileHeaderIdx] {
		t.Error("file header line should not change during focus update")
	}

	// lastRenderedFocus should track the new focus
	if m.lastRenderedFocus != 1 {
		t.Errorf("lastRenderedFocus = %d, want 1", m.lastRenderedFocus)
	}
}

func TestIncrementalSelectionUpdate(t *testing.T) {
	m := newTestDiffViewer(80, 24)
	m.files = []github.PRFile{
		{
			Filename: "a.go", Status: "modified", Additions: 2, Deletions: 0,
			Patch: "@@ -1,2 +1,4 @@\n ctx\n+line1\n+line2",
		},
	}
	m.parseAllHunks()
	m.focusedHunkIdx = 0
	m.buildCachedLines()

	// Snapshot before selection
	beforeSelect := make([]string, len(m.cachedLines))
	copy(beforeSelect, m.cachedLines)

	// Mark hunk as selected and dirty
	m.selectedHunks = map[int]bool{0: true}
	m.markHunkDirty(0)
	m.refreshContent()

	// Hunk lines should have changed (selection adds background)
	r := m.hunkLineRanges[0]
	changed := false
	for i := r[0]; i < r[1]; i++ {
		if m.cachedLines[i] != beforeSelect[i] {
			changed = true
			break
		}
	}
	if !changed {
		t.Error("hunk lines should change after selection toggle")
	}
}

func TestMarkHunkDirty_OutOfBounds(t *testing.T) {
	m := newTestDiffViewer(80, 24)
	m.files = []github.PRFile{
		{
			Filename: "a.go", Status: "modified", Additions: 1, Deletions: 0,
			Patch: "@@ -1,1 +1,2 @@\n ctx\n+new",
		},
	}
	m.parseAllHunks()

	// Should not panic on out-of-bounds
	m.markHunkDirty(-1)
	m.markHunkDirty(999)
	if m.dirtyHunks != nil {
		t.Error("dirtyHunks should be nil for out-of-bounds indices")
	}
}

func TestCacheInvalidation_SetSize(t *testing.T) {
	m := newTestDiffViewer(80, 24)
	m.files = []github.PRFile{
		{
			Filename: "a.go", Status: "modified", Additions: 1, Deletions: 0,
			Patch: "@@ -1,1 +1,2 @@\n ctx\n+new",
		},
	}
	m.parseAllHunks()
	m.buildCachedLines()

	if m.cachedLines == nil {
		t.Fatal("cachedLines should exist before SetSize")
	}

	// SetSize should invalidate cache
	m.SetSize(100, 30)

	// After SetSize+refreshContent, cache should be rebuilt (not nil)
	if m.cachedLines == nil {
		t.Fatal("cachedLines should be rebuilt after SetSize")
	}
}

func TestCachedLineInfo_ParallelToCachedLines(t *testing.T) {
	m := newTestDiffViewer(80, 24)
	m.files = []github.PRFile{
		{
			Filename: "a.go", Status: "modified", Additions: 1, Deletions: 1,
			Patch: "@@ -1,3 +1,3 @@\n-old\n+new\n ctx",
		},
	}
	m.parseAllHunks()
	m.buildCachedLines()

	if len(m.cachedLineInfo) != len(m.cachedLines) {
		t.Fatalf("cachedLineInfo len=%d != cachedLines len=%d", len(m.cachedLineInfo), len(m.cachedLines))
	}

	// Find hunk lines in the info — should have at least the hunk's diff lines
	var diffLines int
	for _, info := range m.cachedLineInfo {
		if info.isDiffLine {
			diffLines++
		}
	}
	// Hunk has 4 lines: @@, -, +, context
	if diffLines != 4 {
		t.Errorf("expected 4 diff lines, got %d", diffLines)
	}
}

func TestCachedLineInfo_CommentableLines(t *testing.T) {
	m := newTestDiffViewer(80, 24)
	m.files = []github.PRFile{
		{
			Filename: "a.go", Status: "modified", Additions: 1, Deletions: 1,
			Patch: "@@ -1,3 +1,3 @@\n-old\n+new\n ctx",
		},
	}
	m.parseAllHunks()
	m.buildCachedLines()

	// Count commentable lines — should be + and context lines (2 out of 4)
	var commentable int
	for _, info := range m.cachedLineInfo {
		if info.isCommentable {
			commentable++
		}
	}
	if commentable != 2 {
		t.Errorf("expected 2 commentable lines (+new, ctx), got %d", commentable)
	}
}

func TestMoveCursor_SkipsNonDiffLines(t *testing.T) {
	m := newTestDiffViewer(80, 24)
	m.files = []github.PRFile{
		{
			Filename: "a.go", Status: "modified", Additions: 1, Deletions: 1,
			Patch: "@@ -1,3 +1,3 @@\n-old\n+new\n ctx",
		},
	}
	m.parseAllHunks()
	m.buildCachedLines()

	// Cursor should start on a diff line
	if !m.cachedLineInfo[m.cursorLine].isDiffLine {
		t.Error("cursor should start on a diff line")
	}

	startLine := m.cursorLine

	// Move down repeatedly and verify cursor only lands on diff lines
	for i := 0; i < 10; i++ {
		m.moveCursor(1)
		if m.cursorLine < len(m.cachedLineInfo) && !m.cachedLineInfo[m.cursorLine].isDiffLine {
			t.Errorf("cursor at %d is not on a diff line after moving down", m.cursorLine)
		}
	}

	// Move back up
	for i := 0; i < 20; i++ {
		m.moveCursor(-1)
	}
	// Should be back at or near the start
	if m.cursorLine > startLine {
		t.Errorf("cursor should be at or before start after moving up, got %d > %d", m.cursorLine, startLine)
	}
}

func TestCommentTargetFromCursor(t *testing.T) {
	m := newTestDiffViewer(80, 24)
	m.files = []github.PRFile{
		{
			Filename: "a.go", Status: "modified", Additions: 2, Deletions: 0,
			Patch: "@@ -1,2 +1,4 @@\n ctx1\n+added1\n+added2\n ctx2",
		},
	}
	m.parseAllHunks()
	m.buildCachedLines()

	// Move cursor to a commentable line and check target
	for i := 0; i < len(m.cachedLineInfo); i++ {
		if m.cachedLineInfo[i].isCommentable && m.cachedLineInfo[i].newLineNum > 0 {
			m.cursorLine = i
			break
		}
	}

	lineNum, filename := m.commentTargetFromCursor()
	if filename != "a.go" {
		t.Errorf("expected filename a.go, got %q", filename)
	}
	if lineNum == 0 {
		t.Error("expected non-zero line number from commentTargetFromCursor")
	}
}
