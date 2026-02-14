package github

import "testing"

func TestDeduplicateReviews(t *testing.T) {
	t.Run("keeps latest per user", func(t *testing.T) {
		reviews := []ghReview{
			{Author: struct {
				Login string `json:"login"`
			}{Login: "alice"}, State: "CHANGES_REQUESTED"},
			{Author: struct {
				Login string `json:"login"`
			}{Login: "alice"}, State: "APPROVED"},
			{Author: struct {
				Login string `json:"login"`
			}{Login: "bob"}, State: "APPROVED"},
		}
		got := deduplicateReviews(reviews)
		if len(got) != 2 {
			t.Fatalf("got %d reviews, want 2", len(got))
		}
		// alice's latest should be APPROVED (overwrites CHANGES_REQUESTED)
		byUser := make(map[string]string)
		for _, r := range got {
			byUser[r.Author.Login] = r.State
		}
		if byUser["alice"] != "APPROVED" {
			t.Errorf("alice's state = %q, want APPROVED", byUser["alice"])
		}
		if byUser["bob"] != "APPROVED" {
			t.Errorf("bob's state = %q, want APPROVED", byUser["bob"])
		}
	})

	t.Run("skips COMMENTED reviews", func(t *testing.T) {
		reviews := []ghReview{
			{Author: struct {
				Login string `json:"login"`
			}{Login: "charlie"}, State: "COMMENTED"},
		}
		got := deduplicateReviews(reviews)
		if len(got) != 0 {
			t.Errorf("got %d reviews, want 0 (COMMENTED should be skipped)", len(got))
		}
	})

	t.Run("empty input", func(t *testing.T) {
		got := deduplicateReviews(nil)
		if len(got) != 0 {
			t.Errorf("got %d reviews, want 0", len(got))
		}
	})
}
