package sessionstate_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/urmzd/humagen/internal/wire/sessionstate"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
	"github.com/urmzd/saige/flow/session"
)

func TestRunsUseBindingOrderAcrossReplicasAndRestart(t *testing.T) {
	statetest.Processes(t, func(t *testing.T, first, second state.Store) {
		for _, ids := range [][]string{{"run-a", "run-b", "run-c"}, {"run-z", "run-y", "run-x"}} {
			t.Run(ids[0], func(t *testing.T) {
				a, b := sessionstate.New(first), sessionstate.New(second)
				r, err := a.Open(t.Context(), "one", session.Open{Parent: "console://" + ids[0], Agent: "helper"})
				if err != nil {
					t.Fatal(err)
				}
				for i, id := range ids {
					writer := a
					if i%2 == 1 {
						writer = b
					}
					if err := writer.BindRun(t.Context(), "one", r.ID, id); err != nil {
						t.Fatal(err)
					}
				}
				for _, id := range ids {
					if err := b.BindRun(t.Context(), "one", r.ID, id); err != nil {
						t.Fatal(err)
					}
				}
				for _, reader := range []*sessionstate.Store{a, b, sessionstate.New(second)} {
					for _, limit := range []int{1, 2, session.MaxPage, 0, session.MaxPage + 1} {
						want := ids
						if limit == 1 || limit == 2 {
							want = ids[len(ids)-limit:]
						}
						got, err := reader.Runs(t.Context(), "one", r.ID, limit)
						if err != nil || !slices.Equal(got, want) {
							t.Fatalf("limit %d: got %v, want %v, err=%v", limit, got, want, err)
						}
					}
				}
				if _, err := b.Runs(t.Context(), "another", r.ID, 1); !errors.Is(err, session.ErrNotFound) {
					t.Fatalf("cross-tenant runs: %v", err)
				}
			})
		}
	})
}

func TestConcurrentRunBindingsRemainUniqueAndKeepTheirLatestWindow(t *testing.T) {
	statetest.Processes(t, func(t *testing.T, first, second state.Store) {
		a, b := sessionstate.New(first), sessionstate.New(second)
		r, err := a.Open(t.Context(), "one", session.Open{Parent: "console://concurrent", Agent: "helper"})
		if err != nil {
			t.Fatal(err)
		}
		const total = 32
		var wg sync.WaitGroup
		for i := range total * 3 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				writer := a
				if i%2 == 1 {
					writer = b
				}
				if err := writer.BindRun(t.Context(), "one", r.ID, fmt.Sprintf("run-%03d", i%total)); err != nil {
					t.Errorf("bind: %v", err)
				}
			}()
		}
		wg.Wait()
		all, err := a.Runs(t.Context(), "one", r.ID, session.MaxPage)
		if err != nil || len(all) != total {
			t.Fatalf("lost or repeated binding: %v err=%v", all, err)
		}
		seen := map[string]bool{}
		for _, id := range all {
			if seen[id] {
				t.Fatalf("duplicate binding: %s", id)
			}
			seen[id] = true
		}
		for i := range total {
			if !seen[fmt.Sprintf("run-%03d", i)] {
				t.Fatalf("missing run %d", i)
			}
		}
		for i := len(all) - 1; i >= 0; i-- {
			if err := b.BindRun(t.Context(), "one", r.ID, all[i]); err != nil {
				t.Fatal(err)
			}
		}
		reopened := sessionstate.New(second)
		for _, limit := range []int{1, 5, total} {
			got, err := reopened.Runs(t.Context(), "one", r.ID, limit)
			if err != nil || !slices.Equal(got, all[len(all)-limit:]) {
				t.Fatalf("latest %d after restart/retries: got %v want %v err=%v", limit, got, all[len(all)-limit:], err)
			}
		}
	})
}

func TestSessionLinksKeepEachKindAndItsFirstPosition(t *testing.T) {
	statetest.Processes(t, func(t *testing.T, first, second state.Store) {
		a, b := sessionstate.New(first), sessionstate.New(second)
		r, err := a.Open(t.Context(), "one", session.Open{Parent: "console://links", Agent: "helper"})
		if err != nil {
			t.Fatal(err)
		}
		target, err := b.Open(t.Context(), "one", session.Open{Parent: "console://target", Agent: "helper"})
		if err != nil {
			t.Fatal(err)
		}
		for _, kind := range []string{"evidence", "context"} {
			if err := a.Link(t.Context(), "one", r.ID, session.Link{Target: target.ID, Kind: kind}); err != nil {
				t.Fatal(err)
			}
		}
		before, err := a.Links(t.Context(), "one", r.ID, 2)
		if err != nil || len(before) != 2 || before[0].Kind != "evidence" || before[1].Kind != "context" {
			t.Fatalf("distinct link kinds or insertion order lost: %+v err=%v", before, err)
		}
		var wg sync.WaitGroup
		for i := range 32 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				writer := a
				if i%2 == 1 {
					writer = b
				}
				if err := writer.Link(t.Context(), "one", r.ID, before[i%2]); err != nil {
					t.Errorf("duplicate link: %v", err)
				}
			}()
		}
		wg.Wait()
		reopened := sessionstate.New(second)
		after, err := reopened.Links(t.Context(), "one", r.ID, session.MaxPage)
		if err != nil || len(after) != 2 {
			t.Fatalf("links after restart: %+v err=%v", after, err)
		}
		for i := range before {
			if after[i].Target != target.ID || after[i].Kind != before[i].Kind || !after[i].CreatedAt.Equal(before[i].CreatedAt) {
				t.Fatalf("retry changed link: before=%+v after=%+v", before[i], after[i])
			}
		}
		limited, err := reopened.Links(t.Context(), "one", r.ID, 1)
		if err != nil || len(limited) != 1 || limited[0].Kind != "evidence" {
			t.Fatalf("oldest link window: %+v err=%v", limited, err)
		}
		if _, err := reopened.Links(t.Context(), "another", r.ID, 2); !errors.Is(err, session.ErrNotFound) {
			t.Fatalf("cross-tenant links: %v", err)
		}
	})
}

type failedBindingWrite struct {
	state.Store
	fail atomic.Bool
}

var errBindingWrite = errors.New("binding write unavailable")

func (s *failedBindingWrite) Write(ctx context.Context, namespace string, ops ...state.Op) error {
	if s.fail.Swap(false) {
		return errBindingWrite
	}
	return s.Store.Write(ctx, namespace, ops...)
}

func TestFailedBindingCommitDoesNotLeaveADuplicateMarker(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := &failedBindingWrite{Store: open(t)}
		s := sessionstate.New(st)
		r, err := s.Open(t.Context(), "one", session.Open{Parent: "console://failed", Agent: "helper"})
		if err != nil {
			t.Fatal(err)
		}
		st.fail.Store(true)
		if err := s.BindRun(t.Context(), "one", r.ID, "run-first"); !errors.Is(err, errBindingWrite) {
			t.Fatalf("failed commit: %v", err)
		}
		got, err := sessionstate.New(st).Runs(t.Context(), "one", r.ID, 1)
		if err != nil || len(got) != 0 {
			t.Fatalf("failed binding became visible: %v err=%v", got, err)
		}
		if err := s.BindRun(t.Context(), "one", r.ID, "run-first"); err != nil {
			t.Fatal(err)
		}
		got, err = sessionstate.New(st).Runs(t.Context(), "one", r.ID, 1)
		if err != nil || !slices.Equal(got, []string{"run-first"}) {
			t.Fatalf("retry lost behind a duplicate marker: %v err=%v", got, err)
		}
	})
}
