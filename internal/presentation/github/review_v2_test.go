package github

import (
	"strings"
	"testing"

	transport "github.com/urmzd/orchagent/flow/connectors/github"
)

func reviewV2() ReviewReport {
	r := testReport()
	r.Format = ReviewFormatV2
	r.Title = "Retry safety"
	r.Verdict = "comment"
	r.Scope = &ReviewScope{Commit: strings.Repeat("a", 40)}
	for i := range r.Findings {
		r.Findings[i].ID = string(rune('a' + i))
		r.Findings[i].Evidence[0].Step = "test-1"
	}
	return r
}
func TestReviewV2RequiresPinnedScopeAndUniqueFindings(t *testing.T) {
	for _, mutate := range []func(*ReviewReport){
		func(r *ReviewReport) { r.Title = "" }, func(r *ReviewReport) { r.Scope.Commit = "main" }, func(r *ReviewReport) { r.Verdict = "approved" }, func(r *ReviewReport) { r.Findings[0].ID = "" }, func(r *ReviewReport) { r.Findings = append(r.Findings, r.Findings[0]) },
	} {
		r := reviewV2()
		mutate(&r)
		if _, err := FormatReview(r); err == nil {
			t.Fatal("invalid v2 accepted")
		}
	}
	r := reviewV2()
	r.Findings[0].Evidence[0].Step = ""
	body, err := FormatReview(r)
	if err != nil || !strings.Contains(body, "unverified") {
		t.Fatalf("missing provenance was trusted: %s %v", body, err)
	}
}
func TestNativeReviewChecksDiffAndSuggestions(t *testing.T) {
	r := reviewV2()
	replacement := "return nil"
	r.Findings[0].Location = &ReviewLocation{Path: "a.go", Line: 2}
	r.Findings[0].Change = &replacement
	files := []transport.ContextFile{{Filename: "a.go", Patch: "@@ -1,2 +1,2 @@\n unchanged\n-old\n+new"}}
	review, err := NativeReview(r, files, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(review.Comments) != 1 || !strings.Contains(review.Comments[0].Body, "```suggestion") {
		t.Fatalf("suggestion missing: %+v", review)
	}
	r.Findings[0].Location.Line = 100
	review, err = NativeReview(r, files, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(review.Comments) != 0 || !strings.Contains(review.Body, "location unverified") {
		t.Fatal("outside diff did not degrade")
	}
	r.Verdict = "approve"
	review, err = NativeReview(r, files, false)
	if err == nil && review.Event == "APPROVE" {
		t.Fatal("ungranted approval")
	}
}
