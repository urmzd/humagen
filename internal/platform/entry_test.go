package platform

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
)

func TestEntryUsesPackageAndProjectGrants(t *testing.T) {
	ctx := context.Background()
	p, err := New(Config{Workspace: t.TempDir(), EntryAgent: "codex", BuiltinProvider: "offline", BuiltinModel: "offline"})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if err := p.EnsureBuiltins(ctx, "acme"); err != nil {
		t.Fatal(err)
	}
	r, err := p.Catalog().Get(ctx, "acme", "codex", 0)
	if err != nil {
		t.Fatal(err)
	}
	if r.Spec.Package.Name != "humagen/core" || len(r.Spec.Access.Spawn) != 0 {
		t.Fatal(r)
	}
	_, err = p.CreateProject(ctx, "acme", projects.Writer{By: "ana", Govern: true}, ProjectInput{Name: "limited"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.Projects.Update("acme", "limited", 1, projects.Writer{By: "ana", Govern: true}, time.Now(), func(pr *projects.Project) error { pr.Agents.Allow = []string{"codex", BuiltinCodeReviewer}; return nil })
	if err != nil {
		t.Fatal(err)
	}
	grants, err := p.entrySpawn(ctx, "acme", "limited", r)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(grants, []string{BuiltinCodeReviewer}) {
		t.Fatalf("spawn: %v", grants)
	}
	ds, err := p.join(ctx, "acme", p.ProjectTree("acme"), "limited", "hello", nil)
	if err != nil || len(ds) != 1 || ds[0].Agent != "codex" {
		t.Fatalf("route: %+v %v", ds, err)
	}
	direct, err := p.resolveAgents(ctx, "acme", Place{Project: "limited"}, BuiltinCodeReviewer, "review", nil)
	if err != nil || direct[0].Agent != BuiltinCodeReviewer {
		t.Fatalf("direct: %+v %v", direct, err)
	}
	run, err := p.Invoke(ctx, wire.NewEvent(wire.Raise{ID: "entry-call", Tenant: "acme", Agent: "codex", Origin: runs.OriginAPI, Text: "review", Context: map[string]string{runs.MetaProject: "limited"}}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Call(ctx, "acme", run.ID, call.Request{Agent: BuiltinSecurityReviewer, Text: "review"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("unavailable call: %v", err)
	}
	r.CreatedBy = "ana"
	r.Spec.Access.Spawn = nil
	if grants, err := p.entrySpawn(ctx, "acme", "limited", r); err != nil || len(grants) != 0 {
		t.Fatalf("custom grant: %v %v", grants, err)
	}

}
