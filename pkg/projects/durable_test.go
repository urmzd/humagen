package projects_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

func TestRestart(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		s, err := projects.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		mustCreate(t, s, "acme", team("Field", ""), ada)
		mustCreate(t, s, "acme", projects.Project{Name: "Globex", Parent: "Field", Kind: projects.Account,
			Account: &projects.AccountInfo{Customer: "Globex Inc", ExternalID: "sf:1"}, Instructions: "Never promise dates."}, ada)
		mustCreate(t, s, "acme", team("Launch", ""), ada)
		if _, err := s.Update("acme", "Field", 1, ada, t0.Add(time.Minute), func(p *projects.Project) error { p.Title = "Field engineering"; return nil }); err != nil {
			t.Fatal(err)
		}
		if err := s.Delete("acme", "Launch", ada, t0, nil); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Upsert("acme", projects.Project{Name: "News", Kind: projects.Company}, []string{projects.FieldKind}, "file", t0); err != nil {
			t.Fatal(err)
		}
		mustCreate(t, s, "globex", team("Field", ""), ada)

		again, err := projects.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(again.List("acme"), s.List("acme")) || !reflect.DeepEqual(again.Tree("acme").Root(), s.Tree("acme").Root()) {
			t.Fatalf("after restart = %+v, want %+v", again.List("acme"), s.List("acme"))
		}
		news, _ := again.Get("acme", "News")
		if news.Managed != projects.ManagedFile || !reflect.DeepEqual(news.FileOwns, []string{projects.FieldKind}) {
			t.Fatalf("what the file owns did not survive: %+v", news)
		}
		h1, _, _ := s.History("acme", "Field", 0, 0)
		h2, _, _ := again.History("acme", "Field", 0, 0)
		if !reflect.DeepEqual(h1, h2) || len(h2) != 2 {
			t.Fatalf("history after restart = %+v, want %+v", h2, h1)
		}
		if _, err := again.Create("acme", team("Launch", ""), ada, t0); !errors.Is(err, projects.ErrForbidden) {
			t.Fatalf("a tombstone did not survive: %v", err)
		}
		if _, err := again.Get("globex", "Globex"); !errors.Is(err, projects.ErrNotFound) {
			t.Fatalf("another tenant's project = %v", err)
		}
		// Changes after a restart number on from those before it.
		mustCreate(t, again, "acme", team("Later", "Field"), ada)
		third, err := projects.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		h, _, _ := third.History("acme", "Later", 0, 0)
		all := 0
		for _, name := range []string{"", "Field", "Globex", "Launch", "News", "Later"} {
			c, _, _ := third.History("acme", name, 0, 0)
			all += len(c)
		}
		if len(h) != 1 || h[0].Seq != int64(all) {
			t.Fatalf("the newest change is %+v of %d", h, all)
		}
	})
}

func TestTheHistoryRingIsPrunedInTheSameWrite(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		s, err := projects.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		mustCreate(t, s, "acme", team("Field", ""), ada)
		for i := range projects.MaxHistory + 5 {
			if _, err := s.Update("acme", "Field", i+1, ada, t0, func(p *projects.Project) error { p.Description = strings.Repeat("x", i%400+1); return nil }); err != nil {
				t.Fatal(err)
			}
		}
		recs, err := st.Load(context.Background(), "projects")
		if err != nil {
			t.Fatal(err)
		}
		kept := 0
		for _, r := range recs {
			if strings.HasPrefix(r.Key, "h:") {
				kept++
			}
		}
		if kept != projects.MaxHistory {
			t.Fatalf("%d changes kept, want %d", kept, projects.MaxHistory)
		}
		again, err := projects.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		page, _, _ := again.History("acme", "Field", 1, 0)
		if page[0].After.Version != projects.MaxHistory+6 {
			t.Fatalf("the newest change after restart = %+v", page[0])
		}
	})
}

