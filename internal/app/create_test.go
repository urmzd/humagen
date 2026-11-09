package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func createGitHubArgs() []string {
	return []string{"github", "--app-id", "123", "--installation", "456", "--private-key", "op://vault/gh/key", "--webhook-secret", "env:HUMAGEN_GH_SECRET", "--repo", "acme/one", "--repo", "acme/two", "--repo", "other/three"}
}
func createSlackArgs() []string {
	return []string{"slack", "--app-id", "A123", "--team", "T123", "--bot-token", "env:HUMAGEN_SLACK_TOKEN", "--signing-secret", "env:HUMAGEN_SLACK_SECRET"}
}

func TestCreateAPI(t *testing.T) {
	tests := []struct {
		name                                       string
		args                                       []string
		list, method, path, body, response, output string
		// store is what GET /api/store/packages answers; empty is 404.
		store               string
		force, bearer, json bool
	}{
		{name: "github", args: createGitHubArgs(), body: `{"name":"github","surface":"github","identity":{"app_id":"123","installation":"456","owners":["acme","other"]},"default_agent":"code-reviewer","github":{"read_repositories":["acme/one","acme/two","other/three"]},"credentials":{"token":{"ref":"op://vault/gh/key"},"signing_secret":{"ref":"env:HUMAGEN_GH_SECRET"}}}`, response: `{"id":"gh-1","name":"github","surface":"github","default_agent":"code-reviewer"}`, output: "Created github client github (id gh-1), answered by @code-reviewer\nNext: install the Review package from the Store, then mention @code-reviewer on a pull request\n", store: reviewStore(false)},
		{name: "github with review", args: createGitHubArgs(), body: `{"name":"github","surface":"github","identity":{"app_id":"123","installation":"456","owners":["acme","other"]},"default_agent":"code-reviewer","github":{"read_repositories":["acme/one","acme/two","other/three"]},"credentials":{"token":{"ref":"op://vault/gh/key"},"signing_secret":{"ref":"env:HUMAGEN_GH_SECRET"}}}`, response: `{"id":"gh-1","name":"github","surface":"github","default_agent":"code-reviewer"}`, output: "Created github client github (id gh-1), answered by @code-reviewer\nNext: mention @code-reviewer on a pull request\n", store: reviewStore(true)},
		{name: "github without a store", args: createGitHubArgs(), body: `{"name":"github","surface":"github","identity":{"app_id":"123","installation":"456","owners":["acme","other"]},"default_agent":"code-reviewer","github":{"read_repositories":["acme/one","acme/two","other/three"]},"credentials":{"token":{"ref":"op://vault/gh/key"},"signing_secret":{"ref":"env:HUMAGEN_GH_SECRET"}}}`, response: `{"id":"gh-1","name":"github","surface":"github","default_agent":"code-reviewer"}`, output: "Created github client github (id gh-1), answered by @code-reviewer\nNext: mention @code-reviewer on a pull request\n"},
		{name: "slack default", args: createSlackArgs(), body: `{"name":"slack","surface":"slack","identity":{"app_id":"A123","team":"T123"},"default_agent":"","slack":{},"credentials":{"token":{"ref":"env:HUMAGEN_SLACK_TOKEN"},"signing_secret":{"ref":"env:HUMAGEN_SLACK_SECRET"}}}`, response: `{"id":"sl-1","name":"slack","surface":"slack"}`, output: "Created slack client slack (id sl-1), answered by the workspace default\nNext: mention the app in Slack\n"},
		{name: "slack explicit", args: append(createSlackArgs(), "--signing-secret", "keychain:humagen/slack", "--agent", "helper", "--name", "acme"), body: `{"name":"acme","surface":"slack","identity":{"app_id":"A123","team":"T123"},"default_agent":"helper","slack":{},"credentials":{"token":{"ref":"env:HUMAGEN_SLACK_TOKEN"},"signing_secret":{"ref":"keychain:humagen/slack"}}}`, response: `{"id":"sl-2","name":"acme","surface":"slack","default_agent":"helper"}`, output: "Created slack client acme (id sl-2), answered by @helper\nNext: mention @helper in Slack\n", bearer: true},
		{name: "duplicate", args: createSlackArgs(), list: `{"clients":[{"id":"sl-1","name":"slack","surface":"slack"}]}`, output: "client slack exists; pass --force to update it"},
		{name: "force", args: createSlackArgs(), list: `{"clients":[{"id":"sl-1","name":"slack","surface":"slack"}]}`, method: http.MethodPut, path: "/api/clients/sl-1", body: `{"name":"slack","surface":"slack","identity":{"app_id":"A123","team":"T123"},"default_agent":"","slack":{},"credentials":{"token":{"ref":"env:HUMAGEN_SLACK_TOKEN"},"signing_secret":{"ref":"env:HUMAGEN_SLACK_SECRET"}}}`, response: `{"id":"sl-1","name":"slack","surface":"slack","extra":"preserved"}`, force: true, json: true},
		{name: "same name other surface", args: createSlackArgs(), list: `{"clients":[{"id":"gh-1","name":"slack","surface":"github"}]}`, body: `{"name":"slack","surface":"slack","identity":{"app_id":"A123","team":"T123"},"default_agent":"","slack":{},"credentials":{"token":{"ref":"env:HUMAGEN_SLACK_TOKEN"},"signing_secret":{"ref":"env:HUMAGEN_SLACK_SECRET"}}}`, response: `{"id":"sl-1","name":"slack","surface":"slack"}`, json: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("HUMAGEN_TOKEN", "")
			if tt.bearer {
				t.Setenv("HUMAGEN_TOKEN", "api-test-token")
			}
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet && r.URL.Path == "/api/store/packages" {
					if tt.store == "" {
						http.NotFound(w, r)
						return
					}
					w.Header().Set("Content-Type", "application/json")
					fmt.Fprint(w, tt.store)
					return
				}
				calls++
				wantAuth := ""
				if tt.bearer {
					wantAuth = "Bearer api-test-token"
				}
				if r.Header.Get("Authorization") != wantAuth {
					t.Errorf("authorization = %q", r.Header.Get("Authorization"))
				}
				w.Header().Set("Content-Type", "application/json")
				if calls == 1 {
					if r.Method != http.MethodGet || r.URL.Path != "/api/clients" {
						t.Errorf("list request = %s %s", r.Method, r.URL.Path)
					}
					list := tt.list
					if list == "" {
						list = `{"clients":[]}`
					}
					fmt.Fprint(w, list)
					return
				}
				method, path := tt.method, tt.path
				if method == "" {
					method = http.MethodPost
				}
				if path == "" {
					path = "/api/clients"
				}
				if r.Method != method || r.URL.Path != path {
					t.Errorf("write request = %s %s; want %s %s", r.Method, r.URL.Path, method, path)
				}
				if r.Header.Get("Content-Type") != "application/json" {
					t.Error("missing JSON content type")
				}
				var got, want any
				if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
					t.Error(err)
				}
				if err := json.Unmarshal([]byte(tt.body), &want); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(got, want) {
					t.Errorf("body = %#v; want %#v", got, want)
				}
				fmt.Fprint(w, tt.response)
			}))
			defer server.Close()
			// HUMAGEN_URL also verifies that the environment default is used.
			t.Setenv("HUMAGEN_URL", server.URL)
			args := append([]string{}, tt.args...)
			if tt.force {
				args = append(args, "--force")
			}
			if tt.json {
				args = append(args, "--json")
			}
			var out bytes.Buffer
			err := createCmd(args, &out)
			if tt.body == "" {
				if err == nil || err.Error() != tt.output {
					t.Fatalf("error = %v", err)
				}
				if calls != 1 {
					t.Fatalf("duplicate caused %d requests", calls)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if calls != 2 {
				t.Errorf("requests = %d", calls)
			}
			wantOut := tt.output
			if tt.json {
				wantOut = tt.response + "\n"
			}
			if out.String() != wantOut {
				t.Errorf("output = %q; want %q", out.String(), wantOut)
			}
		})
	}
}

