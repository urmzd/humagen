package app

import (
	"bytes"
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
	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/credref"
	"github.com/urmzd/humagen/pkg/spec"
	"github.com/urmzd/humagen/pkg/state"
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

// The app is told which client it is only once the server keeps that record:
// with nobody mapped, its environment is the one it always had.
func TestTheSlackAppIsToldItsClientOnlyWhenTheServerKeepsOne(t *testing.T) {
	lookup := func(k string) (string, bool) {
		v, ok := map[string]string{"BOT": "xoxb-1", "APP": "xapp-1"}[k]
		return v, ok
	}
	app := &slackApp{tenant: "acme", keys: credref.Resolver{LookupEnv: lookup},
		choice: spec.Slack{Enabled: true, Team: "T0123ACME", BotToken: &spec.Ref{Ref: "env:BOT"}, AppToken: &spec.Ref{Ref: "env:APP"},
			People: map[string]string{"U0123ANA": "tailscale:ana@github"}}}
	names := func() []string {
		t.Helper()
		env, err := app.env(context.Background(), "http://127.0.0.1:8080", "", lookup)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, kv := range env {
			if k, _, _ := strings.Cut(kv, "="); strings.HasSuffix(k, "_CLIENT") {
				out = append(out, kv)
			}
		}
		return out
	}
	// People in the file are not enough: no record, no name.
	if got := names(); len(got) != 0 {
		t.Fatalf("with no record kept: %v", got)
	}
	app.client = "slack-app-t0123acme"
	if got := names(); !slices.Equal(got, []string{"HUMAGEN_CLIENT=slack-app-t0123acme", "WARP_CLIENT=slack-app-t0123acme"}) {
		t.Fatalf("with a record kept: %v", got)
	}
	if got := slackMachine("tailscale", spec.Slack{Team: "T1"}); got != "slack-app:T1" {
		t.Errorf("the machine under tailscale = %q", got)
	}
	if got := slackMachine("oidc", spec.Slack{Team: "T1", Machine: "humagen-slack"}); got != "humagen-slack" {
		t.Errorf("the machine under oidc = %q", got)
	}
}

// slackStandIn starts a server from a file with a stand-in for the Slack
// app's node, and returns the server's address and what the app was given:
// its token and the client it was told it is.
func slackStandIn(t *testing.T, file string) (addr, token, client string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in for node is a shell script")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "humagen.yaml")
	if err := os.WriteFile(path, []byte(file), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEST_SLACK_BOT", "xoxb-1")
	t.Setenv("TEST_SLACK_APP", "xapp-1")
	t.Setenv(envWebDir, t.TempDir())
	appDir, bin := t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(appDir, "node_modules"), 0o700); err != nil {
		t.Fatal(err)
	}
	node := "#!/bin/sh\nprintf '%s\\n%s' \"$HUMAGEN_TOKEN\" \"${HUMAGEN_CLIENT-unset}\" > given.tmp && mv given.tmp given\nexec sleep 600\n"
	if err := os.WriteFile(filepath.Join(bin, "node"), []byte(node), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HUMAGEN_SLACK_APP_DIR", appDir)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	appDone := make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-appDone:
		case <-time.After(30 * time.Second):
			t.Error("the Slack app's goroutine did not end with the server")
		}
	})
	addr = serveFor(t, Options{slackDone: func(string, error) { close(appDone) }}, "-f", path, "-addr", "127.0.0.1:0", "-public-url", "https://humagen.example.ts.net")
	for deadline := time.Now().Add(30 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		if b, err := os.ReadFile(filepath.Join(appDir, "given")); err == nil {
			token, client, _ = strings.Cut(string(b), "\n")
			return addr, token, client
		}
		if time.Now().After(deadline) {
			t.Fatal("the Slack app was not started")
		}
	}
}

const slackTailscaleFile = `api_version: humagen/v1
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
    slack: {enabled: true, team: T0123ACME, bot_token: {ref: "env:TEST_SLACK_BOT"}, app_token: {ref: "env:TEST_SLACK_APP"}%s}
`

