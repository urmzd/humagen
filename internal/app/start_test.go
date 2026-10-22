package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	saigetypes "github.com/urmzd/saige/agent/types"

	"github.com/urmzd/fiber/pkg/agentrt"

	"github.com/urmzd/warp/pkg/spec"
)

func TestStartWorksWithNothingConfigured(t *testing.T) {
	home := t.TempDir()
	t.Setenv("WARP_HOME", home)
	t.Setenv(envWebDir, "")
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	addrs := make(chan string, 1)
	done := make(chan error, 1)
	o := Options{ctx: ctx, ready: func(a string) { addrs <- a }}
	go func() { done <- start(o, []string{"-yes", "-addr", "127.0.0.1:0"}, strings.NewReader(""), io.Discard) }()
	var addr string
	select {
	case addr = <-addrs:
	case err := <-done:
		t.Fatalf("start returned before listening: %v", err)
	case <-time.After(30 * time.Second):
		t.Fatal("start did not listen")
	}
	// The seeded agents are there, signed in as the developer, with no key.
	var body []byte
	deadline := time.Now().Add(10 * time.Second)
	for {
		resp, err := http.Get("http://" + addr + "/api/agents")
		if err == nil {
			body, _ = io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode == 200 {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("GET /api/agents: %d %s", resp.StatusCode, body)
			}
		} else if time.Now().After(deadline) {
			t.Fatal(err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !strings.Contains(string(body), `"lead"`) {
		t.Errorf("no seeded lead agent: %s", body)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("start: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("start did not stop")
	}
	if _, err := os.Stat(filepath.Join(home, "warp.yaml")); !os.IsNotExist(err) {
		t.Errorf("-yes wrote choices nobody made: %v", err)
	}
}

// Help, or a flag start does not know, asks nothing and writes nothing,
// whatever stdin is, and /dev/null is not a terminal to ask on.
func TestStartHelpAsksNothingAndWritesNothing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("WARP_HOME", home)
	devnull, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer devnull.Close()
	// The flag set prints its usage to stderr, which the test does not need.
	stderr := os.Stderr
	os.Stderr = devnull
	defer func() { os.Stderr = stderr }()
	if interactive(devnull) {
		t.Errorf("%s is taken for a terminal", os.DevNull)
	}
	for _, args := range [][]string{{"-h"}, {"--help"}, {"-setup", "-h"}, {"-addr", "127.0.0.1:0", "-help"}, {"-setup", "-no-such-flag"}} {
		var out bytes.Buffer
		if err := start(Options{}, args, devnull, &out); err == nil {
			t.Errorf("start %v ran", args)
		}
		if _, err := os.Stat(filepath.Join(home, "warp.yaml")); !os.IsNotExist(err) {
			t.Fatalf("start %v wrote warp.yaml: %v", args, err)
		}
		if out.Len() != 0 {
			t.Errorf("start %v said %q", args, out.String())
		}
	}
	// Help asked for is not a failure.
	if code := Execute(Options{}, []string{"start", "-h"}); code != 0 {
		t.Errorf("warp start -h exited %d", code)
	}
}

// start says it found no console only when it serves none: a console -web
// names is served, and one turned off was turned off on purpose.
func TestStartSaysThereIsNoConsoleOnlyWhenNoneIsServed(t *testing.T) {
	t.Setenv("WARP_HOME", t.TempDir())
	t.Setenv(envWebDir, "")
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	web := t.TempDir()
	if err := os.WriteFile(filepath.Join(web, "index.html"), []byte("<!doctype html><title>the console</title>"), 0o600); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) (said, page string) {
		t.Helper()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		var out bytes.Buffer
		addrs := make(chan string, 1)
		done := make(chan error, 1)
		o := Options{ctx: ctx, ready: func(a string) { addrs <- a }}
		go func() {
			done <- start(o, append([]string{"-yes", "-addr", "127.0.0.1:0"}, args...), strings.NewReader(""), &out)
		}()
		select {
		case addr := <-addrs:
			if resp, err := http.Get("http://" + addr + "/"); err == nil {
				b, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				page = string(b)
			}
		case err := <-done:
			t.Fatalf("start %v returned before listening: %v", args, err)
		case <-time.After(30 * time.Second):
			t.Fatalf("start %v did not listen", args)
		}
		cancel()
		if err := <-done; err != nil {
			t.Fatalf("start %v: %v", args, err)
		}
		return out.String(), page
	}
	const note = "no built console found"
	if said, page := run("-web", web); strings.Contains(said, note) || !strings.Contains(page, "the console") {
		t.Errorf("with -web: said %q, served %q", said, page)
	}
	if said, _ := run(); !strings.Contains(said, note) {
		t.Errorf("with no console anywhere, start said %q", said)
	}
	if said, _ := run("-web="); strings.Contains(said, note) {
		t.Errorf("with the console turned off, start said %q", said)
	}
}

func TestSetupWithNoAnswersWritesAWorkingSpec(t *testing.T) {
	path := filepath.Join(t.TempDir(), "warp.yaml")
	// Every answer is the default except "y" to a model provider, which
	// takes the first preset with its usual key reference.
	if err := runSetup(bufio.NewReader(strings.NewReader("\n\ny\n\n\n\nn\nn\n")), io.Discard, path); err != nil {
		t.Fatal(err)
	}
	cfg, err := spec.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.System.Spec.Auth.Kind != "dev" || len(cfg.Platforms) != 1 {
		t.Fatalf("config = %+v", cfg)
	}
	m := cfg.Platforms[0].Spec.Models
	blk, _ := spec.Block[spec.ModelBlock](m[0].Slot)
	if len(m) != 1 || m[0].Name != "anthropic" || m[0].Kind != "anthropic" || blk.Key.Ref != "env:ANTHROPIC_API_KEY" {
		t.Errorf("models = %+v %+v", m, blk)
	}
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o600 {
		t.Errorf("spec mode %v", st.Mode().Perm())
	}
	// A second run keeps what is there.
	if err := runSetup(bufio.NewReader(strings.NewReader("")), io.Discard, path); err != nil {
		t.Fatal(err)
	}
	if again, _ := spec.Load(path); len(again.Platforms[0].Spec.Models) != 1 {
		t.Errorf("a second setup changed the models: %+v", again.Platforms[0].Spec.Models)
	}
}

func TestSetupNeverTakesAKeyForAReference(t *testing.T) {
	path := filepath.Join(t.TempDir(), "warp.yaml")
	in := "\ndev\ny\n2\n\nsk-live-abc\n\nn\nn\nn\n"
	var out bytes.Buffer
	if err := runSetup(bufio.NewReader(strings.NewReader(in)), &out, path); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "use env:NAME") {
		t.Errorf("the key was not refused:\n%s", out.String())
	}
	if b, _ := os.ReadFile(path); bytes.Contains(b, []byte("sk-live")) {
		t.Fatal("the key was written")
	}
}

// Under OIDC the Slack app calls warp's API as a machine, so setup asks
// where that machine's credentials are, and the spec it writes is one a
// check takes.
func TestSetupGivesTheSlackAppACredentialUnderOIDC(t *testing.T) {
	path := filepath.Join(t.TempDir(), "warp.yaml")
	in := strings.Join([]string{
		"", "oidc", "https://login.example.com", "", "", "", // name, sign-in, issuer, client id, secret, audience
		"n", "n", // no model, no tool
		"y", "T1", "", "", // Slack: team, bot token, app token
		"https://login.example.com/oauth2/v1/token", "", "", // the app's token URL, client id, secret
	}, "\n") + "\n"
	if err := runSetup(bufio.NewReader(strings.NewReader(in)), io.Discard, path); err != nil {
		t.Fatal(err)
	}
	cfg, err := spec.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	sl := cfg.Platforms[0].Spec.Surfaces.Slack
	if sl == nil || !sl.Enabled || sl.WarpClient == nil {
		t.Fatalf("slack = %+v", sl)
	}
	if c := sl.WarpClient; c.TokenURL != "https://login.example.com/oauth2/v1/token" || c.ClientID != "warp-slack" || c.ClientSecret.Ref != "env:WARP_SLACK_CLIENT_SECRET" {
		t.Errorf("warp_client = %+v", c)
	}
}

// serveFor runs serve with args until the test ends and returns its address.
func serveFor(t *testing.T, o Options, args ...string) string {
	t.Helper()
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	addrs := make(chan string, 1)
	done := make(chan error, 1)
	o.ctx, o.ready = ctx, func(a string) { addrs <- a }
	go func() { done <- serve(o, args, serveDefaults{}) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("serve: %v", err)
		}
	})
	select {
	case a := <-addrs:
		return a
	case err := <-done:
		// The cleanup waits on done, so what serve returned is put back.
		done <- nil
		t.Fatalf("serve returned before listening: %v", err)
	case <-time.After(30 * time.Second):
		t.Fatal("serve did not listen")
	}
	return ""
}

