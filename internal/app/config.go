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
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/urmzd/fiber/pkg/agentrt"
	"github.com/urmzd/fiber/pkg/agentrt/payload"
	"github.com/urmzd/fiber/pkg/agentspec"
	"github.com/urmzd/fiber/pkg/bus"
	"github.com/urmzd/fiber/pkg/registry"
	"github.com/urmzd/fiber/pkg/runtime"
	"github.com/urmzd/fiber/pkg/toolprovider"

	"github.com/urmzd/humagen/internal/legacy"
	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/credref"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/spec"
)

// Factory builds a registered kind from its block. What it returns depends on
// the slot: a blob.Store for blobs, a bus.Bus for bus, an
// agentrt.ProviderFunc for models, a runtime.Catalog for tools, and an
// Extension for extensions.
type Factory func(ctx context.Context, block json.RawMessage) (any, error)

// Extension is a process-wide part a binary registered, run beside the
// server until ctx ends.
type Extension interface {
	Run(ctx context.Context) error
}

// kinds is the registered kinds as the spec checks them.
func (o Options) kinds() spec.Kinds {
	k := spec.Kinds{}
	for slot, byName := range o.Kinds {
		k[slot] = slices.Sorted(maps.Keys(byName))
	}
	return k
}

// build makes a registered kind and checks it is what its slot takes.
func build[T any](ctx context.Context, o Options, slot string, s spec.Slot) (T, error) {
	var zero T
	f := o.Kinds[slot][s.Kind]
	if f == nil {
		return zero, fmt.Errorf("%s: kind %q is not registered", slot, s.Kind)
	}
	v, err := f(ctx, s.Block)
	if err != nil {
		return zero, fmt.Errorf("%s %s: %w", slot, s.Kind, err)
	}
	t, ok := v.(T)
	if !ok {
		return zero, fmt.Errorf("%s %s: the factory returned %T, which a %s slot does not take", slot, s.Kind, v, slot)
	}
	return t, nil
}

// files is a repeatable -f.
type files []string

func (f *files) String() string     { return strings.Join(*f, ",") }
func (f *files) Set(v string) error { *f = append(*f, v); return nil }

// scanFiles finds the -f values in args before they are parsed, since what
// the files say becomes the other flags' defaults.
func scanFiles(args []string) []string {
	var out []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			break
		}
		name, val, hasVal := strings.Cut(strings.TrimLeft(a, "-"), "=")
		if !strings.HasPrefix(a, "-") || name != "f" {
			continue
		}
		if hasVal {
			out = append(out, val)
		} else if i+1 < len(args) {
			out = append(out, args[i+1])
			i++
		}
	}
	return out
}

// loaded is what the files said, resolved for serve.
type loaded struct {
	cfg spec.Config
	// args are the System's flags, put ahead of the command line's so a flag
	// typed wins.
	args []string
	// env are values the server reads from its environment, here from the
	// file's references, resolved at start.
	env map[string]string
	// roles maps more identity provider groups to roles than the flags can.
	roles map[string]auth.Role
	// any reports whether any document was read.
	any bool
}

