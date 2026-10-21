package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestSigningInPutsAPersonInTheDirectoryWithNoEmail(t *testing.T) {
	f := newFixture(t)
	c := f.signIn(t, "warp-viewers")
	res, out := f.do(t, c, "GET", "/api/people", "", "")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("a viewer reading the directory = %d %v", res.StatusCode, out)
	}
	people, _ := out["people"].([]any)
	if len(people) != 1 {
		t.Fatalf("the directory after one sign-in = %v", out)
	}
	ada := people[0].(map[string]any)
	if ada["subject"] != "00u-ada" || ada["name"] != "Ada" || ada["handle"] != "ada" || ada["picture"] != nil {
		t.Fatalf("Ada = %v", ada)
	}
	raw, _ := json.Marshal(out)
	if strings.Contains(strings.ToLower(string(raw)), "email") || strings.Contains(string(raw), "@") {
		t.Fatalf("the directory carries an address: %s", raw)
	}
}

func TestAMachineIsNotAPerson(t *testing.T) {
	f := newFixture(t)
	tok := f.machine(t, "acme", "warp:people:read")
	want(t, f, tok, "GET", "/api/people", "", http.StatusOK)
	if n := f.p.People.Len("acme"); n != 0 {
		t.Fatalf("a machine's request recorded %d people", n)
	}
}

func TestTheDirectoryIsReadWithPeopleRead(t *testing.T) {
	f := newFixture(t)
	if code, _ := status(t, f, f.machine(t, "acme", "warp:projects:read"), "GET", "/api/people", ""); code != http.StatusForbidden {
		t.Fatalf("the directory without people:read = %d", code)
	}
}

func TestSettingsNamingTheBaselineSayWhereItWent(t *testing.T) {
	f := newFixture(t)
	tok := f.machine(t, "acme", everything...)
	out := want(t, f, tok, "PUT", "/api/settings", `{"baseline":"lead","history":"recent","history_limit":12000}`, http.StatusBadRequest)
	if msg, _ := out["error"].(string); !strings.Contains(msg, "workspace default") || !strings.Contains(msg, "/api/projects/~/agents") {
		t.Fatalf("the refusal = %v", out)
	}
}

func TestAgentsSayWhereEachLivesAndIsEnabled(t *testing.T) {
	f := newFixture(t)
	tok := f.machine(t, "acme", everything...)
	want(t, f, tok, "POST", "/api/agents", helperSpec, http.StatusCreated)
	want(t, f, tok, "POST", "/api/projects", `{"name":"Field"}`, http.StatusCreated)
	want(t, f, tok, "PUT", "/api/agents/helper/home", `{"project":"Field"}`, http.StatusOK)
	out := want(t, f, tok, "GET", "/api/agents", "", http.StatusOK)
	homes, _ := out["homes"].(map[string]any)
	enabled, _ := out["enabled_in"].(map[string]any)
	if homes["helper"] != "Field" || len(enabled["helper"].([]any)) != 0 {
		t.Fatalf("homes %v, enabled_in %v", homes, enabled)
	}
	// A project agent is not available in the workspace itself.
	if code, body := status(t, f, tok, "POST", "/api/runs", `{"agent":"helper","text":"hi"}`); code != http.StatusForbidden || !strings.Contains(body["error"].(string), "lives in Field") {
		t.Fatalf("a project agent outside its home = %d %v", code, body)
	}
	want(t, f, tok, "POST", "/api/runs", `{"agent":"helper","text":"hi","project":"Field"}`, http.StatusAccepted)
	want(t, f, tok, "PUT", "/api/agents/helper/home", `{"project":"~"}`, http.StatusOK)
	want(t, f, tok, "POST", "/api/runs", `{"agent":"helper","text":"hi"}`, http.StatusAccepted)
}

func TestAPersonsPhotoIsTheirOwn(t *testing.T) {
	f := newFixture(t)
	c := f.signIn(t, "warp-viewers")
	png := "\x89PNG\r\n\x1a\n" + strings.Repeat("\x00", 32)
	req, _ := http.NewRequest("PUT", f.srv.URL+"/api/preferences/photo", strings.NewReader(png))
	req.Header.Set("Origin", f.srv.URL)
	res, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("uploading a photo = %d", res.StatusCode)
	}
	_, out := f.do(t, c, "GET", "/api/people", "", "")
	ada := out["people"].([]any)[0].(map[string]any)
	pic, _ := ada["picture"].(string)
	if !strings.HasPrefix(pic, "/api/people/ada/photo?v=") {
		t.Fatalf("Ada's picture = %q", pic)
	}
	res, err = c.Get(f.srv.URL + pic)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK || string(body) != png || res.Header.Get("Content-Type") != "image/png" {
		t.Fatalf("Ada's photo = %d %q", res.StatusCode, res.Header.Get("Content-Type"))
	}
}
