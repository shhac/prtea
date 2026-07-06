package ai

import "testing"

func TestRemoteMatches(t *testing.T) {
	cases := []struct {
		name   string
		remote string
		owner  string
		repo   string
		want   bool
	}{
		{"https", "https://github.com/acme/widgets", "acme", "widgets", true},
		{"https with .git", "https://github.com/acme/widgets.git", "acme", "widgets", true},
		{"ssh", "git@github.com:acme/widgets.git", "acme", "widgets", true},
		{"case insensitive", "https://github.com/Acme/Widgets.git", "acme", "widgets", true},
		{"wrong repo", "https://github.com/acme/gadgets", "acme", "widgets", false},
		{"wrong owner", "https://github.com/notacme/widgets", "acme", "widgets", false},
		{"owner suffix near-miss", "https://github.com/xacme/widgets", "acme", "widgets", false},
		{"repo suffix near-miss", "git@github.com:acme/mywidgets", "acme", "widgets", false},
		{"empty remote", "", "acme", "widgets", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := remoteMatches(tc.remote, tc.owner, tc.repo); got != tc.want {
				t.Errorf("remoteMatches(%q, %q, %q) = %v, want %v", tc.remote, tc.owner, tc.repo, got, tc.want)
			}
		})
	}
}
