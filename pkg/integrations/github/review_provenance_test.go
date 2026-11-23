package github_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	presentation "github.com/urmzd/humagen/internal/presentation/github"
	github "github.com/urmzd/humagen/pkg/integrations/github"
	"github.com/urmzd/saige/flow/address"
	transport "github.com/urmzd/saige/flow/connectors/github"
	"github.com/urmzd/saige/flow/event"
)

func TestReviewHostProjectionPrecedesNativeAndCommentRendering(t *testing.T) {
	for _, kind := range []string{"pull", "issues"} {
		t.Run(kind, func(t *testing.T) {
			api := transport.NewFake()
			sha := strings.Repeat("a", 40)
			proof := transport.Context{}
			proof.Subject.Head.SHA = sha
			if err := api.SeedContext(transport.Ref{Owner: "acme", Repo: "repo", Number: 1}, proof); err != nil {
				t.Fatal(err)
			}
			report := presentation.ReviewReport{Format: presentation.ReviewFormatV2, Title: "Review", Verdict: "comment", Scope: &presentation.ReviewScope{Commit: sha}, Summary: "Inspected changes", Findings: []presentation.ReviewFinding{{ID: "one", Severity: "warning", Title: "Input", Suggestion: "Check it", Prompt: "Fix input", Evidence: []presentation.ReviewEvidence{{Step: "fabricated", Observation: "Claimed a failure"}}}}}
			raw, _ := json.Marshal(report)
			calls := 0
			peer, err := github.New(github.Config{API: api, Owners: []string{"acme"}, Tenant: "t", Login: "humagen[bot]", Interval: -1, ReviewText: func(v transport.View) (string, error) {
				calls++
				var safe presentation.ReviewReport
				if err := json.Unmarshal([]byte(v.Text), &safe); err != nil {
					t.Fatal(err)
				}
				safe.Findings[0].Evidence[0].Step = ""
				safe.Findings[0].Evidence[0].Source = "Unverified evidence"
				b, _ := json.Marshal(safe)
				return string(b), nil
			}})
			if err != nil {
				t.Fatal(err)
			}
			target := address.MustParse("github://acme/repo/" + kind + "/1?head_sha=" + sha)
			e := event.Envelope{Tenant: "t", ID: "completed", Run: "run", Seq: 1, Agent: "reviewer", Type: event.TypeCompleted, Time: time.Now(), Channel: target.String(), Data: event.Completed{Text: string(raw)}}
			if err := peer.Deliver(t.Context(), target, e); err != nil {
				t.Fatal(err)
			}
			body := ""
			if kind == "pull" {
				if reviews := api.SubmittedReviews(); len(reviews) == 1 {
					body = reviews[0].Review.Body
				} else {
					t.Fatalf("native reviews: %+v", reviews)
				}
			} else {
				if comments := api.Comments(); len(comments) == 1 {
					body = comments[0].Body
				} else {
					t.Fatalf("comments: %+v", comments)
				}
			}
			if calls == 0 || strings.Contains(body, "fabricated") || !strings.Contains(strings.ToLower(body), "not verified") {
				t.Fatalf("host projection bypassed: %q calls=%d", body, calls)
			}
		})
	}
}

// The host pins the reviewed commit from the run's channel: a report the
// model wrote without one is posted at the pinned head, with the session
// link under it. A run the host saved as a reply posts no native review,
// whatever its agent answered.
func TestNativeReviewIsPinnedByTheHostAndOnlyForARequestedReview(t *testing.T) {
	for _, asked := range []bool{true, false} {
		api := transport.NewFake()
		sha := strings.Repeat("a", 40)
		proof := transport.Context{}
		proof.Subject.Head.SHA = sha
		if err := api.SeedContext(transport.Ref{Owner: "acme", Repo: "repo", Number: 1}, proof); err != nil {
			t.Fatal(err)
		}
		answer := `{"format":"humagen.review.v2","title":"Review","verdict":"comment","summary":"Inspected the change.","findings":[]}`
		peer, err := github.New(github.Config{API: api, Owners: []string{"acme"}, Tenant: "t", Login: "humagen[bot]", Interval: -1,
			AsksReview:  func(transport.View) bool { return asked },
			SessionLink: func(transport.View) string { return "https://humagen.example/sessions/s_1" },
		})
		if err != nil {
			t.Fatal(err)
		}
		target := address.MustParse("github://acme/repo/pull/1?head_sha=" + sha)
		e := event.Envelope{Tenant: "t", ID: "completed", Run: "run", Seq: 1, Agent: "reviewer", Type: event.TypeCompleted, Time: time.Now(), Channel: target.String(), Data: event.Completed{Text: answer}}
		if err := peer.Deliver(t.Context(), target, e); err != nil {
			t.Fatal(err)
		}
		reviews := api.SubmittedReviews()
		if !asked {
			if len(reviews) != 0 {
				t.Fatalf("a reply posted a native review: %+v", reviews)
			}
			continue
		}
		if len(reviews) != 1 || reviews[0].Review.CommitID != sha || !strings.Contains(reviews[0].Review.Body, "**No problems found.**") || !strings.Contains(reviews[0].Review.Body, "[View the session in Humagen ↗](https://humagen.example/sessions/s_1)") {
			t.Fatalf("native review: %+v", reviews)
		}
	}
}
