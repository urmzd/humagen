package platform

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/playscape/pkg/isolate/egress"
	saigetypes "github.com/urmzd/saige/agent/types"

	"github.com/urmzd/humagen/fiber/pkg/isolation"
	"github.com/urmzd/humagen/pkg/audit"
	"github.com/urmzd/humagen/pkg/personctx"
	"github.com/urmzd/humagen/pkg/runctx"
	runsettings "github.com/urmzd/humagen/pkg/settings"
)

// A run's instance is granted the hosts its pinned egress ceiling allows,
// and nothing when no level opened egress or the context cannot be read.
func TestARunsInstanceIsGrantedItsEgressCeiling(t *testing.T) {
	ws := runsettings.Layer{Ceilings: runsettings.Ceilings{Egress: []string{"api.github.com", "*.pypi.org"}}}
	project := runsettings.Layer{Ceilings: runsettings.Ceilings{Egress: []string{"files.pypi.org", "api.github.com"}}}
	req := requestWith(t, runctx.New("ada", runsettings.Resolve(ws, project), personctx.Context{}))
	if got := egressHosts(req); !slices.Equal(got, []string{"api.github.com", "files.pypi.org"}) {
		t.Fatalf("hosts = %q", got)
	}
	p := &Platform{}
	if got := p.runInstances(t.Context(), req).Request().Egress; !slices.Equal(got, []string{"api.github.com", "files.pypi.org"}) {
		t.Fatalf("the run's request = %q", got)
	}
	if got := egressHosts(requestWith(t, runctx.New("ada", runsettings.Resolve(runsettings.Layer{}), personctx.Context{}))); len(got) != 0 {
		t.Fatalf("no level opened egress, hosts = %q", got)
	}
	bad := requestWith(t, runctx.Context{})
	for k := range bad.Meta {
		bad.Meta[k] = "{"
	}
	if got := egressHosts(bad); got == nil || len(got) != 0 {
		t.Fatalf("an unreadable context = %q", got)
	}
}

// The shell tells the model which hosts it may reach only when the sandbox
// has a gate and the run has hosts; otherwise it says there is no network.
func TestTheShellSaysWhatItCanReach(t *testing.T) {
	ws := runsettings.Layer{Ceilings: runsettings.Ceilings{Egress: []string{"api.github.com"}}}
	req := requestWith(t, runctx.New("ada", runsettings.Resolve(ws), personctx.Context{}))
	describe := func(p *Platform) string { return p.shellTool(req, nil).(*saigetypes.ToolFunc).Def.Description }
	with := describe(&Platform{cfg: Config{Sandbox: isolation.Sandbox{Egress: true}}})
	if !strings.Contains(with, "api.github.com") || strings.Contains(with, "no network") {
		t.Fatalf("with a gate: %s", with)
	}
	without := describe(&Platform{})
	if !strings.Contains(without, "The sandbox has no network and is destroyed when the run ends; files") {
		t.Fatalf("without a gate: %s", without)
	}
}

// Every decision is audited for the run's tenant: allowed as succeeded,
// refused as failed with its reason. A decision with no tenant is not.
func TestEgressDecisionsAreAudited(t *testing.T) {
	store, err := audit.Open(nil)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC)
	p := &Platform{Audit: store, now: func() time.Time { return at }}
	req := isolation.Request{Tenant: "acme", Run: "r1"}
	if err := p.AuditEgress(req, egress.Decision{Grant: "i1", Method: "CONNECT", Host: "api.github.com", Port: 443, Allowed: true}); err != nil {
		t.Fatal(err)
	}
	if err := p.AuditEgress(req, egress.Decision{Grant: "i1", Method: "GET", Host: "evil.example", Port: 80, Reason: "no rule allows evil.example:80"}); err != nil {
		t.Fatal(err)
	}
	if err := p.AuditEgress(isolation.Request{}, egress.Decision{Host: "x", Port: 80}); err != nil {
		t.Fatal(err)
	}
	rows, err := store.List("acme", audit.Query{Resource: AuditEgressResource, From: at.Add(-time.Hour), To: at.Add(time.Hour), Limit: 10}, func(audit.Record) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %+v", rows)
	}
	byHost := map[string]audit.Record{}
	for _, r := range rows {
		byHost[r.After["host"]] = r
	}
	ok, denied := byHost["api.github.com"], byHost["evil.example"]
	if ok.Outcome != audit.Succeeded || ok.Action != AuditEgressAction || ok.Change.ID != "api.github.com:443" || ok.After["run"] != "r1" || ok.Actor != egressActor {
		t.Fatalf("allowed = %+v", ok)
	}
	if denied.Outcome != audit.Failed || denied.After["reason"] == "" || denied.After["allowed"] != "false" {
		t.Fatalf("denied = %+v", denied)
	}
}