func get(t *testing.T, url string) string {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("GET %s: %d %s", url, resp.StatusCode, b)
	}
	return string(b)
}

func TestASpecFileRunsThePlatform(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "warp.yaml")
	if err := os.WriteFile(path, []byte(`api_version: warp/v1
kind: System
metadata: {name: acme-agents, display_name: Acme Agents}
spec:
  server: {addr: "127.0.0.1:1"}
  blobs: {kind: memory}
  execution: {max_depth: 2}
---
api_version: warp/v1
kind: Platform
metadata: {name: default, display_name: Acme}
spec:
  workspace:
    agents: {enable: ["*"], default: greeter}
    projects: [{name: Launch}]
  models:
    - {name: house, kind: house-model, house-model: {voice: calm}}
  agents:
    - spec: {name: greeter, provider: house, model: h1, system_prompt: Greet.}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	var gotBlock string
	o := Options{Kinds: map[string]map[string]Factory{spec.SlotModels: {"house-model": func(_ context.Context, b json.RawMessage) (any, error) {
		gotBlock = string(b)
		return agentrt.ProviderFunc(func(agentrt.Spec) (saigetypes.Provider, error) { return nil, nil }), nil
	}}}}
	// The file says 127.0.0.1:1; the flag wins.
	addr := serveFor(t, o, "-f", path, "-addr", "127.0.0.1:0")
	if !strings.Contains(gotBlock, "calm") {
		t.Errorf("the registered kind got %q", gotBlock)
	}
	if body := get(t, "http://"+addr+"/api/agents"); !strings.Contains(body, `"greeter"`) {
		t.Errorf("the file's agent is not there: %s", body)
	}
	if body := get(t, "http://"+addr+"/api/settings"); !strings.Contains(body, `"workspace_name":"Acme"`) {
		t.Errorf("the file's workspace is not applied: %s", body)
	}
	if body := get(t, "http://"+addr+"/api/projects/~/agents"); !strings.Contains(body, `"default":"greeter"`) {
		t.Errorf("the file's workspace default is not applied: %s", body)
	}
}

func TestAnUnregisteredKindStopsStartup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "warp.yaml")
	if err := os.WriteFile(path, []byte("api_version: warp/v1\nkind: System\nspec:\n  blobs: {kind: gcs, gcs: {bucket: b}}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := serve(Options{}, []string{"-f", path, "-addr", "127.0.0.1:0"}, serveDefaults{})
	if err == nil || !strings.Contains(err.Error(), "disk, memory, s3") {
		t.Fatalf("err = %v", err)
	}
}

func TestConfigPrintFillsTheDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "warp.yaml")
	if err := os.WriteFile(path, []byte("api_version: warp/v1\nkind: System\nmetadata: {name: acme}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := configCmd(Options{}, []string{"print", "-f", path}, &out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"name: acme", "kind: dev", "max_fan_out: 16"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("print lacks %q:\n%s", want, out.String())
		}
	}
}

func TestTheSlackManifestIsWhatWarpGenerates(t *testing.T) {
	var got bytes.Buffer
	if err := slackCmd([]string{"manifest"}, &got); err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join("..", "..", "apps", "slack", "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var a, b any
	_ = json.Unmarshal(got.Bytes(), &a)
	_ = json.Unmarshal(want, &b)
	if !reflect.DeepEqual(a, b) {
		t.Errorf("apps/slack/manifest.json is out of date: run `go run ./cmd/warp slack manifest > apps/slack/manifest.json`\n%s", got.String())
	}
}

func TestTheSlackManifestTakesItsNameFromTheSpec(t *testing.T) {
	path := filepath.Join(t.TempDir(), "warp.yaml")
	if err := os.WriteFile(path, []byte(`api_version: warp/v1
kind: Platform
metadata: {name: default, display_name: Acme}
spec:
  surfaces:
    slack: {enabled: true, team: T1, bot_token: {ref: "env:B"}, app_token: {ref: "env:A"}, command: /acme}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	var got bytes.Buffer
	if err := slackCmd([]string{"manifest", "-f", path}, &got); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.String(), `"name": "Acme"`) || !strings.Contains(got.String(), `"command": "/acme"`) {
		t.Errorf("manifest:\n%s", got.String())
	}
}
