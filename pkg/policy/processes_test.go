package policy

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

func agentPolicy(name string, runs int) Policy {
	return Policy{Scope: Scope{Agent, name}, Period: Day, MaxRuns: runs, Action: Block}
}

// Two processes on one database, each with its own store, see each other's
// policies at once, and changes made through both at the same time are each
// kept: every one is in the history, and each scope's versions count on
// without a gap or a repeat.
func TestTwoProcessesAgree(t *testing.T) {
	at := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		one, _ := Open(a)
		two, _ := Open(b)
		if _, err := one.Put("acme", agentPolicy("lead", 10), "olu", at); err != nil {
			t.Fatal(err)
		}
		if got := forRun(t, two, "acme", nil, "lead", ""); len(got) != 1 || got[0].MaxRuns != 10 {
			t.Fatalf("the other process reads %+v", got)
		}

		const writes = 24
		var wg sync.WaitGroup
		for i := range writes {
			s := []*Store{one, two}[i%2]
			wg.Go(func() {
				if _, err := s.Put("acme", agentPolicy(fmt.Sprintf("a%d", i%3), i+1), "olu", at.Add(time.Duration(i)*time.Minute)); err != nil {
					t.Error(err)
				}
			})
		}
		wg.Wait()
		for _, s := range []*Store{one, two} {
			h := history(t, s, "acme")
			if len(h) != writes+1 {
				t.Fatalf("%d changes, want %d", len(h), writes+1)
			}
			versions := map[Scope][]int{}
			for i := len(h) - 1; i >= 0; i-- {
				versions[h[i].Scope] = append(versions[h[i].Scope], h[i].After.Version)
			}
			for scope, vs := range versions {
				for i, v := range vs {
					if v != i+1 {
						t.Fatalf("%s versions %v", scope, vs)
					}
				}
			}
			if got := list(t, s, "acme"); len(got) != 4 {
				t.Fatalf("policies %+v", got)
			}
		}
		if got := list(t, two, "other"); len(got) != 0 {
			t.Fatalf("another tenant reads %+v", got)
		}
	})
}

// A tenant written before history was kept under its own keys has it moved
// once, by the first change after, though two processes change it at the
// same time: nothing lost, nothing twice, the old changes first.
func TestTwoProcessesConvertOnce(t *testing.T) {
	at := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		const old = 30
		var r stored
		for i := range old {
			p := agentPolicy("lead", i+1)
			p.Version = i + 1
			r.History = append(r.History, Change{Scope: p.Scope, By: "old", At: at.Add(time.Duration(i) * time.Minute), After: &p})
		}
		last := *r.History[old-1].After
		r.Policies = []Policy{last}
		r.Versions = []version{{Scope: last.Scope, Version: old}}
		raw, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		if err := a.Write(t.Context(), namespace, state.Put("acme", tenantKey, raw)); err != nil {
			t.Fatal(err)
		}
		one, _ := Open(a)
		two, _ := Open(b)
		if h := history(t, two, "acme"); len(h) != old || h[0].After.MaxRuns != old {
			t.Fatalf("history before converting: %d changes", len(h))
		}
		if p, ok := policyAt(t, one, "acme", Scope{Agent, "lead"}, at.Add(5*time.Minute)); !ok || p.MaxRuns != 6 {
			t.Fatalf("At before converting = %+v, %v", p, ok)
		}

		const writes = 16
		var wg sync.WaitGroup
		for i := range writes {
			s := []*Store{one, two}[i%2]
			wg.Go(func() {
				if _, err := s.Put("acme", agentPolicy(fmt.Sprintf("n%d", i), 1), "new", at.Add(time.Hour)); err != nil {
					t.Error(err)
				}
			})
		}
		wg.Wait()
		h := history(t, one, "acme")
		if len(h) != old+writes {
			t.Fatalf("%d changes after converting, want %d", len(h), old+writes)
		}
		seen := map[Scope]bool{}
		for i, c := range h {
			if i < writes {
				if c.By != "new" || seen[c.Scope] {
					t.Fatalf("change %d = %+v", i, c)
				}
				seen[c.Scope] = true
				continue
			}
			if want := old - (i - writes); c.By != "old" || c.After.MaxRuns != want {
				t.Fatalf("change %d = %+v, want old change %d", i, c, want)
			}
		}
		rec, err := b.Lookup(t.Context(), namespace, "acme", tenantKey)
		if err != nil {
			t.Fatal(err)
		}
		var now stored
		if err := json.Unmarshal(rec.Value, &now); err != nil {
			t.Fatal(err)
		}
		if len(now.History) != 0 || now.Head != old+writes {
			t.Fatalf("record after converting: %d changes inside, head %d", len(now.History), now.Head)
		}
		if p, ok := policyAt(t, two, "acme", Scope{Agent, "lead"}, at.Add(5*time.Minute)); !ok || p.MaxRuns != 6 {
			t.Fatalf("At after converting = %+v, %v", p, ok)
		}
		if p, err := two.Put("acme", agentPolicy("lead", 99), "olu", at.Add(2*time.Hour)); err != nil || p.Version != old+1 {
			t.Fatalf("Put after converting = %+v, %v", p, err)
		}
	})
}

// History keeps the newest maxHistory changes and drops the oldest.
func TestHistoryIsBounded(t *testing.T) {
	s := NewStore()
	at := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	for i := range maxHistory + 5 {
		if _, err := s.Put("acme", agentPolicy("lead", i+1), "olu", at.Add(time.Duration(i)*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	h := history(t, s, "acme")
	if len(h) != maxHistory || h[0].After.MaxRuns != maxHistory+5 || h[len(h)-1].After.MaxRuns != 6 {
		t.Fatalf("%d changes, newest %d, oldest %d", len(h), h[0].After.MaxRuns, h[len(h)-1].After.MaxRuns)
	}
	if _, ok := policyAt(t, s, "acme", Scope{Agent, "lead"}, at.Add(3*time.Second)); ok {
		t.Fatal("At reads a change past the history")
	}
	if p, ok := policyAt(t, s, "acme", Scope{Agent, "lead"}, at.Add(500*time.Second)); !ok || p.MaxRuns != 501 {
		t.Fatalf("At = %+v, %v", p, ok)
	}
}
