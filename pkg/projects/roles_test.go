package projects_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/state"
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
	// An account is its team's: members only, its creator owning it.
	globex := mustCreate(t, s, "acme", projects.Project{Name: "Globex", Parent: "Field", Kind: projects.Account}, projects.Writer{By: "bo"})
	if want := members("bo=owner"); !slices.Equal(globex.Members, want) {
		t.Fatalf("an account = %+v, want %+v", globex.Members, want)
	}
	// A team under a members-only account is members only too.
	renewals := mustCreate(t, s, "acme", team("Renewals", "Globex"), ada)
	if want := members("ada=owner"); !slices.Equal(renewals.Members, want) {
		t.Fatalf("a team under an account = %+v, want %+v", renewals.Members, want)
	}
	// One the file declares, with no creator to own it, is opened to everyone
	// as a viewer, or only projects:govern would see it.
	if got := projects.DefaultMembers(projects.Account, "role:ops"); !slices.Equal(got, members("*=viewer")) {
		t.Fatalf("an account with no creator = %+v", got)
	}
	// Under a project everyone owns, nothing is members only, so an account
	// made with the default members is opened to everyone as a viewer, at
	// any depth below it.
	mustCreate(t, s, "acme", with(team("Platform", ""), members("*=owner", "ada=owner")), ada)
	mustCreate(t, s, "acme", team("Infra", "Platform"), ada)
	for _, parent := range []string{"Platform", "Infra"} {
		acct := mustCreate(t, s, "acme", projects.Project{Name: "Initech-" + parent, Parent: parent, Kind: projects.Account}, projects.Writer{By: "bo"})
		if want := members("*=viewer", "bo=owner"); !slices.Equal(acct.Members, want) {
			t.Fatalf("an account under %s, which everyone owns = %+v, want %+v", parent, acct.Members, want)
		}
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

// A project may be members only, read by its members alone: anyone else has
// no role in it and does not see it. Ownership flows down, so it cannot sit
// under a project everyone owns, and a project cannot be opened to everyone
// as owners while one below it is members only.
func TestAProjectMayBeMembersOnlyWhereNobodyOwnsAll(t *testing.T) {
	s := projects.NewStore()
	mustCreate(t, s, "acme", team("Field", ""), ada)
	secret := mustCreate(t, s, "acme", with(team("Secret", "Field"), members("ada=owner", "bo=member")), ada)
	if a := s.Tree("acme").Resolve("Secret", projects.Reader{Subject: "stranger"}); a.Role != projects.None {
		t.Fatalf("a stranger in a members-only project = %+v", a)
	}
	if a := s.Tree("acme").Resolve("Secret", projects.Reader{Subject: "bo"}); a.Role != projects.Member {
		t.Fatalf("a member of it = %+v", a)
	}
	if _, err := s.Update("acme", "Field", 1, ada, t0, func(p *projects.Project) error { p.Members = members("ada=owner"); return nil }); err != nil {
		t.Fatalf("closing a project nobody owns wholesale = %v", err)
	}
	// Everyone owning Open owns what is below it, so nothing there is members only.
	mustCreate(t, s, "acme", with(team("Open", ""), members("*=owner")), ada)
	if _, err := s.Create("acme", with(team("Hidden", "Open"), members("ada=owner")), ada, t0); !errors.Is(err, projects.ErrInvalid) {
		t.Fatalf("members only under a project everyone owns = %v", err)
	}
	if _, err := s.Update("acme", "Field", 2, ada, t0, func(p *projects.Project) error { p.Members = members("*=owner"); return nil }); !errors.Is(err, projects.ErrInvalid) {
		t.Fatalf("everyone owning a project with %s below it = %v", secret.Name, err)
	}
	if _, err := s.Create("acme", with(projects.Project{Name: "News", Kind: projects.Company}, members("*=viewer", "root=owner")), admin, t0); !errors.Is(err, projects.ErrInvalid) {
		t.Fatalf("a company project everyone only views = %v", err)
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

// A person's own projects are the ones that name them: a member or owner
// entry for their subject or a role they hold, there or as owner above.
// Everyone's entry and projects:govern name nobody, even where they grant
// more than the entry that does, and an entry on a project hidden from the
// reader without projects:govern names nobody either.
func TestNamedIsWhereAnEntryNamesTheReader(t *testing.T) {
	s := projects.NewStore()
	mustCreate(t, s, "acme", with(team("Field", ""), members("*=member", "role:field-eng=member", "ada=owner")), ada)
	mustCreate(t, s, "acme", with(projects.Project{Name: "Globex", Parent: "Field", Kind: projects.Account}, members("*=viewer", "cy=member")), ada)
	mustCreate(t, s, "acme", with(team("Renewals", "Globex"), members("*=viewer", "fay=viewer")), ada)
	mustCreate(t, s, "acme", with(team("Platform", ""), members("*=owner", "bo=member")), ada)
	mustCreate(t, s, "acme", with(team("Secret", ""), members("ed=owner")), ada)
	mustCreate(t, s, "acme", with(team("Inner", "Secret"), members("ed=owner", "ana=member")), ada)
	tree := s.Tree("acme")
	for _, c := range []struct {
		name string
		r    projects.Reader
		want []string
	}{
		{"an owner by name owns everything below too", projects.Reader{Subject: "ada"}, []string{"Field", "Globex", "Renewals"}},
		{"a member by name, and not where everyone is", projects.Reader{Subject: "cy"}, []string{"Globex"}},
		{"a member by a role they hold", projects.Reader{Subject: "dee", Roles: []string{"field-eng"}}, []string{"Field"}},
		{"a machine holds no role", projects.Reader{Subject: "dee", Roles: []string{"field-eng"}, Machine: true}, []string{}},
		{"a viewer by name is not named", projects.Reader{Subject: "fay"}, []string{}},
		{"named as a member where everyone owns", projects.Reader{Subject: "bo"}, []string{"Platform"}},
		{"govern names nobody", projects.Reader{Subject: "bo", Govern: true}, []string{"Platform"}},
		{"govern alone is no project", projects.Reader{Subject: "root", Govern: true}, []string{}},
		{"an entry under a project hidden from them names nobody", projects.Reader{Subject: "ana", Govern: true}, []string{}},
		{"an owner of a members-only project", projects.Reader{Subject: "ed"}, []string{"Secret", "Inner"}},
		{"everyone's entries alone are nobody's", projects.Reader{Subject: "gil"}, []string{}},
		{"no subject", projects.Reader{Roles: []string{"field-eng"}}, []string{}},
	} {
		if got := tree.Named(c.r); !slices.Equal(got, c.want) {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

// What a deleted project held is read by whom it was read by when it was
// deleted, under its parent as the parent stands now: a members-only child's
// members still read it and nobody else does, and a parent made members only
// since narrows an open child's readers to the parent's. A tombstone that kept
// no members is read only with projects:govern.
func TestADeletedProjectIsReadByWhoReadItThen(t *testing.T) {
	s := projects.NewStore()
	now := time.Now()
	mustCreate(t, s, "acme", with(team("Field", ""), members("*=member", "ada=owner")), ada)
	mustCreate(t, s, "acme", with(team("Vault", "Field"), members("ada=owner", "cy=member")), ada)
	mustCreate(t, s, "acme", with(team("Open", "Field"), members("*=viewer", "ada=owner")), ada)
	for _, name := range []string{"Vault", "Open"} {
		if err := s.Delete("acme", name, ada, now, nil); err != nil {
			t.Fatal(err)
		}
	}
	tomb := func(name string) (projects.Tombstone, bool) { return s.Tombstone("acme", name) }
	role := func(name, who string) projects.Role {
		x, ok := tomb(name)
		if !ok {
			t.Fatalf("%s left no tombstone", name)
		}
		return s.Tree("acme").DeletedRole(x, projects.Reader{Subject: who}, tomb).Role
	}
	for _, c := range []struct {
		project, who string
		want         projects.Role
	}{
		{"Vault", "cy", projects.Member},
		{"Vault", "bo", projects.None},
		{"Vault", "ada", projects.Owner},
		{"Open", "bo", projects.Viewer},
	} {
		if got := role(c.project, c.who); got != c.want {
			t.Errorf("%s in deleted %s = %q, want %q", c.who, c.project, got, c.want)
		}
	}

	// Field turns members only: Open's old readers outside it read nothing.
	if _, err := s.Update("acme", "Field", 1, ada, now, func(p *projects.Project) error {
		p.Members = members("ada=owner", "dee=member")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if got := role("Open", "bo"); got != projects.None {
		t.Errorf("after Field closed, bo reads deleted Open as %q", got)
	}
	if got := role("Open", "dee"); got != projects.Viewer {
		t.Errorf("after Field closed, dee reads deleted Open as %q", got)
	}

	old := projects.Tombstone{Name: "Before", At: now}
	if got := s.Tree("acme").DeletedRole(old, projects.Reader{Subject: "ada"}, tomb); got.Role != projects.None {
		t.Errorf("a tombstone with no members reads as %+v", got)
	}
	if got := s.Tree("acme").DeletedRole(old, projects.Reader{Subject: "root", Govern: true}, tomb); got.Role != projects.Owner {
		t.Errorf("govern reads an old tombstone as %+v", got)
	}
}
