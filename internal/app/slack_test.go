package app

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/credref"
	"github.com/urmzd/humagen/pkg/spec"
)

// The Slack app runs npm dependencies, so it is given what Node needs and
// its own settings, never humagen's database URL, sign-in secret or model keys.
func TestTheSlackAppIsGivenOnlyWhatItNeeds(t *testing.T) {
	process := map[string]string{
		"PATH": "/usr/bin", "HOME": "/home/humagen", "HTTPS_PROXY": "http://proxy:3128",
		"NODE_EXTRA_CA_CERTS": "/etc/ca.pem",
		// What the server keeps for itself.
		"HUMAGEN_POSTGRES_URL":       "postgres://humagen:pw@db/humagen",
		"HUMAGEN_OIDC_CLIENT_SECRET": "the-servers-own-secret",
		"ANTHROPIC_API_KEY":          "sk-ant-x",
		"AWS_SECRET_ACCESS_KEY":      "aws-x",
		"HUMAGEN_S3_SECRET_KEY":      "s3-x",
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
			HumagenClient: &spec.ClientCredentials{
				TokenURL: "https://login.example.com/token", ClientID: "humagen-slack",
				ClientSecret: &spec.Ref{Ref: "env:SLACK_CLIENT_SECRET"},
				Scopes:       []string{"humagen:runs:read", "humagen:runs:write"}, Audience: "humagen",
			},
		},
	}
	env, err := app.env(context.Background(), "http://127.0.0.1:8080", "https://humagen.example.com", lookup)
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
		"PATH": "/usr/bin", "HOME": "/home/humagen", "HTTPS_PROXY": "http://proxy:3128", "NODE_EXTRA_CA_CERTS": "/etc/ca.pem",
		"SLACK_BOT_TOKEN": "xoxb-1", "SLACK_APP_TOKEN": "xapp-1", "SLACK_TEAM_ID": "T1", "SLACK_OPERATORS": "U1,U2",
		"HUMAGEN_URL": "http://127.0.0.1:8080", "HUMAGEN_TENANT": "acme", "HUMAGEN_PUBLIC_URL": "https://humagen.example.com",
		"HUMAGEN_OIDC_TOKEN_URL": "https://login.example.com/token", "HUMAGEN_OIDC_CLIENT_ID": "humagen-slack",
		"HUMAGEN_OIDC_CLIENT_SECRET": "the-apps-secret", "HUMAGEN_OIDC_SCOPES": "humagen:runs:read humagen:runs:write",
		"HUMAGEN_OIDC_AUDIENCE": "humagen",
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
	app.choice.HumagenClient, app.choice.HumagenToken = nil, &spec.Ref{Ref: "env:API_TOKEN"}
	env, err = app.env(context.Background(), "http://127.0.0.1:8080", "", lookup)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(env, "HUMAGEN_TOKEN=tok-1") || slices.ContainsFunc(env, func(e string) bool { return strings.HasPrefix(e, "HUMAGEN_OIDC_") }) {
		t.Errorf("with humagen_token: %v", env)
	}
}

// The Slack app is a client of the server: one that cannot start (no
// apps/slack, no Node.js, no dependencies) is logged and left off, and the
// server goes on answering.
func TestASlackAppThatCannotStartLeavesTheServerUp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "humagen.yaml")
	if err := os.WriteFile(path, []byte(`api_version: humagen/v1
kind: System
spec:
  auth: {kind: dev}
  blobs: {kind: memory}
---
api_version: humagen/v1
kind: Platform
metadata: {name: default}
spec:
  surfaces:
    slack: {enabled: true, team: T1, bot_token: {ref: "env:TEST_SLACK_BOT"}, app_token: {ref: "env:TEST_SLACK_APP"}}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	// An apps/slack with nothing installed in it.
	t.Setenv("HUMAGEN_SLACK_APP_DIR", t.TempDir())
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
	path := filepath.Join(t.TempDir(), "humagen.yaml")
	if err := os.WriteFile(path, []byte(`api_version: humagen/v1
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
