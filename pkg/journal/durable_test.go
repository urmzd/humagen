package journal

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/urmzd/humagen/fiber/pkg/event"

	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

func TestRestart(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		j, err := Open(st, 0)
		if err != nil {
			t.Fatal(err)
		}
		src := filled(t)
		src.mu.RLock()
		src.each(func(h held) { j.Append(h.e, h.origin) })
		src.mu.RUnlock()
		// Nothing is written until a flush.
		if early, _ := Open(st, 0); len(early.Events("acme", []string{"r1"})) != 0 {
			t.Fatal("events were written before a flush")
		}
		if err := j.Flush(); err != nil {
			t.Fatal(err)
		}
		again, err := Open(st, 0)
		if err != nil {
			t.Fatal(err)
		}
		if got, want := again.Events("acme", []string{"r1"}), j.Events("acme", []string{"r1"}); !reflect.DeepEqual(got, want) || len(got) != 7 {
			t.Fatalf("events after restart = %+v, want %+v", got, want)
		}
		if got, want := again.Trace("acme", []string{"r1"}), j.Trace("acme", []string{"r1"}); !reflect.DeepEqual(got, want) {
			t.Fatalf("trace after restart = %+v, want %+v", got, want)
		}
		if got := again.Query(Query{Tenant: "globex"}); len(got) != 1 {
			t.Fatalf("globex after restart = %+v", got)
		}
	})
}

// The ring holds no more in the store than in memory, and a restart with a
// smaller capacity keeps the newest events.
func TestRestartKeepsTheBound(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		j, _ := Open(st, 5)
		for i := range 12 {
			j.Append(ev(t, "acme", "r1", i, event.Text{Text: fmt.Sprint(i)}), "slack")
			if i%4 == 0 {
				if err := j.Flush(); err != nil {
					t.Fatal(err)
				}
			}
		}
		if err := j.Flush(); err != nil {
			t.Fatal(err)
		}
		recs, _ := st.Load(t.Context(), "journal")
		if len(recs) != 5+1 {
			t.Fatalf("the store holds %d records for a ring of 5", len(recs))
		}
		again, _ := Open(st, 5)
		got := again.Events("acme", []string{"r1"})
		if len(got) != 5 || got[0].Seq != 7 || got[4].Seq != 11 {
			t.Fatalf("after restart = %d events, first %d", len(got), got[0].Seq)
		}
		smaller, err := Open(st, 3)
		if err != nil {
			t.Fatal(err)
		}
		if got := smaller.Events("acme", []string{"r1"}); len(got) != 3 || got[0].Seq != 9 {
			t.Fatalf("with a smaller ring = %+v", got)
		}
		smaller.Append(ev(t, "acme", "r1", 12, event.Text{Text: "12"}), "slack")
		if err := smaller.Flush(); err != nil {
			t.Fatal(err)
		}
		last, _ := Open(st, 3)
		if got := last.Events("acme", []string{"r1"}); len(got) != 3 || got[0].Seq != 10 || got[2].Seq != 12 {
			t.Fatalf("after writing to the smaller ring = %+v", got)
		}
		if recs, _ := st.Load(t.Context(), "journal"); len(recs) != 3+1 {
			t.Fatalf("the store holds %d records for a ring of 3", len(recs))
		}
	})
}

func TestFailedFlushIsRetried(t *testing.T) {
	f := statetest.NewFaulty(state.NewMemory())
	j, _ := Open(f, 0)
	j.Append(ev(t, "acme", "r1", 0, event.Accepted{}), "slack")
	f.Fail(statetest.ErrInjected)
	if err := j.Flush(); !errors.Is(err, statetest.ErrInjected) {
		t.Fatalf("Flush = %v", err)
	}
	j.Append(ev(t, "acme", "r1", 1, event.Started{Attempt: 1}), "slack")
	f.Fail(nil)
	if err := j.Flush(); err != nil {
		t.Fatal(err)
	}
	again, _ := Open(f, 0)
	if got := again.Events("acme", []string{"r1"}); len(got) != 2 || got[0].Seq != 0 || got[1].Seq != 1 {
		t.Fatalf("after a failed flush and a good one = %+v", got)
	}
}
