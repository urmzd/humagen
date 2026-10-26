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
	ana := f.person(t, "acme", "00u-ana", "Ana", "humagen-operators")
	bo := f.person(t, "acme", "00u-bo", "Bo", "humagen-operators")
	cy := f.person(t, "acme", "00u-cy", "Cy", "humagen-operators")

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
	if notAdded, ok := page["not_added"].([]any); !ok || len(notAdded) != 0 {
		t.Fatalf("a share that added everyone it named says not_added %v", page["not_added"])
	}
	if raw, _ := read(t, f, bo, "/api/inbox"); !strings.Contains(raw, "Ana added you to a conversation in Field #general") {
		t.Errorf("bo was not told he was added: %s", raw)
	}
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
	if code, out := send(t, f, ana, "POST", "/api/threads/"+id+"/share", `{"project":"~"}`); code != http.StatusConflict {
		t.Fatalf("moving a conversation that has runs = %d %s", code, out)
	}
	// cy takes no part, so cy does not share it.
	want(t, f, cy, "POST", "/api/threads/"+id+"/share", `{}`, http.StatusForbidden)
}

// A conversation's selected agents are in its view, and selecting an agent
// the caller may not read answers exactly as selecting one that does not
// exist.
func TestSelectingAHiddenAgentAnswersAsAnUnknownOne(t *testing.T) {
	f := newFixture(t)
	owner := f.machine(t, "acme", everything...)
	want(t, f, owner, "POST", "/api/agents", helperSpec, http.StatusCreated)
	want(t, f, owner, "POST", "/api/agents", spySpec, http.StatusCreated)
	want(t, f, owner, "POST", "/api/projects", `{"name":"Secret"}`, http.StatusCreated)
	want(t, f, owner, "PUT", "/api/projects/Secret/members", `{"version":1,"members":[{"who":"svc-acme","role":"owner"}]}`, http.StatusOK)
	want(t, f, owner, "PUT", "/api/agents/spy/home", `{"project":"Secret"}`, http.StatusOK)
	want(t, f, owner, "POST", "/api/projects", `{"name":"Field"}`, http.StatusCreated)
	want(t, f, owner, "PUT", "/api/projects/Field/members", `{"version":1,"members":[{"who":"*","role":"member"}]}`, http.StatusOK)
	ana := f.person(t, "acme", "00u-ana", "Ana", "humagen-operators")

	started := want(t, f, ana, "POST", "/api/threads", `{"project":"Field","agents":["helper"],"text":"hello"}`, http.StatusCreated)
	th := obj(started["thread"])
	if started["rule"] != "routed" || len(sliceOf(th["agents"])) != 1 || len(sliceOf(started["runs"])) != 1 || obj(sliceOf(started["runs"])[0])["routed"] != "all" {
		t.Fatalf("a conversation with helper selected started %s", mustJSON(t, started))
	}
	id := th["id"].(string)
	page := want(t, f, ana, "GET", "/api/threads/"+id, "", http.StatusOK)
	if got := sliceOf(obj(page["thread"])["agents"]); len(got) != 1 || got[0] != "helper" {
		t.Fatalf("the view's agents are %v", got)
	}
	for _, body := range []string{`{"project":"Field","agents":["%s"],"text":"x"}`} {
		code, out := send(t, f, ana, "POST", "/api/threads", strings.ReplaceAll(body, "%s", "spy"))
		ghostCode, ghostOut := send(t, f, ana, "POST", "/api/threads", strings.ReplaceAll(body, "%s", "ghost"))
		if code != http.StatusBadRequest || code != ghostCode || strings.ReplaceAll(out, "spy", "ghost") != ghostOut {
			t.Fatalf("selecting a hidden agent = %d %s; an unknown one = %d %s", code, out, ghostCode, ghostOut)
		}
	}
	code, out := send(t, f, ana, "PUT", "/api/threads/"+id, `{"version":1,"visibility":"private","agents":["spy"]}`)
	ghostCode, ghostOut := send(t, f, ana, "PUT", "/api/threads/"+id, `{"version":1,"visibility":"private","agents":["ghost"]}`)
	if code != http.StatusBadRequest || code != ghostCode || strings.ReplaceAll(out, "spy", "ghost") != ghostOut {
		t.Fatalf("selecting a hidden agent = %d %s; an unknown one = %d %s", code, out, ghostCode, ghostOut)
	}
}