func TestFailedWriteChangesNothing(t *testing.T) {
	f := statetest.NewFaulty(state.NewMemory())
	s, err := projects.Open(f)
	if err != nil {
		t.Fatal(err)
	}
	mustCreate(t, s, "acme", team("Field", ""), ada)
	mustCreate(t, s, "acme", team("Launch", ""), ada)
	before := s.Tree("acme")
	f.Fail(statetest.ErrInjected)
	if _, err := s.Create("acme", team("Ops", ""), ada, t0); !errors.Is(err, statetest.ErrInjected) {
		t.Fatalf("Create = %v", err)
	}
	if _, err := s.Update("acme", "Field", 1, ada, t0, func(p *projects.Project) error { p.Title = "x"; return nil }); !errors.Is(err, statetest.ErrInjected) {
		t.Fatalf("Update = %v", err)
	}
	if err := s.Delete("acme", "Launch", ada, t0, nil); !errors.Is(err, statetest.ErrInjected) {
		t.Fatalf("Delete = %v", err)
	}
	if _, err := s.Upsert("acme", projects.Project{Name: "Field", Title: "y"}, []string{projects.FieldTitle}, "file", t0); !errors.Is(err, statetest.ErrInjected) {
		t.Fatalf("Upsert = %v", err)
	}
	if err := s.Import("globex", []projects.Project{{Name: "A"}}, nil, "migration", t0); !errors.Is(err, statetest.ErrInjected) {
		t.Fatalf("Import = %v", err)
	}
	if s.Tree("acme") != before || !reflect.DeepEqual(s.List("acme"), before.Projects()) || stored(s, "globex") {
		t.Fatal("a failed write changed the tree")
	}
	if _, ok := s.Tombstone("acme", "Launch"); ok {
		t.Fatal("a failed delete left a tombstone")
	}
	if h, _, _ := s.History("acme", "Field", 0, 0); len(h) != 1 {
		t.Fatalf("a failed write recorded a change: %+v", h)
	}
	f.Fail(nil)
	// What memory holds is what a restart reads.
	again, err := projects.Open(f)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(again.List("acme"), s.List("acme")) {
		t.Fatalf("after restart = %+v, want %+v", again.List("acme"), s.List("acme"))
	}
	if _, err := s.Create("acme", team("Ops", ""), ada, t0); err != nil {
		t.Fatalf("a write after the failure: %v", err)
	}
}

func TestReadRefusesATreeThatIsNotOne(t *testing.T) {
	put := func(st state.Store, tenant, key string, v any) {
		t.Helper()
		raw, _ := json.Marshal(v)
		if err := st.Write(context.Background(), "projects", state.Put(tenant, key, raw)); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range []struct {
		name  string
		write func(st state.Store)
	}{
		{"a cycle", func(st state.Store) {
			put(st, "acme", "p:A", projects.Project{Name: "A", Kind: projects.Team, Parent: "B", Version: 1})
			put(st, "acme", "p:B", projects.Project{Name: "B", Kind: projects.Team, Parent: "A", Version: 1})
		}},
		{"an orphan", func(st state.Store) {
			put(st, "acme", "p:A", projects.Project{Name: "A", Kind: projects.Team, Parent: "Gone", Version: 1})
		}},
		{"a record under another name", func(st state.Store) {
			put(st, "acme", "p:A", projects.Project{Name: "B", Kind: projects.Team, Version: 1})
		}},
		{"one name twice", func(st state.Store) {
			put(st, "acme", "p:A", projects.Project{Name: "A", Kind: projects.Team, Version: 1})
			put(st, "acme", "p:a", projects.Project{Name: "a", Kind: projects.Team, Version: 1})
		}},
		{"an unknown key", func(st state.Store) { put(st, "acme", "q:A", map[string]string{}) }},
		{"an invalid record", func(st state.Store) {
			put(st, "acme", "p:A", projects.Project{Name: "A", Kind: "guild", Version: 1})
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			st := state.NewMemory()
			c.write(st)
			s, err := projects.Open(st)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.Read("acme"); !errors.Is(err, projects.ErrInvalid) {
				t.Fatalf("Read = %v, want ErrInvalid", err)
			}
			if _, err := s.Create("acme", projects.Project{Name: "New", Kind: projects.Team}, projects.Writer{By: "ada", Govern: true}, t0); !errors.Is(err, projects.ErrInvalid) {
				t.Fatalf("a write = %v, want ErrInvalid", err)
			}
		})
	}
}
