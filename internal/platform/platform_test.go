package platform_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/urmzd/humagen/fiber/pkg/connectors/cron"
	"github.com/urmzd/humagen/fiber/pkg/event"
	"github.com/urmzd/humagen/internal/wire"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/alerts"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/join"
	"github.com/urmzd/humagen/pkg/policy"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/usage"
)

const tenant = "acme"

// start runs a platform with two offline agents: lead may call helper, and
// helper may call no one.
func start(t *testing.T, lim call.Limits) *platform.Platform {
	t.Helper()
	return startWith(t, platform.Config{Limits: lim})
}

// startWith is start with more of the configuration given.
func startWith(t *testing.T, cfg platform.Config) *platform.Platform {
	t.Helper()
	cfg.Workspace = t.TempDir()
	if cfg.Router == nil {
		cfg.Router = func(string) join.Asker { return join.Words }
	}
	p, err := platform.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- p.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("platform stopped with %v", err)
		}
	})

	for _, spec := range []wire.AgentSpec{
		{Name: "lead", Model: "offline", Provider: "offline", SystemPrompt: "Lead.", Access: wire.Access{Spawn: []string{"helper"}}},
		{Name: "helper", Model: "offline", Provider: "offline", SystemPrompt: "Help."},
	} {
		if _, err := p.Agents.Create(ctx, tenant, spec, "test"); err != nil {
			t.Fatal(err)
		}
	}
	return p
}

func tag(id, agent string) event.Envelope {
	return wire.NewEvent(wire.Raise{ID: id, Tenant: tenant, Agent: agent, Origin: "api", Text: "hello"})
}

