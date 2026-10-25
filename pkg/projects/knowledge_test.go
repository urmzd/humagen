package projects_test

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

func setKnowledge(t *testing.T, s *projects.Store, name string, w projects.Writer, sources ...string) projects.Project {
	t.Helper()
	p, err := s.Get("acme", name)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.SetKnowledge("acme", name, p.Version, sources, w, t0)
	if err != nil {
		t.Fatalf("knowledge of %q: %v", name, err)
	}
	return got
}

// given is a tree's knowledge for a project as "name<from" pairs.
func given(tree *projects.Tree, name string) []string {
	var out []string
	for _, s := range tree.Knowledge(name) {
		out = append(out, s.Name+"<"+s.From)
	}
	return out
}

// A run filed in a project is given the sources its project and every
// project above it name, nearest first, each once from the nearest project
// that names it: never a sibling's, and never a child's.
func TestKnowledgeIsInheritedDownTheTreeNearestFirst(t *testing.T) {
	s := projects.NewStore()
	mustCreate(t, s, "acme", team("Field", ""), ada)
	mustCreate(t, s, "acme", projects.Project{Name: "Globex", Parent: "Field", Kind: projects.Account}, ada)
	mustCreate(t, s, "acme", projects.Project{Name: "Initech", Parent: "Field", Kind: projects.Account}, ada)
	setKnowledge(t, s, projects.Root, admin, "handbook")
	setKnowledge(t, s, "Field", ada, "playbook", "handbook")
	setKnowledge(t, s, "Globex", ada, "globex-notes")
	setKnowledge(t, s, "Initech", ada, "initech-notes", "playbook")
	tree := s.Tree("acme")
	for _, c := range []struct {
		project string
		want    []string
	}{
		{"Globex", []string{"globex-notes<Globex", "playbook<Field", "handbook<Field"}},
		{"Initech", []string{"initech-notes<Initech", "playbook<Initech", "handbook<Field"}},
		{"Field", []string{"playbook<Field", "handbook<Field"}},
		{projects.Root, []string{"handbook<"}},
		{"Nowhere", nil},
	} {
		if got := given(tree, c.project); !reflect.DeepEqual(got, c.want) {
			t.Errorf("knowledge given in %q = %v, want %v", c.project, got, c.want)
		}
	}
}

// A run is given at most MaxRunKnowledge sources: the farthest are left out.
func TestARunIsGivenAtMostSixteenSourcesTheNearestFirst(t *testing.T) {
	s := projects.NewStore()
	mustCreate(t, s, "acme", team("A", ""), ada)
	mustCreate(t, s, "acme", team("B", "A"), ada)
	mustCreate(t, s, "acme", team("C", "B"), ada)
	names := func(prefix string) []string {
		var out []string
		for i := range projects.MaxKnowledge {
			out = append(out, fmt.Sprintf("%s-%d", prefix, i))
		}
		return out
	}
	setKnowledge(t, s, projects.Root, admin, names("root")...)
	setKnowledge(t, s, "A", ada, names("a")...)
	setKnowledge(t, s, "B", ada, names("b")...)
	setKnowledge(t, s, "C", ada, names("c")...)
	got := s.Tree("acme").Knowledge("C")
	if len(got) != projects.MaxRunKnowledge {
		t.Fatalf("%d sources, want %d", len(got), projects.MaxRunKnowledge)
	}
	for _, src := range got {
		if !strings.HasPrefix(src.Name, "c-") && !strings.HasPrefix(src.Name, "b-") {
			t.Fatalf("given %v; C's and B's come before A's and the root's", got)
		}
	}
}

