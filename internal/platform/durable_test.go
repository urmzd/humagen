package platform_test

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/urmzd/anchor/pkg/ledger"
	dqueue "github.com/urmzd/anchor/pkg/queue"
	"github.com/urmzd/humagen/fiber/pkg/address"
	"github.com/urmzd/humagen/fiber/pkg/connector"
	"github.com/urmzd/humagen/fiber/pkg/connectors/cron"
	"github.com/urmzd/humagen/fiber/pkg/event"
	"github.com/urmzd/humagen/fiber/pkg/turn/native"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/saige/agent/agenttest"
	saigetypes "github.com/urmzd/saige/agent/types"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/alerts"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/credentials"
	"github.com/urmzd/humagen/pkg/inbox"
	"github.com/urmzd/humagen/pkg/personctx"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/settings"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

// backends gives a test the same backends twice, as a process restarted on
// the same database would have them: once for the platform before the
// restart, and once after the first has stopped.
type backends struct {
	name string
	open func(t *testing.T) (first platform.Backends, again func() platform.Backends)
}

func eachBackend(t *testing.T, fn func(t *testing.T, first platform.Backends, again func() platform.Backends)) {
	for _, b := range []backends{
		{name: "memory", open: func(*testing.T) (platform.Backends, func() platform.Backends) {
			// Memory stands in for a database by being shared.
			b := platform.Backends{
				State: state.NewMemory(), Agents: wire.NewMemoryAgents(),
				Ledger: ledger.NewMemory(), Queue: dqueue.NewMemory(),
			}
			return b, func() platform.Backends { return b }
		}},
		{name: "postgres", open: func(t *testing.T) (platform.Backends, func() platform.Backends) {
			pool := statetest.Pool(t)
			b, release, err := platform.OpenPostgres(t.Context(), pool)
			if err != nil {
				t.Fatal(err)
			}
			// Released here when the test does not restart. Releasing
			// twice is harmless.
			t.Cleanup(func() { _ = release() })
			return b, func() platform.Backends {
				if err := release(); err != nil {
					t.Fatal(err)
				}
				again, release, err := platform.OpenPostgres(t.Context(), pool)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = release() })
				return again
			}
		}},
	} {
		t.Run(b.name, func(t *testing.T) {
			first, again := b.open(t)
			fn(t, first, again)
		})
	}
}

// running starts a platform and returns it with a function that stops it and
// waits until it has.
func running(t *testing.T, b platform.Backends) (*platform.Platform, func()) {
	t.Helper()
	return runningWith(t, platform.Config{Backends: b})
}

// runningWith is running with more of the configuration given.
func runningWith(t *testing.T, cfg platform.Config) (*platform.Platform, func()) {
	t.Helper()
	if cfg.Workspace == "" {
		cfg.Workspace = t.TempDir()
	}
	p, err := platform.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- p.Run(ctx) }()
	stopped := false
	stop := func() {
		if stopped {
			return
		}
		stopped = true
		cancel()
		if err := <-done; err != nil {
			t.Errorf("platform stopped with %v", err)
		}
	}
	t.Cleanup(stop)
	return p, stop
}

