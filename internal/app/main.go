// Package app is the humagen command tree and the process's composition root:
// it turns a command line, files and an environment into a running platform.
// The module root package humagen is its public face, so a program that adds
// providers or stores imports it and passes options instead of forking.
package app

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/urmzd/humagen/fiber/pkg/event"
	"github.com/urmzd/humagen/internal/branding"
	"github.com/urmzd/humagen/internal/httpapi"
	"github.com/urmzd/humagen/internal/legacy"
	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/ready"
	"github.com/urmzd/humagen/internal/release"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/alerts"
	"github.com/urmzd/humagen/pkg/blob"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/credref"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/rooms"
	"github.com/urmzd/humagen/pkg/spec"
)

// Version is the build metadata `humagen version` reports.
type Version struct {
	Version, Commit, Date string
}

// Options is what a program built on humagen passes to Execute: its build
// metadata and what it adds to the platform. The humagen package fills it.
type Options struct {
	Version Version
	// Changelog is humagen's CHANGELOG.md, which the humagen package embeds. A
	// workspace that sets workspace.release_notes is posted the running
	// release's breaking changes and features from it.
	Changelog string
	// Ext is added to whatever a providers file declares. A name declared
	// by both is an error rather than an override.
	Ext platform.Extensions
	// Blobs replaces the store -storage selects.
	Blobs blob.Store
	// Connections seats connectors the program built itself.
	Connections []platform.Connection
	// Platform, when set, edits the platform's configuration last, after
	// flags and files. It is the escape hatch for a store or a seam no
	// option names yet.
	Platform func(*platform.Config) error
	// Kinds are the kinds a binary registered, by slot and name, that a
	// spec file selects (docs/reference/spec.md).
	Kinds map[string]map[string]Factory

	// ctx, ready and slackDone are for tests: the parent context, a call
	// with the address once the server listens, and a call when a Slack
	// app's supervisor returns.
	ctx       context.Context
	ready     func(addr string)
	slackDone func(tenant string, err error)
	// signals, for tests, stands for the process's SIGTERM and Ctrl-C.
	signals <-chan os.Signal
	// running, for tests, is called with the platform once it runs, in
	// every role.
	running func(*platform.Platform)

	// notes are said once each at start, about an old name in use.
	notes *legacy.Notes
}

const usage = `usage: humagen start [-f PATH] [flags]     run the platform here; no file needs no configuration
       humagen serve [-f PATH] [flags]     run it as a server: OIDC sign-in, nothing seeded, no questions
       humagen worker [-f PATH] [flags]    run only the turns on the shared run queue, with no API (needs -state=postgres)
       humagen deploy [-f PATH] [-o DIR]  run it where the spec's deploy says: local, compose or kubernetes
       humagen setup                      answer a few questions and write humagen.yaml
       humagen config check|print -f PATH check a spec, or print it with every default
       humagen config copy-keys           list the console's text branding.copy may replace
       humagen slack manifest [flags]     print the Slack app manifest
       humagen authz check FILE | authz explain [flags] PERMISSION
       humagen version`

// Execute runs the humagen command tree with args (os.Args[1:]) and returns the
// process exit code.
func Execute(o Options, args []string) int {
	return execute(o, args, sys{Environ: os.Environ, Setenv: os.Setenv, Stderr: os.Stderr, Arg0: os.Args[0]})
}

// sys is what execute reads of the process, so a test can give its own.
type sys struct {
	Environ func() []string
	Setenv  func(k, v string) error
	Stderr  io.Writer
	Arg0    string
}

// execute is Execute over s. Before any command runs, a setting under its
// old WARP_ name is copied to its HUMAGEN_ one and each old name in use is
// noted on stderr, so every command, and a program built on humagen, reads
// one set of names.
func execute(o Options, args []string, s sys) int {
	o.notes = legacy.NewNotes(s.Stderr)
	legacy.FromCommand(s.Arg0, o.notes)
	legacy.AdoptEnv(s.Environ(), s.Setenv, o.notes)
	err := run(o, args)
	// Help asked for is printed by the flag set, and is not a failure.
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if err != nil {
		fmt.Fprintln(s.Stderr, "humagen:", err)
		return 1
	}
	return 0
}

