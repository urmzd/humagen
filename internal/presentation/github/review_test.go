package github

import (
	"encoding/json"
	"strings"
	"testing"

	render "github.com/urmzd/saige/flow/connectors/github"
)

func testReport() ReviewReport {
	return ReviewReport{Format: ReviewFormat, Summary: "Retries can duplicate writes.", Comments: []string{"Scope: inspected the retry path; tests were not executed."}, Findings: []ReviewFinding{
		{Severity: "error", Title: "Duplicate write", Suggestion: "Deduplicate by request ID", Evidence: []ReviewEvidence{{Observation: "The retry calls Create again.", Source: "https://github.com/acme/repo/blob/abc/retry.go#L42"}}, Prompt: "Add a retry test asserting one write."},
		{Severity: "warning", Title: "Missing regression", Suggestion: "Cover timeout recovery", Evidence: []ReviewEvidence{{Observation: "The inspected test covers only success.", Source: "retry_test.go:12"}}, Prompt: "Test a timeout after the write succeeds."},
		{Severity: "info", Title: "Existing ordering", Suggestion: "Keep the ordering guarantee", Evidence: []ReviewEvidence{{Observation: "Writes are serialized by run.", Source: "router.go:30"}}, Prompt: "Preserve this invariant when changing retries."},
	}, Related: []ReviewLink{{Title: "Idempotency", URL: "https://example.test/idempotency"}}}
}

func TestReviewReadsAsProseInSeverityOrder(t *testing.T) {
	r := testReport()
	r.Findings[0], r.Findings[2] = r.Findings[2], r.Findings[0]
	body, err := FormatReview(r)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"**Two problems worth fixing before merging.**", "Retries can duplicate writes.", "- Scope: inspected", "**1. Duplicate write**\n<sub>Must fix</sub>", "**2. Missing regression**", "**3. Existing ordering**\n<sub>Note</sub>", "<details><summary>Evidence</summary>", "#L42)", "Related: [Idempotency](https://example.test/idempotency)", "<!-- humagen.github.review.v1 verdict=comment errors=1 warnings=1 info=1 -->"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in %s", want, body)
		}
	}
	// Three findings read as prose alone: no table, and the summary first.
	if strings.Contains(body, "| # |") || strings.Index(body, "Retries can duplicate") > strings.Index(body, "**1.") {
		t.Fatal(body)
	}
	assertHumanVoice(t, body)
}

func TestTheVerdictIsSaidInWords(t *testing.T) {
	finding := func(sev string) ReviewFinding { return ReviewFinding{Severity: sev} }
	for _, tc := range []struct {
		verdict  string
		findings []ReviewFinding
		want     string
	}{
		{"approve", nil, "Looks good to merge."},
		{"approve", []ReviewFinding{finding("warning")}, "Looks good to merge after one fix."},
		{"approve", []ReviewFinding{finding("info"), finding("info")}, "Looks good to merge. Two notes below."},
		{"request_changes", []ReviewFinding{finding("error"), finding("warning"), finding("info")}, "Needs changes: one problem to fix before merging, and one more worth fixing."},
		{"request_changes", []ReviewFinding{finding("error")}, "Needs changes: one problem to fix before merging."},
		{"request_changes", []ReviewFinding{finding("warning"), finding("warning")}, "Needs changes: two problems worth fixing before merging."},
		{"request_changes", nil, "Needs changes before merging."},
		{"comment", nil, "No problems found."},
		{"comment", []ReviewFinding{finding("info")}, "One note, nothing blocking."},
		{"comment", []ReviewFinding{finding("error")}, "One problem worth fixing before merging."},
	} {
		if got := verdictLine(tc.verdict, tc.findings); got != tc.want {
			t.Errorf("%s %d: %q, want %q", tc.verdict, len(tc.findings), got, tc.want)
		}
	}
}

func TestReviewRejectsUnsupportedClaimsAndInvalidSeverity(t *testing.T) {
	for _, mutate := range []func(*ReviewReport){
		func(r *ReviewReport) { r.Findings[0].Evidence = nil },
		func(r *ReviewReport) { r.Findings[0].Evidence[0].Source = "" },
		func(r *ReviewReport) { r.Findings[0].Prompt = "" },
		func(r *ReviewReport) { r.Findings[0].Severity = "critical" },
		func(r *ReviewReport) { r.Summary = "" },
	} {
		r := testReport()
		mutate(&r)
		if _, err := FormatReview(r); err == nil {
			t.Fatal("accepted incomplete report")
		}
	}
}

