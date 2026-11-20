package httpapi

import (
	"fmt"
	"net/http"
	"testing"
)

// An agent's look is kept, reaches every reader of its hue, stays hidden
// with the agent, and never takes another agent's name or color.
func TestAgentLooks(t *testing.T) {
	f := newFixture(t)
	owner := f.machine(t, "acme", everything...)
	secretAgent(t, f, owner)

	got := want(t, f, owner, "PUT", "/api/agents/helper/look",
		`{"look":{"display_name":"  Helpy ","shape":"ghost","hat":"crown","face":"eyes"}}`, http.StatusOK)
	look := got["look"].(map[string]any)
	if look["display_name"] != "Helpy" || look["shape"] != "ghost" || look["hat"] != "crown" || look["face"] != nil {
		t.Fatalf("kept %v", look)
	}
	if got["choices"].(map[string]any)["shapes"] == nil {
		t.Fatal("no choices to pick from")
	}

	reader := f.machineAs(t, "reader", "acme", "humagen:runs:read")
	looks := want(t, f, reader, "GET", "/api/agent-hues?names=helper,spy", "", http.StatusOK)["looks"].(map[string]any)
	if looks["helper"].(map[string]any)["display_name"] != "Helpy" {
		t.Fatalf("lookup looks = %v", looks)
	}
	want(t, f, owner, "PUT", "/api/agents/spy/look", `{"look":{"hat":"beret"}}`, http.StatusOK)
	looks = want(t, f, reader, "GET", "/api/agent-hues?names=helper,spy", "", http.StatusOK)["looks"].(map[string]any)
	if _, shown := looks["spy"]; shown {
		t.Fatal("a hidden agent's look reached a reader who may not see it")
	}
	viewer := f.machineAs(t, "viewer", "acme", "humagen:agents:read")
	want(t, f, viewer, "GET", "/api/agents/spy/look", "", http.StatusNotFound)
	want(t, f, viewer, "PUT", "/api/agents/helper/look", `{"look":{}}`, http.StatusForbidden)
	listed := want(t, f, viewer, "GET", "/api/agents", "", http.StatusOK)["looks"].(map[string]any)
	if _, shown := listed["spy"]; shown || listed["helper"] == nil {
		t.Fatalf("listed looks = %v", listed)
	}

	want(t, f, owner, "PUT", "/api/agents/spy/look", `{"look":{"display_name":"helpy"}}`, http.StatusConflict)
	want(t, f, owner, "PUT", "/api/agents/spy/look", `{"look":{"display_name":"Helper"}}`, http.StatusConflict)
	want(t, f, owner, "PUT", "/api/agents/helper/look", `{"look":{"shape":"dragon"}}`, http.StatusBadRequest)
	want(t, f, owner, "PUT", "/api/agents/helper/look", `{"look":{},"hue":400}`, http.StatusBadRequest)
	want(t, f, owner, "PUT", "/api/agents/missing/look", `{"look":{}}`, http.StatusNotFound)

	spy := want(t, f, owner, "GET", "/api/agents/spy/look", "", http.StatusOK)["hue"].(float64)
	want(t, f, owner, "PUT", "/api/agents/helper/look", fmt.Sprintf(`{"look":{"display_name":"Helpy"},"hue":%v}`, spy), http.StatusConflict)
	moved := want(t, f, owner, "PUT", "/api/agents/helper/look", `{"look":{"display_name":"Helpy"},"hue":347.5}`, http.StatusOK)
	if moved["hue"] != 347.5 {
		t.Fatalf("hue after choosing = %v", moved["hue"])
	}
	for _, h := range moved["taken"].([]any) {
		if h == 347.5 {
			t.Fatal("an agent's own hue is listed as taken")
		}
	}
	hues := want(t, f, owner, "GET", "/api/agents", "", http.StatusOK)["hues"].(map[string]any)
	if hues["helper"] != 347.5 {
		t.Fatalf("listed hue = %v", hues["helper"])
	}

	want(t, f, owner, "PUT", "/api/agents/helper/look", `{"look":{}}`, http.StatusOK)
	looks = want(t, f, reader, "GET", "/api/agent-hues?names=helper", "", http.StatusOK)["looks"].(map[string]any)
	if len(looks) != 0 {
		t.Fatalf("a reset look is still served: %v", looks)
	}
}
