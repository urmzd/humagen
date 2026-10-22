package httpapi

import (
	"net/http"
	"strings"
	"testing"
)

// The console's chat: a conversation starts private, nothing runs until an
// agent is tagged, and the tagged agent is sent the conversation so far.
// Sharing it makes it shared in a project's channel, with the people named
// added, so a member who could not read it reads and joins it. Sharing into
// a project the sharer cannot see answers as sharing into one that is not
// there.
func TestAConversationIsPrivateUntilShared(t *testing.T) {
	f := newFixture(t)
	owner := f.machine(t, "acme", everything...)
	want(t, f, owner, "POST", "/api/agents", helperSpec, http.StatusCreated)
	want(t, f, owner, "POST", "/api/projects", `{"name":"Secret"}`, http.StatusCreated)
	want(t, f, owner, "PUT", "/api/projects/Secret/members", `{"version":1,"members":[{"who":"svc-acme","role":"owner"}]}`, http.StatusOK)
	want(t, f, owner, "POST", "/api/projects", `{"name":"Field"}`, http.StatusCreated)
	want(t, f, owner, "PUT", "/api/projects/Field/members", `{"version":1,"members":[{"who":"*","role":"member"}]}`, http.StatusOK)
	ana := f.person(t, "acme", "00u-ana", "Ana", "warp-operators")
	bo := f.person(t, "acme", "00u-bo", "Bo", "warp-operators")
	cy := f.person(t, "acme", "00u-cy", "Cy", "warp-operators")

	started := want(t, f, ana, "POST", "/api/threads", `{"project":"Field","text":"FIRST-THOUGHT"}`, http.StatusCreated)
	th := obj(started["thread"])
	id := th["id"].(string)
	if th["visibility"] != "private" || started["rule"] != "untagged" || len(sliceOf(started["runs"])) != 0 {
		t.Fatalf("a conversation the console starts is %s", mustJSON(t, started))
	}
	second := want(t, f, ana, "POST", "/api/threads/"+id+"/messages", `{"text":"SECOND-THOUGHT"}`, http.StatusCreated)
	if second["rule"] != "untagged" || len(sliceOf(second["runs"])) != 0 {
		t.Fatalf("a second untagged message did %s", mustJSON(t, second))
	}
	want(t, f, bo, "GET", "/api/threads/"+id, "", http.StatusNotFound)
	want(t, f, bo, "POST", "/api/threads/"+id+"/share", `{}`, http.StatusNotFound)

	// A project ana cannot see answers as one that is not there.
	code, out := send(t, f, ana, "POST", "/api/threads/"+id+"/share", `{"project":"Secret"}`)
	ghostCode, ghostOut := send(t, f, ana, "POST", "/api/threads/"+id+"/share", `{"project":"Nowhere"}`)
	if code != ghostCode || strings.ReplaceAll(out, "Secret", "Nowhere") != ghostOut {
		t.Fatalf("sharing into a hidden project = %d %s; into none = %d %s", code, out, ghostCode, ghostOut)
	}

	page := want(t, f, ana, "POST", "/api/threads/"+id+"/share", `{"add":["bo"]}`, http.StatusOK)
	shared := obj(page["thread"])
	if shared["visibility"] != "shared" || shared["channel"] != "general" || len(sliceOf(page["turns"])) != 2 || page["answers"] == nil {
		t.Fatalf("shared, the thread reads %s", mustJSON(t, page))
	}
	if raw := mustJSON(t, shared["participants"]); !strings.Contains(raw, `"name":"bo"`) {
		t.Fatalf("bo was not added: %s", raw)
	}
	// cy, a member not added, reads it now too, and may join.
	want(t, f, cy, "GET", "/api/threads/"+id, "", http.StatusOK)
	asked := want(t, f, bo, "POST", "/api/threads/"+id+"/messages", `{"text":"@helper what do you make of it?"}`, http.StatusCreated)
	runs := sliceOf(asked["runs"])
	if asked["rule"] != "tagged" || len(runs) != 1 {
		t.Fatalf("a tagged message did %s", mustJSON(t, asked))
	}
	runID := obj(runs[0])["id"].(string)
	run, err := f.p.Runs.Get("acme", runID)
	if err != nil {
		t.Fatal(err)
	}
	if text := run.Tag().Text; !strings.Contains(text, "FIRST-THOUGHT") || !strings.Contains(text, "SECOND-THOUGHT") {
		t.Errorf("the tagged agent was not sent the conversation so far: %s", text)
	}
	// Once a run is filed, the conversation stays in its project.
	if code, out := send(t, f, ana, "POST", "/api/threads/"+id+"/share", `{"project":"~"}`); code != http.StatusConflict || !strings.Contains(out, "a conversation that has runs stays in its project") {
		t.Fatalf("moving a conversation that has runs = %d %s", code, out)
	}
	// cy takes no part, so cy does not share it.
	want(t, f, cy, "POST", "/api/threads/"+id+"/share", `{}`, http.StatusForbidden)
}
