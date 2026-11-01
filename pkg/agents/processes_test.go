package agents_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/urmzd/humagen/fiber/pkg/definition"
	"github.com/urmzd/humagen/fiber/pkg/definition/definitiontest"
	"github.com/urmzd/humagen/pkg/agents"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

func open2(t *testing.T, a, b state.Store) (*agents.Store, *agents.Store) {
	t.Helper()
	one, err := agents.Open(a)
	if err != nil {
		t.Fatal(err)
	}
	two, err := agents.Open(b)
	if err != nil {
		t.Fatal(err)
	}
	return one, two
}

// Two processes on one database, each with its own store, see each other's
// versions and retirements at once, and versions written through both at
// the same time are each kept once: one writer wins each version, and the
// head counts every version.
func TestTwoProcessesAgree(t *testing.T) {
	ctx := context.Background()
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		one, two := open2(t, a, b)
		if _, err := one.Put(ctx, definitiontest.Record("acme", "docs-bot", "1.0.0", "One.")); err != nil {
			t.Fatal(err)
		}
		if got, err := two.Latest(ctx, "acme", "docs-bot"); err != nil || got.Version != "1.0.0" {
			t.Fatalf("the other process reads %+v, %v", got, err)
		}

		// Each version is written by both at once, with other content: one
		// wins, the other gets ErrExists.
		const n = 20
		var wg sync.WaitGroup
		var mu sync.Mutex
		won := map[string]int{}
		for v := 2; v <= n+1; v++ {
			for i, s := range []*agents.Store{one, two} {
				wg.Go(func() {
					version := fmt.Sprintf("%d.0.0", v)
					_, err := s.Put(ctx, definitiontest.Record("acme", "docs-bot", version, fmt.Sprintf("v%d from %d", v, i)))
					switch {
					case err == nil:
						mu.Lock()
						won[version]++
						mu.Unlock()
					case !errors.Is(err, definition.ErrExists):
						t.Error(err)
					}
				})
			}
		}
		wg.Wait()
		for v := 2; v <= n+1; v++ {
			if c := won[fmt.Sprintf("%d.0.0", v)]; c != 1 {
				t.Fatalf("version %d.0.0 written %d times", v, c)
			}
		}
		for _, s := range []*agents.Store{one, two} {
			all, err := s.Versions(ctx, "acme", "docs-bot")
			if err != nil || len(all) != n+1 {
				t.Fatalf("%d versions, %v; want %d", len(all), err, n+1)
			}
			latest, err := s.Latest(ctx, "acme", "docs-bot")
			if err != nil || latest.Version != fmt.Sprintf("%d.0.0", n+1) {
				t.Fatalf("latest = %s, %v", latest.Version, err)
			}
		}

		// Agents created through both at once are each listed.
		for i := range 10 {
			s := []*agents.Store{one, two}[i%2]
			wg.Go(func() {
				if _, err := s.Put(ctx, definitiontest.Record("acme", fmt.Sprintf("bot-%02d", i), "1.0.0", "x")); err != nil {
					t.Error(err)
				}
			})
		}
		wg.Wait()
		list, _, err := one.List(ctx, "acme", definition.Page{Size: 100})
		if err != nil || len(list) != 11 {
			t.Fatalf("list = %d, %v", len(list), err)
		}

		if err := two.Retire(ctx, "acme", "docs-bot", "ada"); err != nil {
			t.Fatal(err)
		}
		if _, err := one.Latest(ctx, "acme", "docs-bot"); !errors.Is(err, definition.ErrRetired) {
			t.Fatalf("latest after the other retired = %v", err)
		}
		if _, err := one.Put(ctx, definitiontest.Record("acme", "docs-bot", "99.0.0", "x")); !errors.Is(err, definition.ErrRetired) {
			t.Fatalf("put after the other retired = %v", err)
		}
		if err := one.Revive(ctx, "acme", "docs-bot"); err != nil {
			t.Fatal(err)
		}
		if _, ok, err := two.Retired("acme", "docs-bot"); err != nil || ok {
			t.Fatalf("retired after the other revived = %v, %v", ok, err)
		}
	})
}

