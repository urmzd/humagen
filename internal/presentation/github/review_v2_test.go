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
	if err != nil || !strings.Contains(body, "not verified") {
		t.Fatalf("missing provenance was trusted: %s %v", body, err)
	}
}

// Free text is free but bounded, and a location is a repository path.
func TestReviewV2BoundsFreeTextAndPaths(t *testing.T) {
	for name, mutate := range map[string]func(*ReviewReport){
		"long summary":    func(r *ReviewReport) { r.Summary = strings.Repeat("a", maxSummary+1) },
		"long title":      func(r *ReviewReport) { r.Findings[0].Title = strings.Repeat("a", maxTitle+1) },
		"long why":        func(r *ReviewReport) { r.Findings[0].Why = strings.Repeat("a", maxWhy+1) },
		"many comments":   func(r *ReviewReport) { r.Comments = make([]string, maxComments+1) },
		"absolute path":   func(r *ReviewReport) { r.Findings[0].Location = &ReviewLocation{Path: "/etc/passwd", Line: 1} },
		"parent path":     func(r *ReviewReport) { r.Findings[0].Location = &ReviewLocation{Path: "a/../b.go", Line: 1} },
		"backtick path":   func(r *ReviewReport) { r.Findings[0].Location = &ReviewLocation{Path: "a`b.go", Line: 1} },
		"newline in path": func(r *ReviewReport) { r.Findings[0].Location = &ReviewLocation{Path: "a\nb.go", Line: 1} },
		"backward range":  func(r *ReviewReport) { r.Findings[0].Location = &ReviewLocation{Path: "a.go", Line: 1, StartLine: 3} },
	} {
		r := reviewV2()
		mutate(&r)
		if _, err := FormatReview(r); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	r := reviewV2()
	r.Findings[0].Title = "Drop `<script>` and @someone [x](javascript:alert(1)) **now**"
	body, err := FormatReview(r)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(body, "@someone") || strings.Contains(body, "](javascript:") || strings.Contains(body, "**now**") {
		t.Fatalf("model text changed the layout: %s", body)
	}
	if !strings.Contains(body, "`<script>`") {
		t.Fatalf("inline code did not stay code: %s", body)
	}
}

// The host pins the reviewed commit: a report the model wrote without one
// parses scoped to the head, and one it wrote with another is replaced.
func TestParseReviewPinsTheHostsHead(t *testing.T) {
	head := strings.Repeat("b", 40)
	for _, text := range []string{
		`{"format":"humagen.review.v2","title":"T","verdict":"comment","summary":"S","findings":[]}`,
		`{"format":"humagen.review.v2","title":"T","verdict":"comment","scope":{"commit":"` + strings.Repeat("c", 40) + `"},"summary":"S","findings":[]}`,
	} {
		r, ok := ParseReview(text, head)
		if !ok || r.Scope == nil || r.Scope.Commit != head {
			t.Fatalf("not pinned to the head: %+v", r)
		}
		if _, err := FormatReview(r); err != nil {
			t.Fatal(err)
		}
	}
	if r, ok := ParseReview(`{"format":"humagen.review.v2","title":"T","verdict":"comment","summary":"S","findings":[]}`, "main"); !ok || r.Scope != nil {
		t.Fatalf("a malformed head scoped the review: %+v", r)
	}
	if _, ok := ParseReview("Looks good.", head); ok {
		t.Fatal("prose parsed as a review")
	}
}
func TestNativeReviewChecksDiffAndSuggestions(t *testing.T) {
	r := reviewV2()
	replacement := "return nil"
	r.Findings[0].Location = &ReviewLocation{Path: "a.go", Line: 2}
	r.Findings[0].Change = &replacement
	files := []transport.ContextFile{{Filename: "a.go", Patch: "@@ -1,2 +1,2 @@\n unchanged\n-old\n+new"}}
	review, err := NativeReview(r, files, Target{})
	if err != nil {
		t.Fatal(err)
	}
	if len(review.Comments) != 1 || !strings.Contains(review.Comments[0].Body, "```suggestion") || strings.Contains(review.Body, "Suggested change") {
		t.Fatalf("suggestion missing: %+v", review)
	}
	r.Findings[0].Location.Line = 100
	review, err = NativeReview(r, files, Target{})
	if err != nil {
		t.Fatal(err)
	}
	// A finding off the diff stays in the body, with its change, once.
	if len(review.Comments) != 0 || !strings.Contains(review.Body, "Suggested change") || strings.Contains(review.Body, "location unverified") || strings.Count(review.Body, "Duplicate write") != 2 {
		t.Fatalf("outside diff did not degrade: %s", review.Body)
	}
	r.Verdict = "approve"
	review, err = NativeReview(r, files, Target{})
	if err != nil || review.Event != "COMMENT" || strings.Contains(review.Body, "policy") {
		t.Fatalf("an approve verdict was not posted as a plain comment: %+v %v", review, err)
	}
}

// The transport refuses an approving review outright, whatever asks for one:
// nothing in Humagen can grant it.
func TestTheTransportRefusesAnApprovingReview(t *testing.T) {
	files := []transport.ContextFile{{Filename: "a.go", Patch: "@@ -1,2 +1,2 @@\n unchanged\n-old\n+new"}}
	review, err := nativeReview(reviewV2(), files, Target{})
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
	if review, err := RecoveredReview(r, files, Target{}, "APPROVE"); err != nil || review.Event != "APPROVE" {
		t.Fatalf("a posted approval was not reproduced: %+v %v", review, err)
	}
	if review, err := RecoveredReview(r, files, Target{}, "COMMENT"); err != nil || review.Event != "COMMENT" {
		t.Fatalf("a posted comment was not reproduced: %+v %v", review, err)
	}
	r.Verdict = "comment"
	if review, err := RecoveredReview(r, files, Target{}, "APPROVE"); err != nil || review.Event != "COMMENT" {
		t.Fatalf("a comment verdict became an approval: %+v %v", review, err)
	}
}

func TestFoundByNamesTheSpecialistOnEachFinding(t *testing.T) {
	r := reviewV2()
	r.Findings[0].FoundBy = "security-reviewer#a"
	r.Findings[0].Location = &ReviewLocation{Path: "a.go", Line: 2}
	files := []transport.ContextFile{{Filename: "a.go", Patch: "@@ -1,2 +1,2 @@\n unchanged\n-old\n+new"}}
	review, err := NativeReview(r, files, Target{})
	if err != nil {
		t.Fatal(err)
	}
	// The agent is named; its session label is an internal name.
	if len(review.Comments) != 1 || !strings.Contains(review.Comments[0].Body, "found by security-reviewer</sub>") {
		t.Fatalf("inline comment does not say who found it: %+v", review.Comments)
	}
	if !strings.Contains(review.Body, "found by security-reviewer</sub>") || strings.Contains(review.Body, "#a") {
		t.Fatalf("the body does not say who found it: %s", review.Body)
	}
	for _, bad := range []string{"@everyone", "Security Reviewer", "a b", strings.Repeat("x", 65), "x#" + strings.Repeat("y", 33)} {
		r.Findings[0].FoundBy = bad
		if _, err := FormatReview(r); err == nil {
			t.Fatalf("found_by %q accepted", bad)
		}
	}
}
