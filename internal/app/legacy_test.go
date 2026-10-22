package app

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/legacy"
	"github.com/urmzd/humagen/pkg/credref"
	"github.com/urmzd/humagen/pkg/spec"
)

// A setting under its old name is noted on stderr before any command runs,
// once, by name and never by value, and copied to its new name.
func TestExecuteNotesAnOldSettingOnce(t *testing.T) {
	set := map[string]string{}
	var stderr bytes.Buffer
	s := sys{
		Environ: func() []string {
			return []string{"WARP_POSTGRES_URL=secret", "WARP_IS_LOCAL_SHELL_SESSION=1", "PATH=/bin"}
		},
		Setenv: func(k, v string) error { set[k] = v; return nil },
		Stderr: &stderr,
		Arg0:   "/usr/local/bin/humagen",
	}
	if code := execute(Options{}, []string{"version"}, s); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	want := "humagen: WARP_POSTGRES_URL is the old name of HUMAGEN_POSTGRES_URL; it still works, rename it\n"
	if stderr.String() != want {
		t.Errorf("stderr %q, want %q", stderr.String(), want)
	}
	if set["HUMAGEN_POSTGRES_URL"] != "secret" || len(set) != 1 {
		t.Errorf("set %v", set)
	}

	// Nothing old in use: nothing said. The old command name is noted.
	stderr.Reset()
	s.Environ = func() []string { return []string{"HUMAGEN_POSTGRES_URL=x"} }
	if execute(Options{}, []string{"version"}, s); stderr.Len() != 0 {
		t.Errorf("noted %q with nothing old in use", stderr.String())
	}
	s.Arg0 = "/usr/local/bin/warp"
	if execute(Options{}, []string{"version"}, s); stderr.String() != "humagen: the command is humagen now; warp still runs it\n" {
		t.Errorf("stderr %q", stderr.String())
	}
}

// start reads a spec it finds in the old place and asks nothing: it is the
// configuration of a deployment made before the rename.
func TestStartReadsTheOldSpecFileAndAsksNothing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HUMAGEN_HOME", home)
	t.Setenv(envWebDir, "")
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	old := filepath.Join(home, "warp.yaml")
	if err := os.WriteFile(old, []byte("api_version: warp/v1\nkind: System\nmetadata: {name: acme, display_name: Acme}\nspec: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready := make(chan string, 1)
	done := make(chan error, 1)
	var notes bytes.Buffer
	var out syncBuffer
	o := Options{ctx: ctx, ready: func(a string) { ready <- a }, notes: legacy.NewNotes(&notes)}
	// -ask would ask; a terminal would ask when no spec is found. Here the
	// old one is found, so the reader is never read.
	go func() { done <- start(o, []string{"-addr", "127.0.0.1:0"}, failingReader{t}, &out) }()
	select {
	case <-ready:
	case err := <-done:
		t.Fatalf("start returned: %v", err)
	case <-time.After(30 * time.Second):
		t.Fatal("start did not listen")
	}
	cancel()
	if err := <-done; err != nil {
		t.Errorf("start: %v", err)
	}
	if !strings.Contains(out.String(), "humagen: reading "+old) {
		t.Errorf("start did not read %s:\n%s", old, out.String())
	}
	for _, want := range []string{"the old place", "api_version warp/v1 still works"} {
		if !strings.Contains(notes.String(), want) {
			t.Errorf("no note %q in:\n%s", want, notes.String())
		}
	}
	if _, err := os.Stat(filepath.Join(home, "humagen.yaml")); !os.IsNotExist(err) {
		t.Errorf("start wrote a second spec beside the old one: %v", err)
	}
}

// setup writes back to the old file it read, in the old language, so the
// release before the rename still reads it.
func TestSetupWritesAnOldSpecAsItWas(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HUMAGEN_HOME", home)
	old := filepath.Join(home, "warp.yaml")
	if err := os.WriteFile(old, []byte("api_version: warp/v1\nkind: System\nmetadata: {name: acme}\nspec: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := setupCmd(Options{}, nil, strings.NewReader(""), io.Discard); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(old)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "api_version: warp/v1") || strings.Contains(string(b), "humagen/v1") {
		t.Errorf("setup rewrote the old spec in the new language:\n%s", b)
	}
	if _, err := os.Stat(filepath.Join(home, "humagen.yaml")); !os.IsNotExist(err) {
		t.Errorf("setup forked a second spec: %v", err)
	}
	if _, err := spec.Load(old); err != nil {
		t.Errorf("the written spec does not read: %v", err)
	}
}

// An old spec's Slack client survives setup under its old name.
func TestMarshalOldKeepsTheOldFieldNames(t *testing.T) {
	cfg, err := spec.Parse([]byte(`api_version: warp/v1
kind: Platform
metadata: {name: acme}
spec:
  surfaces:
    slack:
      enabled: true
      team: T1
      bot_token: {ref: env:SLACK_BOT_TOKEN}
      app_token: {ref: env:SLACK_APP_TOKEN}
      warp_client: {token_url: "https://idp.example.com/token", client_id: c, client_secret: {ref: env:WARP_SLACK_CLIENT_SECRET}}
`))
	if err != nil {
		t.Fatal(err)
	}
	b, err := spec.MarshalOld(cfg.Config)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "warp_client:") || strings.Contains(string(b), "humagen_client") {
		t.Errorf("MarshalOld wrote:\n%s", b)
	}
	if b, _ := spec.Marshal(cfg.Config); !strings.Contains(string(b), "humagen_client:") || strings.Contains(string(b), "warp_client") {
		t.Errorf("Marshal wrote:\n%s", b)
	}
}

// failingReader fails the test if anything is asked.
type failingReader struct{ t *testing.T }

func (r failingReader) Read([]byte) (int, error) {
	r.t.Error("start asked a question")
	return 0, io.EOF
}

// syncBuffer is a bytes.Buffer safe to write from the server's goroutines.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// The shipped compose specs, unchanged since before the rename, start under
// the environment compose.yaml and a new .env give them.
func TestTheComposeSpecsReadTheRenamedVariables(t *testing.T) {
	for _, dir := range []string{"../../deploy/compose/config", "../../deploy/compose/dev"} {
		t.Run(filepath.Base(dir), func(t *testing.T) {
			t.Setenv("HUMAGEN_POSTGRES_URL", "postgres://warp:warp@postgres:5432/warp?sslmode=disable")
			t.Setenv("HUMAGEN_OIDC_CLIENT_SECRET", "s")
			for _, old := range []string{"WARP_POSTGRES_URL", "WARP_OIDC_CLIENT_SECRET"} {
				t.Setenv(old, "")
			}
			l, err := loadFiles(t.Context(), Options{}, []string{dir}, credref.Default)
			if err != nil {
				t.Fatal(err)
			}
			if l.env["HUMAGEN_POSTGRES_URL"] == "" {
				t.Errorf("the stores' URL did not resolve: %v", l.env)
			}
		})
	}
}