func run(o Options, args []string) error {
	if len(args) == 0 {
		return errors.New(usage)
	}
	switch args[0] {
	case "version":
		v := o.Version
		if v.Version == "" {
			v = Version{Version: "dev", Commit: "none", Date: "unknown"}
		}
		fmt.Printf("%s (%s, %s)\n", v.Version, v.Commit, v.Date)
		return nil
	case "authz":
		return runAuthz(args[1:], os.Stdout, os.Getenv)
	case "start":
		return start(o, args[1:], os.Stdin, os.Stdout)
	case "serve":
		// The server: what a container, a chart or a unit file runs. It
		// reads only the files -f names and never asks.
		return serve(o, args[1:], serveDefaults{})
	case "worker":
		// The worker role: the turns on the run queue, beside api
		// processes on the same database.
		return serve(o, args[1:], serveDefaults{role: platform.RoleWorker})
	case "deploy":
		return deployCmd(o, args[1:], os.Stdin, os.Stdout)
	case "setup":
		return setupCmd(o, args[1:], os.Stdin, os.Stdout)
	case "slack":
		return slackCmd(args[1:], os.Stdout)
	case "config":
		return configCmd(o, args[1:], os.Stdout)
	case "help", "-h", "-help", "--help":
		fmt.Println(usage)
		return nil
	default:
		return errors.New(usage)
	}
}

// serveDefaults are what a command changes of serve's own defaults, which
// are a server's: OIDC sign-in, nothing seeded, no file but -f. start signs
// in as the developer, seeds example agents and reads the spec in the config
// directory. A System document sets sign-in over either, and a flag wins
// over all of them.
type serveDefaults struct {
	// auth is -auth's default. Empty is oidc.
	auth string
	seed bool
	web  string
	// files are read when the command line names none.
	files []string
	// prepare, when set, runs once the command line is known to parse and
	// before any file is read: start's first-run questions. Help, or a flag
	// serve does not know, never reaches it. It runs before serve handles
	// Ctrl-C, so Ctrl-C at a question ends the process.
	prepare func() error
	// notes, when set, is where serve tells the person at a terminal what
	// it chose for them: that it found no console to serve.
	notes io.Writer
	// sandboxRuntime is -sandbox-runtime's default. Empty is runsc.
	sandboxRuntime string
	// sandboxInsecure is -sandbox-insecure-development-runtime's default:
	// whether a runtime that shares the host's kernel, such as runc, is
	// accepted.
	sandboxInsecure bool
	// role, when set, is the only role the command runs as: humagen worker
	// is the worker role. A spec's server.role does not change it, and a
	// different -role on the command line is refused.
	role platform.ProcessRole
}

