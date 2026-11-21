package platform

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/urmzd/saige/flow/isolation"
	"github.com/urmzd/saige/sandbox/isolate"

	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/runs"
)

// A command a person runs by hand in a conversation's sandbox.
//
// It runs where an agent's shell runs and nowhere else: in an instance the
// deployment's sandbox starts for this one command, with the conversation's
// files in its work directory, no network, no memory and no tool servers.
// What the command changed is written back under the rules a run's copyback
// follows, and the instance is destroyed. Nothing of it reaches an agent:
// the console records it and sends it with the person's next message.
//
// A deployment runs it only when its sandbox is contained
// (Config.SandboxContained). The zero value refuses, as do an unisolated
// deployment and the local sandbox, whose instances are processes on this
// host: a person's command never runs on the server.

// Bounds on one command a person runs. Together they stay inside the HTTP
// server's 30 second answer.
const (
	// PersonCommandTimeout is how long the command itself may run.
	PersonCommandTimeout = 15 * time.Second
	// personCommandStart is how long its instance may take to start and
	// take the conversation's files.
	personCommandStart = 8 * time.Second
	// personCommandSave is how long writing its files back may take.
	personCommandSave = 5 * time.Second
	// MaxPersonCommand is the longest command, in bytes.
	MaxPersonCommand = 2000
	// MaxPersonCommandOutput is how much of the output is answered: the
	// end of it.
	MaxPersonCommandOutput = 16 << 10
	// MaxPersonCommands is how many commands one workspace runs at once.
	MaxPersonCommands = 4
	// maxPersonCommandFiles is how many changed files one answer names.
	maxPersonCommandFiles = 50
)

// ErrSandboxUnavailable reports a sandbox that could not start an instance.
var ErrSandboxUnavailable = errors.New("platform: the sandbox is unavailable")

// PersonCommand is what a person asks to run.
type PersonCommand struct {
	Command string `json:"command"`
}

// PersonCommandResult is how a person's command came out.
type PersonCommandResult struct {
	Command string `json:"command"`
	// ExitCode is the command's exit status, or -1 when it was stopped at
	// the time limit.
	ExitCode int  `json:"exit_code"`
	TimedOut bool `json:"timed_out"`
	// Output is the end of stdout and stderr, as one stream.
	Output     string `json:"output"`
	Truncated  bool   `json:"truncated"`
	DurationMS int64  `json:"duration_ms"`
	// Files are the conversation's files the command changed or removed.
	Files []runs.SandboxFile `json:"files"`
	// More counts changed files past the ones named.
	More int `json:"more,omitempty"`
	// Conflicts are changes that were not kept, and why.
	Conflicts []string `json:"conflicts"`
}

// personCommands counts the commands in flight: one per conversation, and
// at most MaxPersonCommands per tenant.
type personCommands struct {
	mu       sync.Mutex
	threads  map[string]bool
	tenants  map[string]int
	initOnce sync.Once
	// timeout stands in for PersonCommandTimeout in a test.
	timeout time.Duration
}

func (c *personCommands) enter(tenant, thread string) (func(), error) {
	c.initOnce.Do(func() { c.threads, c.tenants = map[string]bool{}, map[string]int{} })
	key := tenant + "\x00" + thread
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.threads[key] {
		return nil, fmt.Errorf("%w: a command is already running in this conversation", ErrWorkspaceConflict)
	}
	if c.tenants[tenant] >= MaxPersonCommands {
		return nil, fmt.Errorf("%w: this workspace is already running %d commands; try again when one ends", ErrWorkspaceConflict, MaxPersonCommands)
	}
	c.threads[key] = true
	c.tenants[tenant]++
	return func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		delete(c.threads, key)
		if c.tenants[tenant]--; c.tenants[tenant] <= 0 {
			delete(c.tenants, tenant)
		}
	}, nil
}

// PersonCommands reports whether a person may run commands in this
// deployment's sandbox at all: it starts instances, and they are contained.
func (p *Platform) PersonCommands() bool {
	return p.cfg.SandboxContained && p.cfg.Sandbox.Isolated()
}

