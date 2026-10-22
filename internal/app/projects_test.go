package app

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urmzd/warp/internal/platform"
	"github.com/urmzd/warp/pkg/projects"
	"github.com/urmzd/warp/pkg/spec"
)

func newProjectsPlatform(t *testing.T) *platform.Platform {
	t.Helper()
	p, err := platform.New(platform.Config{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	return p
}

func projectsOf(ps ...spec.Project) spec.Workspace { return spec.Workspace{Projects: ps} }

func mustGet(t *testing.T, p *platform.Platform, tenant, name string) projects.Project {
	t.Helper()
	pr, err := p.GetProject(tenant, name)
	if err != nil {
		t.Fatalf("%q: %v", name, err)
	}
	return pr
}

// The file's projects merge into the tree: a console project survives a
// start, an unchanged file makes no version, and a project that left the file
// stays, unmanaged.
func TestTheFilesProjectsMergeIntoTheTree(t *testing.T) {
	p := newProjectsPlatform(t)
	const tenant = "acme"
	if _, err := p.CreateProject(t.Context(), tenant, projects.Writer{By: "ada"}, platform.ProjectInput{Name: "Console"}); err != nil {
		t.Fatal(err)
	}
	file := projectsOf(spec.Project{Name: "Launch", Description: "Everything for the Q4 launch"}, spec.Project{Name: "Ops"})
	for range 2 {
		if err := applyProjects(p, tenant, file); err != nil {
			t.Fatal(err)
		}
	}
	launch := mustGet(t, p, tenant, "Launch")
	if launch.Managed != projects.ManagedFile || launch.Version != 1 || launch.Description != "Everything for the Q4 launch" {
		t.Fatalf("the file's project = %+v", launch)
	}
	if _, err := p.UpdateProject(t.Context(), tenant, "Launch", projects.Writer{By: "ada"}, platform.ProjectUpdate{Version: 1, Description: "mine"}); !errors.Is(err, projects.ErrManaged) {
		t.Fatalf("changing what the file sets = %v", err)
	}
	if !p.HasProject(tenant, "Console") {
		t.Fatal("a console project did not survive the file")
	}
	// Ops leaves the file.
	if err := applyProjects(p, tenant, projectsOf(file.Projects[0])); err != nil {
		t.Fatal(err)
	}
	if ops := mustGet(t, p, tenant, "Ops"); ops.Managed != "" {
		t.Fatalf("a project that left the file = %+v", ops)
	}
	if err := p.DeleteProject(t.Context(), tenant, "Ops", projects.Writer{By: "ada"}); err != nil {
		t.Fatalf("deleting a project the file let go of: %v", err)
	}
	if !p.HasProject(tenant, "Console") || mustGet(t, p, tenant, "Launch").Version != 1 {
		t.Fatal("letting one project go changed another")
	}
}

// Every field the file sets lands, parents are made before their children
// whatever order the file lists them in, and a field the file sets is the
// file's while one it leaves out stays the console's, across starts.
func TestTheFileDeclaresTheTreeParentsFirst(t *testing.T) {
	p := newProjectsPlatform(t)
	const tenant = "acme"
	file := projectsOf(
		spec.Project{Name: "Q4", Parent: "Globex"},
		spec.Project{Name: "Globex", Parent: "Field", Kind: spec.ProjectAccount, Instructions: "Quote the contract.",
			Account: &spec.Account{Customer: "Globex Inc", ExternalID: "sf:0015g00000ABC", URL: "https://crm.example.com/globex"}},
		spec.Project{Name: "Field", Title: "Field engineering", Kind: spec.ProjectTeam, Instructions: "Never promise dates."},
		spec.Project{Name: "News", Kind: spec.ProjectCompany, Parent: spec.RootParent},
	)
	if err := applyProjects(p, tenant, file); err != nil {
		t.Fatal(err)
	}
	tree := p.ProjectTree(tenant)
	if got := tree.Depth("Q4"); got != 3 {
		t.Fatalf("Q4 sits %d levels down, want 3", got)
	}
	g := mustGet(t, p, tenant, "Globex")
	if g.Kind != projects.Account || g.Parent != "Field" || g.Instructions != "Quote the contract." || g.Account == nil ||
		*g.Account != (projects.AccountInfo{Customer: "Globex Inc", ExternalID: "sf:0015g00000ABC", URL: "https://crm.example.com/globex"}) {
		t.Fatalf("Globex = %+v", g)
	}
	f := mustGet(t, p, tenant, "Field")
	if f.Title != "Field engineering" || f.Kind != projects.Team || f.Parent != "" || f.Managed != projects.ManagedFile {
		t.Fatalf("Field = %+v", f)
	}
	if n := mustGet(t, p, tenant, "News"); n.Kind != projects.Company || n.Parent != "" {
		t.Fatalf(`"~" is the workspace: News = %+v`, n)
	}
	// The file sets Field's title and instructions; its description is the
	// console's.
	ada := projects.Writer{By: "ada"}
	for name, change := range map[string]platform.ProjectUpdate{
		"title":        {Version: f.Version, Title: "Field", Instructions: f.Instructions},
		"instructions": {Version: f.Version, Title: f.Title, Instructions: "Promise anything."},
	} {
		if _, err := p.UpdateProject(t.Context(), tenant, "Field", ada, change); !errors.Is(err, projects.ErrManaged) {
			t.Errorf("changing the file's %s = %v", name, err)
		}
	}
	f, err := p.UpdateProject(t.Context(), tenant, "Field", ada, platform.ProjectUpdate{Version: f.Version, Title: f.Title, Instructions: f.Instructions, Description: "Kept by people."})
	if err != nil {
		t.Fatalf("changing what the file leaves out: %v", err)
	}
	if err := applyProjects(p, tenant, file); err != nil {
		t.Fatal(err)
	}
	again := mustGet(t, p, tenant, "Field")
	if again.Description != "Kept by people." || again.Version != f.Version {
		t.Fatalf("the next start overwrote or rewrote what the console set: %+v, was %+v", again, f)
	}
	for _, name := range []string{"Q4", "Globex", "News"} {
		if v := mustGet(t, p, tenant, name).Version; v != 1 {
			t.Errorf("an unchanged entry made a version: %s is at %d", name, v)
		}
	}
}

// workspace.instructions are the root's: set, they are the file's; removed,
// they stay and are the console's again.
func TestTheWorkspaceInstructionsAreTheRoots(t *testing.T) {
	p := newProjectsPlatform(t)
	const tenant = "acme"
	text := "Acme sells warehouse robots. Answer plainly."
	file := spec.Workspace{Instructions: &text}
	for range 2 {
		if err := applyProjects(p, tenant, file); err != nil {
			t.Fatal(err)
		}
	}
	root := mustGet(t, p, tenant, projects.RootAddress)
	if root.Instructions != text || root.Managed != projects.ManagedFile || root.Version != 1 || root.Kind != projects.Company {
		t.Fatalf("the workspace = %+v", root)
	}
	admin := projects.Writer{By: "root", Govern: true}
	if _, err := p.UpdateProject(t.Context(), tenant, projects.RootAddress, admin, platform.ProjectUpdate{Version: 1, Instructions: "Other."}); !errors.Is(err, projects.ErrManaged) {
		t.Fatalf("changing the file's workspace instructions = %v", err)
	}
	if err := applyProjects(p, tenant, spec.Workspace{}); err != nil {
		t.Fatal(err)
	}
	root = mustGet(t, p, tenant, projects.RootAddress)
	if root.Instructions != text || root.Managed != "" {
		t.Fatalf("instructions that left the file = %+v", root)
	}
	if _, err := p.UpdateProject(t.Context(), tenant, projects.RootAddress, admin, platform.ProjectUpdate{Version: root.Version, Instructions: "Other."}); err != nil {
		t.Fatalf("the console's again: %v", err)
	}
	// A file that never set them leaves the workspace alone.
	before := mustGet(t, p, tenant, projects.RootAddress)
	if err := applyProjects(p, tenant, spec.Workspace{}); err != nil {
		t.Fatal(err)
	}
	if after := mustGet(t, p, tenant, projects.RootAddress); after.Version != before.Version || after.Instructions != "Other." {
		t.Fatalf("a file without instructions changed the workspace: %+v", after)
	}
}

// A console project the file names is taken over, keeping what the file
// leaves out, and the history says so.
func TestAConsoleProjectTheFileNamesIsTakenOver(t *testing.T) {
	p := newProjectsPlatform(t)
	const tenant = "acme"
	ada := projects.Writer{By: "ada"}
	if _, err := p.CreateProject(t.Context(), tenant, ada, platform.ProjectInput{Name: "Ops", Title: "Operations", Description: "Ada's"}); err != nil {
		t.Fatal(err)
	}
	if err := applyProjects(p, tenant, projectsOf(spec.Project{Name: "Ops", Instructions: "Page the on-call first."})); err != nil {
		t.Fatal(err)
	}
	ops := mustGet(t, p, tenant, "Ops")
	if ops.Managed != projects.ManagedFile || ops.Title != "Operations" || ops.Description != "Ada's" || ops.Instructions != "Page the on-call first." || ops.CreatedBy != "ada" {
		t.Fatalf("taken over = %+v", ops)
	}
	changes, _, err := p.ProjectHistory(tenant, "Ops", 0, 0)
	if err != nil || len(changes) == 0 || changes[0].What != projects.Managed || changes[0].By != fileWriter {
		t.Fatalf("history = %+v", changes)
	}
}

// Kind and parent are fixed when a project is made, so a file that names
// another for an existing project fails the start, with the entry's path, and
// changes nothing.
func TestAKindOrParentTheFileChangesFailsTheStart(t *testing.T) {
	p := newProjectsPlatform(t)
	const tenant = "acme"
	ada := projects.Writer{By: "ada"}
	for _, in := range []platform.ProjectInput{{Name: "Field"}, {Name: "Ops"}} {
		if _, err := p.CreateProject(t.Context(), tenant, ada, in); err != nil {
			t.Fatal(err)
		}
	}
	for name, file := range map[string]spec.Workspace{
		"kind":   projectsOf(spec.Project{Name: "Ops", Kind: spec.ProjectAccount}),
		"parent": projectsOf(spec.Project{Name: "Field"}, spec.Project{Name: "Ops", Parent: "Field"}),
	} {
		err := applyProjects(p, tenant, file)
		if !errors.Is(err, projects.ErrInvalid) || !strings.Contains(err.Error(), `platform "acme": workspace.projects[`) || !strings.Contains(err.Error(), "fixed when it is created") {
			t.Errorf("a changed %s = %v", name, err)
		}
		if ops := mustGet(t, p, tenant, "Ops"); ops.Version != 1 || ops.Managed != "" || ops.Kind != projects.Team || ops.Parent != "" {
			t.Errorf("a refused %s changed Ops: %+v", name, ops)
		}
	}
}

// A real start on Postgres: a project made in the console survives a restart,
// an unchanged entry makes no version, one that left the file stays,
// unmanaged, and the workspace's instructions are the file's.
func TestAConsoleProjectSurvivesARestartOnPostgres(t *testing.T) {
	url := os.Getenv("WARP_POSTGRES_TEST_URL")
	if url == "" {
		t.Skip("WARP_POSTGRES_TEST_URL is not set")
	}
	t.Setenv("SPEC_TEST_PG", url)
	dir := t.TempDir()
	write := func(projectsYAML string) string {
		path := filepath.Join(dir, "warp.yaml")
		body := "api_version: warp/v1\nkind: System\nspec:\n  stores: {kind: postgres, postgres: {url: {ref: \"env:SPEC_TEST_PG\"}}}\n---\n" +
			"api_version: warp/v1\nkind: Platform\nmetadata: {name: default}\nspec:\n  workspace:\n    instructions: Answer plainly.\n    projects:\n" + projectsYAML
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	type view struct {
		Project struct {
			projects.Project
		} `json:"project"`
	}
	read := func(addr, name string) projects.Project {
		var v view
		if err := json.Unmarshal([]byte(get(t, "http://"+addr+"/api/projects/"+name)), &v); err != nil {
			t.Fatal(err)
		}
		return v.Project.Project
	}
	both := write("      - {name: Field, title: Field engineering}\n      - {name: Globex, parent: Field, kind: account}\n")
	var field, globex projects.Project
	func() {
		addr, stop := serveUntil(t, Options{}, "-f", both, "-addr", "127.0.0.1:0")
		defer stop()
		resp, err := http.Post("http://"+addr+"/api/projects", "application/json", strings.NewReader(`{"name":"Console","description":"made by hand"}`))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusConflict {
			t.Fatalf("create: %d", resp.StatusCode)
		}
		field, globex = read(addr, "Field"), read(addr, "Globex")
	}()
	if field.Managed != projects.ManagedFile || globex.Parent != "Field" || globex.Kind != projects.Account {
		t.Fatalf("the file's tree = %+v %+v", field, globex)
	}
	// Globex leaves the file; Field is unchanged.
	addr, stop := serveUntil(t, Options{}, "-f", write("      - {name: Field, title: Field engineering}\n"), "-addr", "127.0.0.1:0")
	defer stop()
	if c := read(addr, "Console"); c.Description != "made by hand" {
		t.Fatalf("the console's project after a restart = %+v", c)
	}
	if f := read(addr, "Field"); f.Version != field.Version || f.Managed != projects.ManagedFile {
		t.Fatalf("an unchanged entry across a restart = %+v, was %+v", f, field)
	}
	if g := read(addr, "Globex"); g.Managed != "" || g.Parent != "Field" {
		t.Fatalf("a project that left the file = %+v", g)
	}
	if r := read(addr, "~"); r.Instructions != "Answer plainly." || r.Managed != projects.ManagedFile {
		t.Fatalf("the workspace = %+v", r)
	}
}
