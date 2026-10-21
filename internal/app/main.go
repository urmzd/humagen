// Package app is the warp command tree and the process's composition root:
// it turns a command line, files and an environment into a running platform.
// warpapp is its public face, so a program that adds providers or stores
// imports warpapp and passes options instead of forking this package.
package app

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/urmzd/fiber/pkg/agentspec"
	"github.com/urmzd/fiber/pkg/envelope"
	"github.com/urmzd/fiber/pkg/registry"

	"github.com/urmzd/warp/internal/httpapi"
	"github.com/urmzd/warp/internal/platform"
	"github.com/urmzd/warp/pkg/alerts"
	"github.com/urmzd/warp/pkg/blob"
	"github.com/urmzd/warp/pkg/call"
	"github.com/urmzd/warp/pkg/credref"
)

// Version is the build metadata `warp version` reports.
type Version struct {
	Version, Commit, Date string
}

// Options is what a program built on warp passes to Execute: its build
// metadata and what it adds to the platform. warpapp fills it.
type Options struct {
	Version Version
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

	// ctx and ready are for tests: the parent context, and a call with the
	// address once the server listens.
	ctx   context.Context
	ready func(addr string)
}

const usage = `usage: warp start [flags]              run the platform with zero configuration
       warp serve [flags]              run the platform from flags and files
       warp setup [flags]              choose providers, auth and Slack, and write them down
       warp slack manifest [flags]     print the Slack app manifest
       warp authz check FILE | authz explain [flags] PERMISSION
       warp version`

// Execute runs the warp command tree with args (os.Args[1:]) and returns the
// process exit code.
func Execute(o Options, args []string) int {
	if err := run(o, args); err != nil {
		fmt.Fprintln(os.Stderr, "warp:", err)
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
	case "serve":
		return serve(o, args[1:], serveDefaults{auth: "oidc"})
	case "start":
		return start(o, args[1:], os.Stdin, os.Stdout)
	case "setup":
		return setupCmd(args[1:], os.Stdin, os.Stdout)
	case "slack":
		return slackCmd(args[1:], os.Stdout)
	case "help", "-h", "-help", "--help":
		fmt.Println(usage)
		return nil
	default:
		return errors.New(usage)
	}
}

// serveDefaults are the flag defaults a command starts from: serve's are
// production's, start's are a laptop's.
type serveDefaults struct {
	auth      string
	seed      bool
	web       string
	providers string
	// slack, when set, is started beside the server and stopped with it.
	slack *slackApp
}