func serve(o Options, args []string, d serveDefaults) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	addr := fs.String("addr", "127.0.0.1:8080", "listen address")
	webhookAddr := fs.String("webhook-addr", "", "optional separate listener exposing only signed connector webhooks")
	roleFlag := fs.String("role", string(platform.RoleAll), `which parts of the platform this process runs: "all", the API and the turns; "api", the API, executing no turns; or "worker", the turns on the run queue, serving nothing. api and worker share the queue, so they need -state=postgres`)
	authDefault := d.auth
	if authDefault == "" {
		authDefault = "oidc"
	}
	authMode := fs.String("auth", authDefault, `who may call: "oidc" (or "okta") for SSO; "tailscale" behind local Tailscale Serve; "mock" for the loopback development identity provider; "dev" for a loopback preview with no sign-in`)
	tenant := fs.String("tenant", "default", "tenant every caller is confined to, unless -tenant-claim is set")
	tenantClaim := fs.String("tenant-claim", "", "token claim that carries the caller's tenant")
	groupsClaim := fs.String("groups-claim", "groups", "token claim that lists a person's groups")
	groups := addGroupFlags(fs)
	storage := addStorageFlags(fs)
	sandboxing := addSandboxFlags(fs, d.sandboxRuntime, d.sandboxInsecure)
	stateKind := addStateFlags(fs, os.Getenv)
	mockAddr := fs.String("mock-idp-addr", "127.0.0.1:8081", "with -auth=mock, the loopback address the development identity provider listens on")
	mockPersonas := fs.String("mock-personas", "", `with -auth=mock, a JSON file of {"people": [...], "machines": [...]} to sign in as. Empty means the built-in set`)
	publicURL := fs.String("public-url", "http://localhost:8080", "where browsers reach this server")
	webDir := fs.String("web", d.web, "directory of a built web console to serve")
	maxDepth := fs.Int("max-depth", 4, "deepest an agent-to-agent call may land")
	maxFanOut := fs.Int("max-fan-out", 16, "most calls one run may make")
	seed := fs.Bool("seed", d.seed, "create example agents that need no model key")
	builtinProvider := fs.String("builtin-provider", "", `model provider for the agents the platform provides, such as "anthropic" or "openai". Empty means none are provided`)
	builtinModel := fs.String("builtin-model", "", "model for the agents the platform provides")
	personRefsFlag := fs.String("person-refs", "", `how the references each tenant's people may give for their own tool credentials start, with {subject} where each person's subject goes, as "tenant=op://People/{subject}/,keychain:humagen-people-{subject}/;tenant=env:HUMAGEN_PERSON_{subject}_". The server reads them with its own access, so a person may give only their own. A tenant not listed may reference nothing`)
	secretRefsFlag := fs.String("secret-refs", "", `environment variables each tenant's clients may reference as secrets, as "tenant=NAME,NAME;tenant=NAME". A tenant not listed may reference none`)
	agentSpecs := fs.String("agent-specs", "", "comma-separated agent JSON files to register on startup")
	var specFiles files
	fs.Var(&specFiles, "f", "a spec file or directory of System and Platform documents (docs/reference/spec.md); repeatable. Flags win over it")
	workspaceDir := fs.String("workspace", "", "directory agents write their files under. Empty means the user cache directory")
	concurrency := fs.Int("concurrency", 4, "how many runs execute at once; a run that calls another agent needs a second")
	roomCoalesce := fs.Int("room-coalesce-above", rooms.DefaultCoalesceAbove, "readers of one conversation on this server above which each is sent at once only what involves them, and the rest as a page")
	roomTail := fs.Duration("room-tail-interval", rooms.DefaultTailInterval, "how often a large conversation's page is sent")
	roomRate := fs.Int("room-max-runs-per-minute", rooms.DefaultRoomRunsPerMinute, "most runs one conversation starts in any minute")
	personRuns := fs.Int("room-max-runs-per-person", rooms.DefaultPersonRuns, "most runs one person has in flight at once")
	drainGrace := fs.Duration("drain-grace", spec.DefaultDrainGrace, "after SIGTERM, how long runs in flight may finish before what is left is handed back for another process to resume")
	shutdownTimeout := fs.Duration("shutdown-timeout", spec.DefaultShutdownTimeout, "once the drain is over, how long open requests and event streams may take to end")
	busKind := fs.String("bus", "", `the event bus: "memory", for one process, or "postgres", on the stores' database. Empty means postgres with -state=postgres, and memory otherwise`)
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	parent := o.ctx
	if parent == nil {
		parent = context.Background()
	}

	// The command line is checked before anything is asked, read or
	// written, so -h, or a flag misspelled, does only what parsing does.
	if probe(fs, args) != nil {
		return fs.Parse(args)
	}
	// The spec's System document becomes the flags' defaults, so a flag
	// typed on the command line wins over the file.
	paths := scanFiles(args)
	if len(paths) == 0 {
		paths = d.files
		// Asked before serve takes Ctrl-C for itself: a person who presses
		// it at a question has stopped humagen, and nothing is written.
		if d.prepare != nil {
			if err := d.prepare(); err != nil {
				return err
			}
		}
	}
	// The first signal stops a server that is still starting, and drains
	// one that is serving; a second stops it at once.
	ctx, stop := context.WithCancel(parent)
	defer stop()
	sigs := o.signals
	if sigs == nil {
		c := make(chan os.Signal, 2)
		signal.Notify(c, os.Interrupt, syscall.SIGTERM)
		defer signal.Stop(c)
		sigs = c
	}
	var serving atomic.Bool
	drainc := make(chan struct{})
	go func() {
		select {
		case <-sigs:
		case <-ctx.Done():
			return
		}
		if !serving.Load() {
			stop()
			return
		}
		close(drainc)
		select {
		case <-sigs:
			log.Warn("stopping now: runs still in flight are handed back")
			stop()
		case <-ctx.Done():
		}
	}()
	keys := credref.Default
	file, err := loadFiles(ctx, o, paths, keys)
	if err != nil {
		return err
	}
	if err := fs.Parse(append(file.args, args...)); err != nil {
		return err
	}
	role, err := processRole(*roleFlag, args, d.role)
	if err != nil {
		return err
	}
	serves := role.Serves()
	// A split role hands turns to other processes through the run queue,
	// which only a shared database holds.
	if role != platform.RoleAll && strings.TrimSpace(*stateKind.kind) != "postgres" {
		return fmt.Errorf("the %s role shares the run queue with other processes; set -state=postgres (or stores.kind: postgres) and $%s", role, postgresURLEnv)
	}
	// Branding's files are read now, so one that is missing fails the start
	// rather than a page.
	brand, err := branding.Load(file.cfg)
	if err != nil {
		return err
	}
	// Said once every flag is known: a console -web or the file names is
	// served, and one turned off was turned off on purpose.
	if d.notes != nil && serves && *webDir == "" && !given(fs, "web") {
		fmt.Fprintln(d.notes, "humagen: no built console found (run pnpm --filter @humagen/console build, or set HUMAGEN_WEB_DIR); serving the API only")
	}
	getenv := overlay(file.env, os.Getenv)
	// A Slack app calls this server's API, so it needs a credential for the
	// sign-in a flag may have chosen over the file's.
	// A worker serves no API, so it starts no Slack app.
	var slack []*slackApp
	if serves {
		slack = slackApps(file.cfg, keys, getenv)
	}
	for _, app := range slack {
		if err := app.choice.CheckSignIn(*authMode); err != nil {
			return fmt.Errorf("platform %q: surfaces.slack: %w", app.tenant, err)
		}
	}

	// Credentials come from the environment, never from flags: a flag ends up
	// in shell history and in ps output.
	pol, groupRoles, err := loadPolicy(fs, groups, getenv)
	if err != nil {
		return err
	}
	maps.Copy(groupRoles, file.roles)
	if *authMode == "oidc" || *authMode == "okta" {
		noteOldGroups(groupRoles, o.notes)
	}
	ac := authConfig{
		mode: *authMode, addr: *addr, publicURL: *publicURL,
		tenant: *tenant, tenantClaim: *tenantClaim, groups: *groupsClaim,
		policy: pol, groupRoles: groupRoles,
		mockAddr: *mockAddr, mockPersonas: *mockPersonas,
		getenv: getenv, log: log,
	}
	// Sign-in is for callers of the API, which a worker does not serve.
	var authn *httpapi.Authn
	if serves {
		var stopAuthn func()
		if authn, stopAuthn, err = newAuthn(ctx, ac); err != nil {
			return err
		}
		defer stopAuthn()
	}

	refs, err := secretRefs(*secretRefsFlag, *tenant, getenv)
	if err != nil {
		return err
	}
	// A variable a grant names that is set only under its old name is noted.
	for _, names := range refs {
		legacy.NoteNamed(os.LookupEnv, names, o.notes)
	}
	people, err := personRefs(*personRefsFlag)
	if err != nil {
		return err
	}
	blobs := o.Blobs
	if blobs == nil {
		if blobs, err = openBlobs(ctx, o, file, storage, getenv); err != nil {
			return err
		}
	}
	// Where each run's tools execute, and the egress gate's decisions,
	// audited once the platform is up.
	egressDecisions := newEgressLog(log)
	sbx, closeSandbox, err := openSandbox(ctx, o, file, sandboxing, log, egressDecisions)
	if err != nil {
		return err
	}
	defer closeSandbox()
	embedder, err := openEmbedder(ctx, o, file, getenv)
	if err != nil {
		return err
	}
	connectors, err := openConnectors(ctx, o, file.cfg)
	if err != nil {
		return err
	}
	// Model and tool providers: the spec's Platform documents for the common
	// cases, options for the rest. Keys are references, read when used.
	fromFile, err := fileExtensions(ctx, o, file.cfg, keys)
	if err != nil {
		return err
	}
	ext, err := fromFile.Merge(o.Ext)
	if err != nil {
		return err
	}
	if ext.Resolver == nil {
		ext.Resolver = keys.MCP()
	}
	// Every store, the agent registry and the run ledger: memory, or the one
	// Postgres the spec or $HUMAGEN_POSTGRES_URL names.
	backends, releaseState, checks, err := openState(ctx, stateKind, getenv, log, ext.ProviderNames()...)
	if err != nil {
		return err
	}
	defer releaseState()
	broker, err := openBus(ctx, o, file, *busKind, backends)
	if err != nil {
		return err
	}
	// A bus that can say whether it is connected is asked too.
	if b, ok := broker.(interface{ Ping(context.Context) error }); ok {
		checks = append(checks, ready.Check{Name: "bus", Probe: b.Ping})
	}
	gate := ready.New(log, checks...)
	elector, err := openLeader(stateKind, getenv, log)
	if err != nil {
		return err
	}
	cfg := platform.Config{
		Email:    emailConfiguration(file.cfg.System, *publicURL, keys),
		Wording:  brand.Wording,
		Backends: backends,
		// A client may keep its secrets in this process's environment, under
		// the names the operator gave its tenant.
		Env:        legacy.LookupEnv,
		SecretRefs: refs,
		// People's own tool credentials: references from where the
		// operator allows, values where the workspace keeps them.
		CredentialRefs:   people,
		CredentialValues: personValues(file.cfg),
		Limits:           call.Limits{MaxDepth: *maxDepth, MaxFanOut: *maxFanOut},
		Log:              log,
		BuiltinProvider:  *builtinProvider,
		BuiltinModel:     *builtinModel,
		Blobs:            blobs,
		Embedder:         embedder,
		Connectors:       connectors,
		Bus:              broker,
		Workspace:        *workspaceDir,
		Sandbox:          sbx,
		Concurrency:      *concurrency,
		Role:             role,
		Leader:           elector,
		// How a conversation with many readers is read and admitted.
		Rooms: rooms.Config{CoalesceAbove: *roomCoalesce, TailInterval: *roomTail, RoomRunsPerMinute: *roomRate, PersonRuns: *personRuns},
		Ext:   ext,
		// What a person a client's own connector maps holds: the roles
		// the policy grants their subject, as for every other caller.
		Policy: pol,
		// How long each workspace's inbox keeps what was settled.
		InboxRetention: inboxRetention(file.cfg),
		// The mention kinds each workspace declares.
		MentionKinds: mentionKinds(file.cfg),
	}
	if len(o.Connections) > 0 {
		conns := o.Connections
		cfg.Connections = func(wire.AgentStore) ([]platform.Connection, error) { return conns, nil }
	}
	if o.Platform != nil {
		if err := o.Platform(&cfg); err != nil {
			return err
		}
	}
	p, err := platform.New(cfg)
	if err != nil {
		return err
	}
	defer func() { _ = p.Close() }()
	go egressDecisions.run(ctx, p.AuditEgress)
	for _, path := range list(*agentSpecs) {
		raw, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("agent spec %s: %w", path, err)
		}
		var spec wire.AgentSpec
		if err := json.Unmarshal(raw, &spec); err != nil {
			return fmt.Errorf("agent spec %s: %w", path, err)
		}
		if spec.Model == "YOUR_MODEL_ID" {
			spec.Model = legacy.Getenv("HUMAGEN_AGENT_MODEL")
		}
		if spec.Model == "" {
			return fmt.Errorf("agent spec %s needs a model or HUMAGEN_AGENT_MODEL", path)
		}
		if err := checkNewAgentName(ctx, p, *tenant, spec.Name); err != nil {
			return fmt.Errorf("agent spec %s: %w", path, err)
		}
		if err := registerSpec(ctx, p.Agents, *tenant, spec); err != nil {
			return fmt.Errorf("agent spec %s: %w", path, err)
		}
	}
	if err := seedClients(ctx, p, *tenant, getenv); err != nil {
		return err
	}
	if *seed {
		if err := seedAgents(ctx, p, *tenant); err != nil {
			return err
		}
	}
	if err := applyPlatforms(ctx, p, file.cfg, log); err != nil {
		return err
	}
	postReleaseNotes(p, file.cfg, release.Running(o.Version.Version), o.Changelog, log)
	// After the agents, so an agent the file or the seed names keeps its
	// name and a person whose handle would be it gets a suffix.
	seeGranted(p, *tenant, ac.claimed(), pol, log)
	extensions, err := fileRunners(ctx, o, file.cfg)
	if err != nil {
		return err
	}

	log.Info("process", "role", role, "serves_api", serves, "executes_turns", role.Executes())
	errc := make(chan error, 8)
	ran := make(chan struct{})
	go func() { defer close(ran); errc <- p.Run(ctx) }()
	if o.running != nil {
		o.running(p)
	}
	// The listeners, sign-in's clients and the seed are the API's. A worker
	// runs the platform's loops, the turns among them, and the extensions,
	// and listens on nothing; it stops through the same path as a server.
	var srv, ingress *http.Server
	if serves {
		srv = &http.Server{
			Addr:              *addr,
			Handler:           (&httpapi.Server{P: p, Authn: authn, Log: log, WebDir: *webDir, Branding: brand, Ready: gate}).Handler(),
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       15 * time.Second,
			WriteTimeout:      30 * time.Second,
			IdleTimeout:       2 * time.Minute,
			MaxHeaderBytes:    1 << 16,
		}

		if *webhookAddr != "" {
			ingress = &http.Server{Addr: *webhookAddr, Handler: p.IngressHandler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 2 * time.Minute, MaxHeaderBytes: 1 << 16}
			go func() {
				log.Info("webhook ingress listening", "addr", *webhookAddr)
				if err := ingress.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
					errc <- err
				}
			}()
		}
		if *seed {
			go func() {
				// After the platform is running, so the conversation the files
				// belong to is a real one.
				select {
				case <-ctx.Done():
					return
				case <-time.After(500 * time.Millisecond):
				}
				if err := seedFiles(ctx, p, *tenant); err != nil {
					log.Warn("seed files", "error", err)
				}
			}()
		}
		ln, err := net.Listen("tcp", *addr)
		if err != nil {
			return err
		}
		go func() {
			log.Info("listening", "addr", ln.Addr().String(), "auth", *authMode, "tenant", *tenant)
			if err := srv.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
				errc <- err
			}
		}()
		if o.ready != nil {
			o.ready(ln.Addr().String())
		}
		// Each Slack app is a client of this server, started once it listens.
		// One that cannot start is logged and left off: the server, the console
		// and every other surface keep running without it.
		for _, app := range slack {
			go func() {
				err := app.run(ctx, "http://"+ln.Addr().String(), *publicURL, log)
				if err != nil {
					log.Error("slack app is off", "tenant", app.tenant, "error", err)
				}
				if o.slackDone != nil {
					o.slackDone(app.tenant, err)
				}
			}()
		}
	}
	runExtensions(ctx, extensions, errc)

	serving.Store(true)
	gate.Started()

	select {
	case err = <-errc:
	case <-ctx.Done():
	case <-drainc:
		drainPlatform(ctx, p, gate, *drainGrace, log)
	}
	shutdown, cancel := context.WithTimeout(context.Background(), *shutdownTimeout)
	defer cancel()
	if srv != nil {
		_ = srv.Shutdown(shutdown)
	}
	if ingress != nil {
		_ = ingress.Shutdown(shutdown)
	}
	// The platform writes what it has not written yet as it stops, so the
	// state it keeps is let go only after.
	stop()
	<-ran
	return err
}

