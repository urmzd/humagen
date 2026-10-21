package platform_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/urmzd/fiber/pkg/connectors/cron"
	"github.com/urmzd/fiber/pkg/envelope"
	"github.com/urmzd/saige/agent/agenttest"
	saigetypes "github.com/urmzd/saige/agent/types"

	"github.com/urmzd/warp/internal/platform"
	"github.com/urmzd/warp/pkg/alerts"
	"github.com/urmzd/warp/pkg/call"
	"github.com/urmzd/warp/pkg/memory"
	"github.com/urmzd/warp/pkg/policy"
	"github.com/urmzd/warp/pkg/projects"
	"github.com/urmzd/warp/pkg/runs"
	"github.com/urmzd/warp/pkg/state"
	"github.com/urmzd/warp/pkg/state/statetest"
	"github.com/urmzd/warp/pkg/usage"
)

var (
	ada    = projects.Writer{By: "ada"}
	govern = projects.Writer{By: "root", Govern: true}
)

// field makes Field with Globex and Initech under it, and Renewals under
// Globex.
func field(t *testing.T, p *platform.Platform) {
	t.Helper()
	for _, in := range []platform.ProjectInput{
		{Name: "Field", Title: "Field engineering"},
		{Name: "Globex", Parent: "Field", Kind: projects.Account, Account: &projects.AccountInfo{Customer: "Globex Inc"}},
		{Name: "Initech", Parent: "Field"},
		{Name: "Renewals", Parent: "Globex"},
	} {
		if _, err := p.CreateProject(t.Context(), tenant, ada, in); err != nil {
			t.Fatal(err)
		}
	}
}

func inProject(id, agent, project string) envelope.Tag {
	tg := tag(id, agent)
	tg.Meta = map[string]string{runs.MetaProject: project}
	return tg
}

// faultyIn fails every write to one namespace while it is set, so a test can
// stop the platform between two writes to different stores.
type faultyIn struct {
	state.Store
	mu        sync.Mutex
	namespace string
}

func (f *faultyIn) fail(namespace string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.namespace = namespace
}

func (f *faultyIn) Write(ctx context.Context, namespace string, ops ...state.Op) error {
	f.mu.Lock()
	failing := f.namespace == namespace
	f.mu.Unlock()
	if failing {
		return statetest.ErrInjected
	}
	return f.Store.Write(ctx, namespace, ops...)
}

// legacySettings writes settings as an earlier version kept them, with the
// workspace's projects inside.
func legacySettings(t *testing.T, st state.Store, tenantName string, setupAt time.Time) {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"baseline": "", "history": "recent", "history_limit": 12000, "workspace_name": "Acme",
		"setup_at": setupAt, "setup_by": "ada",
		"projects": []map[string]string{{"name": "Launch", "description": "Everything for the Q4 launch"}, {"name": "Ops"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Write(t.Context(), "settings", state.Put(tenantName, "settings", raw)); err != nil {
		t.Fatal(err)
	}
}

func storedSettings(t *testing.T, st state.Store, tenantName string) map[string]any {
	t.Helper()
	raw, err := st.Get(t.Context(), "settings", tenantName, "settings")
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func newOn(t *testing.T, st state.Store) (*platform.Platform, error) {
	t.Helper()
	p, err := platform.New(platform.Config{Backends: platform.Backends{State: st}, Workspace: t.TempDir()})
	if err == nil {
		t.Cleanup(func() { _ = p.Close() })
	}
	return p, err
}

func TestTheMigrationMovesProjectsOutOfSettings(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		setup := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)
		legacySettings(t, st, tenant, setup)
		p, err := newOn(t, st)
		if err != nil {
			t.Fatal(err)
		}
		list := p.Projects.List(tenant)
		if len(list) != 2 || list[0].Name != "Launch" || list[0].Description != "Everything for the Q4 launch" || list[1].Name != "Ops" {
			t.Fatalf("migrated = %+v", list)
		}
		for _, pr := range list {
			if pr.Kind != projects.Team || pr.Parent != "" || pr.Title != "" || pr.CreatedBy != "migration" || !pr.CreatedAt.Equal(setup) || pr.Version != 1 {
				t.Fatalf("a migrated project = %+v", pr)
			}
		}
		if root, _ := p.GetProject(tenant, "~"); root.Version != 1 {
			t.Fatalf("the migration did not create the root: %+v", root)
		}
		if _, held := storedSettings(t, st, tenant)["projects"]; held {
			t.Fatal("settings still hold the projects")
		}
		if s := p.Settings(tenant); s.WorkspaceName != "Acme" || s.SetupBy != "ada" {
			t.Fatalf("the rest of the settings = %+v", s)
		}
		// A second start finds nothing to move and changes nothing.
		if err := p.Close(); err != nil {
			t.Fatal(err)
		}
		again, err := newOn(t, st)
		if err != nil {
			t.Fatal(err)
		}
		if h, _ := again.Projects.History(tenant, "Launch", 0, 0); len(h) != 1 {
			t.Fatalf("a second start wrote history: %+v", h)
		}
	})
}

