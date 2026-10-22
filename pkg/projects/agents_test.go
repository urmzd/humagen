package projects_test

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/urmzd/warp/pkg/projects"
)

// field builds acme's tree: Field and Launch at the top, Globex and Initech
// under Field, and Renewals under Globex.
func field(t *testing.T) *projects.Store {
	t.Helper()
	s := projects.NewStore()
	mustCreate(t, s, "acme", team("Field", ""), ada)
	mustCreate(t, s, "acme", team("Launch", ""), ada)
	mustCreate(t, s, "acme", projects.Project{Name: "Globex", Parent: "Field", Kind: projects.Account}, ada)
	mustCreate(t, s, "acme", projects.Project{Name: "Initech", Parent: "Field", Kind: projects.Account}, ada)
	mustCreate(t, s, "acme", team("Renewals", "Globex"), ada)
	return s
}

func setAgents(t *testing.T, s *projects.Store, name string, a projects.Agents, w projects.Writer) projects.Project {
	t.Helper()
	p, err := s.Get("acme", name)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.SetAgents("acme", name, p.Version, a, w, t0)
	if err != nil {
		t.Fatalf("agents of %q: %v", name, err)
	}
	return got
}

func mustHome(t *testing.T, s *projects.Store, agent, project string) {
	t.Helper()
	if err := s.SetHome("acme", agent, project, admin, t0, nil); err != nil {
		t.Fatalf("home %s in %q: %v", agent, project, err)
	}
}

func TestAWorkspaceThatWroteNothingReadsAsTheUpgradeLeavesIt(t *testing.T) {
	tree := projects.NewStore().Tree("acme")
	if got := tree.Available(projects.Root); !got.All || len(got.Names) != 0 {
		t.Fatalf("a workspace with no records = %+v, want every platform agent", got)
	}
	if d, from := tree.DefaultOf(projects.Root); d != projects.Auto || from != projects.Root {
		t.Fatalf("its default = %q from %q, want auto at the root", d, from)
	}
	// Its first write keeps that, rather than closing everything.
	s := projects.NewStore()
	mustCreate(t, s, "acme", team("Field", ""), ada)
	if !s.Tree("acme").IsAvailable("Field", "helper") || s.Tree("acme").Root().Agents.Default != projects.Auto {
		t.Fatalf("the first write closed the workspace: %+v", s.Tree("acme").Root().Agents)
	}
}

func TestAZeroAgentsAddsNothingAndStarIsWritten(t *testing.T) {
	s := field(t)
	setAgents(t, s, projects.Root, projects.Agents{}, admin)
	tree := s.Tree("acme")
	for _, p := range []string{projects.Root, "Field", "Globex", "Renewals"} {
		if got := tree.Available(p); got.All || len(got.Names) != 0 || tree.IsAvailable(p, "helper") {
			t.Fatalf("%q with nothing enabled = %+v", p, got)
		}
	}
	if enable := tree.Root().Agents.Enable; enable == nil {
		t.Fatal("a stored enable list is nil")
	}
	setAgents(t, s, "Field", projects.Agents{Enable: []string{"helper"}}, ada)
	tree = s.Tree("acme")
	if tree.IsAvailable(projects.Root, "helper") || tree.IsAvailable("Launch", "helper") {
		t.Fatal("an enable list reached above or beside its project")
	}
	if !tree.IsAvailable("Field", "helper") || !tree.IsAvailable("Renewals", "helper") || tree.IsAvailable("Renewals", "other") {
		t.Fatal("an enable list did not reach exactly its agent, here and below")
	}
}

