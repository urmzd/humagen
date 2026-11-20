package platform

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
)

func TestTheEntryAgentIsToldWhoMayFitAndIsHeldToItsPlace(t *testing.T) {
	ctx := t.Context()
	p, err := New(Config{Workspace: t.TempDir(), TestBuiltinProvider: "offline", TestBuiltinModel: "offline"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	for _, spec := range []wire.AgentSpec{
		{Name: "ops", Provider: "offline", Model: "offline", SystemPrompt: "You run deployments. Explain why a deploy failed."},
		{Name: "analyst", Provider: "offline", Model: "offline", SystemPrompt: "You answer questions about cost."},
		{Name: "hidden", Provider: "offline", Model: "offline", SystemPrompt: "You also explain deploy failures."},
	} {
		if _, err := p.Agents.Create(ctx, "acme", spec, "test"); err != nil {
			t.Fatal(err)
		}
	}
	if err := p.EnsureBuiltins(ctx, "acme"); err != nil {
		t.Fatal(err)
	}
	hints := p.entryHints(ctx, "acme", "", "why did the deploy fail", func(a string) bool { return a != "hidden" })
	for _, want := range []string{"@ops (matched deploy", "You run deployments.", "Other agents you may call:", "@analyst"} {
		if !strings.Contains(hints, want) {
			t.Fatalf("hints lack %q:\n%s", want, hints)
		}
	}
	if strings.Contains(hints, "hidden") || strings.Contains(hints, "@"+BuiltinEntry+" ") {
		t.Fatalf("hints name an agent the poster may not read, or the entry agent itself:\n%s", hints)
	}
	if got := p.entryHintsFor(ctx, "acme", "", "ops", "why did the deploy fail", nil); got != "" {
		t.Fatalf("another agent was given the entry agent's hints: %q", got)
	}

	entry, err := p.Catalog().Get(ctx, "acme", BuiltinEntry, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.entryMayCall("acme", entry, runs.Run{}, "ops"); err != nil {
		t.Fatalf("an agent available at the root was refused: %v", err)
	}
	w := projects.Writer{By: "ada", Govern: true}
	if _, err := p.CreateProject(ctx, "acme", w, ProjectInput{Name: "Field"}); err != nil {
		t.Fatal(err)
	}
	cur, err := p.ProjectAgentsOf("acme", "Field")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.SetProjectAgents(ctx, "acme", "Field", w, AgentsInput{Version: cur.Version, Enable: []string{}, Allow: []string{"analyst"}}); err != nil {
		t.Fatal(err)
	}
	if err := p.entryMayCall("acme", entry, runs.Run{Project: "Field"}, "ops"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("an agent not available in Field = %v, want ErrForbidden", err)
	}
	if err := p.entryMayCall("acme", entry, runs.Run{Project: "Field"}, "analyst"); err != nil {
		t.Fatalf("an agent available in Field was refused: %v", err)
	}
	// A run answering a message a client sent calls only the agents that
	// client may route to.
	if _, err := p.Clients.Create("acme", clients.Client{ID: "desk", Name: "Desk", Surface: clients.Slack, Identity: clients.Identity{AppID: "A01", Team: "T01"}, Enabled: true, Agents: []string{"analyst"}}, clients.DefaultCaps(), time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := p.entryMayCall("acme", entry, runs.Run{Client: "desk"}, "ops"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("an agent the client may not route to = %v, want ErrForbidden", err)
	}
	if err := p.entryMayCall("acme", entry, runs.Run{Client: "desk"}, "analyst"); err != nil {
		t.Fatalf("an agent the client routes to was refused: %v", err)
	}
	if err := p.entryMayCall("acme", entry, runs.Run{Client: "gone"}, "analyst"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("a call for a client that is not there = %v, want ErrForbidden", err)
	}
	ops, _ := p.Catalog().Get(ctx, "acme", "ops", 0)
	if err := p.entryMayCall("acme", ops, runs.Run{Project: "Field"}, "helper"); err != nil {
		t.Fatalf("another caller was held to the entry agent's rule: %v", err)
	}
}