func TestThePlatformSurvivesARestart(t *testing.T) {
	eachBackend(t, func(t *testing.T, first platform.Backends, again func() platform.Backends) {
		ctx := t.Context()
		p, stop := running(t, first)
		for _, spec := range []wire.AgentSpec{
			{Name: "lead", Model: "offline", Provider: "offline", SystemPrompt: "Lead.", Access: wire.Access{Spawn: []string{"helper"}}},
			{Name: "helper", Model: "offline", Provider: "offline", SystemPrompt: "Help."},
		} {
			if _, err := p.Agents.Create(ctx, tenant, spec, "test"); err != nil {
				t.Fatal(err)
			}
		}
		cfg := p.Settings(tenant)
		cfg.WorkspaceName = "Acme Ops"
		if err := p.SetSettings(ctx, tenant, cfg); err != nil {
			t.Fatal(err)
		}
		if _, err := p.CreateProject(ctx, tenant, projects.Writer{By: "ada"}, platform.ProjectInput{Name: "Launch"}); err != nil {
			t.Fatal(err)
		}
		if _, err := p.SetProjectAgents(ctx, tenant, "~", projects.Writer{By: "ada", Govern: true}, platform.AgentsInput{
			Version: p.ProjectTree(tenant).Root().Version, Enable: []string{"*"}, Default: "lead",
		}); err != nil {
			t.Fatal(err)
		}
		if err := p.SetAgentHome(ctx, tenant, "helper", "Launch", projects.Writer{By: "ada"}); err != nil {
			t.Fatal(err)
		}
		if _, err := p.MarkSetup(tenant, "ada"); err != nil {
			t.Fatal(err)
		}
		if _, err := p.CreateClient(ctx, tenant, "ada", platform.ClientInput{
			Client: clients.Client{
				ID: "docs", Name: "docs", Surface: clients.Slack, Enabled: true,
				Identity: clients.Identity{AppID: "A0DOCS", Team: "T01ACME"}, DefaultAgent: "lead",
			},
			Token: clients.SecretInput{Value: "xoxb-docs"}, SigningSecret: clients.SecretInput{Value: "sig-docs"},
		}); err != nil {
			t.Fatal(err)
		}
		if err := p.Alerts.AddRule(tenant, alerts.Rule{Name: "fails", Kind: alerts.RunFailed, Severity: alerts.Critical, CreatedBy: "ada"}); err != nil {
			t.Fatal(err)
		}
		if err := p.Schedules.Add(cron.Schedule{Name: "nightly", Expression: "0 3 * * *", Agent: "lead", Text: "summarize", Tenant: tenant}, platform.About{Description: "Every night"}); err != nil {
			t.Fatal(err)
		}
		if err := p.Schedules.SetPaused(tenant, "nightly", true); err != nil {
			t.Fatal(err)
		}
		// Who wrote what it runs is kept: ben rewrote it, and ada saving it
		// again wrote nothing.
		for _, by := range []string{"ben", "ada"} {
			if err := p.Schedules.Replace(cron.Schedule{Name: "nightly", Expression: "0 3 * * *", Agent: "lead", Text: "summarize the night", Tenant: tenant}, platform.About{Description: "Every night", UpdatedBy: by}); err != nil {
				t.Fatal(err)
			}
		}
		r, err := p.Invoke(ctx, tag("run-1", "lead"))
		if err != nil {
			t.Fatal(err)
		}
		done := await(t, p, r.ID, runs.Completed)
		if _, _, err := p.Inbox.Raise(tenant, "note", inbox.Message{Kind: inbox.KindResult, Subject: "Kept", Parts: []inbox.Part{{Type: inbox.PartText, State: inbox.PartInfo, Text: "hi"}}}, time.Now()); err != nil {
			t.Fatal(err)
		}
		// A person's own tool credentials: a reference, and a value kept
		// beside the clients' secrets.
		if _, err := p.Credentials.Put(tenant, "ada", "issues", credentials.Input{Ref: "op://People/ada/issues"}, time.Now()); err != nil {
			t.Fatal(err)
		}
		if _, err := p.Credentials.Put(tenant, "ada", "drive", credentials.Input{Value: "tok-ada-drive"}, time.Now()); err != nil {
			t.Fatal(err)
		}
		// A person's own context, and a run admitted with it.
		mine, err := p.PersonContexts.Set(tenant, "ada", personctx.Input{Run: settings.Layer{Preferences: settings.Preferences{CompactionTokens: 4096}}}, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		stop()

		q, _ := running(t, again())
		if _, err := q.Agents.Get(ctx, tenant, "lead", 0); err != nil {
			t.Fatalf("the agent after restart: %v", err)
		}
		s := q.Settings(tenant)
		if s.WorkspaceName != "Acme Ops" || s.SetupBy != "ada" || s.SetupAt.IsZero() {
			t.Fatalf("settings after restart = %+v", s)
		}
		if d, _ := q.ProjectTree(tenant).DefaultOf(projects.Root); d != "lead" {
			t.Fatalf("the workspace default after restart = %q, want lead", d)
		}
		if home, _ := q.ProjectTree(tenant).Home("helper"); home != "Launch" {
			t.Fatalf("helper lives in %q after restart, want Launch", home)
		}
		if l := q.Projects.List(tenant); len(l) != 1 || l[0].Name != "Launch" || l[0].CreatedBy != "ada" {
			t.Fatalf("projects after restart = %+v", l)
		}
		if c, err := q.Clients.Get(tenant, "docs"); err != nil || !c.Enabled || !c.Credentials.Token.Set {
			t.Fatalf("the client after restart = %+v, %v", c, err)
		}
		if rules := kept(q.Alerts.Rules(tenant)); len(rules) != 1 || rules[0].Name != "fails" {
			t.Fatalf("alert rules after restart = %+v", rules)
		}
		if sc, paused, err := q.Schedules.Get(tenant, "nightly"); err != nil || !paused || sc.Expression != "0 3 * * *" || func() string { a, _ := q.Schedules.About(tenant, "nightly"); return a.Description }() != "Every night" {
			t.Fatalf("the schedule after restart = %+v, %v, %v", sc, paused, err)
		}
		if _, author, err := q.Schedules.Authored(tenant, "nightly"); err != nil || author != "ben" {
			t.Fatalf("the schedule's author after restart = %q, %v, want ben", author, err)
		}
		got, err := q.Runs.Get(tenant, r.ID)
		if err != nil || got.Status != runs.Completed || got.Answer != done.Answer {
			t.Fatalf("the run after restart = %+v, %v", got, err)
		}
		if trace := kept(q.Journal.Events(tenant, []string{r.ID})); len(trace) == 0 {
			t.Fatal("the run's events were not kept")
		}
		if m, ok := q.Inbox.ByKey(tenant, "note"); !ok || m.Subject != "Kept" {
			t.Fatalf("the inbox after restart = %+v, %v", m, ok)
		}
		if c, err := q.PersonContexts.Get(tenant, "ada"); err != nil || c.Version != 1 || c.Memory != mine.Memory || c.Run.Preferences.CompactionTokens != 4096 {
			t.Fatalf("a person's context after restart = %+v", c)
		}
		if c, ok, err := q.RunContext(tenant, done.ID); !ok || err != nil || c.Person != "" {
			t.Fatalf("a run's pinned context after restart = %+v, %v, %v", c, ok, err)
		}
		if c, ok, _ := q.Credentials.Get(tenant, "ada", "issues"); !ok || c.Ref != "op://People/ada/issues" {
			t.Fatalf("a person's reference after restart = %+v, %v", c, ok)
		}
		if v, ok, _ := q.Credentials.Value(tenant, "ada", "drive"); !ok || v != "tok-ada-drive" {
			t.Fatalf("a person's stored value after restart = %q, %v", v, ok)
		}
		// A run accepted after the restart is not mistaken for one before.
		r2, err := q.Invoke(ctx, tag("run-2", "lead"))
		if err != nil {
			t.Fatal(err)
		}
		await(t, q, r2.ID, runs.Completed)
		if l := q.Runs.List(tenant, 10); len(l) != 2 || l[0].ID != r2.ID {
			t.Fatalf("runs after restart = %+v", l)
		}
	})
}

// surfaceFake is a GitHub connection that keeps what it is given to deliver,
// as the issue a run was raised on would show it.
type surfaceFake struct {
	mu   sync.Mutex
	sent []delivered
}

type delivered struct {
	to address.Address
	e  event.Envelope
}

func (f *surfaceFake) Name() string { return string(clients.GitHub) }

func (f *surfaceFake) Ingest(ctx context.Context, _ connector.Publish) error {
	<-ctx.Done()
	return nil
}

func (f *surfaceFake) Deliver(_ context.Context, to address.Address, e event.Envelope) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, delivered{to: to, e: e})
	return nil
}

