package httpapi

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
)

func viewActions(v map[string]any) []string {
	var out []string
	for _, a := range v["actions"].([]any) {
		out = append(out, a.(string))
	}
	return out
}

// A role only narrows what the route's permission allows: an owner who holds
// only projects:read still cannot write, and a viewer of a project who holds
// projects:write cannot work in it.
func TestAProjectRoleOnlyNarrowsATenantPermission(t *testing.T) {
	f := newFixture(t)
	admin := f.machineAs(t, "svc-admin", "acme", everything...)
	ana := f.machineAs(t, "ana", "acme", operator...)
	readOnly := f.machineAs(t, "ana", "acme", "warp:projects:read")
	bo := f.machineAs(t, "bo", "acme", operator...)
	want(t, f, admin, "POST", "/api/projects", `{"name":"Field","members":[{"who":"*","role":"viewer"},{"who":"ana","role":"owner"}]}`, http.StatusCreated)

	// Ana owns Field, but with projects:read alone she reads it and nothing more.
	field := want(t, f, readOnly, "GET", "/api/projects/Field", "", http.StatusOK)["project"].(map[string]any)
	if a := field["access"].(map[string]any); a["role"] != "owner" || a["via"] != "ana" || len(viewActions(field)) != 0 {
		t.Fatalf("an owner with projects:read alone = %v, actions %v", a, viewActions(field))
	}
	if out := want(t, f, readOnly, "PUT", "/api/projects/Field", `{"version":1,"title":"Mine"}`, http.StatusForbidden); out["error"] != "you need projects:write" {
		t.Fatalf("an owner without projects:write = %v", out)
	}
	// Bo holds projects:write and is only a viewer of Field.
	if got := viewActions(want(t, f, bo, "GET", "/api/projects/Field", "", http.StatusOK)["project"].(map[string]any)); len(got) != 0 {
		t.Fatalf("a viewer may %v", got)
	}
	for _, tc := range []struct{ method, path, body, says string }{
		{"POST", "/api/projects", `{"name":"Sub","parent":"Field"}`, "you need to be a member of Field"},
		{"POST", "/api/projects/Field/memory", `{"text":"x"}`, "you need to be a member of Field"},
		{"PUT", "/api/projects/Field", `{"version":1,"title":"Bo's"}`, "you need to own Field"},
		{"PUT", "/api/projects/Field/members", `{"version":1,"members":[{"who":"*","role":"viewer"}]}`, "you need to own Field"},
		{"DELETE", "/api/projects/Field", "", "you need to own Field"},
	} {
		if got, out := status(t, f, bo, tc.method, tc.path, tc.body); got != http.StatusForbidden || out["error"] != tc.says {
			t.Errorf("a viewer %s %s = %d %v, want 403 %s", tc.method, tc.path, got, out, tc.says)
		}
	}
	// Ana, with projects:write, does what she owns.
	if got := viewActions(want(t, f, ana, "GET", "/api/projects/Field", "", http.StatusOK)["project"].(map[string]any)); !slices.Contains(got, "edit") || !slices.Contains(got, "create") {
		t.Fatalf("an owner may %v", got)
	}
	want(t, f, ana, "PUT", "/api/projects/Field", `{"version":1,"title":"Field engineering"}`, http.StatusOK)
	sub := want(t, f, ana, "POST", "/api/projects", `{"name":"Globex","parent":"Field","kind":"account"}`, http.StatusCreated)
	// Ownership flows down, and a new project is no more open than its parent.
	if a := sub["access"].(map[string]any); a["role"] != "owner" {
		t.Fatalf("Ana in what she made under Field = %v", a)
	}
	ms := want(t, f, bo, "GET", "/api/projects/Globex/members", "", http.StatusOK)
	if fmt.Sprint(ms["members"]) != "[map[role:viewer who:*] map[role:owner who:ana]]" || fmt.Sprint(ms["inherited"]) != "[map[from:Field who:ana]]" {
		t.Fatalf("Globex's members = %v", ms)
	}
}

// Until reads are confined to members, every project keeps an entry for
// everyone, and a child is never more open than its parent.
func TestEveryProjectStaysVisible(t *testing.T) {
	f := newFixture(t)
	admin := f.machineAs(t, "svc-admin", "acme", everything...)
	want(t, f, admin, "POST", "/api/projects", `{"name":"Field"}`, http.StatusCreated)
	want(t, f, admin, "POST", "/api/projects", `{"name":"Globex","parent":"Field"}`, http.StatusCreated)
	out := want(t, f, admin, "PUT", "/api/projects/Globex/members", `{"version":1,"members":[{"who":"svc-admin","role":"owner"}]}`, http.StatusConflict)
	if !strings.HasPrefix(out["error"].(string), "members-only projects need read confinement, not built") {
		t.Fatalf("a members-only project = %v", out)
	}
	want(t, f, admin, "PUT", "/api/projects/Field/members", `{"version":1,"members":[{"who":"*","role":"viewer"}]}`, http.StatusBadRequest)
	want(t, f, admin, "PUT", "/api/projects/Globex/members", `{"version":1,"members":[{"who":"*","role":"viewer"}]}`, http.StatusOK)
	want(t, f, admin, "PUT", "/api/projects/Globex/members", `{"version":2,"members":[{"who":"role:x","role":"member"},{"who":"*","role":"member"},{"who":"*","role":"viewer"}]}`, http.StatusBadRequest)
}

