package projects_test

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/projects"
)

var (
	t0    = time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	ada   = projects.Writer{By: "ada"}
	admin = projects.Writer{By: "root", Govern: true}
)

func team(name, parent string) projects.Project {
	return projects.Project{Name: name, Parent: parent}
}

func mustCreate(t *testing.T, s *projects.Store, tenant string, p projects.Project, w projects.Writer) projects.Project {
	t.Helper()
	got, err := s.Create(tenant, p, w, t0)
	if err != nil {
		t.Fatalf("create %q: %v", p.Name, err)
	}
	return got
}

func TestFortyRacersCreatingOneNameKeepOne(t *testing.T) {
	s := projects.NewStore()
	var wg sync.WaitGroup
	var won, lost atomic.Int64
	for i := range 40 {
		wg.Go(func() {
			// The same name, in every case a person might type it.
			name := "Launch"
			if i%2 == 1 {
				name = "LAUNCH"
			}
			_, err := s.Create("acme", team(name, ""), ada, t0)
			switch {
			case err == nil:
				won.Add(1)
			case errors.Is(err, projects.ErrExists):
				lost.Add(1)
			default:
				t.Errorf("create: %v", err)
			}
		})
	}
	wg.Wait()
	if won.Load() != 1 || lost.Load() != 39 || s.Tree("acme").Len() != 1 {
		t.Fatalf("%d won, %d lost, %d kept; want one of each name", won.Load(), lost.Load(), s.Tree("acme").Len())
	}
}

func TestTheCapHoldsUnderRacingCreates(t *testing.T) {
	s := projects.NewStore(projects.WithCaps(projects.Caps{Projects: 100}))
	var wg sync.WaitGroup
	var limited atomic.Int64
	for i := range 140 {
		wg.Go(func() {
			_, err := s.Create("acme", team(fmt.Sprintf("p-%03d", i), ""), ada, t0)
			if errors.Is(err, projects.ErrLimit) {
				limited.Add(1)
			} else if err != nil {
				t.Errorf("create: %v", err)
			}
		})
	}
	wg.Wait()
	if n := s.Tree("acme").Len(); n != 100 || limited.Load() != 40 {
		t.Fatalf("%d kept and %d refused, want 100 and 40", n, limited.Load())
	}
	// Another tenant has its own cap.
	if _, err := s.Create("globex", team("p-000", ""), ada, t0); err != nil {
		t.Fatalf("another tenant: %v", err)
	}
}

func TestDepthFiveAndTheHundredAndFirstChildAreInvalid(t *testing.T) {
	s := projects.NewStore(projects.WithCaps(projects.Caps{Projects: 200}))
	parent := ""
	for _, name := range []string{"L1", "L2", "L3", "L4"} {
		mustCreate(t, s, "acme", team(name, parent), ada)
		parent = name
	}
	if d := s.Tree("acme").Depth("L4"); d != 4 {
		t.Fatalf("depth of L4 = %d", d)
	}
	if _, err := s.Create("acme", team("L5", "L4"), ada, t0); !errors.Is(err, projects.ErrInvalid) {
		t.Fatalf("depth 5 = %v, want ErrInvalid", err)
	}
	// L1 has L2, and 99 more.
	for i := range 99 {
		mustCreate(t, s, "acme", team(fmt.Sprintf("c-%03d", i), "L1"), ada)
	}
	if _, err := s.Create("acme", team("c-100", "L1"), ada, t0); !errors.Is(err, projects.ErrInvalid) {
		t.Fatalf("the 101st child = %v, want ErrInvalid", err)
	}
}

