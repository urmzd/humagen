package platform_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/fiber/pkg/envelope"
	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/memory"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
)

func TestInitiativeRollupUsesOnlyReadableApprovedUnexpiredTypedMemory(t *testing.T) {
	eachBackend(t, func(t *testing.T, backends platform.Backends, again func() platform.Backends) {
		p, err := platform.New(platform.Config{Backends: backends, Workspace: t.TempDir()})
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"Initiative", "Visible", "Hidden"} {
			members := []projects.Entry{{Who: operator.Subject, Role: projects.Member}, {Who: admin.Subject, Role: projects.Owner}}
			parent := ""
			if name != "Initiative" {
				parent = "Initiative"
			}
			if name == "Hidden" {
				members = []projects.Entry{{Who: admin.Subject, Role: projects.Owner}}
			}
			if _, err := p.CreateProject(t.Context(), tenant, govern, platform.ProjectInput{Name: name, Parent: parent, Members: members}); err != nil {
				t.Fatal(err)
			}
		}
		now := time.Now()
		for _, in := range []struct {
			project, text string
			kind          memory.Kind
			status        memory.Status
			expiry        time.Time
		}{
			{"Visible", "Approved decision", memory.Decision, memory.Approved, time.Time{}},
			{"Visible", "Approved risk", memory.Risk, memory.Approved, time.Time{}},
			{"Visible", "Pending risk", memory.Risk, memory.Pending, time.Time{}},
			{"Visible", "Expired decision", memory.Decision, memory.Approved, now.Add(-time.Hour)},
			{"Visible", "Fact with risk word", memory.Fact, memory.Approved, time.Time{}},
			{"Hidden", "Secret decision", memory.Decision, memory.Approved, time.Time{}},
		} {
			if _, err := p.Memory.AddDetailed(tenant, memory.ProjectOwner(in.project), in.text, memory.Person, in.status, admin.Subject, now.Add(-2*time.Hour), memory.Details{Kind: in.kind, ExpiresAt: in.expiry}); err != nil {
				t.Fatal(err)
			}
		}
		check := func(p *platform.Platform) {
			got, err := p.RollupProject(operator, "Initiative")
			if err != nil {
				t.Fatal(err)
			}
			if len(got.Projects) != 1 || got.Projects[0].Name != "Visible" || len(got.Notes) != 2 {
				t.Fatalf("rollup: %+v", got)
			}
			for _, n := range got.Notes {
				if strings.Contains(n.Text, "Secret") || strings.Contains(n.Text, "Pending") {
					t.Fatalf("leaked note: %+v", n)
				}
			}
			if _, err := p.RollupProject(operator, "Hidden"); !errors.Is(err, projects.ErrNotFound) {
				t.Fatalf("hidden:%v", err)
			}
		}
		check(p)
		if err := p.Close(); err != nil {
			t.Fatal(err)
		}
		reopened, err := platform.New(platform.Config{Backends: again(), Workspace: t.TempDir()})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := reopened.Close(); err != nil {
				t.Error(err)
			}
		})
		check(reopened)
	})
}

func TestInitiativeRollupCountsCurrentPlacementAndLeavesPrivateWorkOut(t *testing.T) {
	p := startWith(t, platform.Config{})
	field(t, p)
	for _, name := range []string{"Strategy", "Child"} {
		parent := ""
		if name == "Child" {
			parent = "Strategy"
		}
		if _, err := p.CreateProject(t.Context(), tenant, govern, platform.ProjectInput{Name: name, Parent: parent, Members: []projects.Entry{{Who: projects.Everyone, Role: projects.Member}, {Who: admin.Subject, Role: projects.Owner}}}); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	public := startThread(t, p, operator, platform.ThreadInput{Project: "Field", Text: "public work", Visibility: threads.Private, PostOnly: true})
	private := startThread(t, p, admin, platform.ThreadInput{Project: "Child", Text: "secret work", Visibility: threads.Private, PostOnly: true})
	for _, in := range []struct {
		id, thread, project string
		at                  time.Time
	}{{"r33333333333333333333333333333333", public.Thread.ID, "Field", now}, {"r44444444444444444444444444444444", private.Thread.ID, "Child", now}} {
		if err := p.Runs.Record(in.id, envelope.Tag{ID: in.id, Tenant: tenant, Agent: "helper", Origin: "api", Text: "work", At: in.at, Meta: map[string]string{runs.MetaProject: in.project, runs.MetaThread: in.thread}}, 1); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := p.Threads.UpdateMovingAs(tenant, public.Thread.ID, public.Thread.Version, operator.Subject, now, func(th *threads.Thread) error { th.Project = "Child"; return nil }); err != nil {
		t.Fatal(err)
	}
	got, err := p.RollupProject(operator, "Strategy")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Projects) != 1 || got.Projects[0].Runs30Days != 1 || got.Projects[0].Active != 1 || got.Projects[0].Conversations != 1 {
		t.Fatalf("moved or private work miscounted: %+v", got)
	}
	original, _ := p.Runs.Get(tenant, "r33333333333333333333333333333333")
	if original.Project != "Field" {
		t.Fatal("rollup rewrote accounting placement")
	}
}
