package platform

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/urmzd/humagen/internal/wire"

	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/library"
	"github.com/urmzd/humagen/pkg/memory"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
)

func TestOutcomeSourceDependenciesBoundInputsAndVisibleRows(t *testing.T) {
	p := sourceGroundingPlatform(t)
	p.Threads = threads.NewStore()
	p.Memory = memory.NewStore()
	var err error
	p.SkillLibrary, err = library.Open(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Runs.Record("visible-origin", wire.NewEvent(wire.Raise{ID: "visible-origin", Tenant: "tenant", Agent: "worker", Context: map[string]string{runs.MetaProject: "Secret"}}), 1); err != nil {
		t.Fatal(err)
	}
	who := auth.Principal{Tenant: "tenant", Subject: "owner", Kind: auth.User, Scopes: []auth.Permission{auth.KnowledgeRead, auth.RunsRead, auth.ProjectsRead}}
	for _, scope := range []library.Scope{{Kind: "personal", Subject: "owner"}, {Kind: "project", Project: "Secret"}} {
		for i := range library.MaxSkills {
			definition := library.Definition{Skill: projects.Skill{Name: fmt.Sprintf("derived-%02d", i), Description: "Derived skill", Instructions: "Review the plan."}, Tools: []string{}}
			if _, err := p.SkillLibrary.Draft("tenant", "owner", scope, definition, 0, nil, p.now(), "visible-origin"); err != nil {
				t.Fatal(err)
			}
		}
	}
	for i := range memory.MaxNotes {
		if _, err := p.Memory.RememberDecision("tenant", memory.PersonalOwner("owner"), fmt.Sprintf("Reviewed decision %02d", i), "owner", p.now(), "visible-origin"); err != nil {
			t.Fatal(err)
		}
	}
	reader := outcomeSourceDependencies{p}
	got, err := reader.ReadSourceDependencies(t.Context(), who, []string{"visible-origin"})
	if err != nil || len(got.Skills) != 50 || len(got.Memory) != 50 {
		t.Fatal("unbounded or missing visible rows", len(got.Skills), len(got.Memory), err)
	}
	ids := make([]string, 201)
	ids[200] = "visible-origin"
	got, err = reader.ReadSourceDependencies(t.Context(), who, ids)
	if err != nil || len(got.Skills) != 0 || len(got.Memory) != 0 {
		t.Fatal("read beyond 200 supplied origins", got, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := reader.ReadSourceDependencies(ctx, who, []string{"visible-origin"}); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation ignored", err)
	}
	who.Kind = auth.Machine
	got, err = reader.ReadSourceDependencies(t.Context(), who, []string{"visible-origin"})
	if err != nil || len(got.Skills) != 0 || len(got.Memory) != 0 {
		t.Fatal("machine acquired learning lineage", got, err)
	}
}

func TestOutcomeSourceDependencyDefaultPreservesExplicitReader(t *testing.T) {
	called := false
	reader := dependencyReaderFunc(func(context.Context, auth.Principal, []string) (SourceExtraDependencies, error) {
		called = true
		return SourceExtraDependencies{Supported: []string{"skills", "memory"}}, nil
	})
	p, err := New(Config{Workspace: t.TempDir(), SourceDependencies: reader})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	out := SourceDependencies{Unsupported: []string{"skills", "memory"}}
	if err := p.extraSourceDependencies(t.Context(), auth.Principal{}, nil, &out); err != nil || !called || len(out.Unsupported) != 0 {
		t.Fatal("configured dependency reader replaced", called, out, err)
	}
	if err := p.RegisterSourceDependencyReader(reader); err == nil {
		t.Fatal("configured reader replaced by registration")
	}
}
