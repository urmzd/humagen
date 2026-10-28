package legacy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/urmzd/humagen/fiber/pkg/definition"
	"github.com/urmzd/humagen/pkg/agents"
	"github.com/urmzd/humagen/pkg/state"
)

// BuiltinAgents maps the handles from before the rename to their current
// handles. The old revisions remain addressable for saved runs and grants.
func BuiltinAgents() map[string]string {
	return map[string]string{
		"warp-writer":  "humagen-writer",
		"warp-analyst": "humagen-analyst",
		"warp-builder": "humagen-builder",
	}
}

// IsBuiltinAgent reports whether name is a handle from before the rename.
func IsBuiltinAgent(name string) bool { _, ok := BuiltinAgents()[name]; return ok }

// BuiltinAgent returns the current spelling, leaving other names unchanged.
func BuiltinAgent(name string) string {
	if current, ok := BuiltinAgents()[name]; ok {
		return current
	}
	return name
}

// BuiltinAgentKey keeps an agent's color reservation at its original key.
func BuiltinAgentKey(name string) string {
	for old, current := range BuiltinAgents() {
		if name == current {
			return old
		}
	}
	return name
}

// Agents were kept by fiber's registry, in fiber_agents and
// fiber_agent_revisions, as AgentSpec JSON with a revision number. They are
// read only here, to move them once into saige definitions (pkg/agents).

// AgentsConvertedBy retires an agent that was deleted before the move.
const AgentsConvertedBy = "system:legacy-agents"

// agentsMarker is the state record that says the move ran.
const (
	agentsMarkerNamespace = "legacy.agents"
	agentsMarkerKey       = "converted"
)

// OldRevision is one stored revision of an old agent.
type OldRevision struct {
	Rev       int
	Spec      json.RawMessage
	CreatedAt time.Time
	CreatedBy string
}

// OldAgent is one old agent name in one tenant, with every revision.
type OldAgent struct {
	Tenant    string
	Name      string
	Deleted   bool
	Revisions []OldRevision
}

// OldAgents reads the agents kept before the move.
type OldAgents interface {
	LoadOldAgents(ctx context.Context) ([]OldAgent, error)
}

// OldAgentRows is a fixed set of old agents.
type OldAgentRows []OldAgent

// LoadOldAgents implements OldAgents.
func (r OldAgentRows) LoadOldAgents(context.Context) ([]OldAgent, error) { return r, nil }

// PostgresOldAgents reads fiber's registry tables, first renaming them from
// the prefix before the rename. A database that never had them holds no old
// agents.
type PostgresOldAgents struct{ Pool *pgxpool.Pool }

// LoadOldAgents implements OldAgents.
func (p PostgresOldAgents) LoadOldAgents(ctx context.Context) ([]OldAgent, error) {
	if _, err := p.Pool.Exec(ctx, FiberTablesSQL("agents", "agent_revisions")); err != nil {
		return nil, fmt.Errorf("legacy: rename old agent tables: %w", err)
	}
	var present bool
	if err := p.Pool.QueryRow(ctx,
		`SELECT to_regclass('fiber_agents') IS NOT NULL AND to_regclass('fiber_agent_revisions') IS NOT NULL`,
	).Scan(&present); err != nil {
		return nil, fmt.Errorf("legacy: look for old agents: %w", err)
	}
	if !present {
		return nil, nil
	}
	rows, err := p.Pool.Query(ctx, `
		SELECT a.tenant, a.name, a.deleted, r.rev, r.spec, r.created_at, r.created_by
		  FROM fiber_agents a
		  JOIN fiber_agent_revisions r ON r.tenant = a.tenant AND r.name = a.name
		 ORDER BY a.tenant, a.name, r.rev`)
	if err != nil {
		return nil, fmt.Errorf("legacy: read old agents: %w", err)
	}
	defer rows.Close()
	var out []OldAgent
	for rows.Next() {
		var (
			a   OldAgent
			rev OldRevision
		)
		if err := rows.Scan(&a.Tenant, &a.Name, &a.Deleted, &rev.Rev, &rev.Spec, &rev.CreatedAt, &rev.CreatedBy); err != nil {
			return nil, fmt.Errorf("legacy: read old agents: %w", err)
		}
		if n := len(out); n > 0 && out[n-1].Tenant == a.Tenant && out[n-1].Name == a.Name {
			out[n-1].Revisions = append(out[n-1].Revisions, rev)
			continue
		}
		a.Revisions = []OldRevision{rev}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("legacy: read old agents: %w", err)
	}
	return out, nil
}

