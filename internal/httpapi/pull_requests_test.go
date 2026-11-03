package httpapi

import (
	"fmt"
	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/integrations/github"
	"github.com/urmzd/humagen/pkg/runs"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestReviewedPullsScopeFiltersAndBounds(t *testing.T) {
	f := newFixture(t)
	answer := `{"format":"humagen.github.review.v1","summary":"Review https://github.com/acme/api/pull/42","findings":[]}`
	seed := func(id, tenant string, at time.Time) {
		t.Helper()
		if err := f.p.Runs.Record(id, wire.NewEvent(wire.Raise{ID: id, Tenant: tenant, Agent: "writer", Origin: runs.OriginAPI, At: at}), 1); err != nil {
			t.Fatal(err)
		}
		f.p.Runs.End(tenant, id, runs.Completed, answer, "", at)
	}
	seed("review", "acme", time.Now())
	seed("foreign", "other", time.Now())
	seed("old", "acme", time.Now().AddDate(0, 0, -31))
	tok := f.machine(t, "acme", "humagen:runs:read")
	res, out := f.do(t, nil, "GET", "/api/pull-requests", tok, "")
	if res.StatusCode != 200 {
		t.Fatalf("%d %v", res.StatusCode, out)
	}
	items := out["pulls"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["run"] != "review" {
		t.Fatalf("scope: %v", out)
	}
	for _, q := range []string{"?repo=other/repo", "?agent=other", "?q=missing", "?status=done"} {
		_, out = f.do(t, nil, "GET", "/api/pull-requests"+q, tok, "")
		if len(out["pulls"].([]any)) != 0 {
			t.Fatalf("filter %s: %v", q, out)
		}
	}
	for _, q := range []string{"?status=unknown", "?q=" + strings.Repeat("x", 201)} {
		res, _ = f.do(t, nil, "GET", "/api/pull-requests"+q, tok, "")
		if res.StatusCode != 400 {
			t.Fatal(res.StatusCode)
		}
	}
	res, _ = f.do(t, nil, "GET", "/api/pull-requests", f.machine(t, "acme", "humagen:agents:read"), "")
	if res.StatusCode != http.StatusForbidden {
		t.Fatal(res.StatusCode)
	}
}

func TestReviewedPullsRowCap(t *testing.T) {
	f := newFixture(t)
	for i := 0; i < 105; i++ {
		id := fmt.Sprintf("review-%03d", i)
		at := time.Now()
		if err := f.p.Runs.Record(id, wire.NewEvent(wire.Raise{ID: id, Tenant: "acme", Agent: "writer", Origin: runs.OriginAPI, At: at}), 1); err != nil {
			t.Fatal(err)
		}
		answer := fmt.Sprintf(`{"format":"humagen.github.review.v1","summary":"https://github.com/acme/api/pull/%d","findings":[]}`, i+1)
		f.p.Runs.End("acme", id, runs.Completed, answer, "", at)
	}
	_, out := f.do(t, nil, "GET", "/api/pull-requests", f.machine(t, "acme", "humagen:runs:read"), "")
	if n := len(out["pulls"].([]any)); n != 100 {
		t.Fatalf("row bound: %d", n)
	}
}

func TestReviewedPullsMetadataAndCI(t *testing.T) {
	fake := github.NewFake()
	f := newFixtureWith(t, platform.Config{GitHubAPI: func(string) github.API { return fake }, ClientInterval: -1})
	seatSourceClient(t, f, "acme", fake)
	ref := github.Ref{Owner: "acme", Repo: "api", Number: 42}
	var c github.Context
	c.Subject.Number = 42
	c.Subject.Title = "Fix leases"
	c.Subject.State = "closed"
	c.Subject.Head.SHA = sourceCommit
	c.Subject.PullRequest = &struct {
		URL string `json:"url"`
	}{URL: "https://api.github.com/repos/acme/api/pulls/42"}
	if err := fake.SeedContext(ref, c); err != nil {
		t.Fatal(err)
	}
	if err := fake.SeedWorkflow(ref, github.WorkflowRun{ID: 1, Name: "tests", HeadSHA: sourceCommit, Attempt: 1, Status: "completed", Conclusion: "failure"}); err != nil {
		t.Fatal(err)
	}
	at := time.Now()
	if err := f.p.Runs.Record("review", wire.NewEvent(wire.Raise{ID: "review", Tenant: "acme", Agent: "writer", Origin: runs.OriginAPI, At: at}), 1); err != nil {
		t.Fatal(err)
	}
	f.p.Runs.End("acme", "review", runs.Completed, `{"format":"humagen.github.review.v1","summary":"https://github.com/acme/api/pull/42","findings":[]}`, "", at)
	_, out := f.do(t, nil, "GET", "/api/pull-requests?status=done&q=leases", f.machine(t, "acme", "humagen:runs:read"), "")
	items := out["pulls"].([]any)
	if len(items) != 1 {
		t.Fatalf("metadata filter: %v", out)
	}
	item := items[0].(map[string]any)
	if item["title"] != "Fix leases" || item["ci"] != "failure" || item["status"] != "done" {
		t.Fatalf("metadata: %v", item)
	}
}
func TestReviewedPullCIUsesLatestAttemptAtHead(t *testing.T) {
	rows := []github.WorkflowRun{
		{ID: 1, Name: "tests", HeadSHA: sourceCommit, Status: "completed", Conclusion: "failure"},
		{ID: 2, Name: "tests", HeadSHA: sourceCommit, Status: "completed", Conclusion: "success"},
		{ID: 3, Name: "other", HeadSHA: "different", Status: "completed", Conclusion: "failure"},
	}
	if got := reviewedPullCI(rows, sourceCommit); got != "success" {
		t.Fatal(got)
	}
	if got := reviewedPullCI(rows, "absent"); got != "unknown" {
		t.Fatal(got)
	}
}