func TestAllowOnlyTightensDownTheChain(t *testing.T) {
	s := field(t)
	setAgents(t, s, "Field", projects.Agents{Enable: []string{}, Allow: []string{"lead", "writer"}}, ada)
	// Globex allows an agent Field does not: it stays out.
	setAgents(t, s, "Globex", projects.Agents{Enable: []string{}, Allow: []string{"writer", "wide"}}, ada)
	tree := s.Tree("acme")
	want := map[string][]string{
		"Field":    {"lead", "writer"},
		"Initech":  {"lead", "writer"},
		"Globex":   {"writer"},
		"Renewals": {"writer"},
	}
	for p, names := range want {
		got := tree.Available(p)
		if got.All || !slices.Equal(got.Names, names) {
			t.Errorf("%s = %+v, want %v", p, got, names)
		}
		for _, a := range []string{"lead", "writer", "wide", "other"} {
			if tree.IsAvailable(p, a) != slices.Contains(names, a) {
				t.Errorf("IsAvailable(%s, %s) disagrees with %v", p, a, names)
			}
		}
	}
	if got := tree.Available("Launch"); !got.All {
		t.Fatalf("a sibling was narrowed: %+v", got)
	}
	// Allowed is what the lists permit, whatever is offered: what a move
	// compares, so it never widens.
	for p, names := range map[string][]string{"Field": {"lead", "writer"}, "Renewals": {"writer"}, "Launch": nil, projects.Root: nil} {
		if got := tree.Allowed(p); !slices.Equal(got, names) || (names == nil) != (got == nil) {
			t.Errorf("Allowed(%s) = %v, want %v", p, got, names)
		}
	}
	for _, c := range []struct {
		was, next []string
		widens    bool
	}{
		{nil, nil, false}, {nil, []string{"lead"}, false}, {[]string{"lead"}, nil, true},
		{[]string{"lead", "writer"}, []string{"writer"}, false}, {[]string{"writer"}, []string{"writer", "wide"}, true},
		{[]string{"writer"}, []string{}, false},
	} {
		if got := projects.Widens(c.was, c.next); got != c.widens {
			t.Errorf("Widens(%v, %v) = %v", c.was, c.next, got)
		}
	}
}

func TestStarNeverReachesAnAgentThatLivesInAProject(t *testing.T) {
	s := field(t)
	mustHome(t, s, "globex-helper", "Globex")
	tree := s.Tree("acme")
	for _, p := range []string{projects.Root, "Field", "Initech", "Launch"} {
		if tree.IsAvailable(p, "globex-helper") || slices.Contains(tree.Available(p).Names, "globex-helper") {
			t.Errorf("globex-helper is available in %q, outside its home", p)
		}
	}
	for _, p := range []string{"Globex", "Renewals"} {
		got := tree.Available(p)
		if !tree.IsAvailable(p, "globex-helper") || !got.All || !slices.Equal(got.Names, []string{"globex-helper"}) {
			t.Errorf("%q = %+v, want every platform agent and globex-helper", p, got)
		}
	}
	if home, ok := tree.Home("globex-helper"); !ok || home != "Globex" || !slices.Equal(tree.Homes("Globex"), []string{"globex-helper"}) {
		t.Fatalf("home = %q %v, homes %v", home, ok, tree.Homes("Globex"))
	}
	// An allow list above the home still decides inside it.
	setAgents(t, s, "Field", projects.Agents{Enable: []string{}, Allow: []string{"writer"}}, ada)
	if s.Tree("acme").IsAvailable("Globex", "globex-helper") {
		t.Fatal("an ancestor's allow list did not narrow a project agent")
	}
}

func TestAnEnableEntryNamingAProjectAgentAddsNothing(t *testing.T) {
	s := field(t)
	setAgents(t, s, projects.Root, projects.Agents{}, admin)
	mustHome(t, s, "globex-helper", "Globex")
	// Only the file can write one now, since the API refuses it.
	if _, err := s.Upsert("acme", projects.Project{Name: "Initech", Kind: projects.Account, Parent: "Field",
		Agents: projects.Agents{Enable: []string{"globex-helper"}}}, []string{projects.FieldAgents}, "file", t0); err != nil {
		t.Fatal(err)
	}
	if tree := s.Tree("acme"); tree.IsAvailable("Initech", "globex-helper") || len(tree.Available("Initech").Names) != 0 {
		t.Fatalf("an enable entry reached a project agent: %+v", tree.Available("Initech"))
	}
	p, _ := s.Get("acme", "Field")
	if _, err := s.SetAgents("acme", "Field", p.Version, projects.Agents{Enable: []string{"globex-helper"}}, ada, t0); !errors.Is(err, projects.ErrInvalid) {
		t.Fatalf("enabling a project agent = %v, want ErrInvalid", err)
	}
}

