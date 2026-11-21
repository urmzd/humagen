package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// pullDaemon is a Docker daemon that pulls nothing: it answers the docker
// CLI's pull and inspect, and keeps the X-Registry-Auth of each pull.
type pullDaemon struct {
	mu    sync.Mutex
	auths []string
}

func (d *pullDaemon) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Api-Version", "1.47")
	w.Header().Set("Content-Type", "application/json")
	switch {
	case strings.HasSuffix(r.URL.Path, "/images/create"):
		d.mu.Lock()
		d.auths = append(d.auths, r.Header.Get("X-Registry-Auth"))
		d.mu.Unlock()
		_, _ = w.Write([]byte(`{"status":"done"}` + "\n"))
	case strings.HasSuffix(r.URL.Path, "/json"):
		_, _ = w.Write([]byte(`{"Id":"sha256:1","RepoDigests":["ghcr.io/acme/private@sha256:ab","python@sha256:ab"]}`))
	case strings.HasSuffix(r.URL.Path, "/_ping"):
		_, _ = w.Write([]byte("OK"))
	default:
		_, _ = w.Write([]byte("{}"))
	}
}

// sent is the registry credential of the daemon's only pull, as the CLI
// sent it: the decoded X-Registry-Auth.
func (d *pullDaemon) sent(t *testing.T) map[string]string {
	t.Helper()
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.auths) != 1 {
		t.Fatalf("the daemon was asked for %d pulls, want 1", len(d.auths))
	}
	header := d.auths[0]
	d.auths = nil
	out := map[string]string{}
	if header == "" {
		return out
	}
	raw, err := base64.URLEncoding.DecodeString(header)
	if err != nil {
		t.Fatalf("X-Registry-Auth %q: %v", header, err)
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("X-Registry-Auth %s: %v", raw, err)
	}
	for k, v := range out {
		if v == "" {
			delete(out, k)
		}
	}
	return out
}

