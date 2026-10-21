package httpapi

import (
	"net/http"
	"testing"
)

// person signs an access token for a person: one with groups, which the
// verifier reads as a person rather than a credential.
func (f *fixture) person(t *testing.T, org, subject, name string, groups ...string) string {
	t.Helper()
	tok := f.iss.Sign(t, subject, audience, map[string]any{"org": org, "groups": groups, "name": name})
	// Whoever calls is put in the workspace's directory, so they can be
	// tagged by their handle.
	f.do(t, nil, "GET", "/api/me", tok, "")
	return tok
}

// A person posts only as themselves: no body names an author or an agent to
// speak as, and what is posted is the caller's.
func TestNoRouteLetsAnyonePostAsSomeoneElse(t *testing.T) {
	f := newFixture(t)
	ana := f.person(t, "acme", "00u-ana", "Ana", "warp-admins")
	for _, body := range []string{
		`{"visibility":"shared","text":"hi","by":"00u-vic"}`,
		`{"visibility":"shared","text":"hi","agent":"helper"}`,
		`{"visibility":"shared","text":"hi","name":"Vic"}`,
	} {
		if res, out := f.do(t, nil, "POST", "/api/threads", ana, body); res.StatusCode != http.StatusBadRequest {
			t.Errorf("%s = %d %v, want 400", body, res.StatusCode, out)
		}
	}
	res, out := f.do(t, nil, "POST", "/api/threads", ana, `{"visibility":"shared","text":"hello","post_only":true}`)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("starting a thread = %d %v", res.StatusCode, out)
	}
	msg := out["message"].(map[string]any)
	if msg["by"] != "00u-ana" || msg["name"] != "Ana" || msg["via"] != "console" {
		t.Fatalf("posted as %v", msg)
	}
	id := out["thread"].(map[string]any)["id"].(string)
	if res, out := f.do(t, nil, "POST", "/api/threads/"+id+"/messages", ana, `{"text":"again","by":"00u-vic"}`); res.StatusCode != http.StatusBadRequest {
		t.Fatalf("a message naming its author = %d %v", res.StatusCode, out)
	}
	if res, out := f.do(t, nil, "POST", "/api/threads/"+id+"/messages", f.machine(t, "acme", everything...), `{"text":"as a credential"}`); res.StatusCode != http.StatusForbidden {
		t.Fatalf("a credential acting for nobody = %d %v", res.StatusCode, out)
	}
}

// A person tagged is sent a mention, and replies to it from the inbox: the
// reply is a message by them in the thread, and starts no run.
func TestAMentionIsRepliedToFromTheInbox(t *testing.T) {
	f := newFixture(t)
	ana := f.person(t, "acme", "00u-ana", "Ana", "warp-admins")
	bo := f.person(t, "acme", "00u-bo", "Bo", "warp-admins")
	res, out := f.do(t, nil, "POST", "/api/threads", ana, `{"visibility":"shared","text":"@bo can you check the renewal?"}`)
	if res.StatusCode != http.StatusCreated || out["rule"] != "people" {
		t.Fatalf("tagging bo = %d %v", res.StatusCode, out)
	}
	thread := out["thread"].(map[string]any)["id"].(string)
	_, inbox := f.do(t, nil, "GET", "/api/inbox?kind=mention", bo, "")
	list, _ := inbox["messages"].([]any)
	if len(list) != 1 {
		t.Fatalf("bo's inbox holds %v", inbox)
	}
	m := list[0].(map[string]any)
	part := m["parts"].([]any)[0].(map[string]any)
	if acts := part["actions"].([]any); len(acts) != 2 || acts[0] != "reply" || acts[1] != "done" {
		t.Fatalf("bo may %v", acts)
	}
	if _, theirs := f.do(t, nil, "GET", "/api/inbox?kind=mention", ana, ""); len(theirs["messages"].([]any)) != 0 {
		t.Fatalf("ana reads bo's mention: %v", theirs)
	}
	path := "/api/inbox/" + m["id"].(string) + "/parts/" + part["id"].(string) + "/resolve"
	if res, out := f.do(t, nil, "POST", path, bo, `{"action":"reply","text":"Checked: it renews in May."}`); res.StatusCode != http.StatusOK {
		t.Fatalf("replying = %d %v", res.StatusCode, out)
	}
	_, page := f.do(t, nil, "GET", "/api/threads/"+thread, ana, "")
	turns := page["turns"].([]any)
	if len(turns) != 2 {
		t.Fatalf("the thread holds %v", page)
	}
	reply := turns[1].(map[string]any)["message"].(map[string]any)
	if reply["by"] != "00u-bo" || reply["prev"] != "m-1" || reply["text"] != "Checked: it renews in May." {
		t.Fatalf("the reply is %v", reply)
	}
}
