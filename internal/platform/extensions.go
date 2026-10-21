package platform

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"

	saigetypes "github.com/urmzd/saige/agent/types"

	"github.com/urmzd/fiber/pkg/agentrt"
	"github.com/urmzd/fiber/pkg/agentrt/payload"
	"github.com/urmzd/fiber/pkg/mcptools"
	"github.com/urmzd/fiber/pkg/runtime"
	"github.com/urmzd/fiber/pkg/toolprovider"
)

// Extensions is what a program built on warp adds to the platform without
// forking it: model providers, tool sources and MCP tool providers. The zero
// value adds nothing. The warp package fills it from options and a spec file.
type Extensions struct {
	// Providers are model providers by name. A spec whose provider is the
	// name runs on what the function builds, and a built-in of the same name
	// is replaced.
	Providers map[string]agentrt.ProviderFunc
	// Catalogs resolve tool names no connector or built-in tool does, in order.
	Catalogs []runtime.Catalog
	// ToolProviders are MCP servers recorded at startup, each with the tools
	// the deployment accepts from it, pinned by schema hash.
	ToolProviders []ToolProviderSeed
	// ToolStore keeps tool provider records and accepted snapshots. Nil means
	// in memory.
	ToolStore toolprovider.Store
	// Resolver turns a tool provider's credential reference into its value
	// when a session opens. Nil means "env:NAME" references only.
	Resolver mcptools.Resolver
}

// ToolProviderSeed is one MCP tool provider a deployment declares, and the
// tools it accepts from it.
type ToolProviderSeed struct {
	Provider toolprovider.Provider
	Accept   []toolprovider.Tool
}

// ProviderNames lists the extension providers' names, sorted.
func (e Extensions) ProviderNames() []string { return slices.Sorted(maps.Keys(e.Providers)) }

// Merge combines two sets of extensions. A name declared by both is an error
// rather than an override, so a file cannot silently replace an option.
func (e Extensions) Merge(o Extensions) (Extensions, error) {
	out := Extensions{
		Providers: maps.Clone(e.Providers),
		Catalogs:  slices.Concat(e.Catalogs, o.Catalogs),
		ToolStore: e.ToolStore, Resolver: e.Resolver,
	}
	if out.Providers == nil {
		out.Providers = map[string]agentrt.ProviderFunc{}
	}
	for name, fn := range o.Providers {
		if _, dup := out.Providers[name]; dup {
			return Extensions{}, fmt.Errorf("platform: model provider %q is declared twice", name)
		}
		out.Providers[name] = fn
	}
	seen := map[string]bool{}
	for _, s := range slices.Concat(e.ToolProviders, o.ToolProviders) {
		key := s.Provider.Tenant + "/" + s.Provider.Name
		if seen[key] {
			return Extensions{}, fmt.Errorf("platform: tool provider %q is declared twice", s.Provider.Name)
		}
		seen[key] = true
		out.ToolProviders = append(out.ToolProviders, s)
	}
	if o.ToolStore != nil {
		if out.ToolStore != nil {
			return Extensions{}, errors.New("platform: two tool provider stores")
		}
		out.ToolStore = o.ToolStore
	}
	if o.Resolver != nil {
		if out.Resolver != nil {
			return Extensions{}, errors.New("platform: two credential resolvers")
		}
		out.Resolver = o.Resolver
	}
	return out, nil
}

// agentOptions are the extension providers as agent options. They come after
// the built-ins, so one named like a built-in replaces it.
func (e Extensions) agentOptions() []agentrt.Option {
	var out []agentrt.Option
	for _, name := range e.ProviderNames() {
		out = append(out, agentrt.WithProvider(name, e.Providers[name]))
	}
	return out
}

// extensionCatalog resolves a tool name against base, then each extension
// catalog in order.
func (e Extensions) catalog(base runtime.Catalog) runtime.Catalog {
	if len(e.Catalogs) == 0 {
		return base
	}
	all := slices.Concat([]runtime.Catalog{base}, e.Catalogs)
	return func(req runtime.Request, action func(payload.Action) error) (agentrt.Tools, error) {
		var chain []agentrt.Tools
		for _, c := range all {
			if c == nil {
				continue
			}
			t, err := c(req, action)
			if err != nil {
				return nil, err
			}
			if t != nil {
				chain = append(chain, t)
			}
		}
		return agentrt.ToolsFunc(func(name string) (saigetypes.Tool, error) {
			for _, t := range chain {
				tool, err := t.Lookup(name)
				if err == nil {
					return tool, nil
				}
				if !errors.Is(err, agentrt.ErrNoSuchTool) {
					return nil, err
				}
			}
			return nil, fmt.Errorf("%w: %q", agentrt.ErrNoSuchTool, name)
		}), nil
	}
}

// toolGroups opens the tool provider store and the grants that expand an
// "mcp:<provider>[:write]" grant into that provider's accepted tools, and
// records the declared providers. It returns what to close on shutdown.
func (p *Platform) toolGroups(ctx context.Context) (runtime.ToolGroupSource, func() error, error) {
	e := p.cfg.Ext
	store := e.ToolStore
	if store == nil {
		store = toolprovider.NewMemory()
	}
	p.ToolProviders = store
	resolver := e.Resolver
	if resolver == nil {
		resolver = mcptools.EnvResolver()
	}
	dial := mcptools.Dialer(mcptools.WithResolver(resolver), mcptools.WithLogger(p.log))
	grants := mcptools.NewGrants(store, dial)
	if err := seedToolProviders(ctx, store, e.ToolProviders, dial, p.log); err != nil {
		_ = grants.Close()
		return nil, nil, err
	}
	return grants, grants.Close, nil
}

