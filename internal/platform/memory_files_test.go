package platform_test

import (
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/fiber/pkg/turn/native"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/drive"
	"github.com/urmzd/humagen/pkg/memory"
	"github.com/urmzd/humagen/pkg/runs"

	"github.com/urmzd/humagen/fiber/pkg/runtime"
)

// A run's memory is installed into its files and what it changed comes back:
// the requester's own memory committed, the agent's proposed for a person to
// decide. Nobody else's memory is installed or found.
func TestARunsMemoryIsInstalledAndItsChangesKept(t *testing.T) {
	p := start(t, call.Limits{})
	ctx := t.Context()
	who := auth.Principal{Kind: auth.User, Tenant: tenant, Subject: "ada", Roles: []auth.Role{auth.Admin}}
	personal, err := p.PersonalProject(who)
	if err != nil {
		t.Fatal(err)
	}
	mine, err := p.Memory.Add(tenant, memory.PersonalOwner("ada"), "Ada prefers tables.", memory.Person, memory.Approved, "ada", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	theirs, _ := p.Memory.Add(tenant, memory.PersonalOwner("bob"), "Bob prefers prose.", memory.Person, memory.Approved, "bob", time.Now())
	agent, _ := p.Memory.Add(tenant, "lead", "Releases go out on Tuesdays.", memory.Person, memory.Approved, "ada", time.Now())
	r, err := p.Converse(ctx, tenant, platform.Turn{ID: "files-1", Agent: "lead", Text: "Hello", Project: personal.Name, Actor: platform.ActorOf(who)})
	if err != nil {
		t.Fatal(err)
	}
	run := await(t, p, r.ID, runs.Completed)

	mounts := p.MemoryMounts(run)
	var dirs []string
	for _, m := range mounts {
		dirs = append(dirs, m.Dir)
	}
	if !slices.Contains(dirs, "me/memory") || !slices.Contains(dirs, "project/memory/lead") || !slices.Contains(dirs, "project/memory/@project") {
		t.Fatalf("mounts = %v", dirs)
	}
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	target := drive.RootTarget(root)
	installed, err := p.InstallMemory(ctx, run, target, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"me/memory/" + memory.NotePath(mine.ID), "project/memory/lead/" + memory.NotePath(agent.ID)} {
		if _, err := root.Stat(name); err != nil {
			t.Fatalf("%s was not installed: %v", name, err)
		}
	}
	if b, _ := root.ReadFile("me/memory/" + memory.NotePath(theirs.ID)); len(b) > 0 {
		t.Fatal("another person's memory was installed")
	}

	_ = target.WriteFile("me/memory/notes/home.md", []byte("Ada works from Toronto."))
	_ = target.WriteFile("project/memory/lead/notes/learned.md", []byte("Deploys happen at noon."))
	done, err := p.FinishMemory(ctx, run, installed, target)
	if err != nil {
		t.Fatal(err)
	}
	committed, proposed := 0, 0
	for _, f := range done {
		if f.Version > 0 {
			committed++
		}
		proposed += len(f.Proposals)
	}
	if committed != 1 || proposed != 1 {
		t.Fatalf("finished = %+v", done)
	}
	if got := promptAtOf(t, p.Memory, tenant, memory.PersonalOwner("ada"), "Notes", time.Now()); !strings.Contains(got, "Ada works from Toronto.") {
		t.Fatalf("the person's own memory did not keep the run's note: %q", got)
	}
	if got := promptOf(t, p.Memory, tenant, "lead"); strings.Contains(got, "noon") {
		t.Fatal("a run's write to the agent's memory skipped approval")
	}
	pending := pendingOf(t, p.Memory, tenant)
	if len(pending) != 1 || pending[0].Agent != "lead" || pending[0].Note.Text != "Deploys happen at noon." || pending[0].Note.By != run.ID {
		t.Fatalf("pending = %+v", pending)
	}
	if n, err := p.ApproveMemory(tenant, "lead", []string{pending[0].Note.ID}); n != 1 || err != nil {
		t.Fatalf("approve = %d, %v", n, err)
	}
	if got := promptOf(t, p.Memory, tenant, "lead"); !strings.Contains(got, "noon") {
		t.Fatalf("an approved proposal is not sent: %q", got)
	}

	// memory_search reads what the run may read, and nothing else.
	req := runtime.Request{RunID: run.ID, Tenant: tenant, Definition: native.Revision{Spec: native.Spec{Name: "lead"}, Version: wire.VersionOf(1)}}
	out, err := use(t, p, req, platform.ToolMemorySearch, map[string]any{"query": "prefers tables prose"})
	if err != nil || !strings.Contains(out, "/me/memory/"+memory.NotePath(mine.ID)) || strings.Contains(out, theirs.ID) {
		t.Fatalf("memory_search = %s, %v", out, err)
	}
	bob := auth.Principal{Kind: auth.User, Tenant: tenant, Subject: "bob", Roles: []auth.Role{auth.Admin}}
	hits, err := p.SearchMemory(ctx, bob, "tables", 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range hits {
		if h.Tree.Kind == drive.PersonKind && h.Tree.Person != "bob" {
			t.Fatalf("bob found %+v", h)
		}
	}
	// Another tenant holds none of it.
	other := auth.Principal{Kind: auth.User, Tenant: "globex", Subject: "ada", Roles: []auth.Role{auth.Admin}}
	if hits, _ := p.SearchMemory(ctx, other, "tables Tuesdays noon", 10); len(hits) != 0 {
		t.Fatalf("another tenant found %+v", hits)
	}
}