func serve(o Options, args []string, d serveDefaults) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	addr := fs.String("addr", "127.0.0.1:8080", "listen address")
	webhookAddr := fs.String("webhook-addr", "", "optional separate listener exposing only signed connector webhooks")
	authMode := fs.String("auth", d.auth, `who may call: "oidc" (or "okta") for SSO; "tailscale" behind local Tailscale Serve; "mock" for the loopback development identity provider; "dev" for a loopback preview with no sign-in`)
	tenant := fs.String("tenant", "default", "tenant every caller is confined to, unless -tenant-claim is set")
	tenantClaim := fs.String("tenant-claim", "", "token claim that carries the caller's tenant")
	groupsClaim := fs.String("groups-claim", "groups", "token claim that lists a person's groups")
	groups := addGroupFlags(fs)
	storage := addStorageFlags(fs)
	mockAddr := fs.String("mock-idp-addr", "127.0.0.1:8081", "with -auth=mock, the loopback address the development identity provider listens on")
	mockPersonas := fs.String("mock-personas", "", `with -auth=mock, a JSON file of {"people": [...], "machines": [...]} to sign in as. Empty means the built-in set`)
	publicURL := fs.String("public-url", "http://localhost:8080", "where browsers reach this server")
	webDir := fs.String("web", d.web, "directory of a built web console to serve")
	maxDepth := fs.Int("max-depth", 4, "deepest an agent-to-agent call may land")
	maxFanOut := fs.Int("max-fan-out", 16, "most calls one run may make")
	seed := fs.Bool("seed", d.seed, "create example agents that need no model key")
	builtinProvider := fs.String("builtin-provider", "", `model provider for the agents the platform provides, such as "anthropic" or "ollama". Empty means none are provided`)
	builtinModel := fs.String("builtin-model", "", "model for the agents the platform provides")
	secretRefsFlag := fs.String("secret-refs", "", `environment variables each tenant's clients may reference as secrets, as "tenant=NAME,NAME;tenant=NAME". A tenant not listed may reference none`)
	agentSpecs := fs.String("agent-specs", "", "comma-separated agent JSON files to register on startup")
	providersPath := fs.String("providers", d.providers, "JSON file of model providers and MCP tool providers (see docs/providers.md)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	parent := o.ctx
	if parent == nil {
		parent = context.Background()
	}
	ctx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Credentials come from the environment, never from flags: a flag ends up
	// in shell history and in ps output.
	pol, groupRoles, err := loadPolicy(fs, groups, os.Getenv)
	if err != nil {
		return err
	}
	authn, stopAuthn, err := newAuthn(ctx, authConfig{
		mode: *authMode, addr: *addr, publicURL: *publicURL,
		tenant: *tenant, tenantClaim: *tenantClaim, groups: *groupsClaim,
		policy: pol, groupRoles: groupRoles,
		mockAddr: *mockAddr, mockPersonas: *mockPersonas,
		getenv: os.Getenv, log: log,
	})
	if err != nil {
		return err
	}
	defer stopAuthn()

	refs, err := secretRefs(*secretRefsFlag, *tenant, os.Getenv)
	if err != nil {
		return err
	}
	blobs := o.Blobs
	if blobs == nil {
		if blobs, err = openBlobs(ctx, storage, os.Getenv); err != nil {
			return err
		}
	}
	// Model and tool providers: a file for the common cases, options for the
	// rest. Keys are named in the file and read when they are used.
	file, err := ReadProvidersFile(*providersPath, d.providers != "" && *providersPath == d.providers)
	if err != nil {
		return err
	}
	ext, err := file.Extensions(*tenant, credref.Default).Merge(o.Ext)
	if err != nil {
		return err
	}
	if ext.Resolver == nil {
		ext.Resolver = credref.Default.MCP()
	}
	cfg := platform.Config{
		// A client may keep its secrets in this process's environment, under
		// the names the operator gave its tenant.
		Env:             os.LookupEnv,
		SecretRefs:      refs,
		Limits:          call.Limits{MaxDepth: *maxDepth, MaxFanOut: *maxFanOut},
		Log:             log,
		BuiltinProvider: *builtinProvider,
		BuiltinModel:    *builtinModel,
		Blobs:           blobs,
		Ext:             ext,
	}
	if len(o.Connections) > 0 {
		conns := o.Connections
		cfg.Connections = func(registry.Store) ([]platform.Connection, error) { return conns, nil }
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
	for _, path := range list(*agentSpecs) {
		raw, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("agent spec %s: %w", path, err)
		}
		var spec agentspec.AgentSpec
		if err := json.Unmarshal(raw, &spec); err != nil {
			return fmt.Errorf("agent spec %s: %w", path, err)
		}
		if spec.Model == "YOUR_MODEL_ID" {
			spec.Model = os.Getenv("WARP_AGENT_MODEL")
		}
		if spec.Model == "" {
			return fmt.Errorf("agent spec %s needs a model or WARP_AGENT_MODEL", path)
		}
		if _, err := p.Agents.Create(ctx, *tenant, spec, "startup"); err != nil {
			return fmt.Errorf("agent spec %s: %w", path, err)
		}
	}
	if err := seedClients(ctx, p, *tenant, os.Getenv); err != nil {
		return err
	}
	if *seed {
		if err := seedAgents(ctx, p, *tenant); err != nil {
			return err
		}
	}

	srv := &http.Server{
		Addr:              *addr,
		Handler:           (&httpapi.Server{P: p, Authn: authn, Log: log, WebDir: *webDir}).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    1 << 16,
	}

	errc := make(chan error, 4)
	var ingress *http.Server
	if *webhookAddr != "" {
		ingress = &http.Server{Addr: *webhookAddr, Handler: p.IngressHandler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 2 * time.Minute, MaxHeaderBytes: 1 << 16}
		go func() {
			log.Info("webhook ingress listening", "addr", *webhookAddr)
			if err := ingress.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
				errc <- err
			}
		}()
	}
	go func() { errc <- p.Run(ctx) }()
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
	if d.slack != nil {
		// The Slack app is a client of this server, started once it listens.
		go func() {
			if err := d.slack.run(ctx, "http://"+ln.Addr().String(), *tenant, *publicURL, log); err != nil {
				errc <- fmt.Errorf("slack: %w", err)
			}
		}()
	}

	select {
	case err = <-errc:
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdown)
	if ingress != nil {
		_ = ingress.Shutdown(shutdown)
	}
	return err
}

// seedFiles publishes a few example files, so the storage page has something
// on it before any agent with a model has run.
func seedFiles(ctx context.Context, p *platform.Platform, tenant string) error {
	if p.Artifacts.Usage(tenant).Count > 0 {
		return nil
	}
	run, err := p.Converse(ctx, tenant, platform.Turn{
		ID: "seed-files", Agent: "lead", Text: "Draft the launch checklist", Project: "Launch",
		Actor: envelope.Actor{ID: "seed", Display: "Seed", Bot: true},
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
	for _, spec := range []agentspec.AgentSpec{
		{
			Name: "lead", Description: "Takes a request and may call the helper.",
			Model: "offline", Provider: "offline", SystemPrompt: "You coordinate work.",
			Access: agentspec.Access{Spawn: []string{"helper"}},
		},
		{
			Name: "helper", Description: "Does one piece of work for the lead.",
			Model: "offline", Provider: "offline", SystemPrompt: "You do one task well.",
		},
	} {
		_, err := p.Agents.Create(ctx, tenant, spec, "seed")
		if err != nil && !errors.Is(err, registry.ErrExists) {
			return fmt.Errorf("seed %s: %w", spec.Name, err)
		}
	}
	// The lead answers when nobody picked an agent and none matched.
	if err := p.SetSettings(ctx, tenant, platform.Settings{
		Baseline: "lead",
		Projects: []platform.Project{{Name: "Launch", Description: "Everything for the Q4 launch"}},
	}); err != nil {
		return fmt.Errorf("seed baseline: %w", err)
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