// loadFiles reads paths and turns the System document into serve's terms.
func loadFiles(ctx context.Context, o Options, paths []string, keys credref.Resolver) (loaded, error) {
	l := loaded{env: map[string]string{}, roles: map[string]auth.Role{}}
	if len(paths) == 0 {
		return l, nil
	}
	cfg, err := loadSpec(o, paths...)
	if err != nil {
		return l, err
	}
	if err := cfg.Check(o.kinds()); err != nil {
		return l, err
	}
	l.cfg, l.any = cfg, true
	// The stores' default comes from $HUMAGEN_POSTGRES_URL, so the file sets
	// -state only when it says.
	declaredStores := cfg.System.Spec.Stores.Kind
	sys := cfg.System
	sys.Defaults()
	sp := sys.Spec
	set := func(name, v string) {
		if v != "" {
			l.args = append(l.args, "-"+name+"="+v)
		}
	}
	secret := func(env string, r *spec.Ref) error {
		if !r.Set() {
			return nil
		}
		v, err := keys.Resolve(ctx, r.Ref)
		if err != nil {
			return fmt.Errorf("%s: %w", env, err)
		}
		l.env[env] = v
		return nil
	}
	set("addr", sp.Server.Addr)
	set("public-url", sp.Server.PublicURL)
	set("webhook-addr", sp.Server.WebhookAddr)
	if !sp.Server.Console.On() {
		l.args = append(l.args, "-web=")
	} else if d := sp.Server.Console.Dir; d != "" && d != "auto" {
		set("web", d)
	}

	// A System document sets sign-in, dev when it names none, as it sets
	// every field. Platform documents alone leave the command's own: dev for
	// start, oidc for serve.
	if !reflect.DeepEqual(cfg.System, spec.System{}) {
		set("auth", sp.Auth.Kind)
	}
	switch sp.Auth.Kind {
	case "oidc":
		b, _ := spec.Block[spec.OIDC](sp.Auth.Slot)
		l.env["HUMAGEN_OIDC_ISSUER"], l.env["HUMAGEN_OIDC_CLIENT_ID"], l.env["HUMAGEN_OIDC_AUDIENCE"] = b.Issuer, b.ClientID, b.Audience
		if err := secret("HUMAGEN_OIDC_CLIENT_SECRET", b.ClientSecret); err != nil {
			return l, err
		}
	case "mock":
		b, _ := spec.Block[spec.Mock](sp.Auth.Slot)
		set("mock-idp-addr", b.Addr)
		set("mock-personas", b.Personas)
	}
	if t := sp.Auth.Tenant; t.Claim != "" {
		set("tenant-claim", t.Claim)
	} else {
		set("tenant", t.Fixed)
	}
	set("groups-claim", sp.Auth.Groups.Claim)
	for role, groups := range sp.Auth.Groups.Roles {
		r := auth.Role(role)
		if !slices.Contains([]auth.Role{auth.Admin, auth.Manager, auth.Operator, auth.Viewer}, r) {
			return l, fmt.Errorf("system: auth.groups.roles: %q is not a role (admin, manager, operator, viewer)", role)
		}
		for _, g := range groups {
			l.roles[g] = r
		}
	}
	if sp.Auth.PolicyFile != "" {
		l.env[policyEnv] = sp.Auth.PolicyFile
	}

	switch sp.Blobs.Kind {
	case "disk":
		b, _ := spec.Block[spec.Disk](sp.Blobs)
		set("storage", "disk")
		set("storage-dir", b.Dir)
	case "memory":
		set("storage", "memory")
	case "s3":
		b, _ := spec.Block[spec.S3](sp.Blobs)
		set("storage", "s3")
		set("s3-bucket", b.Bucket)
		set("s3-region", b.Region)
		set("s3-endpoint", b.Endpoint)
		set("s3-prefix", b.Prefix)
		if b.PathStyle {
			set("s3-path-style", "true")
		}
		if err := secret(s3AccessEnv, b.AccessKey); err != nil {
			return l, err
		}
		if err := secret(s3SecretEnv, b.SecretKey); err != nil {
			return l, err
		}
	}
	if declaredStores != "" {
		set("state", declaredStores)
	}
	if sp.Stores.Kind == "postgres" {
		b, _ := spec.Block[spec.URLBlock](sp.Stores)
		if err := secret(envPostgres, b.URL); err != nil {
			return l, err
		}
	}
	set("bus", sp.Bus.Kind)
	if sp.Bus.Kind == "redis" {
		b, _ := spec.Block[spec.URLBlock](sp.Bus)
		if err := secret(envRedis, b.URL); err != nil {
			return l, err
		}
	}

	set("workspace", sp.Execution.Workspace)
	set("concurrency", strconv.Itoa(sp.Execution.Concurrency))
	set("max-depth", strconv.Itoa(sp.Execution.MaxDepth))
	set("max-fan-out", strconv.Itoa(sp.Execution.MaxFanOut))
	set("builtin-provider", sp.Execution.Builtin.Provider)
	set("builtin-model", sp.Execution.Builtin.Model)
	if len(sp.SecretEnv) > 0 {
		var parts []string
		for _, t := range slices.Sorted(maps.Keys(sp.SecretEnv)) {
			parts = append(parts, t+"="+strings.Join(sp.SecretEnv[t], ","))
		}
		set("secret-refs", strings.Join(parts, ";"))
	}
	if len(sp.PersonRefs) > 0 {
		var parts []string
		for _, t := range slices.Sorted(maps.Keys(sp.PersonRefs)) {
			parts = append(parts, t+"="+strings.Join(sp.PersonRefs[t], ","))
		}
		set("person-refs", strings.Join(parts, ";"))
	}
	return l, nil
}

