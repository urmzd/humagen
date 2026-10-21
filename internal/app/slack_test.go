package app

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/warp/pkg/credref"
	"github.com/urmzd/warp/pkg/spec"
)

// The Slack app runs npm dependencies, so it is given what Node needs and
// its own settings, never warp's database URL, sign-in secret or model keys.
func TestTheSlackAppIsGivenOnlyWhatItNeeds(t *testing.T) {
	process := map[string]string{
		"PATH": "/usr/bin", "HOME": "/home/warp", "HTTPS_PROXY": "http://proxy:3128",
		"NODE_EXTRA_CA_CERTS": "/etc/ca.pem",
		// What the server keeps for itself.
		"WARP_POSTGRES_URL":       "postgres://warp:pw@db/warp",
		"WARP_OIDC_CLIENT_SECRET": "the-servers-own-secret",
		"ANTHROPIC_API_KEY":       "sk-ant-x",
		"AWS_SECRET_ACCESS_KEY":   "aws-x",
		"WARP_S3_SECRET_KEY":      "s3-x",
		// Where the choices say the app's own credentials are kept.
		"BOT": "xoxb-1", "APP": "xapp-1", "SLACK_CLIENT_SECRET": "the-apps-secret", "API_TOKEN": "tok-1",
	}
	lookup := func(k string) (string, bool) { v, ok := process[k]; return v, ok }
	app := &slackApp{
		tenant: "acme",
		keys:   credref.Resolver{LookupEnv: lookup},
		choice: spec.Slack{
			Enabled: true, Team: "T1", Operators: []string{"U1", "U2"},
			BotToken: &spec.Ref{Ref: "env:BOT"}, AppToken: &spec.Ref{Ref: "env:APP"},
			WarpClient: &spec.ClientCredentials{
				TokenURL: "https://login.example.com/token", ClientID: "warp-slack",
				ClientSecret: &spec.Ref{Ref: "env:SLACK_CLIENT_SECRET"},
				Scopes:       []string{"warp:runs:read", "warp:runs:write"}, Audience: "warp",
			},
		},
	}
	env, err := app.env(context.Background(), "http://127.0.0.1:8080", "https://warp.example.com", lookup)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		if _, twice := got[k]; twice {
			t.Errorf("%s is set twice", k)
		}
		got[k] = v
	}
	want := map[string]string{
		"PATH": "/usr/bin", "HOME": "/home/warp", "HTTPS_PROXY": "http://proxy:3128", "NODE_EXTRA_CA_CERTS": "/etc/ca.pem",
		"SLACK_BOT_TOKEN": "xoxb-1", "SLACK_APP_TOKEN": "xapp-1", "SLACK_TEAM_ID": "T1", "SLACK_OPERATORS": "U1,U2",
		"WARP_URL": "http://127.0.0.1:8080", "WARP_TENANT": "acme", "WARP_PUBLIC_URL": "https://warp.example.com",
		"WARP_OIDC_TOKEN_URL": "https://login.example.com/token", "WARP_OIDC_CLIENT_ID": "warp-slack",
		"WARP_OIDC_CLIENT_SECRET": "the-apps-secret", "WARP_OIDC_SCOPES": "warp:runs:read warp:runs:write",
		"WARP_OIDC_AUDIENCE": "warp",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
	for k := range got {
		if _, ok := want[k]; !ok {
			t.Errorf("%s reached the Slack app", k)
		}
	}

	// A token is given as it is.
	app.choice.WarpClient, app.choice.WarpToken = nil, &spec.Ref{Ref: "env:API_TOKEN"}
	env, err = app.env(context.Background(), "http://127.0.0.1:8080", "", lookup)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(env, "WARP_TOKEN=tok-1") || slices.ContainsFunc(env, func(e string) bool { return strings.HasPrefix(e, "WARP_OIDC_") }) {
		t.Errorf("with warp_token: %v", env)
	}
}

// The Slack app is a client of the server: one that cannot start (no
// apps/slack, no Node.js, no dependencies) is logged and left off, and the
// server goes on answering.
func TestASlackAppThatCannotStartLeavesTheServerUp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "warp.yaml")
	if err := os.WriteFile(path, []byte(`api_version: warp/v1
kind: System
spec:
  auth: {kind: dev}
  blobs: {kind: memory}
---
api_version: warp/v1
kind: Platform
metadata: {name: default}
spec:
  surfaces:
    slack: {enabled: true, team: T1, bot_token: {ref: "env:TEST_SLACK_BOT"}, app_token: {ref: "env:TEST_SLACK_APP"}}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	// An apps/slack with nothing installed in it.
	t.Setenv("WARP_SLACK_APP_DIR", t.TempDir())
	stopped := make(chan error, 1)
	o := Options{slackDone: func(_ string, err error) { stopped <- err }}
	addr := serveFor(t, o, "-f", path, "-addr", "127.0.0.1:0")
	select {
	case err := <-stopped:
		if err == nil {
			t.Fatal("the Slack app's supervisor returned no reason")
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the Slack app neither started nor gave up")
	}
	// serveFor fails the test if serve returned; the API still answers.
	get(t, "http://"+addr+"/api/agents")
}

// A flag can choose a sign-in the file's Slack surface has no credential
// for; serve refuses that before it listens, as a check refuses the file.
func TestServeRefusesASlackSurfaceWithNoCredentialForItsSignIn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "warp.yaml")
	if err := os.WriteFile(path, []byte(`api_version: warp/v1
kind: Platform
metadata: {name: default}
spec:
  surfaces:
    slack: {enabled: true, team: T1, bot_token: {ref: "env:B"}, app_token: {ref: "env:A"}}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(envWebDir, t.TempDir())
	for _, args := range [][]string{
		{"serve", "-f", path, "-addr", "127.0.0.1:0"},
		{"start", "-f", path, "-addr", "127.0.0.1:0", "-auth", "mock"},
	} {
		err := run(Options{}, args)
		if err == nil || !strings.Contains(err.Error(), "surfaces.slack") {
			t.Errorf("%v: err = %v", args, err)
		}
	}
}
