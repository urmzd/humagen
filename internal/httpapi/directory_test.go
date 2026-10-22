package httpapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/urmzd/warp/internal/platform"
	"github.com/urmzd/warp/pkg/auth"
	"github.com/urmzd/warp/pkg/projects"
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

// A subject can hold an address: a Tailscale login does, and so can the
// subject a Slack people map names. The directory leaves such a subject out
// and never searches subjects, so no address reaches a reader.
func TestASubjectThatIsAnAddressNeverReachesTheDirectory(t *testing.T) {
	f := newFixture(t)
	f.p.SeePerson(auth.Principal{Kind: auth.User, Tenant: "acme", Subject: "tailscale:grace@acme.test", Name: "Grace"})
	f.p.SeePerson(auth.Principal{Kind: auth.User, Tenant: "acme", Subject: "okta|00u-ada", Name: "Ada"})
	tok := f.machine(t, "acme", "warp:people:read")
	out := want(t, f, tok, "GET", "/api/people", "", http.StatusOK)
	raw, _ := json.Marshal(out)
	if strings.Contains(string(raw), "@") || strings.Contains(string(raw), "acme.test") {
		t.Fatalf("the directory carries an address: %s", raw)
	}
	byHandle := map[string]map[string]any{}
	for _, p := range out["people"].([]any) {
		byHandle[p.(map[string]any)["handle"].(string)] = p.(map[string]any)
	}
	if _, has := byHandle["grace"]["subject"]; has || byHandle["grace"]["name"] != "Grace" {
		t.Fatalf("Grace = %v, want her name and no subject", byHandle["grace"])
	}
	if byHandle["ada"]["subject"] != "okta|00u-ada" {
		t.Fatalf("Ada = %v, want her opaque subject", byHandle["ada"])
	}
	for _, q := range []string{"acme.test", "tailscale"} {
		found := want(t, f, tok, "GET", "/api/people?q="+q, "", http.StatusOK)["people"].([]any)
		if len(found) != 0 {
			t.Fatalf("q=%s found %v: subjects are not searched", q, found)
		}
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

// The directory lists each person's own projects, those naming them as a
// member or an owner, and only the ones the reader may see: a members-only
// project the reader is not in is never named, nor counted.
func TestTheDirectoryListsAPersonsProjectsTheReaderMaySee(t *testing.T) {
	f := newFixture(t)
	f.p.SeePerson(auth.Principal{Kind: auth.User, Tenant: "acme", Subject: "okta|00u-ada", Name: "Ada"})
	w := projects.Writer{By: "root", Govern: true}
	for _, in := range []platform.ProjectInput{
		{Name: "Field", Title: "Field engineering", Members: []projects.Entry{{Who: projects.Everyone, Role: projects.Viewer}, {Who: "okta|00u-ada", Role: projects.Member}}},
		{Name: "Ops", Members: []projects.Entry{{Who: projects.Everyone, Role: projects.Member}}},
		{Name: "Globex", Kind: projects.Account, Members: []projects.Entry{{Who: "okta|00u-ada", Role: projects.Owner}}},
	} {
		if _, err := f.p.CreateProject(t.Context(), "acme", w, in); err != nil {
			t.Fatal(err)
		}
	}
	projectsOf := func(tok string) []any {
		t.Helper()
		out := want(t, f, tok, "GET", "/api/people", "", http.StatusOK)
		for _, p := range out["people"].([]any) {
			if person := p.(map[string]any); person["handle"] == "ada" {
				got, _ := person["projects"].([]any)
				return got
			}
		}
		t.Fatalf("Ada is not in the directory: %v", out)
		return nil
	}
	reader := f.machine(t, "acme", "warp:people:read", "warp:projects:read")
	got := projectsOf(reader)
	if out := want(t, f, reader, "GET", "/api/people", "", http.StatusOK); strings.Contains(fmt.Sprint(out), "projects_more") {
		t.Fatalf("a hidden project is counted: %v", out)
	}
	if len(got) != 1 || got[0].(map[string]any)["name"] != "Field" || got[0].(map[string]any)["title"] != "Field engineering" || got[0].(map[string]any)["role"] != "member" {
		t.Fatalf("a reader outside Globex reads Ada's projects as %v, want Field alone", got)
	}
	governs := f.machine(t, "acme", "warp:people:read", "warp:projects:read", "warp:projects:govern")
	if got := projectsOf(governs); len(got) != 2 || got[1].(map[string]any)["name"] != "Globex" || got[1].(map[string]any)["role"] != "owner" {
		t.Fatalf("projects:govern reads Ada's projects as %v", got)
	}
	if got := projectsOf(f.machine(t, "acme", "warp:people:read")); len(got) != 0 {
		t.Fatalf("a reader without projects:read reads Ada's projects as %v", got)
	}
}

// A person in the directory lists at most MaxPersonProjects projects, and
// counts the rest the reader may see.
func TestADirectoryPersonsProjectsAreBounded(t *testing.T) {
	f := newFixture(t)
	f.p.SeePerson(auth.Principal{Kind: auth.User, Tenant: "acme", Subject: "okta|00u-ada", Name: "Ada"})
	w := projects.Writer{By: "root", Govern: true}
	for i := range MaxPersonProjects + 3 {
		in := platform.ProjectInput{Name: fmt.Sprintf("P%02d", i), Members: []projects.Entry{{Who: projects.Everyone, Role: projects.Viewer}, {Who: "okta|00u-ada", Role: projects.Member}}}
		if _, err := f.p.CreateProject(t.Context(), "acme", w, in); err != nil {
			t.Fatal(err)
		}
	}
	out := want(t, f, f.machine(t, "acme", "warp:people:read", "warp:projects:read"), "GET", "/api/people", "", http.StatusOK)
	ada := out["people"].([]any)[0].(map[string]any)
	if n := len(ada["projects"].([]any)); n != MaxPersonProjects || ada["projects_more"] != float64(3) {
		t.Fatalf("Ada lists %d projects and %v more", n, ada["projects_more"])
	}
}
