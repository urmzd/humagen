package projects_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/urmzd/warp/pkg/projects"
	"github.com/urmzd/warp/pkg/state"
)

func members(entries ...string) []projects.Entry {
	out := []projects.Entry{}
	for _, e := range entries {
		who, role, _ := strings.Cut(e, "=")
		out = append(out, projects.Entry{Who: who, Role: projects.Role(role)})
	}
	return out
}

func with(p projects.Project, ms []projects.Entry) projects.Project {
	p.Members = ms
	return p
}

func TestAnOwnerAboveOwnsEverythingBelow(t *testing.T) {
	s := projects.NewStore()
	mustCreate(t, s, "acme", with(team("Field", ""), members("*=member", "ada=owner", "role:field-leads=owner")), ada)
	mustCreate(t, s, "acme", with(projects.Project{Name: "Globex", Parent: "Field", Kind: projects.Account}, members("*=viewer", "cy=member")), ada)
	mustCreate(t, s, "acme", with(team("Renewals", "Globex"), members("*=viewer")), ada)
	tree := s.Tree("acme")
	for _, c := range []struct {
		name    string
		project string
		r       projects.Reader
		want    projects.Access
	}{
		{"the owner of Field owns Renewals", "Renewals", projects.Reader{Subject: "ada"}, projects.Access{Role: projects.Owner, From: "Field", Via: projects.ViaOwnerAbove}},
		{"an owner by role owns Globex", "Globex", projects.Reader{Subject: "dee", Roles: []string{"field-leads"}}, projects.Access{Role: projects.Owner, From: "Field", Via: projects.ViaOwnerAbove}},
		{"Field's own entry names it", "Field", projects.Reader{Subject: "ada"}, projects.Access{Role: projects.Owner, From: "Field", Via: "ada"}},
		{"a member of Globex is a viewer below", "Renewals", projects.Reader{Subject: "cy"}, projects.Access{Role: projects.Viewer, From: "Renewals", Via: "*"}},
		{"everyone else sees Globex", "Globex", projects.Reader{Subject: "bo"}, projects.Access{Role: projects.Viewer, From: "Globex", Via: "*"}},
		{"govern owns everything", "Renewals", projects.Reader{Subject: "root", Govern: true}, projects.Access{Role: projects.Owner, From: "", Via: projects.ViaGovern}},
		{"govern owns the root", "", projects.Reader{Subject: "root", Govern: true}, projects.Access{Role: projects.Owner, From: "", Via: projects.ViaGovern}},
		{"everyone is a member of the root", "", projects.Reader{Subject: "bo"}, projects.Access{Role: projects.Member, From: "", Via: "*"}},
		{"a project the tree does not hold", "Nowhere", projects.Reader{Subject: "ada", Govern: true}, projects.Access{}},
	} {
		if got := tree.Resolve(c.project, c.r); got != c.want {
			t.Errorf("%s: %+v, want %+v", c.name, got, c.want)
		}
	}
	got := tree.InheritedOwners("Renewals")
	want := []projects.InheritedOwner{{Who: "ada", From: "Field"}, {Who: "role:field-leads", From: "Field"}}
	if !slices.Equal(got, want) {
		t.Fatalf("inherited owners = %+v, want %+v", got, want)
	}
	if got := tree.InheritedOwners(""); got == nil || len(got) != 0 {
		t.Fatalf("the root inherits %#v", got)
	}
}

// A level nobody lets the reader into closes everything below it, whatever
// the entries there say. The store refuses such a project until reads are
// confined, so the records are written as a later version would write them.
func TestNoRoleAtALevelMeansNoRoleBelow(t *testing.T) {
	st := state.NewMemory()
	put := func(p projects.Project) {
		t.Helper()
		raw, _ := json.Marshal(p)
		if err := st.Write(context.Background(), "projects", state.Put("acme", "p:"+p.Name, raw)); err != nil {
			t.Fatal(err)
		}
	}
	put(projects.Project{Name: "", Kind: projects.Company, Version: 1, Members: members("*=member")})
	put(projects.Project{Name: "Field", Kind: projects.Team, Version: 1, Members: members("*=member")})
	put(projects.Project{Name: "Globex", Kind: projects.Account, Parent: "Field", Version: 1, Members: members("ada=owner")})
	put(projects.Project{Name: "Renewals", Kind: projects.Team, Parent: "Globex", Version: 1, Members: members("*=member", "bo=owner")})
	s, err := projects.Open(st)
	if err != nil {
		t.Fatal(err)
	}
	tree := s.Tree("acme")
	if got := tree.Resolve("Renewals", projects.Reader{Subject: "bo"}); got.Role != projects.None {
		t.Fatalf("bo has no role in Globex and %+v in Renewals", got)
	}
	if got := tree.Resolve("Globex", projects.Reader{Subject: "bo"}); got.Role != projects.None {
		t.Fatalf("bo in Globex = %+v", got)
	}
	if got := tree.Resolve("Renewals", projects.Reader{Subject: "ada"}); got.Role != projects.Owner {
		t.Fatalf("ada in Renewals = %+v", got)
	}
	// A reader with no subject matches nothing, everyone's entry included.
	if got := tree.Resolve("Field", projects.Reader{Roles: []string{"admin"}, Govern: true}); got.Role != projects.None {
		t.Fatalf("no subject = %+v", got)
	}
	if got := tree.Memberships(projects.Reader{}); len(got) != 0 {
		t.Fatalf("no subject belongs to %q", got)
	}
}

