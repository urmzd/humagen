package platform

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/urmzd/playscape/pkg/workspace"
	saigetypes "github.com/urmzd/saige/agent/types"

	"github.com/urmzd/humagen/fiber/pkg/event"
	"github.com/urmzd/humagen/fiber/pkg/isolation"
	"github.com/urmzd/humagen/fiber/pkg/runtime"
)

// Where a run's tools execute (RFC 0004, "The sandbox").
//
// Each turn claims an instance from the configured sandbox
// (Config.Sandbox), leased on the first tool that needs one and destroyed
// when the turn ends. The file tools work in its work directory, the shell
// runs in its workload compartment, and stdio MCP servers run in its
// connectors compartment (fiber/pkg/mcptools). The work directory starts
// from the conversation's scratch directory and is written back to it at
// the end (runFiles), so a follow-up turn sees what the last one wrote. The
// run's memory is installed into /project and /me and kept the same way
// (runMemory, memory_lease.go).
//
// With no sandbox configured the deployment is unisolated: the file tools
// work in the conversation's directory on this host, stdio servers are host
// processes, and the shell is refused. The server says so when it starts.

// ToolShell is the tool that runs a command in the run's sandbox. An agent
// has it only when its spec names it, and only an isolated deployment runs
// it.
const ToolShell = "shell"

// Bounds on one shell command.
const (
	// ShellTimeout is how long a command may run.
	ShellTimeout = 2 * time.Minute
	// MaxShellOutput is how much of a command's output the model is shown:
	// the end of it, where the result and the errors usually are.
	MaxShellOutput = 32 << 10
)

// ErrUnisolated reports a tool this deployment does not run because it has
// no isolated sandbox.
var ErrUnisolated = errors.New("platform: this deployment runs no sandbox")

// runInstances is the claim each turn makes on an instance. A turn
// delegated inside another's has no run and no conversation, so its
// instance starts empty; a called run has a run of its own, so it never
// shares its caller's instance.
func (p *Platform) runInstances(_ context.Context, req runtime.Request) *isolation.Run {
	ir := isolation.Request{Tenant: req.Tenant, Run: req.RunID, Connectors: grantsToolServers(req), Egress: egressHosts(req)}
	if !p.cfg.Sandbox.Isolated() {
		return isolation.Unisolated(ir)
	}
	var files isolation.Files
	if req.RunID != "" {
		files = isolation.AllFiles(
			&runFiles{p: p, tenant: req.Tenant, run: req.RunID},
			&runMemory{p: p, tenant: req.Tenant, run: req.RunID},
		)
	}
	return isolation.NewRun(p.cfg.Sandbox.Leaser, ir, files)
}

// egressHosts are the hosts a turn's sandbox may reach: its pinned egress
// ceiling. A context that cannot be read reaches none.
func egressHosts(req runtime.Request) []string {
	c, err := requestContext(req)
	if err != nil {
		return []string{}
	}
	return c.Settings.Ceilings.EgressHosts()
}

// grantsToolServers reports whether a turn may start a tool server, so its
// instance needs the connectors compartment.
func grantsToolServers(req runtime.Request) bool {
	return slices.ContainsFunc(req.Definition.Spec.Tools, func(t string) bool { return strings.HasPrefix(t, "mcp:") })
}

// runFiles is a run's work directory's source: its conversation's scratch
// directory. Load copies the conversation's files into the instance; Save
// writes back what the run changed, and removes what it removed, under the
// same bounds an agent's own writes are held to. A file another run of the
// conversation changed meanwhile is overwritten only if this run changed it
// too.
type runFiles struct {
	p           *Platform
	tenant, run string

	mu     sync.Mutex
	thread string
	loaded map[string][sha256.Size]byte
}

