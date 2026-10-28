package httpapi

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/platform"
	slackflow "github.com/urmzd/humagen/pkg/integrations/slack"
)

func TestBreakGlassReadsAreRecordedAndAdminOnly(t *testing.T) {
	f := newFixture(t)
	op := f.machineAs(t, "owner", "acme", operator...)
	gov := f.machineAs(t, "governor", "acme", everything...)
	want(t, f, op, "POST", "/api/projects", `{"name":"Secret"}`, 201)
	want(t, f, op, "PUT", "/api/projects/Secret/members", `{"version":1,"members":[{"who":"owner","role":"owner"}]}`, 200)
	before := time.Now().Add(-time.Second)
	want(t, f, gov, "GET", "/api/projects/Secret", "", 200)
	rows := f.p.ReadAudit.List("acme", before, time.Now().Add(time.Second), 500)
	found := false
	for _, r := range rows {
		if r.Subject == "governor" && r.Project == "Secret" && r.Route == "GET /api/projects/{name}" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing route audit: %#v", rows)
	}
	n := len(rows)
	want(t, f, op, "GET", "/api/projects/Secret", "", 200)
	if got := len(f.p.ReadAudit.List("acme", before, time.Now().Add(time.Second), 500)); got != n {
		t.Fatalf("ordinary read audited: %d -> %d", n, got)
	}
	want(t, f, gov, "GET", "/api/read-audit", "", 403)
	admin := f.signIn(t, "humagen-admins")
	res, out := f.do(t, admin, http.MethodGet, "/api/read-audit", "", "")
	if res.StatusCode != 200 || len(out["records"].([]any)) == 0 {
		t.Fatalf("admin audit: %d %#v", res.StatusCode, out)
	}
	for _, q := range []string{"?limit=501", "?limit=0", "?from=bad", "?from=2026-01-01T00:00:00Z&to=2026-03-01T00:00:00Z"} {
		res, _ := f.do(t, admin, "GET", "/api/read-audit"+q, "", "")
		if res.StatusCode != 400 {
			t.Fatalf("%s = %d", q, res.StatusCode)
		}
	}
}

func TestDelegatedReadsKeepTheEffectiveReaderAndServerRoute(t *testing.T) {
	f := newFixtureGranting(t, platform.Config{SlackAPI: func(string) slackflow.API { return slackflow.NewFake() }, ClientInterval: -1}, `grant admin to subject "reviewer"`)
	app := f.machine(t, "acme", everything...)
	want(t, f, app, "POST", "/api/agents", helperSpec, 201)
	want(t, f, app, "POST", "/api/projects", `{"name":"Secret"}`, 201)
	want(t, f, app, "PUT", "/api/projects/Secret/members", `{"version":1,"members":[{"who":"owner","role":"owner"}]}`, 200)
	client := strings.Replace(slackApp("app", "A01APP", "svc-acme"), `"machine":"svc-acme"`, `"machine":"svc-acme","people":{"U0REVIEWER":"reviewer"}`, 1)
	want(t, f, app, "POST", "/api/clients", client, 201)
	start := time.Now().Add(-time.Second)
	want(t, f, app, "GET", "/api/projects?act_as_client=app&act_as_user=U0REVIEWER&q=Secret", "", 200)
	rows := f.p.ReadAudit.List("acme", start, time.Now().Add(time.Second), 500)
	found := false
	for _, row := range rows {
		if row.Project == "Secret" && row.Subject == "reviewer" && row.Route == "GET /api/projects" {
			found = true
		}
	}
	if !found {
		t.Fatalf("delegated read lost effective subject or route: %#v", rows)
	}
}
