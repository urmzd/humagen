package sessionstate_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/urmzd/humagen/internal/wire/sessionstate"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
	"github.com/urmzd/saige/flow/event"
	"github.com/urmzd/saige/flow/session"
	"github.com/urmzd/saige/flow/thread"
	"github.com/urmzd/saige/flow/thread/threadtest"
)

func TestJournalConformance(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		threadtest.Run(t, func(t *testing.T, _ ...thread.Option) thread.Store { return sessionstate.NewJournal(open(t)) })
	})
}

func TestUsageAndBudgetReceiptsSurviveDuplicateLateChildren(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		testUsageAndBudgetReceiptsSurviveDuplicateLateChildren(t, open(t))
	})
}
func testUsageAndBudgetReceiptsSurviveDuplicateLateChildren(t *testing.T, st state.Store) {
	ctx := context.Background()
	a, b := sessionstate.New(st), sessionstate.New(st)
	parent, err := a.Open(ctx, "acme", session.Open{Parent: "console://budget", Agent: "lead"})
	if err != nil {
		t.Fatal(err)
	}
	child, err := a.Open(ctx, "acme", session.Open{Parent: parent.ID, Agent: "helper"})
	if err != nil {
		t.Fatal(err)
	}
	usage := event.UsageCall{AccountingID: "child-call", Requests: 1, PromptTokens: 12, CompletionTokens: 8, Cost: &event.Cost{Micros: 42, Currency: "USD"}}
	for _, s := range []*sessionstate.Store{a, b} {
		if err := s.RecordUsage(ctx, "acme", child.ID, usage); err != nil {
			t.Fatal(err)
		}
	}
	total, err := b.Usage(ctx, "acme", parent.ID)
	if err != nil || total.Total.ModelCalls != 1 || total.Children.PromptTokens != 12 || total.Total.Cost.Micros != 42 {
		t.Fatalf("duplicate rollup: %+v %v", total, err)
	}
	if err := a.OpenBudget(ctx, "acme", "run", session.Budget{CostMicros: 100, Requests: 2}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ReserveBudget(ctx, "acme", "run", "child", session.Budget{CostMicros: 80, Requests: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.ReserveBudget(ctx, "acme", "run", "over", session.Budget{CostMicros: 30, Requests: 1}); !errors.Is(err, session.ErrBudget) {
		t.Fatalf("overspend accepted: %v", err)
	}
	if err := a.CloseBudget(ctx, "acme", "run"); err != nil {
		t.Fatal(err)
	}
	if err := b.SettleBudget(ctx, "acme", "run", "child", session.Budget{CostMicros: 42, Requests: 1}); err != nil {
		t.Fatal(err)
	}
	account, err := a.BudgetAccount(ctx, "acme", "run")
	if err != nil || !account.Ready() || account.Spent().CostMicros != 42 {
		t.Fatalf("late receipt: %+v %v", account, err)
	}
	if err := b.SettleBudget(ctx, "acme", "run", "child", session.Budget{CostMicros: 43, Requests: 1}); !errors.Is(err, session.ErrInvalid) {
		t.Fatalf("rewritten receipt: %v", err)
	}
}
func TestSessionIdentityAndAdmissionAcrossReplicas(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		testSessionIdentityAndAdmissionAcrossReplicas(t, open(t))
	})
}
func testSessionIdentityAndAdmissionAcrossReplicas(t *testing.T, st state.Store) {
	ctx := context.Background()
	a, b := sessionstate.New(st), sessionstate.New(st)
	parent, err := a.Open(ctx, "one", session.Open{Parent: "console://test", Agent: "lead"})
	if err != nil {
		t.Fatal(err)
	}
	again, err := b.Open(ctx, "one", session.Open{Parent: "console://test", Agent: "lead"})
	if err != nil || again.ID != parent.ID {
		t.Fatalf("continuation: %+v %v", again, err)
	}
	child, err := a.Open(ctx, "one", session.Open{Parent: parent.ID, Agent: "helper"})
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := b.Open(ctx, "one", session.Open{Parent: parent.ID, Agent: "helper", Fresh: true})
	if err != nil || fresh.ID == child.ID {
		t.Fatalf("fresh: %+v %v", fresh, err)
	}
	if _, err := a.Open(ctx, "two", session.Open{Parent: parent.ID, Agent: "helper"}); !errors.Is(err, session.ErrNotFound) {
		t.Fatalf("cross tenant parent: %v", err)
	}
	var wg sync.WaitGroup
	var accepted atomic.Int64
	for i := range 24 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s := a
			if i%2 == 0 {
				s = b
			}
			err := s.Reserve(ctx, "one", parent.ID, fmt.Sprint(i), child.ID, 8)
			if err == nil {
				accepted.Add(1)
			} else if !errors.Is(err, session.ErrLimit) {
				t.Errorf("reserve: %v", err)
			}
		}()
	}
	wg.Wait()
	if accepted.Load() != 8 {
		t.Fatalf("accepted %d, want 8", accepted.Load())
	}
	rows, err := b.Reservations(ctx, "one", parent.ID)
	if err != nil || len(rows) != 8 {
		t.Fatalf("persisted reservations: %+v %v", rows, err)
	}
	if err := a.BindReservation(ctx, "one", parent.ID, rows[0].Call, "run-one"); err != nil {
		t.Fatal(err)
	}
	if err := b.Reserve(ctx, "one", parent.ID, rows[0].Call, child.ID, 8); err != nil {
		t.Fatalf("retry at ceiling: %v", err)
	}
	if err := b.Release(ctx, "one", parent.ID, rows[0].Call); err != nil {
		t.Fatal(err)
	}
	if err := a.Reserve(ctx, "one", parent.ID, "replacement", fresh.ID, 8); err != nil {
		t.Fatal(err)
	}
	if err := a.Link(ctx, "one", child.ID, session.Link{Target: fresh.ID, Kind: "evidence"}); err != nil {
		t.Fatal(err)
	}
	if allowed, err := b.MayRead(ctx, "one", fresh.ID, child.ID); err != nil || allowed {
		t.Fatalf("link granted read: %v %v", allowed, err)
	}
	if err := a.GrantRead(ctx, "one", fresh.ID, child.ID); err != nil {
		t.Fatal(err)
	}
	if allowed, err := b.MayRead(ctx, "one", fresh.ID, child.ID); err != nil || !allowed {
		t.Fatalf("grant lost: %v %v", allowed, err)
	}
}
