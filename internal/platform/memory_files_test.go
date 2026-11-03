package platform_test

import (
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/orchagent/flow/isolation"
	"github.com/urmzd/orchagent/flow/isolation/isolationtest"
	"github.com/urmzd/orchagent/flow/turn/native"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/drive"
	"github.com/urmzd/humagen/pkg/memory"
	"github.com/urmzd/humagen/pkg/runs"

	"github.com/urmzd/orchagent/flow/runtime"
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

// The chain is the run's project first, then ancestors nearest first.
func TestMemoryMountsKeepOwnProjectWritableAndAncestorsReadOnly(t *testing.T) {
	p := startWith(t, platform.Config{Sandbox: isolation.Sandbox{Leaser: &isolationtest.Leaser{Dir: t.TempDir()}, Name: "test"}})
	ctx := t.Context()
	who := auth.Principal{Kind: auth.User, Tenant: tenant, Subject: "ada", Roles: []auth.Role{auth.Admin}}
	for _, in := range []platform.ProjectInput{{Name: "Parent"}, {Name: "Child", Parent: "Parent"}} {
		if _, err := p.CreateProject(ctx, tenant, ada, in); err != nil {
			t.Fatal(err)
		}
	}
	for _, project := range []string{"Child", "Parent", ""} {
		if _, err := p.Memory.Add(tenant, memory.ProjectOwner(project), "Memory for "+project, memory.Person, memory.Approved, "ada", time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	r, err := p.Converse(ctx, tenant, platform.Turn{ID: "memory-chain", Agent: "lead", Text: "Hello", Project: "Child", Actor: platform.ActorOf(who)})
	if err != nil {
		t.Fatal(err)
	}
	run := await(t, p, r.ID, runs.Completed)
	want := map[string]struct {
		dir      string
		readOnly bool
	}{
		"Child":  {"project/memory/@project", false},
		"Parent": {"project/memory/@parents/Parent", true},
		"":       {"project/memory/@parents/@workspace", true},
	}
	count := 0
	for _, m := range p.MemoryMounts(run) {
		if m.Tree.Kind != drive.ProjectKind {
			continue
		}
		w, ok := want[m.Tree.Project]
		if !ok || m.Dir != w.dir || m.ReadOnly != w.readOnly {
			t.Fatalf("mount = %+v, want %+v", m, w)
		}
		count++
	}
	if count != 3 {
		t.Fatalf("project mounts = %d", count)
	}
	req := runtime.Request{RunID: run.ID, Tenant: tenant, Definition: native.Revision{Spec: native.Spec{Name: "lead"}, Version: wire.VersionOf(1)}}
	cctx, claim := p.Claim(ctx, req)
	if out, err := useIn(t, cctx, p, req, platform.ToolShell, map[string]any{"command": "pwd"}); err != nil || !strings.HasPrefix(out, "exit 0") {
		t.Fatalf("shell = %q %v", out, err)
	}
	if err := claim.Release(ctx); err != nil {
		t.Fatal(err)
	}
	recorded, err := p.Runs.Get(tenant, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	count = 0
	for _, u := range recorded.Memory {
		if u.Kind != string(drive.ProjectKind) {
			continue
		}
		w, ok := want[u.Project]
		if !ok || u.Dir != w.dir || u.ReadOnly != w.readOnly || u.Files == 0 {
			t.Fatalf("Run.memory = %+v, want %+v", u, w)
		}
		count++
	}
	if count != 3 {
		t.Fatalf("recorded project trees = %d", count)
	}
	view, err := p.ConversationMemories(ctx, who, run.Thread)
	if err != nil {
		t.Fatal(err)
	}
	count = 0
	for _, rm := range view.Runs {
		if rm.Run != run.ID {
			continue
		}
		for _, tr := range rm.Trees {
			if tr.Kind != string(drive.ProjectKind) {
				continue
			}
			w, ok := want[tr.Project]
			if !ok || tr.Dir != "/"+w.dir || tr.ReadOnly != w.readOnly || !tr.Installed {
				t.Fatalf("memory scope = %+v, want %+v", tr, w)
			}
			count++
		}
	}
	if count != 3 {
		t.Fatalf("view project trees = %d", count)
	}
}