func TestAvailableAgreesWithIsAvailable(t *testing.T) {
	s := field(t)
	mustHome(t, s, "globex-helper", "Globex")
	mustHome(t, s, "field-helper", "Field")
	setAgents(t, s, projects.Root, projects.Agents{Enable: []string{"writer"}}, admin)
	setAgents(t, s, "Field", projects.Agents{Enable: []string{projects.Everyone}}, ada)
	setAgents(t, s, "Initech", projects.Agents{Enable: []string{}, Allow: []string{"writer", "field-helper", "globex-helper", "lead"}}, ada)
	setAgents(t, s, "Renewals", projects.Agents{Enable: []string{"lead"}, Allow: []string{"globex-helper", "lead", "other"}}, ada)
	tree := s.Tree("acme")
	agents := []string{"writer", "lead", "other", "globex-helper", "field-helper", "nobody"}
	for _, p := range tree.Subtree(projects.Root) {
		got := tree.Available(p)
		for _, a := range agents {
			_, homed := tree.Home(a)
			listed := slices.Contains(got.Names, a) || (got.All && !homed)
			if listed != tree.IsAvailable(p, a) {
				t.Errorf("%q: Available %+v and IsAvailable(%s) = %v disagree", p, got, a, tree.IsAvailable(p, a))
			}
		}
	}
	// Spot checks of the rule itself.
	for _, c := range []struct {
		project, agent string
		want           bool
	}{
		{projects.Root, "writer", true}, {projects.Root, "lead", false}, {projects.Root, "field-helper", false},
		{"Launch", "lead", false}, {"Field", "lead", true}, {"Field", "field-helper", true}, {"Field", "globex-helper", false},
		{"Initech", "writer", true}, {"Initech", "field-helper", true}, {"Initech", "globex-helper", false}, {"Initech", "other", false},
		{"Renewals", "globex-helper", true}, {"Renewals", "lead", true}, {"Renewals", "writer", false}, {"Renewals", "field-helper", false},
		{"Globex", projects.Auto, false}, {"Nowhere", "writer", false},
	} {
		if got := tree.IsAvailable(c.project, c.agent); got != c.want {
			t.Errorf("IsAvailable(%q, %s) = %v, want %v", c.project, c.agent, got, c.want)
		}
	}
}

func TestADefaultIsInheritedNearestFirst(t *testing.T) {
	s := field(t)
	tree := s.Tree("acme")
	if d, from := tree.DefaultOf("Renewals"); d != projects.Auto || from != projects.Root {
		t.Fatalf("Renewals inherits %q from %q, want the root's auto", d, from)
	}
	if d, _ := tree.NamedDefault("Renewals"); d != "" {
		t.Fatalf("a chain of auto names %q", d)
	}
	setAgents(t, s, "Field", projects.Agents{Enable: []string{}, Default: "field-helper"}, ada)
	setAgents(t, s, "Globex", projects.Agents{Enable: []string{}, Default: projects.Auto}, ada)
	tree = s.Tree("acme")
	for _, c := range []struct{ project, agent, from, named, namedFrom string }{
		{"Renewals", projects.Auto, "Globex", "field-helper", "Field"},
		{"Initech", "field-helper", "Field", "field-helper", "Field"},
		{"Launch", projects.Auto, projects.Root, "", ""},
		{"Nowhere", "", "", "", ""},
	} {
		d, from := tree.DefaultOf(c.project)
		n, nFrom := tree.NamedDefault(c.project)
		if d != c.agent || from != c.from || n != c.named || nFrom != c.namedFrom {
			t.Errorf("%q: default %q from %q, named %q from %q; want %q %q %q %q", c.project, d, from, n, nFrom, c.agent, c.from, c.named, c.namedFrom)
		}
	}
}