// drainPlatform takes the process out of rotation and stops it taking new
// work, then waits up to grace, or until ctx ends, for the runs it holds to
// finish. What is still held when it returns is released as the platform
// stops, uncharged, for another process to resume.
func drainPlatform(ctx context.Context, p *platform.Platform, gate *ready.Gate, grace time.Duration, log *slog.Logger) {
	gate.Drain()
	p.Drain()
	log.Info("draining: no new turns; waiting for runs in flight (signal again to stop now)", "in_flight", p.InFlight(), "grace", grace)
	wait, cancel := context.WithTimeout(ctx, grace)
	defer cancel()
	if err := p.WaitIdle(wait); err != nil {
		log.Warn("drain grace is over: handing back runs still in flight", "in_flight", p.InFlight())
		return
	}
	log.Info("drained: no runs in flight")
}

// runExtensions runs each extension beside the platform until ctx ends. One
// that fails stops the process through errc.
func runExtensions(ctx context.Context, extensions []Extension, errc chan<- error) {
	for _, x := range extensions {
		go func() {
			if err := x.Run(ctx); err != nil && ctx.Err() == nil {
				errc <- err
			}
		}()
	}
}

// processRole is the role a process runs as: fixed, when the command is one
// role (humagen worker), else what -role or the spec says. A -role on the
// command line that contradicts the command is refused rather than ignored.
func processRole(flagValue string, args []string, fixed platform.ProcessRole) (platform.ProcessRole, error) {
	if fixed == "" {
		return platform.ParseProcessRole(flagValue)
	}
	if v, ok := commandLineFlag(args, "role"); ok && v != string(fixed) {
		return "", fmt.Errorf("-role=%s: this command runs the %s role; use humagen serve -role=%s", v, fixed, v)
	}
	return fixed, nil
}

