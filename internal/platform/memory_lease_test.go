package platform_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/fiber/pkg/isolation"
	"github.com/urmzd/humagen/fiber/pkg/isolation/isolationtest"
	"github.com/urmzd/humagen/fiber/pkg/runtime"
	"github.com/urmzd/humagen/fiber/pkg/turn/native"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/drive"
	"github.com/urmzd/humagen/pkg/memory"
	"github.com/urmzd/humagen/pkg/runs"
)

// An isolated run's memory is in its instance from the lease: the person's
// own at /me, the agent's and the project's at /project. What the run
// changes in /me is committed when the instance is released, on the version
// it was given, so a change made meanwhile to another file is kept, and a
// change made meanwhile to the same file is never overwritten.
func TestAnIsolatedRunsMemoryIsInstalledOnLeaseAndKeptOnRelease(t *testing.T) {
	leaser := &isolationtest.Leaser{Dir: t.TempDir()}
	p := startWith(t, platform.Config{Sandbox: isolation.Sandbox{Leaser: leaser, Name: "test"}})
	ctx := t.Context()
	ada := auth.Principal{Kind: auth.User, Tenant: tenant, Subject: "ada", Roles: []auth.Role{auth.Admin}}
	personal, err := p.PersonalProject(ada)
	if err != nil {
		t.Fatal(err)
	}
	mine, err := p.Memory.Add(tenant, memory.PersonalOwner("ada"), "Ada prefers tables.", memory.Person, memory.Approved, "ada", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	theirs, _ := p.Memory.Add(tenant, memory.PersonalOwner("bob"), "Bob prefers prose.", memory.Person, memory.Approved, "bob", time.Now())
	agent, _ := p.Memory.Add(tenant, "lead", "Releases go out on Tuesdays.", memory.Person, memory.Approved, "ada", time.Now())

	claimFor := func(id string) (runtime.Request, *isolation.Run, func(string) string) {
		t.Helper()
		r, err := p.Converse(ctx, tenant, platform.Turn{ID: id, Agent: "lead", Text: "Hello", Project: personal.Name, Actor: platform.ActorOf(ada)})
		if err != nil {
			t.Fatal(err)
		}
		run := await(t, p, r.ID, runs.Completed)
		req := runtime.Request{RunID: run.ID, Tenant: tenant, Definition: native.Revision{Spec: native.Spec{Name: "lead"}, Version: wire.VersionOf(1)}}
		cctx, claim := p.Claim(ctx, req)
		shell := func(command string) string {
			t.Helper()
			out, err := useIn(t, cctx, p, req, platform.ToolShell, map[string]any{"command": command})
			if err != nil || !strings.HasPrefix(out, "exit 0\n") {
				t.Fatalf("shell %q = %q, %v", command, out, err)
			}
			return out
		}
		return req, claim, shell
	}
	head := func() drive.Version {
		t.Helper()
		v, err := p.Drive.Head(ctx, tenant, p.MemoryActor(ada), drive.Person("ada"))
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	read := func(path string) string {
		t.Helper()
		b, _, err := p.Drive.ReadFile(ctx, tenant, p.MemoryActor(ada), drive.Person("ada"), 0, path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		return string(b)
	}
	commit := func(path, content string) {
		t.Helper()
		_, err := p.Drive.Commit(ctx, tenant, p.MemoryActor(ada), drive.Person("ada"), drive.CommitInput{
			Base: drive.Head, Changes: []drive.Change{{Path: path, Data: []byte(content)}}, By: "ada", Origin: "api",
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	// Installed at the lease, nobody else's among it.
	_, claim, shell := claimFor("lease-1")
	out := shell("cat ../me/memory/" + memory.NotePath(mine.ID) + " ../project/memory/lead/" + memory.NotePath(agent.ID) + "; ls -R ../me ../project")
	if !strings.Contains(out, "Ada prefers tables.") || !strings.Contains(out, "Releases go out on Tuesdays.") {
		t.Fatalf("the run's memory is not in its instance: %q", out)
	}
	if strings.Contains(out, theirs.ID) {
		t.Fatalf("another person's memory is in the instance: %q", out)
	}
	inst := leaser.Instances()[0]
	if _, err := os.Stat(filepath.Join(inst.HostDir(), "work", "me")); err == nil {
		t.Fatal("memory reached the work directory, which goes back to the conversation")
	}

	// Kept at the release, beside a change made meanwhile to another file.
	shell("echo 'Ada works from Toronto.' > ../me/memory/notes/home.md")
	commit("notes/elsewhere.md", "Written while the run ran.")
	if err := claim.Release(ctx); err != nil {
		t.Fatal(err)
	}
	if !inst.Closed() {
		t.Fatal("the instance outlived the run")
	}
	v := head()
	for _, path := range []string{memory.NotePath(mine.ID), "notes/home.md", "notes/elsewhere.md"} {
		if _, ok := v.Lookup(path); !ok {
			t.Errorf("%s is not in the person's memory after the run: %+v", path, v.Entries)
		}
	}
	if got := read("notes/home.md"); got != "Ada works from Toronto.\n" {
		t.Fatalf("notes/home.md = %q", got)
	}

	// A file changed meanwhile is not overwritten by the run.
	_, claim, shell = claimFor("lease-2")
	shell("echo 'From the run.' > ../me/memory/notes/home.md")
	commit("notes/home.md", "From the person.")
	if err := claim.Release(ctx); !errors.Is(err, drive.ErrConflict) {
		t.Fatalf("release over a file changed meanwhile: err = %v, want a conflict", err)
	}
	if got := read("notes/home.md"); got != "From the person." {
		t.Fatalf("notes/home.md = %q; the run overwrote the person's change", got)
	}
	if len(leaser.Open()) != 0 {
		t.Fatal("an instance outlived its run")
	}
}
