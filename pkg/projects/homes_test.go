package projects_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

func TestFortyRacersSettingOneHomeKeepOne(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		s, err := projects.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		homes := []string{"Field", "Launch", "Globex", "Initech"}
		mustCreate(t, s, "acme", team("Field", ""), ada)
		mustCreate(t, s, "acme", team("Launch", ""), ada)
		mustCreate(t, s, "acme", team("Globex", "Field"), ada)
		mustCreate(t, s, "acme", team("Initech", "Field"), ada)
		var wg sync.WaitGroup
		var moved atomic.Int64
		for i := range 40 {
			wg.Go(func() {
				if err := s.SetHome("acme", "helper", homes[i%len(homes)], ada, t0, nil); err != nil {
					t.Errorf("SetHome: %v", err)
					return
				}
				moved.Add(1)
			})
		}
		wg.Wait()
		recs, err := st.Load(context.Background(), "projects")
		if err != nil {
			t.Fatal(err)
		}
		var kept []string
		for _, r := range recs {
			if strings.HasPrefix(r.Key, "a:") {
				var h struct{ Agent, Project string }
				if err := json.Unmarshal(r.Value, &h); err != nil {
					t.Fatal(err)
				}
				kept = append(kept, h.Agent+" in "+h.Project)
			}
		}
		home, ok := s.Tree("acme").Home("helper")
		if !ok || len(kept) != 1 || kept[0] != "helper in "+home || moved.Load() != 40 {
			t.Fatalf("kept %v, memory says %q, %d moves", kept, home, moved.Load())
		}
		// Every project but the one it lives in now sees it leave as often as arrive.
		arrived, left := 0, 0
		for _, p := range homes {
			h, _, _ := s.History("acme", p, projects.MaxPage, 0)
			for _, c := range h {
				switch {
				case c.What == projects.HomeChanged && c.Note == projects.LivesHereNote:
					arrived++
				case c.What == projects.HomeChanged:
					left++
				}
			}
		}
		if arrived-left != 1 {
			t.Fatalf("%d arrivals and %d departures for one home", arrived, left)
		}
		again, err := projects.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		if h, _ := again.Tree("acme").Home("helper"); h != home {
			t.Fatalf("after restart helper lives in %q, want %q", h, home)
		}
	})
}

func TestAgentsAndHomesSurviveARestart(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		s, err := projects.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		mustCreate(t, s, "acme", team("Field", ""), ada)
		mustCreate(t, s, "acme", team("Globex", "Field"), ada)
		mustCreate(t, s, "globex", team("Field", ""), ada)
		if _, err := s.SetAgents("acme", projects.Root, 1, projects.Agents{Enable: []string{"writer"}, Default: "writer"}, admin, t0); err != nil {
			t.Fatal(err)
		}
		if _, err := s.SetAgents("acme", "Field", 1, projects.Agents{Enable: []string{"lead"}, Allow: []string{"lead", "globex-helper"}, Default: projects.OpenDefault}, ada, t0); err != nil {
			t.Fatal(err)
		}
		if err := s.SetHome("acme", "globex-helper", "Globex", ada, t0, nil); err != nil {
			t.Fatal(err)
		}
		again, err := projects.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		for _, tenant := range []string{"acme", "globex"} {
			before, after := s.Tree(tenant), again.Tree(tenant)
			if !reflect.DeepEqual(after.Root(), before.Root()) || !reflect.DeepEqual(after.Projects(), before.Projects()) {
				t.Fatalf("%s after restart = %+v, want %+v", tenant, after.Projects(), before.Projects())
			}
			for _, p := range before.Subtree(projects.Root) {
				if !reflect.DeepEqual(after.Available(p), before.Available(p)) {
					t.Fatalf("%s %q after restart = %+v, want %+v", tenant, p, after.Available(p), before.Available(p))
				}
			}
		}
		if h, ok := again.Tree("acme").Home("globex-helper"); !ok || h != "Globex" {
			t.Fatalf("the home after restart = %q %v", h, ok)
		}
		if _, homed := again.Tree("globex").Home("globex-helper"); homed {
			t.Fatal("a home reached another tenant after restart")
		}
		if got := again.Tree("acme").Available("Globex"); !reflect.DeepEqual(got.Names, []string{"globex-helper", "lead"}) || got.All {
			t.Fatalf("Globex after restart = %+v", got)
		}
	})
}

