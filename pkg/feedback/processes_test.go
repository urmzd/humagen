package feedback_test

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/feedback"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

// Two processes on one database, each with its own store, see each other's
// feedback at once, and people rating one run at the same time through
// either are each kept once, numbered without a gap or a repeat.
func TestTwoProcessesAgree(t *testing.T) {
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		one, err := feedback.Open(a)
		if err != nil {
			t.Fatal(err)
		}
		two, err := feedback.Open(b)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := one.Save("acme", feedback.Record{Run: "run", Subject: "ana", Rating: "up", Comment: "good"}); err != nil {
			t.Fatal(err)
		}
		got := list(t, two, "acme", "run", 0)
		if len(got) != 1 || got[0].Subject != "ana" || got[0].Comment != "good" {
			t.Fatalf("the other process reads %+v", got)
		}
		// Rating again through the other process keeps the comment and the
		// place.
		if r, err := two.Save("acme", feedback.Record{Run: "run", Subject: "ana", Rating: "up"}); err != nil || r.Comment != "good" || r.Seq != 1 {
			t.Fatalf("Save = %+v, %v", r, err)
		}

		const people = 24
		var wg sync.WaitGroup
		for i := range people {
			s := []*feedback.Store{one, two}[i%2]
			wg.Go(func() {
				who := fmt.Sprintf("p%02d", i)
				for _, rating := range []string{"down", "up"} {
					if _, err := s.Save("acme", feedback.Record{Run: "run", Subject: who, Rating: rating, Comment: who}); err != nil {
						t.Error(err)
					}
				}
			})
		}
		wg.Wait()
		for _, s := range []*feedback.Store{one, two} {
			got := list(t, s, "acme", "run", 0)
			if len(got) != people+1 {
				t.Fatalf("%d records, want %d", len(got), people+1)
			}
			seen := map[string]bool{}
			for i, r := range got {
				if r.Seq != int64(i+1) || seen[r.Subject] {
					t.Fatalf("record %d = %+v", i, r)
				}
				seen[r.Subject] = true
				if r.Subject != "ana" && (r.Rating != "up" || r.Comment != r.Subject) {
					t.Fatalf("lost a rating: %+v", r)
				}
			}
		}
		if got := list(t, two, "other", "run", 0); len(got) != 0 {
			t.Fatalf("another tenant reads %+v", got)
		}
	})
}

// Records kept one per key by an older release are moved once, though two
// processes open the store at the same time.
func TestTwoProcessesConvertOnce(t *testing.T) {
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		old := state.NewTable[feedback.Record](a, "feedback")
		for i := range 30 {
			r := feedback.Record{Run: fmt.Sprintf("run-%d", i%3), Subject: fmt.Sprintf("p%02d", i), Rating: "up", UpdatedAt: time.Date(2026, 1, 1, 0, i, 0, 0, time.UTC)}
			if i%2 == 0 {
				r.Seq = int64(100 + i)
			}
			if err := old.Put("acme", fmt.Sprintf("%064x", i), r); err != nil {
				t.Fatal(err)
			}
		}
		var wg sync.WaitGroup
		stores := make([]*feedback.Store, 4)
		for i := range stores {
			wg.Go(func() {
				s, err := feedback.Open([]state.Store{a, b}[i%2])
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
		total := 0
		for run := range 3 {
			got := list(t, stores[0], "acme", fmt.Sprintf("run-%d", run), 0)
			total += len(got)
			for i := 1; i < len(got); i++ {
				if got[i].Seq <= got[i-1].Seq {
					t.Fatalf("run-%d out of order: %+v", run, got)
				}
			}
			// Numbered records keep their number, and come first.
			if got[0].Seq < 100 {
				t.Fatalf("run-%d: %+v", run, got[0])
			}
		}
		if total != 30 {
			t.Fatalf("%d records after converting, want 30", total)
		}
		left := 0
		recs, err := a.Load(t.Context(), "feedback")
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range recs {
			if len(r.Key) == 64 {
				left++
			}
		}
		if left != 0 {
			t.Fatalf("%d records left under old keys", left)
		}
		// A new rating follows the run's last number.
		r, err := stores[1].Save("acme", feedback.Record{Run: "run-0", Subject: "new", Rating: "down"})
		if err != nil {
			t.Fatal(err)
		}
		got := list(t, stores[2], "acme", "run-0", 0)
		if got[len(got)-1] != r {
			t.Fatalf("the new rating is not last: %+v", got[len(got)-1])
		}
	})
}
