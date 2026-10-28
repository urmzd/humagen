package wire

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/urmzd/fiber/pkg/agentspec"
	"github.com/urmzd/fiber/pkg/registry"
)

// The agent definition types.
type (
	AgentSpec = agentspec.AgentSpec
	Revision  = agentspec.Revision
	ModelRef  = agentspec.ModelRef
	Access    = agentspec.Access
	Source    = agentspec.Source
	Page      = registry.Page
)

// AgentStore keeps every revision of every agent definition.
type AgentStore = registry.Store

// AgentCatalog is how Humagen reads agent definitions.
type AgentCatalog interface {
	// Get returns one revision, or the latest with rev 0.
	Get(ctx context.Context, tenant, name string, rev int) (Revision, error)
	// List returns a page of the tenant's latest revisions.
	List(ctx context.Context, tenant string, page Page) ([]Revision, string, error)
	// History returns every revision of one agent, oldest first.
	History(ctx context.Context, tenant, name string) ([]Revision, error)
}

var _ AgentCatalog = AgentStore(nil)

// Errors a catalog or a definition reports.
var (
	ErrNotFound     = registry.ErrNotFound
	ErrExists       = registry.ErrExists
	ErrConflict     = registry.ErrConflict
	ErrInvalidPage  = registry.ErrInvalid
	ErrInvalidAgent = agentspec.ErrInvalid
)

// Limits on a definition and a page.
const (
	MaxNameLen  = agentspec.MaxNameLen
	MaxPageSize = registry.MaxPageSize
)

// NormalizeName is the canonical form of an agent name.
func NormalizeName(name string) string { return agentspec.NormalizeName(name) }

// KnownProviders are the model providers a definition may name by default.
func KnownProviders() []string { return agentspec.KnownProviders() }

// FallbackConditions are the conditions a model fallback may name.
func FallbackConditions() []string { return agentspec.FallbackConditions() }

// ParseMCPGrant reads a tool name that grants an MCP server's tools.
func ParseMCPGrant(name string) (agentspec.MCPGrant, bool, error) {
	return agentspec.ParseMCPGrant(name)
}

// NewRevisionWith validates spec as one revision, against providers.
var NewRevisionWith = agentspec.NewRevisionWith

// NewMemoryAgents is an agent store held in memory.
func NewMemoryAgents(providers ...string) AgentStore {
	return registry.NewMemory(registry.WithProviders(providers...))
}

// NewPostgresAgents is an agent store kept in Postgres.
func NewPostgresAgents(pool *pgxpool.Pool, providers ...string) *registry.Postgres {
	return registry.NewPostgres(pool, registry.WithProviders(providers...))
}

// Specs resolves the definitions runs pin, for fiber's runtime.
func Specs(store AgentStore) *registry.Specs { return registry.NewSpecs(store) }

// Policy is what a definition says about who it calls and who acts for it.
type Policy struct {
	Access       Access
	Approvals    []string
	Approvers    []string
	SelfApproval bool
	Answerers    []string
	TaggedOnly   bool
}

// PolicyOf reads a definition's policy fields.
func PolicyOf(spec AgentSpec) Policy {
	return Policy{
		Access: spec.Access, Approvals: spec.Approvals, Approvers: spec.Approvers,
		SelfApproval: spec.SelfApproval, Answerers: spec.Answerers, TaggedOnly: spec.TaggedOnly,
	}
}
