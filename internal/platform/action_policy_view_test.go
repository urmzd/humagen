package platform

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/operations"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

func TestEffectivePolicyKeepsHiddenAncestorIdentityConfined(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		p, err := New(Config{Workspace: t.TempDir(), Backends: Backends{State: open(t)}})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = p.Close() }()
		writer := projects.Writer{By: "owner", Govern: true}
		at := time.Now()
		if _, err := p.Projects.Create("t", projects.Project{Name: "Secret", Title: "HIDDEN-POLICY-CANARY", Members: []projects.Entry{{Who: "owner", Role: projects.Owner}}}, writer, at); err != nil {
			t.Fatal(err)
		}
		if _, err := p.Projects.Create("t", projects.Project{Name: "SharedChild", Parent: "Secret", Members: []projects.Entry{{Who: "owner", Role: projects.Owner}, {Who: "reader", Role: projects.Viewer}}}, writer, at); err != nil {
			t.Fatal(err)
		}
		if _, err := p.Operations.SetPolicy("t", operations.Policy{Scope: operations.Scope{Kind: "project", Name: "Secret"}, Rules: map[operations.Category]operations.Level{operations.ExternalMessages: operations.Never}}, 0, "owner", at); err != nil {
			t.Fatal(err)
		}
		who := auth.Principal{Tenant: "t", Subject: "reader", Kind: auth.User, Roles: []auth.Role{auth.Admin}, AuditRead: func(_ string, project string, _ string) bool { return project == "SharedChild" }}
		if p.Seer(who).Project("Secret") || !p.Seer(who).Project("SharedChild") {
			t.Fatal("test does not isolate a hidden ancestor")
		}
		view := p.ActionPolicy(who, operations.Scope{Kind: "project", Name: "SharedChild"})
		raw, err := json.Marshal(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "Secret") || strings.Contains(string(raw), "HIDDEN-POLICY-CANARY") {
			t.Fatal("inherited policy revealed a hidden project")
		}
		rule := view.Effective[operations.ExternalMessages]
		if rule.Level != operations.Never || !rule.Locked || len(rule.Sources) != 1 || rule.Sources[0].Kind != "inherited" {
			t.Fatalf("hidden constraint lost its honest generic explanation: %+v", rule)
		}
	})
}