// RunPersonCommand runs one command for who in the conversation's sandbox
// and answers how it came out. It refuses a conversation who may not post
// in, one with work under way, and a deployment whose sandbox is not
// contained.
func (p *Platform) RunPersonCommand(ctx context.Context, who auth.Principal, id string, in PersonCommand) (PersonCommandResult, error) {
	if !who.Can(auth.RunsWrite) {
		return PersonCommandResult{}, auth.ErrForbidden
	}
	th, err := p.workspaceThread(who, id)
	if err != nil {
		return PersonCommandResult{}, err
	}
	command := strings.TrimSpace(in.Command)
	switch {
	case command == "":
		return PersonCommandResult{}, fmt.Errorf("%w: a command is required", ErrSandbox)
	case len(command) > MaxPersonCommand:
		return PersonCommandResult{}, fmt.Errorf("%w: a command is at most %d bytes", ErrSandbox, MaxPersonCommand)
	case !utf8.ValidString(command) || strings.ContainsRune(command, 0):
		return PersonCommandResult{}, fmt.Errorf("%w: a command is text", ErrSandbox)
	}
	if !p.PersonCommands() {
		return PersonCommandResult{}, fmt.Errorf("%w: a person's command runs only in an isolated sandbox", ErrUnisolated)
	}
	mu := p.moves.of(who.Tenant, id)
	mu.RLock()
	err = p.workspaceMayWrite(ctx, who, th)
	mu.RUnlock()
	if err != nil {
		return PersonCommandResult{}, err
	}
	leave, err := p.personCommands.enter(who.Tenant, id)
	if err != nil {
		return PersonCommandResult{}, err
	}
	defer leave()

	files := &runFiles{p: p, tenant: who.Tenant, of: id}
	// A person's command runs in the image the session's runs do.
	image, err := p.imageOf(who.Tenant, id)
	if err != nil {
		return PersonCommandResult{}, err
	}
	claim := isolation.NewRun(p.cfg.Sandbox.Leaser, isolation.Request{Tenant: who.Tenant, Egress: []string{}, Image: image}, files)
	released := false
	release := func() error {
		if released {
			return nil
		}
		released = true
		// The files are written back and the instance destroyed even when
		// the caller has gone.
		sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), personCommandSave)
		defer cancel()
		return claim.Release(sctx)
	}
	defer func() { _ = release() }()

	lctx, cancel := context.WithTimeout(ctx, personCommandStart)
	inst, err := claim.Instance(lctx)
	cancel()
	if err != nil {
		if ctx.Err() != nil {
			return PersonCommandResult{}, ctx.Err()
		}
		p.log.Warn("platform: a person's command had no sandbox", "tenant", who.Tenant, "thread", id, "error", err)
		return PersonCommandResult{}, fmt.Errorf("%w: it could not start an instance for this command", ErrSandboxUnavailable)
	}

	limit := PersonCommandTimeout
	if p.personCommands.timeout > 0 {
		limit = p.personCommands.timeout
	}
	out := &tail{max: MaxPersonCommandOutput}
	res := PersonCommandResult{Command: command, Files: []runs.SandboxFile{}, Conflicts: []string{}}
	started := p.now()
	ectx, cancel := context.WithTimeout(ctx, limit+time.Second)
	st, err := inst.Exec(ectx, isolation.Command{Argv: []string{"sh", "-c", command}, Timeout: limit, In: isolation.Workload},
		isolation.Stdio{Stdout: out, Stderr: out})
	timedOut := errors.Is(err, isolate.ErrTimeout) || errors.Is(err, context.DeadlineExceeded) || errors.Is(ectx.Err(), context.DeadlineExceeded)
	cancel()
	res.DurationMS = p.now().Sub(started).Milliseconds()
	switch {
	case err == nil:
		res.ExitCode = st.Code
	case ctx.Err() != nil:
		return PersonCommandResult{}, ctx.Err()
	case timedOut:
		res.ExitCode, res.TimedOut = -1, true
	default:
		p.log.Warn("platform: a person's command did not run", "tenant", who.Tenant, "thread", id, "error", err)
		return PersonCommandResult{}, fmt.Errorf("%w: the command did not run", ErrSandboxUnavailable)
	}
	out.mu.Lock()
	res.Output, res.Truncated = strings.ToValidUTF8(string(out.buf), "�"), out.dropped
	out.mu.Unlock()

	// What did not copy back is said in Conflicts; the command still ran.
	saveErr := release()
	files.mu.Lock()
	wrote, refused := files.wrote, files.refused
	files.mu.Unlock()
	if len(wrote) > maxPersonCommandFiles {
		res.More, wrote = len(wrote)-maxPersonCommandFiles, wrote[:maxPersonCommandFiles]
	}
	res.Files = append(res.Files, wrote...)
	res.Conflicts = append(res.Conflicts, refused...)
	if saveErr != nil && len(refused) == 0 {
		res.Conflicts = append(res.Conflicts, "some of what the command wrote was not kept: it is outside what a conversation may hold")
	}
	p.log.Info("platform: a person ran a command in the sandbox", "tenant", who.Tenant, "subject", who.Subject, "thread", id,
		"exit", res.ExitCode, "timed_out", res.TimedOut, "duration_ms", res.DurationMS, "files", len(res.Files))
	return res, nil
}
