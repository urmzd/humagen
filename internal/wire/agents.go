package wire

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/urmzd/humagen/fiber/pkg/definition"
	"github.com/urmzd/humagen/pkg/agents"
	"github.com/urmzd/humagen/pkg/state"
)

// Page is one page of a listing.
type Page struct {
	Size  int
	Token string
}

// MaxPageSize bounds a page of agents.
const MaxPageSize = 500

// AgentCatalog is how Humagen reads agent definitions.
type AgentCatalog interface {
	// Get returns one revision, or the latest with rev 0.
	Get(ctx context.Context, tenant, name string, rev int) (Revision, error)
	// List returns a page of the tenant's latest revisions.
	List(ctx context.Context, tenant string, page Page) ([]Revision, string, error)
	// History returns every revision of one agent, oldest first.
	History(ctx context.Context, tenant, name string) ([]Revision, error)
}

// AgentStore keeps every revision of every agent definition. Revision N is
// the saige definition at version N.0.0.
type AgentStore interface {
	AgentCatalog
	// Create writes revision 1 of a new agent, or the next revision of a
	// deleted one.
	Create(ctx context.Context, tenant string, spec AgentSpec, by string) (Revision, error)
	// Revise writes the next revision. expectedRev 0 skips the check.
	Revise(ctx context.Context, tenant string, spec AgentSpec, expectedRev int, by string) (Revision, error)
	// Delete retires an agent. Its revisions stay readable.
	Delete(ctx context.Context, tenant, name string, expectedRev int) error
	// Resolver pins a run's definition from the store.
	Resolver() *definition.Resolver
}

// Errors a store or a definition reports.
var (
	ErrNotFound    = errors.New("registry: not found")
	ErrExists      = errors.New("registry: already exists")
	ErrConflict    = errors.New("registry: conflict")
	ErrInvalidPage = errors.New("registry: invalid")
)

// Agents is an AgentStore over saige definitions in pkg/agents.
type Agents struct {
	*DefinitionCatalog
	src       *agents.Store
	providers []string
	now       func() time.Time
}

var _ AgentStore = (*Agents)(nil)

// NewAgents returns a store over src that accepts the named providers.
func NewAgents(src *agents.Store, providers ...string) *Agents {
	if len(providers) == 0 {
		providers = KnownProviders()
	}
	return &Agents{DefinitionCatalog: NewDefinitionCatalog(src), src: src, providers: providers, now: time.Now}
}

// NewMemoryAgents is an agent store held in memory.
func NewMemoryAgents(providers ...string) *Agents {
	if len(providers) == 0 {
		providers = KnownProviders()
	}
	return NewAgents(agents.New(agents.WithChecks(definition.Checks{Providers: providers})), providers...)
}

// OpenAgents is an agent store kept in st that accepts the named providers.
func OpenAgents(st state.Store, providers ...string) (*Agents, error) {
	if len(providers) == 0 {
		providers = KnownProviders()
	}
	src, err := agents.Open(st, agents.WithChecks(definition.Checks{Providers: providers}))
	if err != nil {
		return nil, err
	}
	return NewAgents(src, providers...), nil
}

// Source is the definition source the store writes, which a runtime resolves
// pins from.
func (a *Agents) Source() *agents.Store { return a.src }

// Resolver pins definitions from the store, checked against its providers.
func (a *Agents) Resolver() *definition.Resolver {
	return definition.NewResolver(a.src, definition.Checks{Providers: a.providers})
}

// Create implements AgentStore.
func (a *Agents) Create(ctx context.Context, tenant string, spec AgentSpec, by string) (Revision, error) {
	if tenant == "" {
		return Revision{}, fmt.Errorf("%w: no tenant", ErrInvalidAgent)
	}
	spec = spec.Normalize()
	all, err := a.src.Versions(ctx, tenant, spec.Name)
	if err != nil && !errors.Is(err, definition.ErrNotFound) {
		return Revision{}, err
	}
	if len(all) > 0 {
		if _, retired := a.src.Retired(tenant, spec.Name); !retired {
			return Revision{}, fmt.Errorf("%w: agent %q is already at revision %d", ErrExists, spec.Name, len(all))
		}
		if err := a.src.Revive(ctx, tenant, spec.Name); err != nil {
			return Revision{}, err
		}
	}
	return a.write(ctx, tenant, spec, all, by)
}