func TestAMachineMatchesOnlyStarAndItself(t *testing.T) {
	s := projects.NewStore()
	mustCreate(t, s, "acme", with(team("Field", ""), members("*=viewer", "role:operator=owner", "slack-bot=member")), ada)
	tree := s.Tree("acme")
	for _, c := range []struct {
		name string
		r    projects.Reader
		want projects.Role
	}{
		{"a person by role", projects.Reader{Subject: "ada", Roles: []string{"operator"}}, projects.Owner},
		{"a machine holding the role", projects.Reader{Subject: "ci", Roles: []string{"operator"}, Machine: true}, projects.Viewer},
		{"a machine by its subject", projects.Reader{Subject: "slack-bot", Roles: []string{"operator"}, Machine: true}, projects.Member},
		{"a machine named like a role", projects.Reader{Subject: "role:operator", Machine: true}, projects.Viewer},
	} {
		if got := tree.Resolve("Field", c.r); got.Role != c.want {
			t.Errorf("%s: %+v, want %s", c.name, got, c.want)
		}
	}
}

func TestMembersAreValidated(t *testing.T) {
	s := projects.NewStore()
	tooMany := members("*=member")
	for i := range projects.MaxMembers {
		tooMany = append(tooMany, projects.Entry{Who: fmt.Sprintf("user-%d", i), Role: projects.Viewer})
	}
	for name, ms := range map[string][]projects.Entry{
		"an unknown role":         members("*=member", "ada=admin"),
		"no role":                 members("*=member", "ada="),
		"one person twice":        members("*=member", "ada=owner", "ada=viewer"),
		"a role with no name":     members("*=member", "role:=owner"),
		"a role that is no name":  members("*=member", "role:field leads=owner"),
		"an empty who":            members("*=member", "=owner"),
		"a who on two lines":      members("*=member", "a\nb=owner"),
		"a who with spaces round": members("*=member", " ada=owner"),
		"too many":                tooMany,
	} {
		if _, err := s.Create("acme", with(team("Field", ""), ms), ada, t0); !errors.Is(err, projects.ErrInvalid) {
			t.Errorf("%s: %v, want ErrInvalid", name, err)
		}
	}
	if s.Tree("acme").Has("Field") {
		t.Fatal("a refused create kept the project")
	}
	got := mustCreate(t, s, "acme", with(team("Field", ""), members("*=member", "role:field-eng=member", "okta|00u1ana=owner")), ada)
	if len(got.Members) != 3 {
		t.Fatalf("members = %+v", got.Members)
	}
	// An update that drops the list is refused, not read as nobody.
	if _, err := s.Update("acme", "Field", 1, ada, t0, func(p *projects.Project) error { p.Members = nil; return nil }); !errors.Is(err, projects.ErrInvalid) {
		t.Fatalf("nil members = %v", err)
	}
}

func TestANewProjectGetsDefaultMembers(t *testing.T) {
	s := projects.NewStore()
	field := mustCreate(t, s, "acme", team("Field", ""), ada)
	if want := members("*=member", "ada=owner"); !slices.Equal(field.Members, want) {
		t.Fatalf("a team = %+v, want %+v", field.Members, want)
	}
	globex := mustCreate(t, s, "acme", projects.Project{Name: "Globex", Parent: "Field", Kind: projects.Account}, projects.Writer{By: "bo"})
	if want := members("*=viewer", "bo=owner"); !slices.Equal(globex.Members, want) {
		t.Fatalf("an account = %+v, want %+v", globex.Members, want)
	}
	// A team under an account is no more open than the account.
	renewals := mustCreate(t, s, "acme", team("Renewals", "Globex"), ada)
	if want := members("*=viewer", "ada=owner"); !slices.Equal(renewals.Members, want) {
		t.Fatalf("a team under an account = %+v, want %+v", renewals.Members, want)
	}
	news := mustCreate(t, s, "acme", projects.Project{Name: "News", Kind: projects.Company}, admin)
	if want := members("*=member", "root=owner"); !slices.Equal(news.Members, want) {
		t.Fatalf("a company project = %+v, want %+v", news.Members, want)
	}
	// A writer that cannot be an entry owns nothing.
	if got := projects.DefaultMembers(projects.Team, "role:ops"); !slices.Equal(got, members("*=member")) {
		t.Fatalf("a role as creator = %+v", got)
	}
	if root, _ := s.Get("acme", ""); !slices.Equal(root.Members, members("*=member")) {
		t.Fatalf("the root = %+v", root.Members)
	}
	if root, _ := s.Get("globex", ""); !slices.Equal(root.Members, members("*=member")) {
		t.Fatalf("an untouched root = %+v", root.Members)
	}
}

