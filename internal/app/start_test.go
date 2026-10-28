package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	saigetypes "github.com/urmzd/saige/agent/types"

	"github.com/urmzd/humagen/fiber/pkg/turn/native"

	"github.com/urmzd/humagen/pkg/spec"
)

func TestStartWorksWithNothingConfigured(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HUMAGEN_HOME", home)
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
	if _, err := os.Stat(filepath.Join(home, "humagen.yaml")); !os.IsNotExist(err) {
		t.Errorf("-yes wrote choices nobody made: %v", err)
	}
}

// Help, or a flag start does not know, asks nothing and writes nothing,
// whatever stdin is, and /dev/null is not a terminal to ask on.
func TestStartHelpAsksNothingAndWritesNothing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HUMAGEN_HOME", home)
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
		if _, err := os.Stat(filepath.Join(home, "humagen.yaml")); !os.IsNotExist(err) {
			t.Fatalf("start %v wrote humagen.yaml: %v", args, err)
		}
		if out.Len() != 0 {
			t.Errorf("start %v said %q", args, out.String())
		}
	}
	// Help asked for is not a failure.
	if code := Execute(Options{}, []string{"start", "-h"}); code != 0 {
		t.Errorf("humagen start -h exited %d", code)
	}
}

// Ctrl-C at a first-run question stops humagen start there and then, as it
// would anywhere else, and writes nothing: the person asked to stop, not to
// take the defaults.
func TestStartCtrlCAtAQuestionStopsAndWritesNothing(t *testing.T) {
	home := t.TempDir()
	// -setup asks whatever stdin is, so a pipe stands in for the terminal.
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(),
		testCommand+"=start -setup -addr 127.0.0.1:0",
		"HUMAGEN_HOME="+home, envWebDir+"=", "XDG_CACHE_HOME="+t.TempDir())
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stdin.Close() }()
	const question = "What should people see it called?"
	out := &watched{want: question, seen: make(chan struct{})}
	cmd.Stdout, cmd.Stderr = out, out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	// waited is the process's end, read once exited is closed.
	var waited error
	exited := make(chan struct{})
	go func() {
		waited = cmd.Wait()
		close(exited)
	}()
	defer func() {
		_ = cmd.Process.Kill()
		<-exited
	}()
	select {
	case <-out.seen:
	case <-exited:
		t.Fatalf("humagen start ended before asking: %v\n%s", waited, out)
	case <-time.After(30 * time.Second):
		t.Fatalf("humagen start did not ask %q:\n%s", question, out)
	}
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	select {
	case <-exited:
		if waited == nil {
			t.Errorf("humagen start exited 0 after Ctrl-C:\n%s", out)
		}
	case <-time.After(10 * time.Second):
		t.Errorf("humagen start was still asking 10s after Ctrl-C:\n%s", out)
		// Enter through the rest, as the person might, to see what it does.
		_, _ = io.WriteString(stdin, strings.Repeat("\n", 40))
		time.Sleep(time.Second)
	}
	if _, err := os.Stat(filepath.Join(home, "humagen.yaml")); !os.IsNotExist(err) {
		t.Errorf("humagen start wrote humagen.yaml after Ctrl-C: %v", err)
	}
}

// watched keeps what a process writes and says when want first appears.
type watched struct {
	mu   sync.Mutex
	buf  bytes.Buffer
	want string
	seen chan struct{}
	once sync.Once
}

func (w *watched) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf.Write(p)
	if strings.Contains(w.buf.String(), w.want) {
		w.once.Do(func() { close(w.seen) })
	}
	return len(p), nil
}

func (w *watched) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