func TestADefaultMustBeAvailableWhereItIsSet(t *testing.T) {
	s := field(t)
	mustHome(t, s, "globex-helper", "Globex")
	set := func(name string, a projects.Agents, w projects.Writer) error {
		p, _ := s.Get("acme", name)
		_, err := s.SetAgents("acme", name, p.Version, a, w, t0)
		return err
	}
	if err := set("Initech", projects.Agents{Enable: []string{}, Default: "globex-helper"}, ada); !errors.Is(err, projects.ErrInvalid) {
		t.Fatalf("a default that lives elsewhere = %v", err)
	}
	if err := set("Renewals", projects.Agents{Enable: []string{}, Default: "globex-helper"}, ada); err != nil {
		t.Fatalf("a default that lives above = %v", err)
	}
	if err := set("Field", projects.Agents{Enable: []string{}, Allow: []string{"writer"}, Default: "lead"}, ada); !errors.Is(err, projects.ErrInvalid) {
		t.Fatalf("a default its own allow list refuses = %v", err)
	}
	if err := set("Launch", projects.Agents{Enable: []string{}, Default: projects.Auto}, ada); err != nil {
		t.Fatalf("auto = %v", err)
	}
	if err := set(projects.Root, projects.Agents{Enable: []string{}, Default: "lead"}, admin); !errors.Is(err, projects.ErrInvalid) {
		t.Fatalf("a root default nothing enables = %v", err)
	}
}

func TestAgentListsAreValidated(t *testing.T) {
	many := make([]string, projects.MaxAgentList+1)
	for i := range many {
		many[i] = fmt.Sprintf("a-%02d", i)
	}
	for name, a := range map[string]projects.Agents{
		"65 enabled":           {Enable: many},
		"65 allowed":           {Allow: many},
		"star with others":     {Enable: []string{projects.Everyone, "lead"}},
		"star in allow":        {Allow: []string{projects.Everyone}},
		"auto enabled":         {Enable: []string{projects.Auto}},
		"auto allowed":         {Allow: []string{projects.Auto}},
		"an empty allow list":  {Allow: []string{}},
		"twice":                {Enable: []string{"lead", "Lead"}},
		"not a name":           {Enable: []string{"lead bot"}},
		"fiber's control name": {Enable: []string{"_control"}},
		"a default of star":    {Default: projects.Everyone},
		"a default not a name": {Default: "a/b"},
		"too long":             {Enable: []string{strings.Repeat("a", 65)}},
	} {
		t.Run(name, func(t *testing.T) {
			s := field(t)
			if _, err := s.SetAgents("acme", "Field", 1, a, ada, t0); !errors.Is(err, projects.ErrInvalid) {
				t.Fatalf("SetAgents = %v, want ErrInvalid", err)
			}
		})
	}
	s := field(t)
	got := setAgents(t, s, "Field", projects.Agents{Enable: []string{" Writer", "lead"}, Allow: []string{"WRITER", "lead"}, Default: "Lead"}, ada)
	if want := (projects.Agents{Enable: []string{"lead", "writer"}, Allow: []string{"lead", "writer"}, Default: "lead"}); !slices.Equal(got.Agents.Enable, want.Enable) ||
		!slices.Equal(got.Agents.Allow, want.Allow) || got.Agents.Default != want.Default {
		t.Fatalf("kept %+v, want the canonical %+v", got.Agents, want)
	}
}

