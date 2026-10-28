package github

import (
	"strings"
	"testing"
	"unicode/utf8"

	transport "github.com/urmzd/humagen/fiber/pkg/connectors/github"
)

func testEvidence() transport.Context {
	var evidence transport.Context
	evidence.Subject.Title = "Fix retries"
	evidence.Subject.Body = "Deduplicate by request ID."
	evidence.Subject.Head.SHA = "abc123"
	evidence.Files = []transport.ContextFile{
		{Filename: "retry.go", Status: "modified", Patch: "@@ -1 +1 @@"},
		{Filename: "logo.png", Status: "added"},
	}
	return evidence
}

func TestGuideListsFilesAndTheCommitItCovers(t *testing.T) {
	body := GuideBody(testEvidence(), "reviewer", "review-bot", false)
	for _, want := range []string{
		"## PR guide", "**Fix retries**", "Current commit: `abc123`", "### Intent", "Deduplicate by request ID.",
		"| retry.go | modified | Patch available |", "| logo.png | added | Patch unavailable; inspect the file |",
		"### Working with reviewer", "Tag @review-bot",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in %s", want, body)
		}
	}
	for _, unwanted := range []string{"/fix", "More context is available", "No file patches"} {
		if strings.Contains(body, unwanted) {
			t.Errorf("unexpected %q in %s", unwanted, body)
		}
	}
}

func TestGuideStatesWhatItCouldNotSee(t *testing.T) {
	evidence := testEvidence()
	evidence.Subject.Body = ""
	evidence.Files = nil
	evidence.NextPage = 2
	body := GuideBody(evidence, "reviewer", "review-bot", true)
	for _, want := range []string{"No file patches were returned", "More context is available", "`/fix <requested change>`", "`/check <workflow-run-id>`"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in %s", want, body)
		}
	}
	if strings.Contains(body, "### Intent") {
		t.Error("intent section rendered without a body")
	}
}

func TestGuideEscapesAuthorText(t *testing.T) {
	evidence := testEvidence()
	evidence.Subject.Title = "<b>x</b> @team | [a](javascript:alert(1))"
	evidence.Subject.Body = "a" + strings.Repeat("é", 2000)
	evidence.Files[0].Filename = "a|b.go"
	body := GuideBody(evidence, "re*viewer", "review-bot", false)
	if strings.Contains(body, "<b>") || strings.Contains(body, "@team") || strings.Contains(body, "](javascript:") || strings.Contains(body, "a|b.go") || strings.Contains(body, "re*viewer") {
		t.Fatal(body)
	}
	if !utf8.ValidString(body) {
		t.Fatal("clamped intent split a UTF-8 sequence")
	}
}

func TestClampKeepsWholeRunes(t *testing.T) {
	for _, tc := range []struct {
		in   string
		n    int
		want string
	}{
		{"abc", 5, "abc"},
		{"abc", 2, "ab"},
		{"aé", 2, "a"},
		{"aé", 3, "aé"},
		{"🔴", 3, ""},
		{"abc", 0, ""},
	} {
		if got := clamp(tc.in, tc.n); got != tc.want {
			t.Errorf("clamp(%q, %d) = %q, want %q", tc.in, tc.n, got, tc.want)
		}
	}
}
