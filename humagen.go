// Package humagen runs humagen with extensions: the same command tree, server
// and composition root as the humagen binary, plus model providers, tool
// sources, MCP tool providers, connectors and stores a downstream program
// registers in Go. It is the public seam that makes a private build of humagen
// an import and a few options instead of a fork.
//
//	func main() {
//		os.Exit(humagen.Main(humagen.Version{Version: "v1"},
//			humagen.WithModelProvider("house", newHouseProvider),
//			humagen.WithBlobStore(myBucket),
//		))
//	}
//
// The common cases need no Go at all: a spec file (docs/spec.md) declares
// stores, models, MCP tools, agents and surfaces, and `humagen setup` writes one.
// Go adds what a file cannot: a kind the file then selects by name
// (WithKind), or a provider, tool source or connector directly. Options and
// the file combine; a model or tool provider declared by both is an error
// rather than an override.
//
// It mirrors fiber's fiberapp option for option, and sits at the module root
// because it is the public face of internal/app, which nothing under pkg/ may
// import. It adds no behaviour of its own.
package humagen

import (
	"os"

	saigetypes "github.com/urmzd/saige/agent/types"

	"github.com/urmzd/humagen/fiber/pkg/event"
	"github.com/urmzd/humagen/fiber/pkg/mcptools"
	"github.com/urmzd/humagen/fiber/pkg/runtime"
	"github.com/urmzd/humagen/fiber/pkg/toolprovider"
	"github.com/urmzd/humagen/fiber/pkg/turn"
	"github.com/urmzd/humagen/fiber/pkg/turn/native"

	"github.com/urmzd/humagen/internal/app"
	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/blob"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/knowledge"
	"github.com/urmzd/humagen/pkg/spec"
)

// Version is the build metadata `humagen version` reports.
type Version struct {
	Version string
	Commit  string
	Date    string
}

// Option extends the composition root.
type Option func(*app.Options)

// Main runs the humagen command tree with the options applied and returns the
// process exit code, for os.Exit. Arguments come from os.Args.
func Main(v Version, opts ...Option) int {
	o := app.Options{Version: app.Version(v), Changelog: changelog}
	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}
	return app.Execute(o, os.Args[1:])
}

// WithModelProvider makes a model provider selectable by name: a spec whose
// provider is name runs on what fn builds, and a built-in of the same name is
// replaced.
func WithModelProvider(name string, fn native.ProviderFunc) Option {
	return func(o *app.Options) {
		if name == "" || fn == nil {
			return
		}
		if o.Ext.Providers == nil {
			o.Ext.Providers = map[string]native.ProviderFunc{}
		}
		o.Ext.Providers[name] = fn
	}
}

// WithSourceProvider registers an external source importer by its configured name.
// Implementations receive only the current reader's explicitly connected credential.
func WithSourceProvider(name string, provider knowledge.Provider) Option {
	return func(o *app.Options) {
		if !knowledge.ValidName(name) || provider == nil {
			return
		}
		if o.Ext.SourceProviders == nil {
			o.Ext.SourceProviders = map[string]knowledge.Provider{}
		}
		o.Ext.SourceProviders[name] = provider
	}
}

// WithOpenAICompatible registers an OpenAI-compatible endpoint under ep.Name.
// The key is read from the variable ep.KeyEnv names when an agent is built.
func WithOpenAICompatible(ep native.OpenAICompatible) Option {
	return WithModelProvider(ep.Name, ep.Provider())
}

// WithToolSource adds a catalog that spec tool names resolve against, after
// the connectors' actions and the platform's own tools. It is built per turn,
// so it can bind tools to the run's tenant.
func WithToolSource(c runtime.Catalog) Option {
	return func(o *app.Options) {
		if c != nil {
			o.Ext.Catalogs = append(o.Ext.Catalogs, c)
		}
	}
}

// WithTools adds a fixed set of tools, resolved by name for every turn.
func WithTools(tools ...saigetypes.Tool) Option {
	static := native.StaticTools(tools...)
	return WithToolSource(func(runtime.Request, func(event.Action) error) (native.Tools, error) {
		return static, nil
	})
}

