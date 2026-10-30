package app

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/urmzd/humagen/fiber/pkg/isolation"
	"github.com/urmzd/humagen/pkg/credref"
)

func openSandboxWith(t *testing.T, l loaded, runtimeDefault string, args ...string) (isolation.Sandbox, error) {
	t.Helper()
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	f := addSandboxFlags(fs, runtimeDefault)
	if err := fs.Parse(args); err != nil {
		t.Fatal(err)
	}
	s, closeIt, err := openSandbox(t.Context(), Options{}, l, f, slog.New(slog.DiscardHandler))
	if err == nil {
		t.Cleanup(closeIt)
	}
	return s, err
}

// The default is docker on gVisor; the host is only ever chosen by name.
func TestTheSandboxDefaultsToDockerOnGVisor(t *testing.T) {
	s, err := openSandboxWith(t, loaded{}, "")
	if err != nil {
		t.Fatal(err)
	}
	if !s.Isolated() || s.String() != "docker/runsc" {
		t.Fatalf("default sandbox = %s, isolated %v", s, s.Isolated())
	}
	if s, _ := openSandboxWith(t, loaded{}, "runc"); s.String() != "docker/runc" {
		t.Fatalf("start's default = %s", s)
	}
	if s, err := openSandboxWith(t, loaded{}, "", "-sandbox=unisolated"); err != nil || s.Isolated() {
		t.Fatalf("-sandbox=unisolated = %s, %v", s, err)
	}
	if s, err := openSandboxWith(t, loaded{}, "", "-sandbox=local", "-sandbox-work-root="+t.TempDir()); err != nil || !s.Isolated() || !strings.Contains(s.String(), "not isolated") {
		t.Fatalf("-sandbox=local = %s, %v", s, err)
	}
	if _, err := openSandboxWith(t, loaded{}, "", "-sandbox=chroot"); err == nil {
		t.Fatal("an unknown sandbox was accepted")
	}
}

// Without a docker CLI the server still starts, and every lease says why.
func TestADockerSandboxWithoutDockerFailsClosed(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	s, err := openSandboxWith(t, loaded{}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Leaser.Lease(context.Background(), isolation.Request{Run: "r"}); err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("a lease with no docker = %v", err)
	}
}

// A System document's sandbox becomes serve's flags, and a malformed one is
// refused when the file is read.
func TestTheSpecsSandboxBecomesFlags(t *testing.T) {
	dir := t.TempDir()
	write := func(body string) string {
		path := filepath.Join(dir, "humagen.yaml")
		if err := os.WriteFile(path, []byte("api_version: humagen/v1\nkind: System\nmetadata: {name: humagen}\nspec:\n"+body), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	l, err := loadFiles(t.Context(), Options{}, []string{write("  sandbox:\n    kind: docker\n    docker: {runtime: runc, image: alpine:3.20, connectors_image: node:22-alpine, warm: 2, max: 8}\n")}, credref.Resolver{})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"-sandbox=docker", "-sandbox-runtime=runc", "-sandbox-image=alpine:3.20", "-sandbox-connectors-image=node:22-alpine"} {
		if !slices.Contains(l.args, want) {
			t.Errorf("args %v lack %s", l.args, want)
		}
	}
	l, err = loadFiles(t.Context(), Options{}, []string{write("  sandbox: {kind: unisolated}\n")}, credref.Resolver{})
	if err != nil || !slices.Contains(l.args, "-sandbox=unisolated") {
		t.Fatalf("unisolated: args %v, %v", l.args, err)
	}
	for name, body := range map[string]string{
		"a bad runtime":   "  sandbox:\n    kind: docker\n    docker: {runtime: \"run c\"}\n",
		"warm over max":   "  sandbox:\n    kind: docker\n    docker: {warm: 9, max: 2}\n",
		"an unknown kind": "  sandbox: {kind: chroot}\n",
		"an unknown key":  "  sandbox:\n    kind: docker\n    docker: {network: host}\n",
	} {
		if _, err := loadFiles(t.Context(), Options{}, []string{write(body)}, credref.Resolver{}); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
