package runs

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

// A run turns into a review once, on its own record: the process that did
// not make the change reads it, a second call from either process changes
// nothing, and an index opened later on the same store still reads it.
func TestARunStartsAReviewOnceAcrossProcesses(t *testing.T) {
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		one, other := two(t, a, b)
		record(t, one, "acme", "r1", "lead", "github", 0, nil)
		if r, _ := other.Get("acme", "r1"); r.StartedReview() {
			t.Fatal("a new run has started a review")
		}
		if started, err := one.StartReview("acme", "r1"); err != nil || !started {
			t.Fatalf("StartReview = %v, %v", started, err)
		}
		for name, x := range map[string]*Index{"the same process": one, "another process": other} {
			if r, err := x.Get("acme", "r1"); err != nil || !r.StartedReview() {
				t.Fatalf("%s reads %v, %v", name, r.StartedReview(), err)
			}
			if started, err := x.StartReview("acme", "r1"); err != nil || started {
				t.Fatalf("a second StartReview from %s = %v, %v", name, started, err)
			}
		}
		// What the run does next keeps it.
		if _, ok := other.AskStop("acme", "r1", t0.Add(time.Second)); !ok {
			t.Fatal("AskStop failed")
		}
		restarted, err := Open(b)
		if err != nil {
			t.Fatal(err)
		}
		if r, err := restarted.Get("acme", "r1"); err != nil || !r.StartedReview() || !r.Stopping {
			t.Fatalf("after a restart the run reads %+v, %v", r, err)
		}
		if started, err := restarted.StartReview("acme", "r1"); err != nil || started {
			t.Fatalf("a second StartReview after a restart = %v, %v", started, err)
		}

		if started, err := one.StartReview("acme", "nowhere"); !errors.Is(err, ErrNotFound) || started {
			t.Fatalf("StartReview for no run = %v, %v", started, err)
		}
		if started, err := one.StartReview("globex", "r1"); !errors.Is(err, ErrNotFound) || started {
			t.Fatalf("StartReview from another tenant = %v, %v", started, err)
		}
	})
}

// Of the calls two processes make at once for one run, exactly one starts
// the review.
func TestConcurrentStartReviewCallsStartOneReview(t *testing.T) {
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		one, other := two(t, a, b)
		for _, id := range []string{"r1", "r2", "r3", "r4", "r5"} {
			record(t, one, "acme", id, "lead", "github", 0, nil)
			var started atomic.Int32
			var wg sync.WaitGroup
			begin := make(chan struct{})
			for i := range 8 {
				x := one
				if i%2 == 1 {
					x = other
				}
				wg.Add(1)
				go func() {
					defer wg.Done()
					<-begin
					ok, err := x.StartReview("acme", id)
					if err != nil {
						t.Errorf("StartReview: %v", err)
					}
					if ok {
						started.Add(1)
					}
				}()
			}
			close(begin)
			wg.Wait()
			if got := started.Load(); got != 1 {
				t.Fatalf("%s: %d calls started the review, want one", id, got)
			}
			if r, _ := other.Get("acme", id); !r.StartedReview() {
				t.Fatalf("%s did not start a review", id)
			}
		}
	})
}