func TestKindRules(t *testing.T) {
	s := projects.NewStore()
	mustCreate(t, s, "acme", projects.Project{Name: "News", Kind: projects.Company}, admin)
	mustCreate(t, s, "acme", team("Field", ""), ada)
	mustCreate(t, s, "acme", projects.Project{Name: "Globex", Kind: projects.Account, Parent: "Field",
		Account: &projects.AccountInfo{Customer: "Globex Inc", URL: "https://globex.example/crm"}}, ada)
	for _, c := range []struct {
		name string
		p    projects.Project
		w    projects.Writer
		want error
	}{
		{"company needs govern", projects.Project{Name: "Board", Kind: projects.Company}, ada, projects.ErrForbidden},
		{"company under a team", projects.Project{Name: "Board", Kind: projects.Company, Parent: "Field"}, admin, projects.ErrInvalid},
		{"account under an account", projects.Project{Name: "Sub", Kind: projects.Account, Parent: "Globex"}, ada, projects.ErrInvalid},
		{"account info on a team", projects.Project{Name: "Ops", Account: &projects.AccountInfo{Customer: "x"}}, ada, projects.ErrInvalid},
		{"an unknown kind", projects.Project{Name: "Ops", Kind: "guild"}, ada, projects.ErrInvalid},
		{"a missing parent", team("Ops", "Nowhere"), ada, projects.ErrInvalid},
		{"a link that is not https", projects.Project{Name: "Acct", Kind: projects.Account, Account: &projects.AccountInfo{URL: "http://x.example"}}, ada, projects.ErrInvalid},
		{"a bad name", team("~root", ""), ada, projects.ErrInvalid},
		{"the root", team("", ""), admin, projects.ErrInvalid},
		{"a title of two lines", projects.Project{Name: "Ops", Title: "a\nb"}, ada, projects.ErrInvalid},
		{"long instructions", projects.Project{Name: "Ops", Instructions: strings.Repeat("x", projects.MaxInstructions+1)}, ada, projects.ErrInvalid},
	} {
		if _, err := s.Create("acme", c.p, c.w, t0); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", c.name, err, c.want)
		}
	}
	// A company project goes under the root or another company project, and
	// a team goes anywhere, under an account too.
	mustCreate(t, s, "acme", projects.Project{Name: "Press", Kind: projects.Company, Parent: "News"}, admin)
	mustCreate(t, s, "acme", team("Globex renewals", "Globex"), ada)
	if p, _ := s.Get("acme", "Field"); p.Kind != projects.Team {
		t.Fatalf("the default kind = %q", p.Kind)
	}
}

func TestAtMostTenCompanyProjects(t *testing.T) {
	s := projects.NewStore()
	for i := range 10 {
		mustCreate(t, s, "acme", projects.Project{Name: fmt.Sprintf("Co %d", i), Kind: projects.Company}, admin)
	}
	if _, err := s.Create("acme", projects.Project{Name: "Co 10", Kind: projects.Company}, admin, t0); !errors.Is(err, projects.ErrLimit) {
		t.Fatalf("the 11th company project = %v, want ErrLimit", err)
	}
}

func TestANameIsItsIdentityIgnoringCase(t *testing.T) {
	s := projects.NewStore()
	mustCreate(t, s, "acme", team("Field", ""), ada)
	if _, err := s.Create("acme", team("field", ""), ada, t0); !errors.Is(err, projects.ErrExists) {
		t.Fatalf("another case = %v, want ErrExists", err)
	}
	// A child names its parent exactly.
	if _, err := s.Create("acme", team("Globex", "FIELD"), ada, t0); !errors.Is(err, projects.ErrInvalid) {
		t.Fatalf("a parent in another case = %v", err)
	}
	// The name cannot change, and the parent changes only by moving.
	for _, edit := range []func(*projects.Project){
		func(p *projects.Project) { p.Name = "Field2" },
		func(p *projects.Project) { p.Parent = "Other" },
	} {
		if _, err := s.Update("acme", "Field", 1, ada, t0, func(p *projects.Project) error { edit(p); return nil }); !errors.Is(err, projects.ErrInvalid) {
			t.Fatalf("an edit of identity = %v, want ErrInvalid", err)
		}
	}
	if p, _ := s.Get("acme", "Field"); p.Version != 1 {
		t.Fatalf("a refused edit wrote version %d", p.Version)
	}
	if found, ok := s.Tree("acme").Find("FIELD"); !ok || found.Name != "Field" {
		t.Fatalf("Find = %+v, %v", found, ok)
	}
}

