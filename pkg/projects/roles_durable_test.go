package projects_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

func TestRestartKeepsRolesKindsAndMoves(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		s, err := projects.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		partner := projects.KindSpec{Name: "partner", Title: "Partner", Like: projects.Account}
		if _, err := s.Update("acme", "", 0, admin, t0, func(p *projects.Project) error {
			p.Kinds = []projects.KindSpec{partner}
			p.Members = members("*=member", "role:comms=owner")
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		mustCreate(t, s, "acme", with(team("Field", ""), members("*=viewer", "role:field-eng=member", "ada=owner")), ada)
		mustCreate(t, s, "acme", team("Platform", ""), ada)
		mustCreate(t, s, "acme", projects.Project{Name: "Globex", Parent: "Platform", Kind: "partner", Announce: projects.Member}, ada)
		if _, err := s.Move("acme", "Globex", 1, "Field", ada, t0.Add(time.Minute), nil); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Update("acme", "Platform", 1, ada, t0, func(p *projects.Project) error { p.Archived = true; return nil }); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Upsert("acme", projects.Project{Name: "News", Kind: projects.Company, Members: members("*=member")}, []string{projects.FieldKind, projects.FieldMembers}, "file", t0); err != nil {
			t.Fatal(err)
		}

		again, err := projects.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(again.List("acme"), s.List("acme")) || !reflect.DeepEqual(again.Tree("acme").Root(), s.Tree("acme").Root()) {
			t.Fatalf("after restart = %+v, want %+v", again.List("acme"), s.List("acme"))
		}
		tree := again.Tree("acme")
		if tree.Like("partner") != projects.Account || !slices.Equal(tree.Kinds(), []projects.KindSpec{partner}) {
			t.Fatalf("the workspace's kinds after restart = %+v", tree.Kinds())
		}
		if g, _ := tree.Get("Globex"); g.Parent != "Field" || g.Announce != projects.Member || !slices.Equal(g.Members, members("ada=owner")) {
			t.Fatalf("Globex after restart = %+v", g)
		}
		if a := tree.Resolve("Globex", projects.Reader{Subject: "dee", Roles: []string{"comms"}}); a.Role != projects.Owner || a.From != "" {
			t.Fatalf("a root owner after restart = %+v", a)
		}
		if _, ok := tree.ArchivedOn("Platform"); !ok {
			t.Fatal("archived did not survive")
		}
		news, _ := again.Get("acme", "News")
		if !reflect.DeepEqual(news.FileOwns, []string{projects.FieldKind, projects.FieldMembers}) {
			t.Fatalf("what the file owns = %q", news.FileOwns)
		}
		h1, _, _ := s.History("acme", "Globex", 0, 0)
		h2, _, _ := again.History("acme", "Globex", 0, 0)
		if !reflect.DeepEqual(h1, h2) || h2[0].What != projects.Moved {
			t.Fatalf("history after restart = %+v, want %+v", h2, h1)
		}
	})
}

func TestAFailedMoveChangesNothing(t *testing.T) {
	f := statetest.NewFaulty(state.NewMemory())
	s, err := projects.Open(f)
	if err != nil {
		t.Fatal(err)
	}
	mustCreate(t, s, "acme", team("Field", ""), ada)
	mustCreate(t, s, "acme", team("Platform", ""), ada)
	mustCreate(t, s, "acme", team("Globex", "Field"), ada)
	before := s.Tree("acme")
	f.Fail(statetest.ErrInjected)
	if _, err := s.Move("acme", "Globex", 1, "Platform", ada, t0, nil); !errors.Is(err, statetest.ErrInjected) {
		t.Fatalf("Move = %v", err)
	}
	if _, err := s.Update("acme", "Field", 1, ada, t0, func(p *projects.Project) error { p.Archived = true; return nil }); !errors.Is(err, statetest.ErrInjected) {
		t.Fatalf("archive = %v", err)
	}
	if s.Tree("acme") != before {
		t.Fatal("a failed write replaced the tree")
	}
	if g, _ := s.Get("acme", "Globex"); g.Parent != "Field" || g.Version != 1 {
		t.Fatalf("a failed move moved: %+v", g)
	}
	if h, _, _ := s.History("acme", "Globex", 0, 0); len(h) != 1 {
		t.Fatalf("a failed move was recorded: %+v", h)
	}
	f.Fail(nil)
	again, err := projects.Open(f)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(again.List("acme"), s.List("acme")) {
		t.Fatalf("after restart = %+v, want %+v", again.List("acme"), s.List("acme"))
	}
	if moved, err := s.Move("acme", "Globex", 1, "Platform", ada, t0, nil); err != nil || moved.Parent != "Platform" {
		t.Fatalf("a move after the failure = %+v, %v", moved, err)
	}
}