// seedToolProviders records each declared provider and accepts the tools it
// pins. A record that already matches is left alone, so a restart mints no
// version. A server that cannot be reached is logged and skipped: its grants
// are then unavailable to runs, as an unreachable server is at run time.
func seedToolProviders(ctx context.Context, store toolprovider.Store, seeds []ToolProviderSeed, dial mcptools.Dial, log *slog.Logger) error {
	for _, s := range seeds {
		rec, err := putProvider(ctx, store, s.Provider)
		if err != nil {
			return fmt.Errorf("platform: tool provider %q: %w", s.Provider.Name, err)
		}
		if len(s.Accept) == 0 {
			continue
		}
		held, err := acceptDeclared(ctx, store, rec, s.Accept, dial)
		if err != nil {
			log.Warn("tool provider not reachable at startup; its tools stay pending", "provider", rec.Name, "error", err)
			continue
		}
		for _, h := range held {
			log.Info("tool held back", "provider", rec.Name, "tool", h.tool.Name, "schema_hash", h.tool.SchemaHash, "reason", h.reason)
		}
	}
	return nil
}

func putProvider(ctx context.Context, store toolprovider.Store, p toolprovider.Provider) (toolprovider.Provider, error) {
	cur, err := store.GetProvider(ctx, p.Tenant, p.Name)
	switch {
	case errors.Is(err, toolprovider.ErrNotFound):
		p.UpdatedBy = "config"
		return store.PutProvider(ctx, p)
	case err != nil:
		return toolprovider.Provider{}, err
	}
	if cur.Transport == p.Transport && cur.Endpoint == p.Endpoint && slices.Equal(cur.Args, p.Args) &&
		cur.CredentialRef == p.CredentialRef && cur.CredentialEnv == p.CredentialEnv {
		return cur, nil
	}
	p.Version, p.UpdatedBy = cur.Version, "config"
	return store.PutProvider(ctx, p)
}

// heldBack is an offered tool the declaration does not accept, and why.
type heldBack struct {
	tool   toolprovider.Tool
	reason string
}

// acceptDeclared accepts what the deployment pinned and nothing else, and
// reports every offered tool it left pending.
//
// An offered tool is accepted only when it is declared and its schema hash
// matches the pin. Its description is taken from the server the first time,
// and after that must match the accepted one: a description is what the
// model is told a tool does, so a reworded one is a change like a new schema.
// A declared tool that drifted keeps its previously accepted form in the
// snapshot, which the toolset then holds back until the server serves that
// form again or the pin is updated. A tool the server offers that is not
// declared is never accepted. The snapshot is written only when what it
// holds would change.
func acceptDeclared(ctx context.Context, store toolprovider.Store, p toolprovider.Provider, declared []toolprovider.Tool, dial mcptools.Dial) ([]heldBack, error) {
	s, err := dial(ctx, p)
	if err != nil {
		return nil, err
	}
	offered, err := s.Offered(ctx)
	_ = s.Close()
	if err != nil {
		return nil, err
	}
	cur, err := store.Snapshot(ctx, p.Tenant, p.Name, 0)
	if err != nil {
		return nil, err
	}
	byName := make(map[string]toolprovider.Tool, len(offered))
	for _, o := range offered {
		byName[o.Name] = o
	}
	var accept []toolprovider.Tool
	var held []heldBack
	for _, d := range declared {
		class := d.Class
		if class == "" {
			class = toolprovider.ClassWrite
		}
		prev, wasAccepted := cur.Tool(d.Name)
		o, ok := byName[d.Name]
		var reason string
		switch {
		case !ok:
			reason = "declared but not offered"
		case o.SchemaHash != d.SchemaHash:
			reason = "schema does not match the declared schema_hash"
		case wasAccepted && prev.SchemaHash == o.SchemaHash && prev.Description != o.Description:
			reason = "description changed since it was accepted"
		}
		if reason == "" {
			o.Class = class
			accept = append(accept, o)
			continue
		}
		if ok {
			held = append(held, heldBack{tool: o, reason: reason})
		} else {
			held = append(held, heldBack{tool: toolprovider.Tool{Name: d.Name, SchemaHash: d.SchemaHash}, reason: reason})
		}
		if wasAccepted && prev.SchemaHash == d.SchemaHash {
			// Keep the form that was accepted; the toolset holds it back
			// while the server offers something else.
			prev.Class = class
			accept = append(accept, prev)
		}
	}
	for _, o := range offered {
		if !slices.ContainsFunc(declared, func(d toolprovider.Tool) bool { return d.Name == o.Name }) {
			held = append(held, heldBack{tool: o, reason: "offered but not declared"})
		}
	}
	if !sameTools(cur.Tools, accept) {
		if _, err := store.Accept(ctx, p.Tenant, p.Name, cur.Version, accept, "config"); err != nil {
			return nil, err
		}
	}
	return held, nil
}

func sameTools(a, b []toolprovider.Tool) bool {
	if len(a) != len(b) {
		return false
	}
	for _, t := range b {
		i := slices.IndexFunc(a, func(x toolprovider.Tool) bool { return x.Name == t.Name })
		if i < 0 || a[i].Description != t.Description || a[i].SchemaHash != t.SchemaHash || a[i].Class.Effective() != t.Class.Effective() {
			return false
		}
	}
	return true
}

// Close releases what New opened: MCP sessions, and whatever a backend
// handed it to close. It is safe to call more than once.
func (p *Platform) Close() error {
	var errs []error
	for i := len(p.closers) - 1; i >= 0; i-- {
		errs = append(errs, p.closers[i]())
	}
	p.closers = nil
	return errors.Join(errs...)
}