func TestATombstonedNameNeedsGovern(t *testing.T) {
	s := projects.NewStore()
	mustCreate(t, s, "acme", team("Launch", ""), ada)
	if err := s.Delete("acme", "Launch", ada, t0, nil); err != nil {
		t.Fatal(err)
	}
	if x, ok := s.Tombstone("acme", "LAUNCH"); !ok || x.Name != "Launch" || x.By != "ada" {
		t.Fatalf("tombstone = %+v, %v", x, ok)
	}
	if _, err := s.Create("acme", team("launch", ""), ada, t0); !errors.Is(err, projects.ErrForbidden) {
		t.Fatalf("reusing a deleted name = %v, want ErrForbidden", err)
	}
	got := mustCreate(t, s, "acme", team("Launch", ""), admin)
	if got.Version != 1 {
		t.Fatalf("adopted = %+v", got)
	}
	if _, ok := s.Tombstone("acme", "Launch"); ok {
		t.Fatal("the tombstone outlived the adoption")
	}
	h, _ := s.History("acme", "Launch", 0, 0)
	if len(h) != 3 || h[0].What != projects.Created || h[0].Note != projects.AdoptsNote || h[1].What != projects.Deleted {
		t.Fatalf("history = %+v", h)
	}
	// Another tenant has its own names.
	if _, err := s.Create("globex", team("Launch", ""), ada, t0); err != nil {
		t.Fatalf("another tenant: %v", err)
	}
}

// churn creates and deletes n throwaway names, each a second after the last.
func churn(s *projects.Store, tenant string, n int) error {
	for i := range n {
		name, at := fmt.Sprintf("Junk %04d", i), t0.Add(time.Duration(i+1)*time.Second)
		if _, err := s.Create(tenant, team(name, ""), ada, at); err != nil {
			return err
		}
		if err := s.Delete(tenant, name, ada, at, nil); err != nil {
			return err
		}
	}
	return nil
}

// However many names are deleted after it, a deleted name that still has
// conversations under it keeps its tombstone, so taking it back still needs
// projects:govern. The oldest names with nothing under them make room.
func TestADeletedNameWithConversationsIsNeverForgotten(t *testing.T) {
	filed := func(string) func(string) bool {
		return func(name string) bool { return strings.EqualFold(name, "Target") }
	}
	s := projects.NewStore(projects.WithFiled(filed))
	mustCreate(t, s, "acme", team("Target", ""), ada)
	if err := s.Delete("acme", "Target", ada, t0, nil); err != nil {
		t.Fatal(err)
	}
	later := projects.MaxTombstones + 10
	if err := churn(s, "acme", later); err != nil {
		t.Fatalf("a delete with an empty name to forget = %v", err)
	}
	if _, ok := s.Tombstone("acme", "Target"); !ok {
		t.Fatal("the tombstone of a name with conversations was forgotten")
	}
	if _, err := s.Create("acme", team("target", ""), ada, t0); !errors.Is(err, projects.ErrForbidden) {
		t.Fatalf("reusing it without govern after %d later deletes = %v, want ErrForbidden", later, err)
	}
	if _, ok := s.Tombstone("acme", "Junk 0000"); ok {
		t.Fatal("the oldest empty name is still remembered")
	}
	if _, ok := s.Tombstone("acme", fmt.Sprintf("Junk %04d", later-1)); !ok {
		t.Fatal("the latest delete left no tombstone")
	}
}

// With as many tombstones as a tenant keeps and none it may forget, the next
// delete is refused and changes nothing. A store told nothing about what is
// filed forgets none.
func TestAFullTombstoneTableRefusesTheNextDelete(t *testing.T) {
	everything := projects.WithFiled(func(string) func(string) bool { return func(string) bool { return true } })
	for name, opts := range map[string][]projects.Option{"told nothing": nil, "every name still filed": {everything}} {
		t.Run(name, func(t *testing.T) {
			s := projects.NewStore(opts...)
			if err := churn(s, "acme", projects.MaxTombstones); err != nil {
				t.Fatal(err)
			}
			mustCreate(t, s, "acme", team("Last", ""), ada)
			if err := s.Delete("acme", "Last", ada, t0, nil); !errors.Is(err, projects.ErrLimit) {
				t.Fatalf("a delete past the cap = %v, want ErrLimit", err)
			}
			if !s.Tree("acme").Has("Last") {
				t.Fatal("a refused delete removed the project")
			}
			if _, ok := s.Tombstone("acme", "Junk 0000"); !ok {
				t.Fatal("a refused delete forgot a tombstone")
			}
			if h, _ := s.History("acme", "Last", 0, 0); len(h) != 1 {
				t.Fatalf("a refused delete recorded a change: %+v", h)
			}
			// Taking a deleted name back with govern makes room.
			mustCreate(t, s, "acme", team("Junk 0000", ""), admin)
			if err := s.Delete("acme", "Last", ada, t0, nil); err != nil {
				t.Fatalf("a delete once there is room = %v", err)
			}
		})
	}
}

