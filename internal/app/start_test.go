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

	"github.com/urmzd/warp/internal/platform"
	"github.com/urmzd/warp/pkg/credref"
)

func TestStartWorksWithNothingConfigured(t *testing.T) {
	home := t.TempDir()
	t.Setenv("WARP_HOME", home)
	t.Setenv(envAddr, "127.0.0.1:0")
	t.Setenv(envWebDir, "")
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	addrs := make(chan string, 1)
	done := make(chan error, 1)
	o := Options{ctx: ctx, ready: func(a string) { addrs <- a }}
	go func() { done <- start(o, []string{"-yes"}, strings.NewReader(""), io.Discard) }()
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
	if _, err := os.Stat(filepath.Join(home, "warp.json")); !os.IsNotExist(err) {
		t.Errorf("-yes wrote choices nobody made: %v", err)
	}
}

func TestSetupWithNoAnswersWritesTheDefaults(t *testing.T) {
	dir := t.TempDir()
	sp, pp := filepath.Join(dir, "warp.json"), filepath.Join(dir, "providers.json")
	// "y" adds a model provider and every other answer is the default:
	// the first preset, with its usual key reference.
	sf, err := runSetup(bufio.NewReader(strings.NewReader("\ny\n\n\n\nn\nn\n")), io.Discard, sp, pp)
	if err != nil {
		t.Fatal(err)
	}
	if sf.Auth != "dev" || sf.Slack.Enabled {
		t.Errorf("choices = %+v", sf)
	}
	pf, err := ReadProvidersFile(pp, false)
	if err != nil {
		t.Fatal(err)
	}
	want := []ModelProvider{{Name: "anthropic", Kind: KindAnthropic, APIKeyRef: "env:ANTHROPIC_API_KEY"}}
	if !reflect.DeepEqual(pf.Models, want) {
		t.Errorf("models = %+v, want %+v", pf.Models, want)
	}
	st, _ := os.Stat(pp)
	if st.Mode().Perm() != 0o600 {
		t.Errorf("providers file mode %v", st.Mode().Perm())
	}
	// A second run keeps what is there.
	if _, err := runSetup(bufio.NewReader(strings.NewReader("")), io.Discard, sp, pp); err != nil {
		t.Fatal(err)
	}
	if again, _ := ReadProvidersFile(pp, false); !reflect.DeepEqual(again.Models, want) {
		t.Errorf("second setup changed the providers: %+v", again.Models)
	}
}

func TestSetupNeverTakesAKeyForAReference(t *testing.T) {
	dir := t.TempDir()
	in := "dev\ny\n2\n\nsk-live-abc\n\nn\nn\nn\n"
	var out bytes.Buffer
	if _, err := runSetup(bufio.NewReader(strings.NewReader(in)), &out, filepath.Join(dir, "w.json"), filepath.Join(dir, "p.json")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "use env:NAME") {
		t.Errorf("the key was not refused:\n%s", out.String())
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "p.json")); bytes.Contains(b, []byte("sk-live")) {
		t.Fatal("the key was written")
	}
}

func TestProvidersFileReadsFibersSpelling(t *testing.T) {
	path := filepath.Join(t.TempDir(), "p.json")
	os.WriteFile(path, []byte(`{"models":[{"name":"vllm","base_url":"http://127.0.0.1:8000/v1","api_key_env":"VLLM_KEY"}],
	  "tool_providers":[{"name":"docs","transport":"http","endpoint":"https://mcp.example.com","tools":[{"name":"search","schema_hash":"abc"}]}]}`), 0o600)
	f, err := ReadProvidersFile(path, false)
	if err != nil {
		t.Fatal(err)
	}
	ext := f.Extensions("acme", credref.Resolver{})
	if _, ok := ext.Providers["vllm"]; !ok || len(ext.ToolProviders) != 1 || ext.ToolProviders[0].Provider.Tenant != "acme" {
		t.Fatalf("extensions = %+v", ext)
	}
}

func TestProvidersFileRefusesMistakes(t *testing.T) {
	for name, body := range map[string]string{
		"a key in the file":  `{"models":[{"name":"x","api_key":"sk"}]}`,
		"a key as reference": `{"models":[{"name":"x","api_key_ref":"sk-123"}]}`,
		"hosted with no key": `{"models":[{"name":"x"}]}`,
		"twice":              `{"models":[{"name":"x","base_url":"http://a"},{"name":"x","base_url":"http://b"}]}`,
		"unknown kind":       `{"models":[{"name":"x","kind":"bard","base_url":"http://a"}]}`,
		"credentials in url": `{"models":[{"name":"x","base_url":"http://u:p@a"}]}`,
		"unpinned tool":      `{"tool_providers":[{"name":"d","transport":"http","endpoint":"https://a","tools":[{"name":"s"}]}]}`,
		"bad credential ref": `{"tool_providers":[{"name":"d","transport":"http","endpoint":"https://a","credential_ref":"hunter2","tools":[]}]}`,
		"both key spellings": `{"models":[{"name":"x","api_key_env":"A","api_key_ref":"env:B"}]}`,
	} {
		path := filepath.Join(t.TempDir(), "p.json")
		os.WriteFile(path, []byte(body), 0o600)
		if _, err := ReadProvidersFile(path, false); err == nil {
			t.Errorf("%s: accepted", name)
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

func TestASettingsFileSeedsOnlyAnEmptyWorkspace(t *testing.T) {
	p, err := platform.New(platform.Config{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "s.json")
	os.WriteFile(path, []byte(`{"workspace_name":"Acme","history":"none","history_limit":4000}`), 0o600)
	ctx := context.Background()
	if err := seedSettings(ctx, p, "t", path); err != nil {
		t.Fatal(err)
	}
	if got := p.Settings("t"); got.WorkspaceName != "Acme" || got.History != platform.HistoryNone {
		t.Fatalf("settings = %+v", got)
	}
	os.WriteFile(path, []byte(`{"workspace_name":"Other"}`), 0o600)
	if err := seedSettings(ctx, p, "t", path); err != nil {
		t.Fatal(err)
	}
	if got := p.Settings("t"); got.WorkspaceName != "Acme" {
		t.Fatalf("the file replaced stored settings: %+v", got)
	}
	os.WriteFile(path, []byte(`{"workspace":"typo"}`), 0o600)
	if err := seedSettings(ctx, p, "u", path); err == nil {
		t.Fatal("an unknown field was accepted")
	}
}