// Environment variables the server reads for its stores and bus, which a
// System document's references fill.
const (
	envPostgres = "HUMAGEN_POSTGRES_URL"
	envRedis    = "HUMAGEN_REDIS_URL"
)

// overlay reads vars first, then the process environment.
func overlay(vars map[string]string, next func(string) string) func(string) string {
	return func(k string) string {
		if v, ok := vars[k]; ok {
			return v
		}
		return next(k)
	}
}

// openBus builds the bus the -bus flag (or a registered kind) names.
func openBus(ctx context.Context, o Options, l loaded, kind string, getenv func(string) string) (bus.Bus, func() error, error) {
	switch kind {
	case "", "memory":
		return nil, nil, nil
	case "redis":
		raw := strings.TrimSpace(getenv(envRedis))
		if raw == "" {
			return nil, nil, fmt.Errorf("bus: redis needs %s", envRedis)
		}
		opts, err := redis.ParseURL(raw)
		if err != nil {
			// Not echoed: a URL can carry a password.
			return nil, nil, fmt.Errorf("bus: %s is not a redis:// URL", envRedis)
		}
		client := redis.NewClient(opts)
		if err := client.Ping(ctx).Err(); err != nil {
			_ = client.Close()
			return nil, nil, fmt.Errorf("bus: redis is not reachable")
		}
		return bus.NewRedis(client), client.Close, nil
	default:
		if l.cfg.System.Spec.Bus.Kind == kind {
			b, err := build[bus.Bus](ctx, o, spec.SlotBus, l.cfg.System.Spec.Bus)
			return b, nil, err
		}
		return nil, nil, fmt.Errorf("bus: -bus=%q is not memory or redis", kind)
	}
}

// fileExtensions are the Platform documents' models and tools, as the
// platform takes them.
func fileExtensions(ctx context.Context, o Options, cfg spec.Config, keys credref.Resolver) (platform.Extensions, error) {
	ext := platform.Extensions{Providers: map[string]agentrt.ProviderFunc{}}
	for _, p := range cfg.Platforms {
		tenant := p.Metadata.Name
		for _, m := range p.Spec.Models {
			if _, done := ext.Providers[m.Name]; done {
				continue // the same model, declared by another workspace
			}
			switch m.Kind {
			case "anthropic", "openai", "ollama":
				b, err := spec.Block[spec.ModelBlock](m.Slot)
				if err != nil {
					return ext, err
				}
				ext.Providers[m.Name] = modelProvider(m.Kind, m.Name, b, keys)
			default:
				fn, err := build[agentrt.ProviderFunc](ctx, o, spec.SlotModels, m.Slot)
				if err != nil {
					return ext, err
				}
				ext.Providers[m.Name] = fn
			}
		}
		for _, t := range p.Spec.Tools {
			if t.Kind == "mcp" {
				b, err := spec.Block[spec.MCP](t.Slot)
				if err != nil {
					return ext, err
				}
				seed := platform.ToolProviderSeed{Provider: toolprovider.Provider{
					Tenant: tenant, Name: t.Name, Transport: toolprovider.Transport(b.Transport), Endpoint: b.Endpoint,
					Args: b.Args, CredentialEnv: b.CredentialAs, CredentialScope: toolprovider.CredentialScope(b.CredentialScope),
				}}
				if b.Credential.Set() {
					seed.Provider.CredentialRef = b.Credential.Ref
				}
				if err := seed.Provider.Validate(); err != nil {
					return ext, fmt.Errorf("platform %q: tools %q: %w", tenant, t.Name, err)
				}
				for _, a := range b.Accept {
					seed.Accept = append(seed.Accept, toolprovider.Tool{
						Name: a.Name, SchemaHash: a.SchemaHash, Class: toolprovider.Class(a.Class),
						Description: a.Description, InputSchema: a.InputSchema,
					})
				}
				ext.ToolProviders = append(ext.ToolProviders, seed)
				continue
			}
			cat, err := build[runtime.Catalog](ctx, o, spec.SlotTools, t.Slot)
			if err != nil {
				return ext, err
			}
			ext.Catalogs = append(ext.Catalogs, tenantOnly(tenant, cat))
		}
	}
	return ext, nil
}

