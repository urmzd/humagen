package httpapi

import (
	"fmt"
	"net/http"
	"reflect"
	"testing"
)

// A person's marks are their own: archiving a conversation for themselves
// leaves it for everyone else, a private conversation they cannot read is
// not found, and nobody reads another person's marks.
func TestMarksArePerPersonAndConfined(t *testing.T) {
	f := newFixture(t)
	dana := f.person(t, "acme", "00u-dana", "Dana", "humagen-operators")
	eve := f.person(t, "acme", "00u-eve", "Eve", "humagen-operators")
	started := want(t, f, dana, "POST", "/api/threads", `{"visibility":"private","text":"Mine.","post_only":true}`, http.StatusCreated)
	id := fmt.Sprint(started["thread"].(map[string]any)["id"])

	got := want(t, f, dana, "PATCH", "/api/threads/"+id+"/marks", `{"archived":true,"read":true}`, http.StatusOK)
	if !reflect.DeepEqual(got["archived"], []any{id}) {
		t.Fatalf("archived = %v", got["archived"])
	}
	if _, read := got["read"].(map[string]any)[id]; !read {
		t.Fatalf("read = %v", got["read"])
	}
	// Archiving for oneself does not archive the conversation.
	want(t, f, dana, "GET", "/api/threads/"+id, "", http.StatusOK)

	// Eve cannot read Dana's private conversation, so she cannot mark it.
	want(t, f, eve, "PATCH", "/api/threads/"+id+"/marks", `{"archived":true}`, http.StatusNotFound)
	if mine := want(t, f, eve, "GET", "/api/marks", "", http.StatusOK); len(mine["archived"].([]any)) != 0 {
		t.Fatalf("eve sees %v", mine)
	}

	got = want(t, f, dana, "PATCH", "/api/threads/"+id+"/marks", `{"archived":false}`, http.StatusOK)
	if len(got["archived"].([]any)) != 0 {
		t.Fatalf("unarchived = %v", got["archived"])
	}
	want(t, f, dana, "PATCH", "/api/threads/"+id+"/marks", `{"bogus":1}`, http.StatusBadRequest)

	want(t, f, dana, "POST", "/api/marks/tips", `{"name":"search"}`, http.StatusOK)
	want(t, f, dana, "POST", "/api/marks/tips", `{"name":"search"}`, http.StatusOK)
	if got := want(t, f, dana, "GET", "/api/marks", "", http.StatusOK); !reflect.DeepEqual(got["tips"], []any{"search"}) {
		t.Fatalf("tips = %v", got["tips"])
	}
	want(t, f, dana, "POST", "/api/marks/tips", `{"name":"a b"}`, http.StatusBadRequest)
}

// Marks take runs:read, so a credential without it is refused.
func TestMarksTakeRunsRead(t *testing.T) {
	f := newFixture(t)
	none := f.machineAs(t, "nothing", "acme", "humagen:projects:read")
	want(t, f, none, "GET", "/api/marks", "", http.StatusForbidden)
	want(t, f, none, "POST", "/api/marks/tips", `{"name":"search"}`, http.StatusForbidden)
}
