package platform

import (
	"errors"
	"strings"
	"testing"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/projects"
)

func TestTheEntryAgentIsToldWhoMayFitAndIsHeldToItsPlace(t *testing.T) {
	ctx := t.Context()
	p, err := New(Config{Workspace: t.TempDir(), BuiltinProvider: "offline", BuiltinModel: "offline"})
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
	if err := p.entryMayCall("acme", entry, "", "ops"); err != nil {
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
	if err := p.entryMayCall("acme", entry, "Field", "ops"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("an agent not available in Field = %v, want ErrForbidden", err)
	}
	if err := p.entryMayCall("acme", entry, "Field", "analyst"); err != nil {
		t.Fatalf("an agent available in Field was refused: %v", err)
	}
	ops, _ := p.Catalog().Get(ctx, "acme", "ops", 0)
	if err := p.entryMayCall("acme", ops, "Field", "helper"); err != nil {
		t.Fatalf("another caller was held to the entry agent's rule: %v", err)
	}
}
