package platform

import (
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/alerts"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
	"github.com/urmzd/orchagent/flow/connector"
	transport "github.com/urmzd/orchagent/flow/connectors/github"
	"github.com/urmzd/orchagent/flow/event"
	"github.com/urmzd/orchagent/flow/session"
)

func TestFixPullContextUsesPinnedAlertAndCurrentSessionAccess(t *testing.T) {
	p, err := New(Config{Workspace: t.TempDir(), PublicURL: "https://console.example.test/team"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	c, err := p.Clients.Create("t", clients.Client{ID: "gh", Name: "GitHub", Surface: clients.GitHub, Enabled: true, Identity: clients.Identity{Owners: []string{"acme"}}, Agents: []string{"a"}, GitHub: &clients.GitHubOptions{FixPullRequests: true}}, clients.DefaultCaps(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	create := func(parent, alert string) threads.Thread {
		t.Helper()
		s, err := p.Sessions.Open(t.Context(), "t", session.Open{Parent: parent, Agent: "a"})
		if err != nil {
			t.Fatal(err)
		}
		th, err := p.Threads.Create("t", threads.Thread{ID: s.ID, Channel: "general", Visibility: threads.Private, CreatedBy: "owner", Participants: []threads.Participant{{Kind: threads.Person, Name: "person"}}, Default: "a", Agents: []string{"a"}, Session: &threads.SessionIdentity{Agent: "a", Parent: parent, SourceAlert: alert}}, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		return th
	}
	source := create("console://source", "")
	a, err := p.Alerts.CreateSession("t", alerts.Alert{Session: source.ID, Title: "Review needs a fix", Priority: alerts.Normal, At: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	child := create(source.ID, a.ID)
	record := func(id, thread, alert string) {
		t.Helper()
		tag := wire.NewEvent(wire.Raise{ID: id, Tenant: "t", Origin: "api", Agent: "a", Actor: event.Actor{ID: "person"}, At: time.Now(), Context: map[string]string{runs.MetaThread: thread, wire.MetaSourceAlert: alert}})
		if err := p.Runs.Record(id, tag, 1); err != nil {
			t.Fatal(err)
		}
	}
	record("valid", child.ID, a.ID)
	link, err := p.fixPullContext(c)(connector.WithRunID(t.Context(), "valid"), transport.Ref{})
	if err != nil || !strings.Contains(link.SessionURL, "/team/sessions/"+child.ID) || link.AlertURL != "https://console.example.test/team/alerts?alert="+a.ID {
		t.Fatal(link, err)
	}
	unrelated := create("console://unrelated", "")
	record("forged", unrelated.ID, a.ID)
	if _, err := p.fixPullContext(c)(connector.WithRunID(t.Context(), "forged"), transport.Ref{}); err == nil {
		t.Fatal("unrelated alert pin accepted")
	}
	other := c
	other.Tenant = "elsewhere"
	if _, err := p.fixPullContext(other)(connector.WithRunID(t.Context(), "valid"), transport.Ref{}); err == nil {
		t.Fatal("cross tenant context accepted")
	}
	source, err = p.Threads.Get("t", source.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.Threads.Participants("t", source.ID, source.Version, nil, []string{"person"}, nil, "owner", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := p.fixPullContext(c)(connector.WithRunID(t.Context(), "valid"), transport.Ref{}); err == nil {
		t.Fatal("revoked source context accepted")
	}
}
