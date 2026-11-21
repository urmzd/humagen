package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

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

// fakePuller stands in for the Docker daemon: it pins a reference to a
// made-up digest, and fails one that names "broken".
type fakePuller struct {
	mu    sync.Mutex
	pulls []string // ref and credential, "ref|cred"
}

func (p *fakePuller) Pull(_ context.Context, ref, cred string) (string, error) {
	p.mu.Lock()
	p.pulls = append(p.pulls, ref+"|"+cred)
	p.mu.Unlock()
	if strings.Contains(ref, "broken") {
		return "", errors.New("docker pull: manifest unknown")
	}
	if strings.Contains(ref, "@sha256:") {
		return ref, nil
	}
	repo := ref
	if i := strings.LastIndex(repo, ":"); i > strings.LastIndex(repo, "/") {
		repo = repo[:i]
	}
	return repo + "@sha256:" + strings.Repeat("ab", 32), nil
}

func (p *fakePuller) seen() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.pulls...)
}

func optionsFixture(t *testing.T) (*fixture, string) {
	f, owner, _ := imagesFixture(t)
	return f, owner
}

// imagesFixture is a contained docker-like sandbox whose workspaces may add
// images from ghcr.io, pulled by a fake.
func imagesFixture(t *testing.T) (*fixture, string, *fakePuller) {
	t.Helper()
	leaser := &isolationtest.Leaser{Dir: t.TempDir()}
	puller := &fakePuller{}
	f := newFixtureWith(t, platform.Config{
		Sandbox:          isolation.Sandbox{Leaser: leaser, Name: "docker/runsc"},
		SandboxContained: true,
		SandboxImages:    &platform.ImageConfig{Default: "python:3.13-alpine", Registries: []string{"ghcr.io"}, Puller: puller},
	})
	owner := f.person(t, "acme", "ana", "Ana", "humagen-admins")
	want(t, f, owner, "POST", "/api/agents", helperSpec, http.StatusCreated)
	return f, owner, puller
}

