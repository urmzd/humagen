package platform_test

import (
	"sync"
	"testing"
	"time"

	"github.com/urmzd/humagen/fiber/pkg/runtime"
	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/operations"
)

type sourceVersions struct {
	mu      sync.Mutex
	version uint64
	fresh   bool
}

func (s *sourceVersions) SourceChangeVersion(_, name string) (uint64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.version, s.fresh && name == "feed"
}
func (s *sourceVersions) set(version uint64, fresh bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.version, s.fresh = version, fresh
}
func sourceAwait(t *testing.T, read func() bool) {
	t.Helper()
	until := time.Now().Add(15 * time.Second)
	for !read() {
		if time.Now().After(until) {
			t.Fatal("source-change assignment did not reach its recorded state")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
func TestSourceChangeAssignmentsUseExistingRuntimeAndKeepCursorAfterRestart(t *testing.T) {
	eachBackend(t, func(t *testing.T, b platform.Backends, again func() platform.Backends) {
		versions := &sourceVersions{version: 1, fresh: true}
		p, stop := runningWith(t, platform.Config{Backends: b, AssignmentSources: versions})
		if _, err := p.Knowledge.Create(tenant, "feed", "Feed", "owner", time.Now()); err != nil {
			t.Fatal(err)
		}
		if _, err := p.Knowledge.Add(tenant, "feed", "Initial", "", "retained initial source", "owner", time.Now()); err != nil {
			t.Fatal(err)
		}
		if _, err := p.Agents.Create(t.Context(), tenant, wire.AgentSpec{Name: "watcher", Model: "offline", Provider: "offline", SystemPrompt: "Review retained changes.", Sources: []wire.Source{{Name: "feed", URI: "knowledge://feed"}}}, "owner"); err != nil {
			t.Fatal(err)
		}
		who := auth.Principal{Tenant: tenant, Subject: "owner", Kind: auth.User, Roles: []auth.Role{auth.Admin}}
		a, err := p.SetAssignment(t.Context(), who, operations.Assignment{ID: "watch-feed", Agent: "watcher", Owner: "owner", Goal: "Review feed changes", DoneWhen: "Report saved", State: operations.Requested, Trigger: operations.SourceTrigger, Source: "feed"}, 0)
		if err != nil {
			t.Fatal(err)
		}
		a.State = operations.Watching
		a, err = p.SetAssignment(t.Context(), who, a, a.Version)
		if err != nil {
			t.Fatal(err)
		}
		if a.SourceVersion != 1 {
			t.Fatal("creation did not record baseline")
		}
		versions.set(2, true)
		sourceAwait(t, func() bool {
			got, _, _ := p.Operations.Assignment(tenant, a.ID)
			return got.SourceVersion == 2 && got.State == operations.Watching && got.RunID != ""
		})
		first, _, _ := p.Operations.Assignment(tenant, a.ID)
		run, err := p.Runs.Get(tenant, first.RunID)
		if err != nil || run.Origin != "assignment" || !run.Status.Terminal() {
			t.Fatalf("not an actual retained-source run: %+v, %v", run, err)
		}
		stop()
		versions.set(3, true)
		p2, stop2 := runningWith(t, platform.Config{Backends: again(), AssignmentSources: versions})
		defer stop2()
		sourceAwait(t, func() bool {
			got, _, _ := p2.Operations.Assignment(tenant, a.ID)
			return got.SourceVersion == 3 && got.State == operations.Watching && got.RunID != first.RunID
		})
		versions.set(3, false)
		sourceAwait(t, func() bool {
			got, _, _ := p2.Operations.Assignment(tenant, a.ID)
			return got.State == operations.Blocked
		})
		versions.set(4, true)
		// A fresh source never resumes paused work without its owner's choice.
		time.Sleep(2200 * time.Millisecond)
		got, _, _ := p2.Operations.Assignment(tenant, a.ID)
		if got.State != operations.Blocked || got.SourceVersion != 3 {
			t.Fatalf("work resumed without permission: %+v", got)
		}
		if runs := p2.Runs.List(tenant, 10); len(runs) != 2 {
			t.Fatalf("unchanged source made duplicate runs: %d", len(runs))
		}
	})
}

func TestSourceChangeReservationRecoversExactTriggerAfterRestart(t *testing.T) {
	eachBackend(t, func(t *testing.T, b platform.Backends, again func() platform.Backends) {
		versions := &sourceVersions{version: 1, fresh: true}
		p, stop := runningWith(t, platform.Config{Backends: b, AssignmentSources: versions})
		if _, err := p.Knowledge.Create(tenant, "feed", "Feed", "owner", time.Now()); err != nil {
			t.Fatal(err)
		}
		if _, err := p.Knowledge.Add(tenant, "feed", "Initial", "", "retained initial source", "owner", time.Now()); err != nil {
			t.Fatal(err)
		}
		if _, err := p.Agents.Create(t.Context(), tenant, wire.AgentSpec{Name: "watcher", Model: "offline", Provider: "offline", SystemPrompt: "Review retained changes.", Sources: []wire.Source{{Name: "feed", URI: "knowledge://feed"}}}, "owner"); err != nil {
			t.Fatal(err)
		}
		who := auth.Principal{Tenant: tenant, Subject: "owner", Kind: auth.User, Roles: []auth.Role{auth.Admin}}
		a, err := p.SetAssignment(t.Context(), who, operations.Assignment{ID: "recover-feed", Agent: "watcher", Owner: "owner", Goal: "Review retained changes", DoneWhen: "Report saved", State: operations.Requested, Trigger: operations.SourceTrigger, Source: "feed"}, 0)
		if err != nil {
			t.Fatal(err)
		}
		a.State = operations.Watching
		a, err = p.SetAssignment(t.Context(), who, a, a.Version)
		if err != nil {
			t.Fatal(err)
		}
		stop()
		// Simulate the durable reserve having committed, followed by a crash before
		// the existing Invoke path accepted its exact source-change trigger.
		tagID := "source-recovery-event"
		runID := runtime.RunID(tenant, tagID)
		if _, err := p.Operations.ReserveSource(tenant, a.ID, runID, tagID, a.Version, 2, time.Now()); err != nil {
			t.Fatal(err)
		}
		versions.set(2, true)
		p2, stop2 := runningWith(t, platform.Config{Backends: again(), AssignmentSources: versions})
		defer stop2()
		sourceAwait(t, func() bool { _, err := p2.Runs.Get(tenant, runID); return err == nil })
		run, err := p2.Runs.Get(tenant, runID)
		if err != nil {
			t.Fatal(err)
		}
		if run.ID != runID || run.Origin != "assignment" {
			t.Fatalf("reservation changed trigger identity: %+v", run)
		}
		sourceAwait(t, func() bool {
			got, _, _ := p2.Operations.Assignment(tenant, a.ID)
			return got.State == operations.Watching && got.SourceVersion == 2 && got.RunID == runID
		})
	})
}
