package callpolicy_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/callpolicy"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

// Two processes on one database, each with its own store, see each other's
// policies at once; names kept through both at the same time are all kept,
// and policies created through both at the same time are counted exactly
// against the cap.
func TestTwoProcessesAgree(t *testing.T) {
	at := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		one, _ := callpolicy.Open(a)
		two, _ := callpolicy.Open(b)
		const writers = 16
		var names []string
		for i := range writers {
			names = append(names, fmt.Sprintf("c%02d", i))
		}
		ok := known(names...)
		if _, err := one.Put("acme", callpolicy.Policy{Agent: "h", Callers: callpolicy.Listed, Allow: []string{}, Approval: callpolicy.Never}, ok, "olu", at); err != nil {
			t.Fatal(err)
		}
		if got, found, err := two.Get("acme", "h"); err != nil || !found || got.Callers != callpolicy.Listed {
			t.Fatalf("the other process reads %+v, %v, %v", got, found, err)
		}

		// Each writer adds its own name and keeps every name stored.
		keepAll := func(stored callpolicy.Policy) ([]string, []string) { return stored.Allow, nil }
		var wg sync.WaitGroup
		for i, name := range names {
			s := []*callpolicy.Store{one, two}[i%2]
			wg.Go(func() {
				if _, err := s.PutKeeping("acme", callpolicy.Policy{Agent: "h", Callers: callpolicy.Listed, Allow: []string{name}, Approval: callpolicy.Never}, ok, keepAll, "olu", at); err != nil {
					t.Error(err)
				}
			})
		}
		wg.Wait()
		got, _, err := one.Get("acme", "h")
		if err != nil {
			t.Fatal(err)
		}
		slices.Sort(got.Allow)
		if !slices.Equal(got.Allow, names) || got.Version != writers+1 {
			t.Fatalf("after concurrent writes: allow %v, version %d", got.Allow, got.Version)
		}

		// Fill another tenant to a few short of the cap with policies
		// written before the count existed, then create through both processes at
		// once: the count is made once, exactly the places left are taken,
		// and the rest are refused.
		const left, racing = 8, 24
		const tenant = "globex"
		var ops []state.Op
		for i := range callpolicy.MaxPolicies - left {
			raw, err := json.Marshal(callpolicy.Policy{Agent: fmt.Sprintf("a%04d", i), Callers: callpolicy.Anyone, Approval: callpolicy.Never, Version: 1, UpdatedAt: at})
			if err != nil {
				t.Fatal(err)
			}
			ops = append(ops, state.Put(tenant, fmt.Sprintf("a%04d", i), raw))
		}
		for len(ops) > 0 {
			n := min(len(ops), 500)
			if err := a.Write(t.Context(), "callpolicy", ops[:n]...); err != nil {
				t.Fatal(err)
			}
			ops = ops[n:]
		}
		var mu sync.Mutex
		kept, limited := 0, 0
		var race sync.WaitGroup
		for i := range racing {
			s := []*callpolicy.Store{one, two}[i%2]
			race.Go(func() {
				_, err := s.Put(tenant, callpolicy.Policy{Agent: fmt.Sprintf("r%02d", i), Callers: callpolicy.Anyone, Approval: callpolicy.Never}, ok, "olu", at)
				mu.Lock()
				defer mu.Unlock()
				switch {
				case err == nil:
					kept++
				case errors.Is(err, callpolicy.ErrLimit):
					limited++
				default:
					t.Error(err)
				}
			})
		}
		race.Wait()
		if kept != left || limited != racing-left {
			t.Fatalf("kept %d, refused %d", kept, limited)
		}
		snaps, err := two.Snapshots(tenant, at.Add(-time.Hour), at.Add(time.Hour), 500, nil)
		if err != nil || len(snaps) != 500 {
			t.Fatalf("Snapshots = %d, %v", len(snaps), err)
		}
		// A delete through one frees a place for a create through the other.
		if err := one.Delete(tenant, "a0000"); err != nil {
			t.Fatal(err)
		}
		if _, err := two.Put(tenant, callpolicy.Policy{Agent: "late", Callers: callpolicy.Anyone, Approval: callpolicy.Never}, ok, "olu", at); err != nil {
			t.Fatalf("create after a delete: %v", err)
		}
		if _, found, _ := two.Get(tenant, "a0000"); found {
			t.Fatal("a deleted policy is still read")
		}
	})
}
