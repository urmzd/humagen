// Package platform assembles the system: fiber's connector registry, bus,
// egress and durable runtime, the cron trigger, and this repo's run index and
// call path.
//
//	trigger or API  →  Invoke  →  runtime  →  bus  →  egress  →  sinks
//	                     ↓                     ↓
//	                 run index  ←──────────  observer
//
// Read New top to bottom and you have read how the platform fits together.
package platform

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/urmzd/anchor"
	"github.com/urmzd/anchor/pkg/ledger"
	dqueue "github.com/urmzd/anchor/pkg/queue"
	drun "github.com/urmzd/anchor/pkg/run"
	"github.com/urmzd/anchor/pkg/worker"
	"github.com/urmzd/humagen/fiber/pkg/attachment/resolve"
	filesblob "github.com/urmzd/humagen/fiber/pkg/blob"
	"github.com/urmzd/humagen/fiber/pkg/bus"
	"github.com/urmzd/humagen/fiber/pkg/connector"
	"github.com/urmzd/humagen/fiber/pkg/connectors/cron"
	"github.com/urmzd/humagen/fiber/pkg/egress"
	"github.com/urmzd/humagen/fiber/pkg/event"
	"github.com/urmzd/humagen/fiber/pkg/isolation"
	"github.com/urmzd/humagen/fiber/pkg/runtime"
	"github.com/urmzd/humagen/fiber/pkg/toolprovider"
	"github.com/urmzd/humagen/fiber/pkg/turn"
	"github.com/urmzd/humagen/fiber/pkg/turn/native"
	"github.com/urmzd/humagen/fiber/pkg/turn/sandbox"
	"github.com/urmzd/playscape/pkg/controlplane"
	"github.com/urmzd/playscape/pkg/metrics"
	"github.com/urmzd/playscape/pkg/node/inproc"
	"github.com/urmzd/playscape/pkg/tool"
	"github.com/urmzd/playscape/pkg/workspace"

	"github.com/urmzd/humagen/internal/legacy"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/agentcolors"
	"github.com/urmzd/humagen/pkg/alerts"
	"github.com/urmzd/humagen/pkg/artifacts"
	"github.com/urmzd/humagen/pkg/audit"
	"github.com/urmzd/humagen/pkg/auditrefs"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/blob"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/callpolicy"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/contextflags"
	"github.com/urmzd/humagen/pkg/credentials"
	"github.com/urmzd/humagen/pkg/drive"
	"github.com/urmzd/humagen/pkg/evals"
	"github.com/urmzd/humagen/pkg/feedback"
	"github.com/urmzd/humagen/pkg/inbox"
	"github.com/urmzd/humagen/pkg/integrations/github"
	"github.com/urmzd/humagen/pkg/integrations/slack"
	"github.com/urmzd/humagen/pkg/join"
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
	"github.com/urmzd/humagen/pkg/threads"
	"github.com/urmzd/humagen/pkg/usage"
)

// ErrForbidden reports a call the caller's agent spec does not allow.
var ErrForbidden = errors.New("platform: forbidden")

// Config is what a running platform needs. Nothing here reads the environment,
// so a test can build a second platform in the same process.
type Config struct {
	// Rooms is how conversations with many viewers are read and how fast
	// runs are admitted into them (pkg/rooms). Zero fields are its defaults.
	Rooms rooms.Config
	// RoomCounters keeps the room gate's counts. Nil is
	// Backends.RoomCounters, or in memory without them.
	RoomCounters rooms.Counters
	// ProjectTemplates replaces the embedded file-defined catalog when non-nil.
	ProjectTemplates []projects.Template
	// Email is a configured relay and explicit tenant/subject recipient mapping. Nil disables email.
	Email *EmailConfig
	// Wording is each workspace's product name and the welcome and email
	// text its branding replaces. Nil is Humagen's own.
	Wording func(tenant string) Wording

	// AssignmentSources overrides the native resident source cursor for an embedding boundary. Nil uses this platform.
	AssignmentSources operations.SourceChanges
	// SourceDependencies optionally projects authoritative extra-store origin references.
	SourceDependencies SourceDependencyReader

	// Backends are where everything the platform keeps is kept. The zero
	// value is in memory, lost with the process. OpenPostgres fills it from
	// one database.
	Backends Backends
	// Connections seats connectors the embedding program built itself, after
	// the registry exists. Clients created through CreateClient need none.
	Connections func(wire.AgentStore) ([]Connection, error)
	// Clients keeps client records, and Secrets the secret values written for
	// them. Nil means on Backends.State, or in memory without one. Enabled
	// clients already in the store are started by New.
	Clients clients.Store
	Secrets clients.Secrets
	// Env reads the process's environment for an "env:NAME" secret reference.
	// Nil resolves none.
	Env func(name string) (string, bool)
	// SecretRefs is which environment variables each tenant's clients may
	// reference, by tenant. The environment belongs to the whole process, so
	// this is the tenant boundary on it: a name is read for a tenant only if
	// it is listed here for that tenant. It is the server's configuration and
	// nothing in the API can change it. A tenant with no entry may reference
	// nothing, and can still write its secrets as values.
	SecretRefs map[string][]string
	// CredentialRefs is which references each tenant's people may give for
	// their own tool credentials, by tenant: each entry is how a reference
	// starts, naming the person whose it is ("op://People/{subject}/",
	// "keychain:humagen-people-{subject}/", "env:HUMAGEN_PERSON_{subject}_"), and
	// a person may give only their own (credentials.CheckStarts, Owns). A
	// reference is read with this process's own access (its environment,
	// its keychain, the 1Password account it signed in to), so this is the
	// server's configuration and nothing in the API can change it. A tenant
	// with no entry may reference nothing.
	CredentialRefs map[string][]string
	// CredentialValues is the tenants whose people may store a credential's
	// value with humagen, kept in Secrets as a client's secret is. Everyone
	// else may only reference one.
	CredentialValues map[string]bool
	// InboxRetention is how long each tenant's inbox keeps what was settled
	// for everyone before it is dropped, by tenant, as its Platform document
	// sets workspace.inbox_retention. A tenant with no entry keeps it
	// inbox.DefaultRetention, and none keeps it less than
	// inbox.MinRetention.
	InboxRetention map[string]time.Duration
	// MentionKinds are the mention kinds each tenant's Platform document
	// declares besides the built-in ones, by tenant, as
	// workspace.mention_kinds sets them. The file owns them: nothing in the
	// API changes them.
	MentionKinds map[string][]MentionKind
	// SlackAPI and GitHubAPI build the API a client writes through from its
	// token. Nil means the real one over HTTP.
	SlackAPI  func(token string) slack.API
	GitHubAPI func(token string) github.API
	// ClientInterval coalesces a client's edits to its surface. Zero means
	// the connector's default.
	ClientInterval time.Duration
	// Workspace is the directory agents write artifacts under. Empty takes a
	// directory under the user's cache.
	Workspace string
	// Sandbox is where a run's tools execute: each turn's file tools,
	// shell and stdio MCP servers in an instance its Leaser starts for
	// that run (RFC 0004). The zero value is unisolated: tools run on this
	// host as they always did, the shell is refused, and New says so.
	Sandbox isolation.Sandbox
	// Blobs holds the bytes of published files. Nil means a disk store in
	// the workspace directory.
	Blobs blob.Store
	// Embedder gives knowledge passages vectors for hybrid search, filled in
	// the background while Run runs. Nil keeps search lexical.
	Embedder knowledge.Embedder
	// Bus carries every run's events. Nil means in memory, for one process.
	Bus bus.Bus
	// Limits bounds every tree of calls.
	Limits call.Limits
	// Concurrency is how many runs execute at once. Zero means
	// defaultConcurrency.
	Concurrency int
	// StopGrace is how long a run asked to stop is given to wrap up before
	// its turn is cancelled. Zero means the runtime's default, ten seconds.
	StopGrace time.Duration
	// Stops replaces how a run is asked to stop. Nil means the runtime.
	// For tests only: it exists to test a deployment whose runtime cannot
	// interrupt a run, and a server never sets it.
	Stops func(context.Context, runtime.StopRequest) error
	// Backoff replaces how long a failed attempt waits before it is tried
	// again. Nil means anchor's, which doubles from a quarter second. For
	// tests only: it lets a test fail a run through all its attempts quickly.
	Backoff func(attempt int) time.Duration
	// QuestionTTL is how long a question a run parks on waits for an answer
	// before its default applies. Zero means the runtime's, a day.
	QuestionTTL time.Duration
	// OfflineHooks lets housekeeping run on the offline provider. It exists
	// for tests: an offline answer is a notice, not a title or a fact.
	OfflineHooks bool
	// BuiltinProvider and BuiltinModel are what the platform's own agents
	// run on. With either empty there are no built-in agents.
	BuiltinProvider string
	BuiltinModel    string
	// AgentOptions are passed to every agent the sandbox builds. A test
	// uses them to script a model.
	AgentOptions []native.Option
	// Runtime, when set, runs every turn in place of the native one, inside
	// the same playscape sandbox. It brings its own models and tools: the
	// native options above do not reach it. Nil runs the native turn.
	Runtime turn.Runtime
	Log     *slog.Logger
	// Now is the clock. Nil means time.Now.
	Now func() time.Time
	// Router, when set, asks a workspace's agents whether to take a message
	// that names none (pkg/join), in place of the model the workspace's
	// settings name. A nil Asker for a workspace asks nobody there.
	Router func(tenant string) join.Asker
	// Ext is what a program built on humagen adds: model providers, tool
	// sources and MCP tool providers. The zero value adds nothing.
	Ext Extensions
	// Policy is the workspace's policy, which says what a person holds by
	// their subject alone. It decides what the person a client's own
	// connector maps a Slack user to may do, as a principal's policy does
	// for a caller of the API: nobody signed in, so the roles it grants
	// their subject are all that is known of them. Nil is the built-in
	// policy, which grants nobody anything by subject.
	Policy *auth.Policy
}