// Agents and retirements kept before heads were are given heads once,
// though two processes open the store at the same time.
func TestTwoProcessesConvertOnce(t *testing.T) {
	ctx := context.Background()
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		versions := state.NewTable[definition.Record](a, agents.VersionsNamespace)
		retired := state.NewTable[agents.Retirement](a, agents.RetiredNamespace)
		// Records as an older release wrote them, through a store that
		// prepares them, then stripped of the heads it wrote.
		seed, err := agents.Open(state.NewMemory())
		if err != nil {
			t.Fatal(err)
		}
		for i := range 6 {
			for _, v := range []string{"1.0.0", "1.10.0", "1.2.0"} {
				rec, err := seed.Put(ctx, definitiontest.Record("acme", fmt.Sprintf("bot-%d", i), v, "v"+v))
				if err != nil {
					t.Fatal(err)
				}
				if err := versions.Put("acme", fmt.Sprintf("bot-%d@%s", i, v), rec); err != nil {
					t.Fatal(err)
				}
			}
		}
		if err := retired.Put("acme", "bot-0", agents.Retirement{By: "ada"}); err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		stores := make([]*agents.Store, 4)
		for i := range stores {
			wg.Go(func() {
				s, err := agents.Open([]state.Store{a, b}[i%2])
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
		for _, s := range stores {
			if _, err := s.Latest(ctx, "acme", "bot-0"); !errors.Is(err, definition.ErrRetired) {
				t.Fatalf("bot-0 latest = %v", err)
			}
			got, err := s.Latest(ctx, "acme", "bot-1")
			if err != nil || got.Version != "1.10.0" {
				t.Fatalf("bot-1 latest = %s, %v", got.Version, err)
			}
			list, _, err := s.List(ctx, "acme", definition.Page{Size: 2})
			if err != nil || len(list) != 2 || list[0].Name != "bot-1" {
				t.Fatalf("list = %+v, %v", list, err)
			}
		}
		// A new version follows on the converted head.
		if _, err := stores[1].Put(ctx, definitiontest.Record("acme", "bot-1", "2.0.0", "two")); err != nil {
			t.Fatal(err)
		}
		if got, err := stores[2].Latest(ctx, "acme", "bot-1"); err != nil || got.Version != "2.0.0" {
			t.Fatalf("latest after a put = %s, %v", got.Version, err)
		}
	})
}

// A deleted agent moved by two processes at once is retired once, and an
// agent revived after one move is not retired again by a move run later.
func TestImportRetiredRetiresOnlyWhatItCreates(t *testing.T) {
	ctx := context.Background()
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		one, two := open2(t, a, b)
		rec := definitiontest.Record("acme", "old-bot", "1.0.0", "Gone.")
		rec.CreatedAt = t0()
		var wg sync.WaitGroup
		for _, s := range []*agents.Store{one, two, one, two} {
			wg.Go(func() {
				if _, err := s.ImportRetired(ctx, []definition.Record{rec}, "migration"); err != nil {
					t.Error(err)
				}
			})
		}
		wg.Wait()
		if _, ok, err := one.Retired("acme", "old-bot"); err != nil || !ok {
			t.Fatalf("retired = %v, %v", ok, err)
		}
		if err := two.Revive(ctx, "acme", "old-bot"); err != nil {
			t.Fatal(err)
		}
		if _, err := one.ImportRetired(ctx, []definition.Record{rec}, "migration"); err != nil {
			t.Fatal(err)
		}
		if _, ok, err := two.Retired("acme", "old-bot"); err != nil || ok {
			t.Fatalf("a later move retired a revived agent: %v, %v", ok, err)
		}
	})
}

func t0() time.Time { return time.Date(2025, 6, 1, 9, 0, 0, 0, time.UTC) }
