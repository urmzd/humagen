package httpapi

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/threads"
)

func TestSessionAlertsConfinementAndLifecycle(t *testing.T) {
	f := newFixture(t)
	owner := f.machineAs(t, "ana", "acme", operator...)
	other := f.machineAs(t, "bob", "acme", operator...)
	tenant := f.machineAs(t, "ana", "other", operator...)
	id := "s_" + strings.Repeat("a", 32)
	_, err := f.p.Threads.Create("acme", threads.Thread{ID: id, Project: "", Channel: "general", Title: "Private", CreatedBy: "ana", Visibility: threads.Private, Default: "helper", Agents: []string{"helper"}, Session: &threads.SessionIdentity{Agent: "helper", Parent: "console://private"}}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/sessions/" + id + "/alerts"
	want(t, f, other, "POST", path, `{"title":"Leaked","priority":"urgent"}`, http.StatusNotFound)
	want(t, f, tenant, "POST", path, `{"title":"Leaked","priority":"urgent"}`, http.StatusNotFound)
	want(t, f, owner, "POST", path, `{"title":"Private invitation","priority":"normal","who":["bob"]}`, http.StatusBadRequest)
	want(t, f, owner, "POST", path, `{"title":"Unknown recipient","priority":"normal","who":["missing"]}`, http.StatusBadRequest)
	a := want(t, f, owner, "POST", path, `{"title":"Need a decision","priority":"normal"}`, http.StatusCreated)
	aid := a["id"].(string)
	for _, item := range []struct {
		source string
		want   int
	}{{"session", 1}, {"connection", 0}} {
		listed := want(t, f, owner, "GET", "/api/alerts?source="+item.source, "", http.StatusOK)
		if len(listed["alerts"].([]any)) != item.want {
			t.Fatalf("source %s: %+v", item.source, listed)
		}
	}

	if a["session"] != id {
		t.Fatal("alert lost its session")
	}
	want(t, f, other, "PATCH", "/api/alerts/"+aid, `{"state":"resolved"}`, http.StatusNotFound)
	want(t, f, owner, "PATCH", "/api/alerts/"+aid, `{"state":"snoozed","snoozed_until":"2000-01-01T00:00:00Z"}`, http.StatusBadRequest)
	future := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	want(t, f, owner, "PATCH", "/api/alerts/"+aid, fmt.Sprintf(`{"state":"snoozed","snoozed_until":%q}`, future), http.StatusOK)
	list := want(t, f, owner, "GET", "/api/alerts?state=open&mine=true", "", http.StatusOK)
	if len(list["alerts"].([]any)) != 0 {
		t.Fatal("snoozed alert counted open")
	}
	want(t, f, owner, "PATCH", "/api/alerts/"+aid, `{"state":"resolved"}`, http.StatusOK)
	want(t, f, owner, "PATCH", "/api/alerts/"+aid, `{"state":"open"}`, http.StatusOK)
	list = want(t, f, owner, "GET", "/api/alerts?state=open&mine=true", "", http.StatusOK)
	if len(list["alerts"].([]any)) != 1 {
		t.Fatal("undo lost alert")
	}
	list = want(t, f, other, "GET", "/api/alerts?state=open", "", http.StatusOK)
	if len(list["alerts"].([]any)) != 0 {
		t.Fatal("private session alert listed for stranger")
	}
}
