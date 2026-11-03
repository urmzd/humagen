package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// exerciseWorkspaceFileContract verifies successful changes against the public
// schema and stale revisions without replacing either the saved or published file.
func exerciseWorkspaceFileContract(t *testing.T, c *contract, actor, other string) string {
	t.Helper()
	thread := c.call(actor, "POST", "/api/threads", "/api/threads", `{"text":"Workspace files","visibility":"private","post_only":true}`, http.StatusCreated)
	id := obj(thread.field("thread"))["id"].(string)
	base := "/api/threads/" + id + "/files"
	list := "/api/threads/{id}/files"
	content := "/api/threads/{id}/files/content"
	publish := "/api/threads/{id}/files/publish"
	c.call(actor, "GET", list, base, "", http.StatusOK)
	c.call(actor, "GET", content, base+"/content?path=../escape", "", http.StatusBadRequest)
	c.call(actor, "PUT", content, base+"/content", `{"path":"../escape","content":"x","revision":""}`, http.StatusBadRequest)
	saved := c.call(actor, "PUT", content, base+"/content", `{"path":"notes/plan.md","content":"# First version","revision":""}`, http.StatusOK)
	revision := saved.str("revision")
	c.call(actor, "GET", content, base+"/content?path=notes/plan.md", "", http.StatusOK)
	c.call(actor, "PUT", content, base+"/content", `{"path":"notes/plan.md","content":"stale","revision":""}`, http.StatusConflict)
	c.call(actor, "POST", publish, base+"/publish", `{"path":"notes/plan.md","revision":"bad"}`, http.StatusConflict)
	c.call(actor, "POST", publish, base+"/publish", `{"path":"../escape","revision":"bad"}`, http.StatusBadRequest)
	pub := c.call(actor, "POST", publish, base+"/publish", fmt.Sprintf(`{"path":"notes/plan.md","revision":%q}`, revision), http.StatusCreated)
	if pub.str("agent") != "" || pub.str("run") != "" || pub.str("created_by") == "" {
		t.Fatalf("human publication attribution: %s", pub.raw)
	}
	updated := c.call(actor, "PUT", content, base+"/content", fmt.Sprintf(`{"path":"notes/plan.md","content":"# Second version","revision":%q}`, revision), http.StatusOK)
	_, downloaded := c.f.raw(t, "/api/artifacts/"+pub.str("id")+"/content", actor)
	if downloaded != "# First version" {
		t.Fatalf("immutable publication changed: %q", downloaded)
	}
	c.call(actor, "DELETE", content, base+"/content?path=notes/plan.md&revision="+revision, "", http.StatusConflict)
	c.call(actor, "DELETE", content, base+"/content?path=../escape&revision="+revision, "", http.StatusBadRequest)
	c.call(actor, "DELETE", content, base+"/content?path=notes/plan.md&revision="+updated.str("revision"), "", http.StatusNoContent)
	for _, req := range []struct{ method, template, path, body string }{
		{"GET", list, base, ""}, {"GET", content, base + "/content?path=notes/plan.md", ""},
		{"PUT", content, base + "/content", `{"path":"x","content":"x","revision":""}`},
		{"DELETE", content, base + "/content?path=x&revision=bad", ""},
		{"POST", publish, base + "/publish", `{"path":"x","revision":"bad"}`},
	} {
		c.call(other, req.method, req.template, req.path, req.body, http.StatusNotFound)
	}
	return id
}

func TestWorkspaceFileRoutesSavePublishAndConfine(t *testing.T) {
	f := newFixture(t)
	c := &contract{t: t, f: f, spec: loadSpec(t), seen: map[string]bool{}}
	actor := f.person(t, "acme", "file-author", "File author", "humagen-admins")
	other := f.person(t, "other", "outsider", "Outsider", "humagen-admins")
	id := exerciseWorkspaceFileContract(t, c, actor, other)
	viewer := f.person(t, "acme", "viewer", "Viewer", "humagen-viewers")
	if res, _ := f.do(t, nil, "GET", "/api/threads/"+id+"/files", viewer, ""); res.StatusCode != http.StatusNotFound {
		t.Fatalf("private files exposed: %d", res.StatusCode)
	}
	// JSON escaping is counted separately from the file's actual byte limit.
	body, _ := json.Marshal(map[string]string{"path": "large.txt", "content": strings.Repeat("\t", 600000), "revision": ""})
	if res, out := f.do(t, nil, "PUT", "/api/threads/"+id+"/files/content", actor, string(body)); res.StatusCode != http.StatusOK {
		t.Fatalf("escaped file: %d %v", res.StatusCode, out)
	}
}
