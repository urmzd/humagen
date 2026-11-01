package platform

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/urmzd/humagen/fiber/pkg/connectors/cron"
	"github.com/urmzd/humagen/pkg/operations"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

func nightly(tenant, name string) cron.Schedule {
	return cron.Schedule{Name: name, Expression: "0 2 * * *", Agent: "lead", Text: "report", Tenant: tenant}
}

// Two processes on one database see each other's schedules at once, a name
// is one workspace's however many processes create it, and a pause and an
// edit made at the same time through either are both kept.
func TestSchedulesTwoProcessesAgree(t *testing.T) {
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		one, err := OpenSchedules(a)
		if err != nil {
			t.Fatal(err)
		}
		two, err := OpenSchedules(b)
		if err != nil {
			t.Fatal(err)
		}
		if err := one.Add(nightly("acme", "nightly"), About{Description: "report"}); err != nil {
			t.Fatal(err)
		}
		if e, err := two.Entry("acme", "nightly"); err != nil || e.About.Description != "report" {
			t.Fatalf("the other process reads %+v, %v", e, err)
		}
		if err := two.Add(nightly("globex", "nightly"), About{}); !errors.Is(err, ErrScheduleExists) {
			t.Fatalf("another workspace took the name: %v", err)
		}

		// Racing creates of one name in different workspaces: one wins.
		var wg sync.WaitGroup
		wins := make([]bool, 8)
		for i := range wins {
			s := []*Schedules{one, two}[i%2]
			wg.Go(func() {
				err := s.Add(nightly(fmt.Sprintf("t%d", i), "contested"), About{})
				wins[i] = err == nil
				if err != nil && !errors.Is(err, ErrScheduleExists) {
					t.Error(err)
				}
			})
		}
		wg.Wait()
		won := 0
		for _, w := range wins {
			if w {
				won++
			}
		}
		if won != 1 {
			t.Fatalf("%d workspaces created the same name", won)
		}

		// A pause through one and an edit through the other, at once, are
		// both kept.
		for i := range 10 {
			paused := i%2 == 0
			text := fmt.Sprintf("report %d", i)
			wg.Go(func() {
				if err := one.SetPaused("acme", "nightly", paused); err != nil {
					t.Error(err)
				}
			})
			wg.Go(func() {
				sc := nightly("acme", "nightly")
				sc.Text = text
				if err := two.Replace(sc, About{UpdatedBy: "bob"}); err != nil {
					t.Error(err)
				}
			})
			wg.Wait()
			sc, got, err := one.Get("acme", "nightly")
			if err != nil || got != paused || sc.Text != text {
				t.Fatalf("round %d: %+v paused %v, %v", i, sc, got, err)
			}
		}

		// Racing creates in one workspace through both are each kept.
		for i := range 20 {
			s := []*Schedules{one, two}[i%2]
			wg.Go(func() {
				if err := s.Add(nightly("acme", fmt.Sprintf("job-%02d", i)), About{}); err != nil {
					t.Error(err)
				}
			})
		}
		wg.Wait()
		for _, s := range []*Schedules{one, two} {
			all, err := s.List("acme")
			if err != nil || len(all) != 21 {
				t.Fatalf("%d schedules, %v", len(all), err)
			}
		}
		if n, _, err := one.count("acme"); err != nil || n != 21 {
			t.Fatalf("count %d, %v", n, err)
		}

		// Removed through one, the name is free through the other.
		if err := one.Remove("acme", "nightly"); err != nil {
			t.Fatal(err)
		}
		if err := two.Add(nightly("globex", "nightly"), About{}); err != nil {
			t.Fatalf("a removed name stayed taken: %v", err)
		}
		// The connector reads every workspace's schedules.
		all, err := two.Schedules(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if len(all) != 22 {
			t.Fatalf("the connector reads %d schedules", len(all))
		}
	})
}

// Names kept before claims were are claimed once, though processes open
// the store at the same time, and stay one workspace's.
func TestScheduleClaimsConvertOnce(t *testing.T) {
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		old := state.NewTable[storedSchedule](a, schedulesNamespace)
		for i := range 12 {
			tenant := fmt.Sprintf("t%d", i%3)
			if err := old.Put(tenant, fmt.Sprintf("job-%02d", i), storedSchedule{Seq: int64(i + 1), Schedule: nightly(tenant, fmt.Sprintf("job-%02d", i))}); err != nil {
				t.Fatal(err)
			}
		}
		var wg sync.WaitGroup
		stores := make([]*Schedules, 4)
		for i := range stores {
			wg.Go(func() {
				s, err := OpenSchedules([]state.Store{a, b}[i%2])
				if err != nil {
					t.Error(err)
				}
				stores[i] = s
			})
		}
		wg.Wait()
		if t.Failed() {
			return
		}
		if err := stores[1].Add(nightly("t9", "job-04"), About{}); !errors.Is(err, ErrScheduleExists) {
			t.Fatalf("a kept name was taken: %v", err)
		}
		all, err := stores[2].Schedules(t.Context())
		if err != nil || len(all) != 12 {
			t.Fatalf("the connector reads %d, %v", len(all), err)
		}
		if err := stores[3].Remove("t1", "job-04"); err != nil {
			t.Fatal(err)
		}
		if err := stores[0].Add(nightly("t9", "job-04"), About{}); err != nil {
			t.Fatalf("a kept name was not given up: %v", err)
		}
	})
}

