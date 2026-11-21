package httpapi

import (
	"net/http"
	"strings"
	"testing"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/saige/flow/isolation"
	"github.com/urmzd/saige/flow/isolation/isolationtest"
)

// githubClient is a GitHub client of acme that reads and writes the named
// repositories.
func githubClient(t *testing.T, f *fixture, owner, id, tenantOwner, read, write string) {
	t.Helper()
	want(t, f, owner, "POST", "/api/clients", `{"id":"`+id+`","name":"`+id+`","surface":"github","identity":{"owners":["`+tenantOwner+`"]},"agents":["helper"],`+
		`"github":{"read_repositories":[`+read+`],"action_repositories":[`+write+`]},`+
		`"credentials":{"token":{"value":"`+botToken+`"},"signing_secret":{"value":"`+signingSecret+`"}}}`, http.StatusCreated)
}

func optionsFixture(t *testing.T) (*fixture, string) {
	t.Helper()
	leaser := &isolationtest.Leaser{Dir: t.TempDir()}
	f := newFixtureWith(t, platform.Config{Sandbox: isolation.Sandbox{Leaser: leaser, Name: "docker/runsc"}, SandboxContained: true})
	owner := f.person(t, "acme", "ana", "Ana", "humagen-admins")
	want(t, f, owner, "POST", "/api/agents", helperSpec, http.StatusCreated)
	return f, owner
}

func TestSessionOptionsListThisDeploymentsEnvironmentsAndTheTenantsRepositories(t *testing.T) {
	f, owner := optionsFixture(t)
	githubClient(t, f, owner, "gh", "acme", `"acme/web","acme/api"`, `"acme/web","acme/infra"`)
	other := f.person(t, "other", "olga", "Olga", "humagen-admins")
	want(t, f, other, "POST", "/api/agents", helperSpec, http.StatusCreated)
	githubClient(t, f, other, "gh", "other", `"other/secret"`, ``)

	raw, out := read(t, f, owner, "/api/sessions/options")
	envs, _ := out["environments"].([]any)
	if len(envs) != 2 {
		t.Fatalf("environments = %s", raw)
	}
	auto, docker := envs[0].(map[string]any), envs[1].(map[string]any)
	if auto["name"] != "auto" || auto["label"] != "Auto" || auto["isolated"] != true || auto["terminal"] != true {
		t.Errorf("auto = %v", auto)
	}
	if docker["name"] != "docker" || docker["label"] != "Docker sandbox" || docker["isolated"] != true || docker["terminal"] != true {
		t.Errorf("docker = %v", docker)
	}
	if got := strings.Join(names2(out["repos"]), ","); got != "acme/api,acme/infra,acme/web" {
		t.Errorf("repos = %s, want only acme's, sorted and deduplicated", raw)
	}

	// The unisolated default offers no terminal.
	plain := newFixture(t)
	tok := plain.machine(t, "acme", everything...)
	_, po := read(t, plain, tok, "/api/sessions/options")
	un := po["environments"].([]any)[1].(map[string]any)
	if un["name"] != "unisolated" || un["label"] != "This server (unisolated)" || un["isolated"] != false || un["terminal"] != false {
		t.Errorf("unisolated = %v", un)
	}
	if repos, ok := po["repos"].([]any); !ok || len(repos) != 0 {
		t.Errorf("repos = %v, want an empty list", po["repos"])
	}
}

func names2(list any) []string {
	var out []string
	for _, v := range list.([]any) {
		out = append(out, v.(string))
	}
	return out
}

func TestASessionStoresItsEnvironmentAndRepositories(t *testing.T) {
	f, owner := optionsFixture(t)
	githubClient(t, f, owner, "gh", "acme", `"acme/web","acme/api"`, ``)
	s := want(t, f, owner, "POST", "/api/sessions", `{"agent":"helper","project":"~","text":"Go","environment":"docker","repos":["acme/web","acme/api"]}`, http.StatusCreated)
	raw, _ := read(t, f, owner, "/api/sessions/"+s["id"].(string))
	if !strings.Contains(raw, `"environment":"docker"`) || !strings.Contains(raw, `"repos":["acme/web","acme/api"]`) {
		t.Fatalf("the session does not carry its choice: %s", raw)
	}
	want(t, f, owner, "POST", "/api/sessions", `{"agent":"helper","project":"~","text":"Go","environment":"auto"}`, http.StatusCreated)
}

func TestASessionRefusesAChoiceItWasNotOffered(t *testing.T) {
	f, owner := optionsFixture(t)
	githubClient(t, f, owner, "gh", "acme", `"acme/web"`, ``)
	var many []string
	for i := 0; i < 11; i++ {
		many = append(many, `"acme/web"`)
	}
	for name, body := range map[string]string{
		"unknown environment": `{"agent":"helper","project":"~","text":"Go","environment":"mainframe"}`,
		"unlisted repo":       `{"agent":"helper","project":"~","text":"Go","repos":["acme/secret"]}`,
		"not owner/name":      `{"agent":"helper","project":"~","text":"Go","repos":["web"]}`,
		"duplicate":           `{"agent":"helper","project":"~","text":"Go","repos":["acme/web","acme/web"]}`,
		"eleven repos":        `{"agent":"helper","project":"~","text":"Go","repos":[` + strings.Join(many, ",") + `]}`,
		"with surface":        `{"text":"Go","environment":"auto","surface":{"team":"T1","channel":"C1","thread":"1.2"},"act_as":{"id":"U1"}}`,
	} {
		if code, out := status(t, f, owner, "POST", "/api/sessions", body); code != http.StatusBadRequest {
			t.Errorf("%s = %d %v, want 400", name, code, out)
		}
	}
}

func TestTheNewSessionRoutesCheckRunsReadAndTheTenant(t *testing.T) {
	f, owner := optionsFixture(t)
	s := want(t, f, owner, "POST", "/api/sessions", `{"agent":"helper","project":"~","text":"Go"}`, http.StatusCreated)
	id := s["id"].(string)

	// An agent with no browser provider says so, in plain words.
	b := want(t, f, owner, "GET", "/api/sessions/"+id+"/browser", "", http.StatusOK)
	if b["available"] != false || b["reason"] != "This session has no agent with a browser." {
		t.Errorf("availability = %v", b)
	}
	// Another tenant's session is not there.
	other := f.person(t, "other", "olga", "Olga", "humagen-admins")
	want(t, f, other, "GET", "/api/sessions/"+id+"/browser", "", http.StatusNotFound)

	// Without runs:read both routes are refused.
	noRuns := f.machineAs(t, "svc-noruns", "acme", "humagen:clients:read")
	for _, path := range []string{"/api/sessions/options", "/api/sessions/" + id + "/browser"} {
		want(t, f, noRuns, "GET", path, "", http.StatusForbidden)
	}
}

func TestTheBrowserCheckListsTheAgentsBrowsers(t *testing.T) {
	f, _, token := browserFixture(t)
	path := sessionWith(t, f, token, "surfer")
	b := want(t, f, token, "GET", path, "", http.StatusOK)
	if b["available"] != true || strings.Join(names2(b["providers"]), ",") != "browser" || b["reason"] != "" {
		t.Errorf("availability = %v", b)
	}
}
