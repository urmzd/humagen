package platform_test

import (
	"strings"
	"testing"
	"time"

	"github.com/urmzd/orchagent/flow/isolation"
	"github.com/urmzd/orchagent/flow/isolation/isolationtest"
	"github.com/urmzd/orchagent/flow/runtime"
	"github.com/urmzd/orchagent/flow/turn/native"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/memory"
	"github.com/urmzd/humagen/pkg/runs"
)

// A run's memory record says which trees it was given at which version and
// what it kept, and the Workspace's Memories shows it by scope, naming only
// the trees the reader may read.
func TestMemoriesShowWhatARunUsedAndWroteByScope(t *testing.T) {
	leaser := &isolationtest.Leaser{Dir: t.TempDir()}
	p := startWith(t, platform.Config{Sandbox: isolation.Sandbox{Leaser: leaser, Name: "test"}})
	ctx := t.Context()
	ada := auth.Principal{Kind: auth.User, Tenant: tenant, Subject: "ada", Roles: []auth.Role{auth.Admin}}
	personal, err := p.PersonalProject(ada)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Memory.Add(tenant, memory.PersonalOwner("ada"), "Ada prefers tables.", memory.Person, memory.Approved, "ada", time.Now()); err != nil {
		t.Fatal(err)
	}
	r, err := p.Converse(ctx, tenant, platform.Turn{ID: "mem-scope", Agent: "lead", Text: "Hello", Project: personal.Name, Actor: platform.ActorOf(ada)})
	if err != nil {
		t.Fatal(err)
	}
	run := await(t, p, r.ID, runs.Completed)
	req := runtime.Request{RunID: run.ID, Tenant: tenant, Definition: native.Revision{Spec: native.Spec{Name: "lead"}, Version: wire.VersionOf(1)}}
	cctx, claim := p.Claim(ctx, req)
	if out, err := useIn(t, cctx, p, req, platform.ToolShell, map[string]any{"command": "echo 'Ada works from Toronto.' > ../me/memory/notes/home.md"}); err != nil || !strings.HasPrefix(out, "exit 0") {
		t.Fatalf("shell = %q %v", out, err)
	}
	if err := claim.Release(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := p.Runs.Get(tenant, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	var me *runs.MemoryUse
	for i := range got.Memory {
		if got.Memory[i].Kind == "person" {
			me = &got.Memory[i]
		}
	}
	if me == nil || me.Version < 1 || me.Files < 1 || !me.Finished || me.Committed <= me.Version {
		t.Fatalf("the person's memory use = %+v (all %+v)", me, got.Memory)
	}

	view, err := p.ConversationMemories(ctx, ada, run.Thread)
	if err != nil {
		t.Fatal(err)
	}
	if !view.Isolated || len(view.Runs) == 0 || len(view.Conversation) == 0 || len(view.Session) == 0 {
		t.Fatalf("view = %+v", view)
	}
	var mine *platform.RunMemoryTree
	for i := range view.Runs[0].Trees {
		if view.Runs[0].Trees[i].Self {
			mine = &view.Runs[0].Trees[i]
		}
	}
	if mine == nil || !mine.Installed || mine.Committed != me.Committed || mine.Head != me.Committed {
		t.Fatalf("the run's own memory = %+v", mine)
	}
	// Someone else in the workspace never sees another person's memory named.
	bob := auth.Principal{Kind: auth.User, Tenant: tenant, Subject: "bob", Roles: []auth.Role{auth.Admin}}
	if theirs, err := p.ConversationMemories(ctx, bob, run.Thread); err == nil {
		for _, rm := range theirs.Runs {
			for _, tr := range rm.Trees {
				if tr.Kind == "person" {
					t.Fatalf("bob sees ada's memory: %+v", tr)
				}
			}
		}
	}
}
