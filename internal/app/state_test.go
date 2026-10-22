package app

import (
	"context"
	"flag"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/fiber/pkg/agentspec"
	"github.com/urmzd/fiber/pkg/registry"
)

func TestStateFlags(t *testing.T) {
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	parse := func(env map[string]string, args ...string) stateFlags {
		fs := flag.NewFlagSet("serve", flag.ContinueOnError)
		f := addStateFlags(fs, envOf(env))
		if err := fs.Parse(args); err != nil {
			t.Fatal(err)
		}
		return f
	}
	if f := parse(nil); *f.kind != "memory" {
		t.Fatalf("with no URL the default is %q", *f.kind)
	}
	if f := parse(map[string]string{postgresURLEnv: "postgres://x"}); *f.kind != "postgres" {
		t.Fatalf("with a URL the default is %q", *f.kind)
	}
	b, release, err := openState(t.Context(), parse(nil), envOf(nil), quiet)
	if err != nil || b.State != nil || b.Agents != nil || b.Ledger != nil {
		t.Fatalf("memory = %+v, %v", b, err)
	}
	release()
	if _, _, err := openState(t.Context(), parse(nil, "-state=postgres"), envOf(nil), quiet); err == nil || !strings.Contains(err.Error(), postgresURLEnv) {
		t.Fatalf("postgres with no URL = %v", err)
	}
	if _, _, err := openState(t.Context(), parse(nil, "-state=disk"), envOf(nil), quiet); err == nil {
		t.Fatal("an unknown kind was accepted")
	}
	secret := "postgres://humagen:hunter2@[::1:bad"
	_, _, err = openState(t.Context(), parse(map[string]string{postgresURLEnv: secret}), envOf(map[string]string{postgresURLEnv: secret}), quiet)
	if err == nil || strings.Contains(err.Error(), "hunter2") {
		t.Fatalf("a malformed URL = %v", err)
	}
}

func TestRegisterSpecRevisesWhatIsKept(t *testing.T) {
	ctx := context.Background()
	agents := registry.NewMemory()
	spec := agentspec.AgentSpec{Name: "lead", Model: "offline", Provider: "offline", SystemPrompt: "One."}
	for range 2 {
		if err := registerSpec(ctx, agents, "acme", spec); err != nil {
			t.Fatal(err)
		}
	}
	if h, _ := agents.History(ctx, "acme", "lead"); len(h) != 1 {
		t.Fatalf("an unchanged spec added revisions: %d", len(h))
	}
	spec.SystemPrompt = "Two."
	if err := registerSpec(ctx, agents, "acme", spec); err != nil {
		t.Fatal(err)
	}
	if got, _ := agents.Get(ctx, "acme", "lead", 0); got.Rev != 2 || got.Spec.SystemPrompt != "Two." {
		t.Fatalf("after a changed spec = %+v", got)
	}
}

func TestASpecOnPostgresKeepsItsAgentsAcrossARestart(t *testing.T) {
	url := os.Getenv("HUMAGEN_POSTGRES_TEST_URL")
	if url == "" {
		t.Skip("HUMAGEN_POSTGRES_TEST_URL is not set")
	}
	t.Setenv("SPEC_TEST_PG", url)
	path := filepath.Join(t.TempDir(), "humagen.yaml")
	if err := os.WriteFile(path, []byte(`api_version: humagen/v1
kind: System
spec:
  stores: {kind: postgres, postgres: {url: {ref: "env:SPEC_TEST_PG"}}}
---
api_version: humagen/v1
kind: Platform
metadata: {name: default}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	create := func(addr string) {
		body := strings.NewReader(`{"name":"kept","provider":"offline","model":"offline","system_prompt":"Stay."}`)
		resp, err := http.Post("http://"+addr+"/api/agents", "application/json", body)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode/100 != 2 && resp.StatusCode != http.StatusConflict {
			t.Fatalf("create: %d", resp.StatusCode)
		}
	}
	func() {
		addr, stop := serveUntil(t, Options{}, "-f", path, "-addr", "127.0.0.1:0")
		defer stop()
		create(addr)
	}()
	addr, stop := serveUntil(t, Options{}, "-f", path, "-addr", "127.0.0.1:0")
	defer stop()
	if body := get(t, "http://"+addr+"/api/agents"); !strings.Contains(body, `"kept"`) {
		t.Fatalf("the agent did not survive the restart: %s", body)
	}
}

// serveUntil is serveFor with a stop the test calls itself.
func serveUntil(t *testing.T, o Options, args ...string) (string, func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	addrs := make(chan string, 1)
	done := make(chan error, 1)
	o.ctx, o.ready = ctx, func(a string) { addrs <- a }
	go func() { done <- serve(o, args, serveDefaults{}) }()
	stop := func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("serve: %v", err)
		}
	}
	select {
	case a := <-addrs:
		return a, stop
	case err := <-done:
		t.Fatalf("serve returned before listening: %v", err)
	case <-time.After(30 * time.Second):
		t.Fatal("serve did not listen")
	}
	return "", stop
}