// A workspace's pull is given the workspace's credential or none. The
// machine's own Docker logins never reach it, whichever way the docker CLI
// would find them: its configuration's auths, credsStore and credHelpers,
// the default helper it picks when one is on PATH, DOCKER_CONFIG and
// DOCKER_AUTH_CONFIG. Run against the real docker CLI and a daemon that
// records what each pull was sent.
func TestAWorkspacePullNeverUsesTheMachinesDockerLogins(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("no docker CLI")
	}
	dir := t.TempDir()
	calls := filepath.Join(dir, "helper-calls")
	bin := filepath.Join(dir, "bin")
	if err := os.Mkdir(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	// The machine's helpers, each of which has the operator's login for
	// every registry. "pass" itself is here too: on Linux the CLI picks
	// docker-credential-pass when pass is on PATH.
	helper := "#!/bin/sh\necho \"$0 $1 $(cat)\" >> " + calls + "\nif [ \"$1\" = get ]; then echo '{\"ServerURL\":\"ghcr.io\",\"Username\":\"HOST-OPERATOR\",\"Secret\":\"HOST-TOKEN\"}'; fi\nif [ \"$1\" = list ]; then echo '{\"ghcr.io\":\"HOST-OPERATOR\",\"https://index.docker.io/v1/\":\"HOST-OPERATOR\"}'; fi\n"
	for _, name := range []string{"pass", "docker-credential-pass", "docker-credential-secretservice", "docker-credential-osxkeychain", "docker-credential-desktop", "docker-credential-wincred", "docker-credential-operator"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(helper), 0o755); err != nil { //nolint:gosec // G306: a helper is run
			t.Fatal(err)
		}
	}
	hostAuth := base64.StdEncoding.EncodeToString([]byte("HOST-OPERATOR:HOST-TOKEN"))
	hostConfig := `{"auths":{"ghcr.io":{"auth":"` + hostAuth + `"},"https://index.docker.io/v1/":{"auth":"` + hostAuth + `"}},"credsStore":"operator","credHelpers":{"ghcr.io":"operator"}}`
	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(filepath.Join(home, ".docker"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".docker", "config.json"), []byte(hostConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HOME", home)
	t.Setenv("DOCKER_CONFIG", filepath.Join(home, ".docker"))
	t.Setenv("DOCKER_AUTH_CONFIG", `{"auths":{"ghcr.io":{"auth":"`+hostAuth+`"},"https://index.docker.io/v1/":{"auth":"`+hostAuth+`"}}}`)
	t.Setenv("REGISTRY_AUTH_FILE", filepath.Join(home, ".docker", "config.json"))

	daemon := &pullDaemon{}
	srv := httptest.NewServer(daemon)
	defer srv.Close()
	d := dockerPuller{host: "tcp://" + strings.TrimPrefix(srv.URL, "http://")}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	for _, c := range []struct {
		name, ref, credential string
		want                  map[string]string
	}{
		{"anonymous", "ghcr.io/acme/private:1", "", map[string]string{}},
		{"anonymous by digest", "ghcr.io/acme/private@sha256:" + strings.Repeat("ab", 32), "", map[string]string{}},
		{"anonymous from Docker Hub", "python:3", "", map[string]string{}},
		{"the workspace's credential", "ghcr.io/acme/private:1", "ws-user:ws-token", map[string]string{"username": "ws-user", "password": "ws-token", "serveraddress": "ghcr.io"}},
		{"the workspace's credential for Docker Hub", "python:3", "ws-user:ws-token", map[string]string{"username": "ws-user", "password": "ws-token", "serveraddress": "https://index.docker.io/v1/"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			_ = os.Remove(calls)
			if _, err := d.Pull(ctx, c.ref, c.credential); err != nil {
				t.Fatalf("pull: %v", err)
			}
			got := daemon.sent(t)
			if len(got) != len(c.want) {
				t.Errorf("the pull was sent %v, want %v", got, c.want)
			}
			for k, v := range c.want {
				if got[k] != v {
					t.Errorf("the pull was sent %s=%q, want %q (all of it: %v)", k, got[k], v, got)
				}
			}
			if b, _ := os.ReadFile(calls); len(b) > 0 {
				t.Errorf("the machine's credential helper was asked: %s", b)
			}
		})
	}
}

// The helpers a pull's own PATH puts first have nothing, should the CLI
// ever pick one by itself.
func TestAPullsOwnCredentialHelpersHaveNothing(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
	dir := t.TempDir()
	if err := pullConfig(dir, "ghcr.io/acme/a:1", "ws-user:ws-token"); err != nil {
		t.Fatal(err)
	}
	for _, h := range hostHelpers {
		helper := filepath.Join(dir, "bin", "docker-credential-"+h)
		get := exec.Command(helper, "get")
		get.Stdin = strings.NewReader("ghcr.io")
		out, err := get.Output()
		if err == nil || strings.TrimSpace(string(out)) != "credentials not found in native keychain" {
			t.Errorf("%s get: %q %v, want not found", h, out, err)
		}
		if out, err := exec.Command(helper, "list").Output(); err != nil || strings.TrimSpace(string(out)) != "{}" {
			t.Errorf("%s list: %q %v, want none", h, out, err)
		}
	}
}

// A pull's environment keeps where the daemon is and drops what names or
// carries the machine user's registry credentials.
func TestAPullsEnvironmentDropsTheMachinesDockerSettings(t *testing.T) {
	env := pullEnv([]string{
		"PATH=/usr/bin", "HOME=/root", "DOCKER_HOST=tcp://h:2376", "DOCKER_TLS_VERIFY=1", "DOCKER_CERT_PATH=/certs",
		"DOCKER_CONFIG=/root/.docker", "DOCKER_AUTH_CONFIG={}", "DOCKER_CONTEXT=desktop", "DOCKER_CUSTOM_HEADERS=X-Registry-Auth=e30=", "docker_auth_config={}", "REGISTRY_AUTH_FILE=/x",
	}, "/tmp/pull")
	want := []string{"HOME=/root", "DOCKER_HOST=tcp://h:2376", "DOCKER_TLS_VERIFY=1", "DOCKER_CERT_PATH=/certs", "PATH=/tmp/pull/bin" + string(os.PathListSeparator) + "/usr/bin", "DOCKER_CONFIG=/tmp/pull"}
	if strings.Join(env, "\n") != strings.Join(want, "\n") {
		t.Errorf("pull environment:\n%s\nwant:\n%s", strings.Join(env, "\n"), strings.Join(want, "\n"))
	}
}