func (f *runFiles) conversation() (string, error) {
	r, err := f.p.Runs.Get(f.tenant, f.run)
	if err != nil {
		return "", fmt.Errorf("%w: this run has no conversation", ErrSandbox)
	}
	if r.Thread != "" {
		return r.Thread, nil
	}
	return r.ID, nil
}

// Load implements isolation.Files.
func (f *runFiles) Load(ctx context.Context, inst isolation.Instance) error {
	dst := inst.Files()
	thread, err := f.conversation()
	if err != nil {
		return err
	}
	root, err := f.p.openSandbox(f.tenant, thread)
	if err != nil {
		return err
	}
	src := rootArea{root}
	defer func() { _ = src.close() }()
	files, err := src.list(ctx)
	if err != nil {
		return err
	}
	loaded := make(map[string][sha256.Size]byte, len(files))
	for _, file := range files {
		b, err := src.read(ctx, file.Path)
		if err != nil {
			continue // too large, or gone since it was listed
		}
		if err := dst.Write(ctx, file.Path, bytes.NewReader(b)); err != nil {
			return err
		}
		loaded[file.Path] = sha256.Sum256(b)
	}
	f.mu.Lock()
	f.thread, f.loaded = thread, loaded
	f.mu.Unlock()
	return nil
}

// Save implements isolation.Files.
func (f *runFiles) Save(ctx context.Context, inst isolation.Instance) error {
	src := inst.Files()
	f.mu.Lock()
	thread, loaded := f.thread, f.loaded
	f.mu.Unlock()
	if thread == "" {
		return nil // never loaded, so nothing to write back to
	}
	root, err := f.p.openSandbox(f.tenant, thread)
	if err != nil {
		return err
	}
	dst := rootArea{root}
	defer func() { _ = dst.close() }()
	files, err := (workArea{ws: src}).list(ctx)
	if err != nil {
		return err
	}
	var errs []error
	kept := map[string]bool{}
	for _, file := range files {
		kept[file.Path] = true
		clean, err := cleanPath(file.Path)
		if err != nil || clean != file.Path {
			errs = append(errs, fmt.Errorf("%w: %q was not kept: not a usable path", ErrSandbox, file.Path))
			continue
		}
		b, err := (workArea{ws: src}).read(ctx, clean)
		if err != nil {
			errs = append(errs, fmt.Errorf("%q was not kept: %w", clean, err))
			continue
		}
		if sum, ok := loaded[clean]; ok && sum == sha256.Sum256(b) {
			continue
		}
		if _, err := writeSandbox(ctx, dst, clean, string(b)); err != nil {
			errs = append(errs, fmt.Errorf("%q was not kept: %w", clean, err))
		}
	}
	for name := range loaded {
		if !kept[name] {
			_ = dst.remove(name)
		}
	}
	return errors.Join(errs...)
}

// workArea is the work directory of a run's instance.
type workArea struct{ ws workspace.Workspace }

func (workArea) close() error { return nil }

func (a workArea) list(ctx context.Context) ([]sandboxFile, error) {
	keys, err := a.ws.List(ctx, "")
	if err != nil {
		return nil, err
	}
	// Code in the instance can make any number of files. Past the most a
	// conversation may hold, the count is all a caller needs.
	if len(keys) > MaxSandboxFiles+1 {
		keys = keys[:MaxSandboxFiles+1]
	}
	files := make([]sandboxFile, 0, len(keys))
	for _, k := range keys {
		r, err := a.ws.Read(ctx, k)
		if err != nil {
			continue // gone, or no longer a regular file
		}
		n, _ := io.Copy(io.Discard, io.LimitReader(r, MaxSandboxBytes+1))
		_ = r.Close()
		files = append(files, sandboxFile{Path: k, Size: n})
	}
	return files, nil
}

