package platform_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urmzd/humagen/fiber/pkg/isolation"
	"github.com/urmzd/humagen/fiber/pkg/isolation/isolationtest"
	"github.com/urmzd/humagen/fiber/pkg/runtime"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/call"
)

func useIn(t *testing.T, ctx context.Context, p *platform.Platform, req runtime.Request, name string, args map[string]any) (string, error) {
	t.Helper()
	out, _, err := p.UseTool(ctx, req, name, args)
	return out, err
}

// In an isolated deployment a run's file tools and shell work in the run's
// instance: it starts from the conversation's files, and what the run leaves
// goes back to them when its instance is released.
func TestAnIsolatedRunsToolsWorkInItsInstance(t *testing.T) {
	leaser := &isolationtest.Leaser{Dir: t.TempDir()}
	p := startWith(t, platform.Config{Sandbox: isolation.Sandbox{Leaser: leaser, Name: "test"}})
	run, req := turn(t, p, "api-1", "")
	dir := p.SandboxDir(tenant, run.Thread)
	if err := p.PutFile(tenant, run.Thread, "brief.md", "the brief"); err != nil {
		t.Fatal(err)
	}
	if err := p.PutFile(tenant, run.Thread, "old.md", "remove me"); err != nil {
		t.Fatal(err)
	}

	ctx, claim := p.Claim(t.Context(), req)
	if out, err := useIn(t, ctx, p, req, platform.ToolFileRead, map[string]any{"path": "brief.md"}); err != nil || out != "the brief" {
		t.Fatalf("read the conversation's file = %q, %v", out, err)
	}
	if _, err := useIn(t, ctx, p, req, platform.ToolFileWrite, map[string]any{"path": "notes/a.md", "content": "# A"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "notes", "a.md")); err == nil {
		t.Fatal("a write reached the host's directory before the run ended")
	}
	out, err := useIn(t, ctx, p, req, platform.ToolShell, map[string]any{"command": "cat notes/a.md; echo made > shell.txt; rm old.md"})
	if err != nil || !strings.HasPrefix(out, "exit 0\n# A") {
		t.Fatalf("shell = %q, %v", out, err)
	}
	execs := leaser.Instances()[0].Execs()
	if len(execs) != 1 || execs[0].In != isolation.Workload || len(execs[0].Env) != 0 {
		t.Fatalf("the shell ran as %+v; want the workload, with no environment of the host's", execs)
	}
	if out, err := useIn(t, ctx, p, req, platform.ToolShell, map[string]any{"command": "exit 3"}); err != nil || !strings.HasPrefix(out, "exit 3") {
		t.Fatalf("a failing command = %q, %v", out, err)
	}
	if _, err := useIn(t, ctx, p, req, platform.ToolPublish, map[string]any{"path": "shell.txt"}); err != nil {
		t.Fatalf("publish from the instance: %v", err)
	}
	if len(leaser.Instances()) != 1 {
		t.Fatalf("one run leased %d instances", len(leaser.Instances()))
	}

	if err := claim.Release(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(leaser.Open()) != 0 {
		t.Fatal("the instance outlived the run")
	}
	for name, want := range map[string]string{"brief.md": "the brief", "notes/a.md": "# A", "shell.txt": "made\n"} {
		if b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name))); err != nil || string(b) != want {
			t.Errorf("%s after the run = %q, %v; want %q", name, b, err, want)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "old.md")); err == nil {
		t.Error("a file the run removed is still in the conversation")
	}

	// The next turn of the conversation starts from what this one left.
	next := req
	ctx2, claim2 := p.Claim(t.Context(), next)
	defer func() { _ = claim2.Release(t.Context()) }()
	if out, err := useIn(t, ctx2, p, next, platform.ToolFileRead, map[string]any{"path": "shell.txt"}); err != nil || out != "made\n" {
		t.Fatalf("the next run read %q, %v", out, err)
	}
	if leaser.Instances()[1] == leaser.Instances()[0] {
		t.Fatal("two runs shared an instance")
	}
}

