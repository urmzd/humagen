package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
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

func TestCompanyRowsOfferOnlyCurrentBuiltinNames(t *testing.T) {
	f := newFixtureWith(t, platform.Config{BuiltinProvider: "offline", BuiltinModel: "offline"})
	token := f.machine(t, "acme", everything...)
	want(t, f, token, "GET", "/api/agents", "", http.StatusOK)
	row := want(t, f, token, "GET", "/api/projects/~", "", http.StatusOK)["project"].(map[string]any)
	for _, item := range row["row_agents"].([]any) {
		if strings.HasPrefix(item.(string), "warp-") {
			t.Fatalf("compatibility alias offered in Company marks: %v", row["row_agents"])
		}
	}
	if got := len(row["row_agents"].([]any)); got != 3 {
		t.Fatalf("duplicate or missing Company builtin marks: %v", row["row_agents"])
	}
	choices := want(t, f, token, "GET", "/api/projects/~/agents", "", http.StatusOK)
	raw, _ := json.Marshal(map[string]any{"homes": choices["homes"], "offers": choices["offers"], "available": choices["available"]})
	if strings.Contains(string(raw), "warp-") {
		t.Fatalf("compatibility aliases offered as current agents: %s", raw)
	}
	if home, ok := f.p.ProjectTree("acme").Home("warp-analyst"); !ok || home != "" {
		t.Fatal("current-choice filtering changed stored alias home")
	}
	found := want(t, f, token, "GET", "/api/find-help?q=runs", "", http.StatusOK)
	if len(found["suggestions"].([]any)) == 0 {
		t.Fatal("finder returned no visible builtin candidate")
	}
	raw, _ = json.Marshal(found)
	if strings.Contains(string(raw), "warp-") {
		t.Fatalf("finder offered legacy alias: %s", raw)
	}
}
