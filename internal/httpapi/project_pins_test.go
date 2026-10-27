package httpapi

import (
	"fmt"
	"net/http"
	"reflect"
	"testing"
)

func TestProjectPinsKeepConversationPinsAndCheckReadAccess(t *testing.T) {
	f := newFixture(t)
	owner := f.person(t, "acme", "ana", "Ana", "humagen-admins")
	reader := f.person(t, "acme", "bo", "Bo", "humagen-operators")
	want(t, f, owner, "POST", "/api/projects", `{"name":"Launch"}`, http.StatusCreated)
	want(t, f, owner, "POST", "/api/projects", `{"name":"Secret","parent":"Launch"}`, http.StatusCreated)
	want(t, f, owner, "PUT", "/api/projects/Secret/members", `{"version":1,"members":[{"who":"ana","role":"owner"}]}`, http.StatusOK)
	want(t, f, reader, "PUT", "/api/preferences", `{"theme":"dark","accent":"teal","pinned":["t1"],"pinned_projects":["Launch","~","Launch"]}`, http.StatusOK)
	snapshot := want(t, f, reader, "GET", "/api/preferences", "", http.StatusOK)
	if !reflect.DeepEqual(snapshot["pinned_projects"], []any{"Launch", "~"}) || !reflect.DeepEqual(snapshot["pinned"], []any{"t1"}) {
		t.Fatalf("saved pins = %v", snapshot)
	}
	for _, name := range []string{"Secret", "Missing"} {
		want(t, f, reader, "PUT", "/api/preferences", fmt.Sprintf(`{"display_name":"Must not save","theme":"light","accent":"blue","pinned_projects":[%q]}`, name), http.StatusNotFound)
	}
	if got := want(t, f, reader, "GET", "/api/preferences", "", http.StatusOK); !reflect.DeepEqual(got, snapshot) {
		t.Fatalf("a refused pin changed preferences: %v", got)
	}
	want(t, f, owner, "POST", "/api/projects", `{"name":"Another"}`, http.StatusCreated)
	want(t, f, owner, "PUT", "/api/projects/Launch/members", `{"version":1,"members":[{"who":"ana","role":"owner"}]}`, http.StatusOK)
	want(t, f, reader, "PUT", "/api/preferences", `{"theme":"dark","accent":"teal","pinned_projects":["Another","Launch","~"]}`, http.StatusOK)
	want(t, f, reader, "PUT", "/api/preferences", `{"theme":"dark","accent":"teal","pinned_projects":[]}`, http.StatusOK)
	got := want(t, f, reader, "GET", "/api/preferences", "", http.StatusOK)
	if !reflect.DeepEqual(got["pinned_projects"], []any{}) || !reflect.DeepEqual(got["pinned"], []any{"t1"}) {
		t.Fatalf("unpin project changed conversations: %v", got)
	}
	if got := want(t, f, owner, "GET", "/api/preferences", "", http.StatusOK); !reflect.DeepEqual(got["pinned_projects"], []any{}) {
		t.Fatalf("another person sees pins: %v", got)
	}
}

func TestNewProjectPinsRequireProjectReadScopeWithoutRevealingNames(t *testing.T) {
	f := newFixture(t)
	owner := f.person(t, "acme", "ana", "Ana", "humagen-admins")
	runsOnly := f.machineAs(t, "scoped-reader", "acme", "humagen:runs:read")
	projectReader := f.machineAs(t, "scoped-reader", "acme", "humagen:runs:read", "humagen:projects:read")
	want(t, f, owner, "POST", "/api/projects", `{"name":"Launch"}`, http.StatusCreated)
	want(t, f, owner, "POST", "/api/projects", `{"name":"Secret"}`, http.StatusCreated)
	want(t, f, owner, "PUT", "/api/projects/Secret/members", `{"version":1,"members":[{"who":"ana","role":"owner"}]}`, http.StatusOK)
	want(t, f, runsOnly, "PUT", "/api/preferences", `{"display_name":"Original","theme":"dark","accent":"teal","pinned":["conversation"]}`, http.StatusOK)
	before := want(t, f, runsOnly, "GET", "/api/preferences", "", http.StatusOK)
	var denied map[string]any
	for _, name := range []string{"Launch", "Secret", "Missing", "~"} {
		body := fmt.Sprintf(`{"display_name":"Must not save","theme":"light","accent":"blue","pinned_projects":[%q]}`, name)
		got := want(t, f, runsOnly, "PUT", "/api/preferences", body, http.StatusForbidden)
		if denied == nil {
			denied = got
		} else if !reflect.DeepEqual(got, denied) {
			t.Fatalf("project existence changed a missing-scope response: %v != %v", got, denied)
		}
	}
	if got := want(t, f, runsOnly, "GET", "/api/preferences", "", http.StatusOK); !reflect.DeepEqual(got, before) {
		t.Fatalf("a missing project-read scope changed preferences: %v", got)
	}

	// Scope permits the check, but never replaces current project access.
	want(t, f, projectReader, "PUT", "/api/preferences", `{"theme":"dark","accent":"teal","pinned_projects":["Secret"]}`, http.StatusNotFound)
	want(t, f, projectReader, "PUT", "/api/preferences", `{"theme":"dark","accent":"teal","pinned_projects":["Launch","~"]}`, http.StatusOK)
	want(t, f, owner, "PUT", "/api/projects/Launch/members", `{"version":1,"members":[{"who":"ana","role":"owner"}]}`, http.StatusOK)

	// Losing scope and membership does not require looking up an existing pin
	// merely to retain it or remove a different saved shortcut.
	want(t, f, runsOnly, "PUT", "/api/preferences", `{"theme":"dark","accent":"teal","pinned_projects":["Launch","~"]}`, http.StatusOK)
	want(t, f, runsOnly, "PUT", "/api/preferences", `{"theme":"dark","accent":"teal","pinned_projects":["Launch"]}`, http.StatusOK)
	retained := want(t, f, runsOnly, "GET", "/api/preferences", "", http.StatusOK)
	if !reflect.DeepEqual(retained["pinned_projects"], []any{"Launch"}) || !reflect.DeepEqual(retained["pinned"], []any{"conversation"}) {
		t.Fatalf("retaining a hidden project changed unrelated pins: %v", retained)
	}
	want(t, f, runsOnly, "PUT", "/api/preferences", `{"theme":"dark","accent":"teal","pinned_projects":[]}`, http.StatusOK)
	if got := want(t, f, runsOnly, "GET", "/api/preferences", "", http.StatusOK); !reflect.DeepEqual(got["pinned_projects"], []any{}) {
		t.Fatalf("a hidden project could not be unpinned without scope: %v", got)
	}
}
