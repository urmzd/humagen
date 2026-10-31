package projects_test

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/state"
)

func TestRacingMovesNeverMakeACycle(t *testing.T) {
	st := state.NewMemory()
	s, err := projects.Open(st)
	if err != nil {
		t.Fatal(err)
	}
	mustCreate(t, s, "acme", team("A", ""), ada)
	mustCreate(t, s, "acme", team("B", ""), ada)
	var wg sync.WaitGroup
	for i := range 40 {
		wg.Go(func() {
			name, under := "A", "B"
			if i%2 == 1 {
				name, under = "B", "A"
			}
			// Each racer reads the version it moves from, as a caller does.
			p, err := s.Get("acme", name)
			if err != nil {
				t.Error(err)
				return
			}
			_, err = s.Move("acme", name, p.Version, under, ada, t0, nil)
			if err != nil && !errors.Is(err, projects.ErrVersion) && !errors.Is(err, projects.ErrInvalid) {
				t.Errorf("move %s under %s: %v", name, under, err)
			}
		})
	}
	wg.Wait()
	a, _ := s.Get("acme", "A")
	b, _ := s.Get("acme", "B")
	if (a.Parent == "B") == (b.Parent == "A") {
		t.Fatalf("A is under %q and B under %q; want exactly one under the other", a.Parent, b.Parent)
	}
	if tree := s.Tree("acme"); len(tree.Subtree("")) != tree.Len()+1 {
		t.Fatal("a project fell out of the tree")
	}
	// What was written is a tree: OpenDefault refuses a cycle.
	if _, err := projects.Open(st); err != nil {
		t.Fatalf("after racing moves: %v", err)
	}
}

func TestAMoveCountsTheHeightOfWhatMoves(t *testing.T) {
	s := projects.NewStore()
	for _, p := range [][2]string{{"L1", ""}, {"L2", "L1"}, {"L3", "L2"}, {"X", ""}, {"Y", "X"}} {
		mustCreate(t, s, "acme", team(p[0], p[1]), ada)
	}
	if h := s.Tree("acme").Height("L1"); h != 2 {
		t.Fatalf("height of L1 = %d", h)
	}
	// Y is at depth 2, and L1 brings two levels: 2 + 1 + 2 is past 4.
	if _, err := s.Move("acme", "L1", 1, "Y", ada, t0, nil); !errors.Is(err, projects.ErrInvalid) {
		t.Fatalf("moving three levels under depth 2 = %v", err)
	}
	moved, err := s.Move("acme", "L1", 1, "X", ada, t0, nil)
	if err != nil || moved.Parent != "X" || moved.Version != 2 {
		t.Fatalf("moving under depth 1 = %+v, %v", moved, err)
	}
	tree := s.Tree("acme")
	if tree.Depth("L3") != 4 || !slices.Equal(tree.Ancestors("L3"), []string{"L2", "L1", "X"}) {
		t.Fatalf("L3 is at depth %d under %q", tree.Depth("L3"), tree.Ancestors("L3"))
	}
	// A project cannot go under itself or anything below it.
	for _, under := range []string{"L1", "L3"} {
		if _, err := s.Move("acme", "L1", 2, under, ada, t0, nil); !errors.Is(err, projects.ErrInvalid) {
			t.Fatalf("moving L1 under %s = %v", under, err)
		}
	}
}

