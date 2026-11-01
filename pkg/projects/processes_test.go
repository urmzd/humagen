package projects_test

import (
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"

	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

// two opens the store once on each of two processes' state stores.
func two(t *testing.T, a, b state.Store, opts ...projects.Option) (*projects.Store, *projects.Store) {
	t.Helper()
	one, err := projects.Open(a, opts...)
	if err != nil {
		t.Fatal(err)
	}
	other, err := projects.Open(b, opts...)
	if err != nil {
		t.Fatal(err)
	}
	return one, other
}

// together runs each function at once, after all of them have started.
func together(fns ...func()) {
	var ready, done sync.WaitGroup
	start := make(chan struct{})
	for _, fn := range fns {
		ready.Add(1)
		done.Go(func() {
			ready.Done()
			<-start
			fn()
		})
	}
	ready.Wait()
	close(start)
	done.Wait()
}

// Two processes on one database, each with its own store, see each other's
// writes at once, and writes to one tenant from both at the same time are
// each kept, with one history and no sequence number twice.
func TestTwoProcessesAgree(t *testing.T) {
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		one, other := two(t, a, b)
		mustCreate(t, one, "acme", team("Field", ""), ada)
		if p, err := other.Get("acme", "Field"); err != nil || p.Version != 1 {
			t.Fatalf("the other process reads %+v, %v", p, err)
		}
		if _, err := other.Update("acme", "Field", 1, ada, t0, func(p *projects.Project) error { p.Title = "Field team"; return nil }); err != nil {
			t.Fatal(err)
		}
		if p, err := one.Get("acme", "Field"); err != nil || p.Title != "Field team" || p.Version != 2 {
			t.Fatalf("the first process reads %+v, %v", p, err)
		}

		const n = 16
		var fns []func()
		for i := range n {
			s := []*projects.Store{one, other}[i%2]
			fns = append(fns, func() {
				if _, err := s.Create("acme", team(fmt.Sprintf("Team %02d", i), ""), ada, t0); err != nil {
					t.Error(err)
				}
				if err := s.SetHome("acme", fmt.Sprintf("agent-%02d", i), projects.Root, admin, t0, nil); err != nil {
					t.Error(err)
				}
			})
		}
		together(fns...)
		for _, s := range []*projects.Store{one, other} {
			if got := s.List("acme"); len(got) != n+1 {
				t.Fatalf("%d projects, want %d", len(got), n+1)
			}
			tree, err := s.Read("acme")
			if err != nil {
				t.Fatal(err)
			}
			for i := range n {
				if _, ok := tree.Home(fmt.Sprintf("agent-%02d", i)); !ok {
					t.Fatalf("agent-%02d's home was lost", i)
				}
			}
			changes, err := s.RecentHistory("acme")
			if err != nil {
				t.Fatal(err)
			}
			seen := map[int64]bool{}
			for _, c := range changes {
				if seen[c.Seq] {
					t.Fatalf("the change %d was numbered twice", c.Seq)
				}
				seen[c.Seq] = true
			}
			// The root, the two first changes and two per writer.
			if len(changes) != 3+2*n {
				t.Fatalf("%d changes, want %d", len(changes), 3+2*n)
			}
		}
		var tenants []string
		if err := other.EachTenant(func(tenant string) error { tenants = append(tenants, tenant); return nil }); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(tenants, []string{"acme"}) {
			t.Fatalf("tenants = %v", tenants)
		}
	})
}

// Two processes moving two projects under each other at once cannot together
// make a cycle: one move lands, the other is checked against it and refused,
// and the tree both read is one.
func TestTwoProcessesCannotMakeACycle(t *testing.T) {
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		one, other := two(t, a, b)
		for round := range 8 {
			x, y := fmt.Sprintf("X%d", round), fmt.Sprintf("Y%d", round)
			mustCreate(t, one, "acme", team(x, ""), ada)
			mustCreate(t, one, "acme", team(y, ""), ada)
			var errX, errY error
			together(
				func() { _, errX = one.Move("acme", x, 1, y, ada, t0, nil) },
				func() { _, errY = other.Move("acme", y, 1, x, ada, t0, nil) },
			)
			if (errX == nil) == (errY == nil) {
				t.Fatalf("round %d: moves = %v, %v; want one to land", round, errX, errY)
			}
			if lost := cmpErr(errX, errY); !errors.Is(lost, projects.ErrInvalid) {
				t.Fatalf("round %d: the refused move = %v, want ErrInvalid", round, lost)
			}
			for _, s := range []*projects.Store{one, other} {
				tree, err := s.Read("acme")
				if err != nil {
					t.Fatalf("round %d: the tree is not one: %v", round, err)
				}
				px, _ := tree.Get(x)
				py, _ := tree.Get(y)
				if (px.Parent == y) == (py.Parent == x) {
					t.Fatalf("round %d: %s under %q, %s under %q", round, x, px.Parent, y, py.Parent)
				}
			}
		}
	})
}

