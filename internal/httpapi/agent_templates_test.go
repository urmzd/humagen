package httpapi

import (
	"net/http"
	"strings"
	"testing"

	"github.com/urmzd/humagen/internal/platform"
)

// The templates are read by anyone who may read agents, blank first, each
// an outcome with its starters; an agent made from one is an ordinary agent.
func TestAgentTemplatesAreListed(t *testing.T) {
	f := newFixture(t)
	viewer := f.machineAs(t, "viewer", "acme", "humagen:agents:read")
	got := want(t, f, viewer, "GET", "/api/agent-templates", "", http.StatusOK)
	templates := got["templates"].([]any)
	if len(templates) < 2 {
		t.Fatalf("templates = %v", templates)
	}
	first := templates[0].(map[string]any)
	if first["id"] != "blank" || first["title"] != "Start from scratch" {
		t.Fatalf("first template = %v, want blank", first)
	}
	for _, raw := range templates {
		tpl := raw.(map[string]any)
		for _, field := range []string{"id", "title", "outcome", "description", "greeting", "shape", "system_prompt"} {
			if s, _ := tpl[field].(string); s == "" {
				t.Errorf("template %v has no %s", tpl["id"], field)
			}
		}
		if _, ok := tpl["starters"].([]any); !ok {
			t.Errorf("template %v has no starters list", tpl["id"])
		}
	}
	if _, ok := got["model"].(map[string]any); !ok {
		t.Fatalf("no model in %v", got)
	}
	nobody := f.machineAs(t, "nobody", "acme", "humagen:runs:read")
	want(t, f, nobody, "GET", "/api/agent-templates", "", http.StatusForbidden)
}

// Retiring an agent takes agents:write and the project's ownership, refuses
// a stale revision and the platform's own agents, and takes it out of the
// workspace; a new agent may then take its name.
func TestRetireAgent(t *testing.T) {
	f := newFixtureWith(t, platform.Config{TestBuiltinProvider: "offline", TestBuiltinModel: "offline"})
	owner := f.machine(t, "acme", everything...)
	secretAgent(t, f, owner)
	if err := f.p.EnsureBuiltins(t.Context(), "acme"); err != nil {
		t.Fatal(err)
	}

	viewer := f.machineAs(t, "viewer", "acme", "humagen:agents:read")
	want(t, f, viewer, "DELETE", "/api/agents/helper", "", http.StatusForbidden)
	// One the caller cannot see answers as one that does not exist.
	writer := f.machineAs(t, "writer", "acme", "humagen:agents:read", "humagen:agents:write")
	want(t, f, writer, "DELETE", "/api/agents/spy", "", http.StatusNotFound)
	want(t, f, owner, "DELETE", "/api/agents/missing", "", http.StatusNotFound)
	want(t, f, owner, "DELETE", "/api/agents/helper?rev=9", "", http.StatusConflict)
	want(t, f, owner, "DELETE", "/api/agents/helper?rev=x", "", http.StatusBadRequest)
	want(t, f, owner, "DELETE", "/api/agents/humagen", "", http.StatusConflict)

	want(t, f, owner, "DELETE", "/api/agents/helper?rev=1", "", http.StatusNoContent)
	want(t, f, owner, "GET", "/api/agents/helper/look", "", http.StatusNotFound)
	for _, raw := range want(t, f, owner, "GET", "/api/agents", "", http.StatusOK)["agents"].([]any) {
		if raw.(map[string]any)["spec"].(map[string]any)["name"] == "helper" {
			t.Fatal("a retired agent is still listed")
		}
	}
	// Its name stays taken: no new agent takes what it left.
	want(t, f, owner, "POST", "/api/agents", helperSpec, http.StatusConflict)
}

// A store package's agents and the platform's own are paused, never retired
// by a person: the Store and the platform write them again.
func TestRetireRefusesAgentsThePlatformWrites(t *testing.T) {
	f := newFixtureWith(t, platform.Config{TestBuiltinProvider: "offline", TestBuiltinModel: "offline"})
	owner := f.machine(t, "acme", everything...)
	if err := f.p.EnsureBuiltins(t.Context(), "acme"); err != nil {
		t.Fatal(err)
	}
	want(t, f, owner, "POST", "/api/store/packages/core/install", "", http.StatusOK)
	got := want(t, f, owner, "DELETE", "/api/agents/humagen-writer", "", http.StatusConflict)
	if msg, _ := got["error"].(string); !strings.Contains(msg, "pause it instead") && !strings.Contains(msg, "Store package") {
		t.Fatalf("a package agent's refusal says %q", msg)
	}
	want(t, f, owner, "GET", "/api/agents/humagen-writer/look", "", http.StatusOK)
}

