package httpapi

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// A project's owners name its knowledge sources, and every run filed there
// and below is grounded in them. A reader who may not read knowledge is shown
// no source's name, on the knowledge route, in the project's record or in
// its history, and may not name one.
func TestAProjectsKnowledgeIsNamedByOwnersAndShownToWhoReadsKnowledge(t *testing.T) {
	f := newFixture(t)
	admin := f.machineAs(t, "svc-admin", "acme", everything...)
	want(t, f, admin, "POST", "/api/knowledge", `{"name":"playbook"}`, http.StatusCreated)
	want(t, f, admin, "POST", "/api/knowledge", `{"name":"globex-notes"}`, http.StatusCreated)
	want(t, f, admin, "POST", "/api/projects", `{"name":"Field"}`, http.StatusCreated)
	want(t, f, admin, "POST", "/api/projects", `{"name":"Globex","parent":"Field","members":[{"who":"*","role":"member"},{"who":"svc-admin","role":"owner"}]}`, http.StatusCreated)
	want(t, f, admin, "POST", "/api/projects", `{"name":"Initech","parent":"Field"}`, http.StatusCreated)

	field := want(t, f, admin, "GET", "/api/projects/Field/knowledge", "", http.StatusOK)
	want(t, f, admin, "PUT", "/api/projects/Field/knowledge", fmt.Sprintf(`{"version":%v,"knowledge":["playbook"]}`, field["version"]), http.StatusOK)
	globex := want(t, f, admin, "GET", "/api/projects/Globex/knowledge", "", http.StatusOK)
	got := want(t, f, admin, "PUT", "/api/projects/Globex/knowledge", fmt.Sprintf(`{"version":%v,"knowledge":["globex-notes"]}`, globex["version"]), http.StatusOK)
	if fmt.Sprint(got["given"]) != "[map[from:Globex name:globex-notes] map[from:Field name:playbook]]" {
		t.Fatalf("Globex's knowledge = %v", got)
	}
	if initech := want(t, f, admin, "GET", "/api/projects/Initech/knowledge", "", http.StatusOK); fmt.Sprint(initech["given"]) != "[map[from:Field name:playbook]]" {
		t.Fatalf("Initech is given %v; a sibling's sources are not its", initech["given"])
	}
	if view := want(t, f, admin, "GET", "/api/projects/Globex", "", http.StatusOK); !strings.Contains(fmt.Sprint(view["project"]), "knowledge:[globex-notes]") {
		t.Fatalf("Globex's record = %v", view["project"])
	}

	// A member is not an owner.
	member := f.person(t, "acme", "00u-bo", "Bo", "warp-operators")
	if code, out := status(t, f, member, "PUT", "/api/projects/Globex/knowledge", fmt.Sprintf(`{"version":%v,"knowledge":[]}`, got["version"])); code != http.StatusForbidden || out["error"] != "you need to own Globex" {
		t.Fatalf("a member naming Globex's sources = %d %v", code, out)
	}

	// Someone who owns every project and may not read knowledge.
	blind := f.machineAs(t, "svc-blind", "acme", "warp:projects:read", "warp:projects:write", "warp:projects:govern")
	shown := want(t, f, blind, "GET", "/api/projects/Globex/knowledge", "", http.StatusOK)
	if fmt.Sprint(shown["knowledge"]) != "[]" || fmt.Sprint(shown["given"]) != "[]" {
		t.Fatalf("knowledge shown to a reader without knowledge:read = %v", shown)
	}
	for _, path := range []string{"/api/projects/Globex", "/api/projects?within=Field", "/api/projects/Globex/history"} {
		if _, body := get(t, f, admin, path); !strings.Contains(body, "globex-notes") {
			t.Errorf("%s does not show the owner Globex's sources: %s", path, body)
		}
		if code, body := get(t, f, blind, path); code != http.StatusOK || strings.Contains(body, "playbook") || strings.Contains(body, "globex-notes") {
			t.Errorf("%s shows a reader without knowledge:read a source: %d %s", path, code, body)
		}
	}
	if code, out := status(t, f, blind, "PUT", "/api/projects/Globex/knowledge", fmt.Sprintf(`{"version":%v,"knowledge":[]}`, got["version"])); code != http.StatusForbidden || !strings.Contains(fmt.Sprint(out["error"]), "knowledge:read") {
		t.Fatalf("naming sources without knowledge:read = %d %v", code, out)
	}
}