// tenantOnly answers for one workspace's turns, so a tool one workspace
// declared is not another's.
func tenantOnly(tenant string, c runtime.Catalog) runtime.Catalog {
	return func(req runtime.Request, action func(payload.Action) error) (agentrt.Tools, error) {
		if req.Tenant != tenant {
			return nil, nil
		}
		return c(req, action)
	}
}

// checkNewAgentName refuses, for an agent the workspace does not have yet, a
// reserved word or a person's handle, as the API does: a tag must name one of
// them. An agent the workspace already has keeps its name.
func checkNewAgentName(ctx context.Context, p *platform.Platform, tenant, name string) error {
	if _, err := p.Agents.Get(ctx, tenant, name, 0); !errors.Is(err, registry.ErrNotFound) {
		return nil
	}
	return p.CheckAgentName(tenant, name)
}

// applyPlatforms makes the platform what the Platform documents declare: the
// workspace's instructions and its projects merged into the tree, agents
// created or revised, declared settings applied, a GitHub client managed by
// the file. Nothing the file does not mention is touched. The tree comes
// first, the root and then projects, so what follows can be filed in it.
func applyPlatforms(ctx context.Context, p *platform.Platform, cfg spec.Config, log *slog.Logger) error {
	for _, doc := range cfg.Platforms {
		tenant := doc.Metadata.Name
		if err := applyProjects(p, tenant, doc.Spec.Workspace); err != nil {
			return err
		}
		for i, a := range doc.Spec.Agents {
			s := a.Spec
			if a.File != "" {
				raw, err := os.ReadFile(a.File)
				if err != nil {
					return fmt.Errorf("platform %q: agents[%d]: %w", tenant, i, err)
				}
				var fromFile agentspec.AgentSpec
				if err := decodeStrict(raw, &fromFile); err != nil {
					return fmt.Errorf("platform %q: agents[%d] %s: %w", tenant, i, a.File, err)
				}
				s = &fromFile
			}
			if err := p.CheckAgentModel(tenant, *s); err != nil && !errors.Is(err, platform.ErrModelPolicy) {
				return fmt.Errorf("platform %q: agent %q: %w", tenant, s.Name, err)
			}
			if err := checkNewAgentName(ctx, p, tenant, s.Name); err != nil {
				return fmt.Errorf("platform %q: agents[%d] %q: %w", tenant, i, s.Name, err)
			}
			_, err := p.Agents.Create(ctx, tenant, *s, "file")
			if errors.Is(err, registry.ErrExists) {
				_, err = p.Agents.Revise(ctx, tenant, *s, 0, "file")
			}
			if err != nil {
				return fmt.Errorf("platform %q: agent %q: %w", tenant, s.Name, err)
			}
		}
		if err := applyHomes(ctx, p, doc); err != nil {
			return err
		}
		if err := applySchedules(p, tenant, doc.Spec.Workspace); err != nil {
			return err
		}
		if err := applyWorkspace(ctx, p, doc); err != nil {
			return err
		}
		if g := doc.Spec.Surfaces.GitHub; g != nil {
			if err := applyGitHub(ctx, p, tenant, *g, log); err != nil {
				return err
			}
		}
	}
	return nil
}

