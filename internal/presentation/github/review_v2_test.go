package github

import (
	"errors"
	"strings"
	"testing"

	transport "github.com/urmzd/saige/flow/connectors/github"
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
	review, err := NativeReview(r, files)
	if err != nil {
		t.Fatal(err)
	}
	if len(review.Comments) != 1 || !strings.Contains(review.Comments[0].Body, "```suggestion") {
		t.Fatalf("suggestion missing: %+v", review)
	}
	r.Findings[0].Location.Line = 100
	review, err = NativeReview(r, files)
	if err != nil {
		t.Fatal(err)
	}
	if len(review.Comments) != 0 || !strings.Contains(review.Body, "location unverified") {
		t.Fatal("outside diff did not degrade")
	}
	r.Verdict = "approve"
	review, err = NativeReview(r, files)
	if err != nil || review.Event != "COMMENT" || strings.Contains(review.Body, "policy") {
		t.Fatalf("an approve verdict was not posted as a plain comment: %+v %v", review, err)
	}
}

// The transport refuses an approving review outright, whatever asks for one:
// nothing in Humagen can grant it.
func TestTheTransportRefusesAnApprovingReview(t *testing.T) {
	files := []transport.ContextFile{{Filename: "a.go", Patch: "@@ -1,2 +1,2 @@\n unchanged\n-old\n+new"}}
	review, err := nativeReview(reviewV2())
	if err != nil {
		t.Fatal(err)
	}
	review.Event = "APPROVE"
	if _, err := transport.PrepareReview(review, files); !errors.Is(err, transport.ErrNoApproval) {
		t.Fatalf("an approving review was prepared: %v", err)
	}
}

// Agents never approve. Recovery reproduces an approval posted before that
// rule, so a retried delivery matches it, and reproduces anything else as new.
func TestRecoveredReviewReproducesWhatWasPosted(t *testing.T) {
	r := reviewV2()
	r.Verdict = "approve"
	files := []transport.ContextFile{{Filename: "a.go", Patch: "@@ -1,2 +1,2 @@\n unchanged\n-old\n+new"}}
	if review, err := RecoveredReview(r, files, "APPROVE"); err != nil || review.Event != "APPROVE" {
		t.Fatalf("a posted approval was not reproduced: %+v %v", review, err)
	}
	if review, err := RecoveredReview(r, files, "COMMENT"); err != nil || review.Event != "COMMENT" {
		t.Fatalf("a posted comment was not reproduced: %+v %v", review, err)
	}
	r.Verdict = "comment"
	if review, err := RecoveredReview(r, files, "APPROVE"); err != nil || review.Event != "COMMENT" {
		t.Fatalf("a comment verdict became an approval: %+v %v", review, err)
	}
}

func TestFoundByNamesTheSpecialistOnEachFinding(t *testing.T) {
	r := reviewV2()
	r.Findings[0].FoundBy = "security-reviewer#a"
	r.Findings[0].Location = &ReviewLocation{Path: "a.go", Line: 2}
	files := []transport.ContextFile{{Filename: "a.go", Patch: "@@ -1,2 +1,2 @@\n unchanged\n-old\n+new"}}
	review, err := NativeReview(r, files)
	if err != nil {
		t.Fatal(err)
	}
	if len(review.Comments) != 1 || !strings.Contains(review.Comments[0].Body, "Found by security-reviewer#a") {
		t.Fatalf("inline comment does not say who found it: %+v", review.Comments)
	}
	if !strings.Contains(review.Body, "<br>Found by security-reviewer#a") {
		t.Fatalf("findings table does not say who found it: %s", review.Body)
	}
	for _, bad := range []string{"@everyone", "Security Reviewer", "a b", strings.Repeat("x", 65), "x#" + strings.Repeat("y", 33)} {
		r.Findings[0].FoundBy = bad
		if _, err := FormatReview(r); err == nil {
			t.Fatalf("found_by %q accepted", bad)
		}
	}
}