func TestReviewCellsCannotBreakTheTable(t *testing.T) {
	r := testReport()
	r.Findings[0].Title = "a | b\n<script> @someone [bad](javascript:alert(1))"
	r.Findings[0].Evidence[0].Source = "javascript:alert(1)"
	body, err := FormatReview(r)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(body, "<script>") || strings.Contains(body, "@someone") || strings.Contains(body, "](javascript:") {
		t.Fatal(body)
	}
	if !strings.Contains(body, "a &#124; b") {
		t.Fatal("pipe not escaped")
	}
	r.Findings = append(r.Findings, r.Findings[1], r.Findings[2])
	body, err = FormatReview(r)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "| 1 | a &#124; b &lt;script&gt;") {
		t.Fatalf("a pipe broke the findings table: %s", body)
	}
}

func TestStructuredReviewStreamsProgressThenRendersTheFinalReport(t *testing.T) {
	encoded, _ := json.Marshal(testReport())
	v := render.View{Agent: "reviewer", Status: render.StatusRunning, Text: string(encoded[:80])}
	if body := RenderReview(v); strings.Contains(body, ReviewFormat) || !strings.Contains(body, "Reviewing the change") {
		t.Fatal(body)
	}
	v.Status = render.StatusDone
	v.Text = string(encoded)
	if body := RenderReview(v); !strings.Contains(body, "**1. Duplicate write**") || strings.Contains(hidden.ReplaceAllString(body, ""), ReviewFormat) {
		t.Fatal(body)
	}
	v.Text = string(encoded[:80])
	if body := RenderReview(v); !strings.Contains(body, "The review is incomplete") || strings.Contains(body, "No problems") {
		t.Fatal(body)
	}
}

// An agent told before the rename to answer in warp.github.review.v1 still
// gets a review: complete, held back while it streams, or incomplete.
func TestAReviewUnderTheOldFormatIDStillRenders(t *testing.T) {
	report := testReport()
	report.Format = "warp.github.review.v1"
	if _, err := FormatReview(report); err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(report)
	v := render.View{Agent: "reviewer", Status: render.StatusRunning, Text: string(encoded[:80])}
	if body := RenderReview(v); strings.Contains(body, "warp.github.review.v1") || !strings.Contains(body, "Reviewing the change") {
		t.Fatal(body)
	}
	v.Status, v.Text = render.StatusDone, string(encoded)
	if body := RenderReview(v); !strings.Contains(body, "**1. Duplicate write**") {
		t.Fatal(body)
	}
	report.Format = "other.github.review.v1"
	if _, err := FormatReview(report); err == nil {
		t.Fatal("another format id rendered as a review")
	}
}

// A model often wraps its report in a ```json fence. The fenced report is
// held back while it streams and renders as a review once complete.
func TestAFencedReviewRendersAsAReview(t *testing.T) {
	encoded, _ := json.Marshal(testReport())
	fenced := "```json\n" + string(encoded) + "\n```"
	v := render.View{Agent: "reviewer", Status: render.StatusRunning, Text: fenced[:90]}
	if body := RenderReview(v); strings.Contains(body, ReviewFormat) || !strings.Contains(body, "Reviewing the change") {
		t.Fatal(body)
	}
	v.Status, v.Text = render.StatusDone, fenced
	if body := RenderReview(v); !strings.Contains(body, "**1. Duplicate write**") || strings.Contains(hidden.ReplaceAllString(body, ""), ReviewFormat) {
		t.Fatal(body)
	}
	for in, want := range map[string]string{
		"  {\"a\":1}  ":           `{"a":1}`,
		"```\n{\"a\":1}\n```":     `{"a":1}`,
		"```json\n{\"a\":1}":      `{"a":1}`,
		"```inline``` text":       "```inline``` text",
		"plain answer\n```\ncode": "plain answer\n```\ncode",
	} {
		if got := ReviewJSON(in); got != want {
			t.Errorf("ReviewJSON(%q) = %q, want %q", in, got, want)
		}
	}
}
