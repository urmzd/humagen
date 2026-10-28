package platform_test

import (
	"testing"
	"time"

	"github.com/urmzd/humagen/fiber/pkg/runtime"
	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/operations"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
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
		got, err := p.BridgeDeliveryAllowed(t.Context(), host, c.ID, "C01", run.ID)
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
