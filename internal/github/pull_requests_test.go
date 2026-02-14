package github

import (
	"testing"
	"time"
)

func TestParseNameWithOwner(t *testing.T) {
	tests := []struct {
		input     string
		wantOwner string
		wantRepo  string
	}{
		{"alice/widget-factory", "alice", "widget-factory"},
		{"bob/test-project", "bob", "test-project"},
		{"org/repo-with-dashes", "org", "repo-with-dashes"},
		{"invalid", "", ""},
		{"", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			owner, repo := parseNameWithOwner(tt.input)
			if owner != tt.wantOwner || repo != tt.wantRepo {
				t.Errorf("parseNameWithOwner(%q) = (%q, %q), want (%q, %q)",
					tt.input, owner, repo, tt.wantOwner, tt.wantRepo)
			}
		})
	}
}

func TestConvertSearchResults(t *testing.T) {
	now := time.Date(2026, 2, 14, 12, 0, 0, 0, time.UTC)

	results := []ghSearchPR{
		{
			Number:    42,
			Title:     "Add frobnicate function",
			URL:       "https://github.com/alice/widget-factory/pull/42",
			CreatedAt: now,
			IsDraft:   false,
			Author: struct {
				Login string `json:"login"`
			}{Login: "bob"},
			Repository: struct {
				Name          string `json:"name"`
				NameWithOwner string `json:"nameWithOwner"`
			}{Name: "widget-factory", NameWithOwner: "alice/widget-factory"},
			Labels: []struct {
				Name  string `json:"name"`
				Color string `json:"color"`
			}{
				{Name: "enhancement", Color: "0075ca"},
			},
		},
		{
			Number:    7,
			Title:     "Fix widget alignment",
			URL:       "https://github.com/bob/test-project/pull/7",
			CreatedAt: now,
			IsDraft:   true,
			Author: struct {
				Login string `json:"login"`
			}{Login: "alice"},
			Repository: struct {
				Name          string `json:"name"`
				NameWithOwner string `json:"nameWithOwner"`
			}{Name: "test-project", NameWithOwner: "bob/test-project"},
		},
	}

	items := convertSearchResults(results)
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2", len(items))
	}

	// First item
	if items[0].Number != 42 {
		t.Errorf("items[0].Number = %d, want 42", items[0].Number)
	}
	if items[0].Title != "Add frobnicate function" {
		t.Errorf("items[0].Title = %q", items[0].Title)
	}
	if items[0].Repo.Owner != "alice" {
		t.Errorf("items[0].Repo.Owner = %q, want alice", items[0].Repo.Owner)
	}
	if items[0].Repo.Name != "widget-factory" {
		t.Errorf("items[0].Repo.Name = %q", items[0].Repo.Name)
	}
	if items[0].Author.Login != "bob" {
		t.Errorf("items[0].Author.Login = %q, want bob", items[0].Author.Login)
	}
	if len(items[0].Labels) != 1 || items[0].Labels[0].Name != "enhancement" {
		t.Errorf("items[0].Labels = %+v", items[0].Labels)
	}
	if items[0].Draft {
		t.Error("items[0].Draft should be false")
	}

	// Second item
	if items[1].Number != 7 {
		t.Errorf("items[1].Number = %d, want 7", items[1].Number)
	}
	if !items[1].Draft {
		t.Error("items[1].Draft should be true")
	}
	if items[1].Author.Login != "alice" {
		t.Errorf("items[1].Author.Login = %q, want alice", items[1].Author.Login)
	}
}

func TestConvertSearchResults_Empty(t *testing.T) {
	items := convertSearchResults(nil)
	if len(items) != 0 {
		t.Errorf("got %d items for nil input, want 0", len(items))
	}
}
