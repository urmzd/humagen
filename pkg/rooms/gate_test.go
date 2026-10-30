package rooms

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/urmzd/humagen/internal/legacy"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

var t0 = time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)

// backends are the counters every gate test runs on: memory, and Postgres
// when statetest.URLEnv names a database.
func backends(t *testing.T, run func(t *testing.T, counters func() Counters)) {
	t.Run("memory", func(t *testing.T) { run(t, func() Counters { return NewMemoryCounters() }) })
	t.Run("postgres", func(t *testing.T) {
		pool := statetest.Pool(t)
		run(t, func() Counters {
			c, err := OpenPostgres(t.Context(), pool)
			if err != nil {
				t.Fatal(err)
			}
			// Each gate its own keys: tests reuse tenants.
			_, _ = pool.Exec(t.Context(), `TRUNCATE humagen_room_rate, humagen_room_holds`)
			return c
		})
	})
}

func TestARoomStartsAtMostItsRateInAMinute(t *testing.T) {
	backends(t, func(t *testing.T, counters func() Counters) { roomRate(t, counters()) })
}

func roomRate(t *testing.T, c Counters) {
	g := NewGate(Config{RoomRunsPerMinute: 2}, c)
	ask := Ask{Tenant: "acme", Room: "th-1"}
	for i := range 2 {
		if err := g.Admit(ask, t0.Add(time.Duration(i)*time.Second), nil); err != nil {
			t.Fatal(err)
		}
	}
	err := g.Admit(ask, t0.Add(10*time.Second), nil)
	var r *Refused
	if !errors.As(err, &r) || r.Limit != LimitRoomRate || r.Max != 2 || r.RetryAfter != 50*time.Second {
		t.Fatalf("err = %#v", err)
	}
	if r.Error() != "this conversation has started 2 runs in the last minute, the most one conversation may; try again in 50s" {
		t.Fatalf("words = %q", r.Error())
	}
	// Another conversation, and another tenant's of the same id, have their own.
	if err := g.Admit(Ask{Tenant: "acme", Room: "th-2"}, t0.Add(10*time.Second), nil); err != nil {
		t.Fatal(err)
	}
	if err := g.Admit(Ask{Tenant: "globex", Room: "th-1"}, t0.Add(10*time.Second), nil); err != nil {
		t.Fatal(err)
	}
	// The window slides: a minute after the first, one place is free.
	if err := g.Admit(ask, t0.Add(time.Minute+time.Millisecond), nil); err != nil {
		t.Fatal(err)
	}
}

func TestAPersonHasAtMostTheirLimitInFlight(t *testing.T) {
	backends(t, func(t *testing.T, counters func() Counters) { personRuns(t, counters()) })
}

func personRuns(t *testing.T, c Counters) {
	g := NewGate(Config{PersonRuns: 2}, c)
	ended := map[string]bool{}
	gone := func(run string) bool { return ended[run] }
	for _, run := range []string{"r1", "r2"} {
		if err := g.Admit(Ask{Tenant: "acme", Person: "ana", Run: run}, t0, gone); err != nil {
			t.Fatal(err)
		}
	}
	// The same run asking again holds the place it has.
	if err := g.Admit(Ask{Tenant: "acme", Person: "ana", Run: "r2"}, t0, gone); err != nil {
		t.Fatal(err)
	}
	err := g.Admit(Ask{Tenant: "acme", Person: "ana", Run: "r3"}, t0, gone)
	var r *Refused
	if !errors.As(err, &r) || r.Limit != LimitPersonRuns || r.Error() != "you have 2 runs in flight, the most one person may; wait for one to finish" {
		t.Fatalf("err = %v", err)
	}
	// Someone else, and a run asked by nobody, are not held to Ana's.
	if err := g.Admit(Ask{Tenant: "acme", Person: "bo", Run: "r4"}, t0, gone); err != nil {
		t.Fatal(err)
	}
	if err := g.Admit(Ask{Tenant: "acme", Run: "r5"}, t0, gone); err != nil {
		t.Fatal(err)
	}
	// A run that ended gives its place back.
	if err := g.Release("acme", "ana", "r1"); err != nil {
		t.Fatal(err)
	}
	if err := g.Admit(Ask{Tenant: "acme", Person: "ana", Run: "r3"}, t0, gone); err != nil {
		t.Fatal(err)
	}
	// A run that ended without saying so is reclaimed at the limit.
	ended["r2"] = true
	if err := g.Admit(Ask{Tenant: "acme", Person: "ana", Run: "r6"}, t0, gone); err != nil {
		t.Fatal(err)
	}
}

// A run the room refuses gives back the person's place it took first.
func TestARoomRefusalFreesThePersonsPlace(t *testing.T) {
	backends(t, func(t *testing.T, counters func() Counters) { refusalFrees(t, counters()) })
}

func refusalFrees(t *testing.T, c Counters) {
	g := NewGate(Config{PersonRuns: 1, RoomRunsPerMinute: 1}, c)
	if err := g.Admit(Ask{Tenant: "acme", Room: "th-1", Person: "ana", Run: "r1"}, t0, nil); err != nil {
		t.Fatal(err)
	}
	_ = g.Release("acme", "ana", "r1")
	var r *Refused
	if err := g.Admit(Ask{Tenant: "acme", Room: "th-1", Person: "ana", Run: "r2"}, t0, nil); !errors.As(err, &r) || r.Limit != LimitRoomRate {
		t.Fatalf("err = %v", err)
	}
	if err := g.Admit(Ask{Tenant: "acme", Room: "th-2", Person: "ana", Run: "r3"}, t0, nil); err != nil {
		t.Fatalf("the refused run kept Ana's place: %v", err)
	}
}

// Racers in two processes, each its own pool on one database, ask for one
// person's places and one room's rate at once. Exactly the limits are let in.
func TestTheLimitsHoldAcrossProcesses(t *testing.T) {
	pool := statetest.Pool(t)
	var schema string
	if err := pool.QueryRow(t.Context(), `SELECT current_schema()`).Scan(&schema); err != nil {
		t.Fatal(err)
	}
	cfg, err := pgxpool.ParseConfig(legacy.Getenv(statetest.URLEnv))
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	other, err := pgxpool.NewWithConfig(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(other.Close)
	var gates []*Gate
	for _, p := range []*pgxpool.Pool{pool, other} {
		c, err := OpenPostgres(t.Context(), p)
		if err != nil {
			t.Fatal(err)
		}
		gates = append(gates, NewGate(Config{PersonRuns: 3, RoomRunsPerMinute: 5}, c))
	}
	var people, rooms atomic.Int64
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Go(func() {
			if gates[i%2].Admit(Ask{Tenant: "acme", Person: "ana", Run: string(rune('a' + i))}, t0, nil) == nil {
				people.Add(1)
			}
		})
		wg.Go(func() {
			if gates[i%2].Admit(Ask{Tenant: "acme", Room: "th-1"}, t0, nil) == nil {
				rooms.Add(1)
			}
		})
	}
	wg.Wait()
	if people.Load() != 3 || rooms.Load() != 5 {
		t.Fatalf("let in %d of a person's 3 and %d of a room's 5", people.Load(), rooms.Load())
	}
}