// WithToolProvider records an MCP tool provider at startup and accepts the
// tools in accept from it, each pinned by Name and SchemaHash. Anything the
// server adds or changes stays pending and is logged with its hash. An empty
// p.Tenant is the deployment's tenant. Agents reach it with an "mcp:<name>"
// or "mcp:<name>:write" grant.
func WithToolProvider(p toolprovider.Provider, accept ...toolprovider.Tool) Option {
	return func(o *app.Options) {
		o.Ext.ToolProviders = append(o.Ext.ToolProviders, platform.ToolProviderSeed{Provider: p, Accept: accept})
	}
}

// WithToolProviderStore keeps tool provider records and accepted snapshots
// in s, such as fiber's toolprovider.NewPostgres. Nil keeps them in memory.
func WithToolProviderStore(s toolprovider.Store) Option {
	return func(o *app.Options) { o.Ext.ToolStore = s }
}

// WithCredentialResolver replaces how a tool provider's credential reference
// is read when a session opens. The default reads env:, keychain: and op://
// references, each time.
func WithCredentialResolver(r mcptools.Resolver) Option {
	return func(o *app.Options) { o.Ext.Resolver = r }
}

// WithConnector seats a connector the program built itself for one tenant:
// a client the API does not manage, answering on its surface.
func WithConnector(tenant string, peer platform.Peer) Option {
	return func(o *app.Options) {
		if peer != nil {
			o.Connections = append(o.Connections, platform.Connection{Tenant: tenant, Peer: peer})
		}
	}
}

// WithBlobStore keeps published files' bytes in s instead of what -storage
// selects.
func WithBlobStore(s blob.Store) Option {
	return func(o *app.Options) { o.Blobs = s }
}

// WithStore keeps client records and their secrets in the given stores
// instead of the ones the configuration selects. Either may be nil.
func WithStore(c clients.Store, secrets clients.Secrets) Option {
	return WithPlatform(func(cfg *platform.Config) error {
		if c != nil {
			cfg.Clients = c
		}
		if secrets != nil {
			cfg.Secrets = secrets
		}
		return nil
	})
}

// WithRuntime runs every turn on rt in place of the native saige turn, inside
// the same playscape sandbox, on the same durable runs. rt is handed the pinned
// definition and the triggering event, and streams back catalog event bodies
// (turn.Runtime). It brings its own models and tools: WithModelProvider,
// WithToolSource and the spec's models do not reach it. Nil changes nothing.
func WithRuntime(rt turn.Runtime) Option {
	if rt == nil {
		return nil
	}
	return WithPlatform(func(cfg *platform.Config) error {
		cfg.Runtime = rt
		return nil
	})
}

// WithPlatform edits the platform's configuration after flags and files are
// applied. It reaches every seam platform.Config has, including ones no
// option names yet.
func WithPlatform(fn func(*platform.Config) error) Option {
	return func(o *app.Options) {
		if fn == nil {
			return
		}
		prev := o.Platform
		o.Platform = func(cfg *platform.Config) error {
			if prev != nil {
				if err := prev(cfg); err != nil {
					return err
				}
			}
			return fn(cfg)
		}
	}
}

// Slots a kind can be registered in. See docs/spec.md.
const (
	Blobs      = spec.SlotBlobs
	Bus        = spec.SlotBus
	Extensions = spec.SlotExtensions
	Models     = spec.SlotModels
	Tools      = spec.SlotTools
)

// Factory builds a registered kind from its block in the spec file, which it
// decodes strictly itself. What it returns depends on the slot: a blob.Store
// for Blobs, a bus.Bus for Bus, a native.ProviderFunc for Models, a
// runtime.Catalog for Tools, and an Extension for Extensions.
type Factory = app.Factory

// Extension is a process-wide part, run beside the server until its context
// ends.
type Extension = app.Extension

// WithKind registers a kind in a slot, so a spec file can select it by name:
//
//	humagen.WithKind(humagen.Blobs, "gcs", func(ctx context.Context, block json.RawMessage) (any, error) {
//		var c struct{ Bucket string `json:"bucket"` }
//		if err := strictDecode(block, &c); err != nil {
//			return nil, err
//		}
//		return newGCS(ctx, c.Bucket)
//	})
//
// and then `blobs: {kind: gcs, gcs: {bucket: files}}` in a System document.
func WithKind(slot, name string, f Factory) Option {
	return func(o *app.Options) {
		if name == "" || f == nil {
			return
		}
		if o.Kinds == nil {
			o.Kinds = map[string]map[string]app.Factory{}
		}
		if o.Kinds[slot] == nil {
			o.Kinds[slot] = map[string]app.Factory{}
		}
		o.Kinds[slot][name] = f
	}
}
