package platform

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/urmzd/anchor/pkg/ledger"
	dqueue "github.com/urmzd/anchor/pkg/queue"
	"github.com/urmzd/humagen/fiber/pkg/bus"
	"github.com/urmzd/humagen/fiber/pkg/durable"

	"github.com/urmzd/humagen/internal/legacy"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/agentcolors"
	"github.com/urmzd/humagen/pkg/alerts"
	"github.com/urmzd/humagen/pkg/artifacts"
	"github.com/urmzd/humagen/pkg/audit"
	"github.com/urmzd/humagen/pkg/auditrefs"
	"github.com/urmzd/humagen/pkg/blob"
	"github.com/urmzd/humagen/pkg/callpolicy"
	"github.com/urmzd/humagen/pkg/catalog"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/contextflags"
	"github.com/urmzd/humagen/pkg/credentials"
	"github.com/urmzd/humagen/pkg/drive"
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
	"github.com/urmzd/humagen/pkg/personctx"
	"github.com/urmzd/humagen/pkg/policy"
	"github.com/urmzd/humagen/pkg/prefs"
	"github.com/urmzd/humagen/pkg/projectforks"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/readaudit"
	"github.com/urmzd/humagen/pkg/rooms"
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
	// Shared is where the stores that read and write per request keep their
	// records (see pkg/state): feedback. They need no lock, so Shared may be
	// open in any number of processes at once. Nil uses State.
	Shared state.Store
	// Agents is where agent definitions are kept, as saige definitions.
	// Nil keeps them in memory.
	Agents wire.AgentStore
	// Ledger and Queue are anchor's run ledger and queue, which a run in
	// flight survives a restart in. Nil means anchor's in memory. Set both
	// or neither.
	Ledger Ledger
	Queue  dqueue.Queue
	// Bus is fiber's Postgres bus on the same database: events in a table,
	// readers woken by LISTEN/NOTIFY. Nil means none was opened; the bus is
	// then whatever Config.Bus says.
	Bus bus.Bus
	// Counters are the usage ledger's admission and spend counts in the
	// database State is on, shared by every process that opens it. Nil
	// keeps them in this process, which is right for one process only.
	Counters *usage.Counters
	// RoomCounters are a room's rate and each person's runs in flight
	// (pkg/rooms) on the same database. Nil keeps them in this process.
	RoomCounters *rooms.PostgresCounters
}

// OpenPostgres puts every backend on one Postgres database: humagen's state
// (pkg/state), agent definitions (pkg/agents, on the same state), anchor's run ledger and queue
// (fiber's pkg/durable), fiber's Postgres event bus, and the usage ledger's
// shared counts (usage.Counters). Each migrates its own tables, idempotently. The
// caller owns the pool and closes it after calling release.
//
// It is one process's database: pkg/state takes it for this process, and a
// second process gets state.ErrLocked until release is called or the first
// process's connection ends. The stores that read and write per request are
// on Backends.Shared, which takes no lock; the lock stays while any store is
// loaded at start.
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
	names := append(providers(), providerNames...)
	defs, err := wire.OpenAgents(st, names...)
	if err != nil {
		return Backends{}, nil, err
	}
	// Agents fiber's registry kept move into the store once. A platform
	// agent that cannot be moved fails the start.
	if _, err := legacy.ConvertAgents(ctx, st, defs.Source(), legacy.PostgresOldAgents{Pool: pool}, legacy.ConvertOptions{
		Builtin: func(name string) bool { return IsBuiltin(name) || legacy.IsBuiltinAgent(name) },
		Logf:    func(format string, args ...any) { slog.Warn(fmt.Sprintf(format, args...)) },
	}); err != nil {
		return Backends{}, nil, err
	}
	l, q, err := durable.Open(ctx, pool)
	if err != nil {
		return Backends{}, nil, err
	}
	events := bus.NewPostgres(pool)
	if err := events.Migrate(ctx); err != nil {
		return Backends{}, nil, err
	}
	counters, err := usage.OpenCounters(ctx, pool)
	if err != nil {
		return Backends{}, nil, err
	}
	shared, err := state.OpenPostgresShared(ctx, pool)
	if err != nil {
		return Backends{}, nil, err
	}
	roomCounters, err := rooms.OpenPostgres(ctx, pool)
	if err != nil {
		return Backends{}, nil, err
	}
	return Backends{State: st, Shared: shared, Agents: defs, Ledger: l, Queue: q, Bus: events, Counters: counters, RoomCounters: roomCounters}, st.Close, nil
}

func (b Backends) check() error {
	if (b.Ledger == nil) != (b.Queue == nil) {
		return errors.New("platform: backends: set the run ledger and queue together, or neither")
	}
	if b.Counters != nil && b.State == nil {
		return errors.New("platform: backends: shared usage counters need the state store on their database")
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
	p.CatalogStore, err = catalog.Open(st)
	keep("catalog", err)
	p.SourceEvidence, err = knowledge.OpenEvidenceStore(st)
	keep("source citation evidence", err)
	p.Evals, err = evals.Open(st)
	keep("evals", err)
	perRequest := cfg.Backends.Shared
	if perRequest == nil {
		perRequest = st
	}
	p.Feedback, err = feedback.Open(perRequest)
	keep("feedback", err)
	p.Prefs, err = prefs.Open(st)
	keep("prefs", err)
	p.PersonContexts, err = personctx.Open(st)
	keep("person contexts", err)
	p.ContextFlags, err = contextflags.Open(st)
	keep("private context flags", err)
	p.Notifications, err = notifications.Open(st)
	keep("native notifications", err)
	p.AgentSkillDefaults, err = library.OpenDefaults(st)
	keep("agent skill defaults", err)
	p.EmailNotifications, err = notifications.OpenEmail(st)
	keep("email notifications", err)
	p.SkillLibrary, err = library.Open(st)
	keep("skill library", err)
	// Memory is files: manifests on st, content in the configured blob
	// store. With none, or one in memory, the content is kept beside the
	// manifests, so a restart that keeps the notes' places keeps their text.
	driveState := st
	if driveState == nil {
		driveState = state.NewMemory()
	}
	var driveBlobs blob.Store = blob.NewState(driveState)
	if _, inMemory := cfg.Blobs.(*blob.Memory); cfg.Blobs != nil && !inMemory {
		driveBlobs = cfg.Blobs
	}
	p.Drive, err = drive.Open(driveState, driveBlobs, drive.WithScreen(memory.Screen), drive.WithClock(p.now))
	keep("drive", err)
	if err == nil {
		p.Memory, err = memory.Open(st, p.Drive)
		keep("memory", err)
	}
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
	p.setups, err = openAgentSetups(st)
	keep("agent setups", err)
	keep("agent setup admission", p.RegisterAgentTestGate(setupGate{p: p}))
	p.Operations, err = operations.Open(perRequest)
	keep("operations", err)
	if err == nil {
		keep("operation publication guard", p.RegisterPublicationGuard(p))
	}
	p.CallPolicies, err = callpolicy.Open(st)
	keep("call policies", err)
	var shared []usage.Option
	if cfg.Backends.Counters != nil {
		shared = append(shared, usage.WithCounters(cfg.Backends.Counters))
	}
	p.Usage, err = usage.Open(st, shared...)
	keep("usage", err)
	p.AuditReferences, err = auditrefs.Open(perRequest)
	keep("audit references", err)
	p.Audit, err = audit.Open(perRequest)
	keep("audit", err)
	p.ReadAudit, err = readaudit.Open(perRequest)
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
