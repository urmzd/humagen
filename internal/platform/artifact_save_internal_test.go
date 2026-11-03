package platform

import "testing"

func TestAStructuredReviewIsToldFromText(t *testing.T) {
	for text, want := range map[string]bool{
		`{"format":"humagen.github.review.v1","summary":"ok"}`:    true,
		"```json\n{\"format\":\"humagen.github.review.v1\"}\n```": true,
		`{"format":"other"}`:                    false,
		"The review found nothing.":             false,
		`{"format": "humagen.github.review.v1"`: false,
	} {
		if got := isReview(text); got != want {
			t.Errorf("isReview(%q) = %v", text, got)
		}
	}
	if got := fileTitle("", "lead review", ".review.json"); got != "lead review.review.json" {
		t.Fatal(got)
	}
	if got := fileTitle("notes.txt", "x", ".md"); got != "notes.txt" {
		t.Fatal(got)
	}
}