// start says it found no console only when it serves none: a console -web
// names is served, and one turned off was turned off on purpose.
func TestStartSaysThereIsNoConsoleOnlyWhenNoneIsServed(t *testing.T) {
	t.Setenv("HUMAGEN_HOME", t.TempDir())
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
	path := filepath.Join(t.TempDir(), "humagen.yaml")
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
	path := filepath.Join(t.TempDir(), "humagen.yaml")
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

// Under OIDC the Slack app calls humagen's API as a machine, so setup asks
// where that machine's credentials are, and the spec it writes is one a
// check takes.
func TestSetupGivesTheSlackAppACredentialUnderOIDC(t *testing.T) {
	path := filepath.Join(t.TempDir(), "humagen.yaml")
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
	if sl == nil || !sl.Enabled || sl.HumagenClient == nil {
		t.Fatalf("slack = %+v", sl)
	}
	if c := sl.HumagenClient; c.TokenURL != "https://login.example.com/oauth2/v1/token" || c.ClientID != "humagen-slack" || c.ClientSecret.Ref != "env:HUMAGEN_SLACK_CLIENT_SECRET" {
		t.Errorf("humagen_client = %+v", c)
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
	path := filepath.Join(dir, "humagen.yaml")
	if err := os.WriteFile(path, []byte(`api_version: humagen/v1
kind: System
metadata: {name: acme-agents, display_name: Acme Agents}
spec:
  server: {addr: "127.0.0.1:1"}
  blobs: {kind: memory}
  execution: {max_depth: 2}
---
api_version: humagen/v1
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
		return native.ProviderFunc(func(native.Spec) (saigetypes.Provider, error) { return nil, nil }), nil
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
	path := filepath.Join(t.TempDir(), "humagen.yaml")
	if err := os.WriteFile(path, []byte("api_version: humagen/v1\nkind: System\nspec:\n  blobs: {kind: gcs, gcs: {bucket: b}}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := serve(Options{}, []string{"-f", path, "-addr", "127.0.0.1:0"}, serveDefaults{})
	if err == nil || !strings.Contains(err.Error(), "disk, memory, s3") {
		t.Fatalf("err = %v", err)
	}
}

func TestConfigPrintFillsTheDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "humagen.yaml")
	if err := os.WriteFile(path, []byte("api_version: humagen/v1\nkind: System\nmetadata: {name: acme}\n"), 0o600); err != nil {
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

func TestTheSlackManifestIsWhatHumagenGenerates(t *testing.T) {
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
		t.Errorf("apps/slack/manifest.json is out of date: run `go run ./cmd/humagen slack manifest > apps/slack/manifest.json`\n%s", got.String())
	}
}

func TestTheSlackManifestTakesItsNameFromTheSpec(t *testing.T) {
	path := filepath.Join(t.TempDir(), "humagen.yaml")
	if err := os.WriteFile(path, []byte(`api_version: humagen/v1
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

// Projects moved from runs:* to projects:*, so config check warns about a
// configured machine that reads runs but not projects.
func TestConfigCheckWarnsOfAMachineThatReadsRunsButNotProjects(t *testing.T) {
	t.Setenv("HUMAGEN_TEST_B", "x")
	t.Setenv("HUMAGEN_TEST_A", "x")
	t.Setenv("HUMAGEN_TEST_C", "x")
	check := func(scopes string) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), "humagen.yaml")
		doc := "api_version: humagen/v1\nkind: System\nspec:\n  auth: {kind: oidc, oidc: {issuer: 'https://login.example.com', client_id: humagen, audience: humagen}}\n---\n" +
			"api_version: humagen/v1\nkind: Platform\nmetadata: {name: default}\nspec:\n  surfaces:\n    slack: {enabled: true, team: T1, bot_token: {ref: env:HUMAGEN_TEST_B}, app_token: {ref: env:HUMAGEN_TEST_A}," +
			" humagen_client: {token_url: 'https://login.example.com/token', client_id: humagen-slack, client_secret: {ref: env:HUMAGEN_TEST_C}, scopes: [" + scopes + "]}}\n"
		if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		if err := configCmd(Options{}, []string{"check", "-f", path}, &out); err != nil {
			t.Fatal(err)
		}
		return out.String()
	}
	if got := check("humagen:runs:read, humagen:runs:write"); !strings.Contains(got, `warning: platform "default": the Slack app's humagen_client has humagen:runs:read but not humagen:projects:read`) || !strings.Contains(got, "ok: ") {
		t.Fatalf("check said:\n%s", got)
	}
	for _, scopes := range []string{"humagen:runs:read, humagen:projects:read", "humagen:agents:read"} {
		if got := check(scopes); strings.Contains(got, "warning") {
			t.Fatalf("scopes %s: check said:\n%s", scopes, got)
		}
	}
	// Scopes under the old prefix still count, and are noted, without
	// telling the operator to rename them before the identity provider does.
	got := check("warp:runs:read, warp:projects:read")
	if !strings.Contains(got, "asks for scopes under warp:, the old prefix, which still works; change them to humagen: once your identity provider defines those") ||
		strings.Contains(got, "lists no projects") {
		t.Fatalf("check said:\n%s", got)
	}
	if got := check("warp:runs:read"); !strings.Contains(got, "lists no projects") {
		t.Fatalf("check said:\n%s", got)
	}
}