// commandLineFlag is the value of -name as typed in args, before any "--".
func commandLineFlag(args []string, name string) (string, bool) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			break
		}
		if !strings.HasPrefix(a, "-") {
			continue
		}
		n, val, hasVal := strings.Cut(strings.TrimLeft(a, "-"), "=")
		if n != name {
			continue
		}
		if hasVal {
			return val, true
		}
		if i+1 < len(args) {
			return args[i+1], true
		}
		return "", true
	}
	return "", false
}

// seedFiles publishes a few example files, so the storage page has something
// on it before any agent with a model has run.
func seedFiles(ctx context.Context, p *platform.Platform, tenant string) error {
	if p.Artifacts.Usage(tenant).Count > 0 {
		return nil
	}
	run, err := p.Converse(ctx, tenant, platform.Turn{
		ID: "seed-files", Agent: "lead", Text: "Draft the launch checklist", Project: "Launch",
		Actor: event.Actor{ID: "seed", Display: "Seed", Bot: true},
	})
	if err != nil {
		return err
	}
	for _, f := range []struct{ path, content, description string }{
		{"launch-checklist.md", "# Launch checklist\n\n- [ ] Freeze the release branch\n- [ ] Run the smoke tests\n", "The first draft"},
		{"launch-checklist.md", "# Launch checklist\n\n- [x] Freeze the release branch\n- [ ] Run the smoke tests\n- [ ] Tell support\n", "With support added"},
		{"signups.csv", "week,signups\n1,120\n2,164\n3,210\n", "Signups by week"},
	} {
		if err := p.PutFile(tenant, run.Thread, f.path, f.content); err != nil {
			return err
		}
		if _, err := p.Publish(ctx, platform.Publication{
			Tenant: tenant, Thread: run.Thread, Run: run.ID, Agent: "lead", Project: "Launch", By: "seed",
			Path: f.path, Description: f.description,
		}); err != nil {
			return err
		}
	}
	return nil
}

