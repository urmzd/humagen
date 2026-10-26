package platform_test

import (
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/audit"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

func TestClientCapabilityAuditRequiresClientReadBeforeLimit(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		p := startWith(t, platform.Config{Backends: platform.Backends{State: open(t)}})
		now := time.Now()
		if err := p.Audit.Record(tenant, "admin", audit.Change{Action: "client.capabilities", Resource: "client", ID: "private-installation", Scope: "workspace"}, now); err != nil {
			t.Fatal(err)
		}
		who := auth.Principal{Kind: auth.Machine, Tenant: tenant, Subject: "auditor", Scopes: []auth.Permission{auth.ProjectsRead}}
		query := audit.Query{Resource: "client", From: now.Add(-time.Hour), To: now.Add(time.Hour), Limit: 1}
		rows, err := p.AuditRecords(who, query)
		if err != nil || len(rows) != 0 {
			t.Fatal("client installation revealed without client read", rows, err)
		}
		who.Scopes = append(who.Scopes, auth.ClientsRead)
		rows, err = p.AuditRecords(who, query)
		if err != nil || len(rows) != 1 || rows[0].Change.ID != "private-installation" {
			t.Fatal("authorized audit missing", rows, err)
		}
	})
}