func (f *surfaceFake) Actions() []connector.Action { return nil }

func (f *surfaceFake) ServeHTTP(w http.ResponseWriter, _ *http.Request) { http.NotFound(w, nil) }

func (f *surfaceFake) Ingesting() bool { return true }

func (f *surfaceFake) Flush(context.Context) error { return nil }

// answer is the final answer delivered for a run, and where it went.
func (f *surfaceFake) answer(runID string) (string, address.Address, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, d := range f.sent {
		if d.e.Run != runID || d.e.Type != event.TypeCompleted {
			continue
		}
		if l, err := wire.LifecycleOf(d.e); err == nil {
			return l.Text, d.to, true
		}
	}
	return "", address.Address{}, false
}

// holding is a model that holds its call open, saying nothing, until it is
// cancelled. It closes called when it is first called.
type holding struct {
	called chan struct{}
	once   sync.Once
}

func (h *holding) Stream(ctx context.Context, _ saigetypes.Request) (<-chan saigetypes.Delta, error) {
	h.once.Do(func() { close(h.called) })
	out := make(chan saigetypes.Delta)
	go func() {
		defer close(out)
		<-ctx.Done()
	}()
	return out, nil
}

// A run still working when its platform stops is resumed from the ledger by
// the next one, and its answer reaches the surface its tag routed it to. The
// router keeps routes only in memory, so the platform that resumes the run has
// to register them again from the run index.
func TestARunInFlightAcrossARestartAnswersOnItsRoute(t *testing.T) {
	eachBackend(t, func(t *testing.T, first platform.Backends, again func() platform.Backends) {
		ctx := t.Context()
		model := func(m saigetypes.Provider) []native.Option {
			return []native.Option{native.WithProvider(native.ProviderOffline, func(native.Spec) (saigetypes.Provider, error) { return m, nil })}
		}
		connected := func(peer platform.Peer) func(wire.AgentStore) ([]platform.Connection, error) {
			return func(wire.AgentStore) ([]platform.Connection, error) {
				return []platform.Connection{{Tenant: tenant, Peer: peer}}, nil
			}
		}

		held := &holding{called: make(chan struct{})}
		before := &surfaceFake{}
		p, stop := runningWith(t, platform.Config{Backends: first, AgentOptions: model(held), Connections: connected(before)})
		if _, err := p.Agents.Create(ctx, tenant, wire.AgentSpec{Name: "lead", Model: "offline", Provider: "offline", SystemPrompt: "Lead."}, "test"); err != nil {
			t.Fatal(err)
		}
		issue := address.MustParse("github://acme/repo/issues/7")
		asked := tag("in-flight", "lead")
		asked.Channel = issue.String()
		r, err := p.Invoke(ctx, asked)
		if err != nil {
			t.Fatal(err)
		}
		select {
		case <-held.called:
		case <-time.After(10 * time.Second):
			t.Fatal("the run never called its model")
		}
		stop()
		if _, _, ok := before.answer(r.ID); ok {
			t.Fatal("the run answered before the restart")
		}

		after := &surfaceFake{}
		const answer = "Answered after the restart."
		q, _ := runningWith(t, platform.Config{
			Backends:     again(),
			AgentOptions: model(&agenttest.ScriptedProvider{Responses: [][]saigetypes.Delta{say(answer)}}),
			Connections:  connected(after),
		})
		await(t, q, r.ID, runs.Completed)
		eventually(t, "the answer reached the issue", func() bool {
			_, _, ok := after.answer(r.ID)
			return ok
		})
		if got, to, _ := after.answer(r.ID); got != answer || to.String() != issue.String() {
			t.Fatalf("delivered %q to %s, want %q to %s", got, to, answer, issue)
		}
	})
}
