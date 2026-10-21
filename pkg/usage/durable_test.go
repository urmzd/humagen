package usage

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/urmzd/warp/pkg/policy"
	"github.com/urmzd/warp/pkg/state"
	"github.com/urmzd/warp/pkg/state/statetest"
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
		if !reflect.DeepEqual(again.Entries(), l.Entries()) {
			t.Fatalf("entries after restart = %+v\nwant %+v", again.Entries(), l.Entries())
		}
		for _, scope := range []policy.Scope{all, launch, lead} {
			if got, want := again.Usage("acme", scope, t0), l.Usage("acme", scope, t0); !reflect.DeepEqual(got, want) {
				t.Fatalf("%v after restart = %+v, want %+v", scope, got, want)
			}
		}
		if got := again.Usage("acme", all, t0); got.Concurrent != 1 || got.Spend != 500 {
			t.Fatalf("after restart: %+v", got)
		}
		// A call counted before the restart is not counted again.
		if call(again, "acme", "r1", "r1/a1/1", t0.Add(time.Minute), Spend{Micros: 300, ModelCalls: 1}) {
			t.Fatal("a call was counted twice across a restart")
		}
		if n := again.Notices("acme", 0); len(n) != 1 || n[0].Reason != "full" {
			t.Fatalf("notices after restart = %+v", n)
		}
	})
}

// What the ledger prunes from memory it deletes from the store.
func TestRestartAfterPruning(t *testing.T) {
	st := state.NewMemory()
	l, _ := Open(st)
	admit(l, "acme", "", "lead", "old", t0)
	later := t0.Add(keepAdmissions + 48*time.Hour)
	admit(l, "acme", "", "lead", "new", later)
	recs, _ := st.Load(t.Context(), "usage")
	if len(recs) != 1 {
		t.Fatalf("the store holds %d entries after a prune, want 1", len(recs))
	}
	again, _ := Open(st)
	if e := again.Entries(); len(e) != 1 || e[0].RunID != "new" {
		t.Fatalf("after restart = %+v", e)
	}
}

func TestFailedWrites(t *testing.T) {
	f := statetest.NewFaulty(state.NewMemory())
	l, _ := Open(f)
	f.Fail(statetest.ErrInjected)
	// An admission that cannot be kept is not counted.
	if ok, err := l.Append(Entry{Kind: Admitted, Tenant: "acme", Agent: "lead", RunID: "r1", At: t0}); ok || !errors.Is(err, statetest.ErrInjected) {
		t.Fatalf("Append(admitted) = %v, %v", ok, err)
	}
	if got := l.Usage("acme", all, t0); got.Runs != 0 || got.Concurrent != 0 {
		t.Fatalf("a refused admission was counted: %+v", got)
	}
	// A call already happened: it is counted and written later.
	if added, ok, err := l.Record(Entry{Kind: Spent, Tenant: "acme", Agent: "lead", RunID: "r0", Key: "k1", Spend: &Spend{Micros: 7, ModelCalls: 1}, At: t0}); !ok || added.Micros != 7 || !errors.Is(err, statetest.ErrInjected) {
		t.Fatalf("Record(spent) = %+v, %v, %v", added, ok, err)
	}
	if err := l.Flush(); !errors.Is(err, statetest.ErrInjected) {
		t.Fatalf("Flush = %v", err)
	}
	f.Fail(nil)
	if err := l.Flush(); err != nil {
		t.Fatal(err)
	}
	again, _ := Open(f)
	if got := again.Usage("acme", all, t0); got.Spend != 7 || got.Runs != 0 {
		t.Fatalf("after the store recovered and a restart = %+v", got)
	}
}
