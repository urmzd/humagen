package platform

import (
	"testing"

	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/threads"
)

// A direct message with a Slack client that names no home is filed in the
// person's own project, as a session they start in the console is. A
// channel's thread, and anything from a client with a home, stays where the
// client files it.
func TestADirectMessageIsFiledInThePersonsOwnProject(t *testing.T) {
	p, _, _ := slackPlatform(t)
	machine := slackReporting(t, p, "app", "svc-slack")
	if _, err := p.CreateClient(t.Context(), "acme", "test", ClientInput{
		Client: clients.Client{
			ID: "bare", Name: "bare", Surface: clients.Slack, Enabled: true, DefaultAgent: "lead",
			Identity: clients.Identity{AppID: "A0bare", Team: "T0BARE"},
			Slack:    &clients.SlackOptions{Sessions: true, Machine: "svc-bare", People: map[string]string{"U0ADA": "ada"}},
		},
		Token:         clients.SecretInput{Value: "token-bare"},
		SigningSecret: clients.SecretInput{Value: "secret-bare"},
	}); err != nil {
		t.Fatal(err)
	}
	bare := p.byPolicy("acme", "svc-bare")
	bare.Kind, bare.Scopes = auth.Machine, []auth.Permission{auth.RunsRead, auth.RunsWrite}
	own, err := p.PersonalProject(p.byPolicy("acme", "ada"))
	if err != nil {
		t.Fatal(err)
	}
	start := func(who auth.Principal, client, channel, ts string) threads.Thread {
		t.Helper()
		got, err := p.StartThread(t.Context(), who, ThreadInput{
			Text: "what is the status of my repos?", ActAs: &ActAs{Client: client, User: "U0ADA"},
			Surface: &SurfaceRef{Client: client, Channel: channel, TS: ts, AppUser: "UAPP"},
		})
		if err != nil {
			t.Fatalf("%s %s: %v", client, channel, err)
		}
		for _, r := range got.Runs {
			awaitEnd(t, p, r.ID)
		}
		return got.Thread
	}
	if th := start(bare, "bare", "D0ADA", "1700000000.000100"); th.Project != own.Name || th.Visibility != threads.Private {
		t.Fatalf("a direct message with a client that has no home is filed in %q (%s), want ada's own project %q", th.Project, th.Visibility, own.Name)
	}
	if th := start(bare, "bare", "C0TEAM", "1700000000.000200"); th.Project != "" {
		t.Fatalf("a channel's thread was filed in %q, want the client's home, the workspace", th.Project)
	}
	if th := start(machine, "app", "D0ADA", "1700000000.000300"); th.Project != "Field" {
		t.Fatalf("a direct message with a client that names a home is filed in %q, want its home Field", th.Project)
	}
}
