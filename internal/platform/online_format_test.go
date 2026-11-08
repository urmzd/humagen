package platform

import (
	"encoding/json"
	"strings"
	"testing"

	github "github.com/urmzd/humagen/internal/presentation/github"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/runs"
)

func onlineReview(t *testing.T) string {
	t.Helper()
	raw, err := json.Marshal(github.ReviewReport{Format: github.ReviewFormatV2, Title: "Review", Verdict: "comment", Summary: "No findings in the inspected diff.", Scope: &github.ReviewScope{Commit: strings.Repeat("a", 40)}, Findings: []github.ReviewFinding{}})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestOnlineReviewFormatIsBoundedDeterministicAndOnlyAssessesKnownContracts(t *testing.T) {
	valid := onlineReview(t)
	for _, tc := range []struct {
		name, instructions, answer string
		applies, passed            bool
	}{
		{"ordinary prose", "Be concise", "Done", false, false},
		{"unknown contract", "Use custom.data", `{"format":"custom.data","value":1}`, false, false},
		{"nested example is not declared output", "", `{"example":{"format":"humagen.review.v2"},"format":"custom.data"}`, false, false},
		{"output declares review", "", valid, true, true},
		{"fenced review", github.ReviewFormatV2, "```json\n" + valid + "\n```", true, true},
		{"requested review omitted", github.ReviewFormatV2, "Looks good", true, false},
		{"wrong review version", github.ReviewFormatV2, `{"format":"humagen.github.review.v1","summary":"Looks good","findings":[]}`, true, false},
		{"truncated declaration", "", `{"format":"humagen.review.v2","title":`, true, false},
		{"invalid verdict", "", strings.Replace(valid, `"comment"`, `"approved"`, 1), true, false},
		{"unpinned commit", "", strings.Replace(valid, strings.Repeat("a", 40), "main", 1), true, false},
		{"oversized review", github.ReviewFormatV2, valid + strings.Repeat(" ", onlineFormatBytes), true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			verdict, applies := checkReviewFormat(tc.instructions, tc.answer)
			if applies != tc.applies || verdict.Passed != tc.passed {
				t.Fatalf("%+v applies=%v", verdict, applies)
			}
			if len(verdict.Reason) > 200 || strings.Contains(verdict.Reason, tc.answer) && tc.answer != "" {
				t.Fatalf("format failure disclosed answer: %+v", verdict)
			}
		})
	}
}

func TestOnlineReviewFormatUsesTheRunsImmutableAgentRevision(t *testing.T) {
	p, err := New(Config{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close() }()
	spec := wire.AgentSpec{Name: "reviewer", Provider: "offline", Model: "offline", SystemPrompt: "Return " + github.ReviewFormatV2}
	first, err := p.Agents.Create(t.Context(), "t", spec, "test")
	if err != nil {
		t.Fatal(err)
	}
	spec.SystemPrompt = "Answer in ordinary prose"
	if _, err = p.Agents.Revise(t.Context(), "t", spec, first.Rev, "test"); err != nil {
		t.Fatal(err)
	}
	run := runs.Run{Tenant: "t", Agent: spec.Name, Rev: first.Rev, Answer: "Looks good"}
	verdict, applies := p.onlineFormat(run)
	if !applies || verdict.Passed {
		t.Fatalf("later instructions changed historical assessment: %+v %v", verdict, applies)
	}
	run.Answer = onlineReview(t)
	verdict, applies = p.onlineFormat(run)
	if !applies || !verdict.Passed {
		t.Fatalf("historical valid review failed: %+v %v", verdict, applies)
	}
}
