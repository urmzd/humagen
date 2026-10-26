package platform_test

import (
	"context"
	"slices"
	"testing"

	"github.com/urmzd/fiber/pkg/agentspec"
	"github.com/urmzd/fiber/pkg/registry"
	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/projects"
)

func TestBuiltinNamesUseHumagen(t *testing.T) {
	want := []string{"humagen-writer", "humagen-analyst", "humagen-builder"}
	if !slices.Equal(platform.Builtins(), want) {
		t.Fatalf("builtins = %v, want %v", platform.Builtins(), want)
	}
	p, err := platform.New(platform.Config{Workspace: t.TempDir(), BuiltinProvider: "offline", BuiltinModel: "offline"})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.EnsureBuiltins(context.Background(), "acme"); err != nil {
		t.Fatal(err)
	}
	revs, _, err := p.AgentDirectory(context.Background(), "acme", registry.Page{Size: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(revs) != 3 {
		t.Fatalf("directory contains %d agents, want 3", len(revs))
	}
	for _, r := range revs {
		if !slices.Contains(want, r.Spec.Name) {
			t.Fatalf("directory contains %s", r.Spec.Name)
		}
	}
}

// The new directory must not invalidate a saved revision or the old handles
// used by schedules, project lists, client grants and historical runs.
func TestBuiltinCompatibilitySurvivesRestart(t *testing.T) {
	eachBackend(t, func(t *testing.T, first platform.Backends, again func() platform.Backends) {
		ctx := context.Background()
		original, err := first.Agents.Create(ctx, "acme", agentspec.AgentSpec{Name: "warp-analyst", Provider: "offline", Model: "offline", SystemPrompt: "Old instructions"}, "platform")
		if err != nil {
			t.Fatal(err)
		}
		p, err := platform.New(platform.Config{Backends: first, Workspace: t.TempDir(), BuiltinProvider: "offline", BuiltinModel: "offline"})
		if err != nil {
			t.Fatal(err)
		}
		if err := p.EnsureBuiltins(ctx, "acme"); err != nil {
			t.Fatal(err)
		}
		pinned, err := p.Agents.Get(ctx, "acme", "warp-analyst", original.Rev)
		if err != nil || pinned.Hash != original.Hash || pinned.Spec.SystemPrompt != original.Spec.SystemPrompt {
			t.Fatalf("saved revision changed: %+v, %v", pinned, err)
		}
		if err := p.Close(); err != nil {
			t.Fatal(err)
		}
		p, err = platform.New(platform.Config{Backends: again(), Workspace: t.TempDir(), BuiltinProvider: "offline", BuiltinModel: "offline"})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = p.Close() })
		if err := p.EnsureBuiltins(ctx, "acme"); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"humagen-analyst", "warp-analyst", "humagen-writer", "warp-writer", "humagen-builder", "warp-builder"} {
			rev, err := p.Agents.Get(ctx, "acme", name, 0)
			if err != nil || rev.Spec.Name != name || !platform.IsBuiltin(name) {
				t.Fatalf("compatibility handle %s: %+v, %v", name, rev, err)
			}
		}
		pinned, err = p.Agents.Get(ctx, "acme", "warp-analyst", original.Rev)
		if err != nil || pinned.Hash != original.Hash {
			t.Fatalf("pinned revision after restart: %+v, %v", pinned, err)
		}
		rows, _, err := p.AgentDirectory(ctx, "acme", registry.Page{Size: 100})
		if err != nil || len(rows) != 3 {
			t.Fatalf("directory after restart = %v, %v", rows, err)
		}
	})
}

func TestBuiltinRenameNeverOverwritesACustomAgent(t *testing.T) {
	ctx := context.Background()
	store := registry.NewMemory()
	old, err := store.Create(ctx, "acme", agentspec.AgentSpec{Name: "humagen-analyst", Provider: "offline", Model: "offline", SystemPrompt: "My custom work"}, "ada")
	if err != nil {
		t.Fatal(err)
	}
	p, err := platform.New(platform.Config{Backends: platform.Backends{Agents: store}, Workspace: t.TempDir(), BuiltinProvider: "offline", BuiltinModel: "offline"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	if err := p.EnsureBuiltins(ctx, "acme"); err == nil {
		t.Fatal("custom agent overwritten")
	}
	got, err := store.Get(ctx, "acme", old.Spec.Name, 0)
	if err != nil || got.Hash != old.Hash || got.Rev != old.Rev {
		t.Fatalf("custom agent changed: %+v, %v", got, err)
	}
}

func TestBuiltinCompatibilityKeepsAnOldProjectRoutingGrant(t *testing.T) {
	ctx := context.Background()
	p, err := platform.New(platform.Config{Workspace: t.TempDir(), BuiltinProvider: "offline", BuiltinModel: "offline"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	_, err = p.SetProjectAgents(ctx, "acme", "~", projects.Writer{By: "ada", Govern: true}, platform.AgentsInput{Enable: []string{"warp-analyst"}, Allow: []string{"warp-analyst"}})
	if err != nil {
		t.Fatal(err)
	}
	selected, err := p.Pick(ctx, "acme", "", "runs failures alerts")
	if err != nil || selected.Agent != "warp-analyst" {
		t.Fatalf("old routing grant = %+v, %v", selected, err)
	}
	if err := p.Available("acme", "", "humagen-analyst"); err == nil {
		t.Fatal("old allow-list granted the new handle")
	}
}