func TestTheMigrationIsRepairedAfterAStopBetweenItsWrites(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		f := &faultyIn{Store: open(t)}
		legacySettings(t, f, tenant, time.Time{})
		// The import is written; the platform stops before settings are.
		f.fail("settings")
		if _, err := newOn(t, f); !errors.Is(err, statetest.ErrInjected) {
			t.Fatalf("a start that could not clear the settings = %v", err)
		}
		if _, held := storedSettings(t, f, tenant)["projects"]; !held {
			t.Fatal("the settings changed although their write failed")
		}
		f.fail("")
		p, err := newOn(t, f)
		if err != nil {
			t.Fatal(err)
		}
		if l := p.Projects.List(tenant); len(l) != 2 {
			t.Fatalf("after the repair = %+v", l)
		}
		for _, name := range []string{"", "Launch", "Ops"} {
			if h, _ := p.Projects.History(tenant, name, 0, 0); len(h) != 1 || h[0].What != projects.Created {
				t.Fatalf("the repair imported %q again: %+v", name, h)
			}
		}
		if _, held := storedSettings(t, f, tenant)["projects"]; held {
			t.Fatal("the repair did not clear the settings")
		}
	})
}

func TestAFailedImportFailsTheStartAndChangesNothing(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		f := &faultyIn{Store: open(t)}
		legacySettings(t, f, tenant, time.Time{})
		f.fail("projects")
		if _, err := newOn(t, f); !errors.Is(err, statetest.ErrInjected) {
			t.Fatalf("a start whose import failed = %v", err)
		}
		recs, err := f.Load(t.Context(), "projects")
		if err != nil || len(recs) != 0 {
			t.Fatalf("a failed import left %d records (%v)", len(recs), err)
		}
		if _, held := storedSettings(t, f, tenant)["projects"]; !held {
			t.Fatal("a failed import cleared the settings")
		}
	})
}