func TestMoveIsCheckedAndRecorded(t *testing.T) {
	s := projects.NewStore()
	mustCreate(t, s, "acme", team("Field", ""), ada)
	mustCreate(t, s, "acme", projects.Project{Name: "Globex", Parent: "Field", Kind: projects.Account}, ada)
	mustCreate(t, s, "acme", projects.Project{Name: "Initech", Kind: projects.Account}, ada)
	mustCreate(t, s, "acme", team("Platform", ""), ada)
	mustCreate(t, s, "acme", projects.Project{Name: "News", Kind: projects.Company}, admin)
	for _, c := range []struct {
		name        string
		project, to string
		version     int
		w           projects.Writer
		want        error
	}{
		{"the root", "", "Field", 1, admin, projects.ErrInvalid},
		{"a missing project", "Nowhere", "Field", 1, ada, projects.ErrNotFound},
		{"a stale version", "Globex", "Platform", 2, ada, projects.ErrVersion},
		{"a missing parent", "Globex", "Nowhere", 1, ada, projects.ErrInvalid},
		{"an account under an account", "Globex", "Initech", 1, ada, projects.ErrInvalid},
		{"a company project under a team", "News", "Platform", 1, admin, projects.ErrInvalid},
		{"a company project without govern", "News", "", 1, ada, projects.ErrForbidden},
	} {
		if _, err := s.Move("acme", c.project, c.version, c.to, c.w, t0, nil); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", c.name, err, c.want)
		}
	}
	// Going nowhere new changes nothing.
	if same, err := s.Move("acme", "Globex", 1, "Field", ada, t0, nil); err != nil || same.Version != 1 {
		t.Fatalf("a move to the same parent = %+v, %v", same, err)
	}
	// leaving hears the tree before the move and after it, and the
	// ancestors left behind, under the lock, and can refuse.
	mustCreate(t, s, "acme", team("Renewals", "Globex"), ada)
	var heard []string
	refuse := func(before, after *projects.Tree, left []string) error {
		if p, _ := before.Get("Renewals"); p.Parent != "Globex" {
			t.Errorf("the tree before the move holds Renewals under %q", p.Parent)
		}
		if p, _ := after.Get("Renewals"); p.Parent != "Platform" {
			t.Errorf("the tree after the move holds Renewals under %q", p.Parent)
		}
		heard = left
		return fmt.Errorf("%w: leaving %s needs billing:write", projects.ErrForbidden, strings.Join(left, ", "))
	}
	if _, err := s.Move("acme", "Renewals", 1, "Platform", ada, t0, refuse); !errors.Is(err, projects.ErrForbidden) {
		t.Fatalf("a refused leave = %v", err)
	}
	if !slices.Equal(heard, []string{"Globex", "Field"}) {
		t.Fatalf("left behind %q", heard)
	}
	if r, _ := s.Get("acme", "Renewals"); r.Parent != "Globex" || r.Version != 1 {
		t.Fatalf("a refused move moved: %+v", r)
	}
	// Going down within the chain leaves nothing, and leaving is still
	// asked, since new projects come above it: a limit below can change
	// hands.
	var left []string
	asked := false
	if _, err := s.Move("acme", "Platform", 1, "Field", ada, t0, func(_, _ *projects.Tree, l []string) error { asked, left = true, l; return nil }); err != nil || !asked || len(left) != 0 {
		t.Fatalf("moving under a sibling = %v, asked %v with %q", err, asked, left)
	}
	h, _, _ := s.History("acme", "Platform", 1, 0)
	if h[0].What != projects.Moved || h[0].Before.Parent != "" || h[0].After.Parent != "Field" {
		t.Fatalf("recorded %+v", h[0])
	}
	// A parent the file sets is the file's to change.
	if _, err := s.Upsert("acme", projects.Project{Name: "Platform", Parent: "Field"}, []string{projects.FieldParent}, "file", t0); err != nil {
		t.Fatal(err)
	}
	p, _ := s.Get("acme", "Platform")
	if _, err := s.Move("acme", "Platform", p.Version, "", ada, t0, nil); !errors.Is(err, projects.ErrManaged) {
		t.Fatalf("moving a parent the file sets = %v", err)
	}
	// The file moves it the way the console would, under the same checks.
	if _, err := s.Upsert("acme", projects.Project{Name: "Platform", Parent: "Platform"}, []string{projects.FieldParent}, "file", t0); !errors.Is(err, projects.ErrInvalid) {
		t.Fatalf("the file putting a project under itself = %v", err)
	}
	back, err := s.Upsert("acme", projects.Project{Name: "Platform"}, []string{projects.FieldParent}, "file", t0)
	if err != nil || back.Parent != "" {
		t.Fatalf("the file moving a project = %+v, %v", back, err)
	}
}

