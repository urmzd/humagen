package projects_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/urmzd/warp/pkg/projects"
)

// A listing reads every row's role in one walk; it must answer what Resolve
// answers for each project, for every kind of reader.
func TestRolesAgreeWithResolve(t *testing.T) {
	s := projects.NewStore()
	mustCreate(t, s, "acme", with(team("Field", ""), members("*=member", "ada=owner", "role:field-leads=owner")), ada)
	mustCreate(t, s, "acme", with(projects.Project{Name: "Globex", Parent: "Field", Kind: projects.Account}, members("*=viewer", "cy=member", "bot=member")), ada)
	mustCreate(t, s, "acme", with(team("Renewals", "Globex"), members("*=viewer", "role:sales=member")), ada)
	mustCreate(t, s, "acme", team("Launch", ""), ada)
	tree := s.Tree("acme")
	for _, r := range []projects.Reader{
		{Subject: "ada"}, {Subject: "cy"}, {Subject: "dee", Roles: []string{"field-leads"}},
		{Subject: "eve", Roles: []string{"sales"}}, {Subject: "bot", Machine: true}, {Subject: "bot", Machine: true, Roles: []string{"sales"}},
		{Subject: "root", Govern: true}, {}, {Govern: true},
	} {
		all := tree.Roles(r)
		for _, name := range append([]string{""}, tree.Subtree("")[1:]...) {
			if got, want := all[name], tree.Resolve(name, r); got != want {
				t.Errorf("%+v in %q: Roles says %+v, Resolve %+v", r, name, got, want)
			}
		}
	}
}

// Who owns a project is decided by the tree when asked, so a proposal
// addressed to a project's owners reaches whoever owns it then.
func TestOwnedIsWhatTheReaderOwnsHereAndBelow(t *testing.T) {
	s := projects.NewStore()
	mustCreate(t, s, "acme", with(team("Field", ""), members("*=member", "ada=owner")), ada)
	mustCreate(t, s, "acme", with(team("Globex", "Field"), members("*=member", "cy=owner")), ada)
	mustCreate(t, s, "acme", with(team("Launch", ""), members("*=member", "cy=member")), ada)
	tree := s.Tree("acme")
	for _, c := range []struct {
		r    projects.Reader
		want []string
	}{
		{projects.Reader{Subject: "ada"}, []string{"Field", "Globex"}},
		{projects.Reader{Subject: "cy"}, []string{"Globex"}},
		{projects.Reader{Subject: "bo"}, []string{}},
		{projects.Reader{Subject: "root", Govern: true}, []string{"", "Field", "Globex", "Launch"}},
		{projects.Reader{Govern: true}, []string{}},
	} {
		if got := tree.Owned(c.r); !slices.Equal(got, c.want) {
			t.Errorf("%+v owns %q, want %q", c.r, got, c.want)
		}
	}
}

// Keep narrows a page without breaking its cursor: the rows it drops are
// skipped, and the next page starts after the last row kept.
func TestKeepNarrowsAPageAndItsCursor(t *testing.T) {
	s := projects.NewStore()
	for _, n := range []string{"A", "B", "C", "D", "E"} {
		mustCreate(t, s, "acme", team(n, ""), ada)
	}
	mustCreate(t, s, "acme", team("B1", "B"), ada)
	tree := s.Tree("acme")
	keep := func(p projects.Project) bool { return p.Name != "C" }
	var got []string
	after := ""
	for range 10 {
		page, next, err := tree.Page(projects.Query{Limit: 2, After: after, Keep: keep})
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range page {
			got = append(got, p.Name)
		}
		if next == "" {
			break
		}
		after = next
	}
	if want := []string{"A", "B", "B1", "D", "E"}; !slices.Equal(got, want) {
		t.Fatalf("pages = %q, want %q", got, want)
	}
}

// A default that stops being available is refused, never passed over, so a
// move that would leave one unavailable below it is refused instead, naming
// it, and the tree is as it was.
func TestAMoveNeverStrandsADefault(t *testing.T) {
	s := field(t)
	setAgents(t, s, "", projects.Agents{Enable: []string{}, Default: projects.Auto}, admin)
	setAgents(t, s, "Field", projects.Agents{Enable: []string{"field-helper"}}, admin)
	setAgents(t, s, "Globex", projects.Agents{Enable: []string{}, Default: "field-helper"}, admin)
	globex, _ := s.Get("acme", "Globex")
	_, err := s.Move("acme", "Globex", globex.Version, "Launch", admin, t0, nil)
	if !errors.Is(err, projects.ErrInUse) || !strings.Contains(err.Error(), "the default of Globex, field-helper") {
		t.Fatalf("a move that strands a default = %v", err)
	}
	if again, _ := s.Get("acme", "Globex"); again.Parent != "Field" || again.Version != globex.Version {
		t.Fatalf("a refused move changed Globex: %+v", again)
	}
	// Under a parent that offers the agent too, it moves.
	setAgents(t, s, "Launch", projects.Agents{Enable: []string{"field-helper"}}, admin)
	if _, err := s.Move("acme", "Globex", globex.Version, "Launch", admin, t0, nil); err != nil {
		t.Fatal(err)
	}
}

// Nor a channel's default below it, nor an agent that lives there, which
// would be available nowhere: a move under an allow list that leaves either
// out is refused, naming it, and moves nothing. An archived channel, which
// takes no new threads and whose default cannot change, holds no move back.
func TestAMoveNeverStrandsAChannelsDefaultOrAHomedAgent(t *testing.T) {
	s := field(t)
	setAgents(t, s, "Field", projects.Agents{Enable: []string{"field-helper"}}, admin)
	setAgents(t, s, "Launch", projects.Agents{Enable: []string{}, Allow: []string{"other"}}, admin)
	if _, err := s.CreateChannel("acme", projects.Channel{Project: "Renewals", Name: "q3", Default: "field-helper"}, admin, t0); err != nil {
		t.Fatal(err)
	}
	mustHome(t, s, "renewals-bot", "Renewals")
	globex, _ := s.Get("acme", "Globex")
	_, err := s.Move("acme", "Globex", globex.Version, "Launch", admin, t0, nil)
	if !errors.Is(err, projects.ErrInUse) || !strings.Contains(err.Error(), "the default of #q3 in Renewals, field-helper, would not be available") ||
		!strings.Contains(err.Error(), "renewals-bot, which lives in Renewals, would not be available there") {
		t.Fatalf("a move that strands a channel's default and a homed agent = %v", err)
	}
	if again, _ := s.Get("acme", "Globex"); again.Parent != "Field" || again.Version != globex.Version {
		t.Fatalf("a refused move changed Globex: %+v", again)
	}
	if _, err := s.ArchiveChannel("acme", "Renewals", "q3", admin, t0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Move("acme", "Globex", globex.Version, "Launch", admin, t0, nil); !errors.Is(err, projects.ErrInUse) || strings.Contains(err.Error(), "#q3") {
		t.Fatalf("a move that strands a homed agent alone = %v", err)
	}
	setAgents(t, s, "Launch", projects.Agents{Enable: []string{}, Allow: []string{"other", "renewals-bot"}}, admin)
	if _, err := s.Move("acme", "Globex", globex.Version, "Launch", admin, t0, nil); err != nil {
		t.Fatal(err)
	}
}