func TestSettingAgentsIsVersionedAndRecorded(t *testing.T) {
	s := field(t)
	if _, err := s.SetAgents("acme", projects.Root, 1, projects.Agents{Enable: []string{projects.Everyone}, Default: "lead"}, ada, t0); !errors.Is(err, projects.ErrForbidden) {
		t.Fatalf("the workspace default without govern = %v", err)
	}
	root := setAgents(t, s, projects.Root, projects.Agents{Enable: []string{projects.Everyone}, Default: "lead"}, admin)
	if root.Agents.Default != "lead" || root.Version != 2 {
		t.Fatalf("root = %+v", root)
	}
	h, _ := s.History("acme", projects.Root, 1, 0)
	if h[0].What != projects.AgentsChanged || h[0].Before.Agents.Default != projects.Auto || h[0].After.Agents.Default != "lead" {
		t.Fatalf("the change recorded = %+v", h[0])
	}
	again := setAgents(t, s, projects.Root, projects.Agents{Enable: []string{projects.Everyone}, Default: "lead"}, admin)
	if again.Version != 2 {
		t.Fatalf("setting the same lists made version %d", again.Version)
	}
	if _, err := s.SetAgents("acme", "Field", 7, projects.Agents{}, ada, t0); !errors.Is(err, projects.ErrVersion) {
		t.Fatalf("a stale version = %v", err)
	}
	// A project the file sets the agents of is changed only by the file.
	if _, err := s.Upsert("acme", projects.Project{Name: "Launch", Agents: projects.Agents{Enable: []string{"lead"}}}, []string{projects.FieldAgents}, "file", t0); err != nil {
		t.Fatal(err)
	}
	p, _ := s.Get("acme", "Launch")
	if _, err := s.SetAgents("acme", "Launch", p.Version, projects.Agents{}, ada, t0); !errors.Is(err, projects.ErrManaged) {
		t.Fatalf("the file's agents through the API = %v", err)
	}
	if !slices.Equal(p.Agents.Enable, []string{"lead"}) {
		t.Fatalf("the file's agents = %+v", p.Agents)
	}
}

func TestAHomeIsRefusedWhileSomethingWouldLoseTheAgent(t *testing.T) {
	s := field(t)
	setAgents(t, s, "Launch", projects.Agents{Enable: []string{}, Default: "helper"}, ada)
	setAgents(t, s, "Initech", projects.Agents{Enable: []string{"helper"}, Allow: []string{"helper", "writer"}}, ada)
	// An allow list above the new home still decides there: it does not count.
	setAgents(t, s, "Field", projects.Agents{Enable: []string{}, Allow: []string{"helper", "writer"}}, ada)
	err := s.SetHome("acme", "helper", "Globex", admin, t0, nil)
	if !errors.Is(err, projects.ErrInUse) {
		t.Fatalf("SetHome = %v, want ErrInUse", err)
	}
	for _, want := range []string{"the default of Launch", "the enable list of Initech", "the allow list of Initech"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%q does not name %s", err, want)
		}
	}
	if strings.Contains(err.Error(), "Field") {
		t.Errorf("%q names the allow list above the home", err)
	}
	if _, homed := s.Tree("acme").Home("helper"); homed {
		t.Fatal("a refused home was kept")
	}

	// What the platform knows of is asked too, with the tree as it would be.
	s = field(t)
	var sawBefore, sawAfter bool
	err = s.SetHome("acme", "helper", "Globex", admin, t0, func(before, after *projects.Tree) []string {
		sawBefore = before.IsAvailable("Launch", "helper")
		sawAfter = !after.IsAvailable("Launch", "helper") && after.IsAvailable("Renewals", "helper")
		return []string{"the schedule nightly in Launch"}
	})
	if !errors.Is(err, projects.ErrInUse) || !strings.Contains(err.Error(), "the schedule nightly in Launch") || !sawBefore || !sawAfter {
		t.Fatalf("SetHome = %v, before %v, after %v", err, sawBefore, sawAfter)
	}
}

