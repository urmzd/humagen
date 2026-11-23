package platform

import (
	"encoding/json"
	"strings"
	"testing"

	github "github.com/urmzd/humagen/internal/presentation/github"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/saige/flow/runtime"
)

// onlineReview is a valid review as a model writes it: without the
// reviewed commit, which the host pins.
func onlineReview(t *testing.T) string {
	t.Helper()
	raw, err := json.Marshal(github.ReviewReport{Format: github.ReviewFormatV2, Title: "Review", Verdict: "comment", Summary: "No findings in the inspected diff.", Findings: []github.ReviewFinding{}})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestOnlineReviewFormatChecksOnlyARequestedReview(t *testing.T) {
	valid := onlineReview(t)
	head := strings.Repeat("a", 40)
	review := wire.ShapeReview
	for _, tc := range []struct {
		name, shape, head, answer string
		applies, passed           bool
	}{
		{"a reply is not checked", wire.ShapeReply, head, "Done", false, false},
		{"a reply that declares a review is not checked", wire.ShapeReply, head, `{"format":"humagen.review.v2","title":`, false, false},
		{"a run from before shapes is not checked", "", head, `{"format":"humagen.review.v2","title":`, false, false},
		{"a review scoped by the host", review, head, valid, true, true},
		{"a fenced review", review, head, "```json\n" + valid + "\n```", true, true},
		{"prose where a review was asked", review, head, "Looks good", true, false},
		{"the first review format", review, head, `{"format":"humagen.github.review.v1","summary":"Looks good","findings":[]}`, true, true},
		{"an unknown format", review, head, `{"format":"custom.data","summary":"Looks good"}`, true, false},
		{"truncated", review, head, `{"format":"humagen.review.v2","title":`, true, false},
		{"invalid verdict", review, head, strings.Replace(valid, `"comment"`, `"approved"`, 1), true, false},
		{"no pinned head", review, "", valid, true, false},
		{"oversized", review, head, valid + strings.Repeat(" ", onlineFormatBytes), true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			verdict, applies := checkReviewFormat(tc.shape, tc.head, tc.answer)
			if applies != tc.applies || verdict.Passed != tc.passed {
				t.Fatalf("%+v applies=%v", verdict, applies)
			}
			if len(verdict.Reason) > 200 || strings.Contains(verdict.Reason, tc.answer) && tc.answer != "" {
				t.Fatalf("format failure disclosed answer: %+v", verdict)
			}
		})
	}
}

// The check reads the shape saved on the run's tag when it started, not
// its agent's instructions: a plain reply from an agent whose prompt names
// the review format raises nothing, and a retry is assessed as first asked.
func TestOnlineReviewFormatUsesTheShapeSavedOnTheRun(t *testing.T) {
	p, err := New(Config{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close() }()
	spec := wire.AgentSpec{Name: "reviewer", Provider: "offline", Model: "offline", SystemPrompt: "Return " + github.ReviewFormatV2}
	if _, err := p.Agents.Create(t.Context(), "t", spec, "test"); err != nil {
		t.Fatal(err)
	}
	record := func(id, shape string) runs.Run {
		tag := wire.NewEvent(wire.Raise{ID: id, Tenant: "t", Agent: "reviewer", Origin: "github", Channel: "github://acme/site/pull/7?head_sha=" + strings.Repeat("a", 40), Text: "Review"})
		if shape != "" {
			wire.SetShape(&tag, shape)
		}
		runID := runtime.RunID("t", tag.ID)
		if err := p.Runs.Record(runID, tag, 1); err != nil {
			t.Fatal(err)
		}
		run, _ := p.Runs.Get("t", runID)
		return run
	}
	reply := record("reply", wire.ShapeReply)
	reply.Answer = "The retry limit is three."
	if verdict, applies := p.onlineFormat(reply); applies {
		t.Fatalf("a plain reply was format-checked: %+v", verdict)
	}
	review := record("review", wire.ShapeReview)
	review.Answer = "Looks good"
	if verdict, applies := p.onlineFormat(review); !applies || verdict.Passed {
		t.Fatalf("prose passed where a review was asked: %+v %v", verdict, applies)
	}
	review.Answer = onlineReview(t)
	if verdict, applies := p.onlineFormat(review); !applies || !verdict.Passed {
		t.Fatalf("a review scoped to the pinned head failed: %+v %v", verdict, applies)
	}
}