// Platform is the assembled system.
type Platform struct {
	// Rooms holds the conversations followed on this server (pkg/rooms).
	Rooms    *rooms.Hub
	roomGate *rooms.Gate
	files    filesblob.Store
	// health is the last health report and each component's recent checks.
	health healthBook

	cfg                    Config
	log                    *slog.Logger
	now                    func() time.Time
	grounding              sourceGrounding
	sourceDependencyMu     sync.RWMutex
	sourceDependencyReader SourceDependencyReader

	// Agents keeps agent definitions. Read them through Catalog.
	Agents      wire.AgentStore
	Runs        *runs.Index
	Schedules   *Schedules
	Journal     *journal.Journal
	Alerts      *alerts.Engine
	Knowledge   *knowledge.Store
	SourceLinks *sourcelinks.Store
	Evals       *evals.Store
	Feedback    *feedback.Store
	Prefs       *prefs.Store
	// PersonContexts is what each person brings to the runs that act for
	// them: their run settings, tool servers and memory volume.
	PersonContexts       *personctx.Store
	runSettings          runSettings
	Memory               *memory.Store
	Drive                *drive.Store // the versioned file trees memory is kept in
	SkillLibrary         *library.Store
	AgentSkillDefaults   *library.DefaultsStore
	skillDefaultsMu      sync.Mutex
	Notifications        *notifications.Store
	ContextFlags         *contextflags.Store
	SourceSuggestions    *sourcelinks.Suggestions
	SourceEvidence       *knowledge.EvidenceStore
	EmailNotifications   *notifications.Store
	emailCursor          int
	emailMu              sync.Mutex
	emailPreferenceLocks [64]sync.Mutex
	// AgentColors reserves each workspace's stable agent hues.
	AgentColors *agentcolors.Store
	// Projects is each tenant's tree of projects.
	Projects     *projects.Store
	ProjectForks *projectforks.Store
	// People is each tenant's directory of people.
	People *people.Store
	// Threads is each tenant's shared threads: their records and the
	// messages people post in them.
	Threads  *threads.Store
	Policies *policy.Store
	// CallPolicies is what each agent says about being called.
	operationContexts          sync.Map
	agentTestGates             sync.Map
	assignmentSources          sync.Map
	assignmentSourceCursor     sync.Map
	assignmentSettlementCursor sync.Map
	operationReminderCursor    sync.Map
	operationScheduleCursor    sync.Map
	CallPolicies               *callpolicy.Store
	Operations                 *operations.Store
	Usage                      *usage.Ledger
	Inbox                      *inbox.Store
	ReadAudit                  *readaudit.Store
	Audit                      *audit.Store
	Artifacts                  *artifacts.Index
	setups                     *agentSetups

	AuditReferences *auditrefs.Store
	// Clients is the external clients each tenant answers through.
	Clients clients.Store
	// Credentials is each person's own credentials for person-scoped tool
	// providers. Values are kept in the store client secrets are kept in.
	Credentials *credentials.Store
	// Blobs holds the bytes of published files.
	Blobs blob.Store
	// ToolProviders is the MCP servers agents reach through "mcp:<name>"
	// grants, and the tools accepted from each.
	ToolProviders toolprovider.Store
	// closers release what New opened, in reverse, on Close.
	closers []func() error

	// dir is the workspace directory, resolved once.
	dir string
	// waiting counts runs that are waiting on a run attached to them.
	waiting atomic.Int64
	// publishing makes a storage check and the write it allows one step.
	publishing sync.Mutex

	broker  bus.Bus
	sinks   *connector.Registry
	deliver *egress.Engine
	// watching is the tenants whose events are observed and delivered.
	watching tenants
	rt       *runtime.Runtime
	// stops is how a run is asked to stop: the runtime, unless a test
	// stands in for it.
	stops stopper
	// asked reads whether the runtime holds a stop request for a run.
	asked  func(ctx context.Context, tenant, runID string) (runtime.StopRecord, bool, error)
	ledger Ledger
	queue  dqueue.Queue

	settings settings

	// surfaces is one per client surface, by address scheme. The map is
	// filled by New and never changes; what is seated on each does.
	surfaces map[string]*surface
	secrets  clients.Secrets
	// clientsMu serializes creating, updating and deleting clients.
	clientsMu sync.Mutex
	// admitting holds runs between admission and their record, by run id.
	admitting         sync.Map
	publicationGuards []ConversationPublicationGuard
	// moves serialize starting a thread's runs with moving the thread.
	moves threadLocks
	// projectForkLocks serialize destination copies independently of the
	// conversation placement locks acquired while capturing archives.
	projectForkLocks threadLocks
	// afterAccept, when set, runs between a run's acceptance and its record.
	// Only tests set it, to stand for a loaded machine.
	afterAccept func(event.Envelope)
	// beforeCarryOut, when set, runs after a message is posted and before
	// what it starts is decided. Only tests set it, to move a thread then.
	beforeCarryOut func()
	// beforeQuestionKept, when set, runs after a question's message is
	// found missing and built, and before it is kept. Only tests set it, to
	// answer in that moment.
	beforeQuestionKept func()
	// halting holds runs being stopped at a limit, by run id, so a run is
	// asked once however many of its calls cross it.
	halting sync.Map
	// holding holds the runs held at a limit for approval, by run id and
	// hold id, so a run is held once per hold however many calls cross it.
	holding sync.Map
	// grants are budget grants being answered, until the ledger has them.
	grants pendingGrants

	// wakers are the timers that wake parked runs at their questions'
	// deadlines, stopped with the platform.
	wakers     sync.Mutex
	wakerSet   map[*time.Timer]struct{}
	wakersDone bool

	// admission serializes letting a run in. It is taken inside calls, never
	// the other way round.
	admission sync.Mutex

	// calls serializes Call, so two calls from one run cannot both pass a
	// fan-out or budget check that only one of them fits under.
	calls sync.Mutex

	// approved notes the calls to agents a person approved, until their run
	// ends, so the call path can tell an approved call from one nobody was
	// asked about.
	approved approvals

	// answers keeps a recorded decision and its surface provenance together
	// before resumed or terminal events can settle the inbox question.
	answers sync.Mutex

	// posts is how fast people post in threads, which starts no run for
	// admission to count.
	posts postRate
}