func TestHomingInAndOut(t *testing.T) {
	s := field(t)
	mustHome(t, s, "helper", "Globex")
	mustHome(t, s, "helper", "Field")
	tree := s.Tree("acme")
	if home, _ := tree.Home("helper"); home != "Field" || !tree.IsAvailable("Initech", "helper") || len(tree.Homes("Globex")) != 0 {
		t.Fatalf("after a move: home %q, homes in Globex %v", home, tree.Homes("Globex"))
	}
	h, _ := s.History("acme", "Globex", 0, 0)
	if h[0].What != projects.HomeChanged || h[0].Agent != "helper" || h[0].Note != projects.LeftNote {
		t.Fatalf("the old home's change = %+v", h[0])
	}
	h, _ = s.History("acme", "Field", 0, 0)
	if h[0].What != projects.HomeChanged || h[0].Note != projects.LivesHereNote {
		t.Fatalf("the new home's change = %+v", h[0])
	}
	if err := s.DropHome("acme", "helper", admin, t0, nil); err != nil {
		t.Fatal(err)
	}
	if tree := s.Tree("acme"); !tree.IsAvailable(projects.Root, "helper") || !tree.IsAvailable("Launch", "helper") {
		t.Fatal("a platform agent again is not available where * is")
	}
	// Doing nothing writes nothing.
	n, _ := s.History("acme", "Field", 0, 0)
	if err := s.DropHome("acme", "helper", admin, t0, nil); err != nil {
		t.Fatal(err)
	}
	if again, _ := s.History("acme", "Field", 0, 0); len(again) != len(n) {
		t.Fatal("dropping no home recorded a change")
	}
	for _, c := range []struct {
		agent, project string
		want           error
	}{
		{"helper", "Nowhere", projects.ErrNotFound},
		{projects.Auto, "Globex", projects.ErrInvalid},
		{"Not A Name", "Globex", projects.ErrInvalid},
	} {
		if err := s.SetHome("acme", c.agent, c.project, admin, t0, nil); !errors.Is(err, c.want) {
			t.Errorf("SetHome(%s, %s) = %v, want %v", c.agent, c.project, err, c.want)
		}
	}
}

func TestACompanyProjectIsAHomeOnlyWithGovern(t *testing.T) {
	s := field(t)
	mustCreate(t, s, "acme", projects.Project{Name: "News", Kind: projects.Company}, admin)
	if err := s.SetHome("acme", "herald", "News", ada, t0, nil); !errors.Is(err, projects.ErrForbidden) {
		t.Fatalf("homing in a company project without govern = %v", err)
	}
	mustHome(t, s, "herald", "News")
	if err := s.SetHome("acme", "herald", "Field", ada, t0, nil); !errors.Is(err, projects.ErrForbidden) {
		t.Fatalf("leaving a company project without govern = %v", err)
	}
}

func TestAProjectAnAgentLivesInCannotBeDeleted(t *testing.T) {
	s := field(t)
	mustHome(t, s, "launch-bot", "Launch")
	err := s.Delete("acme", "Launch", ada, t0, nil)
	if !errors.Is(err, projects.ErrInUse) || !strings.Contains(err.Error(), "launch-bot") {
		t.Fatalf("Delete = %v, want ErrInUse naming launch-bot", err)
	}
	if err := s.DropHome("acme", "launch-bot", ada, t0, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete("acme", "Launch", ada, t0, nil); err != nil {
		t.Fatalf("Delete after the agent left = %v", err)
	}
}

func TestHomesAreConfinedToATenant(t *testing.T) {
	s := field(t)
	mustCreate(t, s, "globex", team("Field", ""), ada)
	mustHome(t, s, "helper", "Globex")
	if _, homed := s.Tree("globex").Home("helper"); homed || !s.Tree("globex").IsAvailable("Field", "helper") {
		t.Fatal("a home reached another tenant")
	}
	if err := s.SetHome("globex", "helper", "Globex", admin, t0, nil); !errors.Is(err, projects.ErrNotFound) {
		t.Fatalf("homing in another tenant's project = %v", err)
	}
	setAgents(t, s, projects.Root, projects.Agents{}, admin)
	if !s.Tree("globex").IsAvailable(projects.Root, "lead") {
		t.Fatal("one tenant's lists narrowed another")
	}
}
