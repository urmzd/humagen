package httpapi

import (
	"net/http"
	"strings"
	"testing"
)

func TestAgentHuesAreStableAndReadConfined(t *testing.T) {
	f := newFixture(t)
	owner := f.machine(t, "acme", everything...)
	secretAgent(t, f, owner)
	first := want(t, f, owner, "GET", "/api/agents", "", http.StatusOK)["hues"].(map[string]any)
	if _, ok := first["spy"]; !ok {
		t.Fatal("owner lacks spy's hue")
	}
	viewer := f.machineAs(t, "reader", "acme", "humagen:agents:read")
	second := want(t, f, viewer, "GET", "/api/agents", "", http.StatusOK)["hues"].(map[string]any)
	if _, ok := second["spy"]; ok {
		t.Fatal("hidden agent's hue revealed")
	}
	if second["helper"] != first["helper"] {
		t.Fatal("reader recolored helper")
	}
	third := want(t, f, owner, "GET", "/api/agents", "", http.StatusOK)["hues"].(map[string]any)
	if third["spy"] != first["spy"] {
		t.Fatal("another reader recolored spy")
	}
}

func TestAgentHueLookupIsBoundedAndDoesNotEnumerateHiddenNames(t *testing.T) {
	f := newFixture(t)
	owner := f.machine(t, "acme", everything...)
	secretAgent(t, f, owner)
	reader := f.machineAs(t, "reader", "acme", "humagen:runs:read")
	want(t, f, reader, "GET", "/api/agent-hues", "", http.StatusBadRequest)
	out := want(t, f, reader, "GET", "/api/agent-hues?names=helper,spy,missing", "", http.StatusOK)["hues"].(map[string]any)
	if len(out) != 1 || out["helper"] == nil {
		t.Fatalf("lookup = %v", out)
	}
	want(t, f, reader, "GET", "/api/agent-hues?names="+strings.Repeat("helper,", 100)+"helper", "", http.StatusBadRequest)
	want(t, f, f.machine(t, "acme"), "GET", "/api/agent-hues?names=helper", "", http.StatusForbidden)
}

func TestProjectOnlyReadersResolveAgentHuesWithoutRunOrAgentRead(t *testing.T) {
	f := newFixture(t)
	owner := f.machine(t, "acme", everything...)
	secretAgent(t, f, owner)
	reader := f.machineAs(t, "project-reader", "acme", "humagen:projects:read")
	want(t, f, reader, "GET", "/api/agent-hues?names=helper", "", http.StatusForbidden)
	out := want(t, f, reader, "GET", "/api/project-agent-hues?names=helper,spy,missing", "", http.StatusOK)["hues"].(map[string]any)
	if len(out) != 1 || out["helper"] == nil {
		t.Fatalf("project-only hues = %v", out)
	}
	want(t, f, f.machine(t, "acme"), "GET", "/api/project-agent-hues?names=helper", "", http.StatusForbidden)
}
