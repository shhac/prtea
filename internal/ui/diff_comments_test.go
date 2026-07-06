package ui

import "testing"

func TestHunkLineIndex(t *testing.T) {
	hunk := []string{
		"@@ -10,3 +10,4 @@ func foo() {",
		" context line",  // new line 10
		"-removed line",  // no new line
		"+added line",    // new line 11
		"+another added", // new line 12
		`\ No newline at end of file`,
	}

	cases := []struct {
		name       string
		targetLine int
		want       int
	}{
		{"first context line", 10, 1},
		{"added line", 11, 3},
		{"second added line", 12, 4},
		{"not found falls back to 0", 999, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := hunkLineIndex(hunk, tc.targetLine); got != tc.want {
				t.Errorf("hunkLineIndex(%d) = %d, want %d", tc.targetLine, got, tc.want)
			}
		})
	}
}

func TestHunkLineIndexMultipleHeaders(t *testing.T) {
	hunk := []string{
		"@@ -1,2 +1,2 @@",
		" a", // new line 1
		" b", // new line 2
		"@@ -50,2 +60,2 @@",
		" c", // new line 60
		"+d", // new line 61
	}
	if got := hunkLineIndex(hunk, 61); got != 5 {
		t.Errorf("hunkLineIndex(61) = %d, want 5", got)
	}
	if got := hunkLineIndex(hunk, 2); got != 2 {
		t.Errorf("hunkLineIndex(2) = %d, want 2", got)
	}
}

func TestFormatCommentTarget(t *testing.T) {
	if got := formatCommentTarget("a.go", 0, 5); got != "a.go:5" {
		t.Errorf("single line = %q", got)
	}
	if got := formatCommentTarget("a.go", 3, 5); got != "a.go:3-5" {
		t.Errorf("range = %q", got)
	}
}
