package app

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/spec"
)

func testPEM(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
}

// noFollow is a browser that stops at each redirect, so a test reads where it was sent.
var noFollow = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

func browse(t *testing.T, target string) (*http.Response, string) {
	t.Helper()
	resp, err := noFollow.Get(target)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

func TestInstallGitHubRegistersInstallsAndConnects(t *testing.T) {
	key := testPEM(t)
	var github *httptest.Server
	github = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/app-manifests/code-1/conversions":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 4242, "slug": "humagen-acme", "html_url": github.URL + "/apps/humagen-acme", "pem": key, "webhook_secret": "hook-value"})
		case r.Method == http.MethodGet && r.URL.Path == "/app/installations/77" && strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ey"):
			_, _ = io.WriteString(w, `{"account":{"login":"acme"}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer github.Close()

	var posted map[string]any
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /api/clients":
			_, _ = io.WriteString(w, `{"clients":[]}`)
		case "POST /api/clients":
			_ = json.NewDecoder(r.Body).Decode(&posted)
			_, _ = io.WriteString(w, `{"id":"gh-1","name":"github","surface":"github","default_agent":"code-reviewer"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer api.Close()

	var out bytes.Buffer
	var manifest map[string]any
	i := installer{
		in: bufio.NewReader(strings.NewReader("")), out: &out, http: github.Client(),
		githubWeb: github.URL, githubAPI: github.URL, wait: 20 * time.Second,
		// The person's part: the form goes to GitHub, GitHub sends them back twice.
		open: func(start string) error {
			go func() {
				_, form := browse(t, start)
				action := regexp.MustCompile(`action="([^"]+)"`).FindStringSubmatch(form)
				value := regexp.MustCompile(`name="manifest" value="([^"]*)"`).FindStringSubmatch(form)
				if action == nil || value == nil {
					t.Errorf("no form in %q", form)
					return
				}
				to, _ := url.Parse(html.UnescapeString(action[1]))
				_ = json.Unmarshal([]byte(html.UnescapeString(value[1])), &manifest)
				base := strings.TrimSuffix(start, "/")
				// A callback with another state is refused and uses nothing up.
				if resp, _ := browse(t, base+"/callback?code=code-1&state=wrong"); resp.StatusCode != http.StatusBadRequest {
					t.Errorf("wrong state: HTTP %d", resp.StatusCode)
				}
				resp, _ := browse(t, base+"/callback?code=code-1&state="+to.Query().Get("state"))
				if got := resp.Header.Get("Location"); got != github.URL+"/apps/humagen-acme/installations/new" {
					t.Errorf("after registering, sent to %q", got)
				}
				// An installation GitHub does not know is not taken from the address bar.
				if resp, _ := browse(t, base+"/installed?installation_id=78"); resp.StatusCode != http.StatusBadRequest {
					t.Errorf("unknown installation: HTTP %d", resp.StatusCode)
				}
				if _, page := browse(t, base+"/installed?installation_id=77"); strings.Contains(page, "hook-value") || strings.Contains(page, "PRIVATE KEY") {
					t.Error("the browser was shown a credential")
				}
			}()
			return nil
		},
	}
	err := i.github([]string{"--server", api.URL, "--token", "", "--webhook-url", "https://hooks.example/connectors/github", "--app-name", "Humagen Acme", "--repo", "acme/one"})
	if err != nil {
		t.Fatal(err)
	}
	if manifest["name"] != "Humagen Acme" || manifest["hook_attributes"].(map[string]any)["url"] != "https://hooks.example/connectors/github" || !strings.HasSuffix(manifest["redirect_url"].(string), "/callback") || !strings.HasSuffix(manifest["setup_url"].(string), "/installed") {
		t.Fatalf("manifest = %v", manifest)
	}
	want := map[string]any{
		"name": "github", "surface": "github", "default_agent": "code-reviewer",
		"identity":    map[string]any{"app_id": "4242", "installation": "77", "owners": []any{"acme"}},
		"github":      map[string]any{"read_repositories": []any{"acme/one"}, "login": "humagen-acme[bot]"},
		"credentials": map[string]any{"token": map[string]any{"value": key}, "signing_secret": map[string]any{"value": "hook-value"}},
	}
	a, _ := json.Marshal(posted)
	b, _ := json.Marshal(want)
	if string(a) != string(b) {
		t.Fatalf("posted\n%s\nwant\n%s", a, b)
	}
	if got := out.String(); !strings.Contains(got, "Connected GitHub App") || !strings.Contains(got, "mention @humagen-acme on a pull request in acme") || strings.Contains(got, "hook-value") || strings.Contains(got, "PRIVATE KEY") {
		t.Fatalf("output = %q", got)
	}
}

func TestInstallGitHubAsksTheServerBeforeGitHub(t *testing.T) {
	opened := false
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"clients":[{"id":"gh-1","name":"github","surface":"github"}]}`)
	}))
	defer api.Close()
	i := installer{in: bufio.NewReader(strings.NewReader("")), out: io.Discard, http: http.DefaultClient, wait: time.Second, open: func(string) error { opened = true; return nil }}
	err := i.github([]string{"--server", api.URL, "--token", "", "--webhook-url", "https://hooks.example/connectors/github"})
	if err == nil || !strings.Contains(err.Error(), "exists") || opened {
		t.Fatalf("err = %v, opened = %v", err, opened)
	}
	for _, args := range [][]string{{}, {"--webhook-url", "http://plain.example/x"}, {"--webhook-url", "https://h.example/x", "--org", "not a login"}, {"--webhook-url", "https://h.example/x", "--repo", "norepo"}} {
		if err := i.github(args); err == nil || opened {
			t.Fatalf("%v: err = %v, opened = %v", args, err, opened)
		}
	}
}

func TestInstallSlackKeepsTokensAndWritesWhereTheyAre(t *testing.T) {
	slack := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path + " " + r.Header.Get("Authorization") {
		case "/auth.test Bearer xoxb-1234567890-abc":
			_, _ = io.WriteString(w, `{"ok":true,"team":"Acme","team_id":"T0ACME"}`)
		case "/apps.connections.open Bearer xapp-1-A1-1234567890":
			_, _ = io.WriteString(w, `{"ok":true,"url":"wss://x"}`)
		default:
			_, _ = io.WriteString(w, `{"ok":false,"error":"invalid_auth"}`)
		}
	}))
	defer slack.Close()

	path := filepath.Join(t.TempDir(), "humagen.yaml")
	kept := map[string]string{}
	var opened string
	answers := []string{"xoxb-1234567890-abc", "xapp-1-A1-1234567890"}
	var out bytes.Buffer
	i := installer{
		in: bufio.NewReader(strings.NewReader("")), out: &out, http: slack.Client(),
		slackWeb: "https://api.slack.example", slackAPI: slack.URL, wait: 10 * time.Second,
		open: func(u string) error { opened = u; return nil },
		keep: func(_ context.Context, service, account, value string) (string, error) {
			kept[service+"/"+account] = value
			return "keychain:" + service + "/" + account, nil
		},
		secret: func(string) (string, error) { v := answers[0]; answers = answers[1:]; return v, nil },
	}
	if err := i.slack(Options{}, []string{"-o", path, "--name", "Acme Bot"}); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(opened)
	if err != nil || u.Query().Get("new_app") != "1" {
		t.Fatalf("opened %q", opened)
	}
	var manifest SlackManifest
	if err := json.Unmarshal([]byte(u.Query().Get("manifest_json")), &manifest); err != nil || manifest.DisplayInformation.Name != "Acme Bot" || !manifest.Settings.SocketMode {
		t.Fatalf("manifest in link: %v %+v", err, manifest.DisplayInformation)
	}
	if kept["humagen/slack-bot-T0ACME"] != "xoxb-1234567890-abc" || kept["humagen/slack-app-T0ACME"] != "xapp-1-A1-1234567890" {
		t.Fatalf("kept = %v", kept)
	}
	cfg, err := spec.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	sl := cfg.Platforms[0].Spec.Surfaces.Slack
	if sl == nil || !sl.Enabled || sl.Team != "T0ACME" || sl.AppName != "Acme Bot" || sl.BotToken.Ref != "keychain:humagen/slack-bot-T0ACME" || sl.AppToken.Ref != "keychain:humagen/slack-app-T0ACME" {
		t.Fatalf("slack = %+v", sl)
	}
	written, _ := os.ReadFile(path)
	for _, where := range []string{string(written), out.String()} {
		if strings.Contains(where, "xoxb-") && strings.Contains(where, "1234567890") || strings.Contains(where, "xapp-1") {
			t.Fatalf("a token was written or printed: %q", where)
		}
	}
	if !strings.Contains(out.String(), "humagen start") {
		t.Fatalf("output = %q", out.String())
	}
}

func TestInstallSlackRefusesWhatSlackRefuses(t *testing.T) {
	slack := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"ok":false,"error":"invalid_auth"}`)
	}))
	defer slack.Close()
	path := filepath.Join(t.TempDir(), "humagen.yaml")
	for _, token := range []string{"not-a-token", "xoxb-1234567890-abc"} {
		i := installer{
			in: bufio.NewReader(strings.NewReader("")), out: io.Discard, http: slack.Client(), slackWeb: "https://api.slack.example", slackAPI: slack.URL, wait: 5 * time.Second,
			open: func(string) error { return errors.New("no browser") },
			keep: func(context.Context, string, string, string) (string, error) {
				t.Fatal("kept a refused token")
				return "", nil
			},
			secret: func(string) (string, error) { return token, nil },
		}
		if err := i.slack(Options{}, []string{"-o", path}); err == nil {
			t.Fatalf("%s accepted", token)
		}
		if _, err := os.Stat(path); err == nil {
			t.Fatal("a spec was written for a refused token")
		}
	}
}
