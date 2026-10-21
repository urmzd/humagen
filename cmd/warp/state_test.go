package main

import (
	"context"
	"flag"
	"io"
	"log/slog"
	"strings"
	"testing"

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
	secret := "postgres://warp:hunter2@[::1:bad"
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
