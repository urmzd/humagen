package platform_test

import (
	"context"
	"slices"
	"testing"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/projects"
)

func TestBuiltinNamesAndReviewGrants(t *testing.T) {
	want := []string{"humagen-writer", "humagen-analyst", "humagen-builder", "code-reviewer", "security-reviewer", "architect-reviewer", "qa-reviewer"}
	if !slices.Equal(platform.Builtins(), want) {
		t.Fatalf("builtins = %v, want %v", platform.Builtins(), want)
	}
	p, err := platform.New(platform.Config{Workspace: t.TempDir(), TestBuiltinProvider: "offline", TestBuiltinModel: "offline"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	if err := p.EnsureBuiltins(context.Background(), "acme"); err != nil {
		t.Fatal(err)
	}
	revs, _, err := p.AgentDirectory(context.Background(), "acme", wire.Page{Size: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(revs) != len(want) {
		t.Fatalf("directory contains %d agents, want %d", len(revs), len(want))
	}
	for _, r := range revs {
		if !slices.Contains(want, r.Spec.Name) {
			t.Fatalf("directory contains %s", r.Spec.Name)
		}
		if !platform.IsBuiltin(r.Spec.Name) || r.CreatedBy != "platform" {
			t.Fatalf("agent is not platform-owned: %+v", r)
		}
		if err := r.Spec.Validate(); err != nil {
			t.Fatalf("invalid builtin %s: %v", r.Spec.Name, err)
		}
		if r.Spec.Name == platform.BuiltinCodeReviewer {
			got := slices.Clone(r.Spec.Access.Spawn)
			wantSpawn := []string{"security-reviewer", "architect-reviewer", "qa-reviewer"}
			slices.Sort(got)
			slices.Sort(wantSpawn)
			if !slices.Equal(got, wantSpawn) {
				t.Fatalf("code-reviewer spawn = %v, want %v", got, wantSpawn)
			}
		} else if len(r.Spec.Access.Spawn) != 0 {
			t.Fatalf("%s may spawn %v", r.Spec.Name, r.Spec.Access.Spawn)
		}
		if slices.Contains(want[3:], r.Spec.Name) {
			tools := []string{platform.ToolFileRead, platform.ToolFileList, "github_read_context", "github_read_file"}
			slices.Sort(tools)
			got := slices.Clone(r.Spec.Tools)
			slices.Sort(got)
			if !slices.Equal(got, tools) {
				t.Fatalf("%s tools = %v, want read-only %v", r.Spec.Name, got, tools)
			}
			if r.Spec.Name != platform.BuiltinCodeReviewer {
				pol, err := p.CallPolicy(context.Background(), "acme", r.Spec.Name)
				if err != nil || !pol.Admits(platform.BuiltinCodeReviewer) || pol.NeedsApproval(platform.BuiltinCodeReviewer) {
					t.Fatalf("specialist policy = %+v, %v", pol, err)
				}
			}
		}
	}
}

// Platform-owned review grants are restored, while unchanged specs stay pinned.
func TestBuiltinReviewGrantsFollowThePlatform(t *testing.T) {
	ctx := context.Background()
	p, err := platform.New(platform.Config{Workspace: t.TempDir(), TestBuiltinProvider: "offline", TestBuiltinModel: "offline"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	if err := p.EnsureBuiltins(ctx, "acme"); err != nil {
		t.Fatal(err)
	}
	for _, name := range platform.Builtins()[3:] {
		r, err := p.Agents.Get(ctx, "acme", name, 0)
		if err != nil {
			t.Fatal(err)
		}
		if name == platform.BuiltinCodeReviewer {
			r.Spec.Access.Spawn = nil
		} else {
			r.Spec.Access.Spawn = []string{platform.BuiltinCodeReviewer}
		}
		r.Spec.Access.WorkspaceAreas = []string{"unexpected"}
		if _, err := p.Agents.Revise(ctx, "acme", r.Spec, r.Rev, "platform"); err != nil {
			t.Fatal(err)
		}
	}
	for range 2 {
		if err := p.EnsureBuiltins(ctx, "acme"); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range platform.Builtins()[3:] {
		r, err := p.Agents.Get(ctx, "acme", name, 0)
		if err != nil {
			t.Fatal(err)
		}
		if r.Rev != 3 || len(r.Spec.Access.WorkspaceAreas) != 0 {
			t.Fatalf("reviewer grants were not restored idempotently: %+v", r)
		}
		want := []string(nil)
		if name == platform.BuiltinCodeReviewer {
			want = []string{platform.BuiltinSecurityReviewer, platform.BuiltinArchitectReviewer, platform.BuiltinQAReviewer}
		}
		slices.Sort(want)
		if !slices.Equal(r.Spec.Access.Spawn, want) {
			t.Fatalf("%s spawn = %v, want %v", name, r.Spec.Access.Spawn, want)
		}
	}
}

// The new directory must not invalidate a saved revision or the old handles
// used by schedules, project lists, client grants and historical runs.
func TestBuiltinCompatibilitySurvivesRestart(t *testing.T) {
	eachBackend(t, func(t *testing.T, first platform.Backends, again func() platform.Backends) {
		ctx := context.Background()
		original, err := first.Agents.Create(ctx, "acme", wire.AgentSpec{Name: "warp-analyst", Provider: "offline", Model: "offline", SystemPrompt: "Old instructions"}, "platform")
		if err != nil {
			t.Fatal(err)
		}
		p, err := platform.New(platform.Config{Backends: first, Workspace: t.TempDir(), TestBuiltinProvider: "offline", TestBuiltinModel: "offline"})
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
		p, err = platform.New(platform.Config{Backends: again(), Workspace: t.TempDir(), TestBuiltinProvider: "offline", TestBuiltinModel: "offline"})
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
		rows, _, err := p.AgentDirectory(ctx, "acme", wire.Page{Size: 100})
		if err != nil || len(rows) != len(platform.Builtins()) {
			t.Fatalf("directory after restart = %v, %v", rows, err)
		}
	})
}

func TestBuiltinRenameNeverOverwritesACustomAgent(t *testing.T) {
	ctx := context.Background()
	store := wire.NewMemoryAgents()
	old, err := store.Create(ctx, "acme", wire.AgentSpec{Name: "humagen-analyst", Provider: "offline", Model: "offline", SystemPrompt: "My custom work"}, "ada")
	if err != nil {
		t.Fatal(err)
	}
	p, err := platform.New(platform.Config{Backends: platform.Backends{Agents: store}, Workspace: t.TempDir(), TestBuiltinProvider: "offline", TestBuiltinModel: "offline"})
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
	p, err := platform.New(platform.Config{Workspace: t.TempDir(), TestBuiltinProvider: "offline", TestBuiltinModel: "offline"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	_, err = p.SetProjectAgents(ctx, "acme", "~", projects.Writer{By: "ada", Govern: true}, platform.AgentsInput{Enable: []string{"warp-analyst"}, Allow: []string{"warp-analyst"}})
	if err != nil {
		t.Fatal(err)
	}
	ds, err := p.Route(ctx, "acme", "", "runs failures alerts")
	if err != nil || len(ds) != 1 || ds[0].Agent != "warp-analyst" {
		t.Fatalf("old routing grant = %+v, %v", ds, err)
	}
	if err := p.Available("acme", "", "humagen-analyst"); err == nil {
		t.Fatal("old allow-list granted the new handle")
	}
}
