package app

import (
	"context"
	"encoding/json"
	"flag"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/auth/mockidp"
	"github.com/urmzd/humagen/pkg/blob"
	"github.com/urmzd/humagen/pkg/credref"
)

// serve is what a server runs, behind a proxy or in a pod, so it keeps a
// server's defaults: OIDC sign-in, nothing seeded, and no spec but the ones
// -f names. The laptop's defaults are start's alone.
func TestServeSignsInWithOIDCAndSeedsNothing(t *testing.T) {
	// A spec in the config directory that would turn on dev sign-in and
	// declare an agent, were serve to read it.
	home := t.TempDir()
	t.Setenv("HUMAGEN_HOME", home)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	if err := os.WriteFile(filepath.Join(home, "humagen.yaml"), []byte(`api_version: humagen/v1
kind: System
spec: {auth: {kind: dev}}
---
api_version: humagen/v1
kind: Platform
metadata: {name: default}
spec:
  agents:
    - spec: {name: from-home, provider: offline, model: offline, system_prompt: Hi.}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	idp, err := mockidp.Start(mockidp.Config{
		Enabled: true, Addr: "127.0.0.1:0",
		ClientID: "humagen", ClientSecret: "web-secret", RedirectURIs: []string{"http://127.0.0.1/auth/callback"},
		Audience: "humagen", Tenant: "default",
		Machines: []mockidp.Machine{{ClientID: "probe", Secret: "probe-secret", Scopes: []string{"humagen:agents:read"}}},
		Log:      slog.New(slog.DiscardHandler),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = idp.Close() }()
	t.Setenv("HUMAGEN_OIDC_ISSUER", idp.Issuer())
	t.Setenv("HUMAGEN_OIDC_CLIENT_ID", "humagen")
	t.Setenv("HUMAGEN_OIDC_CLIENT_SECRET", "web-secret")
	t.Setenv("HUMAGEN_OIDC_AUDIENCE", "humagen")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	addrs, done := make(chan string, 1), make(chan error, 1)
	o := Options{ctx: ctx, ready: func(a string) { addrs <- a }}
	go func() { done <- run(o, []string{"serve", "-addr", "127.0.0.1:0"}) }()
	var addr string
	select {
	case addr = <-addrs:
	case err := <-done:
		t.Fatalf("serve returned before listening: %v", err)
	case <-time.After(30 * time.Second):
		t.Fatal("serve did not listen")
	}
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("serve: %v", err)
		}
	}()

	// No credential is no one, where dev sign-in would be an admin.
	resp, err := http.Get("http://" + addr + "/api/me")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("GET /api/me with no credential: %d, want 401", resp.StatusCode)
	}

	// A machine signed in through the provider sees no agents: none
	// seeded, none read from the config directory.
	tok, err := http.PostForm(idp.Issuer()+"/token", url.Values{
		"grant_type": {"client_credentials"}, "client_id": {"probe"}, "client_secret": {"probe-secret"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var grant struct {
		AccessToken string `json:"access_token"`
	}
	err = json.NewDecoder(tok.Body).Decode(&grant)
	tok.Body.Close()
	if err != nil || grant.AccessToken == "" {
		t.Fatalf("token: %d %v", tok.StatusCode, err)
	}
	req, _ := http.NewRequest(http.MethodGet, "http://"+addr+"/api/agents", nil)
	req.Header.Set("Authorization", "Bearer "+grant.AccessToken)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/agents as a machine: %d %s", resp.StatusCode, body)
	}
	for _, name := range []string{`"lead"`, `"helper"`, `"from-home"`} {
		if strings.Contains(string(body), name) {
			t.Errorf("serve has agent %s: %s", name, body)
		}
	}
}

// Dev auth makes every request an admin, so it must never be reachable from
// another machine.
func TestDevAuthIsLoopbackOnly(t *testing.T) {
	for addr, want := range map[string]bool{
		"127.0.0.1:8080": true,
		"localhost:8080": true,
		"[::1]:8080":     true,
		"0.0.0.0:8080":   false,
		":8080":          false,
		"10.0.0.5:8080":  false,
		"example.com:80": false,
		"nonsense":       false,
	} {
		if got := loopback(addr); got != want {
			t.Errorf("loopback(%q) = %v, want %v", addr, got, want)
		}
	}
	for _, addr := range []string{"0.0.0.0:8080", ":8080"} {
		if err := run(Options{}, []string{"serve", "-auth=dev", "-addr", addr}); err == nil {
			t.Errorf("serve -auth=dev -addr %s started", addr)
		}
	}
}

func TestStorageFlags(t *testing.T) {
	open := func(env map[string]string, args ...string) (blob.Store, error) {
		t.Helper()
		fs := flag.NewFlagSet("serve", flag.ContinueOnError)
		f := addStorageFlags(fs)
		if err := fs.Parse(args); err != nil {
			t.Fatal(err)
		}
		return openBlobs(t.Context(), Options{}, loaded{}, f, func(k string) string { return env[k] })
	}
	if s, err := open(nil); s != nil || err != nil {
		t.Fatalf("the default = %T, %v; want the platform's own", s, err)
	}
	dir := t.TempDir()
	if s, err := open(nil, "-storage-dir", dir); err != nil {
		t.Fatal(err)
	} else if _, ok := s.(*blob.Disk); !ok {
		t.Fatalf("-storage-dir = %T", s)
	}
	s, err := open(map[string]string{s3AccessEnv: "k", s3SecretEnv: "s"}, "-storage=s3", "-s3-bucket=humagen", "-s3-endpoint=http://127.0.0.1:9000", "-s3-region=auto", "-s3-path-style", "-s3-prefix=prod")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.(*blob.S3); !ok {
		t.Fatalf("-storage=s3 = %T", s)
	}
	if s, err = open(nil, "-storage=gcs", "-gcs-bucket=humagen", "-gcs-endpoint=http://127.0.0.1:4443", "-gcs-prefix=prod"); err != nil {
		t.Fatal(err)
	} else if _, ok := s.(*blob.GCS); !ok {
		t.Fatalf("-storage=gcs = %T", s)
	}
	for name, c := range map[string]struct {
		env  map[string]string
		args []string
	}{
		"no bucket":          {nil, []string{"-storage=s3"}},
		"no gcs bucket":      {nil, []string{"-storage=gcs", "-gcs-endpoint=http://127.0.0.1:4443"}},
		"half a credential":  {map[string]string{s3AccessEnv: "k"}, []string{"-storage=s3", "-s3-bucket=humagen"}},
		"an unknown storage": {nil, []string{"-storage=tape"}},
	} {
		if _, err := open(c.env, c.args...); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

// A System document's gcs blobs become serve's storage flags.
func TestTheSpecsGCSBlobsBecomeFlags(t *testing.T) {
	path := filepath.Join(t.TempDir(), "humagen.yaml")
	body := "api_version: humagen/v1\nkind: System\nmetadata: {name: humagen}\nspec:\n  blobs:\n    kind: gcs\n    gcs: {bucket: humagen-files, prefix: prod, endpoint: \"http://127.0.0.1:4443\"}\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	l, err := loadFiles(t.Context(), Options{}, []string{path}, credref.Resolver{})
	if err != nil {
		t.Fatal(err)
	}
	args := " " + strings.Join(l.args, " ") + " "
	for _, want := range []string{"-storage=gcs", "-gcs-bucket=humagen-files", "-gcs-prefix=prod", "-gcs-endpoint=http://127.0.0.1:4443"} {
		if !strings.Contains(args, " "+want+" ") {
			t.Errorf("args %v lack %s", l.args, want)
		}
	}
}