func applyWorkspace(ctx context.Context, p *platform.Platform, doc spec.Platform) error {
	w, tenant := doc.Spec.Workspace, doc.Metadata.Name
	s := p.Settings(tenant)
	if doc.Metadata.DisplayName != "" {
		s.WorkspaceName = doc.Metadata.DisplayName
	}
	if len(doc.Spec.ModelPolicies) > 0 {
		s.ModelPolicies = nil
		for _, m := range doc.Spec.ModelPolicies {
			s.ModelPolicies = append(s.ModelPolicies, platform.ModelPolicy{Name: m.Name, Description: m.Description, Models: m.Models, FallbackOn: m.FallbackOn})
		}
	}
	if w.TitleAgent != nil {
		s.TitleAgent = *w.TitleAgent
	}
	if w.MemoryAgent != nil {
		s.MemoryAgent = *w.MemoryAgent
	}
	if h := w.History; h != nil {
		if h.Mode != "" {
			s.History = platform.History(h.Mode)
		}
		if h.Limit != 0 {
			s.HistoryLimit = h.Limit
		}
	}
	if err := p.SetSettings(ctx, tenant, s); err != nil {
		return fmt.Errorf("platform %q: workspace: %w", tenant, err)
	}
	return nil
}

// fileWriter is who the file's changes to projects are recorded as.
const fileWriter = "file"

// applyProjects merges the file's tree into the store, never replacing it.
// workspace.instructions are the root's. Each project is upserted, parents
// first, as managed by the file, and the fields the file sets are the file's:
// the API answers 409 for them, and a field it leaves out stays the
// console's. An unchanged entry makes no new version. A project made in the
// console survives every start, one with a name the file declares is taken
// over, and one that left the file stays, unmanaged. A kind or parent the
// file names for a project that already has another fails the start: both
// are fixed when a project is created.
func applyProjects(p *platform.Platform, tenant string, w spec.Workspace) error {
	now := time.Now()
	root, err := p.Projects.Get(tenant, projects.Root)
	if err != nil {
		return fmt.Errorf("platform %q: workspace: %w", tenant, err)
	}
	declared, owns := fileRoot(w)
	if w.AgentPosts != nil {
		declared.AgentPosts, owns = *w.AgentPosts, append(owns, projects.FieldAgentPosts)
	}
	if w.Agents != nil {
		declared.Agents, owns = fileAgents(*w.Agents), append(owns, projects.FieldAgents)
	}
	switch {
	case len(owns) > 0:
		_, err = p.Projects.Upsert(tenant, declared, owns, fileWriter, now)
	case root.Managed == projects.ManagedFile:
		_, err = p.Projects.Release(tenant, projects.Root, fileWriter, now)
	}
	if err != nil {
		return fmt.Errorf("platform %q: workspace: %w", tenant, err)
	}
	in := map[string]bool{}
	for _, i := range parentsFirst(w.Projects) {
		pr, owns := fileProject(w.Projects[i])
		if _, err := p.Projects.Upsert(tenant, pr, owns, fileWriter, now); err != nil {
			return fmt.Errorf("platform %q: workspace.projects[%d]: %w", tenant, i, err)
		}
		in[pr.Name] = true
	}
	for _, pr := range p.Projects.List(tenant) {
		if pr.Managed != projects.ManagedFile || in[pr.Name] {
			continue
		}
		if _, err := p.Projects.Release(tenant, pr.Name, fileWriter, now); err != nil {
			return fmt.Errorf("platform %q: workspace.projects: let go of %q: %w", tenant, pr.Name, err)
		}
	}
	return applyChannels(p, tenant, w)
}

