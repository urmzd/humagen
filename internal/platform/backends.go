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

	"github.com/urmzd/humagen/pkg/agentcolors"
	"github.com/urmzd/humagen/pkg/alerts"
	"github.com/urmzd/humagen/pkg/artifacts"
	"github.com/urmzd/humagen/pkg/audit"
	"github.com/urmzd/humagen/pkg/auditrefs"
	"github.com/urmzd/humagen/pkg/callpolicy"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/contextflags"
	"github.com/urmzd/humagen/pkg/credentials"
	"github.com/urmzd/humagen/pkg/evals"
	"github.com/urmzd/humagen/pkg/feedback"
	"github.com/urmzd/humagen/pkg/inbox"
	"github.com/urmzd/humagen/pkg/journal"
	"github.com/urmzd/humagen/pkg/knowledge"
	"github.com/urmzd/humagen/pkg/library"
	"github.com/urmzd/humagen/pkg/memory"
	"github.com/urmzd/humagen/pkg/notifications"
	"github.com/urmzd/humagen/pkg/operations"
	"github.com/urmzd/humagen/pkg/people"
	"github.com/urmzd/humagen/pkg/policy"
	"github.com/urmzd/humagen/pkg/prefs"
	"github.com/urmzd/humagen/pkg/projectforks"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/readaudit"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/sourcelinks"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/threads"
	"github.com/urmzd/humagen/pkg/usage"
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
	// State is where humagen's own stores keep their records: clients and
	// their secrets (unless Config sets its own), people's own credentials
	// for tool providers (their values beside the clients' secrets), the
	// inbox, preferences, memory, projects, threads, limits, call policies,
	// alerts, evals, schedules, the journal,
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

// OpenPostgres puts every backend on one Postgres database: humagen's state
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

// openStores builds humagen's own stores on st, reading back what it holds. A
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
	p.AgentColors, err = agentcolors.Open(st)
	keep("agent colors", err)
	p.Runs, err = runs.Open(st)
	keep("runs", err)
	p.Schedules, err = OpenSchedules(st)
	keep("schedules", err)
	p.Journal, err = journal.Open(st, 0)
	keep("journal", err)
	p.Alerts, err = alerts.Open(st)
	keep("alerts", err)
	p.SourceLinks, err = sourcelinks.Open(st)
	keep("source link rules", err)
	p.SourceSuggestions, err = sourcelinks.OpenSuggestions(st)
	keep("source link suggestions", err)
	p.Knowledge, err = knowledge.Open(st)
	keep("knowledge", err)
	p.SourceEvidence, err = knowledge.OpenEvidenceStore(st)
	keep("source citation evidence", err)
	p.Evals, err = evals.Open(st)
	keep("evals", err)
	p.Feedback, err = feedback.Open(st)
	keep("feedback", err)
	p.Prefs, err = prefs.Open(st)
	keep("prefs", err)
	p.ContextFlags, err = contextflags.Open(st)
	keep("private context flags", err)
	p.Notifications, err = notifications.Open(st)
	keep("native notifications", err)
	p.SkillLibrary, err = library.Open(st)
	keep("skill library", err)
	p.Memory, err = memory.Open(st)
	keep("memory", err)
	// A deleted name is forgotten only once nothing is filed under it.
	p.ProjectForks, err = projectforks.Open(st)
	keep("project forks", err)
	p.Projects, err = projects.Open(st, projects.WithFiled(p.filedUnder))
	keep("projects", err)
	// An agent's name is a handle no person is given.
	p.People, err = people.Open(st, people.WithTaken(p.agentTaken))
	keep("people", err)
	p.Threads, err = threads.Open(st)
	keep("threads", err)
	p.Policies, err = policy.Open(st)
	keep("policies", err)
	p.Operations, err = operations.Open(st)
	keep("operations", err)
	if err == nil {
		keep("operation publication guard", p.RegisterPublicationGuard(p))
	}
	p.CallPolicies, err = callpolicy.Open(st)
	keep("call policies", err)
	p.Usage, err = usage.Open(st)
	keep("usage", err)
	p.AuditReferences, err = auditrefs.Open(st)
	keep("audit references", err)
	p.Audit, err = audit.Open(st)
	keep("audit", err)
	p.ReadAudit, err = readaudit.Open(st)
	keep("read audit", err)
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
