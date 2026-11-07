package github

import (
	"strings"
	"testing"
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