// Retiring keeps the name, so nothing the agent left keyed by it passes to a
// new agent: not its memory, look, action policy, home, skills, Slack app or
// control, each of which is gone with it, and its schedules are paused and
// run again only once they ask another agent.
func TestARetiredAgentLeavesNothingToANewOne(t *testing.T) {
	f := newFixture(t)
	owner := f.machine(t, "acme", everything...)
	secretAgent(t, f, owner)
	keeper := `{"name":"keeper","model":"offline","provider":"offline","system_prompt":"Keep."}`
	want(t, f, owner, "POST", "/api/agents", keeper, http.StatusCreated)
	want(t, f, owner, "POST", "/api/agents/keeper/memory", `{"text":"The keeper's private note."}`, http.StatusCreated)
	want(t, f, owner, "PUT", "/api/agents/keeper/look", `{"look":{"display_name":"Keeper","shape":"ghost"}}`, http.StatusOK)
	want(t, f, owner, "PUT", "/api/agents/keeper/action-policy", `{"rules":{"other":"never"},"version":0}`, http.StatusOK)
	want(t, f, owner, "POST", "/api/schedules", `{"name":"keeper-nightly","expression":"0 3 * * *","agent":"keeper","text":"Nightly."}`, http.StatusCreated)
	want(t, f, owner, "PUT", "/api/agents/keeper/home", `{"project":"Secret"}`, http.StatusOK)

	want(t, f, owner, "DELETE", "/api/agents/keeper", "", http.StatusNoContent)

	for _, path := range []string{
		"/api/agents/keeper/memory",
		"/api/agents/keeper/look",
		"/api/agents/keeper/action-policy",
		"/api/agents/keeper/skills",
		"/api/agents/keeper/slack-app",
		"/api/agents/keeper/control",
	} {
		if code, out := send(t, f, owner, "GET", path, ""); code != http.StatusNotFound {
			t.Errorf("GET %s after retiring = %d %s, want 404", path, code, out)
		}
	}
	// Its revisions stay readable.
	want(t, f, owner, "GET", "/api/agents/keeper/history", "", http.StatusOK)
	got := want(t, f, owner, "POST", "/api/agents", keeper, http.StatusConflict)
	if msg, _ := got["error"].(string); !strings.Contains(msg, "retired") {
		t.Fatalf("taking a retired name says %q", msg)
	}

	// Its schedule was paused, and neither runs nor resumes while it asks
	// the retired agent.
	var sched map[string]any
	for _, raw := range want(t, f, owner, "GET", "/api/schedules", "", http.StatusOK)["schedules"].([]any) {
		if e := raw.(map[string]any); e["name"] == "keeper-nightly" {
			sched = e
		}
	}
	if sched == nil || sched["paused"] != true {
		t.Fatalf("a retired agent's schedule = %v, want paused", sched)
	}
	got = want(t, f, owner, "POST", "/api/schedules/keeper-nightly/run", "", http.StatusConflict)
	if msg, _ := got["error"].(string); !strings.Contains(msg, "point the schedule at another agent") {
		t.Fatalf("Run Now on a retired agent's schedule says %q", msg)
	}
	want(t, f, owner, "POST", "/api/schedules/keeper-nightly/resume", "", http.StatusConflict)
	// Pointed at another agent on purpose, it runs that one.
	want(t, f, owner, "PUT", "/api/schedules/keeper-nightly", `{"expression":"0 3 * * *","agent":"helper","text":"Nightly."}`, http.StatusOK)
	want(t, f, owner, "POST", "/api/schedules/keeper-nightly/resume", "", http.StatusOK)
	ran := want(t, f, owner, "POST", "/api/schedules/keeper-nightly/run", "", http.StatusAccepted)
	if ran["agent"] != "helper" {
		t.Fatalf("the re-pointed schedule ran %v", ran["agent"])
	}
}
