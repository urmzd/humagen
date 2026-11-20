package httpapi

import (
	"net/http"
	"testing"

	"github.com/urmzd/humagen/internal/platform"
)

// The record of the Slack app a server starts is an administrator's to
// route: through the API they choose its agents, held to agents that exist,
// and what the file owns stays the file's. It is not theirs to delete.
func TestAnAdministratorRoutesTheSlackAppsRecordAndNothingElse(t *testing.T) {
	f := newFixtureGranting(t, platform.Config{}, "")
	admin := f.machine(t, "acme", everything...)
	want(t, f, admin, "POST", "/api/agents", helperSpec, http.StatusCreated)
	app := platform.SlackApp{Team: "T01ACME", Machine: "slack-app:T01ACME", People: map[string]string{"U01ANA": "ana"}}
	if _, err := f.p.ApplySlackApp(t.Context(), "acme", "file", app); err != nil {
		t.Fatal(err)
	}
	const path = "/api/clients/slack-app-t01acme"
	read := want(t, f, admin, "GET", path, "", http.StatusOK)
	if read["managed"] != "app" || obj(read["slack"])["machine"] != "slack-app:T01ACME" {
		t.Fatalf("the record reads %v", read)
	}
	want(t, f, admin, "PUT", path, `{"name":"Slack app","surface":"slack","identity":{"team":"T01ACME"},"agents":["ghost"]}`, http.StatusBadRequest)
	out := want(t, f, admin, "PUT", path, `{"name":"Slack app","surface":"slack","identity":{"team":"T09EVIL"},"default_agent":"helper","slack":{"machine":"svc-evil","people":{"U01EVE":"ana"}}}`, http.StatusOK)
	people := obj(obj(out["slack"])["people"])
	if out["default_agent"] != "helper" || obj(out["identity"])["team"] != "T01ACME" || obj(out["slack"])["machine"] != "slack-app:T01ACME" || len(people) != 1 || people["U01ANA"] != "ana" {
		t.Fatalf("after an administrator's change the record reads %v", out)
	}
	want(t, f, admin, "PUT", path, `{"name":"Slack app","surface":"slack","identity":{"team":"T01ACME"},"credentials":{"token":{"value":"xoxb-1"}}}`, http.StatusConflict)
	want(t, f, admin, "DELETE", path, "", http.StatusConflict)
}