func TestAFailedAgentsOrHomeWriteChangesNothing(t *testing.T) {
	f := statetest.NewFaulty(state.NewMemory())
	s, err := projects.Open(f)
	if err != nil {
		t.Fatal(err)
	}
	mustCreate(t, s, "acme", team("Field", ""), ada)
	mustCreate(t, s, "acme", team("Launch", ""), ada)
	if err := s.SetHome("acme", "launch-bot", "Launch", ada, t0, nil); err != nil {
		t.Fatal(err)
	}
	before := s.Tree("acme")
	history, _, _ := s.History("acme", "Field", 0, 0)
	f.Fail(statetest.ErrInjected)
	if _, err := s.SetAgents("acme", "Field", 1, projects.Agents{Enable: []string{"lead"}}, ada, t0); !errors.Is(err, statetest.ErrInjected) {
		t.Fatalf("SetAgents = %v", err)
	}
	if err := s.SetHome("acme", "helper", "Field", ada, t0, nil); !errors.Is(err, statetest.ErrInjected) {
		t.Fatalf("SetHome = %v", err)
	}
	if err := s.SetHome("acme", "launch-bot", "Field", ada, t0, nil); !errors.Is(err, statetest.ErrInjected) {
		t.Fatalf("a move = %v", err)
	}
	if err := s.DropHome("acme", "launch-bot", ada, t0, nil); !errors.Is(err, statetest.ErrInjected) {
		t.Fatalf("DropHome = %v", err)
	}
	if s.Tree("acme") != before {
		t.Fatal("a failed write replaced the tree")
	}
	if h, _ := s.Tree("acme").Home("launch-bot"); h != "Launch" {
		t.Fatalf("a failed move moved it to %q", h)
	}
	if _, homed := s.Tree("acme").Home("helper"); homed {
		t.Fatal("a failed home was kept")
	}
	if h, _, _ := s.History("acme", "Field", 0, 0); !reflect.DeepEqual(h, history) {
		t.Fatalf("a failed write recorded a change: %+v", h)
	}
	f.Fail(nil)
	again, err := projects.Open(f)
	if err != nil {
		t.Fatal(err)
	}
	if h, _ := again.Tree("acme").Home("launch-bot"); h != "Launch" || !reflect.DeepEqual(again.List("acme"), s.List("acme")) {
		t.Fatalf("after restart launch-bot lives in %q", h)
	}
}

func TestReadRefusesAHomeThatIsNoProject(t *testing.T) {
	put := func(t *testing.T, st state.Store, key string, v any) {
		t.Helper()
		raw, _ := json.Marshal(v)
		if err := st.Write(context.Background(), "projects", state.Put("acme", key, raw)); err != nil {
			t.Fatal(err)
		}
	}
	field := projects.Project{Name: "Field", Kind: projects.Team, Version: 1}
	for name, write := range map[string]func(*testing.T, state.Store){
		"a project that is not there": func(t *testing.T, st state.Store) {
			put(t, st, "a:helper", map[string]string{"agent": "helper", "project": "Gone"})
		},
		"kept under another agent": func(t *testing.T, st state.Store) {
			put(t, st, "p:Field", field)
			put(t, st, "a:helper", map[string]string{"agent": "other", "project": "Field"})
		},
		"auto": func(t *testing.T, st state.Store) {
			put(t, st, "p:Field", field)
			put(t, st, "a:auto", map[string]string{"agent": "auto", "project": "Field"})
		},
		"an invalid agent list": func(t *testing.T, st state.Store) {
			put(t, st, "p:Field", projects.Project{Name: "Field", Kind: projects.Team, Version: 1, Agents: projects.Agents{Enable: []string{"auto"}}})
		},
	} {
		t.Run(name, func(t *testing.T) {
			st := state.NewMemory()
			write(t, st)
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
	// A record from before projects had agents loads, and adds nothing.
	st := state.NewMemory()
	put(t, st, "p:Field", map[string]any{"name": "Field", "kind": "team", "version": 1})
	put(t, st, "p:", map[string]any{"name": "", "kind": "company", "version": 1})
	s, err := projects.Open(st)
	if err != nil {
		t.Fatal(err)
	}
	if root := s.Tree("acme").Root(); root.Agents.Enable != nil || s.Tree("acme").IsAvailable("Field", "lead") {
		t.Fatalf("a record from before agents = %+v", root.Agents)
	}
}

func TestEnsureRootWritesTheRootOnce(t *testing.T) {
	s := projects.NewStore()
	if err := s.EnsureRoot("acme", "migration", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := s.EnsureRoot("acme", "someone", time.Now()); err != nil {
		t.Fatal(err)
	}
	root := s.Tree("acme").Root()
	if root.Version != 1 || root.CreatedBy != "migration" || root.Agents.Default != projects.OpenDefault {
		t.Fatalf("the root = %+v", root)
	}
	if h, _, _ := s.History("acme", projects.Root, 0, 0); len(h) != 1 {
		t.Fatalf("history = %+v", h)
	}
	var got []string
	if err := s.EachTenant(func(t string) error { got = append(got, t); return nil }); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "acme" {
		t.Fatalf("tenants = %v", got)
	}
}

func TestCompanyHomeIsExplicitAndKeepsAvailability(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		s, err := projects.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		before := s.Tree("acme").IsAvailable(projects.Root, "helper")
		if err := s.SetHome("acme", "helper", projects.Root, projects.Writer{By: "ada", Govern: true}, t0, nil); err != nil {
			t.Fatal(err)
		}
		home, ok := s.Tree("acme").Home("helper")
		if !ok || home != projects.Root {
			t.Fatalf("Company home missing: %q %v", home, ok)
		}
		if s.Tree("acme").IsAvailable(projects.Root, "helper") != before {
			t.Fatal("a home changed availability")
		}
		again, err := projects.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := again.Tree("acme").Home("helper"); !ok {
			t.Fatal("Company home lost on restart")
		}
	})
}
