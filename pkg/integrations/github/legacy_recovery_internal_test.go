package github

import (
	"strings"
	"testing"

	"github.com/urmzd/saige/flow/address"
	transport "github.com/urmzd/saige/flow/connectors/github"
	"github.com/urmzd/saige/flow/deliverystate"
	"github.com/urmzd/saige/flow/event"
)

// A review posted by the code from before reviews read as prose, whose
// receipt was not kept, is recovered in that layout after the deploy: it is
// receipted, its finding stays mapped, and nothing is posted again.
func TestAReviewPostedInTheEarlierLayoutIsRecovered(t *testing.T) {
	api := transport.NewFake()
	ref := transport.Ref{Owner: "acme", Repo: "repo", Number: 7}
	sha := strings.Repeat("a", 40)
	snapshot := transport.Context{Files: []transport.ContextFile{{Filename: "a.go", Patch: "@@ -1 +1 @@\n-old\n+new"}}}
	snapshot.Subject.Head.SHA = sha
	if err := api.SeedContext(ref, snapshot); err != nil {
		t.Fatal(err)
	}
	target := address.MustParse("github://acme/repo/pull/7").WithParam("head_sha", sha)
	text := `{"format":"humagen.review.v2","title":"Review","verdict":"request_changes","scope":{"commit":"` + sha + `"},"summary":"One bug.","findings":[{"id":"f1","severity":"error","title":"Wrong value","suggestion":"Return the old value","evidence":[{"observation":"It returns new","source":"a.go:1","step":"test-1"}],"prompt":"Fix it","location":{"path":"a.go","line":1}}]}`
	env := event.Envelope{Tenant: "tenant", Run: "run", Seq: 1, Channel: target.String(), Type: event.TypeCompleted, Data: event.Completed{Text: text}}
	store := deliverystate.NewMemory()
	deliver := func(scope string, legacy bool) error {
		c, err := New(Config{API: api, Tenant: "tenant", Login: "humagen[bot]", DeliveryState: store, DeliveryScope: scope, legacyPosts: legacy})
		if err != nil {
			t.Fatal(err)
		}
		return c.Deliver(t.Context(), target, env)
	}
	if err := deliver("before-deploy", true); err != nil {
		t.Fatal(err)
	}
	posted := api.SubmittedReviews()
	if len(posted) != 1 || !strings.Contains(posted[0].Review.Body, "| Level |") {
		t.Fatalf("the earlier layout was not posted: %+v", posted)
	}
	if err := deliver("after-deploy", false); err != nil {
		t.Fatalf("the earlier review was not recovered: %v", err)
	}
	if n := len(api.SubmittedReviews()); n != 1 {
		t.Fatalf("recovery posted again: %d reviews", n)
	}
	// Recorded as posted in the current layout, the same review no longer
	// matches: the record, not a guess, picks the layout.
	if err := newLayouts(Config{DeliveryState: store}).mark(transport.View{Tenant: "tenant", RunID: "run", Channel: target.String()}); err != nil {
		t.Fatal(err)
	}
	if err := deliver("after-deploy-again", false); err == nil {
		t.Fatal("a review in the earlier layout was recovered as the current one")
	}
}

// A review posted in the prose layout from before reviews carried the
// agent's mark is recovered without the mark, and the record picks which.
func TestAReviewPostedBeforeTheAgentMarkIsRecovered(t *testing.T) {
	api := transport.NewFake()
	ref := transport.Ref{Owner: "acme", Repo: "repo", Number: 7}
	sha := strings.Repeat("a", 40)
	snapshot := transport.Context{Files: []transport.ContextFile{{Filename: "a.go", Patch: "@@ -1 +1 @@\n-old\n+new"}}}
	snapshot.Subject.Head.SHA = sha
	if err := api.SeedContext(ref, snapshot); err != nil {
		t.Fatal(err)
	}
	target := address.MustParse("github://acme/repo/pull/7").WithParam("head_sha", sha)
	text := `{"format":"humagen.review.v2","title":"Review","verdict":"comment","scope":{"commit":"` + sha + `"},"summary":"Looked.","findings":[]}`
	env := event.Envelope{Tenant: "tenant", Agent: "gatekeeper", Run: "run", Seq: 1, Channel: target.String(), Type: event.TypeCompleted, Data: event.Completed{Text: text}}
	store := deliverystate.NewMemory()
	deliver := func(scope string, unmarked bool) error {
		c, err := New(Config{API: api, Tenant: "tenant", Login: "humagen[bot]", DeliveryState: store, DeliveryScope: scope, unmarkedPosts: unmarked})
		if err != nil {
			t.Fatal(err)
		}
		return c.Deliver(t.Context(), target, env)
	}
	if err := deliver("before-deploy", true); err != nil {
		t.Fatal(err)
	}
	if posted := api.SubmittedReviews(); len(posted) != 1 || strings.Contains(posted[0].Review.Body, "humagen.review.agent") {
		t.Fatalf("the unmarked layout was not posted: %+v", posted)
	}
	if err := deliver("after-deploy", false); err != nil {
		t.Fatalf("the unmarked review was not recovered: %v", err)
	}
	if n := len(api.SubmittedReviews()); n != 1 {
		t.Fatalf("recovery posted again: %d reviews", n)
	}
	if err := newLayouts(Config{DeliveryState: store}).mark(transport.View{Tenant: "tenant", RunID: "run", Channel: target.String()}); err != nil {
		t.Fatal(err)
	}
	if err := deliver("after-deploy-again", false); err == nil {
		t.Fatal("an unmarked review was recovered as a marked one")
	}
}