// fileProject is a project as the file declares it, and the fields the file
// sets, which become the file's.
func fileProject(d spec.Project) (projects.Project, []string) {
	pr := projects.Project{
		Name: d.Name, Title: d.Title, Description: d.Description, Kind: projects.Kind(d.Kind),
		Parent: spec.ParentOf(d), Instructions: d.Instructions, Experts: expertsOf(d.Experts), AgentPosts: d.AgentPosts,
		Knowledge: append([]string(nil), d.Knowledge...), Skills: skillsOf(d.Skills),
	}
	if a := d.Account; a != nil {
		pr.Account = &projects.AccountInfo{Customer: a.Customer, ExternalID: a.ExternalID, URL: a.URL}
	}
	if d.Agents != nil {
		pr.Agents = fileAgents(*d.Agents)
	}
	if d.Members != nil {
		pr.Members = membersOf(d.Members)
	}
	var owns []string
	for field, set := range map[string]bool{
		projects.FieldTitle:        d.Title != "",
		projects.FieldDescription:  d.Description != "",
		projects.FieldKind:         d.Kind != "",
		projects.FieldParent:       d.Parent != "",
		projects.FieldAccount:      d.Account != nil,
		projects.FieldInstructions: d.Instructions != "",
		projects.FieldAgentPosts:   d.AgentPosts != "",
		projects.FieldExperts:      d.Experts != nil,
		projects.FieldKnowledge:    d.Knowledge != nil,
		projects.FieldSkills:       d.Skills != nil,
		projects.FieldAgents:       d.Agents != nil,
		projects.FieldMembers:      d.Members != nil,
	} {
		if set {
			owns = append(owns, field)
		}
	}
	return pr, owns
}

// fileAgents is a project's agents as the file declares them.
func fileAgents(a spec.Agents) projects.Agents {
	return projects.Agents{Enable: a.Enable, Allow: a.Allow, Default: a.Default}
}

// applyHomes makes each agent the file places in a project live there, once
// the file's agents exist, and holds every agent the file's lists name to the
// registry: a name the check could not see, in an agent read from a file, is
// refused here. A home that left the file stays, as a project does. A home
// something outside it still needs fails the start, naming what.
func applyHomes(ctx context.Context, p *platform.Platform, doc spec.Platform) error {
	tenant, w := doc.Metadata.Name, doc.Spec.Workspace
	if err := p.EnsureBuiltins(ctx, tenant); err != nil {
		return fmt.Errorf("platform %q: the platform's own agents: %w", tenant, err)
	}
	lists := []struct {
		path   string
		agents *spec.Agents
	}{{"workspace.agents", w.Agents}}
	for i, pr := range w.Projects {
		lists = append(lists, struct {
			path   string
			agents *spec.Agents
		}{fmt.Sprintf("workspace.projects[%d].agents", i), pr.Agents})
	}
	for _, l := range lists {
		if l.agents == nil {
			continue
		}
		for _, n := range slices.Concat(l.agents.Enable, l.agents.Allow, []string{l.agents.Default}) {
			if n == "" || n == spec.EveryAgent || n == spec.AutoAgent {
				continue
			}
			if _, err := p.Agents.Get(ctx, tenant, n, 0); err != nil {
				return fmt.Errorf("platform %q: %s: there is no agent called %q", tenant, l.path, n)
			}
		}
	}
	for i, a := range doc.Spec.Agents {
		if a.Spec == nil || a.Project == "" || a.Project == spec.RootParent {
			continue
		}
		if err := p.SetAgentHome(ctx, tenant, agentspec.NormalizeName(a.Spec.Name), a.Project, projects.Writer{By: fileWriter, Govern: true}); err != nil {
			return fmt.Errorf("platform %q: agents[%d].project: %w", tenant, i, err)
		}
	}
	return nil
}

// membersOf is a list of members as the store keeps them.
func membersOf(ms []spec.Member) []projects.Entry {
	out := make([]projects.Entry, 0, len(ms))
	for _, m := range ms {
		out = append(out, projects.Entry{Who: m.Who, Role: projects.Role(m.Role)})
	}
	return out
}

