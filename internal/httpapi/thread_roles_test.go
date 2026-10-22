package httpapi

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/urmzd/warp/internal/platform"
	"github.com/urmzd/warp/pkg/memory"
)

// A project's role holds in its channels, experts and threads, after the
// route's permission: an owner changes its channels and experts, a member
// starts and posts in its threads, and a viewer only answers what they were
// asked, which starts nothing. A thread is changed by whoever started it or
// an owner of its project.
func TestAProjectsRolesHoldInItsChannelsExpertsAndThreads(t *testing.T) {
	f := newFixture(t)
	admin := f.machineAs(t, "svc-admin", "acme", everything...)
	ana := f.person(t, "acme", "00u-ana", "Ana", "warp-operators")
	bo := f.person(t, "acme", "00u-bo", "Bo", "warp-operators")
	cy := f.person(t, "acme", "00u-cy", "Cy", "warp-operators")
	want(t, f, admin, "POST", "/api/projects", `{"name":"Globex","members":[{"who":"*","role":"viewer"},{"who":"00u-ana","role":"owner"},{"who":"00u-bo","role":"member"}]}`, http.StatusCreated)

	experts := want(t, f, cy, "GET", "/api/projects/Globex/experts", "", http.StatusOK)
	want(t, f, cy, "GET", "/api/projects/Globex/channels", "", http.StatusOK)
	for _, tc := range []struct{ method, path, body string }{
		{"POST", "/api/projects/Globex/channels", `{"name":"renewals"}`},
		{"PUT", "/api/projects/Globex/channels/general", `{"version":0,"purpose":"Bo's"}`},
		{"DELETE", "/api/projects/Globex/channels/general", ""},
		{"PUT", "/api/projects/Globex/experts", fmt.Sprintf(`{"version":%v,"experts":[{"who":"00u-bo","topics":["billing"]}]}`, experts["version"])},
	} {
		if got, out := status(t, f, bo, tc.method, tc.path, tc.body); got != http.StatusForbidden || out["error"] != "you need to own Globex" {
			t.Errorf("a member %s %s = %d %v, want 403", tc.method, tc.path, got, out)
		}
	}
	want(t, f, ana, "POST", "/api/projects/Globex/channels", `{"name":"renewals"}`, http.StatusCreated)
	want(t, f, ana, "PUT", "/api/projects/Globex/experts", fmt.Sprintf(`{"version":%v,"experts":[{"who":"00u-bo","topics":["billing"]}]}`, experts["version"]), http.StatusOK)

	// A viewer starts nothing, and posts nothing that could start a run.
	if out := want(t, f, cy, "POST", "/api/threads", `{"project":"Globex","visibility":"shared","text":"hi","post_only":true}`, http.StatusForbidden); out["error"] != "you need to be a member of Globex" {
		t.Fatalf("a viewer starting a thread = %v", out)
	}
	started := want(t, f, bo, "POST", "/api/threads", `{"project":"Globex","channel":"renewals","visibility":"shared","text":"@cy can you check the renewal?"}`, http.StatusCreated)
	id := started["thread"].(map[string]any)["id"].(string)
	if out := want(t, f, cy, "POST", "/api/threads/"+id+"/messages", `{"text":"and another thing","post_only":true}`, http.StatusForbidden); out["error"] != "you need to be a member of Globex" {
		t.Fatalf("a viewer posting = %v", out)
	}
	// What tagged them, a viewer answers.
	mentions := want(t, f, cy, "GET", "/api/inbox?kind=mention", "", http.StatusOK)["messages"].([]any)
	if len(mentions) != 1 {
		t.Fatalf("cy's mentions = %v", mentions)
	}
	m := mentions[0].(map[string]any)
	part := m["parts"].([]any)[0].(map[string]any)
	if fmt.Sprint(part["actions"]) != "[reply done]" {
		t.Fatalf("a viewer tagged may %v", part["actions"])
	}
	want(t, f, cy, "POST", fmt.Sprintf("/api/inbox/%s/parts/%s/resolve", m["id"], part["id"]), `{"action":"reply","text":"It renews in May."}`, http.StatusOK)

	// Whoever started a thread, or an owner of its project, changes it.
	version := func() any {
		t.Helper()
		return want(t, f, ana, "GET", "/api/threads/"+id, "", http.StatusOK)["thread"].(map[string]any)["version"]
	}
	if out := want(t, f, cy, "PUT", "/api/threads/"+id, fmt.Sprintf(`{"version":%v,"title":"Cy's"}`, version()), http.StatusForbidden); out["error"] != "only whoever started this thread, or an owner of its project, changes it" {
		t.Fatalf("a viewer changing a thread = %v", out)
	}
	want(t, f, bo, "PUT", "/api/threads/"+id, fmt.Sprintf(`{"version":%v,"title":"Globex renewal"}`, version()), http.StatusOK)
	want(t, f, ana, "PUT", "/api/threads/"+id, fmt.Sprintf(`{"version":%v,"title":"Globex renewal, May"}`, version()), http.StatusOK)
	want(t, f, cy, "PUT", "/api/threads/"+id+"/participants", fmt.Sprintf(`{"version":%v,"add":[{"kind":"person","name":"cy","at":"2026-01-01T00:00:00Z"}]}`, version()), http.StatusForbidden)
	want(t, f, bo, "PUT", "/api/threads/"+id+"/participants", fmt.Sprintf(`{"version":%v,"add":[{"kind":"person","name":"cy","at":"2026-01-01T00:00:00Z"}]}`, version()), http.StatusOK)
	dee := f.person(t, "acme", "00u-dee", "Dee", "warp-operators")
	want(t, f, admin, "PUT", "/api/projects/Globex/members", `{"version":2,"members":[{"who":"*","role":"viewer"},{"who":"00u-ana","role":"owner"},{"who":"00u-bo","role":"member"},{"who":"00u-dee","role":"member"}]}`, http.StatusOK)
	if out := want(t, f, dee, "PUT", "/api/threads/"+id+"/participants", fmt.Sprintf(`{"version":%v,"remove":["cy"]}`, version()), http.StatusForbidden); out["error"] != "only whoever started this thread, or an owner of its project, changes it" {
		t.Fatalf("another member removing someone = %v", out)
	}
	want(t, f, bo, "PUT", "/api/threads/"+id+"/participants", fmt.Sprintf(`{"version":%v,"remove":["cy"]}`, version()), http.StatusOK)
}

