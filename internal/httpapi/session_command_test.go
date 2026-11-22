package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/saige/flow/isolation"
	"github.com/urmzd/saige/flow/isolation/isolationtest"
)

// bangMessage is the command record of the newest message of a session's
// answer, or nil.
func bangMessage(r reply) map[string]any {
	msgs := sliceOf(r.field("messages"))
	for i := len(msgs) - 1; i >= 0; i-- {
		if m := obj(obj(msgs[i])["message"]); m != nil {
			return obj(m["command"])
		}
	}
	return nil
}

// exerciseBangCommandContract checks a "!" message, which runs its command
// in the session's sandbox and posts how it came out, against the public
// schema on every route that takes one. The deployment c is on has no
// contained sandbox, so it refuses and makes no session; one with a sandbox
// runs it; one whose sandbox cannot start an instance says so.
func exerciseBangCommandContract(t *testing.T, c *contract) {
	t.Helper()
	actor := c.f.person(t, "acme", "bang-author", "Bang author", "humagen-admins")
	// Refused before any agent is looked for, and no session is made.
	c.call(actor, "POST", "/api/sessions", "/api/sessions", `{"agent":"helper","project":"~","text":"!echo hi","command":true}`, http.StatusConflict)

	leaser := &isolationtest.Leaser{Dir: t.TempDir()}
	ran := &contract{t: t, spec: c.spec, seen: c.seen,
		f: newFixtureWith(t, platform.Config{Sandbox: isolation.Sandbox{Leaser: leaser, Name: "test"}, SandboxContained: true})}
	actor = ran.f.person(t, "acme", "bang-author", "Bang author", "humagen-admins")
	other := ran.f.person(t, "other", "outsider", "Outsider", "humagen-admins")
	ran.call(actor, "POST", "/api/agents", "/api/agents", helperSpec, http.StatusCreated)
	started := ran.call(actor, "POST", "/api/sessions", "/api/sessions", `{"agent":"helper","project":"~","text":"!echo hello","command":true}`, http.StatusCreated)
	id := started.str("id")
	if cmd := bangMessage(started); cmd["command"] != "echo hello" || cmd["output"] != "hello\n" || fmt.Sprint(cmd["exit_code"]) != "0" {
		t.Fatalf("the first command: %s", started.raw)
	}
	for _, m := range sliceOf(started.field("messages")) {
		if obj(m)["run"] != nil {
			t.Fatalf("a command started a run: %s", started.raw)
		}
	}
	path := "/api/sessions/" + id + "/messages"
	posted := ran.call(actor, "POST", "/api/sessions/{id}/messages", path, `{"text":"!ls; exit 4","command":true}`, http.StatusCreated)
	if cmd := bangMessage(posted); cmd["command"] != "ls; exit 4" || fmt.Sprint(cmd["exit_code"]) != "4" {
		t.Fatalf("the second command: %s", posted.raw)
	}
	thread := ran.call(actor, "POST", "/api/threads/{id}/messages", "/api/threads/"+id+"/messages", `{"text":"!pwd","command":true}`, http.StatusCreated)
	if obj(obj(thread.field("message"))["command"])["command"] != "pwd" || len(sliceOf(thread.field("runs"))) != 0 {
		t.Fatalf("the thread's command: %s", thread.raw)
	}
	ran.call(actor, "POST", "/api/sessions/{id}/messages", path, `{"text":"!   ","command":true}`, http.StatusBadRequest)
	ran.call(actor, "POST", "/api/sessions/{id}/messages", path, `{"text":"!ls","command":true,"agent":"helper","fresh":true}`, http.StatusBadRequest)
	// Another workspace's session is not there; nor is one not shared.
	ran.call(other, "POST", "/api/sessions/{id}/messages", path, `{"text":"!ls","command":true}`, http.StatusNotFound)
	if n := len(leaser.Instances()); n != 3 || len(leaser.Open()) != 0 {
		t.Fatalf("instances = %d, open = %d", n, len(leaser.Open()))
	}

	down := isolation.LeaserFunc(func(context.Context, isolation.Request) (isolation.Instance, error) {
		return nil, errors.New("the daemon is not there")
	})
	none := &contract{t: t, spec: c.spec, seen: c.seen,
		f: newFixtureWith(t, platform.Config{Sandbox: isolation.Sandbox{Leaser: down, Name: "test"}, SandboxContained: true})}
	actor = none.f.person(t, "acme", "bang-author", "Bang author", "humagen-admins")
	none.call(actor, "POST", "/api/agents", "/api/agents", helperSpec, http.StatusCreated)
	none.call(actor, "POST", "/api/sessions", "/api/sessions", `{"agent":"helper","project":"~","text":"!echo hi","command":true}`, http.StatusServiceUnavailable)
}

func TestABangCommandRunsInTheSessionSandboxAndStartsNoRun(t *testing.T) {
	c := &contract{t: t, f: newFixture(t), spec: loadSpec(t), seen: map[string]bool{}}
	exerciseBangCommandContract(t, c)
}
