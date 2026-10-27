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
