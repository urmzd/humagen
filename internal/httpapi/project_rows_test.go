package httpapi

import (
	"net/http"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/inbox"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

func TestProjectRowsCountOnlyTheReadersVisibleUnreadUpdates(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		f := newFixtureWith(t, platform.Config{Backends: platform.Backends{State: open(t)}})
		ana := f.person(t, "acme", "ana", "Ana", "humagen-operators")
		bo := f.person(t, "acme", "bo", "Bo", "humagen-operators")
		want(t, f, ana, "POST", "/api/projects", `{"name":"Field"}`, http.StatusCreated)
		want(t, f, ana, "POST", "/api/projects", `{"name":"Vault","kind":"account"}`, http.StatusCreated)
		admin := f.machineAs(t, "admin", "acme", everything...)
		want(t, f, admin, "POST", "/api/agents", helperSpec, http.StatusCreated)
		want(t, f, admin, "PUT", "/api/agents/helper/home", `{"project":"Field"}`, http.StatusOK)
		row := want(t, f, bo, "GET", "/api/projects/Field", "", http.StatusOK)["project"].(map[string]any)
		agents := row["row_agents"].([]any)
		if len(agents) != 1 || agents[0] != "helper" {
			t.Fatalf("project agents = %v", agents)
		}

		posted := want(t, f, ana, "POST", "/api/projects/Field/announcements", `{"subject":"Offsite","text":"May"}`, http.StatusCreated)
		// A private thread's alert is not counted merely because its project is visible.
		room := want(t, f, ana, "POST", "/api/threads", `{"project":"Field","visibility":"private","text":"Private plan"}`, http.StatusCreated)
		thread := room["thread"].(map[string]any)["id"].(string)
		_, _, err := f.p.Inbox.Raise("acme", "private", inbox.Message{Kind: inbox.KindAlert, Subject: "Secret", Source: inbox.Source{Project: "Field", Thread: thread}, Parts: []inbox.Part{{Type: inbox.PartAlert}}}, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		count := func(tok, name string, n float64) {
			t.Helper()
			out := want(t, f, tok, "GET", "/api/projects?limit=1&q="+name, "", http.StatusOK)
			rows := out["projects"].([]any)
			if len(rows) != 1 || rows[0].(map[string]any)["unread"] != n {
				t.Fatalf("%s unread = %v, want %v", name, rows, n)
			}
		}
		count(bo, "Field", 1)
		count(ana, "Field", 1) // The author has read their announcement, but owns the private alert.
		want(t, f, bo, "POST", "/api/inbox/"+posted["id"].(string)+"/read", `{"read":true}`, http.StatusOK)
		count(bo, "Field", 0)
		count(ana, "Field", 1)
		out := want(t, f, bo, "GET", "/api/projects", "", http.StatusOK)
		for _, row := range out["projects"].([]any) {
			if row.(map[string]any)["name"] == "Vault" {
				t.Fatal("hidden project was listed")
			}
		}
	})
}
