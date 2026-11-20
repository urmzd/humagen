package app

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/httpapi"
	"github.com/urmzd/humagen/pkg/auth"
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
	// Each of the app's own settings also under its old name, for an app
	// directory from before the rename, which reads only those; the new app
	// is told they are copies and notes nothing.
	for k, v := range maps.Clone(want) {
		if rest, ok := strings.CutPrefix(k, "HUMAGEN_"); ok {
			want["WARP_"+rest] = v
		}
	}
	want["HUMAGEN_OLD_NAMES_COPIED"] = "1"
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

	// Code channels are off unless the surface turns them on.
	app.choice.CodeChannels = true
	env, err = app.env(context.Background(), "http://127.0.0.1:8080", "", lookup)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(env, "SLACK_CODE_CHANNELS=1") {
		t.Errorf("with code_channels: %v", env)
	}
	app.choice.CodeChannels = false

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

// Under tailscale sign-in no process can be given a credential from outside,
// so the server makes one for the Slack app it starts: the app's environment
// carries it, the API takes it as the app's machine and nothing else, and it
// is in no log line and no error.
func TestTheSlackAppTheServerStartsSignsInUnderTailscale(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in for node is a shell script")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "humagen.yaml")
	if err := os.WriteFile(path, []byte(`api_version: humagen/v1
kind: System
spec:
  auth: {kind: tailscale}
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
	t.Setenv("TEST_SLACK_BOT", "xoxb-1")
	t.Setenv("TEST_SLACK_APP", "xapp-1")
	t.Setenv(envWebDir, t.TempDir())
	// An apps/slack whose node writes down what it was given and waits.
	appDir, bin := t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(appDir, "node_modules"), 0o700); err != nil {
		t.Fatal(err)
	}
	node := "#!/bin/sh\nprintf '%s\\n%s' \"$HUMAGEN_URL\" \"$HUMAGEN_TOKEN\" > given.tmp && mv given.tmp given\nexec sleep 600\n"
	if err := os.WriteFile(filepath.Join(bin, "node"), []byte(node), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HUMAGEN_SLACK_APP_DIR", appDir)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	// The server and the app both write to stderr, so that is what is read.
	logs, err := os.Create(filepath.Join(dir, "stderr"))
	if err != nil {
		t.Fatal(err)
	}
	stderr := os.Stderr
	os.Stderr = logs
	// The app's goroutine starts its child with this file, so the file is
	// closed only once that goroutine has ended. This cleanup runs after the
	// one that stops the server.
	appDone := make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-appDone:
		case <-time.After(30 * time.Second):
			t.Error("the Slack app's goroutine did not end with the server")
		}
		os.Stderr = stderr
		_ = logs.Close()
	})

	addr := serveFor(t, Options{slackDone: func(string, error) { close(appDone) }}, "-f", path, "-addr", "127.0.0.1:0", "-public-url", "https://humagen.example.ts.net")
	var url, token string
	for deadline := time.Now().Add(30 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		if b, err := os.ReadFile(filepath.Join(appDir, "given")); err == nil {
			url, token, _ = strings.Cut(string(b), "\n")
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the Slack app was not started")
		}
	}
	if url != "http://"+addr || len(token) < 43 {
		t.Fatalf("the app was given the address %q and a token of %d characters", url, len(token))
	}

	call := func(path, bearer string, header ...string) (int, string) {
		t.Helper()
		req, err := http.NewRequest("GET", url+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		for i := 0; i+1 < len(header); i += 2 {
			req.Header.Set(header[i], header[i+1])
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}
	status, body := call("/api/me", token)
	if status != 200 {
		t.Fatalf("the app's own token: %d %s", status, body)
	}
	var me struct {
		Principal   auth.Principal    `json:"principal"`
		Permissions []auth.Permission `json:"permissions"`
	}
	if err := json.Unmarshal([]byte(body), &me); err != nil {
		t.Fatal(err)
	}
	if who := me.Principal; who.Kind != auth.Machine || who.Tenant != "default" || who.Subject != slackMachineSubject("T1") || len(who.Roles) != 0 {
		t.Errorf("the app is %+v", who)
	}
	if !slices.Equal(slices.Sorted(slices.Values(me.Permissions)), slices.Sorted(slices.Values(slackMachineScopes))) {
		t.Errorf("the app may %v, want exactly %v", me.Permissions, slackMachineScopes)
	}
	if status, _ := call("/api/agents", token); status != 200 {
		t.Errorf("the app reading agents: %d", status)
	}
	for name, got := range map[string]int{
		"no credential":         first(call("/api/me", "")),
		"a wrong token":         first(call("/api/me", strings.Repeat("A", len(token)))),
		"a token and a login":   first(call("/api/me", token, "Tailscale-User-Login", "alice@example.com")),
		"a token and a name":    first(call("/api/me", token, "Tailscale-User-Name", "Alice")),
		"an admin's permission": first(call("/api/audit", token)),
	} {
		want := 401
		if name == "an admin's permission" {
			want = 403
		}
		if got != want {
			t.Errorf("%s: %d, want %d", name, got, want)
		}
	}

	if err := logs.Sync(); err != nil {
		t.Fatal(err)
	}
	said, err := os.ReadFile(logs.Name())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(said), "slack app starting") {
		t.Fatalf("the log was not read: %s", said)
	}
	if strings.Contains(string(said), token) {
		t.Error("the token is in the log")
	}
}

func first(status int, _ string) int { return status }

// The credential is the server's to make: only under tailscale, one per app,
// each its own, and an app that cannot start names it in no error.
func TestTheServerMintsOneCredentialForEachSlackApp(t *testing.T) {
	lookup := func(k string) (string, bool) {
		v, ok := map[string]string{"BOT": "xoxb-1", "APP": "xapp-1"}[k]
		return v, ok
	}
	apps := func() []*slackApp {
		var out []*slackApp
		for tenant, team := range map[string]string{"acme": "T1", "other": "T2"} {
			out = append(out, &slackApp{tenant: tenant, keys: credref.Resolver{LookupEnv: lookup},
				choice: spec.Slack{Enabled: true, Team: team, BotToken: &spec.Ref{Ref: "env:BOT"}, AppToken: &spec.Ref{Ref: "env:APP"}}})
		}
		return out
	}
	authn, err := httpapi.NewTailscaleAuthn("127.0.0.1:8080", "https://humagen.example.ts.net", "acme", auth.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	dev := auth.Principal{Subject: "dev", Tenant: "acme", Kind: auth.User}
	for _, mode := range []string{"dev", "oidc", "mock"} {
		none := apps()
		if err := mintSlackCredentials(mode, &httpapi.Authn{Dev: &dev}, none); err != nil || none[0].token != "" || none[1].token != "" {
			t.Errorf("%s: minted a credential (%v)", mode, err)
		}
	}
	two := apps()
	if err := mintSlackCredentials("tailscale", authn, two); err != nil {
		t.Fatal(err)
	}
	if two[0].token == "" || two[0].token == two[1].token {
		t.Fatal("the apps do not each have a token of their own")
	}
	for _, app := range two {
		env, err := app.env(context.Background(), "http://127.0.0.1:8080", "https://humagen.example.ts.net", lookup)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Contains(env, "HUMAGEN_TOKEN="+app.token) || !slices.Contains(env, "WARP_TOKEN="+app.token) {
			t.Errorf("%s: the app's environment has no token", app.tenant)
		}
		r := httptest.NewRequest("GET", "http://127.0.0.1:8080/api/me", nil)
		r.RemoteAddr = "127.0.0.1:2345"
		r.Header.Set("Authorization", "Bearer "+app.token)
		who, err := authn.Authenticate(r)
		if err != nil || who.Tenant != app.tenant || who.Subject != slackMachineSubject(app.choice.Team) {
			t.Errorf("%s: %+v, %v", app.tenant, who, err)
		}
		// No apps/slack: the reason it cannot start does not hold the token.
		if err := app.run(context.Background(), "http://127.0.0.1:8080", "", slog.New(slog.DiscardHandler)); err == nil || strings.Contains(err.Error(), app.token) {
			t.Errorf("%s: err = %v", app.tenant, err)
		}
	}
	// A second start is a second process: its tokens are new.
	again, err := httpapi.NewTailscaleAuthn("127.0.0.1:8080", "https://humagen.example.ts.net", "acme", auth.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	next := apps()
	if err := mintSlackCredentials("tailscale", again, next); err != nil {
		t.Fatal(err)
	}
	for _, app := range next {
		for _, before := range two {
			if app.token == before.token {
				t.Error("a token was made twice")
			}
		}
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