func TestCreateFlags(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"app id", []string{"github"}, "--app-id is required"},
		{"installation", []string{"github", "--app-id", "123"}, "--installation is required"},
		{"private key", []string{"github", "--app-id", "123", "--installation", "456"}, "--private-key is required"},
		{"webhook secret", []string{"github", "--app-id", "123", "--installation", "456", "--private-key", "env:KEY"}, "--webhook-secret is required"},
		{"owners", []string{"github", "--app-id", "123", "--installation", "456", "--private-key", "env:KEY", "--webhook-secret", "env:SECRET"}, "--owner is required"},
		{"team", []string{"slack"}, "--team is required"},
		{"bot token", []string{"slack", "--team", "T123"}, "--bot-token is required"},
		{"signing secret", []string{"slack", "--team", "T123", "--bot-token", "env:HUMAGEN_SLACK_TOKEN"}, "--signing-secret is required"},
		{"raw private key", append(createGitHubArgs(), "--private-key", "raw-secret"), "--private-key must be a credential reference"},
		{"raw webhook secret", append(createGitHubArgs(), "--webhook-secret", "raw-secret"), "--webhook-secret must be a credential reference"},
		{"raw bot token", append(createSlackArgs(), "--bot-token", "raw-secret"), "--bot-token must be a credential reference"},
		{"raw signing secret", append(createSlackArgs(), "--signing-secret", "raw-secret"), "--signing-secret must be a credential reference"},
		{"raw API token", append(createSlackArgs(), "--token", "raw-secret"), "--token must be a credential reference"},
		{"bad repo", append(createGitHubArgs(), "--repo", "owner"), "--repo must be owner/name"},
		{"bad id", append(createGitHubArgs(), "--app-id", "zero"), "--app-id must contain 1 to 20 digits"},
		{"bad server", append(createSlackArgs(), "--server", "file:///tmp"), "--server must be"},
		{"unknown kind", []string{"email"}, "unknown create kind"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := createCmd(tt.args, io.Discard)
			if err == nil || !strings.HasPrefix(err.Error(), tt.want) {
				t.Fatalf("error = %v; want %s", err, tt.want)
			}
			if strings.Contains(err.Error(), "raw-secret") {
				t.Fatal("error disclosed credential")
			}
		})
	}
	c, err := parseCreate(append(createGitHubArgs(), "--owner", "explicit", "--owner", "second"), io.Discard)
	if err != nil || !reflect.DeepEqual(c.owners, []string{"explicit", "second"}) {
		t.Fatalf("owners = %v; error %v", c.owners, err)
	}
}