// Revise implements AgentStore.
func (a *Agents) Revise(ctx context.Context, tenant string, spec AgentSpec, expectedRev int, by string) (Revision, error) {
	if expectedRev < 0 {
		return Revision{}, fmt.Errorf("%w: expected revision %d is negative", ErrInvalidAgent, expectedRev)
	}
	spec = spec.Normalize()
	if _, err := a.src.Latest(ctx, tenant, spec.Name); err != nil {
		return Revision{}, catalogErr(err)
	}
	all, err := a.src.Versions(ctx, tenant, spec.Name)
	if err != nil {
		return Revision{}, catalogErr(err)
	}
	if expectedRev != 0 && expectedRev != len(all) {
		return Revision{}, fmt.Errorf("%w: agent %q is at revision %d, not %d; re-read it and reapply the change", ErrConflict, spec.Name, len(all), expectedRev)
	}
	return a.write(ctx, tenant, spec, all, by)
}

// write appends spec after the versions held, unless it is the latest's
// content again, which is no new revision.
func (a *Agents) write(ctx context.Context, tenant string, spec AgentSpec, held []definition.Record, by string) (Revision, error) {
	rev := len(held) + 1
	canonical, err := NewRevisionWith(spec, rev, by, a.now(), a.providers)
	if err != nil {
		return Revision{}, err
	}
	if n := len(held); n > 0 {
		last, err := RevisionOf(held[n-1], n)
		if err == nil && last.Spec.SameContent(canonical.Spec) {
			return last, nil
		}
	}
	rec, err := RecordOf(canonical.Spec, rev)
	if err != nil {
		return Revision{}, err
	}
	rec.Tenant, rec.CreatedBy = tenant, by
	stored, err := a.src.Put(ctx, rec)
	if err != nil {
		if errors.Is(err, definition.ErrInvalid) {
			return Revision{}, fmt.Errorf("%w: %w", ErrInvalidAgent, err)
		}
		return Revision{}, err
	}
	return RevisionOf(stored, rev)
}

// Delete implements AgentStore.
func (a *Agents) Delete(ctx context.Context, tenant, name string, expectedRev int) error {
	if expectedRev < 0 {
		return fmt.Errorf("%w: expected revision %d is negative", ErrInvalidAgent, expectedRev)
	}
	name = NormalizeName(name)
	if _, err := a.src.Latest(ctx, tenant, name); err != nil {
		return catalogErr(err)
	}
	all, err := a.src.Versions(ctx, tenant, name)
	if err != nil {
		return catalogErr(err)
	}
	if expectedRev != 0 && expectedRev != len(all) {
		return fmt.Errorf("%w: agent %q is at revision %d, not %d; re-read it before retiring it", ErrConflict, name, len(all), expectedRev)
	}
	return a.src.Retire(ctx, tenant, name, "humagen")
}

// RecordOf converts a spec to the saige definition stored as revision rev.
func RecordOf(spec AgentSpec, rev int) (definition.Record, error) {
	raw, err := json.Marshal(spec)
	if err != nil {
		return definition.Record{}, err
	}
	var old definition.AgentSpec
	if err := json.Unmarshal(raw, &old); err != nil {
		return definition.Record{}, err
	}
	rec, err := definition.FromAgentSpec(old, rev)
	if err != nil {
		var names *definition.NameError
		if errors.As(err, &names) {
			return definition.Record{}, fmt.Errorf("%w: %w", ErrInvalidAgent, err)
		}
		return definition.Record{}, fmt.Errorf("%w: %w", ErrInvalidAgent, err)
	}
	return rec, nil
}

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

// RevOf reads the revision number a definition version holds: N for N.0.0.
func RevOf(version string) int {
	major, _, _ := strings.Cut(version, ".")
	n := 0
	for _, c := range major {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}

// VersionOf is the definition version revision rev is stored as: "N.0.0".
func VersionOf(rev int) string {
	if rev <= 0 {
		return ""
	}
	return strconv.Itoa(rev) + ".0.0"
}
