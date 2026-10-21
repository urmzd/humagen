package platform_test

import (
	"context"
	"testing"
	"time"

	"github.com/urmzd/anchor/pkg/ledger"
	dqueue "github.com/urmzd/anchor/pkg/queue"
	"github.com/urmzd/fiber/pkg/agentspec"
	"github.com/urmzd/fiber/pkg/connectors/cron"
	"github.com/urmzd/fiber/pkg/registry"

	"github.com/urmzd/warp/internal/platform"
	"github.com/urmzd/warp/pkg/alerts"
	"github.com/urmzd/warp/pkg/clients"
	"github.com/urmzd/warp/pkg/inbox"
	"github.com/urmzd/warp/pkg/projects"
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
	p, err := platform.New(platform.Config{Backends: b, Workspace: t.TempDir()})
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