func TestCreateBearerAndServerFlag(t *testing.T) {
	t.Setenv("HUMAGEN_URL", "http://unused.invalid")
	t.Setenv("CREATE_API_TOKEN", "custom-token")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer custom-token" {
			t.Error("missing explicit bearer token")
		}
		if r.Method == http.MethodGet {
			fmt.Fprint(w, `{"clients":[]}`)
		} else {
			fmt.Fprint(w, `{"id":"id","name":"slack","surface":"slack"}`)
		}
	}))
	defer server.Close()
	if err := createCmd(append(createSlackArgs(), "--server", server.URL, "--token", "env:CREATE_API_TOKEN"), io.Discard); err != nil {
		t.Fatal(err)
	}
}

func TestCreateAPIFailures(t *testing.T) {
	for _, tt := range []struct {
		name, body, want string
		status           int
	}{
		{"unauthorized", `{"error":"unauthorized"}`, "HTTP 401", 401},
		{"malformed", `not JSON`, "invalid JSON", 200},
		{"oversized", strings.Repeat("x", (1<<20)+1), "exceeds 1 MiB", 200},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tt.status); fmt.Fprint(w, tt.body) }))
			defer server.Close()
			err := createCmd(append(createSlackArgs(), "--server", server.URL, "--token", ""), io.Discard)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestCreateHelp(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"github", "--help"}, {"slack", "--help"}} {
		var out bytes.Buffer
		if err := createCmd(args, &out); !errors.Is(err, flag.ErrHelp) {
			t.Fatalf("%v: %v", args, err)
		}
		if !strings.Contains(out.String(), "github") && !strings.Contains(out.String(), "slack") {
			t.Fatalf("missing kinds: %s", &out)
		}
		if args[0] == "slack" && !strings.Contains(out.String(), "Slack signing secret reference (required)") {
			t.Fatalf("missing signing-secret requirement: %s", &out)
		}
		if strings.Contains(out.String(), "app-token") || strings.Contains(out.String(), "-o ") {
			t.Fatalf("obsolete flags: %s", &out)
		}
	}
}

func TestCreateIdentityValidationBeforeRequests(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		http.Error(w, "unexpected request", 500)
	}))
	defer server.Close()
	for _, tt := range []struct {
		name string
		args []string
		want string
	}{
		{"short team", append(createSlackArgs(), "--team", "T1"), "--team must match"},
		{"lowercase team", append(createSlackArgs(), "--team", "t123"), "--team must match"},
		{"long team", append(createSlackArgs(), "--team", strings.Repeat("T", 33)), "--team must match"},
		{"short Slack app", append(createSlackArgs(), "--app-id", "A1"), "--app-id must match"},
		{"wrong Slack prefix", append(createSlackArgs(), "--app-id", "B123"), "--app-id must match"},
		{"lowercase Slack app", append(createSlackArgs(), "--app-id", "Aabc"), "--app-id must match"},
		{"long Slack app", append(createSlackArgs(), "--app-id", strings.Repeat("A", 33)), "--app-id must match"},
		{"signed GitHub app", append(createGitHubArgs(), "--app-id", "+123"), "--app-id must contain"},
		{"long GitHub app", append(createGitHubArgs(), "--app-id", strings.Repeat("1", 21)), "--app-id must contain"},
		{"GitHub installation", append(createGitHubArgs(), "--installation", "abc"), "--installation must contain"},
		{"signed GitHub installation", append(createGitHubArgs(), "--installation", "-456"), "--installation must contain"},
		{"long GitHub installation", append(createGitHubArgs(), "--installation", strings.Repeat("1", 21)), "--installation must contain"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := createCmd(append(tt.args, "--server", server.URL, "--token", ""), io.Discard)
			if err == nil || !strings.HasPrefix(err.Error(), tt.want) {
				t.Fatalf("error = %v; want %s", err, tt.want)
			}
		})
	}
	if calls != 0 {
		t.Fatalf("invalid identities sent %d requests", calls)
	}
	for _, args := range [][]string{
		append(createSlackArgs(), "--team", "T12", "--app-id", "A12"),
		append(createSlackArgs(), "--team", strings.Repeat("T", 32), "--app-id", strings.Repeat("A", 32)),
		append(createSlackArgs(), "--app-id", ""),
		append(createGitHubArgs(), "--app-id", "18446744073709551615", "--installation", strings.Repeat("9", 20)),
	} {
		if _, err := parseCreate(args, io.Discard); err != nil {
			t.Errorf("valid identity rejected: %v", err)
		}
	}
}