// From the file alone, under tailscale: the server keeps the app's record,
// tells the app which client it is, and the credential it made for the app
// is the one that acts for the people the file maps, and for nobody else.
// A message that names no agent is still the entry agent's to answer.
func TestTheSlackAppTheServerStartsActsForThePeopleTheFileMaps(t *testing.T) {
	// A Slack click carries no sign-in, so a mapped person holds what the
	// policy grants their subject, as their own tailscale sign-in does.
	policy := filepath.Join(t.TempDir(), "policy.authz")
	if err := os.WriteFile(policy, []byte("role viewer { allow read on * }\nrole operator extends viewer { allow write on runs }\n"+
		"grant operator to subject \"tailscale:ana@github\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(policyEnv, policy)
	addr, token, client := slackStandIn(t, strings.Replace(slackTailscaleFile, "%s", `, people: {U0123ANA: "tailscale:ana@github"}`, 1))
	if client != "slack-app-t0123acme" || client != platform.SlackAppClient("T0123ACME") {
		t.Fatalf("the app was told it is the client %q", client)
	}
	call := func(method, path, body string) (int, map[string]any) {
		t.Helper()
		req, err := http.NewRequest(method, "http://"+addr+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out
	}
	// The app reads its own record, as it does every half minute.
	status, record := call("GET", "/api/clients/"+client, "")
	slackOpts, _ := record["slack"].(map[string]any)
	people, _ := slackOpts["people"].(map[string]any)
	identity, _ := record["identity"].(map[string]any)
	if status != 200 || record["surface"] != "slack" || record["enabled"] != true || identity["team"] != "T0123ACME" ||
		slackOpts["machine"] != slackMachineSubject("T0123ACME") || len(people) != 1 || people["U0123ANA"] != "tailscale:ana@github" {
		t.Fatalf("the app's record: %d %v", status, record)
	}
	// What the app routes by: any agent may be named, and none is its default.
	if agents, _ := record["agents"].([]any); record["default_agent"] != nil || len(agents) != 1 || agents[0] != "*" {
		t.Fatalf("the record routes to default %v and %v", record["default_agent"], record["agents"])
	}
	// A mapped Slack user is that person; anyone else is nobody.
	as := func(user string) string { return "?act_as_client=" + client + "&act_as_user=" + user }
	if status, out := call("GET", "/api/inbox/count"+as("U0123ANA"), ""); status != 200 {
		t.Fatalf("reading for a mapped person: %d %v", status, out)
	}
	if status, out := call("GET", "/api/inbox/count"+as("U0123EVE"), ""); status != 403 {
		t.Fatalf("reading for an unmapped person: %d %v", status, out)
	}
	if status, out := call("GET", "/api/inbox/count?act_as_client=slack-app-t0999none&act_as_user=U0123ANA", ""); status != 404 {
		t.Fatalf("reading through a client that is not there: %d %v", status, out)
	}
	if status, out := call("POST", "/api/runs", `{"text":"hello","act_as":{"client":"`+client+`","user":"U0123EVE"}}`); status != 403 {
		t.Fatalf("a run for an unmapped person: %d %v", status, out)
	}
	// An unnamed message, for a mapped person and for nobody, has one answerer.
	status, own := call("POST", "/api/runs", `{"text":"hello"}`)
	if status != 202 {
		t.Fatalf("the app's own unnamed run: %d %v", status, own)
	}
	status, theirs := call("POST", "/api/runs", `{"text":"hello","act_as":{"client":"`+client+`","user":"U0123ANA"}}`)
	if status != 202 {
		t.Fatalf("an unnamed run for a mapped person: %d %v", status, theirs)
	}
	actor, _ := theirs["actor"].(map[string]any)
	if own["agent"] != "humagen" || theirs["agent"] != own["agent"] || actor["user"] != "tailscale:ana@github" || actor["bot"] == true {
		t.Fatalf("unnamed runs are answered by %v (the app's own) and %v (ana's, actor %v)", own["agent"], theirs["agent"], actor)
	}
	// The same holds where the app posts a mapped person's Slack message into
	// its thread here, in a direct message and under a mention in a channel.
	for where, body := range map[string]string{
		"a direct message": `{"text":"hello","act_as":{"client":"` + client + `","user":"U0123ANA"},"surface":{"client":"` + client + `","channel":"D0123ANA","ts":"1700000000.000100","message":"1700000000.000100","app_user":"U0APP"}}`,
		"a mention":        `{"text":"<@U0APP> hello","act_as":{"client":"` + client + `","user":"U0123ANA"},"surface":{"client":"` + client + `","channel":"C0123ROOM","ts":"1700000000.000200","message":"1700000000.000200","app_user":"U0APP"}}`,
	} {
		status, posted := call("POST", "/api/threads", body)
		started, _ := posted["runs"].([]any)
		if status != 201 || len(started) != 1 {
			t.Fatalf("%s from a mapped person: %d %v", where, status, posted)
		}
		message, _ := posted["message"].(map[string]any)
		if run, _ := started[0].(map[string]any); run["agent"] != "humagen" || message["by"] != "ana" {
			t.Fatalf("%s is answered by %v, posted by %v", where, run["agent"], message["by"])
		}
	}
	if status, out := call("POST", "/api/threads", `{"text":"hello","act_as":{"client":"`+client+`","user":"U0123EVE"},"surface":{"client":"`+client+`","channel":"D0123EVE","ts":"1700000000.000300","message":"1700000000.000300","app_user":"U0APP"}}`); status != 403 {
		t.Fatalf("a message posted for an unmapped person: %d %v", status, out)
	}
}

// With nobody mapped nothing changes: no record, and no client named.
func TestASlackAppThatMapsNobodyIsToldOfNoClient(t *testing.T) {
	addr, token, client := slackStandIn(t, strings.Replace(slackTailscaleFile, "%s", "", 1))
	if client != "unset" {
		t.Fatalf("the app was told it is the client %q", client)
	}
	req, err := http.NewRequest("GET", "http://"+addr+"/api/clients", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		Clients []map[string]any `json:"clients"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || resp.StatusCode != 200 || len(out.Clients) != 0 {
		t.Fatalf("clients with nobody mapped: %d %v %v", resp.StatusCode, out.Clients, err)
	}
}

// Each start makes the app's record what the file says: made when people are
// first mapped, people added and removed after, what an administrator chose
// kept, and nobody left to act for once the file maps nobody.
func TestTheSlackAppsRecordFollowsTheFileAtEveryStart(t *testing.T) {
	st := state.NewMemory()
	var logs bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logs, nil))
	start := func(mode, surface string) (*platform.Platform, map[string]string) {
		t.Helper()
		cfg := mustLoadSlack(t, mode, surface)
		p, err := platform.New(platform.Config{Workspace: t.TempDir(), Backends: platform.Backends{State: st}, ClientInterval: -1, Log: log})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = p.Close() })
		kept, err := applySlackClients(t.Context(), p, cfg, mode, log)
		if err != nil {
			t.Fatal(err)
		}
		return p, kept
	}
	slackWith := func(extra string) string {
		return "    slack: {enabled: true, team: T0123ACME, bot_token: {ref: env:B}, app_token: {ref: env:A}" + extra + "}\n"
	}
	const id = "slack-app-t0123acme"

	// Nobody mapped: no record, and nothing to tell the app.
	p, kept := start("tailscale", slackWith(""))
	if all, err := p.Clients.List("default"); err != nil || len(all) != 0 || len(kept) != 0 {
		t.Fatalf("with nobody mapped: %d clients, kept %v, %v", len(all), kept, err)
	}
	_ = p.Close()

	// Under tailscale the machine is the one the server makes a credential for.
	p, kept = start("tailscale", slackWith(", people: {U0123ANA: 'tailscale:ana@github', U0123BEN: 'tailscale:ben@github'}"))
	c, err := p.Clients.Get("default", id)
	if err != nil || kept["default"] != id || c.Slack.Machine != "slack-app:T0123ACME" || len(c.Slack.People) != 2 || c.CreatedBy != "file" {
		t.Fatalf("the first start with people: kept %v, %+v (slack %+v), %v", kept, c, c.Slack, err)
	}
	// An administrator gives the app a default agent of its own.
	c.DefaultAgent = "lead"
	if _, err := p.UpdateClient(t.Context(), "default", id, "ada", platform.ClientInput{Client: c}); err != nil {
		t.Fatal(err)
	}
	_ = p.Close()

	// The next start: ben is out, cy is in, and the default agent stays.
	p, kept = start("tailscale", slackWith(", people: {U0123ANA: 'tailscale:ana@github', U0123CY: 'tailscale:cy@github'}"))
	c, err = p.Clients.Get("default", id)
	if err != nil || kept["default"] != id || !maps.Equal(c.Slack.People, map[string]string{"U0123ANA": "tailscale:ana@github", "U0123CY": "tailscale:cy@github"}) {
		t.Fatalf("the second start: kept %v, %+v, %v", kept, c.Slack, err)
	}
	if c.DefaultAgent != "lead" || !slices.Equal(c.Agents, []string{clients.AnyAgent}) || !c.Enabled {
		t.Fatalf("the second start took back what an administrator chose: %+v", c)
	}
	version := c.Version
	_ = p.Close()

	// The same file again writes nothing. Under another sign-in the machine is
	// the file's.
	p, _ = start("tailscale", slackWith(", people: {U0123ANA: 'tailscale:ana@github', U0123CY: 'tailscale:cy@github'}"))
	if c, _ = p.Clients.Get("default", id); c.Version != version {
		t.Fatalf("an unchanged file made version %d of %d", c.Version, version)
	}
	_ = p.Close()
	p, kept = start("oidc", slackWith(", humagen_token: {ref: env:T}, machine: humagen-slack, people: {U0123ANA: ana}"))
	if c, _ = p.Clients.Get("default", id); kept["default"] != id || c.Slack.Machine != "humagen-slack" || !maps.Equal(c.Slack.People, map[string]string{"U0123ANA": "ana"}) {
		t.Fatalf("under oidc: kept %v, %+v", kept, c.Slack)
	}
	_ = p.Close()

	// A file that maps nobody, or has no Slack surface, leaves the credential
	// nobody to act for and names no client to the app.
	for name, surface := range map[string]string{"nobody mapped": slackWith(""), "the surface off": strings.Replace(slackWith(", people: {U0123ANA: ana}"), "enabled: true", "enabled: false", 1), "no surface": ""} {
		p, kept = start("tailscale", slackWith(", people: {U0123ANA: 'tailscale:ana@github'}"))
		if c, _ = p.Clients.Get("default", id); len(kept) != 1 || len(c.Slack.People) != 1 {
			t.Fatalf("%s: mapping again: kept %v, %+v", name, kept, c.Slack)
		}
		_ = p.Close()
		p, kept = start("tailscale", surface)
		c, err = p.Clients.Get("default", id)
		if err != nil || len(kept) != 0 || c.Slack.Machine != "" || len(c.Slack.People) != 0 || c.DefaultAgent != "lead" {
			t.Fatalf("%s: kept %v, %+v (slack %+v), %v", name, kept, c, c.Slack, err)
		}
		if _, ok := c.ActsFor("slack-app:T0123ACME", "U0123ANA"); ok {
			t.Fatalf("%s: the app still acts for U0123ANA", name)
		}
		_ = p.Close()
	}

	// Another client under the record's id is left alone, said in the log,
	// and the app is told of no client.
	other := state.NewMemory()
	st = other
	p, _ = start("tailscale", "")
	if _, err := p.CreateClient(t.Context(), "default", "ada", platform.ClientInput{
		Client: clients.Client{ID: id, Name: "Mine", Surface: clients.Slack, Enabled: true, DefaultAgent: "lead", Identity: clients.Identity{Team: "T0123ACME"}},
		Token:  clients.SecretInput{Value: "xoxb-mine"}, SigningSecret: clients.SecretInput{Value: "sig-mine"},
	}); err != nil {
		t.Fatal(err)
	}
	logs.Reset()
	kept, err = applySlackClients(t.Context(), p, mustLoadSlack(t, "tailscale", slackWith(", people: {U0123ANA: 'tailscale:ana@github'}")), "tailscale", log)
	if err != nil || len(kept) != 0 || !strings.Contains(logs.String(), "the app acts for nobody") {
		t.Fatalf("over another client: kept %v, %v, log %s", kept, err, logs.String())
	}
	if c, _ = p.Clients.Get("default", id); c.Managed != "" || c.Slack != nil && len(c.Slack.People) != 0 {
		t.Fatalf("the other client was changed: %+v", c)
	}
}

// mustLoadSlack loads a file whose sign-in is mode and whose one workspace
// has the Slack surface given, or none.
func mustLoadSlack(t *testing.T, mode, surface string) spec.Config {
	t.Helper()
	auth := "{kind: " + mode + "}"
	if mode == "oidc" {
		auth = "{kind: oidc, oidc: {issuer: 'https://login.example.com', client_id: humagen, audience: humagen}}"
	}
	doc := "api_version: humagen/v1\nkind: System\nspec:\n  auth: " + auth + "\n---\n" +
		"api_version: humagen/v1\nkind: Platform\nmetadata: {name: default}\nspec:\n  surfaces:\n" + surface
	if surface == "" {
		doc = strings.TrimSuffix(doc, "spec:\n  surfaces:\n") + "spec: {}\n"
	}
	path := filepath.Join(t.TempDir(), "humagen.yaml")
	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := spec.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}
