package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
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
	if msg["by"] != "ana" || msg["name"] != "Ana" || msg["via"] != "console" {
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
	if reply["by"] != "bo" || reply["prev"] != "m-1" || reply["text"] != "Checked: it renews in May." {
		t.Fatalf("the reply is %v", reply)
	}
}

// A subject can hold an address, as a Tailscale login does. A thread never
// shows one: whoever posts and whoever is tagged is named by their handle,
// and called by their name or else their handle, in what posting answers,
// in the thread, in the list of threads and in the mention the tag raises.
func TestAThreadNeverShowsASubjectThatHoldsAnAddress(t *testing.T) {
	f := newFixture(t)
	pat := f.person(t, "acme", "tailscale:pat@example.com", "", "warp-admins")
	ada := f.person(t, "acme", "tailscale:ada@example.com", "Ada", "warp-admins")
	res, posted := f.do(t, nil, "POST", "/api/threads", pat, `{"visibility":"shared","text":"@ada can you check the renewal?"}`)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("tagging ada = %d %v", res.StatusCode, posted)
	}
	id := posted["thread"].(map[string]any)["id"].(string)
	if res, out := f.do(t, nil, "PUT", "/api/threads/"+id+"/participants", pat, `{"version":1,"add":[{"kind":"person","name":"ada","at":"2026-01-01T00:00:00Z"}]}`); res.StatusCode != http.StatusOK {
		t.Fatalf("adding ada by handle = %d %v", res.StatusCode, out)
	}
	_, page := f.do(t, nil, "GET", "/api/threads/"+id, ada, "")
	_, list := f.do(t, nil, "GET", "/api/threads", ada, "")
	_, mentions := f.do(t, nil, "GET", "/api/inbox?kind=mention", ada, "")
	// The inbox says to whom a message is, which only that reader is sent.
	for what, out := range map[string]map[string]any{"posting": posted, "the thread": page, "the list": list, "the mention": mentions} {
		raw, err := json.Marshal(out)
		if err != nil {
			t.Fatal(err)
		}
		if shown := strings.ReplaceAll(string(raw), `"to":"tailscale:ada@example.com"`, ""); strings.Contains(shown, "example.com") {
			t.Errorf("%s shows an address: %s", what, raw)
		}
	}
	msg := posted["message"].(map[string]any)
	if msg["by"] != "pat" || msg["name"] != "pat" {
		t.Errorf("the message is by %v, called %v", msg["by"], msg["name"])
	}
	if notified, _ := posted["notified"].([]any); len(notified) != 1 || notified[0] != "ada" {
		t.Errorf("notified %v", posted["notified"])
	}
	th := page["thread"].(map[string]any)
	if th["created_by"] != "pat" {
		t.Errorf("the thread was started by %v", th["created_by"])
	}
	var people []string
	for _, p := range th["participants"].([]any) {
		if who := p.(map[string]any); who["kind"] == "person" {
			people = append(people, who["name"].(string))
		}
	}
	if strings.Join(people, ",") != "pat,ada" {
		t.Errorf("the people in it are %v", people)
	}
	if got := mentions["messages"].([]any); len(got) != 1 || !strings.HasPrefix(got[0].(map[string]any)["subject"].(string), "pat mentioned you") {
		t.Errorf("ada's mentions are %v", mentions)
	}
}

// Which of a Slack client's channels a project answers in is the client's
// admins' to decide: an owner of a project without clients:write cannot bind
// a Slack channel to it, and so cannot capture what is said there, but keeps
// or removes a binding an admin made.
func TestBindingASlackChannelTakesTheClientsAdmin(t *testing.T) {
	f := newFixture(t)
	admin := f.machine(t, "acme", everything...)
	opal := f.machineAs(t, "opal", "acme", operator...)
	want(t, f, admin, "POST", "/api/agents", helperSpec, http.StatusCreated)
	want(t, f, admin, "POST", "/api/clients", slackApp("app", "A01APP", "svc-app"), http.StatusCreated)
	want(t, f, opal, "POST", "/api/projects", `{"name":"Mine"}`, http.StatusCreated)
	for _, tc := range []struct{ method, path, body string }{
		{"POST", "/api/projects/Mine/channels", `{"name":"grab","slack":{"client":"app","channel":"C0FINANCE"}}`},
		{"PUT", "/api/projects/Mine/channels/general", `{"version":0,"slack":{"client":"app","channel":"C0FINANCE"}}`},
	} {
		if got, out := status(t, f, opal, tc.method, tc.path, tc.body); got != http.StatusForbidden || !strings.Contains(fmt.Sprint(out["error"]), "clients:write") {
			t.Errorf("an owner binding %s %s = %d %v, want 403", tc.method, tc.path, got, out)
		}
	}
	if b := want(t, f, admin, "GET", "/api/clients/app/bindings", "", http.StatusOK)["bindings"].([]any); len(b) != 0 {
		t.Fatalf("a refused binding was kept: %v", b)
	}
	// An admin binds it; the owner keeps it as it is, then removes it.
	want(t, f, admin, "PUT", "/api/projects/Mine/channels/general", `{"version":0,"slack":{"client":"app","channel":"C0MINE"}}`, http.StatusOK)
	want(t, f, opal, "PUT", "/api/projects/Mine/channels/general", `{"version":1,"title":"General","slack":{"client":"app","channel":"C0MINE"}}`, http.StatusOK)
	if got, out := status(t, f, opal, "PUT", "/api/projects/Mine/channels/general", `{"version":2,"slack":{"client":"app","channel":"C0FINANCE"}}`); got != http.StatusForbidden {
		t.Fatalf("an owner moving a binding = %d %v", got, out)
	}
	want(t, f, opal, "PUT", "/api/projects/Mine/channels/general", `{"version":2,"title":"General"}`, http.StatusOK)
}