func TestCreateSecretReferenceHint(t *testing.T) {
	const refusal = `{"error":"that secret reference cannot be used by this workspace"}`
	for _, tt := range []struct {
		name, tenant string
		args         []string
		want         string
	}{
		{"default", "default", append(createGitHubArgs(), "--private-key", "env:HUMAGEN_GITHUB_KEY", "--webhook-secret", "env:HUMAGEN_GITHUB_WEBHOOK_SECRET"), `the server does not let this workspace read HUMAGEN_GITHUB_KEY, HUMAGEN_GITHUB_WEBHOOK_SECRET; start it with -secret-refs "default=HUMAGEN_GITHUB_KEY,HUMAGEN_GITHUB_WEBHOOK_SECRET" (or set spec.secret_env in the System document)`},
		{"authenticated tenant", "acme", createSlackArgs(), `-secret-refs "acme=HUMAGEN_SLACK_TOKEN,HUMAGEN_SLACK_SECRET"`},
		{"unknown tenant", "", createSlackArgs(), `-secret-refs "<tenant>=HUMAGEN_SLACK_TOKEN,HUMAGEN_SLACK_SECRET"`},
		{"no env references", "default", append(createGitHubArgs(), "--webhook-secret", "keychain:humagen/secret"), "use env:HUMAGEN_* references granted to this workspace"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/api/clients":
					fmt.Fprint(w, `{"clients":[]}`)
				case r.Method == http.MethodGet && r.URL.Path == "/api/me":
					if tt.tenant == "" {
						http.Error(w, "unavailable", http.StatusServiceUnavailable)
					} else {
						_ = json.NewEncoder(w).Encode(map[string]any{"principal": map[string]string{"tenant": tt.tenant}})
					}
				case r.Method == http.MethodPost && r.URL.Path == "/api/clients":
					w.WriteHeader(http.StatusBadRequest)
					fmt.Fprint(w, refusal)
				default:
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				}
			}))
			defer server.Close()
			err := createCmd(append(tt.args, "--server", server.URL, "--token", ""), io.Discard)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v; want %s", err, tt.want)
			}
			if strings.ContainsAny(err.Error(), "\r\n") {
				t.Fatalf("multiline error: %v", err)
			}
			if calls != 3 {
				t.Errorf("requests = %d", calls)
			}
		})
	}
}

func TestCreateOtherBadRequestIsPreserved(t *testing.T) {
	for _, tt := range []struct {
		status int
		body   string
	}{
		{400, `{"error":"some other invalid client field"}`},
		{403, `{"error":"that secret reference cannot be used by this workspace"}`},
	} {
		t.Run(fmt.Sprint(tt.status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet && r.URL.Path == "/api/clients" {
					fmt.Fprint(w, `{"clients":[]}`)
					return
				}
				if r.URL.Path == "/api/me" {
					t.Error("unexpected principal lookup")
				}
				w.WriteHeader(tt.status)
				fmt.Fprint(w, tt.body)
			}))
			defer server.Close()
			err := createCmd(append(createSlackArgs(), "--server", server.URL, "--token", ""), io.Discard)
			want := fmt.Sprintf("POST /api/clients: HTTP %d: %s", tt.status, tt.body)
			if err == nil || err.Error() != want {
				t.Fatalf("error = %v; want %s", err, want)
			}
		})
	}
}

// reviewStore is the store with the Review package, installed or not.
func reviewStore(installed bool) string {
	return fmt.Sprintf(`{"packages":[{"name":"humagen/review","title":"Review","installed":%v,"agents":[{"name":"code-reviewer","installed":%v,"taken":false}]}]}`, installed, installed)
}
