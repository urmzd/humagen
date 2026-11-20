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

const commandsRoute = "/api/threads/{id}/sandboxes/commands"

// commandThread starts a private conversation for actor and returns its id.
func commandThread(c *contract, actor string) string {
	thread := c.call(actor, "POST", "/api/threads", "/api/threads", `{"text":"Commands","visibility":"private","post_only":true}`, http.StatusCreated)
	return obj(thread.field("thread"))["id"].(string)
}

// exerciseSandboxCommandContract checks every answer of the command route
// against the public schema. The deployment c is on has no contained
// sandbox, so it refuses; one with a sandbox runs the command, and one whose
// sandbox cannot start an instance says so. All three share what c has seen.
func exerciseSandboxCommandContract(t *testing.T, c *contract) {
	t.Helper()
	actor := c.f.person(t, "acme", "command-author", "Command author", "humagen-admins")
	id := commandThread(c, actor)
	boxes := c.call(actor, "GET", "/api/threads/{id}/sandboxes", "/api/threads/"+id+"/sandboxes", "", http.StatusOK)
	if obj(boxes.field("session"))["commands"] != false {
		t.Fatalf("commands offered on the host: %s", boxes.raw)
	}
	c.call(actor, "POST", commandsRoute, "/api/threads/"+id+"/sandboxes/commands", `{"command":"echo hi"}`, http.StatusConflict)

	leaser := &isolationtest.Leaser{Dir: t.TempDir()}
	ran := &contract{t: t, spec: c.spec, seen: c.seen,
		f: newFixtureWith(t, platform.Config{Sandbox: isolation.Sandbox{Leaser: leaser, Name: "test"}, SandboxContained: true})}
	actor = ran.f.person(t, "acme", "command-author", "Command author", "humagen-admins")
	other := ran.f.person(t, "other", "outsider", "Outsider", "humagen-admins")
	viewer := ran.f.person(t, "acme", "viewer", "Viewer", "humagen-viewers")
	id = commandThread(ran, actor)
	path := "/api/threads/" + id + "/sandboxes/commands"
	boxes = ran.call(actor, "GET", "/api/threads/{id}/sandboxes", "/api/threads/"+id+"/sandboxes", "", http.StatusOK)
	if obj(boxes.field("session"))["commands"] != true {
		t.Fatalf("commands not offered: %s", boxes.raw)
	}
	out := ran.call(actor, "POST", commandsRoute, path, `{"command":"echo made > made.txt; echo done; exit 2"}`, http.StatusOK)
	if fmt.Sprint(out.field("exit_code")) != "2" || out.str("output") != "done\n" || out.field("timed_out") != false {
		t.Fatalf("result: %s", out.raw)
	}
	if files, _ := out.field("files").([]any); len(files) != 1 || obj(files[0])["path"] != "made.txt" {
		t.Fatalf("files: %s", out.raw)
	}
	ran.call(actor, "GET", "/api/threads/{id}/files/content", "/api/threads/"+id+"/files/content?path=made.txt", "", http.StatusOK)
	ran.call(actor, "POST", commandsRoute, path, `{"command":"  "}`, http.StatusBadRequest)
	ran.call(actor, "POST", commandsRoute, path, `{"command":"echo hi","cwd":"/"}`, http.StatusBadRequest)
	// Another workspace's conversation is not there; a reader may not run.
	ran.call(other, "POST", commandsRoute, path, `{"command":"echo hi"}`, http.StatusNotFound)
	ran.call(viewer, "POST", commandsRoute, path, `{"command":"echo hi"}`, http.StatusForbidden)
	ran.call("", "POST", commandsRoute, path, `{"command":"echo hi"}`, http.StatusUnauthorized)
	if n := len(leaser.Instances()); n != 1 || len(leaser.Open()) != 0 {
		t.Fatalf("instances = %d, open = %d", n, len(leaser.Open()))
	}

	down := isolation.LeaserFunc(func(context.Context, isolation.Request) (isolation.Instance, error) {
		return nil, errors.New("the daemon is not there")
	})
	none := &contract{t: t, spec: c.spec, seen: c.seen,
		f: newFixtureWith(t, platform.Config{Sandbox: isolation.Sandbox{Leaser: down, Name: "test"}, SandboxContained: true})}
	actor = none.f.person(t, "acme", "command-author", "Command author", "humagen-admins")
	id = commandThread(none, actor)
	none.call(actor, "POST", commandsRoute, "/api/threads/"+id+"/sandboxes/commands", `{"command":"echo hi"}`, http.StatusServiceUnavailable)
}

func TestSandboxCommandRouteRunsForItsPosterAndNobodyElse(t *testing.T) {
	c := &contract{t: t, f: newFixture(t), spec: loadSpec(t), seen: map[string]bool{}}
	exerciseSandboxCommandContract(t, c)
}
