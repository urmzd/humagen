package platform_test

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/urmzd/anchor/pkg/ledger"
	dqueue "github.com/urmzd/anchor/pkg/queue"
	"github.com/urmzd/fiber/pkg/address"
	"github.com/urmzd/fiber/pkg/agentrt"
	"github.com/urmzd/fiber/pkg/agentrt/payload"
	"github.com/urmzd/fiber/pkg/agentspec"
	"github.com/urmzd/fiber/pkg/connector"
	"github.com/urmzd/fiber/pkg/connectors/cron"
	"github.com/urmzd/fiber/pkg/envelope"
	"github.com/urmzd/fiber/pkg/registry"
	"github.com/urmzd/saige/agent/agenttest"
	saigetypes "github.com/urmzd/saige/agent/types"

	"github.com/urmzd/warp/internal/platform"
	"github.com/urmzd/warp/pkg/alerts"
	"github.com/urmzd/warp/pkg/clients"
	"github.com/urmzd/warp/pkg/inbox"
	"github.com/urmzd/warp/pkg/runs"
	"github.com/urmzd/warp/pkg/state"
	"github.com/urmzd/warp/pkg/state/statetest"
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
				State: state.NewMemory(), Agents: registry.NewMemory(),
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
		for _, spec := range []agentspec.AgentSpec{
			{Name: "lead", Model: "offline", Provider: "offline", SystemPrompt: "Lead.", Access: agentspec.Access{Spawn: []string{"helper"}}},
			{Name: "helper", Model: "offline", Provider: "offline", SystemPrompt: "Help."},
		} {
			if _, err := p.Agents.Create(ctx, tenant, spec, "test"); err != nil {
				t.Fatal(err)
			}
		}
		cfg := p.Settings(tenant)
		cfg.Baseline, cfg.WorkspaceName = "lead", "Acme Ops"
		cfg.Projects = []platform.Project{{Name: "Launch"}}
		if err := p.SetSettings(ctx, tenant, cfg); err != nil {
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
		r, err := p.Invoke(ctx, tag("run-1", "lead"))
		if err != nil {
			t.Fatal(err)
		}
		done := await(t, p, r.ID, runs.Completed)
		if _, _, err := p.Inbox.Raise(tenant, "note", inbox.Message{Kind: inbox.KindResult, Subject: "Kept", Parts: []inbox.Part{{Type: inbox.PartText, State: inbox.PartInfo, Text: "hi"}}}, time.Now()); err != nil {
			t.Fatal(err)
		}
		stop()

		q, _ := running(t, again())
		if _, err := q.Agents.Get(ctx, tenant, "lead", 0); err != nil {
			t.Fatalf("the agent after restart: %v", err)
		}
		s := q.Settings(tenant)
		if s.Baseline != "lead" || s.WorkspaceName != "Acme Ops" || len(s.Projects) != 1 || s.SetupBy != "ada" || s.SetupAt.IsZero() {
			t.Fatalf("settings after restart = %+v", s)
		}
		if c, err := q.Clients.Get(tenant, "docs"); err != nil || !c.Enabled || !c.Credentials.Token.Set {
			t.Fatalf("the client after restart = %+v, %v", c, err)
		}
		if rules := q.Alerts.Rules(tenant); len(rules) != 1 || rules[0].Name != "fails" {
			t.Fatalf("alert rules after restart = %+v", rules)
		}
		if sc, paused, err := q.Schedules.Get(tenant, "nightly"); err != nil || !paused || sc.Expression != "0 3 * * *" || q.Schedules.About("nightly").Description != "Every night" {
			t.Fatalf("the schedule after restart = %+v, %v, %v", sc, paused, err)
		}
		got, err := q.Runs.Get(tenant, r.ID)
		if err != nil || got.Status != runs.Completed || got.Answer != done.Answer {
			t.Fatalf("the run after restart = %+v, %v", got, err)
		}
		if trace := q.Journal.Events(tenant, []string{r.ID}); len(trace) == 0 {
			t.Fatal("the run's events were not kept")
		}
		if m, ok := q.Inbox.ByKey(tenant, "note"); !ok || m.Subject != "Kept" {
			t.Fatalf("the inbox after restart = %+v, %v", m, ok)
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
	e  envelope.Event
}

func (f *surfaceFake) Name() string { return string(clients.GitHub) }

func (f *surfaceFake) Ingest(ctx context.Context, _ chan<- envelope.Tag) error {
	<-ctx.Done()
	return nil
}

func (f *surfaceFake) Deliver(_ context.Context, to address.Address, e envelope.Event) error {
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
		if d.e.RunID != runID || d.e.Kind != envelope.KindCompleted {
			continue
		}
		if l, err := payload.Decode[payload.Lifecycle](d.e); err == nil {
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
		model := func(m saigetypes.Provider) []agentrt.Option {
			return []agentrt.Option{agentrt.WithProvider(agentrt.ProviderOffline, func(agentrt.Spec) (saigetypes.Provider, error) { return m, nil })}
		}
		connected := func(peer platform.Peer) func(registry.Store) ([]platform.Connection, error) {
			return func(registry.Store) ([]platform.Connection, error) {
				return []platform.Connection{{Tenant: tenant, Peer: peer}}, nil
			}
		}

		held := &holding{called: make(chan struct{})}
		before := &surfaceFake{}
		p, stop := runningWith(t, platform.Config{Backends: first, AgentOptions: model(held), Connections: connected(before)})
		if _, err := p.Agents.Create(ctx, tenant, agentspec.AgentSpec{Name: "lead", Model: "offline", Provider: "offline", SystemPrompt: "Lead."}, "test"); err != nil {
			t.Fatal(err)
		}
		issue := address.MustParse("github://acme/repo/issues/7")
		asked := tag("in-flight", "lead")
		asked.Source = issue
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