// DisabledAgent is an old agent that was not moved, and why.
type DisabledAgent struct {
	Tenant string `json:"tenant"`
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

// AgentConversion is what the move did. It is kept with the marker, so the
// setup status can show it on every start, not only the first.
type AgentConversion struct {
	At        time.Time       `json:"at"`
	Converted int             `json:"converted"`
	Versions  int             `json:"versions"`
	Disabled  []DisabledAgent `json:"disabled,omitempty"`
}

// ConvertOptions say how the move treats each agent.
type ConvertOptions struct {
	// Builtin reports a platform agent. One that cannot be moved fails the
	// move, and so the start: the platform does not run without its own
	// agents. Nil means none is.
	Builtin func(name string) bool
	// Logf, when set, is told of every disabled agent.
	Logf func(format string, args ...any)
	// Now is when the move ran. Nil is time.Now.
	Now func() time.Time
}

// ConvertAgents moves every old agent into dst once. Revisions become
// versions N.0.0 through definition.FromAgentSpec, in revision order, keeping
// who wrote each and when. A deleted agent is retired after its versions are
// moved. An agent that cannot be moved, such as one whose name breaks saige's
// rule, is disabled: it is left out and reported, and the others move. A
// builtin that cannot be moved is an error and nothing is marked done.
//
// The marker is written last and every write before it is idempotent, so a
// move cut short is finished by the next start. Once marked, later calls
// return the kept report and read nothing.
func ConvertAgents(ctx context.Context, st state.Store, dst *agents.Store, old OldAgents, opts ConvertOptions) (AgentConversion, error) {
	marker := state.NewTable[AgentConversion](st, agentsMarkerNamespace)
	if done, ok, err := AgentConversionOf(ctx, st); err != nil || ok {
		return done, err
	}
	list, err := old.LoadOldAgents(ctx)
	if err != nil {
		return AgentConversion{}, err
	}
	list = slices.Clone(list)
	slices.SortStableFunc(list, func(a, b OldAgent) int {
		if c := strings.Compare(a.Tenant, b.Tenant); c != 0 {
			return c
		}
		return strings.Compare(a.Name, b.Name)
	})
	now := time.Now
	if opts.Now != nil {
		now = opts.Now
	}
	report := AgentConversion{At: now().UTC()}
	for _, a := range list {
		recs, err := convertAgent(a)
		if err == nil {
			_, err = dst.Import(ctx, recs)
		}
		if err != nil {
			if opts.Builtin != nil && opts.Builtin(a.Name) {
				return AgentConversion{}, fmt.Errorf("legacy: the platform agent %q cannot be moved to a saige definition: %w", a.Name, err)
			}
			if !errors.Is(err, definition.ErrInvalid) && !errors.Is(err, definition.ErrExists) {
				return AgentConversion{}, fmt.Errorf("legacy: move agent %q of %q: %w", a.Name, a.Tenant, err)
			}
			d := DisabledAgent{Tenant: a.Tenant, Name: a.Name, Reason: err.Error()}
			report.Disabled = append(report.Disabled, d)
			if opts.Logf != nil {
				opts.Logf("agent %q in %q is disabled: %s", d.Name, d.Tenant, d.Reason)
			}
			continue
		}
		if a.Deleted {
			if err := dst.Retire(ctx, a.Tenant, recs[0].Name, AgentsConvertedBy); err != nil {
				return AgentConversion{}, fmt.Errorf("legacy: retire agent %q of %q: %w", a.Name, a.Tenant, err)
			}
		}
		report.Converted++
		report.Versions += len(recs)
	}
	if err := marker.Put("", agentsMarkerKey, report); err != nil {
		return AgentConversion{}, fmt.Errorf("legacy: mark agents moved: %w", err)
	}
	return report, nil
}

// AgentConversionOf returns the kept report, and whether the move has run.
func AgentConversionOf(ctx context.Context, st state.Store) (AgentConversion, bool, error) {
	if st == nil {
		return AgentConversion{}, false, nil
	}
	raw, err := st.Get(ctx, agentsMarkerNamespace, "", agentsMarkerKey)
	switch {
	case errors.Is(err, state.ErrNotFound):
		return AgentConversion{}, false, nil
	case err != nil:
		return AgentConversion{}, false, fmt.Errorf("legacy: read the agents marker: %w", err)
	}
	var done AgentConversion
	if err := json.Unmarshal(raw, &done); err != nil {
		return AgentConversion{}, false, fmt.Errorf("legacy: read the agents marker: %w", err)
	}
	return done, true, nil
}

// convertAgent turns every revision of one old agent into a record, in
// revision order.
func convertAgent(a OldAgent) ([]definition.Record, error) {
	if len(a.Revisions) == 0 {
		return nil, fmt.Errorf("%w: agent %q has no revisions", definition.ErrInvalid, a.Name)
	}
	revs := slices.Clone(a.Revisions)
	slices.SortStableFunc(revs, func(x, y OldRevision) int { return x.Rev - y.Rev })
	out := make([]definition.Record, 0, len(revs))
	for _, r := range revs {
		var spec definition.AgentSpec
		if err := json.Unmarshal(r.Spec, &spec); err != nil {
			return nil, fmt.Errorf("%w: agent %q revision %d: %v", definition.ErrInvalid, a.Name, r.Rev, err)
		}
		rec, err := definition.FromAgentSpec(spec, r.Rev)
		if err != nil {
			return nil, err
		}
		if rec.Name != definition.NormalizeName(a.Name) {
			return nil, fmt.Errorf("%w: agent %q revision %d names %q", definition.ErrInvalid, a.Name, r.Rev, rec.Name)
		}
		rec.Tenant, rec.CreatedAt, rec.CreatedBy = a.Tenant, r.CreatedAt.UTC(), r.CreatedBy
		out = append(out, rec)
	}
	return out, nil
}
