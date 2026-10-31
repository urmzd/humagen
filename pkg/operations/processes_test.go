package operations_test

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/operations"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

func openTwo(t *testing.T, a, b state.Store) (*operations.Store, *operations.Store) {
	t.Helper()
	one, err := operations.Open(a)
	if err != nil {
		t.Fatal(err)
	}
	two, err := operations.Open(b)
	if err != nil {
		t.Fatal(err)
	}
	return one, two
}

// Two processes on one database see each other's writes at once, and
// writers on one control through either are each kept: a stale version
// conflicts and is retried, never lost.
func TestTwoProcessesAgree(t *testing.T) {
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		one, two := openTwo(t, a, b)
		at := time.Now().UTC()
		if _, err := one.SetControl("acme", "helper", operations.Control{Paused: true}, 0, "ana", at); err != nil {
			t.Fatal(err)
		}
		if paused, err := two.Paused("acme", "helper"); err != nil || !paused {
			t.Fatalf("the other process reads paused = %v, %v", paused, err)
		}

		const writers = 16
		var wg sync.WaitGroup
		for i := range writers {
			s := []*operations.Store{one, two}[i%2]
			wg.Go(func() {
				for {
					c, err := s.Control("acme", "counter")
					if err != nil {
						t.Error(err)
						return
					}
					c.StopRules += fmt.Sprintf("w%02d;", i)
					if _, err := s.SetControl("acme", "counter", c, c.Version, "ana", at); errors.Is(err, operations.ErrConflict) {
						continue
					} else if err != nil {
						t.Error(err)
					}
					return
				}
			})
		}
		wg.Wait()
		c, err := one.Control("acme", "counter")
		if err != nil {
			t.Fatal(err)
		}
		if c.Version != writers {
			t.Fatalf("version %d after %d writes", c.Version, writers)
		}
		for i := range writers {
			if want := fmt.Sprintf("w%02d;", i); !strings.Contains(c.StopRules, want) {
				t.Fatalf("lost write %s: %q", want, c.StopRules)
			}
		}
		// The other tenant reads nothing.
		if c, err := two.Control("other", "counter"); err != nil || c.Version != 0 {
			t.Fatalf("another tenant reads %+v, %v", c, err)
		}
		var tenants []string
		if err := two.EachTenant(func(t string) error { tenants = append(tenants, t); return nil }); err != nil || len(tenants) != 1 || tenants[0] != "acme" {
			t.Fatalf("tenants = %v, %v", tenants, err)
		}
	})
}

// Creates through both processes count exactly toward the tenant's cap.
func TestTwoProcessesCountEveryRecord(t *testing.T) {
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		one, two := openTwo(t, a, b)
		at := time.Now().UTC()
		const records = 40
		var wg sync.WaitGroup
		for i := range records {
			s := []*operations.Store{one, two}[i%2]
			wg.Go(func() {
				if _, err := s.SetControl("acme", fmt.Sprintf("agent-%02d", i), operations.Control{}, 0, "ana", at); err != nil {
					t.Error(err)
				}
			})
		}
		wg.Wait()
		n, err := state.NewTable[int](a, "operations").Lookup("acme", "#count")
		if err != nil {
			t.Fatal(err)
		}
		if n.Value != records {
			t.Fatalf("count %d, want %d", n.Value, records)
		}
	})
}

// A run reserved through one process settles through the other, and a
// deadline marked at the same time is not lost: Settle reads the record as
// it stands.
func TestTwoProcessesSettleByRun(t *testing.T) {
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		one, two := openTwo(t, a, b)
		at := time.Now().UTC()
		const n = 12
		for i := range n {
			id := fmt.Sprintf("a%02d", i)
			as, err := one.SetAssignment("acme", operations.Assignment{ID: id, Agent: "helper", Owner: "ana", Goal: "Work", DoneWhen: "Done", State: operations.Requested, DueAt: at.Add(time.Minute)}, 0, "ana", at)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := two.Reserve("acme", id, "run-"+id, as.Version, at); err != nil {
				t.Fatal(err)
			}
		}
		var wg sync.WaitGroup
		wg.Go(func() {
			if _, err := one.Due("acme", at.Add(2*time.Minute)); err != nil {
				t.Error(err)
			}
		})
		for i := range n {
			wg.Go(func() {
				if err := two.Settle("acme", fmt.Sprintf("run-a%02d", i), true, at.Add(3*time.Minute)); err != nil {
					t.Error(err)
				}
			})
		}
		wg.Wait()
		for i := range n {
			got, ok, err := one.Assignment("acme", fmt.Sprintf("a%02d", i))
			if err != nil || !ok {
				t.Fatal(ok, err)
			}
			if got.State != operations.Result {
				t.Fatalf("not settled: %+v", got)
			}
		}
		// Settling again finds nothing to change.
		if err := one.Settle("acme", "run-a00", false, at.Add(4*time.Minute)); err != nil {
			t.Fatal(err)
		}
		if got, _, _ := two.Assignment("acme", "a00"); got.State != operations.Result {
			t.Fatalf("settled twice: %+v", got)
		}
	})
}

// Records an older release kept, reserved without a run index, settle once
// two processes open the store at the same time.
func TestTwoProcessesConvertOnce(t *testing.T) {
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		old := state.NewTable[map[string]any](a, "operations")
		at := time.Now().UTC()
		for i := range 20 {
			id := fmt.Sprintf("a%02d", i)
			as := operations.Assignment{ID: id, Agent: "helper", Owner: "ana", Goal: "Work", DoneWhen: "Done", Trigger: operations.ManualTrigger, State: operations.InProgress, RunID: "run-" + id, Version: 2, CreatedAt: at, UpdatedAt: at}
			if err := old.Put(fmt.Sprintf("t%d", i%3), "assignment:"+id, map[string]any{"assignment": as}); err != nil {
				t.Fatal(err)
			}
		}
		var wg sync.WaitGroup
		stores := make([]*operations.Store, 4)
		for i := range stores {
			wg.Go(func() {
				s, err := operations.Open([]state.Store{a, b}[i%2])
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
		for i := range 20 {
			id := fmt.Sprintf("a%02d", i)
			tenant := fmt.Sprintf("t%d", i%3)
			if err := stores[i%4].Settle(tenant, "run-"+id, true, at); err != nil {
				t.Fatal(err)
			}
			got, _, err := stores[(i+1)%4].Assignment(tenant, id)
			if err != nil || got.State != operations.Result {
				t.Fatalf("%s/%s = %+v, %v", tenant, id, got, err)
			}
		}
		var tenants []string
		if err := stores[0].EachTenant(func(t string) error { tenants = append(tenants, t); return nil }); err != nil || len(tenants) != 3 {
			t.Fatalf("tenants = %v, %v", tenants, err)
		}
	})
}