// loopback reports whether addr can only be reached from this machine.
func loopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// seedAgents creates two agents on fiber's offline provider, so the console
// has something to run before a model key is configured.
func seedAgents(ctx context.Context, p *platform.Platform, tenant string) error {
	for _, spec := range []wire.AgentSpec{
		{
			Name: "lead", Description: "Takes a request and may call the helper.",
			Model: "offline", Provider: "offline", SystemPrompt: "You coordinate work.",
			Access: wire.Access{Spawn: []string{"helper"}},
		},
		{
			Name: "helper", Description: "Does one piece of work for the lead.",
			Model: "offline", Provider: "offline", SystemPrompt: "You do one task well.",
		},
	} {
		_, err := p.Agents.Create(ctx, tenant, spec, "seed")
		if err != nil && !errors.Is(err, wire.ErrExists) {
			return fmt.Errorf("seed %s: %w", spec.Name, err)
		}
	}
	// The lead answers a message that tags no agent. A workspace kept from
	// an earlier start is left as it is.
	tree := p.ProjectTree(tenant)
	if cfg := p.Settings(tenant); tree.Root().Version > 0 || tree.Len() > 0 || !cfg.SetupAt.IsZero() {
		return nil
	}
	if _, err := p.SetProjectAgents(ctx, tenant, projects.RootAddress, projects.Writer{By: "seed", Govern: true}, platform.AgentsInput{
		Version: tree.Root().Version, Enable: []string{projects.Everyone}, Default: "lead",
	}); err != nil {
		return fmt.Errorf("seed the workspace default: %w", err)
	}
	if _, err := p.CreateProject(ctx, tenant, projects.Writer{By: "seed"}, platform.ProjectInput{
		Name: "Launch", Description: "Everything for the Q4 launch",
	}); err != nil && !errors.Is(err, projects.ErrExists) {
		return fmt.Errorf("seed project: %w", err)
	}
	// One rule, so a failure shows up somewhere before anyone configures alerts.
	err := p.Alerts.AddRule(tenant, alerts.Rule{Name: "any-run-fails", Kind: alerts.RunFailed, Severity: alerts.Critical, CreatedBy: "seed"})
	if err != nil && !errors.Is(err, alerts.ErrExists) {
		return fmt.Errorf("seed alert rule: %w", err)
	}
	if strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY")) == "" {
		slog.Default().Info("seeded agents use the offline provider and call no model")
	}
	return nil
}