// Path rules hold in an instance as on the host, and a link code in the
// instance plants is not followed out of it.
func TestAnIsolatedRunCannotEscapeItsWorkDirectory(t *testing.T) {
	leaser := &isolationtest.Leaser{Dir: t.TempDir()}
	p := startWith(t, platform.Config{Sandbox: isolation.Sandbox{Leaser: leaser}})
	run, req := turn(t, p, "api-1", "")
	ctx, claim := p.Claim(t.Context(), req)
	defer func() { _ = claim.Release(t.Context()) }()

	secret := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(secret, []byte("not yours"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"../escape.txt", "/etc/passwd", "notes/../../escape.txt", "..", "", secret} {
		if _, err := useIn(t, ctx, p, req, platform.ToolFileWrite, map[string]any{"path": bad, "content": "x"}); !errors.Is(err, platform.ErrSandbox) {
			t.Errorf("write %q = %v, want refused", bad, err)
		}
		if _, err := useIn(t, ctx, p, req, platform.ToolFileRead, map[string]any{"path": bad}); !errors.Is(err, platform.ErrSandbox) {
			t.Errorf("read %q = %v, want refused", bad, err)
		}
	}
	if out, err := useIn(t, ctx, p, req, platform.ToolShell, map[string]any{"command": "ln -s " + secret + " link.txt && cat link.txt"}); err != nil || !strings.HasPrefix(out, "exit 0") {
		// The test instance is not isolated, so the link resolves inside
		// it; what matters is that the host does not follow it.
		t.Fatalf("planting a link = %q, %v", out, err)
	}
	if out, err := useIn(t, ctx, p, req, platform.ToolFileRead, map[string]any{"path": "link.txt"}); err == nil {
		t.Fatalf("read through a planted link returned %q", out)
	}
	if _, err := useIn(t, ctx, p, req, platform.ToolPublish, map[string]any{"path": "link.txt"}); err == nil {
		t.Fatal("published through a planted link")
	}
	if err := claim.Release(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(p.SandboxDir(tenant, run.Thread), "link.txt")); err == nil {
		t.Fatal("a link the run made was kept in the conversation")
	}
}

// Unisolated, the file tools work on the host as they always did, and the
// shell refuses: a model's command never runs on the server's host.
func TestAnUnisolatedDeploymentRefusesTheShell(t *testing.T) {
	p := start(t, call.Limits{})
	_, req := turn(t, p, "api-1", "")
	ctx, claim := p.Claim(t.Context(), req)
	defer func() { _ = claim.Release(t.Context()) }()
	if claim.Isolated() {
		t.Fatal("a deployment with no sandbox claimed an isolated instance")
	}
	if _, err := useIn(t, ctx, p, req, platform.ToolShell, map[string]any{"command": "touch /tmp/humagen-shell-ran"}); !errors.Is(err, platform.ErrUnisolated) {
		t.Fatalf("shell unisolated = %v, want ErrUnisolated", err)
	}
	if _, err := useIn(t, ctx, p, req, platform.ToolFileWrite, map[string]any{"path": "a.md", "content": "a"}); err != nil {
		t.Fatalf("file_write unisolated = %v", err)
	}
}

// A sandbox that cannot start an instance fails the tools that need one,
// with the reason; nothing falls back to the host.
func TestAFailedLeaseFailsClosed(t *testing.T) {
	leaser := &isolationtest.Leaser{Fail: errors.New("docker is not running")}
	p := startWith(t, platform.Config{Sandbox: isolation.Sandbox{Leaser: leaser}})
	run, req := turn(t, p, "api-1", "")
	ctx, claim := p.Claim(t.Context(), req)
	defer func() { _ = claim.Release(t.Context()) }()
	for _, name := range []string{platform.ToolFileWrite, platform.ToolShell} {
		_, err := useIn(t, ctx, p, req, name, map[string]any{"path": "a.md", "content": "a", "command": "true"})
		if err == nil || !strings.Contains(err.Error(), "docker is not running") {
			t.Errorf("%s with no instance = %v, want the lease's failure", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(p.SandboxDir(tenant, run.Thread), "a.md")); err == nil {
		t.Fatal("a write fell back to the host")
	}
}
