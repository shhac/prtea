package ui

import "testing"

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