// New assembles a platform. Nothing runs until Run is called.
func New(cfg Config) (*Platform, error) {
	if cfg.ProjectTemplates == nil {
		cfg.ProjectTemplates = projects.DefaultTemplates()
	}
	if err := projects.ValidateTemplates(cfg.ProjectTemplates); err != nil {
		return nil, err
	}
	if err := validateEmail(cfg.Email); err != nil {
		return nil, err
	}
	if cfg.Email != nil {
		email := *cfg.Email
		email.Recipients = make(map[string]map[string]string, len(cfg.Email.Recipients))
		for tenant, people := range cfg.Email.Recipients {
			email.Recipients[tenant] = maps.Clone(people)
		}
		cfg.Email = &email
	}
	log := cfg.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	if err := cfg.Backends.check(); err != nil {
		return nil, err
	}
	if err := credentials.CheckStarts(cfg.CredentialRefs); err != nil {
		return nil, fmt.Errorf("platform: CredentialRefs: %w", err)
	}
	p := &Platform{
		cfg: cfg, log: log, now: now,
		Agents: cfg.Backends.Agents,
		Blobs:  cfg.Blobs,
		broker: cfg.Bus,
		sinks:  connector.NewRegistry(),
		ledger: cfg.Backends.Ledger,
		queue:  cfg.Backends.Queue,
	}
	if p.Agents == nil {
		p.Agents = wire.NewMemoryAgents(append(providers(), cfg.Ext.ProviderNames()...)...)
	}
	if p.ledger == nil {
		p.ledger, p.queue = ledger.NewMemory(), dqueue.NewMemory()
	}
	dir := cfg.Workspace
	if dir == "" {
		dir = defaultWorkspace()
	}
	files, err := filesblob.NewFS(filepath.Join(dir, "attachments"))
	if err != nil {
		return nil, fmt.Errorf("platform: attachments: %w", err)
	}
	p.files = files
	p.closers = append(p.closers, files.Close)
	// Every store is in memory, reading back what Backends.State holds.
	if err := p.openStores(cfg); err != nil {
		return nil, err
	}
	if cfg.SourceDependencies == nil {
		if err := p.RegisterSourceDependencyReader(outcomeSourceDependencies{p}); err != nil {
			return nil, err
		}
	}

	if err := p.RegisterPublicationGuard(forkPublicationGuard{p}); err != nil {
		return nil, err
	}

	provider := cfg.AssignmentSources
	if provider == nil {
		provider = p
	}
	if err := p.RegisterAssignmentSources(provider); err != nil {
		return nil, err
	}

	if p.broker == nil {
		p.broker = bus.NewMemory()
	}
	if err := p.openRooms(); err != nil {
		return nil, err
	}

	// Schedules are a trigger like any other: the cron connector raises tags
	// and never learns what a runtime is. It reads the store on every tick,
	// so a schedule created through the API takes effect without a restart.
	clock, err := cron.New(cron.Config{Store: p.Schedules, Log: log})
	if err != nil {
		return nil, fmt.Errorf("platform: cron: %w", err)
	}
	if err := p.sinks.Register(clock); err != nil {
		return nil, fmt.Errorf("platform: register cron: %w", err)
	}
	// One surface per scheme, whether or not it has clients yet: clients are
	// created while the platform runs, and the router and the trigger loop
	// are given their connectors once.
	p.surfaces = make(map[string]*surface)
	for _, name := range clients.Surfaces() {
		s := &surface{name: string(name), p: p}
		if err := p.sinks.Register(s); err != nil {
			return nil, fmt.Errorf("platform: register %s: %w", name, err)
		}
		p.surfaces[s.name] = s
	}
	if cfg.Connections != nil {
		connections, err := cfg.Connections(p.Agents)
		if err != nil {
			return nil, fmt.Errorf("platform: connections: %w", err)
		}
		for _, connection := range connections {
			if err := p.static(connection); err != nil {
				return nil, err
			}
		}
	}
	p.restore()

	// Delivery follows the channel: a run's events go to the sink of its
	// trigger's surface, rendered where the trigger came from.
	p.deliver = egress.New(p.broker, p.sinks, egress.WithLogger(log))

	// External tools: "mcp:<provider>[:write]" grants expand, per tenant,
	// into the tools accepted from that provider. Declared providers are
	// recorded before anything can run.
	startup, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	groups, closeGroups, err := p.toolGroups(startup)
	if err != nil {
		return nil, err
	}
	p.closers = append(p.closers, closeGroups)

	if p.cfg.Sandbox.Isolated() {
		p.log.Info("platform: sandbox: each run's tools execute in an instance of their own", "sandbox", p.cfg.Sandbox.String())
	} else {
		p.log.Warn("platform: sandbox: unisolated: tools run on this host with the server's privileges, and the shell is refused. Use it for development and tests only")
	}
	exec, err := p.executor(groups)
	if err != nil {
		_ = p.Close()
		return nil, err
	}
	rt, err := runtime.New(
		anchor.New(p.ledger, p.queue),
		p.Agents.Resolver(),
		p.broker,
		runtime.WithRuntime(exec),
		runtime.WithLogger(log),
		runtime.WithStopGrace(cfg.StopGrace),
		// The ledger is where a stop is recorded, so a request made while a
		// run waits in the queue or between attempts is honoured however long
		// it waits. It is the same ledger the runs execute from.
		runtime.WithStopLedger(p.ledger),
		// A run parked on a person is woken through the queue as soon as
		// it is answered or stopped, not at the worker's next poll.
		runtime.WithWaker(p.queue),
		// A spec's approvers and answerers may name this workspace's roles.
		runtime.WithRoles(p.rolesOf),
		runtime.WithQuestionTTL(cfg.QuestionTTL),
	)
	if err != nil {
		return nil, fmt.Errorf("platform: runtime: %w", err)
	}
	p.rt, p.stops = rt, rt
	p.asked = rt.StopRequested
	if cfg.Stops != nil {
		p.stops = stopFunc(cfg.Stops)
	}
	return p, nil
}

