package notify

import "testing"

func TestEscapeAppleScript(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"simple", "hello", `"hello"`},
		{"empty", "", `""`},
		{"with quotes", `say "hello"`, `"say \"hello\""`},
		{"with backslash", `path\to\file`, `"path\\to\\file"`},
		{"quotes and backslash", `a\"b`, `"a\\\"b"`},
		{"multiple quotes", `"one" "two"`, `"\"one\" \"two\""`},
		{"multiple backslashes", `a\\b`, `"a\\\\b"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := escapeAppleScript(tt.input)
			if got != tt.want {
				t.Errorf("escapeAppleScript(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestEscapeAppleScript_BackslashBeforeQuotes(t *testing.T) {
	// Ensure backslashes are escaped BEFORE quotes (order matters).
	// If quotes were escaped first, the \" would become \\\" instead of \\\".
	input := `\"`
	got := escapeAppleScript(input)
	// \ → \\, then " → \" gives \\"
	want := `"\\\""`
	if got != want {
		t.Errorf("escapeAppleScript(%q) = %q, want %q", input, got, want)
	}
}