// A project written before roles reads as everyone owning it, which is what
// any operator could do to it, and the root as everyone a member. Nothing is
// rewritten until the project changes.
func TestOlderRecordsReadAsEveryoneOwns(t *testing.T) {
	st := state.NewMemory()
	put := func(key string, v any) {
		t.Helper()
		raw, _ := json.Marshal(v)
		if err := st.Write(context.Background(), "projects", state.Put("acme", key, raw)); err != nil {
			t.Fatal(err)
		}
	}
	// The records as phase 1 wrote them: no members field at all.
	put("p:", map[string]any{"name": "", "kind": "company", "version": 1, "created_at": t0, "updated_at": t0})
	put("p:Launch", map[string]any{"name": "Launch", "kind": "team", "version": 1, "created_at": t0, "updated_at": t0})
	put("p:Globex", map[string]any{"name": "Globex", "kind": "account", "parent": "Launch", "version": 1, "created_at": t0, "updated_at": t0})
	s, err := projects.Open(st)
	if err != nil {
		t.Fatal(err)
	}
	if root, _ := s.Get("acme", ""); !slices.Equal(root.Members, members("*=member")) {
		t.Fatalf("the root reads as %+v", root.Members)
	}
	for _, name := range []string{"Launch", "Globex"} {
		p, _ := s.Get("acme", name)
		if !slices.Equal(p.Members, members("*=owner")) {
			t.Fatalf("%s reads as %+v", name, p.Members)
		}
	}
	tree := s.Tree("acme")
	if a := tree.Resolve("Globex", projects.Reader{Subject: "anyone"}); a.Role != projects.Owner || a.From != "Launch" || a.Via != projects.ViaOwnerAbove {
		t.Fatalf("anyone in Globex = %+v", a)
	}
	if a := tree.Resolve("", projects.Reader{Subject: "anyone"}); a.Role != projects.Member {
		t.Fatalf("anyone in the root = %+v", a)
	}
	recs, err := st.Load(context.Background(), "projects")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range recs {
		if strings.Contains(string(r.Value), "members") {
			t.Fatalf("loading rewrote %s: %s", r.Key, r.Value)
		}
	}
	// A project made under one everyone owns is owned by everyone too:
	// ownership flows down until owners are named.
	mustCreate(t, s, "acme", team("Renewals", "Launch"), ada)
	if a := s.Tree("acme").Resolve("Renewals", projects.Reader{Subject: "anyone"}); a.Role != projects.Owner {
		t.Fatalf("anyone in a new child = %+v", a)
	}
	// So is an account, which cannot be members only there: it is made, and
	// opened to everyone as a viewer.
	if acct := mustCreate(t, s, "acme", projects.Project{Name: "Initech", Parent: "Launch", Kind: projects.Account}, ada); !slices.Equal(acct.Members, members("*=viewer", "ada=owner")) {
		t.Fatalf("an account under an older project = %+v", acct.Members)
	}
	// The migration writes the same entries, for a name it keeps too.
	m := projects.NewStore()
	if err := m.Import("acme", []projects.Project{{Name: "Ops"}}, []projects.Project{{Name: "Dropped"}}, "migration", t0); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Ops", "Dropped"} {
		if p, _ := m.Get("acme", name); !slices.Equal(p.Members, members("*=owner")) {
			t.Fatalf("imported project %s = %+v", name, p.Members)
		}
	}
}