// What a thread's runs teach a project is kept unasked, when the workspace
// chose that, only when the person who posted could keep it themselves, as
// for a turn through POST /api/runs.
func TestWhatAThreadTeachesAProjectIsKeptUnaskedOnlyForItsOwners(t *testing.T) {
	f := newFixtureWith(t, platform.Config{OfflineHooks: true})
	admin := f.machineAs(t, "svc-admin", "acme", everything...)
	ana := f.person(t, "acme", "00u-ana", "Ana", "warp-operators")
	bo := f.person(t, "acme", "00u-bo", "Bo", "warp-operators")
	for _, name := range []string{"lead", "scribe"} {
		want(t, f, admin, "POST", "/api/agents", fmt.Sprintf(`{"name":%q,"model":"offline","provider":"offline","system_prompt":"x"}`, name), http.StatusCreated)
	}
	want(t, f, admin, "PUT", "/api/settings", `{"memory_agent":"scribe","memory_auto_approve":true}`, http.StatusOK)
	for _, name := range []string{"Field", "Ops"} {
		want(t, f, admin, "POST", "/api/projects", fmt.Sprintf(`{"name":%q,"members":[{"who":"*","role":"member"},{"who":"00u-ana","role":"owner"}]}`, name), http.StatusCreated)
	}
	taught := func(project string) []string {
		t.Helper()
		for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
			var out []string
			for _, n := range f.p.Memory.List("acme", memory.ProjectOwner(project)) {
				out = append(out, string(n.Status))
			}
			if len(out) > 0 {
				return out
			}
		}
		t.Fatalf("the memory agent wrote nothing for %s", project)
		return nil
	}

	want(t, f, ana, "POST", "/api/threads", `{"project":"Field","visibility":"shared","text":"@lead plan the launch"}`, http.StatusCreated)
	for _, s := range taught("Field") {
		if s != string(memory.Approved) {
			t.Fatalf("an owner's message left Field's notes %v", taught("Field"))
		}
	}
	want(t, f, bo, "POST", "/api/threads", `{"project":"Ops","visibility":"shared","text":"@lead plan the launch"}`, http.StatusCreated)
	for _, s := range taught("Ops") {
		if s != string(memory.Pending) {
			t.Fatalf("a member's message left Ops's notes %v", taught("Ops"))
		}
	}
}