func TestAKindInUseCannotBeRemoved(t *testing.T) {
	s := projects.NewStore()
	partner := projects.KindSpec{Name: "partner", Title: "Partner", Like: projects.Account}
	guild := projects.KindSpec{Name: "guild", Title: "Guild", Like: projects.Team}
	setKinds := func(version int, ks ...projects.KindSpec) (projects.Project, error) {
		return s.Update("acme", "", version, admin, t0, func(p *projects.Project) error { p.Kinds = ks; return nil })
	}
	if _, err := s.Create("acme", projects.Project{Name: "Globex", Kind: "partner"}, ada, t0); !errors.Is(err, projects.ErrInvalid) {
		t.Fatalf("a kind the workspace does not define = %v", err)
	}
	if _, err := s.Update("acme", "", 0, ada, t0, func(p *projects.Project) error { p.Kinds = []projects.KindSpec{partner}; return nil }); !errors.Is(err, projects.ErrForbidden) {
		t.Fatalf("kinds without govern = %v", err)
	}
	root, err := setKinds(0, partner, guild)
	if err != nil || root.Version != 1 {
		t.Fatalf("set kinds = %+v, %v", root, err)
	}
	tree := s.Tree("acme")
	if tree.Like("partner") != projects.Account || tree.Like("guild") != projects.Team || tree.Like(projects.Company) != projects.Company || tree.Like("nope") != "" {
		t.Fatal("Like does not read the workspace's kinds")
	}
	mustCreate(t, s, "acme", team("Field", ""), ada)
	// A partner behaves as an account: it carries one, is members only by
	// default, and cannot sit under another account-like project.
	globex := mustCreate(t, s, "acme", projects.Project{Name: "Globex", Parent: "Field", Kind: "partner", Account: &projects.AccountInfo{Customer: "Globex Inc"}}, ada)
	if want := members("ada=owner"); !slices.Equal(globex.Members, want) {
		t.Fatalf("a partner's default members = %+v", globex.Members)
	}
	if _, err := s.Create("acme", projects.Project{Name: "Sub", Parent: "Globex", Kind: projects.Account}, ada, t0); !errors.Is(err, projects.ErrInvalid) {
		t.Fatalf("an account under a partner = %v", err)
	}
	if _, err := s.Create("acme", projects.Project{Name: "Ops", Kind: "guild", Account: &projects.AccountInfo{Customer: "x"}}, ada, t0); !errors.Is(err, projects.ErrInvalid) {
		t.Fatalf("an account on a team-like kind = %v", err)
	}
	err = func() error { _, err := setKinds(root.Version, guild); return err }()
	if !errors.Is(err, projects.ErrInUse) || !strings.Contains(err.Error(), "Globex") {
		t.Fatalf("removing a kind in use = %v", err)
	}
	if _, err := setKinds(root.Version, projects.KindSpec{Name: "partner", Like: projects.Team}, guild); !errors.Is(err, projects.ErrInUse) {
		t.Fatalf("changing what a kind in use behaves like = %v", err)
	}
	// Its title changes freely, and an unused kind goes.
	if root, err = setKinds(root.Version, projects.KindSpec{Name: "partner", Title: "Partners", Like: projects.Account}); err != nil {
		t.Fatalf("renaming a kind, dropping an unused one: %v", err)
	}
	for name, ks := range map[string][]projects.KindSpec{
		"a built-in name": {{Name: projects.Team, Like: projects.Team}},
		"a bad name":      {{Name: "Partner", Like: projects.Account}},
		"like company":    {{Name: "board", Like: projects.Company}},
		"twice":           {partner, partner},
		"a long title":    {{Name: "x", Title: strings.Repeat("t", projects.MaxKindTitle+1), Like: projects.Team}},
	} {
		if _, err := setKinds(root.Version, ks...); !errors.Is(err, projects.ErrInvalid) {
			t.Errorf("%s: %v, want ErrInvalid", name, err)
		}
	}
	var many []projects.KindSpec
	for i := range projects.MaxKinds + 1 {
		many = append(many, projects.KindSpec{Name: projects.Kind(fmt.Sprintf("k%d", i)), Like: projects.Team})
	}
	if _, err := setKinds(root.Version, many...); !errors.Is(err, projects.ErrInvalid) {
		t.Fatalf("%d kinds = %v", len(many), err)
	}
	if _, err := s.Update("acme", "Field", 1, ada, t0, func(p *projects.Project) error { p.Kinds = []projects.KindSpec{guild}; return nil }); !errors.Is(err, projects.ErrInvalid) {
		t.Fatalf("kinds below the root = %v", err)
	}
}

