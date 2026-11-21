package httpapi

import (
	"net/http"
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
	// Its name is free again.
	want(t, f, owner, "POST", "/api/agents", helperSpec, http.StatusCreated)
}
