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

// A conversation is adopted as a session under its own id: its record is
// kept under that id with the key it would open, and opening that key
// finds it, never a second session. Adopting it again writes nothing, and
// an id or a key that is taken is refused.
func TestAdoptRecordsASessionUnderAConversationsID(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		ctx := context.Background()
		st := open(t)
		s := sessionstate.New(st)
		const id, place = "th-00112233445566aa", "console://th-00112233445566aa"
		rec, err := s.Adopt(ctx, "acme", id, session.Open{Parent: place, Agent: "Lead"})
		if err != nil {
			t.Fatal(err)
		}
		key := session.Key{Tenant: "acme", Parent: place, Agent: "lead"}.Normalize()
		if rec.ID != id || rec.Key != key || rec.Channel == "" {
			t.Fatalf("adopted = %+v, want %s under %+v", rec, id, key)
		}
		if got, err := s.Get(ctx, "acme", id); err != nil || got.ID != id || got.Key != key {
			t.Fatalf("read back = %+v, %v", got, err)
		}
		if _, err := s.Get(ctx, "acme", key.ID()); !errors.Is(err, session.ErrNotFound) {
			t.Fatalf("the key's own id holds a session: %v", err)
		}
		if got, err := s.Open(ctx, "acme", session.Open{Parent: place, Agent: "lead"}); err != nil || got.ID != id {
			t.Fatalf("opening its key = %s, %v; want %s", got.ID, err, id)
		}
		// Another tenant's key and id are its own.
		if _, err := s.Get(ctx, "globex", id); !errors.Is(err, session.ErrNotFound) {
			t.Fatalf("another tenant reads it: %v", err)
		}
		if got, err := s.Open(ctx, "globex", session.Open{Parent: place, Agent: "lead"}); err != nil || got.ID == id {
			t.Fatalf("another tenant opening the key = %s, %v", got.ID, err)
		}
		before, err := st.Load(ctx, "agent-sessions")
		if err != nil {
			t.Fatal(err)
		}
		if again, err := s.Adopt(ctx, "acme", id, session.Open{Parent: place, Agent: "lead"}); err != nil || again.ID != id || !again.CreatedAt.Equal(rec.CreatedAt) {
			t.Fatalf("adopted again = %+v, %v", again, err)
		}
		if after, err := st.Load(ctx, "agent-sessions"); err != nil || fmt.Sprint(after) != fmt.Sprint(before) {
			t.Fatalf("adopting again wrote to the store: %v", err)
		}
		// A child opens under it as under any session.
		child, err := s.Open(ctx, "acme", session.Open{Parent: id, Agent: "helper"})
		if err != nil || child.Key.Parent != id {
			t.Fatalf("a child of it = %+v, %v", child, err)
		}

		for name, tc := range map[string]struct {
			id string
			o  session.Open
		}{
			"another key for its id":   {id, session.Open{Parent: place, Agent: "helper"}},
			"a key's id":               {key.ID(), session.Open{Parent: "console://other", Agent: "lead"}},
			"a place as an id":         {"console://x", session.Open{Parent: "console://x", Agent: "lead"}},
			"no id":                    {"", session.Open{Parent: place, Agent: "lead"}},
			"a child":                  {"th-00112233445566bb", session.Open{Parent: id, Agent: "lead"}},
			"a label":                  {"th-00112233445566bb", session.Open{Parent: "console://th-00112233445566bb", Agent: "lead", Label: "x"}},
			"a key another id adopted": {"th-00112233445566bb", session.Open{Parent: place, Agent: "lead"}},
		} {
			if _, err := s.Adopt(ctx, "acme", tc.id, tc.o); !errors.Is(err, session.ErrInvalid) {
				t.Errorf("%s: adopt = %v", name, err)
			}
		}
		// A key a session was opened under already is not adopted over.
		opened, err := s.Open(ctx, "acme", session.Open{Parent: "console://th-00112233445566cc", Agent: "lead"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.Adopt(ctx, "acme", "th-00112233445566cc", session.Open{Parent: "console://th-00112233445566cc", Agent: "lead"}); !errors.Is(err, sessionstate.ErrAdopted) {
			t.Fatalf("adopting over session %s = %v", opened.ID, err)
		}
	})
}