func TestAKindChangeFollowsTheKindRules(t *testing.T) {
	s := projects.NewStore(projects.WithCaps(projects.Caps{Company: 1}))
	mustCreate(t, s, "acme", team("Field", ""), ada)
	mustCreate(t, s, "acme", projects.Project{Name: "Globex", Parent: "Field", Kind: projects.Account, Account: &projects.AccountInfo{Customer: "Globex Inc"}}, ada)
	mustCreate(t, s, "acme", team("Platform", ""), ada)
	kind := func(name string, version int, w projects.Writer, k projects.Kind) error {
		_, err := s.Update("acme", name, version, w, t0, func(p *projects.Project) error { p.Kind = k; return nil })
		return err
	}
	if err := kind("Field", 1, ada, projects.Account); !errors.Is(err, projects.ErrInvalid) {
		t.Fatalf("an account over an account = %v", err)
	}
	if err := kind("Globex", 1, ada, projects.Team); !errors.Is(err, projects.ErrInvalid) {
		t.Fatalf("a team keeping its account = %v", err)
	}
	if err := kind("Platform", 1, ada, projects.Company); !errors.Is(err, projects.ErrForbidden) {
		t.Fatalf("making a company project without govern = %v", err)
	}
	if err := kind("Platform", 1, admin, projects.Company); err != nil {
		t.Fatalf("making a company project: %v", err)
	}
	if err := kind("Field", 1, admin, projects.Company); !errors.Is(err, projects.ErrLimit) {
		t.Fatalf("past the company cap = %v", err)
	}
	if err := kind("Platform", 2, ada, projects.Team); !errors.Is(err, projects.ErrForbidden) {
		t.Fatalf("a company project changed without govern = %v", err)
	}
	h, _, _ := s.History("acme", "Platform", 1, 0)
	if h[0].What != projects.Updated || h[0].After.Kind != projects.Company {
		t.Fatalf("recorded %+v", h[0])
	}
}

func TestArchivedProjectsTakeNoChildren(t *testing.T) {
	s := projects.NewStore()
	mustCreate(t, s, "acme", team("Field", ""), ada)
	mustCreate(t, s, "acme", team("Globex", "Field"), ada)
	mustCreate(t, s, "acme", team("Platform", ""), ada)
	archive := func(name string, version int, on bool) (projects.Project, error) {
		return s.Update("acme", name, version, ada, t0, func(p *projects.Project) error { p.Archived = on; return nil })
	}
	field, err := archive("Field", 1, true)
	if err != nil || !field.Archived {
		t.Fatalf("archive = %+v, %v", field, err)
	}
	if h, _, _ := s.History("acme", "Field", 1, 0); h[0].What != projects.Archived {
		t.Fatalf("recorded %q", h[0].What)
	}
	for _, under := range []string{"Field", "Globex"} {
		if _, err := s.Create("acme", team("New", under), ada, t0); !errors.Is(err, projects.ErrInvalid) {
			t.Errorf("creating under %s = %v", under, err)
		}
	}
	if _, err := s.Move("acme", "Platform", 1, "Globex", ada, t0, nil); !errors.Is(err, projects.ErrInvalid) {
		t.Fatalf("moving under an archived chain = %v", err)
	}
	if at, ok := s.Tree("acme").ArchivedOn("Globex"); !ok || at != "Field" {
		t.Fatalf("ArchivedOn = %q, %v", at, ok)
	}
	if _, err := s.Update("acme", "", 1, admin, t0, func(p *projects.Project) error { p.Archived = true; return nil }); !errors.Is(err, projects.ErrInvalid) {
		t.Fatalf("archiving the root = %v", err)
	}
	if _, err := s.Create("acme", projects.Project{Name: "Late", Archived: true}, ada, t0); !errors.Is(err, projects.ErrInvalid) {
		t.Fatalf("creating an archived project = %v", err)
	}
	if _, err := archive("Field", field.Version, false); err != nil {
		t.Fatal(err)
	}
	mustCreate(t, s, "acme", team("New", "Globex"), ada)
}

