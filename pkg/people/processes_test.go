package people_test

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sync"
	"testing"

	"github.com/urmzd/humagen/pkg/people"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

// Two processes on one database, each with its own directory, read each
// other's people at once, and people named alike who sign in through both
// at the same time are each kept, each with a handle nobody else has.
func TestTwoProcessesAgree(t *testing.T) {
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		one, err := people.Open(a)
		if err != nil {
			t.Fatal(err)
		}
		two, err := people.Open(b)
		if err != nil {
			t.Fatal(err)
		}
		dana := mustSee(t, one, "acme", signIn("okta|0", "Dana Scully", "viewer"))
		if got, err := two.ByHandle("acme", "dana-scully"); err != nil || got.Subject != "okta|0" {
			t.Fatalf("the other process reads %+v, %v", got, err)
		}
		if got, err := two.Get("acme", "okta|0"); err != nil || got.Handle != dana.Handle {
			t.Fatalf("the other process reads %+v, %v", got, err)
		}

		const racers = 30
		var wg sync.WaitGroup
		for i := range racers {
			s := []*people.Store{one, two}[i%2]
			wg.Go(func() {
				if _, err := s.Seen("acme", signIn(fmt.Sprintf("okta|%d", i+1), "Dana Scully", "viewer"), t0); err != nil {
					t.Error(err)
				}
			})
		}
		// Someone known only by subject gets a name through each process at
		// once: one handle is made from it, and the old one let go.
		mustSee(t, one, "acme", people.Sighting{Subject: "okta|fox", Source: people.Slack})
		for _, s := range []*people.Store{one, two} {
			wg.Go(func() {
				if _, err := s.Seen("acme", signIn("okta|fox", "Fox Mulder", "viewer"), t0); err != nil {
					t.Error(err)
				}
			})
		}
		wg.Wait()

		for _, s := range []*people.Store{one, two} {
			var all []people.Person
			cursor := ""
			for {
				page, next, err := s.List("acme", people.Query{Before: cursor})
				if err != nil {
					t.Fatal(err)
				}
				all = append(all, page...)
				if next == "" {
					break
				}
				cursor = next
			}
			if len(all) != racers+2 || s.Len("acme") != racers+2 {
				t.Fatalf("%d listed, %d counted; want %d", len(all), s.Len("acme"), racers+2)
			}
			seen := map[string]bool{}
			for _, p := range all {
				if seen[p.Subject] {
					t.Fatalf("%s listed twice", p.Subject)
				}
				seen[p.Subject] = true
				back, err := s.ByHandle("acme", p.Handle)
				if err != nil || back.Subject != p.Subject {
					t.Fatalf("@%s is %+v, %v; want %s", p.Handle, back, err, p.Subject)
				}
			}
			if !slices.ContainsFunc(all, func(p people.Person) bool { return p.Handle == "fox-mulder" }) {
				t.Fatalf("fox has no handle from his name: %+v", all)
			}
			if _, err := s.ByHandle("acme", "fox"); err == nil {
				t.Fatal("the handle made from fox's subject was kept")
			}
		}
	})
}

// People kept before handles were indexed are indexed once, though two
// processes open the directory at the same time, and keep their handles.
func TestTwoProcessesIndexOnce(t *testing.T) {
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		for i := range 20 {
			p := people.Person{Subject: fmt.Sprintf("okta|%d", i), Name: "Dana", Handle: fmt.Sprintf("dana-%d", i+2), Roles: []string{}, Sources: []string{people.SignIn}, CreatedAt: t0}
			if i == 0 {
				p.Handle = "dana"
			}
			raw, _ := json.Marshal(p)
			if err := a.Write(context.Background(), "people", state.Put([]string{"acme", "globex"}[i%2], p.Subject, raw)); err != nil {
				t.Fatal(err)
			}
		}
		var wg sync.WaitGroup
		stores := make([]*people.Store, 4)
		for i := range stores {
			wg.Go(func() {
				s, err := people.Open([]state.Store{a, b}[i%2])
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
		if n := stores[1].Len("acme"); n != 10 {
			t.Fatalf("acme counts %d", n)
		}
		if p, err := stores[2].ByHandle("acme", "dana"); err != nil || p.Subject != "okta|0" {
			t.Fatalf("@dana = %+v, %v", p, err)
		}
		if p, err := stores[3].ByHandle("globex", "dana-3"); err != nil || p.Subject != "okta|1" {
			t.Fatalf("@dana-3 = %+v, %v", p, err)
		}
		// A new Dana is given the first handle nobody in acme holds.
		p := mustSee(t, stores[0], "acme", signIn("okta|new", "Dana", "viewer"))
		if p.Handle != "dana-2" || stores[1].Len("acme") != 11 {
			t.Fatalf("a new Dana goes by %q, %d counted", p.Handle, stores[1].Len("acme"))
		}
	})
}
