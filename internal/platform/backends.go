package platform

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/urmzd/anchor/pkg/ledger"
	dqueue "github.com/urmzd/anchor/pkg/queue"
	"github.com/urmzd/fiber/pkg/durable"
	"github.com/urmzd/fiber/pkg/registry"

	"github.com/urmzd/warp/pkg/alerts"
	"github.com/urmzd/warp/pkg/artifacts"
	"github.com/urmzd/warp/pkg/callpolicy"
	"github.com/urmzd/warp/pkg/clients"
	"github.com/urmzd/warp/pkg/credentials"
	"github.com/urmzd/warp/pkg/evals"
	"github.com/urmzd/warp/pkg/inbox"
	"github.com/urmzd/warp/pkg/journal"
	"github.com/urmzd/warp/pkg/knowledge"
	"github.com/urmzd/warp/pkg/memory"
	"github.com/urmzd/warp/pkg/people"
	"github.com/urmzd/warp/pkg/policy"
	"github.com/urmzd/warp/pkg/prefs"
	"github.com/urmzd/warp/pkg/projects"
	"github.com/urmzd/warp/pkg/runs"
	"github.com/urmzd/warp/pkg/state"
	"github.com/urmzd/warp/pkg/usage"
)

// Ledger is the run ledger the runtime executes from and the platform reads
// endings from.
type Ledger interface {
	ledger.Ledger
	ledger.RunGetter
}

// Backends are where the platform keeps what must outlive the process. The
// zero value keeps everything in memory, as a test wants. Each is chosen on
// its own; OpenPostgres chooses all of them on one database.
type Backends struct {
	// State is where warp's own stores keep their records: clients and
	// their secrets (unless Config sets its own), people's own credentials
	// for tool providers (their values beside the clients' secrets), the
	// inbox, preferences,
	// memory, projects, limits, call policies, alerts, evals, schedules, the journal,
	// artifacts, the run index, the usage ledger, knowledge and workspace
	// settings. Nil keeps them in memory.
	State state.Store
	// Agents is fiber's agent registry. Nil means fiber's in memory.
	Agents registry.Store
	// Ledger and Queue are anchor's run ledger and queue, which a run in
	// flight survives a restart in. Nil means anchor's in memory. Set both
	// or neither.
	Ledger Ledger
	Queue  dqueue.Queue
}

// OpenPostgres puts every backend on one Postgres database: warp's state
// (pkg/state), fiber's agent registry, and anchor's run ledger and queue
// (fiber's pkg/durable). Each migrates its own tables, idempotently. The
// caller owns the pool and closes it after calling release.
//
// It is one process's database: pkg/state takes it for this process, and a
// second process gets state.ErrLocked until release is called or the first
// process's connection ends.
//
// providers are model provider names beyond fiber's built-ins that agent
// specs may name, such as the extension providers a program registers.
func OpenPostgres(ctx context.Context, pool *pgxpool.Pool, providerNames ...string) (b Backends, release func() error, err error) {
	st, err := state.OpenPostgres(ctx, pool)
	if err != nil {
		return Backends{}, nil, err
	}
	defer func() {
		if err != nil {
			_ = st.Close()
		}
	}()
	agents := registry.NewPostgres(pool, registry.WithProviders(append(providers(), providerNames...)...))
	if err := agents.Migrate(ctx); err != nil {
		return Backends{}, nil, err
	}
	l, q, err := durable.Open(ctx, pool)
	if err != nil {
		return Backends{}, nil, err
	}
	return Backends{State: st, Agents: agents, Ledger: l, Queue: q}, st.Close, nil
}

func (b Backends) check() error {
	if (b.Ledger == nil) != (b.Queue == nil) {
		return errors.New("platform: backends: set the run ledger and queue together, or neither")
	}
	return nil
}

// openStores builds warp's own stores on st, reading back what it holds. A
// nil st builds them in memory.
func (p *Platform) openStores(cfg Config) error {
	st := cfg.Backends.State
	var errs []error
	keep := func(what string, err error) {
		if err != nil {
			errs = append(errs, fmt.Errorf("platform: %s: %w", what, err))
		}
	}
	var err error
	p.Runs, err = runs.Open(st)
	keep("runs", err)
	p.Schedules, err = OpenSchedules(st)
	keep("schedules", err)
	p.Journal, err = journal.Open(st, 0)
	keep("journal", err)
	p.Alerts, err = alerts.Open(st)
	keep("alerts", err)
	p.Knowledge, err = knowledge.Open(st)
	keep("knowledge", err)
	p.Evals, err = evals.Open(st)
	keep("evals", err)
	p.Prefs, err = prefs.Open(st)
	keep("prefs", err)
	p.Memory, err = memory.Open(st)
	keep("memory", err)
	// A deleted name is forgotten only once nothing is filed under it.
	p.Projects, err = projects.Open(st, projects.WithFiled(p.filedUnder))
	keep("projects", err)
	// An agent's name is a handle no person is given.
	p.People, err = people.Open(st, people.WithTaken(p.agentTaken))
	keep("people", err)
	p.Policies, err = policy.Open(st)
	keep("policies", err)
	p.CallPolicies, err = callpolicy.Open(st)
	keep("call policies", err)
	p.Usage, err = usage.Open(st)
	keep("usage", err)
	p.Inbox, err = inbox.OpenStore(st)
	keep("inbox", err)
	p.Artifacts, err = artifacts.Open(st)
	keep("artifacts", err)
	keep("settings", p.settings.open(st))
	if len(errs) == 0 {
		// Projects once lived in settings; they move before anything reads
		// either.
		keep("projects", p.migrateProjects())
	}
	if len(errs) == 0 {
		// The baseline becomes the workspace's default, on the root the
		// projects' move may just have written.
		keep("agents", p.migrateBaseline())
	}

	p.Clients, p.secrets = cfg.Clients, cfg.Secrets
	if p.Clients == nil {
		p.Clients, err = clients.Open(st)
		keep("clients", err)
	}
	if p.secrets == nil {
		p.secrets, err = clients.OpenSecrets(st)
		keep("client secrets", err)
	}
	// People's values are kept where client secrets are, and guarded the
	// same way.
	p.Credentials, err = credentials.Open(st, p.secrets)
	keep("credentials", err)
	return errors.Join(errs...)
}

// flush writes what the stores that write behind have not written yet: the
// journal, the run index and the usage ledger.
func (p *Platform) flush() {
	if err := p.Journal.Flush(); err != nil {
		p.log.Error("platform: journal could not be written", "error", err)
	}
	if err := p.Runs.Flush(); err != nil {
		p.log.Error("platform: runs could not be written", "error", err)
	}
	if err := p.Usage.Flush(); err != nil {
		p.log.Error("platform: usage could not be written", "error", err)
	}
}