// executor builds the sandbox every turn runs in: a playscape task under the
// NGAC policy compiled from the pinned revision's access grant.
func (p *Platform) executor(groups runtime.ToolGroupSource) (turn.Runtime, error) {
	dir := p.cfg.Workspace
	if dir == "" {
		dir = defaultWorkspace()
	}
	p.dir = dir
	ws, err := workspace.NewLocal(dir)
	if err != nil {
		return nil, fmt.Errorf("platform: workspace %q: %w", dir, err)
	}
	if p.Blobs == nil {
		blobs, err := blob.NewDisk(filepath.Join(dir, "blobs"))
		if err != nil {
			return nil, fmt.Errorf("platform: storage: %w", err)
		}
		p.Blobs = blobs
	}
	tools := tool.NewRegistry()
	plane := controlplane.NewMemory(inproc.NewFactory(tools, ws), metrics.Nop())
	if p.cfg.Runtime != nil {
		inner := sandbox.Sandboxed(p.cfg.Runtime, sandbox.Plane{Control: plane, Tools: tools},
			sandbox.WithReplicas(p.slots()), sandbox.WithLogger(p.log))
		return callExecutor{inner: inner, resolve: p.resolveRevision, bind: p.bindOperationContext, release: p.releaseSourceGrounding}, nil
	}
	exec, err := runtime.NewSandbox(plane, tools, p.Agents.Resolver(),
		runtime.WithCatalog(p.operationsCatalog(p.cfg.Ext.catalog(p.fileTools(p.connectorTools)), groups)),

		runtime.WithDelegator(p.attach),
		// An agent's declared sources ground its turns, searched in the
		// run's tenant.
		runtime.WithKnowledge(p.knowledgeFor),
		runtime.WithCallTool(ToolCallAgent),
		// Each turn claims the instance its tools execute in.
		runtime.WithRunInstances(p.runInstances),
		// A run waiting on an attached run of the same agent holds one of
		// that agent's nodes, so there are as many as there are places.
		runtime.WithReplicas(p.slots()),
		runtime.WithAgentOptions(slices.Concat(p.cfg.Ext.agentOptions(), p.cfg.AgentOptions, []native.Option{native.WithAttachments(resolve.New(p.files))})...),
		runtime.WithSandboxLogger(p.log),
	)
	if err != nil {
		return nil, fmt.Errorf("platform: sandbox: %w", err)
	}
	return callExecutor{inner: exec, resolve: p.resolveRevision, bind: p.bindOperationContext, release: p.releaseSourceGrounding}, nil
}

