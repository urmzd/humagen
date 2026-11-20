package httpapi

import (
	"fmt"
	"io"
	"net/http"
	"strings"
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

// A drawn picture is served at its look's address, to anyone with it, and
// no longer once it is replaced; a token that is not current is a 404.
func TestAgentMarksAreServedByTheirAddress(t *testing.T) {
	f := newFixture(t)
	owner := f.machine(t, "acme", everything...)
	secretAgent(t, f, owner)
	png := "\x89PNG\r\n\x1a\nfirst"
	code, out := sendTyped(t, f, owner, "PUT", "/api/agents/helper/mark", png, "image/png")
	if code != http.StatusOK {
		t.Fatalf("upload = %d %s", code, out)
	}
	marks, _ := f.p.AgentLooks.Marks("acme", []string{"helper"})
	first := marks["helper"]
	if first == "" {
		t.Fatal("no mark recorded")
	}
	get := func(path string) (int, string, http.Header) {
		res, err := http.Get(f.srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(b), res.Header
	}
	code, body, hdr := get("/agent-marks/acme/" + first + ".png")
	if code != http.StatusOK || body != png || hdr.Get("Content-Type") != "image/png" {
		t.Fatalf("serving = %d %q %v", code, body, hdr)
	}
	if code, _, _ := get("/agent-marks/other/" + first + ".png"); code != http.StatusNotFound {
		t.Fatalf("another workspace serves it: %d", code)
	}
	sendTyped(t, f, owner, "PUT", "/api/agents/helper/mark", "\x89PNG\r\n\x1a\nsecond", "image/png")
	if code, _, _ := get("/agent-marks/acme/" + first + ".png"); code != http.StatusNotFound {
		t.Fatalf("a replaced picture is still served: %d", code)
	}
	for _, bad := range []string{"/agent-marks/acme/../x.png", "/agent-marks/acme/" + first, "/agent-marks/acme/zz.png"} {
		if code, _, _ := get(bad); code != http.StatusNotFound {
			t.Fatalf("%s = %d", bad, code)
		}
	}
	viewer := f.machineAs(t, "viewer", "acme", "humagen:agents:read")
	if code, _ := sendTyped(t, f, viewer, "PUT", "/api/agents/helper/mark", png, "image/png"); code != http.StatusForbidden {
		t.Fatalf("a reader drew a picture: %d", code)
	}
}

// sendTyped is send with a body of another type, such as a PNG.
func sendTyped(t *testing.T, f *fixture, tok, method, path, body, contentType string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, f.srv.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", contentType)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	raw, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(raw)
}
