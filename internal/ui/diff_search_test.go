package ui

import (
	"testing"

	"github.com/shhac/prtea/internal/github"
)

func newSearchTestModel(files []github.PRFile) *DiffViewerModel {
	m := &DiffViewerModel{files: files}
	m.parseAllHunks()
	return m
}

func TestComputeSearchMatches_Basic(t *testing.T) {
	m := newSearchTestModel([]github.PRFile{
		{
			Filename: "main.go",
			Patch:    "@@ -1,3 +1,4 @@\n context line\n+added line with hello\n-removed line",
		},
	})

	m.searchTerm = "hello"
	m.computeSearchMatches()

	if len(m.searchMatches) != 1 {
		t.Fatalf("got %d matches, want 1", len(m.searchMatches))
	}
	match := m.searchMatches[0]
	if match.hunkIdx != 0 {
		t.Errorf("hunkIdx = %d", match.hunkIdx)
	}
}

func TestComputeSearchMatches_CaseInsensitive(t *testing.T) {
	m := newSearchTestModel([]github.PRFile{
		{
			Filename: "main.go",
			Patch:    "@@ -1,3 +1,4 @@\n+Hello World\n+HELLO again",
		},
	})

	m.searchTerm = "hello"
	m.computeSearchMatches()

	if len(m.searchMatches) != 2 {
		t.Fatalf("got %d matches, want 2 (case insensitive)", len(m.searchMatches))
	}
}

func TestComputeSearchMatches_MultiplePerLine(t *testing.T) {
	m := newSearchTestModel([]github.PRFile{
		{
			Filename: "main.go",
			Patch:    "@@ -1,1 +1,1 @@\n+foo bar foo baz foo",
		},
	})

	m.searchTerm = "foo"
	m.computeSearchMatches()

	if len(m.searchMatches) != 3 {
		t.Fatalf("got %d matches, want 3", len(m.searchMatches))
	}

	// Verify positions (line includes "+" prefix, so "foo" starts at col 1)
	// Line is "+foo bar foo baz foo"
	if m.searchMatches[0].startCol != 1 || m.searchMatches[0].endCol != 4 {
		t.Errorf("match[0] = [%d:%d], want [1:4]", m.searchMatches[0].startCol, m.searchMatches[0].endCol)
	}
	if m.searchMatches[1].startCol != 9 || m.searchMatches[1].endCol != 12 {
		t.Errorf("match[1] = [%d:%d], want [9:12]", m.searchMatches[1].startCol, m.searchMatches[1].endCol)
	}
	if m.searchMatches[2].startCol != 17 || m.searchMatches[2].endCol != 20 {
		t.Errorf("match[2] = [%d:%d], want [17:20]", m.searchMatches[2].startCol, m.searchMatches[2].endCol)
	}
}

func TestComputeSearchMatches_EmptyTerm(t *testing.T) {
	m := newSearchTestModel([]github.PRFile{
		{Filename: "main.go", Patch: "@@ -1,1 +1,1 @@\n+some content"},
	})

	m.searchTerm = ""
	m.computeSearchMatches()

	if len(m.searchMatches) != 0 {
		t.Errorf("got %d matches for empty term, want 0", len(m.searchMatches))
	}
}

func TestComputeSearchMatches_NoMatches(t *testing.T) {
	m := newSearchTestModel([]github.PRFile{
		{Filename: "main.go", Patch: "@@ -1,1 +1,1 @@\n+hello world"},
	})

	m.searchTerm = "xyz"
	m.computeSearchMatches()

	if len(m.searchMatches) != 0 {
		t.Errorf("got %d matches, want 0", len(m.searchMatches))
	}
}

func TestComputeSearchMatches_MultipleHunks(t *testing.T) {
	m := newSearchTestModel([]github.PRFile{
		{
			Filename: "main.go",
			Patch:    "@@ -1,2 +1,2 @@\n+first error\n context\n@@ -10,2 +10,2 @@\n+second error",
		},
	})

	m.searchTerm = "error"
	m.computeSearchMatches()

	if len(m.searchMatches) != 2 {
		t.Fatalf("got %d matches, want 2 (across hunks)", len(m.searchMatches))
	}
	if m.searchMatches[0].hunkIdx == m.searchMatches[1].hunkIdx {
		t.Error("matches should be in different hunks")
	}
}

func TestGetLineSearchMatches(t *testing.T) {
	m := newSearchTestModel([]github.PRFile{
		{Filename: "main.go", Patch: "@@ -1,1 +1,1 @@\n+hello hello"},
	})

	m.searchTerm = "hello"
	m.computeSearchMatches()

	// Line 1 (index 1 — index 0 is the @@ header)
	matches := m.getLineSearchMatches(0, 1)
	if len(matches) != 2 {
		t.Fatalf("got %d matches for line, want 2", len(matches))
	}
}

func TestGetLineSearchMatches_NilMap(t *testing.T) {
	m := &DiffViewerModel{}
	matches := m.getLineSearchMatches(0, 0)
	if matches != nil {
		t.Error("expected nil for nil map")
	}
}

func TestGetLineSearchMatches_MissingHunk(t *testing.T) {
	m := newSearchTestModel([]github.PRFile{
		{Filename: "main.go", Patch: "@@ -1,1 +1,1 @@\n+hello"},
	})

	m.searchTerm = "hello"
	m.computeSearchMatches()

	matches := m.getLineSearchMatches(99, 0)
	if matches != nil {
		t.Error("expected nil for missing hunk")
	}
}

func TestSearchInfo(t *testing.T) {
	m := &DiffViewerModel{}

	// No search
	if info := m.SearchInfo(); info != "" {
		t.Errorf("SearchInfo = %q, want empty", info)
	}

	// Search with no matches
	m.searchTerm = "xyz"
	if info := m.SearchInfo(); info != "No matches" {
		t.Errorf("SearchInfo = %q, want 'No matches'", info)
	}

	// Search with matches
	m.searchMatches = []searchMatch{{}, {}, {}}
	m.searchMatchIdx = 1
	if info := m.SearchInfo(); info != "2/3" {
		t.Errorf("SearchInfo = %q, want '2/3'", info)
	}
}

func TestParseHunkNewStart(t *testing.T) {
	tests := []struct {
		name   string
		header string
		want   int
	}{
		{"standard", "@@ -1,3 +1,4 @@", 1},
		{"offset", "@@ -7,6 +12,8 @@", 12},
		{"no plus", "no header here", 0},
		{"single line", "@@ -1 +1 @@", 1},
		{"large numbers", "@@ -100,50 +200,60 @@", 200},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseHunkNewStart(tt.header)
			if got != tt.want {
				t.Errorf("parseHunkNewStart(%q) = %d, want %d", tt.header, got, tt.want)
			}
		})
	}
}

func TestFileStatusLabel(t *testing.T) {
	tests := []struct {
		name string
		file github.PRFile
		want string
	}{
		{
			"added",
			github.PRFile{Filename: "new.go", Status: "added", Additions: 50},
			"new.go (new file, +50)",
		},
		{
			"removed",
			github.PRFile{Filename: "old.go", Status: "removed", Deletions: 30},
			"old.go (deleted, -30)",
		},
		{
			"renamed",
			github.PRFile{Filename: "renamed.go", Status: "renamed"},
			"renamed.go (renamed)",
		},
		{
			"modified",
			github.PRFile{Filename: "main.go", Status: "modified", Additions: 5, Deletions: 3},
			"main.go (+5/-3)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := fileStatusLabel(tt.file)
			if got != tt.want {
				t.Errorf("fileStatusLabel() = %q, want %q", got, tt.want)
			}
		})
	}
}