// Invoke accepts an event for the one agent it names and starts its run. The
// tenant's events are watched before the run is accepted, because accepting
// publishes the run's first event.
func (p *Platform) Invoke(ctx context.Context, tag event.Envelope) (runs.Run, error) {
	if tag.Time.IsZero() {
		tag.Time = p.now()
	}
	agent := wire.AgentOf(tag)
	if agent == "" {
		return runs.Run{}, fmt.Errorf("%w: event %q names no agent", runtime.ErrInvalid, tag.ID)
	}
	wire.SetAgent(&tag, agent)
	// Fail before anything is registered if the tag claims to come from an
	// agent but carries no usable edge.
	edge, err := call.EdgeOf(tag)
	if err != nil {
		return runs.Run{}, err
	}
	if _, err := call.InputOf(tag); err != nil {
		return runs.Run{}, err
	}
	// The run id is the runtime's, derived from the tenant and the tag id
	// together. Two tenants offering the same tag id name two different runs,
	// so a trigger's id, chosen outside, cannot join another tenant's run.
	runID := runtime.RunID(tag.Tenant, tag.ID)

	// Admission and acceptance are one step, so the count a policy is judged
	// on always includes every run already let in.
	p.admission.Lock()
	defer p.admission.Unlock()
	// The same tag again joins the run it already started. It is not a new
	// run, so it is neither judged nor counted a second time.
	if existing, err := p.Runs.Get(tag.Tenant, runID); err == nil {
		return existing, nil
	}
	project, within, client, err := p.placeOf(tag, edge)
	if err != nil {
		return runs.Run{}, err
	}
	tag = p.pin(tag, edge, project, within, p.sourcesFor(tag, edge, project))
	tag, err = p.pinAgentSkillDefaults(ctx, tag, edge, project)
	if err != nil {
		return runs.Run{}, err
	}
	if tag, err = p.pinRunContext(tag, edge, project, within); err != nil {
		return runs.Run{}, err
	}
	if err := p.admit(tag, edge, project, within, client); err != nil {
		return runs.Run{}, err
	}
	if wire.Meta(tag)[MetaPersonalMemoryContext] == "true" || wire.Meta(tag)[MetaPrivateLibraryContext] == "true" {
		if err := p.ContextFlags.Mark(tag.Tenant, runID); err != nil {
			p.appendUsage(usage.Entry{Kind: usage.Ended, Tenant: tag.Tenant, RunID: runID, At: p.now()})
			return runs.Run{}, err
		}
	}
	// Accepting publishes the run's first event before the run is recorded,
	// and delivering that event needs to know which client raised it.
	p.admitting.Store(runID, admitted{tenant: tag.Tenant, client: client, origin: wire.Origin(tag), actor: wire.ActorOf(tag), person: p.operationTagPerson(tag, edge), operationMeta: operationMetaOf(wire.Meta(tag)), place: p.placeAdmitted(tag, edge, runID, project)})
	defer p.admitting.Delete(runID)
	p.watch(tag.Tenant)
	// The run id is RunID(tenant, event id): Humagen raises one event per
	// agent, so the event id already names the subscriber.
	acc, err := p.rt.AcceptOwn(ctx, tag, agent)
	if err != nil {
		// It never started, so it holds no place.
		p.appendUsage(usage.Entry{Kind: usage.Ended, Tenant: tag.Tenant, RunID: runID, At: p.now()})
		return runs.Run{}, err
	}
	if p.afterAccept != nil {
		p.afterAccept(tag)
	}
	if err := p.Runs.Record(acc.RunID, tag, wire.RevOf(acc.Version)); err != nil {
		return runs.Run{}, err
	}
	p.recordContextUse(tag, project)
	// Who asked, by every name they go by now: a mapping removed later does
	// not let them approve their own calls.
	p.Runs.PinRequesters(acc.RunID, p.resolveRequester(tag.Tenant, tag, client))
	return p.Runs.Get(tag.Tenant, acc.RunID)
}

// pin writes into a tag what the platform decides at admission, so every
// attempt of the run replays it: the ancestors its project counts toward and
// the knowledge sources its project's chain gives it, whatever the tag said,
// what the person it acts for is called (namedActor), and for a scheduled or
// run-now run its project's context ahead of the schedule's text, which the
// run's record keeps as what was asked. A called run keeps its explicit
// input.
func (p *Platform) pin(tag event.Envelope, edge call.Edge, project string, within, sources []string) event.Envelope {
	wire.SetActor(&tag, p.namedActor(tag.Tenant, wire.ActorOf(tag)))
	// What the platform pins is written to the request's context; the
	// body's typed fields are the trigger's and stay as they are.
	meta := wire.ContextOf(tag)
	if meta == nil {
		meta = map[string]string{}
	}
	if edge.Parent != "" {

		if parent, err := p.Runs.Get(tag.Tenant, edge.Parent); err == nil {
			for _, key := range []string{MetaPersonalMemoryContext, MetaPrivateLibraryContext} {
				if wire.Meta(parent.Tag())[key] == "true" {
					meta[key] = "true"
				}
			}
		}
	}
	delete(meta, runs.MetaWithin)
	if len(within) > 0 {
		meta[runs.MetaWithin] = strings.Join(within, ",")
	}
	// The old key too: SourcesOf falls back to it when the new one is absent.
	delete(meta, runs.MetaSources)
	delete(meta, runs.MetaSourcesV0)
	if len(sources) > 0 {
		meta[runs.MetaSources] = strings.Join(sources, ",")
	}
	delete(meta, metaForkSources)
	if edge.Parent != "" {
		if root, err := p.Runs.Get(tag.Tenant, runtime.RunID(tag.Tenant, edge.Root)); err == nil {
			if names, ok := wire.Meta(root.Tag())[metaForkSources]; ok {
				meta[metaForkSources] = names
			}
		}
	} else if names := p.forkSourceSelection(tag.Tenant, wire.Meta(tag)[runs.MetaThread]); names != nil {
		meta[metaForkSources] = strings.Join(*names, ",")
	}
	// Only the platform pins skills, on a person's message (answerMessage):
	// any other tag that carries the key is not believed.
	if wire.Origin(tag) != runs.OriginAPI || edge.Parent != "" {
		delete(meta, runs.MetaSkills)
	}
	scheduled := (wire.Origin(tag) == cron.Name || wire.Origin(tag) == runs.OriginManual || wire.Origin(tag) == runs.OriginAssignment) && edge.Parent == ""
	if scheduled {
		meta[runs.MetaAsk] = wire.TextOf(tag)
	}
	wire.SetContext(&tag, meta)
	if scheduled {
		wire.SetText(&tag, p.turnContext(tag.Tenant, project, "", tag.Agent, personOf(wire.ActorOf(tag)), wire.TextOf(tag), wire.Meta(tag)))
	}
	return tag
}

// Call has one run call another agent. It is the only path by which an agent
// starts another agent's run.
//
// Four things are checked, in this order: the caller's pinned spec must name
// the target in its spawn allowlist, the target's call policy must admit the
// caller, the tree's limits must allow the call, and the caller's remaining
// budget must cover the lease.
func (p *Platform) Call(ctx context.Context, tenant, runID string, req call.Request) (runs.Run, error) {
	return p.call(ctx, tenant, runID, req, false, nil)
}

// CallAs is Call as who asks it: an agent who may not read is not among the
// agents the caller's spec is shown to them to call, so it is refused as one
// its spawn allowlist does not name.
func (p *Platform) CallAs(ctx context.Context, who auth.Principal, runID string, req call.Request) (runs.Run, error) {
	return p.call(ctx, who.Tenant, runID, req, false, p.Seer(who).Agent)
}