// cmpErr is whichever of two errors is not nil.
func cmpErr(a, b error) error {
	if a != nil {
		return a
	}
	return b
}

// Two processes creating one name at once, in two cases, keep it once, and
// creating past the cap at once keep no more than it.
func TestTwoProcessesKeepNamesAndCaps(t *testing.T) {
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		one, other := two(t, a, b, projects.WithCaps(projects.Caps{Projects: 6}))
		mustCreate(t, one, "acme", team("Field", ""), ada)
		for round := range 6 {
			upper, lower := fmt.Sprintf("Ops%d", round), fmt.Sprintf("ops%d", round)
			var errA, errB error
			together(
				func() { _, errA = one.Create("globex", team(upper, ""), ada, t0) },
				func() { _, errB = other.Create("globex", team(lower, ""), ada, t0) },
			)
			if (errA == nil) == (errB == nil) || !errors.Is(cmpErr(errA, errB), projects.ErrExists) {
				t.Fatalf("round %d: creates = %v, %v; want one to land and one ErrExists", round, errA, errB)
			}
		}
		var made, limited sync.Map
		var fns []func()
		for i := range 12 {
			s := []*projects.Store{one, other}[i%2]
			fns = append(fns, func() {
				_, err := s.Create("acme", team(fmt.Sprintf("Team %02d", i), "Field"), ada, t0)
				switch {
				case err == nil:
					made.Store(i, true)
				case errors.Is(err, projects.ErrLimit):
					limited.Store(i, true)
				default:
					t.Error(err)
				}
			})
		}
		together(fns...)
		count := func(m *sync.Map) (n int) {
			m.Range(func(_, _ any) bool { n++; return true })
			return n
		}
		got := len(other.List("acme"))
		if got != 1+count(&made) || count(&made)+count(&limited) != 12 {
			t.Fatalf("%d projects kept, %d made, %d refused", got, count(&made), count(&limited))
		}
		if tree, _ := one.Read("acme"); tree.Len() > 6 {
			t.Fatalf("the cap let %d projects in", tree.Len())
		}
	})
}

// Two processes importing a tenant's projects at once write it once.
func TestTwoProcessesImportOnce(t *testing.T) {
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		one, other := two(t, a, b)
		ps := []projects.Project{{Name: "Field"}, {Name: "Launch"}}
		var errA, errB error
		together(
			func() { errA = one.Import("acme", ps, nil, "migration", t0) },
			func() { errB = other.Import("acme", ps, nil, "migration", t0) },
		)
		if (errA == nil) == (errB == nil) || !errors.Is(cmpErr(errA, errB), projects.ErrExists) {
			t.Fatalf("imports = %v, %v; want one to land and one ErrExists", errA, errB)
		}
		for _, s := range []*projects.Store{one, other} {
			if got := s.List("acme"); len(got) != 2 {
				t.Fatalf("projects = %+v", got)
			}
			if h, _, _ := s.History("acme", "Field", 0, 0); len(h) != 1 {
				t.Fatalf("Field's history = %+v", h)
			}
		}
	})
}

// A delete in one process waits for nothing a hold in another does, but
// cannot land on the tree it read before the hold's filing: it checks again,
// and sees what was filed.
func TestAHoldFencesAnotherProcesssDelete(t *testing.T) {
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		one, other := two(t, a, b)
		mustCreate(t, one, "acme", team("Field", ""), ada)
		var filed sync.Map
		checked := make(chan struct{})
		held := make(chan struct{})
		var deleteErr error
		var wg sync.WaitGroup
		wg.Go(func() {
			first := true
			deleteErr = other.Delete("acme", "Field", ada, t0, func(projects.Project) []string {
				if first {
					// The first check reads nothing filed, and the hold
					// files and fences before the delete tries to land.
					first = false
					close(checked)
					<-held
				}
				if _, ok := filed.Load("Field"); ok {
					return []string{"a schedule"}
				}
				return nil
			})
		})
		<-checked
		if err := one.Hold("acme", "Field", func() error { filed.Store("Field", true); return nil }); err != nil {
			t.Fatal(err)
		}
		close(held)
		wg.Wait()
		if !errors.Is(deleteErr, projects.ErrInUse) {
			t.Fatalf("Delete = %v, want ErrInUse", deleteErr)
		}
		if _, err := one.Get("acme", "Field"); err != nil {
			t.Fatal(err)
		}
	})
}