// A member's note waits for an owner: it reaches the project's owners in
// their inbox, and only an owner, with projects:write, keeps it.
func TestMembersProposeNotesAndOwnersKeepThem(t *testing.T) {
	f := newFixture(t)
	ana := f.machineAs(t, "ana", "acme", everything...)
	bo := f.machineAs(t, "bo", "acme", operator...)
	want(t, f, ana, "POST", "/api/projects", `{"name":"Field"}`, http.StatusCreated)
	note := want(t, f, bo, "POST", "/api/projects/Field/memory", `{"text":"Renewals close in March."}`, http.StatusCreated)
	if note["status"] != "pending" {
		t.Fatalf("a member's note = %v", note)
	}
	if got := want(t, f, bo, "GET", "/api/inbox?kind=memory", "", http.StatusOK)["messages"].([]any); len(got) != 0 {
		t.Fatalf("the member who proposed it is asked: %v", got)
	}
	msgs := want(t, f, ana, "GET", "/api/inbox?kind=memory&project=Field", "", http.StatusOK)["messages"].([]any)
	if len(msgs) != 1 {
		t.Fatalf("the owner's inbox holds %v", msgs)
	}
	m := msgs[0].(map[string]any)
	part := m["parts"].([]any)[0].(map[string]any)
	if fmt.Sprint(part["actions"]) != "[keep discard]" || m["from"].(map[string]any)["name"] != "bo" {
		t.Fatalf("the proposal = %v", m)
	}
	resolve := fmt.Sprintf("/api/inbox/%s/parts/%s/resolve", m["id"], part["id"])
	body := fmt.Sprintf(`{"action":"keep","ids":[%q]}`, note["id"])
	want(t, f, bo, "POST", resolve, body, http.StatusNotFound)
	want(t, f, ana, "POST", resolve, body, http.StatusOK)
	notes := want(t, f, bo, "GET", "/api/projects/Field/memory", "", http.StatusOK)["notes"].([]any)
	if len(notes) != 1 || notes[0].(map[string]any)["status"] != "approved" {
		t.Fatalf("after the owner kept it = %v", notes)
	}
}

// A move needs ownership of what moves and membership where it goes, and
// never escapes a limit: leaving a project that holds one needs
// billing:write.
func TestAMoveNeverEscapesALimit(t *testing.T) {
	f := newFixture(t)
	admin := f.machineAs(t, "svc-admin", "acme", everything...)
	ana := f.machineAs(t, "ana", "acme", operator...)
	want(t, f, admin, "POST", "/api/projects", `{"name":"Field"}`, http.StatusCreated)
	want(t, f, admin, "POST", "/api/projects", `{"name":"Launch"}`, http.StatusCreated)
	want(t, f, admin, "PUT", "/api/projects/Launch/members", `{"version":1,"members":[{"who":"*","role":"viewer"}]}`, http.StatusOK)
	want(t, f, ana, "POST", "/api/projects", `{"name":"Globex","parent":"Field"}`, http.StatusCreated)
	want(t, f, admin, "PUT", "/api/policies", `{"scope":{"kind":"project","name":"Field"},"period":"day","max_runs":7}`, http.StatusOK)

	if out := want(t, f, ana, "POST", "/api/projects/Globex/move", `{"version":1,"parent":"Launch"}`, http.StatusForbidden); out["error"] != "you need to be a member of Launch" {
		t.Fatalf("a move where Ana is a viewer = %v", out)
	}
	if out := want(t, f, ana, "POST", "/api/projects/Globex/move", `{"version":1,"parent":"~"}`, http.StatusForbidden); !strings.Contains(out["error"].(string), "billing:write") {
		t.Fatalf("a move out from under a limit = %v", out)
	}
	moved := want(t, f, admin, "POST", "/api/projects/Globex/move", `{"version":1,"parent":"~"}`, http.StatusOK)
	if moved["parent"] != nil || moved["depth"].(float64) != 1 {
		t.Fatalf("moved = %v", moved)
	}
	changes := want(t, f, ana, "GET", "/api/projects/Globex/history", "", http.StatusOK)["changes"].([]any)
	if latest := changes[0].(map[string]any); latest["what"] != "moved" || latest["by"] != "svc-admin" {
		t.Fatalf("the newest change = %v", latest)
	}
}