// call is Call. With grant, a call from a run that holds a lease and asked for
// none is granted one here, under the call lock: half of what the caller has
// left, whether it waits on the call or not. Half, because what the caller
// has left is read from the calls already counted, and the model call that
// asked for this one may not be counted yet: the other half holds room for
// it, and for anything else still in flight. Everything is trued up when the
// call settles. That is how an agent's own calls (tagging, delegate_task)
// lease budget, since a model never names an amount. A caller with nothing
// left is refused. sees, when set, is the agents whoever asks may read (see
// CallAs).
func (p *Platform) call(ctx context.Context, tenant, runID string, req call.Request, grant bool, sees func(string) bool) (runs.Run, error) {
	p.calls.Lock()
	defer p.calls.Unlock()

	parent, err := p.callerOf(tenant, runID)
	if err != nil {
		return runs.Run{}, err
	}
	rev, err := p.Catalog().Get(ctx, tenant, parent.Agent, parent.Rev)
	if err != nil {
		return runs.Run{}, fmt.Errorf("platform: caller's spec: %w", err)
	}
	if !slices.Contains(rev.Spec.Access.Spawn, req.Agent) || sees != nil && !sees(req.Agent) {
		return runs.Run{}, fmt.Errorf("%w: refused by the caller: %q may not call %q, which its spec's spawn allowlist does not name", ErrForbidden, parent.Agent, req.Agent)
	}
	if err := p.checkOperationCall(ctx, parent, req); err != nil {
		return runs.Run{}, err
	}
	// The callee agrees too: its call policy must admit the caller.
	if err := p.admitCall(ctx, tenant, parent.Agent, req.Agent); err != nil {
		return runs.Run{}, err
	}

	input := req.Input
	if input == nil {
		input = &call.Input{Messages: []call.Message{{Role: "user", Content: []call.Part{call.TextPart{Text: req.Text}}}}, ResponseFormat: call.TextFormat{}}
	}
	if req.Input != nil && req.Text != "" {
		return runs.Run{}, fmt.Errorf("%w: use input or text, not both", call.ErrInvalid)
	}
	if strings.TrimSpace(req.Key) != "" {
		if existing, err := p.Runs.Get(tenant, runtime.RunID(tenant, call.TagID(parent.Tag().ID, req.Key))); err == nil {
			compare := req
			if grant && compare.Lease == 0 {
				compare.Lease = existing.Lease
			}
			if !sameCallInput(existing, compare, *input) || !p.sameHandoffTerms(existing, req.Contract) {
				return runs.Run{}, fmt.Errorf("%w: call key already identifies a different call", call.ErrInvalid)
			}
			return existing, nil
		}
	}

	if grant && req.Lease == 0 && parent.Lease > 0 {
		left := parent.Left() / 2
		if left <= 0 {
			return runs.Run{}, fmt.Errorf("%w: %q has spent or handed on all of its budget of %s", call.ErrBudget, parent.Agent, money(parent.Lease))
		}
		req.Lease = left
	}
	lim, err := p.callLimits(parent, req.Agent)
	if err != nil {
		return runs.Run{}, err
	}
	tag, err := call.New(parent.Tag(), req, parent.State(), lim, p.now())
	if err != nil {
		return runs.Run{}, err
	}
	// Policy is checked before liveness, so a refusal always names the rule
	// that refused it. A run that was asked to stop is over as far as new
	// work goes, from the moment it was asked.
	if parent.Status.Terminal() || parent.Stopping {
		if existing, err := p.Runs.Get(tenant, runtime.RunID(tenant, tag.ID)); err == nil {
			return existing, nil
		}
		if parent.Stopping {
			return runs.Run{}, fmt.Errorf("%w: run %q was asked to stop and cannot make calls", call.ErrInvalid, runID)
		}
		return runs.Run{}, fmt.Errorf("%w: run %q has ended and cannot make calls", call.ErrInvalid, runID)
	}
	prepared, err := p.prepareCall(ctx, tenant, *input)
	if err != nil {
		return runs.Run{}, err
	}
	var frozen error
	wire.EditMeta(&tag, func(m map[string]string) { frozen = freezeInput(m, prepared) })
	if err := frozen; err != nil {
		return runs.Run{}, err
	}
	if wire.Meta(parent.Tag())[operations.MetaForceAsk] == "true" {
		wire.SetMeta(&tag, operations.MetaForceAsk, "true")
	}
	if allowed, ok := wire.Meta(parent.Tag())[operations.MetaTools]; ok {
		wire.SetMeta(&tag, operations.MetaTools, allowed)
	}
	wire.SetText(&tag, prepared.Messages[len(prepared.Messages)-1].Text)
	if _, err := p.recordHandoff(parent, req, tag, prepared); err != nil {
		return runs.Run{}, err
	}
	child, err := p.Invoke(ctx, tag)
	if err != nil {
		return runs.Run{}, err
	}
	p.Runs.Called(runID, req.Lease)
	return child, nil
}

// callerOf is the record of a run making a call. A run starts before its
// record is written (Invoke accepts it, then records it, holding the
// admission lock throughout), so a call it makes at once can come before its
// record: one still being admitted is waited for, rather than refused as a
// run that does not exist.
func (p *Platform) callerOf(tenant, runID string) (runs.Run, error) {
	if _, admitting := p.admitting.Load(runID); admitting {
		p.admission.Lock()
		defer p.admission.Unlock()
	}
	return p.Runs.Get(tenant, runID)
}

// reserved reports whether an origin is one only the platform may use.
func reserved(origin string) bool {
	switch origin {
	case call.Origin, runs.OriginAPI, runs.OriginSystem, runs.OriginManual, runs.OriginAssignment, OriginEval:
		return true
	}
	return false
}

// reconcileEvery is how often open runs are checked against the ledger.
const reconcileEvery = 2 * time.Second

// flushEvery is how often what the journal, the run index and the usage
// ledger write behind is written: what a crash can lose.
const flushEvery = 250 * time.Millisecond

// reconcile closes runs the ledger says are over but whose events never said so.
//
// The ledger is the authority on whether a run has ended. The usual way to
// learn it is the run's own completed or terminal-failed event, and this is the
// fallback for when there is none: a run that exhausts its retries is failed in
// the ledger with nothing further published.
func (p *Platform) reconcile(ctx context.Context) {
	p.reconcileOperationReminders(ctx)
	p.settleOperations(ctx)
	p.syncAssignmentSchedules()
	for _, id := range p.Runs.Active() {
		p.settleFromLedger(ctx, id)
	}
}

// settleFromLedger ends a run the ledger says is over, with the outcome the
// runtime recorded, and reports whether the ledger says it is over. A run
// whose final event has not been observed yet ends here, and the event, when
// it arrives, changes nothing: it says the same thing.
func (p *Platform) settleFromLedger(ctx context.Context, id string) bool {
	rec, err := p.ledger.GetRun(ctx, id)
	if err != nil {
		return false
	}
	status := runs.Status("")
	switch rec.Status {
	case drun.StatusSucceeded:
		status = runs.Completed
	case drun.StatusFailed:
		status = runs.Failed
	default:
		return false
	}
	at := p.now()
	if !rec.CompletedAt.IsZero() {
		at = rec.CompletedAt
	}
	answer := ""
	if status == runs.Completed {
		if out, err := p.rt.Result(ctx, id); err == nil {
			answer = out.Text
			// A stopped run succeeds in the ledger: it ended and did not
			// fail. Its output says it was stopped, and what it had said.
			if out.Stopped != nil {
				if run, ended := p.over(func() (runs.Run, bool) { return p.Runs.Stop(id, out.Text, out.Stopped.Reason, at) }); ended {
					p.log.Info("platform: run ended stopped in the ledger before its final event was seen", "run", id)
					p.ended(ctx, run)
				}
				return true
			}
		}
	}
	if run, ended := p.over(func() (runs.Run, bool) { return p.Runs.End(id, status, answer, rec.Error, at) }); ended {
		p.log.Info("platform: run ended in the ledger before its final event was seen", "run", id, "status", status)
		p.ended(ctx, run)
	}
	return true
}