func TestDeleteIsRefusedWhileAnythingPointsAtTheProject(t *testing.T) {
	s := projects.NewStore()
	mustCreate(t, s, "acme", team("Field", ""), ada)
	mustCreate(t, s, "acme", team("Globex", "Field"), ada)
	err := s.Delete("acme", "Field", ada, t0, nil)
	if !errors.Is(err, projects.ErrInUse) || !strings.Contains(err.Error(), "Globex") {
		t.Fatalf("delete with a child = %v", err)
	}
	inUse := func(p projects.Project) []string {
		if p.Name == "Globex" {
			return []string{"the schedule renewals", "the alert rule fails", "the policy for project Globex"}
		}
		return nil
	}
	err = s.Delete("acme", "Globex", ada, t0, inUse)
	if !errors.Is(err, projects.ErrInUse) || !strings.Contains(err.Error(), "the schedule renewals, the alert rule fails, the policy for project Globex") {
		t.Fatalf("delete in use = %v", err)
	}
	if !s.Tree("acme").Has("Globex") {
		t.Fatal("a refused delete removed the project")
	}
	if err := s.Delete("acme", "", admin, t0, nil); !errors.Is(err, projects.ErrInvalid) {
		t.Fatalf("deleting the root = %v", err)
	}
	if err := s.Delete("acme", "Nowhere", ada, t0, nil); !errors.Is(err, projects.ErrNotFound) {
		t.Fatalf("deleting nothing = %v", err)
	}
	mustCreate(t, s, "acme", projects.Project{Name: "News", Kind: projects.Company}, admin)
	if err := s.Delete("acme", "News", ada, t0, nil); !errors.Is(err, projects.ErrForbidden) {
		t.Fatalf("deleting a company project without govern = %v", err)
	}
	if err := s.Delete("acme", "Globex", ada, t0, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete("acme", "Field", ada, t0, nil); err != nil {
		t.Fatalf("delete once the child is gone: %v", err)
	}
}

func TestHoldKeepsAProjectFromBeingDeleted(t *testing.T) {
	s := projects.NewStore()
	mustCreate(t, s, "acme", team("Launch", ""), ada)
	filed := make(chan struct{})
	release := make(chan struct{})
	held := make(chan error, 1)
	go func() {
		held <- s.Hold("acme", "Launch", func() error {
			close(filed)
			<-release
			return nil
		})
	}()
	<-filed
	deleted := make(chan error, 1)
	go func() {
		deleted <- s.Delete("acme", "Launch", ada, t0, func(projects.Project) []string { return []string{"the schedule filed while held"} })
	}()
	select {
	case err := <-deleted:
		t.Fatalf("delete did not wait for the hold: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	if err := <-held; err != nil {
		t.Fatal(err)
	}
	if err := <-deleted; !errors.Is(err, projects.ErrInUse) {
		t.Fatalf("delete after the hold = %v", err)
	}
	if err := s.Hold("acme", "Nowhere", func() error { return nil }); !errors.Is(err, projects.ErrNotFound) {
		t.Fatalf("holding nothing = %v", err)
	}
}

func TestManagedFields(t *testing.T) {
	s := projects.NewStore()
	// The console made it first; the file takes it over.
	mustCreate(t, s, "acme", projects.Project{Name: "Field", Title: "Field", Description: "console"}, ada)
	got, err := s.Upsert("acme", projects.Project{Name: "Field", Title: "Field engineering", Instructions: "Never promise dates."},
		[]string{projects.FieldTitle, projects.FieldInstructions}, "file", t0)
	if err != nil {
		t.Fatal(err)
	}
	if got.Managed != projects.ManagedFile || got.Version != 2 || got.Title != "Field engineering" || got.Description != "console" {
		t.Fatalf("taken over = %+v", got)
	}
	h, _ := s.History("acme", "Field", 1, 0)
	if h[0].What != projects.Managed {
		t.Fatalf("takeover recorded as %q", h[0].What)
	}
	// An unchanged file makes no version.
	again, err := s.Upsert("acme", projects.Project{Name: "Field", Title: "Field engineering", Instructions: "Never promise dates."},
		[]string{projects.FieldInstructions, projects.FieldTitle}, "file", t0.Add(time.Hour))
	if err != nil || again.Version != 2 {
		t.Fatalf("an unchanged file = %+v, %v", again, err)
	}
	// What the file sets answers ErrManaged; what it leaves stays the API's.
	if _, err := s.Update("acme", "Field", 2, ada, t0, func(p *projects.Project) error { p.Title = "Mine"; return nil }); !errors.Is(err, projects.ErrManaged) {
		t.Fatalf("a field the file sets = %v", err)
	}
	up, err := s.Update("acme", "Field", 2, ada, t0, func(p *projects.Project) error { p.Description = "still mine"; return nil })
	if err != nil || up.Version != 3 || up.Description != "still mine" {
		t.Fatalf("a field the file leaves = %+v, %v", up, err)
	}
	if err := s.Delete("acme", "Field", ada, t0, nil); !errors.Is(err, projects.ErrManaged) {
		t.Fatalf("deleting a managed project = %v", err)
	}
	// A project that left the file stays, unmanaged.
	released, err := s.Release("acme", "Field", "file", t0)
	if err != nil || released.Managed != "" || released.Title != "Field engineering" {
		t.Fatalf("released = %+v, %v", released, err)
	}
	if _, err := s.Update("acme", "Field", released.Version, ada, t0, func(p *projects.Project) error { p.Title = "Mine"; return nil }); err != nil {
		t.Fatalf("a released field: %v", err)
	}
	// A new project from the file, under the root's instructions.
	created, err := s.Upsert("acme", projects.Project{Name: "Globex", Parent: "Field", Kind: projects.Account}, []string{projects.FieldParent, projects.FieldKind}, "file", t0)
	if err != nil || created.Version != 1 || created.Managed != projects.ManagedFile || created.Kind != projects.Account {
		t.Fatalf("created by the file = %+v, %v", created, err)
	}
	// The file names nobody, so only projects:govern owns what it made.
	if want := []projects.Entry{{Who: "*", Role: projects.Viewer}}; !slices.Equal(created.Members, want) {
		t.Fatalf("the file's members = %+v, want %+v", created.Members, want)
	}
	// The file changes a kind under the same rules as the console: Field
	// cannot become an account with an account under it.
	if _, err := s.Upsert("acme", projects.Project{Name: "Field", Kind: projects.Account}, []string{projects.FieldKind}, "file", t0); !errors.Is(err, projects.ErrInvalid) {
		t.Fatalf("the file breaking a kind rule = %v", err)
	}
	root, err := s.Upsert("acme", projects.Project{Instructions: "Answer plainly."}, []string{projects.FieldInstructions}, "file", t0)
	if err != nil || root.Instructions != "Answer plainly." || root.Kind != projects.Company || !root.IsRoot() {
		t.Fatalf("the root from the file = %+v, %v", root, err)
	}
	if _, err := s.Upsert("acme", projects.Project{Name: "globex"}, nil, "file", t0); !errors.Is(err, projects.ErrExists) {
		t.Fatalf("the file naming a project in another case = %v", err)
	}
}

func TestTheRootIsChangedOnlyWithGovern(t *testing.T) {
	s := projects.NewStore()
	root, err := s.Get("acme", "")
	if err != nil || root.Version != 0 || root.Kind != projects.Company {
		t.Fatalf("an untouched root = %+v, %v", root, err)
	}
	set := func(p *projects.Project) error { p.Instructions = "Answer plainly."; return nil }
	if _, err := s.Update("acme", "", 0, ada, t0, set); !errors.Is(err, projects.ErrForbidden) {
		t.Fatalf("the root without govern = %v", err)
	}
	got, err := s.Update("acme", "", 0, admin, t0, set)
	if err != nil || got.Version != 1 || got.Instructions != "Answer plainly." || got.CreatedAt.IsZero() {
		t.Fatalf("the root with govern = %+v, %v", got, err)
	}
	if _, err := s.Update("acme", "", 0, admin, t0, set); !errors.Is(err, projects.ErrVersion) {
		t.Fatalf("a stale version = %v", err)
	}
	if _, err := s.Update("acme", "", 1, admin, t0, func(p *projects.Project) error { p.Title = "Acme"; return nil }); !errors.Is(err, projects.ErrInvalid) {
		t.Fatalf("a title on the root = %v", err)
	}
}

func TestUpdateNamesTheVersionAndAnUnchangedOneWritesNothing(t *testing.T) {
	s := projects.NewStore()
	mustCreate(t, s, "acme", team("Field", ""), ada)
	edit := func(p *projects.Project) error { p.Title = "Field engineering"; return nil }
	got, err := s.Update("acme", "Field", 1, projects.Writer{By: "bo"}, t0.Add(time.Minute), edit)
	if err != nil || got.Version != 2 || got.UpdatedBy != "bo" || got.CreatedBy != "ada" {
		t.Fatalf("update = %+v, %v", got, err)
	}
	if _, err := s.Update("acme", "Field", 1, ada, t0, edit); !errors.Is(err, projects.ErrVersion) {
		t.Fatalf("a stale version = %v", err)
	}
	same, err := s.Update("acme", "Field", 2, ada, t0, edit)
	if err != nil || same.Version != 2 {
		t.Fatalf("an unchanged update = %+v, %v", same, err)
	}
	if _, err := s.Update("acme", "Nowhere", 1, ada, t0, edit); !errors.Is(err, projects.ErrNotFound) {
		t.Fatalf("updating nothing = %v", err)
	}
	if _, err := s.Update("globex", "Field", 2, ada, t0, edit); !errors.Is(err, projects.ErrNotFound) {
		t.Fatalf("another tenant's project = %v", err)
	}
}

func TestTenantsAreApart(t *testing.T) {
	s := projects.NewStore()
	mustCreate(t, s, "acme", team("Field", ""), ada)
	if s.Tree("globex").Has("Field") || len(s.List("globex")) != 0 {
		t.Fatal("another tenant sees acme's project")
	}
	if _, err := s.Get("globex", "Field"); !errors.Is(err, projects.ErrNotFound) {
		t.Fatalf("Get = %v", err)
	}
	if _, err := s.Create("globex", team("Globex", "Field"), ada, t0); !errors.Is(err, projects.ErrInvalid) {
		t.Fatalf("a parent in another tenant = %v", err)
	}
	if err := s.Delete("globex", "Field", ada, t0, nil); !errors.Is(err, projects.ErrNotFound) {
		t.Fatalf("Delete = %v", err)
	}
	if h, _ := s.History("globex", "Field", 0, 0); len(h) != 0 {
		t.Fatalf("History = %+v", h)
	}
	if s.Stored("globex") || !s.Stored("acme") {
		t.Fatal("Stored is not per tenant")
	}
}

func TestTheTreeAnswersChainsAndSubtrees(t *testing.T) {
	s := projects.NewStore()
	mustCreate(t, s, "acme", team("Field", ""), ada)
	mustCreate(t, s, "acme", projects.Project{Name: "Globex", Parent: "Field", Kind: projects.Account}, ada)
	mustCreate(t, s, "acme", team("Initech", "Field"), ada)
	mustCreate(t, s, "acme", team("Renewals", "Globex"), ada)
	mustCreate(t, s, "acme", team("Platform", ""), ada)
	tree := s.Tree("acme")
	names := func(ps []projects.Project) []string {
		var out []string
		for _, p := range ps {
			out = append(out, p.Name)
		}
		return out
	}
	if got := names(tree.Chain("Renewals")); !slices.Equal(got, []string{"Renewals", "Globex", "Field", ""}) {
		t.Fatalf("Chain = %q", got)
	}
	if got := tree.Ancestors("Renewals"); !slices.Equal(got, []string{"Globex", "Field"}) {
		t.Fatalf("Ancestors = %q", got)
	}
	if tree.Ancestors("Field") != nil || tree.Ancestors("") != nil || tree.Chain("Nowhere") != nil {
		t.Fatal("a top-level project, the root or a missing one has ancestors")
	}
	if got := tree.Subtree("Field"); !slices.Equal(got, []string{"Field", "Globex", "Renewals", "Initech"}) {
		t.Fatalf("Subtree = %q", got)
	}
	if got := names(tree.Projects()); !slices.Equal(got, []string{"Field", "Globex", "Renewals", "Initech", "Platform"}) {
		t.Fatalf("tree order = %q", got)
	}
	if got := names(tree.Children("Field")); !slices.Equal(got, []string{"Globex", "Initech"}) {
		t.Fatalf("Children = %q", got)
	}
	if tree.Depth("Renewals") != 3 || tree.Depth("") != 0 || tree.Depth("Nowhere") != -1 {
		t.Fatal("Depth")
	}
	// A tree once read does not change under its reader.
	mustCreate(t, s, "acme", team("Late", "Field"), ada)
	if tree.Has("Late") || !s.Tree("acme").Has("Late") {
		t.Fatal("a snapshot changed, or the store did not replace it")
	}
	// What a reader is given is a copy.
	g, _ := tree.Get("Globex")
	g.Title = "changed"
	if again, _ := tree.Get("Globex"); again.Title != "" {
		t.Fatal("a reader changed the snapshot")
	}
}

func TestHistoryPagesNewestFirst(t *testing.T) {
	s := projects.NewStore()
	mustCreate(t, s, "acme", team("Field", ""), ada)
	for i := range 5 {
		_, err := s.Update("acme", "Field", i+1, ada, t0, func(p *projects.Project) error { p.Description = fmt.Sprint(i); return nil })
		if err != nil {
			t.Fatal(err)
		}
	}
	page, next := s.History("acme", "Field", 4, 0)
	if len(page) != 4 || page[0].After.Version != 6 || next == 0 {
		t.Fatalf("first page = %d entries, next %d", len(page), next)
	}
	rest, next := s.History("acme", "Field", 4, next)
	if len(rest) != 2 || rest[1].What != projects.Created || next != 0 {
		t.Fatalf("second page = %+v, next %d", rest, next)
	}
	if root, _ := s.History("acme", "", 0, 0); len(root) != 1 || root[0].What != projects.Created {
		t.Fatalf("the root's history = %+v", root)
	}
}

func TestImportWritesOnceAndAsTeams(t *testing.T) {
	s := projects.NewStore()
	legacy := []projects.Project{{Name: "Launch", Description: "Everything for the Q4 launch"}, {Name: "Ops", Kind: projects.Company, Parent: "x"}}
	if err := s.Import("acme", legacy, nil, "migration", t0); err != nil {
		t.Fatal(err)
	}
	ops, _ := s.Get("acme", "Ops")
	if ops.Kind != projects.Team || ops.Parent != "" || ops.CreatedBy != "migration" || !ops.CreatedAt.Equal(t0) {
		t.Fatalf("imported = %+v", ops)
	}
	if root, _ := s.Get("acme", ""); root.Version != 1 {
		t.Fatalf("the root was not created: %+v", root)
	}
	if err := s.Import("acme", legacy, nil, "migration", t0); !errors.Is(err, projects.ErrExists) {
		t.Fatalf("a second import = %v", err)
	}
	// What something outside the tree still names is kept, and says so.
	if err := s.Import("initech", []projects.Project{{Name: "Launch"}}, []projects.Project{{Name: "Dropped"}}, "migration", t0); err != nil {
		t.Fatal(err)
	}
	if h, _ := s.History("initech", "Dropped", 0, 0); len(h) != 1 || h[0].Note != projects.KeptNote || h[0].After.Kind != projects.Team {
		t.Fatalf("a kept name's history = %+v", h)
	}
	if h, _ := s.History("initech", "Launch", 0, 0); len(h) != 1 || h[0].Note != "" {
		t.Fatalf("an imported project's history = %+v", h)
	}
	// Past the cap together, nothing is written.
	capped := projects.NewStore(projects.WithCaps(projects.Caps{Projects: 2}))
	if err := capped.Import("acme", []projects.Project{{Name: "A"}, {Name: "B"}}, []projects.Project{{Name: "C"}}, "migration", t0); !errors.Is(err, projects.ErrLimit) {
		t.Fatalf("an import past the cap = %v, want ErrLimit", err)
	}
	if capped.Stored("acme") {
		t.Fatal("an import past the cap left records")
	}
	// An import that cannot be written whole writes nothing.
	if err := s.Import("globex", []projects.Project{{Name: "A"}, {Name: "a"}}, nil, "migration", t0); !errors.Is(err, projects.ErrExists) {
		t.Fatalf("an import with one name twice = %v", err)
	}
	if s.Stored("globex") {
		t.Fatal("a refused import left records")
	}
}
