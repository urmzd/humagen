package journal

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
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
		for _, e := range append(events(t, src, "acme", "r1"), events(t, src, "globex", "r9")...) {
			appended(t, j, e)
		}
		// An event is written as it is appended.
		again, err := Open(st, 0)
		if err != nil {
			t.Fatal(err)
		}
		if got, want := events(t, again, "acme", "r1"), events(t, src, "acme", "r1"); !reflect.DeepEqual(got, want) || len(got) != 7 {
			t.Fatalf("events after restart = %+v, want %+v", got, want)
		}
		if got, want := traced(t, again, "acme", "r1"), traced(t, src, "acme", "r1"); !reflect.DeepEqual(got, want) {
			t.Fatalf("trace after restart = %+v, want %+v", got, want)
		}
		if got := query(t, again, Query{Tenant: "globex"}); len(got) != 1 {
			t.Fatalf("globex after restart = %+v", got)
		}
	})
}

// A tenant holds no more than the capacity, another tenant's events do not
// count against it, and a journal opened with a smaller capacity shrinks to
// it as events arrive.
func TestEachTenantKeepsTheBound(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		j, _ := Open(st, 5)
		for i := range 12 {
			appended(t, j, ev(t, "acme", "r1", i, event.Text{Text: fmt.Sprint(i)}))
		}
		for i := range 3 {
			appended(t, j, ev(t, "globex", "r9", i, event.Text{Text: fmt.Sprint(i)}))
		}
		if n := kept(t, st, "acme"); n != 5 {
			t.Fatalf("acme holds %d events for a capacity of 5", n)
		}
		if got := events(t, j, "globex", "r9"); len(got) != 3 {
			t.Fatalf("globex = %d events", len(got))
		}
		again, _ := Open(st, 5)
		got := events(t, again, "acme", "r1")
		if len(got) != 5 || got[0].Seq != 7 || got[4].Seq != 11 {
			t.Fatalf("after restart = %d events, first %d", len(got), got[0].Seq)
		}
		smaller, err := Open(st, 3)
		if err != nil {
			t.Fatal(err)
		}
		appended(t, smaller, ev(t, "acme", "r1", 12, event.Text{Text: "12"}))
		if got := events(t, smaller, "acme", "r1"); len(got) != 3 || got[0].Seq != 10 || got[2].Seq != 12 {
			t.Fatalf("after writing with a smaller capacity = %+v", got)
		}
		if n := kept(t, st, "acme"); n != 3 {
			t.Fatalf("acme holds %d events for a capacity of 3", n)
		}
	})
}

// kept counts a tenant's events by time and checks that each has its twin by
// run and that the count says as much.
func kept(t *testing.T, st state.Store, tenant string) int {
	t.Helper()
	recs, err := st.Load(t.Context(), namespace)
	if err != nil {
		t.Fatal(err)
	}
	byTime, byRun := 0, 0
	for _, r := range recs {
		if r.Tenant != tenant {
			continue
		}
		switch {
		case strings.HasPrefix(r.Key, eventPrefix):
			byTime++
		case strings.HasPrefix(r.Key, runPrefix):
			byRun++
		}
	}
	n, err := state.NewTable[stored](st, namespace).Lookup(tenant, countKey)
	if err != nil {
		t.Fatal(err)
	}
	if byTime != byRun || int64(byTime) != n.Value.Count {
		t.Fatalf("%s: %d by time, %d by run, count %d", tenant, byTime, byRun, n.Value.Count)
	}
	return byTime
}

func TestAFailedWriteIsAnError(t *testing.T) {
	f := statetest.NewFaulty(state.NewMemory())
	j, _ := Open(f, 0)
	appended(t, j, ev(t, "acme", "r1", 0, event.Accepted{}))
	f.Fail(statetest.ErrInjected)
	if err := j.Append(ev(t, "acme", "r1", 1, event.Started{Attempt: 1}), "slack"); !errors.Is(err, statetest.ErrInjected) {
		t.Fatalf("Append = %v", err)
	}
	f.Fail(nil)
	appended(t, j, ev(t, "acme", "r1", 1, event.Started{Attempt: 1}))
	if got := events(t, j, "acme", "r1"); len(got) != 2 || got[0].Seq != 0 || got[1].Seq != 1 {
		t.Fatalf("after a failed write and a good one = %+v", got)
	}
}