// observe folds one event from the bus into the platform: the journal, the
// run index, and what runs spend. The bus delivers at least once, so it is
// safe to see an event twice: the index ends a run once, and the usage ledger
// counts a call once.
func (p *Platform) observe(ctx context.Context, e event.Envelope) {
	if event.IsControl(e.Channel) || event.Family(e.Type) != "run" {
		// A request addressed to a run, such as a stop, or an event no run
		// published. The run's own events say what came of it.
		return
	}
	p.Journal.Append(e, p.originOf(e.Tenant, e.Run))
	if event.HasPrefix(e.Type, "run.usage") {
		p.observeUsage(ctx, e)
		return
	}
	p.admission.Lock()
	if e.Type == event.TypeCitation {
		metadata, _, _ := p.trustedCitation(e)
		if err := p.Runs.RecordCitation(e, metadata); err != nil {
			p.log.Error("citation could not be retained", "error", err)
		}
	}
	run, ended := p.Runs.Observe(e)
	if ended {
		p.free(run)
	}
	p.admission.Unlock()
	p.observeQuestion(ctx, e)
	if ended {
		p.ended(ctx, run)
	}
}

// raise checks a run that has just ended against its tenant's alert rules.
func (p *Platform) raise(run runs.Run) {
	spoken := false
	raised, err := p.Alerts.Evaluate(run, p.Runs.Recent)
	if err != nil {
		p.log.Error("platform: alerts could not be kept", "run", run.ID, "error", err)
	}
	for _, a := range raised {
		p.log.Warn("platform: alert", "rule", a.Rule, "severity", a.Severity, "title", a.Title, "run", a.RunID)
		p.inboxAlert(run.Tenant, a)
		spoken = spoken || a.RunID == run.ID
	}
	// A failure no rule was watching for still needs someone.
	if !spoken {
		p.inboxFailure(run)
	}
}

// RunTypes are the type prefixes the platform observes: every run event,
// the opt-in usage and compaction families among them.
var RunTypes = []string{"run", "run.usage", "run.context"}

// channels are the channel roots every run publishes under: one per
// registered connector's scheme, and the channel of a run Humagen raises
// itself.
func (p *Platform) channels() []string {
	out := []string{wire.ChannelScheme + "://"}
	for _, name := range p.sinks.Names() {
		out = append(out, name+"://")
	}
	return out
}

// RunSubscription is what a reader of a tenant's runs subscribes to: the run
// events of every channel in tenant, narrowed to one run when run is set,
// resuming after from.
func (p *Platform) RunSubscription(tenant, run string, from uint64) bus.Subscription {
	return bus.Subscription{Tenant: tenant, Channels: p.channels(), Types: slices.Clone(RunTypes), Run: run, From: from}
}

// EventFilter narrows a reader's stream of run events. The zero filter is
// every run event in the tenant, the opt-in families included.
type EventFilter struct {
	// Channel is a channel prefix. Empty is every channel a run publishes on.
	Channel string
	// Types are type prefixes in the run family. Empty is RunTypes.
	Types []string
	// Run is one run's events.
	Run string
	// Agent is one agent's events. Only an old topic names one.
	Agent string
	// From resumes after this sequence within each run.
	From uint64
}

// Subscribe opens a stream of a tenant's run events, narrowed by f. The
// tenant is the caller's scope, never part of what a request names. A type
// outside the run family, or a control channel, is refused: those are the
// runtime's traffic and ingress, which no reader is owed.
func (p *Platform) Subscribe(ctx context.Context, tenant string, f EventFilter) (*Stream, error) {
	if tenant == "" {
		return nil, fmt.Errorf("%w: no tenant", bus.ErrInvalid)
	}
	sub := p.RunSubscription(tenant, f.Run, f.From)
	if f.Channel != "" {
		if event.IsControl(f.Channel) {
			return nil, fmt.Errorf("%w: channel %q is not a run channel", bus.ErrInvalid, f.Channel)
		}
		sub.Channels = []string{f.Channel}
	}
	if len(f.Types) > 0 {
		for _, t := range f.Types {
			if !event.HasPrefix(t, "run") {
				return nil, fmt.Errorf("%w: type %q is not a run type", bus.ErrInvalid, t)
			}
		}
		sub.Types = slices.Clone(f.Types)
	}
	s, err := p.broker.Subscribe(ctx, sub)
	if err != nil {
		return nil, err
	}
	return &Stream{inner: s, tenant: tenant, agent: f.Agent}, nil
}

// Stream is one tenant's view of the bus.
type Stream struct {
	inner  bus.Stream
	tenant string
	agent  string
}

// Recv returns the next event in the stream's tenant.
func (s *Stream) Recv(ctx context.Context) (event.Envelope, error) {
	for {
		e, err := s.inner.Recv(ctx)
		if err != nil {
			return event.Envelope{}, err
		}
		// Requests addressed to runs are the runtime's traffic, not events
		// a reader of runs is owed.
		if e.Tenant == s.tenant && !event.IsControl(e.Channel) && (s.agent == "" || e.Agent == s.agent) {
			return e, nil
		}
	}
}

// originOf is the origin of a run: the path its trigger came through.
func (p *Platform) originOf(tenant, runID string) string {
	if a, ok := p.admitting.Load(runID); ok {
		if a := a.(admitted); a.tenant == tenant {
			return a.origin
		}
	}
	if run, err := p.Runs.Get(tenant, runID); err == nil {
		return run.Origin
	}
	return ""
}

// Close ends the stream.
func (s *Stream) Close() error { return s.inner.Close() }