// parentsFirst is the order to apply the file's projects in: by how deep
// each sits in the file's tree, then as written. The spec's check has
// refused a cycle; a walk is bounded anyway, so it cannot loop.
func parentsFirst(ps []spec.Project) []int {
	at := make(map[string]int, len(ps))
	for i, pr := range ps {
		at[pr.Name] = i
	}
	depth := make([]int, len(ps))
	for i := range ps {
		for j, ok := i, true; ok && depth[i] <= len(ps); depth[i]++ {
			j, ok = at[spec.ParentOf(ps[j])]
		}
	}
	order := make([]int, len(ps))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int { return depth[a] - depth[b] })
	return order
}

// applyGitHub seats the file's GitHub installation as a client the API cannot
// change, and makes the one an earlier start kept follow the file. Its secrets
// are env: references, which is all a client's secret may be; the operator
// lists the variables in secret_env. A section with enabled: false turns the
// kept client off and reads nothing else.
func applyGitHub(ctx context.Context, p *platform.Platform, tenant string, g spec.GitHub, log *slog.Logger) error {
	if !g.Enabled {
		err := p.TurnOffClient(ctx, tenant, "file-github", "file")
		if errors.Is(err, clients.ErrExists) {
			log.Warn("github client from the file not turned off: it is not the file's", "tenant", tenant, "error", err)
			return nil
		}
		if err != nil {
			return fmt.Errorf("platform %q: surfaces.github: %w", tenant, err)
		}
		return nil
	}
	for name, r := range map[string]*spec.Ref{"token": g.Token, "webhook_secret": g.WebhookSecret} {
		if !strings.HasPrefix(r.Ref, "env:") {
			return fmt.Errorf("platform %q: surfaces.github.%s: a client's secret is an env: reference", tenant, name)
		}
	}
	in := platform.ClientInput{
		Client: clients.Client{
			ID: "file-github", Name: "GitHub", Surface: clients.GitHub, Enabled: true, Managed: clients.ManagedFile,
			Agents:   []string{clients.AnyAgent},
			Identity: clients.Identity{Owners: g.Owners, AppID: g.App, Installation: g.Installation},
			GitHub:   &clients.GitHubOptions{ReadRepositories: g.ReadRepos, Checks: g.Checks, Guides: g.Guides, Mutations: g.Mutations, Login: g.Login},
		},
		Token:         clients.SecretInput{Ref: g.Token.Ref},
		SigningSecret: clients.SecretInput{Ref: g.WebhookSecret.Ref},
	}
	_, err := p.ApplyClient(ctx, tenant, "file", in)
	if errors.Is(err, clients.ErrExists) {
		log.Warn("github client from the file not applied: another client answers for it", "tenant", tenant, "error", err)
		return nil
	}
	if err != nil {
		return fmt.Errorf("platform %q: surfaces.github: %w", tenant, err)
	}
	return nil
}

// slackApps are the Slack apps the Platform documents turn on.
func slackApps(cfg spec.Config, keys credref.Resolver, getenv func(string) string) []*slackApp {
	var out []*slackApp
	for _, doc := range cfg.Platforms {
		s := doc.Spec.Surfaces.Slack
		if s == nil || !s.Enabled {
			continue
		}
		out = append(out, &slackApp{tenant: doc.Metadata.Name, choice: *s, keys: keys, dir: slackDir(getenv)})
	}
	return out
}

