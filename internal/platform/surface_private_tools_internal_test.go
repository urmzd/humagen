package platform

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
	"github.com/urmzd/saige/flow/event"
	"github.com/urmzd/saige/flow/runtime"
	"github.com/urmzd/saige/flow/session"
	flowturn "github.com/urmzd/saige/flow/turn"
)

// A session in a tree a Slack thread started answers in that Slack thread,
// so none of its sessions is the person's own private conversation: not the
// root, not a child an agent handed work to (startSessionChild), which
// copies the root's people and has no surface of its own, and not a child
// the person opened from the console. Personal connection status and consent
// links stay out of all of them.
func TestASlackSessionTreeIsNoOnesOwnPrivateConversation(t *testing.T) {
	policy, err := auth.ParsePolicy("connections", `role user { allow * on * } grant user to subject "alice"`)
	if err != nil {
		t.Fatal(err)
	}
	p, err := New(Config{Workspace: t.TempDir(), Policy: policy, ConnectionAuth: nativeConfig("drive")})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close() }()
	alice := p.byPolicy("t", "alice")
	if _, err := p.Agents.Create(t.Context(), "t", wire.AgentSpec{Name: "lead", Model: "offline", Provider: "offline", SystemPrompt: "Lead."}, "test"); err != nil {
		t.Fatal(err)
	}
	authorizeNative(t, p, alice, "drive", "person")
	at := time.Now()
	people := []threads.Participant{{Kind: threads.Person, Name: "alice", Owner: true, At: at}}
	create := func(th threads.Thread) threads.Thread {
		t.Helper()
		th.Channel, th.Visibility, th.CreatedBy, th.Participants = "general", threads.Private, "alice", people
		if th.Session != nil {
			th.Default, th.Agents = th.Session.Agent, []string{th.Session.Agent}
		}
		got, err := p.Threads.Create("t", th, at)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	runIn := func(id, thread string) runtime.Request {
		t.Helper()
		tag := wire.NewEvent(wire.Raise{ID: id, Tenant: "t", Agent: BuiltinBuilder, Origin: runs.OriginAPI, Actor: event.Actor{ID: "alice", User: "alice"}, At: at, Context: map[string]string{runs.MetaThread: thread}})
		if err := p.Runs.Record(id, tag, 1); err != nil {
			t.Fatal(err)
		}
		return runtime.Request{Tenant: "t", RunID: id, Definition: flowturn.Pinned{Spec: flowturn.Spec{Name: BuiltinBuilder}}}
	}
	own := create(threads.Thread{ID: "th-0123456789abcdef"})
	// A Slack thread's root, opened as StartSurfaceSession opens it.
	place := "slack://T0/C0PUBLIC/1700000000.000100"
	rec, err := p.Sessions.Open(t.Context(), "t", session.Open{Parent: place, Agent: "lead", Channel: wire.Channel("t")})
	if err != nil {
		t.Fatal(err)
	}
	root := create(threads.Thread{ID: rec.ID, Surface: &threads.Surface{Client: "app", Channel: "C0PUBLIC", TS: "1700000000.000100"}, Session: &threads.SessionIdentity{Agent: "lead", Parent: place}})
	// As startSessionChild files a child an agent hands work to.
	handed := create(threads.Thread{ID: "s_" + strings.Repeat("b", 32), Session: &threads.SessionIdentity{Agent: BuiltinBuilder, Parent: root.ID}})
	deeper := create(threads.Thread{ID: "s_" + strings.Repeat("c", 32), Session: &threads.SessionIdentity{Agent: BuiltinBuilder, Parent: handed.ID}})
	// A child the person opens under the Slack root from the console.
	opened, err := p.StartSession(t.Context(), alice, SessionInput{Parent: root.ID, Agent: "lead", Text: "connect my drive"})
	if err != nil {
		t.Fatal(err)
	}
	if opened.Thread.Surface != nil || opened.Thread.Session == nil || opened.Thread.Session.Parent != root.ID {
		t.Fatalf("the console child = %+v", opened.Thread)
	}

	ownReq := runIn("own-run", own.ID)
	if _, person, err := p.ownPrivateConversation(ownReq); err != nil || person != "alice" {
		t.Fatalf("the person's own console session = %q, %v", person, err)
	}
	for _, connect := range []bool{false, true} {
		if raw, err := p.builderConnectionsTool(ownReq, connect).Execute(t.Context(), map[string]any{"provider": "drive"}); err != nil || raw == "" {
			t.Fatalf("own session connect=%v: %q %v", connect, raw, err)
		}
	}
	for name, thread := range map[string]string{"root": root.ID, "handed": handed.ID, "deeper": deeper.ID, "opened": opened.ID} {
		req := runIn(name+"-run", thread)
		if _, _, err := p.ownPrivateConversation(req); err == nil {
			t.Fatalf("the Slack tree's %s session is someone's own private conversation", name)
		}
		for _, connect := range []bool{false, true} {
			raw, err := p.builderConnectionsTool(req, connect).Execute(t.Context(), map[string]any{"provider": "drive"})
			if !errors.Is(err, auth.ErrForbidden) || raw != "" {
				t.Fatalf("the Slack tree's %s session, connect=%v: %q %v", name, connect, raw, err)
			}
		}
	}
}

// catalog_search shows an owner's unpublished entries only in the owner's own
// private conversation, never in a session a Slack thread's tree holds.
func TestCatalogSearchInASlackSessionTreeShowsNoOwnersEntries(t *testing.T) {
	f := newCatalogFixture(t)
	if out := f.search(t, f.runIn(t, "own", "owner", true, "owner"), "region"); !strings.Contains(out, "churn_monthly") {
		t.Fatalf("own = %s", out)
	}
	people := []threads.Participant{{Kind: threads.Person, Name: "owner", Owner: true}}
	root := threads.Thread{ID: "s_" + strings.Repeat("d", 32), Project: "Data", Channel: "general", CreatedBy: "owner", Visibility: threads.Private, Participants: people, Default: "worker", Agents: []string{"worker"}, Surface: &threads.Surface{Client: "app", Channel: "C0PUBLIC", TS: "1700000000.000100"}, Session: &threads.SessionIdentity{Agent: "worker", Parent: "slack://T0/C0PUBLIC/1700000000.000100"}}
	child := threads.Thread{ID: "s_" + strings.Repeat("e", 32), Project: "Data", Channel: "general", CreatedBy: "owner", Visibility: threads.Private, Participants: people, Default: "worker", Agents: []string{"worker"}, Session: &threads.SessionIdentity{Agent: "worker", Parent: root.ID}}
	for _, th := range []threads.Thread{root, child} {
		if _, err := f.p.Threads.Create("tenant", th, f.p.now()); err != nil {
			t.Fatal(err)
		}
	}
	for _, th := range []threads.Thread{root, child} {
		id := "run-" + th.ID[2:6]
		tag := wire.NewEvent(wire.Raise{ID: id, Origin: runs.OriginAPI, Tenant: "tenant", Agent: "worker", Actor: event.Actor{ID: "owner", User: "owner"}, At: f.p.now(), Context: map[string]string{runs.MetaProject: "Data", runs.MetaThread: th.ID}})
		if err := f.p.Runs.Record(id, tag, 1); err != nil {
			t.Fatal(err)
		}
		if out := f.search(t, runtime.Request{Tenant: "tenant", RunID: id}, "region"); strings.Contains(out, "churn_monthly") {
			t.Fatalf("the Slack tree's session %s shows the owner's unpublished entries: %s", th.ID, out)
		}
	}
}