// Run starts every long-lived loop and blocks until ctx is done or one fails.
func (p *Platform) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer p.stopWakers()
	p.Knowledge.UseEmbedder(ctx, p.cfg.Embedder, p.log)

	var wg sync.WaitGroup
	errc := make(chan error, 8)
	loop := func(name string, fn func() error) {
		wg.Go(func() {
			if err := fn(); err != nil && !errors.Is(err, context.Canceled) {
				errc <- fmt.Errorf("platform: %s: %w", name, err)
				cancel()
			}
		})
	}

	// Each tenant's events are observed and delivered on their own: the bus
	// has no subscription across tenants. A tenant is watched from the first
	// run accepted for it, and at start from every run that had not ended.
	// Its observer subscribes before the watch returns, so the journal and
	// the index never miss a run's first event.
	for _, run := range p.Runs.Unfinished() {
		p.watch(run.Tenant)
	}
	p.watching.begin(func(tenant string) {
		sub := p.RunSubscription(tenant, "", 0)
		observed, err := p.broker.Subscribe(ctx, sub)
		if err != nil {
			if ctx.Err() == nil {
				select {
				case errc <- fmt.Errorf("platform: observe %s: %w", tenant, err):
				default:
				}
				cancel()
			}
			return
		}
		loop("observer "+tenant, func() error { return p.observeTenant(ctx, sub, observed) })
		loop("egress "+tenant, func() error { return p.deliver.Run(ctx, tenant) })
	})
	loop("state flush", func() error {
		tick := time.NewTicker(flushEvery)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return nil
			case <-tick.C:
				p.flush()
			}
		}
	})
	loop("email notifications", func() error { return p.runEmailNotifications(ctx) })
	loop("connector flush", func() error { return p.flushConnections(ctx) })
	loop("source refresh", func() error { return p.refreshSources(ctx) })
	loop("inbox retention", func() error {
		tick := time.NewTicker(retainEvery)
		defer tick.Stop()
		for {
			p.retainInbox(ctx)
			select {
			case <-ctx.Done():
				return nil
			case <-tick.C:
			}
		}
	})
	loop("sandbox sweep", func() error {
		tick := time.NewTicker(time.Hour)
		defer tick.Stop()
		for {
			p.sweepSandboxes()
			select {
			case <-ctx.Done():
				return nil
			case <-tick.C:
			}
		}
	})
	loop("worker", func() error {
		opts := []worker.Option{
			worker.WithRegistry(p.rt.Registry()),
			worker.WithConcurrency(p.slots()),
			worker.WithLogger(p.log),
		}
		if p.cfg.Backoff != nil {
			opts = append(opts, worker.WithBackoff(p.cfg.Backoff))
		}
		return worker.New(p.ledger, p.queue, opts...).Run(ctx)
	})
	loop("reconciler", func() error {
		tick := time.NewTicker(reconcileEvery)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return nil
			case <-tick.C:
				p.reconcile(ctx)
			}
		}
	})

	// Ingress adapters publish into one queue, read in order by one loop.
	tags := make(chan event.Envelope)
	publish := func(ctx context.Context, e event.Envelope) error {
		select {
		case tags <- e:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	for _, t := range p.sinks.Triggers() {
		loop("trigger "+t.Name(), func() error { return t.Ingest(ctx, publish) })
	}
	loop("tags", func() error {
		for {
			select {
			case <-ctx.Done():
				return nil
			case tag := <-tags:
				p.ingest(ctx, tag)
			}
		}
	})

	<-ctx.Done()
	// No tenant starts being watched once the loops are winding down.
	p.watching.end()
	wg.Wait()
	// What changed last is written once every loop has stopped, so nothing
	// the observer folded in while the platform stopped is left unwritten.
	p.flush()
	close(errc)
	return <-errc
}

// ingest accepts an event an ingress adapter published.
func (p *Platform) ingest(ctx context.Context, tag event.Envelope) {
	// A trigger's event cannot claim an origin the platform keeps for
	// itself. Lineage is only ever minted by Call, and the rest carry keys
	// the index believes.
	if reserved(wire.Origin(tag)) {
		p.log.Warn("platform: dropped a trigger event claiming a reserved origin", "event", tag.ID, "origin", wire.Origin(tag))
		return
	}
	// Who a run acts for is named only by a surface's stamp, from its
	// clients' people maps. Any other trigger's run, a schedule's, acts for
	// nobody.
	if _, stamped := p.surfaces[wire.Origin(tag)]; !stamped {
		actor := wire.ActorOf(tag)
		actor.User = ""
		wire.SetActor(&tag, actor)
	}
	// A tick names no agent: the schedule it fires does.
	if tick, ok := tag.Data.(event.CronTick); ok && wire.AgentOf(tag) == "" {
		if sc, found, err := p.Schedules.Get(tag.Tenant, tick.Schedule); err == nil && found {
			wire.SetAgent(&tag, sc.Agent)
		}
	}
	if r, err := p.Invoke(ctx, tag); err != nil {
		p.log.Error("platform: accept event", "event", tag.ID, "type", tag.Type, "agent", wire.AgentOf(tag), "error", err)
	} else {
		p.log.Info("platform: accepted", "run", r.ID, "agent", r.Agent, "origin", r.Origin)
	}
}

// observeTenant folds one tenant's run events into the platform until ctx is
// done. A subscriber the bus cut off for falling behind rejoins: the journal
// has a gap, which is logged, and the platform keeps running.
func (p *Platform) observeTenant(ctx context.Context, sub bus.Subscription, observed bus.Stream) error {
	defer func() { _ = observed.Close() }()
	for {
		e, err := observed.Recv(ctx)
		switch {
		case ctx.Err() != nil:
			return nil
		case errors.Is(err, bus.ErrSlowConsumer):
			p.log.Warn("platform: observer fell behind and missed events", "tenant", sub.Tenant)
			_ = observed.Close()
			if observed, err = p.broker.Subscribe(ctx, sub); err != nil {
				return err
			}
			continue
		case errors.Is(err, bus.ErrClosed):
			return nil
		case err != nil:
			return err
		}
		p.observe(ctx, e)
	}
}

// tenants is the set of tenants whose events the platform watches.
type tenants struct {
	mu      sync.Mutex
	seen    map[string]bool
	pending []string
	start   func(tenant string)
	ended   bool
}

// watch has the platform observe and deliver a tenant's events, once. Before
// Run it is remembered and started when Run begins.
func (p *Platform) watch(tenant string) {
	w := &p.watching
	w.mu.Lock()
	defer w.mu.Unlock()
	if tenant == "" || w.seen[tenant] || w.ended {
		return
	}
	if w.seen == nil {
		w.seen = map[string]bool{}
	}
	w.seen[tenant] = true
	if w.start == nil {
		w.pending = append(w.pending, tenant)
		return
	}
	w.start(tenant)
}

// begin starts every tenant remembered so far, and every one after.
func (w *tenants) begin(start func(tenant string)) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.start = start
	for _, t := range w.pending {
		start(t)
	}
	w.pending = nil
}

// end stops starting tenants.
func (w *tenants) end() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.start, w.ended = nil, true
}

// defaultWorkspace is where agents write their files when nothing is set:
// under the user cache directory, at the path stored records point at.
func defaultWorkspace() string {
	base, err := os.UserCacheDir()
	if err != nil {
		base = os.TempDir()
	}
	return legacy.WorkspaceDir(base)
}