func TestFortyRacersInTwoChildrenAgainstAParentsLimitAdmitSeven(t *testing.T) {
	p := start(t, call.Limits{})
	field(t, p)
	ctx := t.Context()
	const limit, racers = 7, 40
	fieldScope := policy.Scope{Kind: policy.Project, Name: "Field"}
	if _, err := p.Policies.Put(tenant, policy.Policy{Scope: fieldScope, Period: policy.Day, MaxRuns: limit, Action: policy.Block}, "test", time.Now()); err != nil {
		t.Fatal(err)
	}
	// A child's own looser limit does not let it escape its parent's.
	if _, err := p.Policies.Put(tenant, policy.Policy{Scope: policy.Scope{Kind: policy.Project, Name: "Globex"}, Period: policy.Day, MaxRuns: 1000, Action: policy.Block}, "test", time.Now()); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	var admitted, refused atomic.Int64
	for i := range racers {
		wg.Go(func() {
			child := []string{"Globex", "Initech"}[i%2]
			_, err := p.Invoke(ctx, inProject(fmt.Sprintf("race-%d", i), "lead", child))
			var d *platform.Denied
			switch {
			case err == nil:
				admitted.Add(1)
			case errors.As(err, &d) && d.Decision.Limit == policy.LimitRuns && d.Decision.Policy.Scope == fieldScope:
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
	today := policy.PeriodStart(policy.Day, time.Now())
	if got := p.Usage.Usage(tenant, fieldScope, today); got.Runs != limit {
		t.Fatalf("Field counted %d runs, want %d", got.Runs, limit)
	}
	// Each run is pinned to what it was admitted within.
	for _, r := range p.Runs.List(tenant, 0) {
		if !slices.Equal(r.Within, []string{"Field"}) {
			t.Fatalf("run %s in %s is within %q", r.ID, r.Project, r.Within)
		}
	}
	// A deeper run counts toward every ancestor, and a sibling's limit never
	// applies.
	if _, err := p.Invoke(ctx, inProject("deep", "lead", "Renewals")); err == nil {
		t.Fatal("a run two levels down escaped Field's limit")
	}
	if _, err := p.Invoke(ctx, tag("at-root", "lead")); err != nil {
		t.Fatalf("a run at the root was held to Field's limit: %v", err)
	}
	scope := policy.Scope{Kind: policy.Tenant}
	deadline := time.Now().Add(10 * time.Second)
	for {
		live, replayed := p.Usage.Usage(tenant, fieldScope, time.Time{}), usage.Rebuild(p.Usage.Entries()).Usage(tenant, fieldScope, time.Time{})
		all, allReplayed := p.Usage.Usage(tenant, scope, time.Time{}), usage.Rebuild(p.Usage.Entries()).Usage(tenant, scope, time.Time{})
		if reflect.DeepEqual(live, replayed) && reflect.DeepEqual(all, allReplayed) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("live %+v, rebuilt %+v", live, replayed)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// A run in one child spends to its parent's limit, to the micro-dollar, and a
// run in the other child is refused with nothing left. A run that crosses the
// parent's limit mid-run is stopped.
func TestSpendStopsAtTheParentsLimitToTheMicroDollar(t *testing.T) {
	p := metered(t, platform.Config{}, map[string]agentModel{
		"lead": {model: func(int) saigetypes.Provider { return script(spends(say("Done."), 1000, 100)) }},
		"probe": {tools: probes, model: func(int) saigetypes.Provider {
			return priced{Provider: &heldAfter{ScriptedProvider: &agenttest.ScriptedProvider{Responses: [][]saigetypes.Delta{
				spends(probe("p1"), 1000, 100), spends(probe("p2"), 1000, 100),
			}}, n: 2}, pricing: card}
		}},
	})
	field(t, p)
	ctx := t.Context()
	fieldScope := policy.Scope{Kind: policy.Project, Name: "Field"}
	if _, err := p.Policies.Put(tenant, policy.Policy{Scope: fieldScope, Period: policy.Month, SpendLimit: cost(1000, 100), Action: policy.Block}, "ada", time.Now()); err != nil {
		t.Fatal(err)
	}
	r, err := p.Invoke(ctx, inProject("evt-1", "lead", "Globex"))
	if err != nil {
		t.Fatal(err)
	}
	await(t, p, r.ID, runs.Completed)
	month := policy.PeriodStart(policy.Month, time.Now())
	within(t, "Field is charged for Globex's run", func() bool { return p.Usage.Usage(tenant, fieldScope, month).Spend == cost(1000, 100) })

	_, err = p.Invoke(ctx, inProject("evt-2", "lead", "Initech"))
	var denied *platform.Denied
	if !errors.As(err, &denied) || denied.Decision.Limit != policy.LimitSpend || denied.Decision.Policy.Scope != fieldScope || denied.Decision.Remaining != 0 {
		t.Fatalf("a run in the sibling was not refused at Field's spend limit: %v", err)
	}
	// One micro-dollar more, and the sibling may start.
	if _, err := p.Policies.Put(tenant, policy.Policy{Scope: fieldScope, Period: policy.Month, SpendLimit: cost(1000, 100) + 1, Action: policy.Block}, "ada", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Invoke(ctx, inProject("evt-3", "lead", "Initech")); err != nil {
		t.Fatalf("a run with one micro-dollar left was refused: %v", err)
	}

	// A day limit on Globex's parent, crossed mid-run two levels down.
	if _, err := p.Policies.Put(tenant, policy.Policy{Scope: policy.Scope{Kind: policy.Project, Name: "Globex"}, Period: policy.Day, SpendLimit: 5000, Action: policy.Block}, "ada", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := p.Policies.Delete(tenant, fieldScope, "ada", time.Now()); err != nil {
		t.Fatal(err)
	}
	day := policy.PeriodStart(policy.Day, time.Now())
	globexScope := policy.Scope{Kind: policy.Project, Name: "Globex"}
	spentBefore := p.Usage.Usage(tenant, globexScope, day).Spend
	held, err := p.Invoke(ctx, inProject("evt-4", "probe", "Renewals"))
	if err != nil {
		t.Fatalf("a run under Globex's limit was refused: %v", err)
	}
	stopped := await(t, p, held.ID, runs.Stopped)
	if !strings.Contains(stopped.Cause, "project Globex has reached its limit of spend") {
		t.Fatalf("the run was stopped for %q", stopped.Cause)
	}
	within(t, "the stopped run is settled", func() bool { return settled(p, held.ID) == 1 })
	stopped, _ = p.Runs.Get(tenant, held.ID)
	if got := p.Usage.Usage(tenant, globexScope, day).Spend - spentBefore; stopped.Usage == nil || got != stopped.Usage.Total.Cost.Micros {
		t.Fatalf("Globex was charged %d for the run, which recorded %+v", got, stopped.Usage)
	}
}

func TestStorageIsSummedOverTheSubtree(t *testing.T) {
	p := start(t, call.Limits{})
	field(t, p)
	if _, err := p.Policies.Put(tenant, policy.Policy{Scope: policy.Scope{Kind: policy.Project, Name: "Field"}, Period: policy.Day, MaxStorage: 30, Action: policy.Block}, "test", time.Now()); err != nil {
		t.Fatal(err)
	}
	publish := func(id, project, content string) error {
		t.Helper()
		_, req := turn(t, p, id, project)
		if _, err := use(t, p, req, platform.ToolFileWrite, map[string]any{"path": "f.txt", "content": content}); err != nil {
			t.Fatal(err)
		}
		_, err := use(t, p, req, platform.ToolPublish, map[string]any{"path": "f.txt"})
		return err
	}
	if err := publish("in-globex", "Renewals", strings.Repeat("a", 20)); err != nil {
		t.Fatalf("a publish under Field's limit = %v", err)
	}
	if err := publish("in-initech", "Initech", strings.Repeat("b", 20)); !errors.Is(err, platform.ErrStorage) {
		t.Fatalf("a publish that takes Field's subtree past its limit = %v", err)
	}
	if err := publish("in-field", "Field", strings.Repeat("c", 10)); err != nil {
		t.Fatalf("a publish that fits = %v", err)
	}
	// Outside Field, its limit does not apply.
	if err := publish("at-root", "", strings.Repeat("d", 40)); err != nil {
		t.Fatalf("a publish outside Field = %v", err)
	}
}

func TestContextLayersInOrderAndWithinBudget(t *testing.T) {
	p := start(t, call.Limits{})
	ctx := t.Context()
	cfg := p.Settings(tenant)
	cfg.WorkspaceName = "Acme"
	if err := p.SetSettings(ctx, tenant, cfg); err != nil {
		t.Fatal(err)
	}
	field(t, p)
	instruct := func(name, text string) {
		t.Helper()
		w := ada
		if name == "~" {
			w = govern
		}
		pr, err := p.GetProject(tenant, name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := p.UpdateProject(ctx, tenant, name, w, platform.ProjectUpdate{Version: pr.Version, Title: pr.Title, Account: pr.Account, Instructions: text}); err != nil {
			t.Fatal(err)
		}
	}
	instruct("~", "ROOT-RULE "+strings.Repeat("r", 3980))
	instruct("Field", "FIELD-RULE "+strings.Repeat("f", 3980))
	instruct("Globex", "GLOBEX-RULE "+strings.Repeat("g", 3980))
	instruct("Renewals", "RENEWALS-RULE Close by the 15th.")
	note := func(project, text string) {
		t.Helper()
		owner, err := p.ProjectMemoryOwner(tenant, project)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := p.Memory.Add(tenant, owner, text, memory.Person, memory.Approved, "ada", time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	note("Renewals", "RENEWALS-NOTE The renewal is in March.")
	// Globex's notes alone pass the ancestors' budget.
	for i := range 30 {
		note("Globex", fmt.Sprintf("GLOBEX-NOTE-%02d %s", i, strings.Repeat("n", 230)))
	}
	note("Field", "FIELD-NOTE Field engineers own renewals.")
	note("~", "WORKSPACE-NOTE Acme sells robots.")
	if _, err := p.Memory.Add(tenant, "lead", "AGENT-NOTE Ada likes tables.", memory.Person, memory.Approved, "ada", time.Now()); err != nil {
		t.Fatal(err)
	}

	first, err := p.Converse(ctx, tenant, platform.Turn{ID: "t1", Agent: "lead", Text: "FIRST-MESSAGE", Project: "Renewals"})
	if err != nil {
		t.Fatal(err)
	}
	await(t, p, first.ID, runs.Completed)
	second, err := p.Converse(ctx, tenant, platform.Turn{ID: "t2", Agent: "lead", Text: "SECOND-MESSAGE", After: first.ID})
	if err != nil {
		t.Fatal(err)
	}
	text := second.Tag().Text
	order := []string{
		"the workspace's first, then each project's down to this one",
		"(instructions of Acme not sent)",
		"(instructions of Field engineering not sent)",
		"GLOBEX-RULE", "RENEWALS-RULE",
		"RENEWALS-NOTE",
		"GLOBEX-NOTE-00", "GLOBEX-NOTE-23", "(6 more notes from Globex not sent)",
		"(1 more note from Field engineering not sent)", "(1 more note from Acme not sent)",
		"AGENT-NOTE",
		"FIRST-MESSAGE", "SECOND-MESSAGE",
	}
	at := 0
	for _, want := range order {
		i := strings.Index(text[at:], want)
		if i < 0 {
			t.Fatalf("%q is missing or out of order in:\n%s", want, text)
		}
		at += i + len(want)
	}
	for _, never := range []string{"ROOT-RULE", "FIELD-RULE", "GLOBEX-NOTE-24", "FIELD-NOTE", "WORKSPACE-NOTE"} {
		if strings.Contains(text, never) {
			t.Fatalf("%q was sent past its budget", never)
		}
	}
	// What the person asked is what the record shows.
	if second.Text != "SECOND-MESSAGE" {
		t.Fatalf("the record shows %q", second.Text)
	}
	// At the root, the workspace's own instructions and notes are sent.
	top, err := p.Converse(ctx, tenant, platform.Turn{ID: "t3", Agent: "lead", Text: "AT-ROOT"})
	if err != nil {
		t.Fatal(err)
	}
	if got := top.Tag().Text; !strings.Contains(got, "ROOT-RULE") || !strings.Contains(got, "What everyone in Acme knows:\n- WORKSPACE-NOTE") || strings.Contains(got, "FIELD") {
		t.Fatalf("a turn at the root was sent:\n%s", got)
	}
}

// A run with nothing to add is sent what it was asked, byte for byte.
func TestATurnWithNoContextIsSentAsAsked(t *testing.T) {
	p := start(t, call.Limits{})
	r, err := p.Converse(t.Context(), tenant, platform.Turn{ID: "t1", Agent: "lead", Text: "just this"})
	if err != nil {
		t.Fatal(err)
	}
	if r.Tag().Text != "just this" {
		t.Fatalf("sent %q", r.Tag().Text)
	}
}

func TestACronRunInAProjectGetsItsContext(t *testing.T) {
	p := start(t, call.Limits{})
	ctx := t.Context()
	field(t, p)
	pr, _ := p.GetProject(tenant, "Globex")
	if _, err := p.UpdateProject(ctx, tenant, "Globex", ada, platform.ProjectUpdate{Version: pr.Version, Account: pr.Account, Instructions: "GLOBEX-RULE"}); err != nil {
		t.Fatal(err)
	}
	owner, _ := p.ProjectMemoryOwner(tenant, "Field")
	if _, err := p.Memory.Add(tenant, owner, "FIELD-NOTE Renewals are quarterly.", memory.Person, memory.Approved, "ada", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Memory.Add(tenant, "lead", "AGENT-NOTE Write short.", memory.Person, memory.Approved, "ada", time.Now()); err != nil {
		t.Fatal(err)
	}
	for _, origin := range []string{"cron", runs.OriginManual} {
		r, err := p.Invoke(ctx, envelope.Tag{
			ID: origin + "-1", Tenant: tenant, Agent: "lead", Origin: origin, Text: "What changed this week?",
			Meta: map[string]string{"cron_schedule": "recap", runs.MetaProject: "Globex", runs.MetaAsk: "forged", runs.MetaWithin: "Initech"},
		})
		if err != nil {
			t.Fatal(err)
		}
		sent := r.Tag().Text
		for i, want := range []string{"GLOBEX-RULE", "FIELD-NOTE", "AGENT-NOTE", "What changed this week?"} {
			if !strings.Contains(sent, want) {
				t.Fatalf("%s run, layer %d: %q missing from:\n%s", origin, i, want, sent)
			}
		}
		if r.Text != "What changed this week?" || r.Project != "Globex" || !slices.Equal(r.Within, []string{"Field"}) {
			t.Fatalf("%s run recorded %q in %q within %q", origin, r.Text, r.Project, r.Within)
		}
		await(t, p, r.ID, runs.Completed)
	}
	// A cron run is filed nowhere the tag cannot claim: a run from another
	// origin is not given a project, nor its context.
	other, err := p.Invoke(ctx, envelope.Tag{ID: "slack-1", Tenant: tenant, Agent: "lead", Origin: "slack", Text: "hi", Meta: map[string]string{runs.MetaProject: "Globex"}})
	if err != nil {
		t.Fatal(err)
	}
	if other.Project != "" || other.Within != nil || strings.Contains(other.Tag().Text, "GLOBEX-RULE") {
		t.Fatalf("a slack tag claimed a project: %+v", other)
	}
}

// A called run counts toward its root's chain, pinned when the root was
// admitted.
func TestACalledRunIsWithinItsRootsChain(t *testing.T) {
	p := start(t, call.Limits{MaxDepth: 2, MaxFanOut: 2})
	field(t, p)
	root, err := p.Invoke(t.Context(), inProject("root", "lead", "Renewals"))
	if err != nil {
		t.Fatal(err)
	}
	// As in TestCallBuildsATree, the root is called from before the worker
	// finishes it.
	child, err := p.Call(t.Context(), tenant, root.ID, call.Request{Key: "k1", Agent: "helper", Text: "help", Mode: call.Delegate})
	if errors.Is(err, call.ErrInvalid) {
		t.Skip("the root finished before the call was made")
	}
	if err != nil {
		t.Fatal(err)
	}
	if child.Project != "Renewals" || !slices.Equal(child.Within, []string{"Globex", "Field"}) {
		t.Fatalf("the called run is in %q within %q", child.Project, child.Within)
	}
}

func TestDeleteKeepsThePolicy(t *testing.T) {
	p := start(t, call.Limits{})
	ctx := t.Context()
	field(t, p)
	scope := policy.Scope{Kind: policy.Project, Name: "Initech"}
	if _, err := p.Policies.Put(tenant, policy.Policy{Scope: scope, Period: policy.Day, MaxRuns: 3, Action: policy.Block}, "ada", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := p.Schedules.Add(cron.Schedule{Name: "initech-recap", Expression: "0 9 * * 1", Agent: "lead", Text: "recap", Tenant: tenant,
		Meta: map[string]string{runs.MetaProject: "Initech"}}, platform.About{}); err != nil {
		t.Fatal(err)
	}
	if err := p.Alerts.AddRule(tenant, alerts.Rule{Name: "initech-fails", Kind: alerts.RunFailed, Severity: alerts.Critical, Project: "Initech", CreatedBy: "ada"}); err != nil {
		t.Fatal(err)
	}
	owner, _ := p.ProjectMemoryOwner(tenant, "Initech")
	if _, err := p.Memory.Add(tenant, owner, "Initech renews in May.", memory.Person, memory.Approved, "ada", time.Now()); err != nil {
		t.Fatal(err)
	}
	err := p.DeleteProject(ctx, tenant, "Initech", ada)
	if !errors.Is(err, projects.ErrInUse) {
		t.Fatalf("delete with a schedule, a rule and limits = %v", err)
	}
	for _, named := range []string{"the schedule initech-recap", "the alert rule initech-fails", "its limits"} {
		if !strings.Contains(err.Error(), named) {
			t.Fatalf("the refusal %q does not name %s", err, named)
		}
	}
	if err := p.Schedules.Remove(tenant, "initech-recap"); err != nil {
		t.Fatal(err)
	}
	if err := p.Alerts.RemoveRule(tenant, "initech-fails"); err != nil {
		t.Fatal(err)
	}
	// The limit still stands: deleting a project never drops one.
	if err := p.DeleteProject(ctx, tenant, "Initech", ada); !errors.Is(err, projects.ErrInUse) {
		t.Fatalf("delete with only its limits left = %v", err)
	}
	if got := p.Policies.For(tenant, []string{"Initech"}, "", ""); len(got) != 1 || got[0].Scope != scope {
		t.Fatalf("the policy after a refused delete = %+v", got)
	}
	if err := p.Policies.Delete(tenant, scope, "ada", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := p.DeleteProject(ctx, tenant, "Initech", ada); err != nil {
		t.Fatal(err)
	}
	if p.HasProject(tenant, "Initech") || len(p.Memory.List(tenant, owner)) != 0 {
		t.Fatal("the project or its notes outlived the delete")
	}
	// A parent with children, and the root, are refused.
	if err := p.DeleteProject(ctx, tenant, "Field", ada); !errors.Is(err, projects.ErrInUse) {
		t.Fatalf("delete with sub-projects = %v", err)
	}
	if err := p.DeleteProject(ctx, tenant, "~", govern); !errors.Is(err, projects.ErrInvalid) {
		t.Fatalf("delete the root = %v", err)
	}
	// The name needs govern to come back.
	if _, err := p.CreateProject(ctx, tenant, ada, platform.ProjectInput{Name: "Initech", Parent: "Field"}); !errors.Is(err, projects.ErrForbidden) {
		t.Fatalf("recreating a deleted name = %v", err)
	}
	if _, err := p.CreateProject(ctx, tenant, govern, platform.ProjectInput{Name: "Initech", Parent: "Field"}); err != nil {
		t.Fatal(err)
	}
}

// Filing something under a project while it is held keeps a delete from
// missing it.
func TestInProjectKeepsAProjectWhileSomethingIsFiled(t *testing.T) {
	p := start(t, call.Limits{})
	field(t, p)
	err := p.InProject(tenant, "Initech", func() error {
		return p.Schedules.Add(cron.Schedule{Name: "s", Expression: "0 9 * * 1", Agent: "lead", Text: "x", Tenant: tenant,
			Meta: map[string]string{runs.MetaProject: "Initech"}}, platform.About{})
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.DeleteProject(t.Context(), tenant, "Initech", ada); !errors.Is(err, projects.ErrInUse) {
		t.Fatalf("delete after a held filing = %v", err)
	}
	if err := p.InProject(tenant, "Nowhere", func() error { return nil }); !errors.Is(err, projects.ErrNotFound) {
		t.Fatalf("holding a missing project = %v", err)
	}
}

func TestProjectsArePerTenant(t *testing.T) {
	p := start(t, call.Limits{})
	field(t, p)
	if p.HasProject("globex", "Field") {
		t.Fatal("another tenant has acme's project")
	}
	if _, err := p.GetProject("globex", "Field"); !errors.Is(err, projects.ErrNotFound) {
		t.Fatalf("GetProject = %v", err)
	}
	if _, err := p.ProjectMemoryOwner("globex", "Field"); !errors.Is(err, projects.ErrNotFound) {
		t.Fatalf("ProjectMemoryOwner = %v", err)
	}
	if _, _, err := p.ProjectHistory("globex", "Field", 0, 0); !errors.Is(err, projects.ErrNotFound) {
		t.Fatalf("ProjectHistory = %v", err)
	}
	if err := p.DeleteProject(t.Context(), "globex", "Initech", ada); !errors.Is(err, projects.ErrNotFound) {
		t.Fatalf("DeleteProject = %v", err)
	}
	if _, err := p.Converse(t.Context(), "globex", platform.Turn{ID: "x", Agent: "lead", Text: "hi", Project: "Field"}); !errors.Is(err, platform.ErrSettings) {
		t.Fatalf("Converse in another tenant's project = %v", err)
	}
	// The run stays its own tenant's: another tenant's limits on a project
	// by the same name do not apply.
	if _, err := p.Policies.Put("globex", policy.Policy{Scope: policy.Scope{Kind: policy.Project, Name: "Field"}, Period: policy.Day, MaxRuns: 1, Action: policy.Block}, "x", time.Now()); err != nil {
		t.Fatal(err)
	}
	for i := range 3 {
		if _, err := p.Invoke(t.Context(), inProject(fmt.Sprintf("g-%d", i), "lead", "Globex")); err != nil {
			t.Fatalf("acme's run was held to globex's limit: %v", err)
		}
	}
}

func TestProjectLimitsAreTheChain(t *testing.T) {
	p := start(t, call.Limits{})
	field(t, p)
	for _, s := range []policy.Scope{{Kind: policy.Tenant}, {Kind: policy.Project, Name: "Field"}, {Kind: policy.Project, Name: "Renewals"}, {Kind: policy.Project, Name: "Initech"}} {
		if _, err := p.Policies.Put(tenant, policy.Policy{Scope: s, Period: policy.Day, MaxRuns: 50, Action: policy.Block}, "ada", time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	got, err := p.ProjectLimits(tenant, "Renewals")
	if err != nil {
		t.Fatal(err)
	}
	var scopes []string
	for _, pol := range got {
		scopes = append(scopes, pol.Scope.String())
	}
	if !slices.Equal(scopes, []string{"project Renewals", "project Field", "the workspace"}) {
		t.Fatalf("limits = %q", scopes)
	}
	if root, _ := p.ProjectLimits(tenant, "~"); len(root) != 1 || root[0].Scope.Kind != policy.Tenant {
		t.Fatalf("the root's limits = %+v", root)
	}
}
