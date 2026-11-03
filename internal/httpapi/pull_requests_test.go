package httpapi

import (
	"fmt"
	"github.com/urmzd/humagen/internal/wire"
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
