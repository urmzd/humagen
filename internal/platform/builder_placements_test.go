package platform

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/personctx"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
	runsettings "github.com/urmzd/humagen/pkg/settings"
	"github.com/urmzd/humagen/pkg/threads"
	"github.com/urmzd/orchagent/flow/event"
	"github.com/urmzd/orchagent/flow/runtime"
	"github.com/urmzd/orchagent/flow/toolprovider"
	flowturn "github.com/urmzd/orchagent/flow/turn"
)

func placementFixture(t *testing.T) (*Platform, runtime.Request, wire.AgentSpec) {
	t.Helper()
	policy, err := auth.ParsePolicy("placement", `role reader { allow read on * } grant reader to subject "alice"`)
	if err != nil {
		t.Fatal(err)
	}
	p, err := New(Config{Workspace: t.TempDir(), Policy: policy})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	InstallPackagesForTest(t, p, "t")
	spec := wire.AgentSpec{Name: "writer", Provider: "offline", Model: "offline", SystemPrompt: "Write clearly", Tools: []string{personToolsGrant}}
	if _, err = p.Agents.Create(t.Context(), "t", spec, "test"); err != nil {
		t.Fatal(err)
	}
	th, err := p.Threads.Create("t", threads.Thread{ID: "th-0123456789abcdef", Channel: "general", Visibility: threads.Private, CreatedBy: "alice", Participants: []threads.Participant{{Kind: threads.Person, Name: "alice"}}}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	tag := wire.NewEvent(wire.Raise{ID: "builder-turn", Tenant: "t", Agent: BuiltinBuilder, Origin: "api", Actor: event.Actor{ID: "alice", User: "alice"}, At: time.Now(), Context: map[string]string{runs.MetaThread: th.ID}})
	if err = p.Runs.Record("builder-run", tag, 1); err != nil {
		t.Fatal(err)
	}
	return p, runtime.Request{Tenant: "t", RunID: "builder-run", Definition: flowturn.Pinned{Spec: flowturn.Spec{Name: BuiltinBuilder}}}, spec
}

func TestBuilderPreviewShowsCurrentPlacementWithoutCredentialsOrWrites(t *testing.T) {
	p, req, spec := placementFixture(t)
	c, err := p.Clients.Create("t", clients.Client{ID: "chat", Name: "Team chat", Surface: clients.Slack, Identity: clients.Identity{AppID: "A01", Team: "T01"}, Agents: []string{"writer"}, Enabled: true, Credentials: clients.Credentials{Token: clients.SecretRef{Set: true, Ref: "env:PRIVATE_TOKEN"}}, Slack: &clients.SlackOptions{ActionChannels: []string{"C09"}}}, clients.DefaultCaps(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		name      string
		direction projects.BridgeDirection
	}{{"incoming", projects.BridgeInbound}, {"outgoing", projects.BridgeOutcomes}} {
		channel := "C01"
		if item.name == "outgoing" {
			channel = "C02"
		}
		_, err = p.Projects.CreateChannel("t", projects.Channel{Name: item.name, Title: item.name + " title", Slack: &projects.SlackBinding{Client: c.ID, Channel: channel, Direction: item.direction}}, projects.Writer{By: "owner", Govern: true}, time.Now())
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err = p.Clients.Create("t", clients.Client{ID: "git", Name: "Repository reviews", Surface: clients.GitHub, Identity: clients.Identity{Owners: []string{"acme"}}, Agents: []string{"writer"}, GitHub: &clients.GitHubOptions{ActionRepositories: []string{"acme/write-only"}, Automations: []clients.GitHubAutomation{{Name: "review", Repository: "acme/repo", Event: "pull_request", Action: "opened", Agent: "writer", Instructions: "Review this"}}}}, clients.DefaultCaps(), time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err = p.Clients.Create("other", clients.Client{ID: "secret", Name: "Other tenant secret", Surface: clients.GitHub, Identity: clients.Identity{Owners: []string{"hidden"}}, Agents: []string{"writer"}}, clients.DefaultCaps(), time.Now()); err != nil {
		t.Fatal(err)
	}
	if err = p.SetRunSettings("t", runsettings.Layer{Ceilings: runsettings.Ceilings{ToolServers: []string{"allowed.example.com"}}}, nil); err != nil {
		t.Fatal(err)
	}
	tool, err := toolprovider.NewTool("search", "Search notes", json.RawMessage(`{"type":"object"}`))
	if err != nil {
		t.Fatal(err)
	}
	tool.Class = toolprovider.ClassRead
	if _, err = p.PersonContexts.Set("t", "alice", personctx.Input{ToolServers: []personctx.ToolServer{{Name: "notes", URL: "https://allowed.example.com/mcp", Credential: "env:PERSON_SECRET", Tools: []toolprovider.Tool{tool}}}}, time.Now()); err != nil {
		t.Fatal(err)
	}
	before, err := p.Clients.List("t")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := p.builderPreviewChangeTool(req).Execute(t.Context(), map[string]any{"name": spec.Name, "system_prompt": "Write more clearly", "tools": "person_tools"})
	if err != nil {
		t.Fatal(err)
	}
	var out builderPlacements
	if err = json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatal(err)
	}
	if out.Connections == nil || len(*out.Connections) != 3 || out.Listens == nil || len(*out.Listens) != 4 {
		t.Fatalf("missing placements: %s", raw)
	}
	for _, forbidden := range []string{"PRIVATE_TOKEN", "PERSON_SECRET", "Other tenant secret", "C09", "C01", "outgoing title", "write-only", "allowed.example.com"} {
		if strings.Contains(raw, forbidden) {
			t.Fatalf("private or write-only detail disclosed: %s", forbidden)
		}
	}
	if !slices.ContainsFunc(*out.Connections, func(c builderConnection) bool {
		return c.Name == "notes" && c.Scope == "personal" && c.Status == "configured"
	}) {
		t.Fatalf("missing personal grant %s", raw)
	}
	if !slices.ContainsFunc(*out.Listens, func(c builderListen) bool { return c.Place == "acme/repo: pull_request/opened" && !c.Enabled }) {
		t.Fatalf("disabled connection was enabled %+v", out)
	}
	after, err := p.Clients.List("t")
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("preview mutated connection configuration", err)
	}
}

func TestBuilderPlacementOmitsUnreadableAudienceAndDistinguishesKnownEmpty(t *testing.T) {
	p, req, spec := placementFixture(t)
	got, err := p.builderPlacementPreview(req, spec)
	if err != nil || got.Connections == nil || got.Listens == nil || len(*got.Connections) != 0 || len(*got.Listens) != 0 {
		t.Fatalf("known empty %+v %v", got, err)
	}
	unknown := req
	unknown.RunID = "missing"
	got, err = p.builderPlacementPreview(unknown, spec)
	if err != nil || got.Connections != nil || got.Listens != nil {
		t.Fatalf("unknown audience disclosed %+v %v", got, err)
	}
	th, err := p.Threads.Get("t", "th-0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.Threads.Participants("t", th.ID, th.Version, []threads.Participant{{Kind: threads.Person, Name: "bob"}}, nil, nil, "alice", time.Now()); err != nil {
		t.Fatal(err)
	}
	got, err = p.builderPlacementPreview(req, spec)
	if err != nil || got.Connections != nil || got.Listens != nil {
		t.Fatalf("broader audience disclosed %+v %v", got, err)
	}
}

func TestBuilderPlacementWithholdsHiddenHomeInsteadOfClaimingWorkspace(t *testing.T) {
	p, req, spec := placementFixture(t)
	if _, err := p.Projects.Create("t", projects.Project{Name: "Vault", Kind: projects.Account}, projects.Writer{By: "owner", Govern: true}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Clients.Create("t", clients.Client{ID: "private", Name: "Private app", Surface: clients.GitHub, Identity: clients.Identity{Owners: []string{"acme"}}, Agents: []string{"writer"}, Home: &clients.Home{Project: "Vault"}}, clients.DefaultCaps(), time.Now()); err != nil {
		t.Fatal(err)
	}
	got, err := p.builderPlacementPreview(req, spec)
	if err != nil || got.Connections == nil || len(*got.Connections) != 1 || got.Listens != nil {
		t.Fatalf("hidden home became workspace: %+v %v", got, err)
	}
	raw, _ := json.Marshal(got)
	if strings.Contains(string(raw), "Vault") || strings.Contains(string(raw), "listens") {
		t.Fatalf("hidden place disclosed: %s", raw)
	}
}