// configCmd is `humagen config check|print -f PATH`.
func configCmd(o Options, args []string, out io.Writer) error {
	if len(args) == 0 || (args[0] != "check" && args[0] != "print") {
		return errors.New("usage: humagen config check|print -f PATH [-f PATH]")
	}
	fs := flag.NewFlagSet("config", flag.ContinueOnError)
	var paths files
	fs.Var(&paths, "f", "a spec file or directory; repeatable")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if len(paths) == 0 {
		def, err := defaultSpec(o.notes)
		if err != nil {
			return err
		}
		if def != "" {
			paths = append(paths, def)
		}
	}
	var cfg spec.Config
	if len(paths) > 0 {
		var err error
		if cfg, err = loadSpec(o, paths...); err != nil {
			return err
		}
	}
	if err := cfg.Check(o.kinds()); err != nil {
		return err
	}
	if args[0] == "check" {
		c, err := carry(context.Background(), cfg, credref.Resolver{Run: func(context.Context, string, ...string) ([]byte, error) {
			return []byte("unread"), nil // check names references, it does not read keychains
		}})
		if err != nil {
			return err
		}
		var unset []string
		for _, v := range c.env {
			if _, ok := c.values[v]; !ok && legacy.Getenv(v) == "" {
				unset = append(unset, v)
			}
		}
		if len(unset) > 0 {
			fmt.Fprintf(out, "note: not set in this shell: %s (start and deploy need them)\n", strings.Join(unset, ", "))
		}
		for _, w := range machineWarnings(cfg) {
			fmt.Fprintf(out, "warning: %s\n", w)
		}
		_, err = fmt.Fprintf(out, "ok: %s\n", strings.Join(paths, ", "))
		return err
	}
	cfg.System.Defaults()
	b, err := spec.Marshal(cfg)
	if err != nil {
		return err
	}
	_, err = out.Write(b)
	return err
}

// machineWarnings is what config check says about the machines a spec
// configures. Projects moved from runs:* to projects:*, so a machine whose
// scopes read runs but not projects lists no projects (docs/projects.md,
// Migration phase 1); a person is unaffected under the built-in policy.
func machineWarnings(cfg spec.Config) []string {
	var out []string
	for _, p := range cfg.Platforms {
		s := p.Spec.Surfaces.Slack
		if s == nil || !s.Enabled || s.HumagenClient == nil {
			continue
		}
		scopes := s.HumagenClient.Scopes
		has := func(p auth.Permission) bool {
			return slices.Contains(scopes, auth.ScopePrefix+string(p)) || slices.Contains(scopes, legacy.ScopePrefix+string(p))
		}
		if slices.ContainsFunc(scopes, func(sc string) bool { return strings.HasPrefix(sc, legacy.ScopePrefix) }) {
			out = append(out, fmt.Sprintf("platform %q: the Slack app's humagen_client asks for scopes under %s, the old prefix, which still works; change them to %s once your identity provider defines those",
				p.Metadata.Name, legacy.ScopePrefix, auth.ScopePrefix))
		}
		if has(auth.RunsRead) && !has(auth.ProjectsRead) {
			out = append(out, fmt.Sprintf("platform %q: the Slack app's humagen_client has %s%s but not %s%s, so it lists no projects; add %s%s (see docs/authorization.md)",
				p.Metadata.Name, auth.ScopePrefix, auth.RunsRead, auth.ScopePrefix, auth.ProjectsRead, auth.ScopePrefix, auth.ProjectsRead))
		}
	}
	return out
}

// loadSpec is spec.Load, noting each name from before the rename the files
// still use.
func loadSpec(o Options, paths ...string) (spec.Config, error) {
	cfg, err := spec.Load(paths...)
	for _, n := range cfg.Notes {
		o.notes.Add("%s", n)
	}
	return cfg, err
}

// specPath is the spec start and setup read and write when no file is named:
// humagen.yaml in HUMAGEN_HOME, else in the user config directory, or the old
// old file where a release before the rename kept it (legacy.SpecFile).
func specPath(n *legacy.Notes) (string, error) {
	return legacy.SpecFile(os.Getenv, os.UserConfigDir, n)
}

// defaultSpec is specPath when the file is there, else "".
func defaultSpec(n *legacy.Notes) (string, error) {
	path, err := specPath(n)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	return "", nil
}

// fileRunners builds the System document's extensions.
func fileRunners(ctx context.Context, o Options, cfg spec.Config) ([]Extension, error) {
	var out []Extension
	for _, e := range cfg.System.Spec.Extensions {
		x, err := build[Extension](ctx, o, spec.SlotExtensions, e)
		if err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, nil
}
