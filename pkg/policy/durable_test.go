package policy

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/urmzd/warp/pkg/state"
	"github.com/urmzd/warp/pkg/state/statetest"
)

func TestRestart(t *testing.T) {
	at := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		s, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.Put("acme", Policy{Scope: tenantScope, Period: Day, MaxRuns: 100, SpendLimit: 5_000_000, Action: Block}, "olu", at); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Put("acme", Policy{Scope: lead, Period: Day, MaxRuns: 10, Action: Block}, "olu", at.Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
		if err := s.Delete("acme", lead, "olu", at.Add(2*time.Hour)); err != nil {
			t.Fatal(err)
		}

		again, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(again.List("acme"), s.List("acme")) {
			t.Fatalf("policies after restart = %+v, want %+v", again.List("acme"), s.List("acme"))
		}
		if !reflect.DeepEqual(again.History("acme"), s.History("acme")) || len(again.History("acme")) != 3 {
			t.Fatalf("history after restart = %+v", again.History("acme"))
		}
		if p, ok := again.At("acme", lead, at.Add(90*time.Minute)); !ok || p.MaxRuns != 10 {
			t.Fatalf("At after restart = %+v, %v", p, ok)
		}
		// A version counts on across a delete and a restart.
		p, err := again.Put("acme", Policy{Scope: lead, Period: Day, MaxRuns: 5, Action: Block}, "olu", at.Add(3*time.Hour))
		if err != nil || p.Version != 2 {
			t.Fatalf("Put after restart = %+v, %v", p, err)
		}
		if len(again.List("globex")) != 0 {
			t.Fatal("another tenant sees the policies")
		}
	})
}

func TestFailedWriteChangesNothing(t *testing.T) {
	f := statetest.NewFaulty(state.NewMemory())
	s, _ := Open(f)
	at := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	if _, err := s.Put("acme", Policy{Scope: tenantScope, Period: Day, MaxRuns: 100, Action: Block}, "olu", at); err != nil {
		t.Fatal(err)
	}
	f.Fail(statetest.ErrInjected)
	if _, err := s.Put("acme", Policy{Scope: tenantScope, Period: Day, MaxRuns: 5, Action: Block}, "olu", at); !errors.Is(err, statetest.ErrInjected) {
		t.Fatalf("Put = %v", err)
	}
	if err := s.Delete("acme", tenantScope, "olu", at); !errors.Is(err, statetest.ErrInjected) {
		t.Fatalf("Delete = %v", err)
	}
	got := s.List("acme")
	if len(got) != 1 || got[0].MaxRuns != 100 || got[0].Version != 1 || len(s.History("acme")) != 1 {
		t.Fatalf("after failed writes = %+v, history %d", got, len(s.History("acme")))
	}
	f.Fail(nil)
	if p, _ := s.Put("acme", Policy{Scope: tenantScope, Period: Day, MaxRuns: 5, Action: Block}, "olu", at); p.Version != 2 {
		t.Fatalf("a failed write used a version: %d", p.Version)
	}
}