// An owner of a project above sets a tighten-only limit; owning the project
// itself is not enough, since a limit holds what its owner does.
func TestAnOwnerLimitsOnlyWhatIsBelowWhatTheyOwn(t *testing.T) {
	f := newFixture(t)
	admin := f.machineAs(t, "svc-admin", "acme", everything...)
	ana := f.machineAs(t, "ana", "acme", operator...)
	bo := f.machineAs(t, "bo", "acme", operator...)
	want(t, f, ana, "POST", "/api/projects", `{"name":"Field"}`, http.StatusCreated)
	want(t, f, ana, "POST", "/api/projects", `{"name":"Globex","parent":"Field"}`, http.StatusCreated)
	want(t, f, ana, "PUT", "/api/projects/Globex/members", `{"version":1,"members":[{"who":"*","role":"member"},{"who":"bo","role":"owner"}]}`, http.StatusOK)

	if out := want(t, f, bo, "PUT", "/api/projects/Globex/limits", `{"period":"day","max_runs":3}`, http.StatusForbidden); out["error"] != "you need to own Field to limit Globex" {
		t.Fatalf("an owner of Globex alone = %v", out)
	}
	globex := want(t, f, ana, "GET", "/api/projects/Globex", "", http.StatusOK)["project"].(map[string]any)
	if !slices.Contains(viewActions(globex), "limits") {
		t.Fatalf("an owner of Field may %v on Globex", viewActions(globex))
	}
	if saved := want(t, f, ana, "PUT", "/api/projects/Globex/limits", `{"period":"day","max_runs":3}`, http.StatusOK); saved["authority"] != "owner" || saved["updated_by"] != "ana" {
		t.Fatalf("an owner's limit = %v", saved)
	}
	want(t, f, ana, "PUT", "/api/projects/Field/limits", `{"period":"day","max_runs":3}`, http.StatusForbidden)
	want(t, f, admin, "PUT", "/api/projects/Field/limits", `{"period":"day","max_runs":2}`, http.StatusOK)
	if out := want(t, f, ana, "PUT", "/api/projects/Globex/limits", `{"period":"day","max_runs":3}`, http.StatusBadRequest); !strings.Contains(out["error"].(string), "project Field") {
		t.Fatalf("a limit looser than Field's = %v", out)
	}
}

// What a project turns on, where an agent lives, and a project agent itself
// are a project's to change: each needs ownership of the project, and a move
// of home needs ownership of the project it leaves and the one it comes to.
func TestOwnersDecideAProjectsAgentsAndWhereAnAgentLives(t *testing.T) {
	f := newFixture(t)
	writes := append([]string{"warp:agents:write"}, operator...)
	admin := f.machineAs(t, "svc-admin", "acme", everything...)
	ana := f.machineAs(t, "ana", "acme", writes...)
	bo := f.machineAs(t, "bo", "acme", writes...)
	want(t, f, admin, "POST", "/api/agents", `{"name":"fieldbot","model":"offline","provider":"offline","system_prompt":"Field work."}`, http.StatusCreated)
	want(t, f, admin, "POST", "/api/projects", `{"name":"Field","members":[{"who":"*","role":"member"},{"who":"ana","role":"owner"}]}`, http.StatusCreated)
	want(t, f, admin, "POST", "/api/projects", `{"name":"Launch"}`, http.StatusCreated)

	if out := want(t, f, bo, "PUT", "/api/projects/Field/agents", `{"version":1,"enable":["fieldbot"]}`, http.StatusForbidden); out["error"] != "you need to own Field" {
		t.Fatalf("a member changing Field's agents = %v", out)
	}
	want(t, f, bo, "PUT", "/api/agents/fieldbot/home", `{"project":"Field"}`, http.StatusForbidden)
	want(t, f, ana, "PUT", "/api/agents/fieldbot/home", `{"project":"Field"}`, http.StatusOK)
	// Ana does not own Launch, so the agent cannot go there; nor can it leave Field for someone who does not own Field.
	if out := want(t, f, ana, "PUT", "/api/agents/fieldbot/home", `{"project":"Launch"}`, http.StatusForbidden); out["error"] != "you need to own Launch" {
		t.Fatalf("moving it to a project Ana does not own = %v", out)
	}
	want(t, f, admin, "PUT", "/api/projects/Launch/members", `{"version":1,"members":[{"who":"*","role":"member"},{"who":"bo","role":"owner"}]}`, http.StatusOK)
	if out := want(t, f, bo, "PUT", "/api/agents/fieldbot/home", `{"project":"Launch"}`, http.StatusForbidden); out["error"] != "you need to own Field" {
		t.Fatalf("taking it from a project Bo does not own = %v", out)
	}
	// Editing a project agent is its home's to do.
	revise := `{"spec":{"name":"fieldbot","model":"offline","provider":"offline","system_prompt":"Field work, carefully."},"expected_rev":1}`
	if out := want(t, f, bo, "PUT", "/api/agents/fieldbot", revise, http.StatusForbidden); out["error"] != "you need to own Field" {
		t.Fatalf("editing an agent that lives in Field, without owning Field = %v", out)
	}
	want(t, f, ana, "PUT", "/api/agents/fieldbot", revise, http.StatusOK)
}
