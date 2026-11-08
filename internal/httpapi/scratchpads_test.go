package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// exerciseScratchpadContract creates, renames, edits and deletes scratchpads
// against the public schema, refuses stale versions and bounds, and keeps
// them from another workspace. It returns a scratchpad that is left in place.
func exerciseScratchpadContract(t *testing.T, c *contract, actor, other, thread string) string {
	t.Helper()
	list := "/api/threads/{id}/scratchpads"
	one := "/api/threads/{id}/scratchpads/{pad}"
	base := "/api/threads/" + thread + "/scratchpads"
	c.call(actor, "GET", list, base, "", http.StatusOK)
	plan := c.call(actor, "POST", list, base, `{"name":"Plan","content":"# Plan"}`, http.StatusCreated)
	id := plan.str("id")
	c.call(actor, "POST", list, base, `{"name":"plan","content":""}`, http.StatusConflict)
	c.call(actor, "POST", list, base, `{"name":"a/b","content":""}`, http.StatusBadRequest)
	c.call(actor, "GET", one, base+"/"+id, "", http.StatusOK)
	edited := c.call(actor, "PUT", one, base+"/"+id, `{"name":"Launch plan","content":"# Plan\n- one","version":1}`, http.StatusOK)
	c.call(actor, "PUT", one, base+"/"+id, `{"name":"Launch plan","content":"stale","version":7}`, http.StatusConflict)
	c.call(actor, "PUT", one, base+"/"+id, `{"name":"Launch plan","content":"none"}`, http.StatusConflict)
	risks := c.call(actor, "POST", list, base, `{"name":"Risks","content":""}`, http.StatusCreated)
	c.call(actor, "DELETE", one, base+"/"+risks.str("id")+"?version=9", "", http.StatusConflict)
	c.call(actor, "DELETE", one, base+"/"+risks.str("id")+"?version=1", "", http.StatusNoContent)
	c.call(actor, "GET", one, base+"/"+risks.str("id"), "", http.StatusNotFound)
	for _, req := range []struct{ method, template, path, body string }{
		{"GET", list, base, ""}, {"POST", list, base, `{"name":"x","content":""}`},
		{"GET", one, base + "/" + id, ""},
		{"PUT", one, base + "/" + id, fmt.Sprintf(`{"name":"x","content":"","version":%v}`, edited.field("version"))},
		{"DELETE", one, base + "/" + id + "?version=1", ""},
	} {
		c.call(other, req.method, req.template, req.path, req.body, http.StatusNotFound)
	}
	return id
}

func TestScratchpadRoutesConfineAndBound(t *testing.T) {
	f := newFixture(t)
	c := &contract{t: t, f: f, spec: loadSpec(t), seen: map[string]bool{}}
	actor := f.person(t, "acme", "pad-author", "Pad author", "humagen-admins")
	other := f.person(t, "other", "outsider", "Outsider", "humagen-admins")
	thread := c.call(actor, "POST", "/api/threads", "/api/threads", `{"text":"Scratchpads","visibility":"private","post_only":true}`, http.StatusCreated)
	id := obj(thread.field("thread"))["id"].(string)
	exerciseScratchpadContract(t, c, actor, other, id)
	viewer := f.person(t, "acme", "viewer", "Viewer", "humagen-viewers")
	if res, _ := f.do(t, nil, "GET", "/api/threads/"+id+"/scratchpads", viewer, ""); res.StatusCode != http.StatusNotFound {
		t.Fatalf("a private conversation's scratchpads were shown: %d", res.StatusCode)
	}
	// The largest scratchpad fits through the body limit with JSON escaping.
	body, _ := json.Marshal(map[string]string{"name": "Big", "content": strings.Repeat("\t", 256<<10)})
	if res, out := f.do(t, nil, "POST", "/api/threads/"+id+"/scratchpads", actor, string(body)); res.StatusCode != http.StatusCreated {
		t.Fatalf("largest scratchpad: %d %v", res.StatusCode, out)
	}
	body, _ = json.Marshal(map[string]string{"name": "Too big", "content": strings.Repeat("x", 256<<10+1)})
	if res, _ := f.do(t, nil, "POST", "/api/threads/"+id+"/scratchpads", actor, string(body)); res.StatusCode != http.StatusBadRequest {
		t.Fatalf("oversized scratchpad: %d", res.StatusCode)
	}
}

// exerciseSaveContract saves a person's message and a scratchpad as
// versioned artifacts, refuses what is not there, and keeps them from
// another workspace.
func exerciseSaveContract(t *testing.T, c *contract, actor, other, thread, pad string) {
	t.Helper()
	at := "/api/threads/{id}/artifacts"
	path := "/api/threads/" + thread + "/artifacts"
	msg := c.call(actor, "POST", at, path, `{"source":"message","id":"m-1","title":"Kickoff"}`, http.StatusCreated)
	if msg.str("title") != "Kickoff.md" || msg.str("created_by") == "" {
		t.Fatalf("saved message: %s", msg.raw)
	}
	first := c.call(actor, "POST", at, path, fmt.Sprintf(`{"source":"scratchpad","id":%q}`, pad), http.StatusCreated)
	again := c.call(actor, "POST", at, path, fmt.Sprintf(`{"source":"scratchpad","id":%q}`, pad), http.StatusCreated)
	if first.str("id") != again.str("id") || first.str("title") != again.str("title") || fmt.Sprint(again.field("version")) != "1" {
		t.Fatalf("saving unchanged source must reuse its artifact: %s then %s", first.raw, again.raw)
	}
	c.call(actor, "POST", at, path, `{"source":"message","id":"m-999"}`, http.StatusNotFound)
	c.call(actor, "POST", at, path, `{"source":"nope","id":"x"}`, http.StatusBadRequest)
	c.call(other, "POST", at, path, `{"source":"message","id":"m-1"}`, http.StatusNotFound)
}
