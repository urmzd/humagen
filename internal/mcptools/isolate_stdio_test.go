package mcptools_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/saige/sandbox/isolate/docker"

	"github.com/urmzd/humagen/internal/mcptools"
	"github.com/urmzd/humagen/internal/toolprovider"
	"github.com/urmzd/saige/flow/isolation"
	poolleaser "github.com/urmzd/saige/flow/isolation/isolate"
)

// testImage has sh, the busybox tools and python3.
const testImage = "python:3.13-alpine"

// dockerLeaser returns a Leaser over docker with runc, or skips: these tests
// need a reachable daemon and the test image.
func dockerLeaser(t *testing.T) (*poolleaser.Leaser, string) {
	t.Helper()
	if testing.Short() {
		t.Skip("docker tests are skipped in -short mode")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker is not installed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := exec.CommandContext(ctx, "docker", "info").Run(); err != nil {
		t.Skip("the docker daemon is not reachable")
	}
	if exec.Command("docker", "image", "inspect", testImage).Run() != nil {
		if out, err := exec.Command("docker", "pull", testImage).CombinedOutput(); err != nil {
			t.Skipf("cannot pull %s: %v: %s", testImage, err, out)
		}
	}
	// Docker Desktop and Colima share directories under HOME with their
	// VM, and not the system temporary directory on macOS.
	cache, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(cache, "humagen-test")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp(base, "t-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	// runc, the runtime Docker has on a developer's machine and in CI,
	// which sandbox accepts only as a development runtime.
	d, err := docker.New(docker.Config{Runtime: docker.RuntimeRunc, InsecureDevelopmentRuntime: true, Log: io.Discard, WorkRoot: dir})
	if err != nil {
		t.Fatal(err)
	}
	l, err := poolleaser.New(d, poolleaser.Config{Image: testImage, Labels: map[string]string{"humagen.test": t.Name()}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		_ = l.Close(ctx)
	})
	return l, dir
}

func sh(t *testing.T, inst isolation.Instance, in isolation.Compartment, script string) (string, int) {
	t.Helper()
	var out bytes.Buffer
	st, err := inst.Exec(context.Background(), isolation.Command{Argv: []string{"sh", "-c", script}, In: in, Timeout: time.Minute},
		isolation.Stdio{Stdout: &out, Stderr: &out})
	if err != nil {
		t.Fatalf("exec %q: %v", script, err)
	}
	return out.String(), st.Code
}

// fakeServer is a stdio MCP server in python: one tool, env, which reports
// whether its credential arrived and where it runs.
const fakeServer = `
import json, os, sys
for line in sys.stdin:
    m = json.loads(line)
    if "id" not in m:
        continue
    meth = m.get("method")
    if meth == "initialize":
        r = {"protocolVersion": m["params"]["protocolVersion"], "capabilities": {"tools": {}}, "serverInfo": {"name": "py", "version": "1"}}
    elif meth == "tools/list":
        r = {"tools": [{"name": "env", "description": "Report", "inputSchema": {"type": "object"}}]}
    elif meth == "tools/call":
        seen = os.path.exists("/work/notes.md")
        r = {"content": [{"type": "text", "text": "token=%s work=%s" % (os.environ.get("FAKE_TOKEN", ""), seen)}]}
    else:
        r = {}
    sys.stdout.write(json.dumps({"jsonrpc": "2.0", "id": m["id"], "result": r}) + "\n")
    sys.stdout.flush()
`

// A stdio MCP server of an isolated run runs inside the run's instance (an
// isolate.Leaser over Docker with runc), in
// its connectors compartment, holding its credential, blind to the work
// directory; it ends when the run releases its instance.
func TestDockerRunsAStdioServerInTheRunsInstance(t *testing.T) {
	l, _ := dockerLeaser(t)
	ctx := context.Background()
	run := isolation.NewRun(l, isolation.Request{Tenant: "acme", Run: "r1", Connectors: true}, nil)
	rctx := isolation.WithRun(ctx, run)
	inst, err := run.Instance(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := inst.Files().Write(ctx, "notes.md", strings.NewReader("private")); err != nil {
		t.Fatal(err)
	}
	p := toolprovider.Provider{
		Tenant: "acme", Name: "py", Transport: toolprovider.TransportStdio, Version: 1,
		Endpoint: "python3", Args: []string{"-c", fakeServer}, CredentialRef: "vault:py", CredentialEnv: "FAKE_TOKEN",
	}
	res := mcptools.ResolverFunc(func(context.Context, string) (mcptools.Secret, error) { return mcptools.NewSecret("tok-123"), nil })
	s, err := mcptools.Connect(rctx, p, mcptools.WithResolver(res))
	if err != nil {
		t.Fatalf("Connect in the instance: %v", err)
	}
	if s.Where() != "run" {
		t.Fatalf("Where = %q, want run", s.Where())
	}
	offered, err := s.Offered(ctx)
	if err != nil {
		t.Fatal(err)
	}
	offered[0].Class = toolprovider.ClassRead
	ts, err := s.Toolset(ctx, toolprovider.Snapshot{Tenant: "acme", Provider: "py", Version: 1, Tools: offered}, toolprovider.GrantRead)
	if err != nil {
		t.Fatal(err)
	}
	tool, err := ts.Lookup("py__env")
	if err != nil {
		t.Fatal(err)
	}
	out, err := tool.Execute(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != "token="+mcptools.Redacted+" work=False" {
		t.Fatalf("the server reported %q", out)
	}
	// The workload cannot read the server's credential.
	if o, _ := sh(t, inst, isolation.Workload, "cat /proc/*/environ 2>/dev/null | tr '\\0' '\\n' | grep FAKE_TOKEN"); strings.Contains(o, "tok-123") {
		t.Fatal("the workload read the connectors compartment's credential")
	}
	_ = s.Close()
	if err := run.Release(ctx); err != nil {
		t.Fatal(err)
	}
	if st := l.Stats(); st.Leased != 0 {
		t.Fatalf("%d instances still leased after the run", st.Leased)
	}
}