func TestTheRecordNamesMembersAndUpdates(t *testing.T) {
	s := projects.NewStore()
	mustCreate(t, s, "acme", team("Field", ""), ada)
	if _, err := s.Update("acme", "Field", 1, ada, t0, func(p *projects.Project) error {
		p.Members = members("*=member", "ada=owner", "bo=member")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Update("acme", "Field", 2, ada, t0, func(p *projects.Project) error { p.Announce = projects.Member; return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Update("acme", "Field", 3, ada, t0, func(p *projects.Project) error { p.Announce = "admin"; return nil }); !errors.Is(err, projects.ErrInvalid) {
		t.Fatalf("an unknown role to announce = %v", err)
	}
	h, _, _ := s.History("acme", "Field", 0, 0)
	if len(h) != 3 || h[0].What != projects.Updated || h[0].After.Announce != projects.Member || h[1].What != projects.Members {
		t.Fatalf("history = %+v", h)
	}
	// Members the file sets are the file's.
	if _, err := s.Upsert("acme", projects.Project{Name: "Field", Members: members("*=viewer", "role:field-leads=owner")}, []string{projects.FieldMembers}, "file", t0); err != nil {
		t.Fatal(err)
	}
	p, _ := s.Get("acme", "Field")
	if _, err := s.Update("acme", "Field", p.Version, ada, t0, func(p *projects.Project) error { p.Members = members("*=member"); return nil }); !errors.Is(err, projects.ErrManaged) {
		t.Fatalf("members the file sets = %v", err)
	}
}

func TestAWorkspaceHoldsFiveHundredProjects(t *testing.T) {
	s := projects.NewStore()
	if projects.DefaultCaps().Projects != 500 {
		t.Fatalf("default cap = %d", projects.DefaultCaps().Projects)
	}
	for i := range 5 {
		parent := fmt.Sprintf("Team %d", i)
		mustCreate(t, s, "acme", team(parent, ""), ada)
		for j := range 99 {
			mustCreate(t, s, "acme", team(fmt.Sprintf("Team %d-%02d", i, j), parent), ada)
		}
	}
	if _, err := s.Create("acme", team("One more", ""), ada, t0); !errors.Is(err, projects.ErrLimit) {
		t.Fatalf("the 501st project = %v", err)
	}
}

func TestPagesKeepTreeOrderAcrossDeletes(t *testing.T) {
	s := projects.NewStore()
	mustCreate(t, s, "acme", team("Field", ""), ada)
	mustCreate(t, s, "acme", projects.Project{Name: "Globex", Parent: "Field", Kind: projects.Account, Account: &projects.AccountInfo{Customer: "Globex Incorporated"}}, ada)
	mustCreate(t, s, "acme", projects.Project{Name: "Initech", Parent: "Field", Kind: projects.Account, Description: "TPS reports"}, ada)
	mustCreate(t, s, "acme", team("Renewals", "Globex"), ada)
	mustCreate(t, s, "acme", projects.Project{Name: "Platform", Title: "Platform engineering"}, ada)
	all := []string{"Field", "Globex", "Renewals", "Initech", "Platform"}
	names := func(ps []projects.Project) []string {
		out := []string{}
		for _, p := range ps {
			out = append(out, p.Name)
		}
		return out
	}
	first, next, err := s.Tree("acme").Page(projects.Query{Limit: 2})
	if err != nil || !slices.Equal(names(first), all[:2]) || next == "" {
		t.Fatalf("first page = %q, %q, %v", names(first), next, err)
	}
	// The last row of the page goes; the next page starts where it was.
	if err := s.Delete("acme", "Renewals", ada, t0, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete("acme", "Globex", ada, t0, nil); err != nil {
		t.Fatal(err)
	}
	rest, end, err := s.Tree("acme").Page(projects.Query{Limit: 2, After: next})
	if err != nil || !slices.Equal(names(rest), []string{"Initech", "Platform"}) || end != "" {
		t.Fatalf("after a delete = %q, %q, %v", names(rest), end, err)
	}
	mustCreate(t, s, "acme", projects.Project{Name: "Globex", Parent: "Field", Kind: projects.Account, Account: &projects.AccountInfo{Customer: "Globex Incorporated"}}, admin)
	tree := s.Tree("acme")
	for q, want := range map[string][]string{
		"globex inc": {"Globex"},
		"tps":        {"Initech"},
		"ENGINEER":   {"Platform"},
		"field":      {"Field"},
		"nothing":    {},
	} {
		got, _, err := tree.Page(projects.Query{Q: q})
		if err != nil || !slices.Equal(names(got), want) {
			t.Errorf("search %q = %q, %v, want %q", q, names(got), err, want)
		}
	}
	if got, _, _ := tree.Page(projects.Query{Kind: projects.Account}); !slices.Equal(names(got), []string{"Globex", "Initech"}) {
		t.Fatalf("accounts = %q", names(got))
	}
	for _, bad := range []string{"not a cursor", "W10", "WyJ-cm9vdCJd"} {
		if _, _, err := tree.Page(projects.Query{After: bad}); !errors.Is(err, projects.ErrInvalid) {
			t.Errorf("cursor %q = %v", bad, err)
		}
	}
	if _, _, err := tree.Page(projects.Query{Q: strings.Repeat("q", projects.MaxQuery+1)}); !errors.Is(err, projects.ErrInvalid) {
		t.Fatalf("a long search = %v", err)
	}
	if got, _, _ := projects.NewStore().Tree("acme").Page(projects.Query{}); got == nil || len(got) != 0 {
		t.Fatalf("an empty workspace = %#v", got)
	}
}

// Ownership flows down, so a move never puts a members-only project, however
// deep below what moves, under a project everyone owns: that would open it to
// the whole workspace as owners.
func TestAMoveNeverPutsAMembersOnlyProjectUnderOneEveryoneOwns(t *testing.T) {
	s := projects.NewStore()
	mustCreate(t, s, "acme", with(team("OpenDefault", ""), members("*=owner")), ada)
	field := mustCreate(t, s, "acme", with(team("Field", ""), members("*=viewer", "ada=owner")), ada)
	mustCreate(t, s, "acme", with(team("Mid", "Field"), members("*=viewer", "ada=owner")), ada)
	mustCreate(t, s, "acme", with(team("Secret", "Mid"), members("ada=owner", "bo=member")), ada)
	stranger := projects.Reader{Subject: "stranger"}
	// Secret is two levels below Field and one below Mid.
	for _, name := range []string{"Field", "Mid"} {
		p, _ := s.Get("acme", name)
		_, err := s.Move("acme", name, p.Version, "OpenDefault", ada, t0, nil)
		if !errors.Is(err, projects.ErrInvalid) || !strings.Contains(err.Error(), "Secret") {
			t.Fatalf("moving %s, with Secret below it, under OpenDefault = %v", name, err)
		}
	}
	if a := s.Tree("acme").Resolve("Secret", stranger); a.Role != projects.None {
		t.Fatalf("after the refused moves a stranger in Secret = %+v", a)
	}
	// Once Secret names everyone, Field may go.
	if _, err := s.Update("acme", "Secret", 1, ada, t0, func(p *projects.Project) error {
		p.Members = members("*=viewer", "ada=owner")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Move("acme", "Field", field.Version, "OpenDefault", ada, t0, nil); err != nil {
		t.Fatalf("moving Field with nothing members only below it = %v", err)
	}
}
