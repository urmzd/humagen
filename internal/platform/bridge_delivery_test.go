package platform_test

import (
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/operations"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
	"github.com/urmzd/saige/flow/runtime"
)

func TestBridgeDeliveryRechecksPolicyDirectionAndPrivateContext(t *testing.T) {
	p := start(t, call.Limits{})
	who := auth.Principal{Kind: auth.User, Tenant: tenant, Subject: "ada", Roles: []auth.Role{auth.Admin}}
	if _, err := p.CreateProject(t.Context(), tenant, govern, platform.ProjectInput{Name: "Bridge"}); err != nil {
		t.Fatal(err)
	}
	c, err := p.Clients.Create(tenant, clients.Client{ID: "bridge", Name: "Bridge", Surface: clients.Slack, Identity: clients.Identity{Team: "T01"}, Agents: []string{"lead"}, Enabled: true, Slack: &clients.SlackOptions{Machine: "bridge-host"}}, clients.DefaultCaps(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	bound, err := p.CreateChannel(t.Context(), tenant, "Bridge", govern, platform.ChannelInput{Name: "updates", Slack: &projects.SlackBinding{Client: c.ID, Channel: "C01", Direction: projects.BridgeOutcomes}})
	if err != nil {
		t.Fatal(err)
	}
	posted, err := p.StartThread(t.Context(), who, platform.ThreadInput{Project: "Bridge", Visibility: threads.Shared, Agents: []string{"lead"}, Text: "@lead Report"})
	if err != nil || len(posted.Runs) != 1 {
		t.Fatal(posted, err)
	}
	run := await(t, p, posted.Runs[0].ID, runs.Completed)
	host := auth.Principal{Kind: auth.Machine, Tenant: tenant, Subject: "bridge-host", Scopes: []auth.Permission{auth.RunsRead, auth.ProjectsRead, auth.ClientsRead}}
	check := func(want bool) {
		t.Helper()
		got, err := p.BridgeDeliveryAllowed(t.Context(), host, c.ID, "C01", "", run.ID)
		if err != nil || got != want {
			t.Fatalf("delivery=%v want%v err%v", got, want, err)
		}
	}
	check(true)
	policy, err := p.SetActionPolicy(who, operations.Scope{Kind: "workspace"}, map[operations.Category]operations.Level{operations.ExternalMessages: operations.Never}, 0)
	if err != nil {
		t.Fatal(err)
	}
	check(false)
	if _, err := p.SetActionPolicy(who, operations.Scope{Kind: "workspace"}, map[operations.Category]operations.Level{operations.ExternalMessages: operations.Allow}, policy.Version); err != nil {
		t.Fatal(err)
	}
	if _, err := p.UpdateChannel(t.Context(), tenant, "Bridge", "updates", govern, platform.ChannelUpdate{Version: bound.Version, Slack: &projects.SlackBinding{Client: c.ID, Channel: "C01", Direction: projects.BridgeInbound}}); err != nil {
		t.Fatal(err)
	}
	check(false)
	current, err := p.Projects.Channel(tenant, "Bridge", "updates")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.UpdateChannel(t.Context(), tenant, "Bridge", "updates", govern, platform.ChannelUpdate{Version: current.Version, Slack: &projects.SlackBinding{Client: c.ID, Channel: "C01", Direction: projects.BridgeTwoWay}}); err != nil {
		t.Fatal(err)
	}
	privateTag := run.Tag()
	privateTag.ID = "private-bridge-classification"
	privateID := runtime.RunID(tenant, privateTag.ID)
	if err := p.Runs.Record(privateID, privateTag, run.Rev); err != nil {
		t.Fatal(err)
	}
	run, err = p.Runs.Get(tenant, privateID)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.MarkPrivateContext(tenant, run.ID); err != nil {
		t.Fatal(err)
	}
	check(false)
}

// A session a Slack thread started answers in that Slack conversation,
// whatever kind it is, and nowhere else; a private console session reaches
// no Slack conversation at all.
func TestASlackThreadsOwnSessionIsDeliveredWhereItWasAsked(t *testing.T) {
	p, machine := slackRoom(t, nil)
	deliverable := func(t *testing.T, channel, ts, run string) bool {
		t.Helper()
		got, err := p.BridgeDeliveryAllowed(t.Context(), machine, "app", channel, ts, run)
		if err != nil {
			t.Fatalf("delivery of %s to %s: %v", run, channel, err)
		}
		return got
	}
	places := []struct {
		name, channel, typ string
		members            []string
		vis                threads.Visibility
	}{
		// What the Slack app sends today: no audience, so private to the asker.
		{"public channel, no audience reported", "C0PUBLIC", "", nil, threads.Private},
		{"public channel", "C0SHARED", platform.ChannelPublic, nil, threads.Shared},
		{"private channel", "C0PRIVATE", platform.ChannelPrivate, []string{"U0ANA", "U0VIC"}, threads.Private},
		{"legacy private channel", "G0PRIVATE", platform.ChannelPrivate, []string{"U0ANA"}, threads.Private},
		{"group direct message", "C0GROUP", platform.ChannelGroupDM, []string{"U0ANA", "U0VIC"}, threads.Private},
		{"legacy group direct message", "G0GROUP", platform.ChannelGroupDM, []string{"U0ANA", "U0VIC"}, threads.Private},
		{"direct message", "D0ANA", platform.ChannelDM, nil, threads.Private},
	}
	started := map[string]string{}
	const ts, otherTS = "1700000000.000100", "1700000000.000900"
	for _, pl := range places {
		t.Run(pl.name, func(t *testing.T) {
			in := slackSession(pl.channel, ts, "", "<@UAPP> howdy")
			if pl.typ != "" {
				in = withAudience(in, pl.typ, pl.members...)
			}
			v, _, err := p.StartSurfaceSession(t.Context(), machine, in)
			if err != nil {
				t.Fatal(err)
			}
			if v.Thread.Visibility != pl.vis {
				t.Fatalf("visibility = %s, want %s", v.Thread.Visibility, pl.vis)
			}
			run := await(t, p, sessionRun(t, v).ID, runs.Completed)
			started[pl.channel] = run.ID
			if !deliverable(t, pl.channel, ts, run.ID) {
				t.Fatalf("the answer of the session %s started is refused there", pl.channel)
			}
			if pl.vis == threads.Private && !strings.HasPrefix(pl.channel, "D") {
				if deliverable(t, pl.channel, otherTS, run.ID) || deliverable(t, pl.channel, "", run.ID) {
					t.Fatalf("the private session a thread of %s started is delivered to another thread there", pl.channel)
				}
			}
		})
	}
	// A private Slack session answers only in its own conversation.
	for _, from := range []string{"C0PUBLIC", "C0PRIVATE", "G0PRIVATE", "C0GROUP", "G0GROUP"} {
		if deliverable(t, "C0SHARED", ts, started[from]) {
			t.Fatalf("the private session %s started is delivered to another channel", from)
		}
	}
	// A private console session reaches no Slack channel.
	who := auth.Principal{Kind: auth.User, Tenant: tenant, Subject: "ana", Roles: []auth.Role{auth.Admin}}
	posted, err := p.StartThread(t.Context(), who, platform.ThreadInput{Project: "Initech", Visibility: threads.Private, Agents: []string{"lead"}, Text: "@lead keep this between us"})
	if err != nil || len(posted.Runs) != 1 {
		t.Fatal(posted, err)
	}
	console := await(t, p, posted.Runs[0].ID, runs.Completed)
	for _, channel := range []string{"C0PUBLIC", "C0SHARED", "C0PRIVATE", "G0GROUP"} {
		got, err := p.BridgeDeliveryAllowed(t.Context(), machine, "app", channel, ts, console.ID)
		if err == nil && got {
			t.Fatalf("a private console session's run is delivered to %s", channel)
		}
	}
}
