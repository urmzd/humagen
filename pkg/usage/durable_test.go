package usage

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/policy"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

func TestRestart(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		l, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		admit(l, "acme", "Launch", "lead", "r1", t0)
		admit(l, "acme", "", "helper", "r2", t0.Add(time.Hour))
		call(l, "acme", "r1", "r1/a1/1", t0.Add(time.Minute), Spend{Micros: 300, ModelCalls: 1, Tokens: 1000})
		if _, _, err := l.Record(Entry{Kind: Settled, Tenant: "acme", Project: "Launch", Agent: "lead", RunID: "r1", Key: "run:r1",
			Spend: &Spend{Micros: 500, ModelCalls: 2, Tokens: 1500}, At: t0.Add(2 * time.Minute)}); err != nil {
			t.Fatal(err)
		}
		if _, err := l.Append(Entry{Kind: Ended, Tenant: "acme", RunID: "r1", At: t0.Add(3 * time.Minute)}); err != nil {
			t.Fatal(err)
		}
		if _, err := l.Append(Entry{Kind: Refused, Tenant: "acme", Agent: "lead", At: t0, Scope: &launch, Version: 2, Limit: policy.LimitRuns, Reason: "full"}); err != nil {
			t.Fatal(err)
		}

		again, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(again.all("acme"), l.all("acme")) {
			t.Fatalf("entries after restart = %+v\nwant %+v", again.all("acme"), l.all("acme"))
		}
		for _, scope := range []policy.Scope{all, launch, lead} {
			if got, want := again.used("acme", scope, t0), l.used("acme", scope, t0); !reflect.DeepEqual(got, want) {
				t.Fatalf("%v after restart = %+v, want %+v", scope, got, want)
			}
		}
		if got := again.used("acme", all, t0); got.Concurrent != 1 || got.Spend != 500 {
			t.Fatalf("after restart: %+v", got)
		}
		// A call counted before the restart is not counted again.
		if call(again, "acme", "r1", "r1/a1/1", t0.Add(time.Minute), Spend{Micros: 300, ModelCalls: 1}) {
			t.Fatal("a call was counted twice across a restart")
		}
		if n := again.notices("acme", 0); len(n) != 1 || n[0].Reason != "full" {
			t.Fatalf("notices after restart = %+v", n)
		}
	})
}

// What no period needs any more is deleted from the store, entries and
// counts alike.
func TestRestartAfterPruning(t *testing.T) {
	st := state.NewMemory()
	l, _ := Open(st)
	admit(l, "acme", "", "lead", "old", t0)
	call(l, "acme", "old", "old/1", t0, Spend{Micros: 5, ModelCalls: 1})
	later := t0.Add(keepAdmissions + 48*time.Hour)
	admit(l, "acme", "", "lead", "new", later)
	recs, _ := st.Load(t.Context(), "usage")
	for _, r := range recs {
		if strings.Contains(r.Key, dayKey(t0)) || r.Key == spentKey("old/1") || r.Key == runKey("old") {
			t.Fatalf("the store still holds %s after a prune", r.Key)
		}
	}
	again, _ := Open(st)
	if e := again.all("acme"); len(e) != 1 || e[0].RunID != "new" {
		t.Fatalf("after restart = %+v", e)
	}
}

// A write that fails is not counted, whatever the entry: an admission is
// refused, and a call is reported to the caller, nothing kept to write later.
func TestFailedWrites(t *testing.T) {
	f := statetest.NewFaulty(state.NewMemory())
	l, _ := Open(f)
	f.Fail(statetest.ErrInjected)
	if ok, err := l.Append(Entry{Kind: Admitted, Tenant: "acme", Agent: "lead", RunID: "r1", At: t0}); ok || !errors.Is(err, statetest.ErrInjected) {
		t.Fatalf("Append(admitted) = %v, %v", ok, err)
	}
	if added, ok, err := l.Record(Entry{Kind: Spent, Tenant: "acme", Agent: "lead", RunID: "r0", Key: "k1", Spend: &Spend{Micros: 7, ModelCalls: 1}, At: t0}); ok || !added.IsZero() || !errors.Is(err, statetest.ErrInjected) {
		t.Fatalf("Record(spent) = %+v, %v, %v", added, ok, err)
	}
	f.Fail(nil)
	if got := l.used("acme", all, t0); got.Spend != 0 || got.Runs != 0 || got.Concurrent != 0 {
		t.Fatalf("a failed write was counted: %+v", got)
	}
	// Delivered again once the store is back, the call counts once.
	if added, ok, err := l.Record(Entry{Kind: Spent, Tenant: "acme", Agent: "lead", RunID: "r0", Key: "k1", Spend: &Spend{Micros: 7, ModelCalls: 1}, At: t0}); !ok || added.Micros != 7 || err != nil {
		t.Fatalf("Record(spent) after recovery = %+v, %v, %v", added, ok, err)
	}
	again, _ := Open(f)
	if got := again.used("acme", all, t0); got.Spend != 7 || got.Runs != 0 {
		t.Fatalf("after the store recovered and a restart = %+v", got)
	}
}

// The ancestors an entry was admitted within are kept with it, so a parent's
// count is the same after a restart.
func TestWithinSurvivesARestart(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		l, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		field := policy.Scope{Kind: policy.Project, Name: "Field"}
		if _, err := l.Append(Entry{Kind: Admitted, Tenant: "acme", Project: "Globex", Within: []string{"Field"}, Agent: "lead", RunID: "r1", At: t0}); err != nil {
			t.Fatal(err)
		}
		if _, err := l.Append(Entry{Kind: Spent, Tenant: "acme", Project: "Globex", Within: []string{"Field"}, Agent: "lead", RunID: "r1", Key: "r1/1",
			Spend: &Spend{Micros: 42, ModelCalls: 1}, At: t0}); err != nil {
			t.Fatal(err)
		}
		again, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		if got := again.used("acme", field, t0); got.Runs != 1 || got.Concurrent != 1 || got.Spend != 42 {
			t.Fatalf("Field after restart = %+v", got)
		}
	})
}
