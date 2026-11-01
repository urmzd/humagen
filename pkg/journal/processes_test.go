package journal

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/urmzd/humagen/fiber/pkg/event"

	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

// Two processes on one database, each with its own journal, read each
// other's events at once; runs appending through either at the same time are
// each kept whole; and an event both processes fold, as each observer does
// until they share the bus, is kept once.
func TestTwoProcessesAgree(t *testing.T) {
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		one, err := Open(a, 0)
		if err != nil {
			t.Fatal(err)
		}
		two, err := Open(b, 0)
		if err != nil {
			t.Fatal(err)
		}
		appended(t, one, ev(t, "acme", "r0", 0, event.Accepted{}))
		if got := events(t, two, "acme", "r0"); len(got) != 1 || got[0].Type != event.TypeAccepted {
			t.Fatalf("the other process reads %+v", got)
		}

		const runs, each = 6, 10
		var wg sync.WaitGroup
		for r := range runs {
			for _, j := range []*Journal{one, two} {
				wg.Go(func() {
					for i := range each {
						// Both processes fold every event of every run.
						if err := j.Append(ev(t, "acme", fmt.Sprintf("r%d", r+1), i, event.Text{Text: fmt.Sprint(i)}), "slack"); err != nil {
							t.Error(err)
						}
					}
				})
			}
		}
		wg.Wait()
		for r := range runs {
			for _, j := range []*Journal{one, two} {
				got := events(t, j, "acme", fmt.Sprintf("r%d", r+1))
				if len(got) != each {
					t.Fatalf("run %d holds %d events, want %d", r+1, len(got), each)
				}
				for i, e := range got {
					if e.Seq != uint64(i) {
						t.Fatalf("run %d event %d = %+v", r+1, i, e)
					}
				}
			}
		}
		if n := kept(t, a, "acme"); n != runs*each+1 {
			t.Fatalf("acme holds %d events, want %d", n, runs*each+1)
		}
		if got := query(t, two, Query{Tenant: "acme", Limit: MaxLimit}); len(got) != runs*each+1 {
			t.Fatalf("a query reads %d events", len(got))
		}
		if got := query(t, one, Query{Tenant: "globex"}); len(got) != 0 {
			t.Fatalf("another tenant reads %+v", got)
		}
	})
}

// The ring kept by an older release, one for every tenant, is moved into
// each tenant's events once, though two processes open the journal at the
// same time.
func TestTwoProcessesConvertOnce(t *testing.T) {
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		old := state.NewTable[stored](a, namespace)
		const capacity = 600
		bt := old.Batch()
		seq := int64(0)
		put := func(e event.Envelope) {
			seq++
			raw := mustJSON(t, e)
			bt.Put("", slotKey(seq, capacity), stored{Seq: seq, Event: raw, Origin: "slack"})
		}
		for i := range 450 {
			put(ev(t, "acme", fmt.Sprintf("r%d", i%3), i, event.Text{Text: fmt.Sprint(i)}))
		}
		for i := range 20 {
			put(ev(t, "globex", "g", i, event.Text{Text: fmt.Sprint(i)}))
		}
		// A redelivered event was kept twice by the ring.
		put(ev(t, "globex", "g", 3, event.Text{Text: "3"}))
		bt.Put("", capacityKey, stored{Capacity: capacity})
		if err := bt.Commit(); err != nil {
			t.Fatal(err)
		}

		var wg sync.WaitGroup
		journals := make([]*Journal, 4)
		for i := range journals {
			wg.Go(func() {
				j, err := Open([]state.Store{a, b}[i%2], 0)
				if err != nil {
					t.Error(err)
				}
				journals[i] = j
			})
		}
		wg.Wait()
		if t.Failed() {
			return
		}
		for r := range 3 {
			got := events(t, journals[r], "acme", fmt.Sprintf("r%d", r))
			if len(got) != 150 {
				t.Fatalf("r%d holds %d events after converting, want 150", r, len(got))
			}
		}
		if got := events(t, journals[3], "globex", "g"); len(got) != 20 {
			t.Fatalf("globex holds %d events, want 20", len(got))
		}
		if kept(t, a, "acme") != 450 || kept(t, b, "globex") != 20 {
			t.Fatal("the counts do not match what is kept")
		}
		recs, err := a.Load(t.Context(), namespace)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range recs {
			if r.Tenant == "" && (strings.HasPrefix(r.Key, slotPrefix) || r.Key == capacityKey) {
				t.Fatalf("%q is left from the old ring", r.Key)
			}
		}
		// The journal goes on from what was moved.
		appended(t, journals[1], ev(t, "acme", "r0", 1000, event.Completed{Text: "done"}))
		if got := query(t, journals[2], Query{Tenant: "acme", Limit: 1}); len(got) != 1 || got[0].Seq != 1000 {
			t.Fatalf("newest = %+v", got)
		}
	})
}

// slotKey is where a release before the layout by tenant kept an event.
func slotKey(seq int64, capacity int) string { return slotPrefix + state.Seq(seq%int64(capacity)) }

func mustJSON(t *testing.T, e event.Envelope) []byte {
	t.Helper()
	raw, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