func (a workArea) read(ctx context.Context, clean string) ([]byte, error) {
	r, err := a.ws.Read(ctx, clean)
	if err != nil {
		return nil, fmt.Errorf("%w: there is no file at %q", ErrSandbox, clean)
	}
	defer func() { _ = r.Close() }()
	b, err := io.ReadAll(io.LimitReader(r, MaxFileBytes+1))
	if err != nil {
		return nil, fmt.Errorf("%w: there is no file at %q", ErrSandbox, clean)
	}
	if len(b) > MaxFileBytes {
		return nil, fmt.Errorf("%w: %q is larger than %d bytes", ErrSandbox, clean, MaxFileBytes)
	}
	return b, nil
}

func (a workArea) write(ctx context.Context, clean, content string) error {
	// The workspace writes a new file and renames it into place, so a link
	// where the file goes is replaced, never written through.
	if err := a.ws.Write(ctx, clean, strings.NewReader(content)); err != nil {
		return fmt.Errorf("%w: %q cannot be written", ErrSandbox, clean)
	}
	return nil
}

// shellTool runs a command in the workload compartment of the run's
// instance. Unisolated, it refuses: a model's command never runs on the
// host.
func (p *Platform) shellTool(req runtime.Request, action func(event.Action) error) saigetypes.Tool {
	network := "The sandbox has no network and is destroyed when the run ends; "
	if hosts := egressHosts(req); p.cfg.Sandbox.Egress && len(hosts) > 0 {
		network = "The sandbox reaches only these hosts, on ports 80 and 443, through the proxy its environment names (HTTPS_PROXY), and refuses any other: " +
			strings.Join(hosts, ", ") + ". It is destroyed when the run ends; "
	}
	return &saigetypes.ToolFunc{
		Def: saigetypes.ToolDef{
			Name: ToolShell,
			Description: "Run a shell command (sh -c) in this run's sandbox, starting in the working directory the file tools use. " +
				network + "files in the working directory are kept for the conversation. " +
				fmt.Sprintf("A command may run for %s; the last %d bytes of its output are returned with its exit code.", ShellTimeout, MaxShellOutput),
			Parameters: schema([]string{"command"}, map[string]string{"command": "The command, run with sh -c"}),
		},
		Fn: func(ctx context.Context, args map[string]any) (string, error) {
			command := strings.TrimSpace(arg(args, "command"))
			if command == "" {
				return "", fmt.Errorf("%w: a command is required", ErrSandbox)
			}
			claim, ok := isolation.RunFrom(ctx)
			if !ok || !claim.Isolated() {
				return "", fmt.Errorf("%w: the shell runs only in an isolated sandbox, and this deployment runs tools on its own host", ErrUnisolated)
			}
			inst, err := claim.Instance(ctx)
			if err != nil {
				return "", fmt.Errorf("%w: the run's sandbox could not start: %v", ErrSandbox, err)
			}
			out := &tail{max: MaxShellOutput}
			st, err := inst.Exec(ctx, isolation.Command{Argv: []string{"sh", "-c", command}, Timeout: ShellTimeout, In: isolation.Workload},
				isolation.Stdio{Stdout: out, Stderr: out})
			if err != nil {
				return "", fmt.Errorf("%w: the command did not run: %v", ErrSandbox, err)
			}
			if action != nil {
				_ = action(event.Action{Name: ToolShell, Summary: fmt.Sprintf("Ran a command in the sandbox (exit %d)", st.Code)})
			}
			return fmt.Sprintf("exit %d\n%s", st.Code, out.String()), nil
		},
	}
}

// tail keeps the last max bytes written to it. Stdout and stderr share one,
// so it is safe for concurrent use.
type tail struct {
	mu      sync.Mutex
	max     int
	buf     []byte
	dropped bool
}

func (t *tail) Write(b []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, b...)
	if over := len(t.buf) - t.max; over > 0 {
		t.buf = append(t.buf[:0], t.buf[over:]...)
		t.dropped = true
	}
	return len(b), nil
}

func (t *tail) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.dropped {
		return "[earlier output cut]\n" + string(t.buf)
	}
	return string(t.buf)
}
