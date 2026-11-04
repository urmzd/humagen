package platform

import (
	"testing"

	"github.com/urmzd/humagen/internal/wire"
	githubflow "github.com/urmzd/humagen/pkg/integrations/github"
	"github.com/urmzd/orchagent/flow/connectors/whatsapp"
	"github.com/urmzd/orchagent/flow/runtime"
)

func TestConnectorToolsBelongOnlyToTheirInstallationTenant(t *testing.T) {
	peer, err := githubflow.New(githubflow.Config{API: githubflow.NewFake(), Owners: []string{"acme"}})
	if err != nil {
		t.Fatal(err)
	}
	p, err := New(Config{Workspace: t.TempDir(), Connections: func(wire.AgentStore) ([]Connection, error) { return []Connection{{Tenant: "acme", Peer: peer}}, nil }})
	if err != nil {
		t.Fatal(err)
	}
	for _, tenant := range []string{"acme", "other", ""} {
		catalog, err := p.connectorTools(runtime.Request{Tenant: tenant, Source: "github://acme/repo/issues/1", RunID: "run"}, nil)
		if err != nil {
			t.Fatal(err)
		}
		_, err = catalog.Lookup("github_read_context")
		if (err == nil) != (tenant == "acme") {
			t.Fatalf("tenant %q tool lookup: %v", tenant, err)
		}
	}
}

// A WhatsApp client's actions reach only the conversation its own run
// answers, so a turn it did not raise never gets them.
func TestAWhatsAppActionGoesOnlyToTheRunsItsClientRaised(t *testing.T) {
	peer, err := whatsapp.New(whatsapp.Config{API: whatsapp.NewFake()})
	if err != nil {
		t.Fatal(err)
	}
	p, err := New(Config{Workspace: t.TempDir(), Connections: func(wire.AgentStore) ([]Connection, error) { return []Connection{{Tenant: "acme", Peer: peer}}, nil }})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := p.connectorTools(runtime.Request{Tenant: "acme", Source: "slack://T1/C1", RunID: "run"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.Lookup("whatsapp_send_message"); err == nil {
		t.Fatal("a run another surface raised was given whatsapp_send_message")
	}
}