// settled waits for an image's check to finish and returns it.
func settled(t *testing.T, f *fixture, token, id string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		_, out := read(t, f, token, "/api/images")
		for _, v := range out["images"].([]any) {
			img := v.(map[string]any)
			if img["id"] == id && img["status"] != "checking" {
				return img
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("image %s is still checking", id)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestSessionOptionsListTheImagesAndTheTenantsRepositories(t *testing.T) {
	f, owner := optionsFixture(t)
	githubClient(t, f, owner, "gh", "acme", `"acme/web","acme/api"`, `"acme/web","acme/infra"`)
	other := f.person(t, "other", "olga", "Olga", "humagen-admins")
	want(t, f, other, "POST", "/api/agents", helperSpec, http.StatusCreated)
	githubClient(t, f, other, "gh", "other", `"other/secret"`, ``)

	raw, out := read(t, f, owner, "/api/sessions/options")
	imgs, _ := out["images"].([]any)
	if len(imgs) != 1 {
		t.Fatalf("images = %s", raw)
	}
	def := imgs[0].(map[string]any)
	if def["id"] != "default" || def["ref"] != "python:3.13-alpine" || def["status"] != "ready" || def["default"] != true {
		t.Errorf("default = %v", def)
	}
	if out["sandbox"] != "docker/runsc" || out["isolated"] != true || out["terminal"] != true {
		t.Errorf("sandbox = %s", raw)
	}
	if got := strings.Join(names2(out["imageRegistries"]), ","); got != "ghcr.io" {
		t.Errorf("registries = %s", raw)
	}
	if got := strings.Join(names2(out["repos"]), ","); got != "acme/api,acme/infra,acme/web" {
		t.Errorf("repos = %s, want only acme's, sorted and deduplicated", raw)
	}

	// The unisolated default offers its own image alone, no terminal and
	// no way to add one.
	plain := newFixture(t)
	tok := plain.machine(t, "acme", everything...)
	praw, po := read(t, plain, tok, "/api/sessions/options")
	if len(po["images"].([]any)) != 1 || po["isolated"] != false || po["terminal"] != false || len(po["imageRegistries"].([]any)) != 0 {
		t.Errorf("unisolated = %s", praw)
	}
	if repos, ok := po["repos"].([]any); !ok || len(repos) != 0 {
		t.Errorf("repos = %v, want an empty list", po["repos"])
	}
	want(t, plain, tok, "POST", "/api/images", `{"name":"Agent","ref":"ghcr.io/acme/agent:1.4"}`, http.StatusConflict)
}

func names2(list any) []string {
	out := []string{}
	for _, v := range list.([]any) {
		out = append(out, v.(string))
	}
	return out
}

// An image added is checked in the background: pulled with its credential,
// pinned to its digest and started. Once ready it is offered and a session
// keeps it; the credential is never shown.
func TestAnImageIsCheckedPinnedAndOffered(t *testing.T) {
	f, owner, puller := imagesFixture(t)
	img := want(t, f, owner, "POST", "/api/images", `{"name":"Agent tools","ref":"ghcr.io/acme/agent:1.4","credential":"bot:s3cret","warm":true}`, http.StatusCreated)
	if img["status"] != "checking" || img["credential"] != true {
		t.Fatalf("added = %v", img)
	}
	id := img["id"].(string)
	got := settled(t, f, owner, id)
	if got["status"] != "ready" || got["digest"] != "ghcr.io/acme/agent@sha256:"+strings.Repeat("ab", 32) {
		t.Fatalf("checked = %v", got)
	}
	if pulls := puller.seen(); len(pulls) != 1 || pulls[0] != "ghcr.io/acme/agent:1.4|bot:s3cret" {
		t.Errorf("pulls = %v", pulls)
	}
	raw, _ := read(t, f, owner, "/api/images")
	if strings.Contains(raw, "s3cret") {
		t.Errorf("the credential is shown: %s", raw)
	}
	_, opts := read(t, f, owner, "/api/sessions/options")
	if imgs := opts["images"].([]any); len(imgs) != 2 || imgs[1].(map[string]any)["id"] != id {
		t.Errorf("options = %v", opts["images"])
	}

	s := want(t, f, owner, "POST", "/api/sessions", `{"agent":"helper","project":"~","text":"Go","image":"`+id+`","repos":[]}`, http.StatusCreated)
	sraw, _ := read(t, f, owner, "/api/sessions/"+s["id"].(string))
	if !strings.Contains(sraw, `"image":"`+id+`"`) {
		t.Fatalf("the session does not carry its image: %s", sraw)
	}
	want(t, f, owner, "POST", "/api/sessions", `{"agent":"helper","project":"~","text":"Go","image":"default"}`, http.StatusCreated)

	// Changed, checked again, removed.
	if ch := want(t, f, owner, "PATCH", "/api/images/"+id, `{"warm":false}`, http.StatusOK); ch["warm"] == true {
		t.Errorf("changed = %v", ch)
	}
	want(t, f, owner, "PATCH", "/api/images/"+id, `{"check":true}`, http.StatusOK)
	settled(t, f, owner, id)
	want(t, f, owner, "DELETE", "/api/images/"+id, "", http.StatusNoContent)
	want(t, f, owner, "DELETE", "/api/images/"+id, "", http.StatusNotFound)
}

func TestAnImageThatFailsItsCheckSaysWhyAndIsNotOffered(t *testing.T) {
	f, owner, _ := imagesFixture(t)
	img := want(t, f, owner, "POST", "/api/images", `{"name":"Broken","ref":"ghcr.io/acme/broken:1"}`, http.StatusCreated)
	id := img["id"].(string)
	got := settled(t, f, owner, id)
	if got["status"] != "failed" || !strings.Contains(got["problem"].(string), "manifest unknown") {
		t.Fatalf("checked = %v", got)
	}
	if code, out := status(t, f, owner, "POST", "/api/sessions", `{"agent":"helper","project":"~","text":"Go","image":"`+id+`"}`); code != http.StatusBadRequest {
		t.Errorf("a session on a failed image = %d %v, want 400", code, out)
	}
}

func TestAnImageIsRefusedWhenItIsNotAllowedOrNotTheTenants(t *testing.T) {
	f, owner, _ := imagesFixture(t)
	for name, body := range map[string]string{
		"registry not allowed": `{"name":"Hub","ref":"docker.io/library/python:3"}`,
		"bare docker hub":      `{"name":"Hub","ref":"python:3"}`,
		"an option":            `{"name":"X","ref":"-v /:/host"}`,
		"spaces":               `{"name":"X","ref":"ghcr.io/a/b c"}`,
		"short digest":         `{"name":"X","ref":"ghcr.io/a/b@sha256:abc"}`,
		"no name":              `{"name":" ","ref":"ghcr.io/a/b:1"}`,
		"two-line credential":  `{"name":"X","ref":"ghcr.io/a/b:1","credential":"a\nb"}`,
	} {
		if code, out := status(t, f, owner, "POST", "/api/images", body); code != http.StatusBadRequest {
			t.Errorf("%s = %d %v, want 400", name, code, out)
		}
	}
	img := want(t, f, owner, "POST", "/api/images", `{"name":"Agent","ref":"ghcr.io/acme/agent:1"}`, http.StatusCreated)
	id := img["id"].(string)
	want(t, f, owner, "POST", "/api/images", `{"name":"agent","ref":"ghcr.io/acme/other:1"}`, http.StatusConflict)

	// Another tenant neither sees nor changes it.
	other := f.person(t, "other", "olga", "Olga", "humagen-admins")
	if _, out := read(t, f, other, "/api/images"); len(out["images"].([]any)) != 0 {
		t.Errorf("other tenant sees %v", out["images"])
	}
	want(t, f, other, "PATCH", "/api/images/"+id, `{"warm":true}`, http.StatusNotFound)
	want(t, f, other, "DELETE", "/api/images/"+id, "", http.StatusNotFound)
	want(t, f, other, "POST", "/api/sessions", `{"agent":"helper","project":"~","text":"Go","image":"`+id+`"}`, http.StatusBadRequest)

	// Reading needs runs:read; adding, changing and removing clients:write.
	reader := f.machineAs(t, "svc-reader", "acme", "humagen:runs:read")
	want(t, f, reader, "GET", "/api/images", "", http.StatusOK)
	want(t, f, reader, "POST", "/api/images", `{"name":"Y","ref":"ghcr.io/acme/y:1"}`, http.StatusForbidden)
	want(t, f, reader, "DELETE", "/api/images/"+id, "", http.StatusForbidden)
	noRuns := f.machineAs(t, "svc-noruns", "acme", "humagen:clients:read")
	want(t, f, noRuns, "GET", "/api/images", "", http.StatusForbidden)
}

func TestASessionRefusesAChoiceItWasNotOffered(t *testing.T) {
	f, owner := optionsFixture(t)
	githubClient(t, f, owner, "gh", "acme", `"acme/web"`, ``)
	var many []string
	for i := 0; i < 11; i++ {
		many = append(many, `"acme/web"`)
	}
	for name, body := range map[string]string{
		"unknown image":  `{"agent":"helper","project":"~","text":"Go","image":"img_000000000000"}`,
		"unlisted repo":  `{"agent":"helper","project":"~","text":"Go","repos":["acme/secret"]}`,
		"not owner/name": `{"agent":"helper","project":"~","text":"Go","repos":["web"]}`,
		"duplicate":      `{"agent":"helper","project":"~","text":"Go","repos":["acme/web","acme/web"]}`,
		"eleven repos":   `{"agent":"helper","project":"~","text":"Go","repos":[` + strings.Join(many, ",") + `]}`,
		"with surface":   `{"text":"Go","image":"default","surface":{"team":"T1","channel":"C1","thread":"1.2"},"act_as":{"id":"U1"}}`,
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