// Two processes see each other's agent setups, and of two writes of one
// setup at one version, one is kept and the other refused.
func TestAgentSetupsTwoProcessesAgree(t *testing.T) {
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		one, _ := openAgentSetups(a)
		two, _ := openAgentSetups(b)
		record := AgentSetup{Name: "draft", Owner: "ada", Phase: "reserved", Version: 1, CreatedAt: time.Now().UTC()}
		if err := one.put("acme", record); err != nil {
			t.Fatal(err)
		}
		got, ok, err := two.get("acme", "draft")
		if err != nil || !ok || got.Phase != "reserved" {
			t.Fatalf("the other process reads %+v %v %v", got, ok, err)
		}
		if err := two.put("acme", record); !errors.Is(err, operations.ErrConflict) {
			t.Fatalf("a second create: %v", err)
		}
		for v := 2; v < 12; v++ {
			var wg sync.WaitGroup
			errs := make([]error, 2)
			for i, s := range []*agentSetups{one, two} {
				next := got
				next.Version, next.Phase = v, fmt.Sprintf("p%d-%d", v, i)
				wg.Go(func() { errs[i] = s.put("acme", next) })
			}
			wg.Wait()
			if (errs[0] == nil) == (errs[1] == nil) {
				t.Fatalf("version %d: %v", v, errs)
			}
			for _, err := range errs {
				if err != nil && !errors.Is(err, operations.ErrConflict) {
					t.Fatal(err)
				}
			}
			got, _, _ = one.get("acme", "draft")
			if got.Version != v {
				t.Fatalf("version %d read as %d", v, got.Version)
			}
		}
		if n := countOf(t, one, "acme"); n != 1 {
			t.Fatalf("count %d", n)
		}
	})
}

func countOf(t *testing.T, s *agentSetups, tenant string) int {
	t.Helper()
	var n int
	if _, err := s.rec.lookup(tenant, setupCount, &n); err != nil {
		t.Fatal(err)
	}
	return n
}

// Two processes read each other's settings at once, and changes to
// different fields made at the same time through either are both kept.
func TestSettingsTwoProcessesAgree(t *testing.T) {
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		var one, two settings
		if err := one.open(a); err != nil {
			t.Fatal(err)
		}
		if err := two.open(b); err != nil {
			t.Fatal(err)
		}
		if err := one.update("acme", func(v *storedSettings) error { v.WorkspaceName = "Acme"; return nil }); err != nil {
			t.Fatal(err)
		}
		if got, err := two.get("acme"); err != nil || got.WorkspaceName != "Acme" {
			t.Fatalf("the other process reads %+v, %v", got, err)
		}
		var wg sync.WaitGroup
		for i := range 10 {
			wg.Go(func() {
				err := one.update("acme", func(v *storedSettings) error { v.HistoryLimit = 1000 + i; return nil })
				if err != nil {
					t.Error(err)
				}
			})
			wg.Go(func() {
				err := two.update("acme", func(v *storedSettings) error { v.MemoryAutoApprove = true; v.TitleAgent = fmt.Sprint(i); return nil })
				if err != nil {
					t.Error(err)
				}
			})
		}
		wg.Wait()
		got, err := one.get("acme")
		if err != nil || got.WorkspaceName != "Acme" || got.HistoryLimit < 1000 || !got.MemoryAutoApprove || got.TitleAgent == "" {
			t.Fatalf("lost a change: %+v, %v", got, err)
		}
		// A baseline an older release kept survives a write that does not
		// name it.
		baseline := "lead"
		if err := one.update("globex", func(v *storedSettings) error { v.LegacyBaseline = &baseline; return nil }); err != nil {
			t.Fatal(err)
		}
		if err := two.update("globex", func(v *storedSettings) error { v.WorkspaceName = "Globex"; return nil }); err != nil {
			t.Fatal(err)
		}
		_, baselines, done, err := one.leftovers()
		if err != nil || done || baselines["globex"] != "lead" {
			t.Fatalf("leftovers %v %v %v", baselines, done, err)
		}
		if err := two.migrated(); err != nil {
			t.Fatal(err)
		}
		if _, _, done, err := one.leftovers(); err != nil || !done {
			t.Fatalf("the migration is not marked: %v %v", done, err)
		}
	})
}