func TestAChildIsNeverMoreOpenThanItsParent(t *testing.T) {
	s := projects.NewStore()
	mustCreate(t, s, "acme", with(team("Field", ""), members("*=viewer", "ada=owner")), ada)
	if _, err := s.Create("acme", with(team("Wide", "Field"), members("*=member")), ada, t0); !errors.Is(err, projects.ErrInvalid) {
		t.Fatalf("a child more open than its parent = %v", err)
	}
	// Everyone owning counts as everyone a member.
	if _, err := s.Create("acme", with(team("Owned", "Field"), members("*=owner")), ada, t0); !errors.Is(err, projects.ErrInvalid) {
		t.Fatalf("everyone owning under everyone viewing = %v", err)
	}
	mustCreate(t, s, "acme", with(team("Platform", ""), members("*=owner")), ada)
	mustCreate(t, s, "acme", with(team("Infra", "Platform"), members("*=member")), ada)
	mustCreate(t, s, "acme", with(team("Edge", "Infra"), members("*=owner")), ada)
	// A parent cannot narrow below a child.
	if _, err := s.Update("acme", "Platform", 1, ada, t0, func(p *projects.Project) error {
		p.Members = members("*=viewer", "ada=owner")
		return nil
	}); !errors.Is(err, projects.ErrInvalid) || !strings.Contains(err.Error(), "Infra") {
		t.Fatalf("narrowing past a child = %v", err)
	}
	// A move that would widen who sees something is refused.
	if _, err := s.Move("acme", "Infra", 1, "Field", ada, t0, nil); !errors.Is(err, projects.ErrInvalid) {
		t.Fatalf("moving under a narrower parent = %v", err)
	}
	// The root is company-wide: everyone is at least a member of it.
	if _, err := s.Update("acme", "", 1, admin, t0, func(p *projects.Project) error { p.Members = members("*=viewer"); return nil }); !errors.Is(err, projects.ErrInvalid) {
		t.Fatalf("narrowing the root = %v", err)
	}
}

func TestEveryProjectStaysVisible(t *testing.T) {
	s := projects.NewStore()
	for name, ms := range map[string][]projects.Entry{
		"nobody":       members(),
		"owners only":  members("ada=owner"),
		"a role only":  members("role:field-eng=member", "ada=owner"),
		"no one reads": members("ada=owner", "bo=member"),
	} {
		if _, err := s.Create("acme", with(team("Secret", ""), ms), ada, t0); !errors.Is(err, projects.ErrConfinement) {
			t.Errorf("%s: %v, want ErrConfinement", name, err)
		}
	}
	mustCreate(t, s, "acme", team("Field", ""), ada)
	if _, err := s.Update("acme", "Field", 1, ada, t0, func(p *projects.Project) error { p.Members = members("ada=owner"); return nil }); !errors.Is(err, projects.ErrConfinement) {
		t.Fatalf("closing a project = %v", err)
	}
	if _, err := s.Create("acme", with(projects.Project{Name: "News", Kind: projects.Company}, members("*=viewer", "root=owner")), admin, t0); !errors.Is(err, projects.ErrInvalid) {
		t.Fatalf("a company project everyone only views = %v", err)
	}
	// Everyone who can see projects sees every one of them.
	mustCreate(t, s, "acme", projects.Project{Name: "Globex", Parent: "Field", Kind: projects.Account}, ada)
	tree := s.Tree("acme")
	for _, name := range tree.Subtree("") {
		if a := tree.Resolve(name, projects.Reader{Subject: "stranger"}); !a.Role.AtLeast(projects.Viewer) {
			t.Errorf("a stranger cannot see %q: %+v", name, a)
		}
	}
}

func TestMembershipsReachMembersAndTheirAncestors(t *testing.T) {
	s := projects.NewStore()
	mustCreate(t, s, "acme", with(team("Field", ""), members("*=viewer", "role:field-eng=member", "ada=owner")), ada)
	mustCreate(t, s, "acme", with(projects.Project{Name: "Globex", Parent: "Field", Kind: projects.Account}, members("*=viewer", "cy=member")), ada)
	mustCreate(t, s, "acme", with(projects.Project{Name: "Initech", Parent: "Field", Kind: projects.Account}, members("*=viewer")), ada)
	mustCreate(t, s, "acme", with(team("Platform", ""), members("*=viewer")), ada)
	tree := s.Tree("acme")
	for _, c := range []struct {
		name string
		r    projects.Reader
		want []string
	}{
		{"a member of an account hears it, Field and the root", projects.Reader{Subject: "cy"}, []string{"", "Field", "Globex"}},
		{"a member by role hears Field and the root", projects.Reader{Subject: "dee", Roles: []string{"field-eng"}}, []string{"", "Field"}},
		{"an owner above hears everything below", projects.Reader{Subject: "ada"}, []string{"", "Field", "Globex", "Initech"}},
		{"a stranger hears the root", projects.Reader{Subject: "bo"}, []string{""}},
		{"govern is not membership", projects.Reader{Subject: "root", Govern: true}, []string{""}},
	} {
		if got := tree.Memberships(c.r); !slices.Equal(got, c.want) {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}