func TestKnowledgeRules(t *testing.T) {
	s := projects.NewStore()
	mustCreate(t, s, "acme", team("Field", ""), ada)
	many := make([]string, projects.MaxKnowledge+1)
	for i := range many {
		many[i] = fmt.Sprintf("source-%d", i)
	}
	for _, c := range []struct {
		name    string
		sources []string
	}{
		{"too many", many},
		{"a name in capitals", []string{"Handbook"}},
		{"a name with a comma", []string{"a,b"}},
		{"no name", []string{""}},
		{"a source twice", []string{"handbook", "handbook"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			if _, err := s.SetKnowledge("acme", "Field", 1, c.sources, ada, t0); !errors.Is(err, projects.ErrInvalid) {
				t.Fatalf("SetKnowledge = %v, want ErrInvalid", err)
			}
		})
	}
	if _, err := s.SetKnowledge("acme", "Field", 7, []string{"handbook"}, ada, t0); !errors.Is(err, projects.ErrVersion) {
		t.Fatalf("a stale version = %v", err)
	}
	if _, err := s.SetKnowledge("acme", projects.Root, 0, []string{"handbook"}, ada, t0); !errors.Is(err, projects.ErrForbidden) {
		t.Fatalf("the workspace's knowledge without govern = %v", err)
	}
	p := setKnowledge(t, s, "Field", ada, "playbook", "handbook")
	if p.Version != 2 || !reflect.DeepEqual(p.Knowledge, []string{"playbook", "handbook"}) {
		t.Fatalf("Field = %+v", p)
	}
	if h, _ := s.History("acme", "Field", 1, 0); h[0].What != projects.KnowledgeChanged || len(h[0].Before.Knowledge) != 0 || len(h[0].After.Knowledge) != 2 {
		t.Fatalf("the change = %+v", h[0])
	}
	if same := setKnowledge(t, s, "Field", ada, "playbook", "handbook"); same.Version != 2 {
		t.Fatalf("the same sources again made version %d", same.Version)
	}
	if _, err := s.Update("acme", "Field", 2, ada, t0, func(p *projects.Project) error { p.Knowledge = nil; return nil }); !errors.Is(err, projects.ErrInvalid) {
		t.Fatalf("changing knowledge through Update = %v, want ErrInvalid", err)
	}
	if cleared := setKnowledge(t, s, "Field", ada); cleared.Version != 3 || cleared.Knowledge != nil {
		t.Fatalf("cleared = %+v", cleared)
	}
}

func TestKnowledgeTheFileSetsIsTheFiles(t *testing.T) {
	s := projects.NewStore()
	got, err := s.Upsert("acme", projects.Project{Name: "Globex", Kind: projects.Account, Knowledge: []string{"globex-notes"}},
		[]string{projects.FieldKind, projects.FieldKnowledge}, "file", t0)
	if err != nil || !reflect.DeepEqual(got.Knowledge, []string{"globex-notes"}) {
		t.Fatalf("Upsert = %+v, %v", got, err)
	}
	if _, err := s.SetKnowledge("acme", "Globex", got.Version, nil, ada, t0); !errors.Is(err, projects.ErrManaged) {
		t.Fatalf("changing the file's knowledge = %v, want ErrManaged", err)
	}
	// A file that leaves it out leaves it to the console.
	left, err := s.Upsert("acme", projects.Project{Name: "Globex", Kind: projects.Account}, []string{projects.FieldKind}, "file", t0)
	if err != nil || !reflect.DeepEqual(left.Knowledge, []string{"globex-notes"}) {
		t.Fatalf("Upsert without knowledge = %+v, %v", left, err)
	}
	if _, err := s.SetKnowledge("acme", "Globex", left.Version, []string{"pricing"}, ada, t0); err != nil {
		t.Fatalf("the console's knowledge again = %v", err)
	}
}

// What a project names survives a restart, and so does the record of the
// change.
func TestKnowledgeSurvivesARestart(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		s, err := projects.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		mustCreate(t, s, "acme", team("Field", ""), ada)
		mustCreate(t, s, "acme", projects.Project{Name: "Globex", Parent: "Field", Kind: projects.Account}, ada)
		setKnowledge(t, s, projects.Root, admin, "handbook")
		setKnowledge(t, s, "Field", ada, "playbook")
		again, err := projects.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		if got := given(again.Tree("acme"), "Globex"); !reflect.DeepEqual(got, []string{"playbook<Field", "handbook<"}) {
			t.Fatalf("knowledge in Globex after a restart = %v", got)
		}
		if h, _ := again.History("acme", "Field", 1, 0); h[0].What != projects.KnowledgeChanged || !reflect.DeepEqual(h[0].After.Knowledge, []string{"playbook"}) {
			t.Fatalf("the change after a restart = %+v", h[0])
		}
	})
}
