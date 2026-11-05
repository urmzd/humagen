package runs

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
	"github.com/urmzd/orchagent/flow/event"
)

func TestCompletionSurvivesCrashAndFencesOldOwner(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		one, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		record(t, one, "acme", "r1", "lead", "api", 0, nil)
		r, _ := end(t, one, "acme", "r1", time.Second, event.TypeCompleted, wire.Lifecycle{Text: "ok"})
		// The process dies immediately after the terminal state transaction.
		two, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		pending, err := two.PendingCompletions()
		if err != nil || len(pending) != 1 || pending[0].ID != r.ID {
			t.Fatalf("pending = %+v, %v", pending, err)
		}
		old, err := one.ClaimCompletion(t.Context(), r, t0)
		if err != nil || old == nil {
			t.Fatalf("claim: %v", err)
		}
		if c, err := two.ClaimCompletion(t.Context(), pending[0], t0); c != nil || err != nil {
			t.Fatalf("concurrent claim = %v, %v", c, err)
		}
		if err := old.Ack(t.Context(), "usage"); err != nil {
			t.Fatal(err)
		}
		// Crash after one acknowledgement. The next process resumes only the rest.
		next, err := two.ClaimCompletion(t.Context(), pending[0], t0.Add(3*time.Minute))
		if err != nil || next == nil || !next.Done("usage") {
			t.Fatalf("resume: %+v, %v", next, err)
		}
		if err := old.Ack(t.Context(), "answer"); err == nil {
			t.Fatal("stale owner acknowledged work")
		}
		if err := old.Finish(t.Context()); err == nil {
			t.Fatal("stale owner removed work")
		}
		if err := next.Finish(t.Context()); err != nil {
			t.Fatal(err)
		}
		pending, err = one.PendingCompletions()
		if err != nil || len(pending) != 0 {
			t.Fatalf("finished = %v, %v", pending, err)
		}
		if _, again := end(t, one, "acme", "r1", time.Second, event.TypeCompleted, wire.Lifecycle{Text: "ok"}); again {
			t.Fatal("duplicate event reopened completion")
		}
	})
}

func TestCompletionFailureKeepsPendingWork(t *testing.T) {
	faulty := statetest.NewFaulty(state.NewMemory())
	x, _ := Open(faulty)
	record(t, x, "acme", "r1", "lead", "api", 0, nil)
	faulty.Fail(statetest.ErrInjected)
	end(t, x, "acme", "r1", time.Second, event.TypeCompleted, wire.Lifecycle{})
	if pending, err := x.PendingCompletions(); err != nil || len(pending) != 0 {
		t.Fatalf("failed ending created pending work: %v %v", pending, err)
	}
	faulty.Fail(nil)
	r, _ := end(t, x, "acme", "r1", time.Second, event.TypeCompleted, wire.Lifecycle{})
	c, err := x.ClaimCompletion(t.Context(), r, t0)
	if err != nil {
		t.Fatal(err)
	}
	faulty.Fail(statetest.ErrInjected)
	if err := c.Ack(t.Context(), "answer"); !errors.Is(err, statetest.ErrInjected) {
		t.Fatal(err)
	}
	faulty.Fail(nil)
	if err := c.Release(t.Context()); err != nil {
		t.Fatal(err)
	}
	c, err = x.ClaimCompletion(t.Context(), r, t0)
	if err != nil || c.Done("answer") {
		t.Fatalf("failed action acknowledged: %v", err)
	}
}

func TestCompletionPagesPastFailingOldWork(t *testing.T) {
	x := NewIndex()
	for i := range 101 {
		id := fmt.Sprintf("r%d", i)
		record(t, x, "acme", id, "lead", "api", 0, nil)
		end(t, x, "acme", id, time.Second, event.TypeCompleted, wire.Lifecycle{})
	}
	first, err := x.PendingCompletions()
	if err != nil || len(first) != 100 {
		t.Fatalf("first = %d, %v", len(first), err)
	}
	// None was acknowledged; the next tick must still reach the new run.
	second, err := x.PendingCompletions()
	if err != nil || len(second) != 1 || second[0].ID != "r100" {
		t.Fatalf("second = %v, %v", second, err)
	}
	again, err := x.PendingCompletions()
	if err != nil || len(again) != 100 {
		t.Fatalf("wrapped = %d, %v", len(again), err)
	}
}
