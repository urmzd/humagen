package projects_test

import (
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/state"
)

func setCorpora(t *testing.T, s *projects.Store, name string, w projects.Writer, names ...string) projects.Project {
	t.Helper()
	p, err := s.Get("acme", name)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.SetCorpora("acme", name, p.Version, names, w, t0)
	if err != nil {
		t.Fatalf("corpora of %q: %v", name, err)
	}
	return got
}

func searched(tree *projects.Tree, name string) []string {
	var out []string
	for _, c := range tree.Corpora(name) {
		out = append(out, c.Name+"<"+c.From)
	}
	return out
}

// A session filed in a project searches the corpora its project and every
// project above it attach, nearest first, each once from the nearest
// project: never a sibling's, and never a child's.
func TestCorporaAreInheritedDownTheTreeNearestFirst(t *testing.T) {
	s := field(t)
	setCorpora(t, s, projects.Root, admin, "handbook")
	setCorpora(t, s, "Field", ada, "playbook", "handbook")
	setCorpora(t, s, "Globex", ada, "globex-contracts")
	tree := s.Tree("acme")
	for name, want := range map[string][]string{
		"Renewals":    {"globex-contracts<Globex", "playbook<Field", "handbook<Field"},
		"Initech":     {"playbook<Field", "handbook<Field"},
		"Launch":      {"handbook<"},
		projects.Root: {"handbook<"},
		"Nowhere":     nil,
	} {
		if got := searched(tree, name); !slices.Equal(got, want) {
			t.Errorf("%s searches %v, want %v", name, got, want)
		}
	}
}

func TestCorporaRules(t *testing.T) {
	s := field(t)
	p, _ := s.Get("acme", "Field")
	many := make([]string, projects.MaxCorpora+1)
	for i := range many {
		many[i] = fmt.Sprintf("c-%02d", i)
	}
	for name, names := range map[string][]string{
		"too many":     many,
		"not a name":   {"Hand Book"},
		"twice":        {"handbook", "handbook"},
		"upper case":   {"Handbook"},
		"leading dash": {"-handbook"},
	} {
		if _, err := s.SetCorpora("acme", "Field", p.Version, names, ada, t0); !errors.Is(err, projects.ErrInvalid) {
			t.Errorf("%s = %v, want ErrInvalid", name, err)
		}
	}
	if _, err := s.SetCorpora("acme", "Field", p.Version+3, []string{"handbook"}, ada, t0); !errors.Is(err, projects.ErrVersion) {
		t.Fatalf("a stale version = %v", err)
	}
	if _, err := s.SetCorpora("acme", projects.Root, 0, []string{"handbook"}, ada, t0); !errors.Is(err, projects.ErrForbidden) {
		t.Fatalf("the workspace's corpora without govern = %v", err)
	}
	got := setCorpora(t, s, "Field", ada, "handbook")
	if again := setCorpora(t, s, "Field", ada, "handbook"); again.Version != got.Version {
		t.Fatalf("setting the same corpora made version %d", again.Version)
	}
	h, _, _ := s.History("acme", "Field", 1, 0)
	if h[0].What != projects.ContextChanged {
		t.Fatalf("the change recorded = %+v", h[0])
	}
	if cleared := setCorpora(t, s, "Field", ada); cleared.Context.Corpora != nil {
		t.Fatalf("clearing left %v", cleared.Context.Corpora)
	}
}

func TestCorporaSurviveARestart(t *testing.T) {
	st := state.NewMemory()
	s, err := projects.Open(st)
	if err != nil {
		t.Fatal(err)
	}
	mustCreate(t, s, "acme", team("Field", ""), ada)
	setCorpora(t, s, "Field", ada, "playbook")
	again, err := projects.Open(st)
	if err != nil {
		t.Fatal(err)
	}
	if got := searched(again.Tree("acme"), "Field"); !slices.Equal(got, []string{"playbook<Field"}) {
		t.Fatalf("after a restart Field searches %v", got)
	}
}
