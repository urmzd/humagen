package platform_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/anchor/pkg/ledger"
	dqueue "github.com/urmzd/anchor/pkg/queue"
	"github.com/urmzd/humagen/fiber/pkg/bus"
	"github.com/urmzd/humagen/internal/wire"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/leader"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

// shared stands in for one database under processes of different roles: the
// agents, the run ledger, the queue and the bus are the same objects.
type shared struct {
	agents wire.AgentStore
	ledger platform.Ledger
	queue  dqueue.Queue
	bus    bus.Bus
}

func newShared(t *testing.T) shared {
	t.Helper()
	s := shared{agents: wire.NewMemoryAgents(), ledger: ledger.NewMemory(), queue: dqueue.NewMemory(), bus: bus.NewMemory()}
	for _, spec := range []wire.AgentSpec{
		{Name: "lead", Model: "offline", Provider: "offline", SystemPrompt: "Lead."},
	} {
		if _, err := s.agents.Create(t.Context(), tenant, spec, "test"); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

func (s shared) config(role platform.ProcessRole) platform.Config {
	return platform.Config{
		Role: role, Bus: s.bus,
		Backends: platform.Backends{State: state.NewMemory(), Agents: s.agents, Ledger: s.ledger, Queue: s.queue},
	}
}

func TestTheDefaultRoleIsAllAndLeads(t *testing.T) {
	p := start(t, call.Limits{})
	if got := p.Process(); got.Role != platform.RoleAll {
		t.Fatalf("role = %q, want all", got.Role)
	}
	awaitLeader(t, p, true)
	r, err := p.Invoke(t.Context(), tag("run-all", "lead"))
	if err != nil {
		t.Fatal(err)
	}
	await(t, p, r.ID, runs.Completed)
}

func TestASplitRoleNeedsASharedQueue(t *testing.T) {
	for _, role := range []platform.ProcessRole{platform.RoleAPI, platform.RoleWorker} {
		_, err := platform.New(platform.Config{Role: role, Workspace: t.TempDir()})
		if err == nil || !strings.Contains(err.Error(), "ledger and queue") {
			t.Fatalf("role %s on memory: %v", role, err)
		}
	}
	if _, err := platform.New(platform.Config{Role: "scheduler", Workspace: t.TempDir()}); err == nil {
		t.Fatal("an unknown role was accepted")
	}
}

// An api process admits a run and executes none; a worker on the same queue
// executes it, and the api process sees it finish.
func TestAnAPIProcessLeavesTurnsToAWorker(t *testing.T) {
	s := newShared(t)
	api, _ := runningWith(t, s.config(platform.RoleAPI))
	if got := api.Process().Role; got != platform.RoleAPI {
		t.Fatalf("role = %q", got)
	}
	r, err := api.Invoke(t.Context(), tag("run-split", "lead"))
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)
	if got, _ := api.Runs.Get(tenant, r.ID); got.Status == runs.Completed {
		t.Fatal("an api process executed a turn")
	}

	runningWith(t, s.config(platform.RoleWorker))
	await(t, api, r.ID, runs.Completed)
}

func awaitLeader(t *testing.T, p *platform.Platform, want bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for p.Process().Leader != want {
		if time.Now().After(deadline) {
			t.Fatalf("leader = %v, want %v", !want, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Two platforms campaign on one Postgres: one leads, the other does not,
// and when the leader stops the other takes over.
func TestOnePlatformLeadsOnOneDatabase(t *testing.T) {
	pool := statetest.Pool(t)
	elect := func() leader.Elector {
		e, err := leader.NewPostgres(pool.Config().ConnConfig, leader.WithInterval(100*time.Millisecond))
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	s := newShared(t)
	cfgA, cfgB := s.config(platform.RoleAll), s.config(platform.RoleAll)
	cfgA.Leader, cfgB.Leader = elect(), elect()
	a, stopA := runningWith(t, cfgA)
	awaitLeader(t, a, true)
	b, _ := runningWith(t, cfgB)
	time.Sleep(time.Second)
	if b.Process().Leader {
		t.Fatal("both platforms lead")
	}
	since := a.Process().LeaderSince
	if since.IsZero() {
		t.Fatal("the leader does not say since when")
	}

	stopA()
	awaitLeader(t, b, true)
	if a.Process().Leader {
		t.Fatal("a stopped platform still leads")
	}
}

// The leader's connection to the database is cut: its term ends and a new
// one begins, on either platform, without either restarting.
func TestTheOtherPlatformTakesOverWhenTheLeadersConnectionCloses(t *testing.T) {
	pool := statetest.Pool(t)
	elect := func() leader.Elector {
		e, err := leader.NewPostgres(pool.Config().ConnConfig, leader.WithInterval(100*time.Millisecond))
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	s := newShared(t)
	cfgA, cfgB := s.config(platform.RoleAll), s.config(platform.RoleAll)
	cfgA.Leader, cfgB.Leader = elect(), elect()
	a, _ := runningWith(t, cfgA)
	awaitLeader(t, a, true)
	b, _ := runningWith(t, cfgB)
	first := a.Process().LeaderSince

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	var killed int
	if err := pool.QueryRow(ctx, `
		SELECT count(pg_terminate_backend(pid)) FROM pg_locks
		WHERE locktype = 'advisory' AND classid = $1::oid AND objid = hashtext(current_schema())::oid AND granted`,
		int64(leader.LockClass)).Scan(&killed); err != nil || killed != 1 {
		t.Fatalf("terminate the leader's session: killed %d, %v", killed, err)
	}
	// Either may win the free lock: b, or a campaigning again. A new term
	// begins, and only one platform leads.
	deadline := time.Now().Add(15 * time.Second)
	for {
		pa, pb := a.Process(), b.Process()
		if pa.Leader && pb.Leader {
			t.Fatal("both platforms lead")
		}
		if pb.Leader || pa.Leader && pa.LeaderSince.After(first) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no new leader: a=%+v b=%+v", pa, pb)
		}
		time.Sleep(10 * time.Millisecond)
	}

	// The new leader runs the singleton work: a run still completes, and
	// the old leader, still running, keeps serving.
	r, err := a.Invoke(t.Context(), tag("run-after-failover", "lead"))
	if err != nil {
		t.Fatal(err)
	}
	await(t, a, r.ID, runs.Completed)
}
