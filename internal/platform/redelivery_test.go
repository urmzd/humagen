package platform_test

import (
	"testing"
	"time"

	"github.com/urmzd/orchagent/flow/turn/native"
	saigetypes "github.com/urmzd/saige/agent/types"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/inbox"
	"github.com/urmzd/humagen/pkg/journal"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

// The observer reads through a consumer group, so a replica that takes a
// tenant over folds again what the last one folded after its last commit.
// These tests hand the observer a run's events again, by (run, seq), and
// check that nothing it does on an event happens twice.

// journaled counts the journal's entries for a run, read by time, which is
// where a second copy of an event would show.
func journaled(t *testing.T, p *platform.Platform, runID string) int {
	t.Helper()
	entries, err := p.Journal.Query(journal.Query{Tenant: tenant, Limit: journal.MaxLimit})
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range entries {
		if e.RunID == runID {
			n++
		}
	}
	return n
}

// messagesAbout counts the inbox messages about a run: its questions and
// its failure.
func messagesAbout(t *testing.T, p *platform.Platform, runID string) int {
	t.Helper()
	msgs, _ := p.Inbox.Snapshot(tenant)
	n := 0
	for _, m := range msgs {
		about := m.Source.Run == runID
		for _, part := range m.Parts {
			about = about || (part.Failure != nil && part.Failure.Run == runID)
		}
		if about {
			n++
		}
	}
	return n
}

// redeliver hands the observer every event the run published, twice.
func redeliver(t *testing.T, p *platform.Platform, runID string) {
	t.Helper()
	events := kept(p.Journal.Events(tenant, []string{runID}))
	if len(events) == 0 {
		t.Fatalf("run %s journaled no events to redeliver", runID)
	}
	for range 2 {
		for _, e := range events {
			p.Redeliver(t.Context(), e)
		}
	}
}

// A question is raised once however often the event that raised it comes
// again, and the run stays parked on it.
func TestRedeliveredEventsRaiseAQuestionOnce(t *testing.T) {
	p := stage(t, &cast{continues: true,
		says:  map[string][][]saigetypes.Delta{"picker": {asking("Which colour?", "red", "blue"), say("Done.")}},
		tools: map[string][]string{"picker": {"ask_person"}},
		shape: map[string]func(*wire.AgentSpec){"picker": func(s *wire.AgentSpec) { s.Answerers = []string{"grace"} }},
	}, platform.Config{})
	run := ask(t, p, "api-picker", "picker")
	questionFor(t, p, run.ID)
	journal, messages := journaled(t, p, run.ID), messagesAbout(t, p, run.ID)

	redeliver(t, p, run.ID)

	if got := messagesAbout(t, p, run.ID); got != messages {
		t.Fatalf("redelivery changed the run's inbox messages from %d to %d", messages, got)
	}
	if got := journaled(t, p, run.ID); got != journal {
		t.Fatalf("redelivery changed the run's journal from %d to %d entries", journal, got)
	}
	if again, _ := p.Runs.Get(tenant, run.ID); again.Status != runs.Parked || again.Question == nil {
		t.Fatalf("redelivery moved the parked run to %q", again.Status)
	}
}

// A failed run is one failure message and one ending, however often its
// events come again.
func TestRedeliveredEventsEndAFailedRunOnce(t *testing.T) {
	p := startWith(t, platform.Config{
		Backoff: func(int) time.Duration { return time.Millisecond },
		AgentOptions: []native.Option{native.WithProvider(native.ProviderOffline, func(native.Spec) (saigetypes.Provider, error) {
			return refusing{}, nil
		})},
	})
	r, err := p.Invoke(t.Context(), tag("evt-1", "lead"))
	if err != nil {
		t.Fatal(err)
	}
	await(t, p, r.ID, runs.Failed)
	within(t, "the failure reached the inbox", func() bool { return messagesAbout(t, p, r.ID) > 0 })
	ended, _ := p.Runs.Get(tenant, r.ID)
	journal, messages := journaled(t, p, r.ID), messagesAbout(t, p, r.ID)
	open := len(p.Inbox.OpenOf(tenant, inbox.KindFailure))

	redeliver(t, p, r.ID)

	if got := messagesAbout(t, p, r.ID); got != messages {
		t.Fatalf("redelivery changed the run's inbox messages from %d to %d", messages, got)
	}
	if got := len(p.Inbox.OpenOf(tenant, inbox.KindFailure)); got != open {
		t.Fatalf("redelivery changed the open failures from %d to %d", open, got)
	}
	if got := journaled(t, p, r.ID); got != journal {
		t.Fatalf("redelivery changed the run's journal from %d to %d entries", journal, got)
	}
	again, _ := p.Runs.Get(tenant, r.ID)
	if again.Status != runs.Failed || !again.UpdatedAt.Equal(ended.UpdatedAt) {
		t.Fatalf("redelivery changed the failed run: %q at %v, was %v", again.Status, again.UpdatedAt, ended.UpdatedAt)
	}
}

// On Postgres a run is accepted through the outbox: its ledger row and its
// accepted event are written in one transaction, and the run then proceeds
// as any other.
func TestARunOnPostgresIsAcceptedThroughTheOutbox(t *testing.T) {
	pool := statetest.Pool(t)
	backends, release, err := platform.OpenPostgres(t.Context(), pool)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = release() })
	if backends.Outbox == nil {
		t.Fatal("the Postgres backends open no outbox")
	}
	if _, err := backends.Agents.Create(t.Context(), tenant, wire.AgentSpec{Name: "lead", Model: "offline", Provider: "offline", SystemPrompt: "Lead."}, "test"); err != nil {
		t.Fatal(err)
	}
	f := &fakes{}
	cfg := f.config()
	cfg.Backends, cfg.Bus = backends, backends.Bus
	p, _ := runningWith(t, cfg)
	r, err := p.Invoke(t.Context(), tag("evt-outbox", "lead"))
	if err != nil {
		t.Fatal(err)
	}
	await(t, p, r.ID, runs.Completed)
	var events, ledger int
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM fiber_events WHERE run = $1 AND type = 'run.accepted.v1'`, r.ID).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM fiber_runs WHERE run_id = $1`, r.ID).Scan(&ledger); err != nil {
		t.Fatal(err)
	}
	if events != 1 || ledger != 1 {
		t.Fatalf("accepted events = %d, ledger rows = %d, want 1 and 1", events, ledger)
	}
}
