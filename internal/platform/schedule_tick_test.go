package platform_test

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/saige/flow/connectors/cron"
	"github.com/urmzd/saige/flow/event"
)

// clockTick is the event the cron connector publishes for the one occurrence
// of a schedule that falls between from and to, read from the platform's own
// schedule store. The connector's clock is the test's, so nothing sleeps.
func clockTick(t *testing.T, p *platform.Platform, name string, from, to time.Time) event.Envelope {
	t.Helper()
	now := from
	clock, err := cron.New(cron.Config{Store: p.Schedules, Interval: -1, MaxCatchUp: 1, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	published := make(chan event.Envelope, 8)
	done := make(chan struct{})
	go func() {
		defer close(done)
		// Ingest ends with the context's error when the test cancels it.
		_ = clock.Ingest(ctx, func(_ context.Context, e event.Envelope) error {
			published <- e
			return nil
		})
	}()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Error("the clock did not stop ingesting when its context ended")
		}
	}()
	for start := time.Now(); !clock.Ingesting(); time.Sleep(time.Millisecond) {
		if time.Since(start) > 10*time.Second {
			t.Fatal("the clock never began ingesting")
		}
	}
	// The store leaves a schedule out of the first read after it is written,
	// and the connector only notes a schedule the first time it is shown it.
	for range 2 {
		if err := clock.Tick(ctx); err != nil {
			t.Fatal(err)
		}
	}
	now = to
	if err := clock.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	for {
		select {
		case e := <-published:
			if tick, ok := e.Data.(event.CronTick); ok && tick.Schedule == name {
				return e
			}
		default:
			t.Fatalf("the clock published no occurrence of %s between %s and %s", name, from, to)
		}
	}
}

// A schedule's occurrence raised by the clock starts a run of the schedule's
// agent: the tick names no agent, and the platform reads it from the
// schedule, in the workspace and in a project alike.
func TestAClockTickStartsARunOfTheSchedulesAgent(t *testing.T) {
	for _, tc := range []struct{ name, project string }{{"in-workspace", ""}, {"in-project", "Field"}} {
		t.Run(tc.name, func(t *testing.T) {
			p := start(t, call.Limits{})
			field(t, p)
			meta := map[string]string{"created_by": "ada"}
			if tc.project != "" {
				meta[runs.MetaProject] = tc.project
			}
			if err := p.Schedules.Add(cron.Schedule{Name: "new-year", Expression: "0 0 1 1 *", Agent: "helper", Text: "Sum up the year.", Tenant: tenant, Context: meta}, platform.About{}); err != nil {
				t.Fatal(err)
			}
			due := time.Date(2031, 1, 1, 0, 0, 0, 0, time.UTC)
			tick := clockTick(t, p, "new-year", due.Add(-30*time.Second), due.Add(30*time.Second))
			if tick.Agent != "" || tick.ID != cron.OccurrenceID("new-year", due) {
				t.Fatalf("the clock's tick = %q for agent %q, want %q naming none", tick.ID, tick.Agent, cron.OccurrenceID("new-year", due))
			}

			p.Ingest(t.Context(), tick)

			got := p.Runs.ForSchedule(tenant, "new-year", 0, nil)
			if len(got) != 1 || got[0].Agent != "helper" || got[0].Project != tc.project {
				t.Fatalf("the occurrence's runs = %+v, want one of helper in %q", got, tc.project)
			}
			p.Ingest(t.Context(), tick)
			if again := p.Runs.ForSchedule(tenant, "new-year", 0, nil); len(again) != 1 || again[0].ID != got[0].ID {
				t.Fatalf("the same occurrence again = %+v, want the run %s only", again, got[0].ID)
			}
		})
	}
}

// A paused schedule runs nothing on the clock: a tick published just before
// the pause is dropped without an error, and once the schedule is resumed
// its next tick starts one run.
func TestAPausedSchedulesTickStartsNoRun(t *testing.T) {
	for _, tc := range []struct{ name, project string }{{"in-workspace", ""}, {"in-project", "Field"}} {
		t.Run(tc.name, func(t *testing.T) {
			said := &logs{}
			p := startWith(t, platform.Config{Log: slog.New(slog.NewTextHandler(said, nil))})
			field(t, p)
			meta := map[string]string{"created_by": "ada"}
			if tc.project != "" {
				meta[runs.MetaProject] = tc.project
			}
			if err := p.Schedules.Add(cron.Schedule{Name: "new-year", Expression: "0 0 1 1 *", Agent: "helper", Text: "Sum up the year.", Tenant: tenant, Context: meta}, platform.About{}); err != nil {
				t.Fatal(err)
			}
			due := time.Date(2031, 1, 1, 0, 0, 0, 0, time.UTC)
			tick := clockTick(t, p, "new-year", due.Add(-30*time.Second), due.Add(30*time.Second))
			if err := p.Schedules.SetPaused(tenant, "new-year", true); err != nil {
				t.Fatal(err)
			}

			p.Ingest(t.Context(), tick)

			if got := p.Runs.ForSchedule(tenant, "new-year", 0, nil); len(got) != 0 {
				t.Fatalf("a paused schedule's tick started %+v", got)
			}
			if out := said.String(); strings.Contains(out, "level=ERROR") || !strings.Contains(out, "a paused schedule's tick was not run") || !strings.Contains(out, "schedule=new-year") {
				t.Fatalf("the log of a paused schedule's tick:\n%s", out)
			}

			if err := p.Schedules.SetPaused(tenant, "new-year", false); err != nil {
				t.Fatal(err)
			}
			next := due.AddDate(1, 0, 0)
			p.Ingest(t.Context(), clockTick(t, p, "new-year", next.Add(-30*time.Second), next.Add(30*time.Second)))
			got := p.Runs.ForSchedule(tenant, "new-year", 0, nil)
			if len(got) != 1 || got[0].Agent != "helper" || got[0].Project != tc.project {
				t.Fatalf("the resumed schedule's runs = %+v, want one of helper in %q", got, tc.project)
			}
		})
	}
}