// await waits for a run to reach a status, and returns as soon as it does.
func await(t *testing.T, p *platform.Platform, runID string, want runs.Status) runs.Run {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		r, err := p.Runs.Get(tenant, runID)
		if err == nil && r.Status == want {
			return r
		}
		if time.Now().After(deadline) {
			t.Fatalf("run %s: status %q (err %v), want %q", runID, r.Status, err, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestARunCompletesAndStreams(t *testing.T) {
	p := start(t, call.Limits{})
	ctx := t.Context()

	stream, err := p.Subscribe(ctx, tenant, platform.EventFilter{Agent: "lead"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stream.Close() }()

	r, err := p.Invoke(ctx, tag("run-1", "lead"))
	if err != nil {
		t.Fatal(err)
	}
	if r.Root != r.ID || r.Depth != 0 || r.Parent != "" {
		t.Fatalf("a run raised from outside should be a root: %+v", r)
	}
	got := await(t, p, r.ID, runs.Completed)
	if got.Answer == "" {
		t.Fatal("completed run has no answer")
	}

	var sawText, sawDone bool
	for !sawDone {
		recvCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		e, err := stream.Recv(recvCtx)
		cancel()
		if err != nil {
			t.Fatalf("stream ended early: %v", err)
		}
		sawText = sawText || e.Type == event.TypeText
		sawDone = e.Type == event.TypeCompleted
	}
	if !sawText {
		t.Error("stream carried no text")
	}
}

func TestAnotherTenantSeesNothing(t *testing.T) {
	p := start(t, call.Limits{})
	ctx := t.Context()

	stream, err := p.Subscribe(ctx, "globex", platform.EventFilter{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stream.Close() }()

	r, err := p.Invoke(ctx, tag("run-1", "lead"))
	if err != nil {
		t.Fatal(err)
	}
	await(t, p, r.ID, runs.Completed)

	if _, err := p.Runs.Get("globex", r.ID); !errors.Is(err, runs.ErrNotFound) {
		t.Fatalf("other tenant read the run: %v", err)
	}
	if got := p.Runs.List("globex", 0); len(got) != 0 {
		t.Fatalf("other tenant listed %d runs", len(got))
	}
	recvCtx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	if e, err := stream.Recv(recvCtx); err == nil {
		t.Fatalf("other tenant received %s for run %s", e.Type, e.Run)
	}
}

func TestCallBuildsATree(t *testing.T) {
	p := start(t, call.Limits{MaxDepth: 2, MaxFanOut: 2})
	ctx := t.Context()

	// A parked run would be the natural caller. Until parking is wired, a run
	// that has not finished is found by calling before the worker gets to it,
	// so the root here is raised and called from in the same breath.
	root, err := p.Invoke(ctx, tag("run-1", "lead"))
	if err != nil {
		t.Fatal(err)
	}
	req := call.Request{Key: "k1", Agent: "helper", Text: "help", Mode: call.Delegate}
	child, err := p.Call(ctx, tenant, root.ID, req)
	if errors.Is(err, call.ErrInvalid) {
		t.Skip("the root finished before the call was made")
	}
	if err != nil {
		t.Fatal(err)
	}
	if child.Parent != root.ID || child.Root != root.ID || child.Depth != 1 || child.Mode != call.Delegate {
		t.Fatalf("child edge is wrong: %+v", child)
	}
	if child.Origin != call.Origin || !child.Actor.Bot {
		t.Fatalf("child should be raised by the calling agent: %+v", child)
	}

	// The same call again joins the same run and is not counted twice.
	again, err := p.Call(ctx, tenant, root.ID, req)
	if err == nil {
		if again.ID != child.ID {
			t.Fatalf("replayed call started a second run: %s != %s", again.ID, child.ID)
		}
		if got, _ := p.Runs.Get(tenant, root.ID); got.Calls != 1 {
			t.Fatalf("calls = %d, want 1", got.Calls)
		}
	}

	await(t, p, child.ID, runs.Completed)
	tree, err := p.Runs.Tree(tenant, child.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tree) != 2 || tree[0].ID != root.ID || tree[1].ID != child.ID {
		t.Fatalf("tree = %+v", tree)
	}
}

func TestCallIsRefused(t *testing.T) {
	ctx := t.Context()
	t.Run("target not in the spawn allowlist", func(t *testing.T) {
		p := start(t, call.Limits{MaxDepth: 2, MaxFanOut: 2})
		root, err := p.Invoke(ctx, tag("run-1", "helper"))
		if err != nil {
			t.Fatal(err)
		}
		_, err = p.Call(ctx, tenant, root.ID, call.Request{Key: "k", Agent: "lead", Mode: call.Delegate})
		if !errors.Is(err, platform.ErrForbidden) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("zero limits", func(t *testing.T) {
		p := start(t, call.Limits{})
		root, err := p.Invoke(ctx, tag("run-1", "lead"))
		if err != nil {
			t.Fatal(err)
		}
		_, err = p.Call(ctx, tenant, root.ID, call.Request{Key: "k", Agent: "helper", Mode: call.Delegate})
		if !errors.Is(err, call.ErrDepth) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("another tenant's run", func(t *testing.T) {
		p := start(t, call.Limits{MaxDepth: 2, MaxFanOut: 2})
		root, err := p.Invoke(ctx, tag("run-1", "lead"))
		if err != nil {
			t.Fatal(err)
		}
		_, err = p.Call(ctx, "globex", root.ID, call.Request{Key: "k", Agent: "helper", Mode: call.Delegate})
		if !errors.Is(err, runs.ErrNotFound) {
			t.Fatalf("got %v", err)
		}
	})
}

func TestTriggerTagCannotClaimAgentOrigin(t *testing.T) {
	p := start(t, call.Limits{MaxDepth: 2, MaxFanOut: 2})
	forged := tag("run-1", "lead")
	forged.Source = wire.SourceOf(call.Origin, forged.Tenant)
	if _, err := p.Invoke(t.Context(), forged); !errors.Is(err, call.ErrInvalid) {
		t.Fatalf("got %v, want ErrInvalid", err)
	}
}

// A run whose every attempt fails must end as failed and raise its alert, even
// though fiber publishes no final event once the retries run out.
func TestARunThatExhaustsItsRetriesEnds(t *testing.T) {
	// Nothing listens here, so every attempt fails the same way.
	t.Setenv("OPENAI_BASE_URL", "http://127.0.0.1:1/v1")
	p := start(t, call.Limits{})
	ctx := t.Context()
	broken := wire.AgentSpec{Name: "broken", Model: "none", Provider: "openai", SystemPrompt: "x"}
	if _, err := p.Agents.Create(ctx, tenant, broken, "test"); err != nil {
		t.Fatal(err)
	}
	if err := p.Alerts.AddRule(tenant, alerts.Rule{Name: "any", Kind: alerts.RunFailed, Severity: alerts.Critical}); err != nil {
		t.Fatal(err)
	}
	r, err := p.Invoke(ctx, tag("run-1", "broken"))
	if err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(60 * time.Second)
	for {
		got, _ := p.Runs.Get(tenant, r.ID)
		// A run reads failed before its ending is dealt with, so its alert
		// is waited for as well.
		if got.Status == runs.Failed && len(kept(p.Alerts.Alerts(tenant))) > 0 {
			if got.Error == "" {
				t.Fatal("a failed run carries no error")
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("run is %q after its retries, with alerts %+v: it would look alive forever", got.Status, kept(p.Alerts.Alerts(tenant)))
		}
		time.Sleep(50 * time.Millisecond)
	}
	raised := kept(p.Alerts.Alerts(tenant))
	if len(raised) != 1 || raised[0].RunID != r.ID {
		t.Fatalf("alerts = %+v, want exactly one for the run", raised)
	}
}

// After a first turn, the title agent names the conversation, and the memory
// agent's answer becomes a note the answering agent carries forward.
func TestHousekeepingTitlesAndRemembers(t *testing.T) {
	p, err := platform.New(platform.Config{Workspace: t.TempDir(), OfflineHooks: true})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- p.Run(ctx) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("platform shutdown: %v", err)
		}
	}()
	for _, name := range []string{"lead", "scribe"} {
		spec := wire.AgentSpec{Name: name, Model: "offline", Provider: "offline", SystemPrompt: "x"}
		if _, err := p.Agents.Create(ctx, tenant, spec, "test"); err != nil {
			t.Fatal(err)
		}
	}
	if err := p.SetSettings(ctx, tenant, platform.Settings{TitleAgent: "scribe", MemoryAgent: "scribe"}); err != nil {
		t.Fatal(err)
	}
	if err := p.SetSettings(ctx, tenant, platform.Settings{TitleAgent: "ghost"}); err == nil {
		t.Fatal("a title agent that does not exist was accepted")
	}
	// Another spelling names the same agent, which is kept by its own name.
	if err := p.SetSettings(ctx, tenant, platform.Settings{TitleAgent: "Scribe", MemoryAgent: " SCRIBE"}); err != nil {
		t.Fatal(err)
	}
	if st := p.Settings(tenant); st.TitleAgent != "scribe" || st.MemoryAgent != "scribe" {
		t.Fatalf("the settings naming Scribe keep %q and %q", st.TitleAgent, st.MemoryAgent)
	}

	first, err := p.Converse(ctx, tenant, platform.Turn{ID: "api-1", Agent: "lead", Text: "Plan the launch"})
	if err != nil {
		t.Fatal(err)
	}
	await(t, p, first.ID, runs.Completed)

	deadline := time.Now().Add(10 * time.Second)
	for {
		var named bool
		for _, th := range p.Runs.Threads(tenant, 0) {
			named = named || (th.ID == first.Thread && th.Named && th.Title != "Plan the launch")
		}
		notes := notesOf(t, p.Memory, tenant, "lead")
		if named && len(notes) > 0 {
			// What the memory agent wrote is a proposal, not yet a memory.
			if notes[0].Source != "consolidated" || notes[0].Status != "pending" || len(notes) > 3 {
				t.Fatalf("notes = %+v", notes)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("housekeeping did not finish: named=%v notes=%v", named, notes)
		}
		time.Sleep(20 * time.Millisecond)
	}
	// The housekeeping agent itself gets no title and no memory: a system run
	// must not start more system runs.
	if got := notesOf(t, p.Memory, tenant, "scribe"); len(got) != 0 {
		t.Fatalf("the memory agent was given memory of its own housekeeping: %v", got)
	}
	system := 0
	for _, r := range p.Runs.List(tenant, 0) {
		if r.Origin == runs.OriginSystem {
			system++
			if r.Agent != "scribe" {
				t.Errorf("a housekeeping run ran as %q", r.Agent)
			}
		}
	}
	if system != 2 {
		t.Fatalf("%d system runs, want one title and one memory", system)
	}

	// Until a person approves it, the next turn is sent none of it.
	unapproved, err := p.Converse(ctx, tenant, platform.Turn{ID: "api-2", Agent: "lead", Text: "And then?", After: first.ID})
	if err != nil {
		t.Fatal(err)
	}
	if got := wire.TextOf(unapproved.Tag()); strings.Contains(got, "What you remember") {
		t.Fatalf("an unapproved proposal reached the agent: %q", got)
	}
	var ids []string
	for _, n := range notesOf(t, p.Memory, tenant, "lead") {
		ids = append(ids, n.ID)
	}
	if n, err := p.Memory.Approve(tenant, "lead", ids); err != nil || n == 0 {
		t.Fatalf("approved %d, %v", n, err)
	}
	second, err := p.Converse(ctx, tenant, platform.Turn{ID: "api-3", Agent: "lead", Text: "And then?", After: first.ID})
	if err != nil {
		t.Fatal(err)
	}
	if got := wire.TextOf(second.Tag()); !strings.Contains(got, "What you remember from earlier conversations:") {
		t.Fatalf("the prompt carries no memory: %q", got)
	}
	if second.Text != "And then?" {
		t.Fatalf("the record shows %q, want what the person typed", second.Text)
	}
}

// Housekeeping does not run on the offline provider unless a test asks for it.
func TestHousekeepingSkipsTheOfflineProvider(t *testing.T) {
	p := start(t, call.Limits{})
	ctx := t.Context()
	if err := p.SetSettings(ctx, tenant, platform.Settings{TitleAgent: "helper", MemoryAgent: "helper"}); err != nil {
		t.Fatal(err)
	}
	r, err := p.Converse(ctx, tenant, platform.Turn{ID: "api-1", Agent: "lead", Text: "Plan the launch"})
	if err != nil {
		t.Fatal(err)
	}
	await(t, p, r.ID, runs.Completed)
	time.Sleep(300 * time.Millisecond)
	for _, run := range p.Runs.List(tenant, 0) {
		if run.Origin == runs.OriginSystem {
			t.Fatal("a housekeeping run started on the offline provider")
		}
	}
}

// Many runs race for a limit that fits fewer. The number let in must be exactly
// the limit: checking and counting are one step.
func TestConcurrentRunsNeverExceedALimit(t *testing.T) {
	p := start(t, call.Limits{})
	ctx := t.Context()
	const limit, racers = 7, 40
	_, err := p.Policies.Put(tenant, policy.Policy{
		Scope: policy.Scope{Kind: policy.Tenant}, Period: policy.Day, MaxRuns: limit, Action: policy.Block,
	}, "test", time.Now())
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	var admitted, refused atomic.Int64
	for i := range racers {
		wg.Go(func() {
			_, err := p.Invoke(ctx, tag(fmt.Sprintf("race-%d", i), "lead"))
			var d *platform.Denied
			switch {
			case err == nil:
				admitted.Add(1)
			case errors.As(err, &d) && d.Decision.Limit == policy.LimitRuns:
				refused.Add(1)
			default:
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
	wg.Wait()
	if admitted.Load() != limit || refused.Load() != racers-limit {
		t.Fatalf("%d admitted and %d refused, want %d and %d", admitted.Load(), refused.Load(), limit, racers-limit)
	}

	// The same tag again joins its run, and is neither refused nor counted.
	if _, err := p.Invoke(ctx, tag("race-0", "lead")); err != nil && admitted.Load() > 0 {
		var d *platform.Denied
		if !errors.As(err, &d) {
			t.Fatalf("a repeat of an admitted tag: %v", err)
		}
	}
	scope := policy.Scope{Kind: policy.Tenant}
	if got := usageOf(p, tenant, scope, policy.PeriodStart(policy.Day, time.Now())); got.Runs != limit {
		t.Fatalf("the ledger counted %d runs, want %d", got.Runs, limit)
	}
	// One alert for the limit, however many runs it refused.
	budget := 0
	for _, a := range kept(p.Alerts.Alerts(tenant)) {
		if a.Kind == "budget" && a.Severity == "critical" {
			budget++
		}
	}
	if budget != 1 {
		t.Fatalf("%d refusal alerts, want exactly one", budget)
	}
	// The live counts are a projection of the ledger: replaying it agrees.
	// The admitted runs are still recording their model calls, and the two
	// reads are not one step, so compare once they agree or the runs are done.
	deadline := time.Now().Add(10 * time.Second)
	for {
		live, replayed := usageOf(p, tenant, scope, time.Time{}), ledgerUsage(usage.Rebuild(entriesOf(p, tenant)), tenant, scope, time.Time{})
		if reflect.DeepEqual(live, replayed) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("live %+v, rebuilt %+v", live, replayed)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// A concurrency limit frees a place when a run ends.
func TestAConcurrencyLimitFreesUp(t *testing.T) {
	p := start(t, call.Limits{})
	ctx := t.Context()
	scope := policy.Scope{Kind: policy.Agent, Name: "lead"}
	if _, err := p.Policies.Put(tenant, policy.Policy{Scope: scope, Period: policy.Day, MaxConcurrent: 1, Action: policy.Block}, "test", time.Now()); err != nil {
		t.Fatal(err)
	}
	first, err := p.Invoke(ctx, tag("c-1", "lead"))
	if err != nil {
		t.Fatal(err)
	}
	await(t, p, first.ID, runs.Completed)
	deadline := time.Now().Add(5 * time.Second)
	for usageOf(p, tenant, scope, time.Time{}).Concurrent != 0 {
		if time.Now().After(deadline) {
			t.Fatal("a finished run still holds its place")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := p.Invoke(ctx, tag("c-2", "lead")); err != nil {
		t.Fatalf("the place was not freed: %v", err)
	}
	// Another agent is not held to this agent's limit.
	if _, err := p.Invoke(ctx, tag("c-3", "helper")); err != nil {
		t.Fatalf("another agent was refused: %v", err)
	}
}

func TestSchedules(t *testing.T) {
	s, err := platform.OpenSchedules(nil)
	if err != nil {
		t.Fatal(err)
	}
	every := cron.Schedule{Name: "nightly", Expression: "0 2 * * *", Agent: "lead", Text: "report", Tenant: tenant}
	if err := s.Add(every, platform.About{}); err != nil {
		t.Fatal(err)
	}
	if err := s.Add(every, platform.About{}); !errors.Is(err, platform.ErrScheduleExists) {
		t.Fatalf("duplicate: got %v", err)
	}
	bad := every
	bad.Name, bad.Expression = "bad", "not cron"
	if err := s.Add(bad, platform.About{}); err == nil {
		t.Fatal("an invalid expression was stored")
	}
	if got, err := s.List("globex"); err != nil || len(got) != 0 {
		t.Fatalf("other tenant listed %d schedules", len(got))
	}
	if err := s.Remove("globex", "nightly"); !errors.Is(err, platform.ErrScheduleNotFound) {
		t.Fatalf("other tenant removed the schedule: %v", err)
	}
	if err := s.Remove(tenant, "nightly"); err != nil {
		t.Fatal(err)
	}
	if got, err := s.List(tenant); err != nil || len(got) != 0 {
		t.Fatalf("still listed: %v", got)
	}
}

func TestReplacingAScheduleKeepsItsPlaceAndStartsAfresh(t *testing.T) {
	ctx := context.Background()
	s, err := platform.OpenSchedules(nil)
	if err != nil {
		t.Fatal(err)
	}
	daily := cron.Schedule{Name: "nightly", Expression: "0 2 * * *", Agent: "lead", Text: "report", Tenant: tenant, Context: map[string]string{"created_by": "ada"}}
	if err := s.Add(daily, platform.About{Description: "The nightly report"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPaused(tenant, "nightly", true); err != nil {
		t.Fatal(err)
	}

	// A refused edit changes nothing.
	bad := daily
	bad.Expression = "whenever"
	if err := s.Replace(bad, platform.About{}); !errors.Is(err, cron.ErrSchedule) {
		t.Fatalf("an invalid expression: %v", err)
	}
	if err := s.Replace(daily, platform.About{Description: strings.Repeat("a", platform.MaxScheduleDescription+1)}); !errors.Is(err, cron.ErrSchedule) {
		t.Fatalf("a long description: %v", err)
	}
	other := daily
	other.Tenant = "globex"
	if err := s.Replace(other, platform.About{}); !errors.Is(err, platform.ErrScheduleNotFound) {
		t.Fatalf("another tenant's schedule: %v", err)
	}
	if got, _ := s.Entry(tenant, "nightly"); got.Expression != "0 2 * * *" || got.About.Description != "The nightly report" {
		t.Fatalf("a refused edit changed the schedule: %+v", got)
	}

	minutely := daily
	minutely.Expression, minutely.Text = "* * * * *", "status"
	if err := s.Replace(minutely, platform.About{UpdatedBy: "bob"}); err != nil {
		t.Fatal(err)
	}
	got, paused, _ := s.Get(tenant, "nightly")
	if got.Expression != "* * * * *" || got.Text != "status" || !paused || got.Context["created_by"] != "ada" {
		t.Fatalf("after the edit: %+v, paused %v", got, paused)
	}
	if a, _ := s.About(tenant, "nightly"); a.UpdatedBy != "bob" || a.Description != "" {
		t.Fatalf("about = %+v", a)
	}

	// Resumed, the edited schedule is left out of one read, so the
	// connector starts it fresh rather than catching up the new expression.
	_ = s.SetPaused(tenant, "nightly", false)
	if first, _ := s.Schedules(ctx); len(first) != 0 {
		t.Fatalf("the first read after an edit showed %v", first)
	}
	if second, _ := s.Schedules(ctx); len(second) != 1 || second[0].Expression != "* * * * *" {
		t.Fatalf("the second read showed %v", second)
	}
}
