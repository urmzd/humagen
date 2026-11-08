package platform_test

import (
	"errors"
	"testing"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
	"github.com/urmzd/orchagent/flow/connectors/cron"
	"github.com/urmzd/orchagent/flow/event"
)

// scheduled is one occurrence of the schedule nightly, filed in project.
func scheduled(id, agent, project string) event.Envelope {
	tg := inProject(id, agent, project)
	tg.Source = wire.SourceOf("cron", tg.Tenant)
	wire.SetMeta(&tg, "cron_schedule", "nightly")
	return tg
}

// Each run of a schedule is a session in the schedule's project, under the
// schedule: the project lists it with its other sessions, and the schedule
// lists its runs as a session lists its children. The same occurrence again
// joins the run and the session it already started.
func TestEachScheduledRunIsASessionInItsProject(t *testing.T) {
	p := start(t, call.Limits{})
	field(t, p)
	if err := p.Schedules.Add(cron.Schedule{Name: "nightly", Expression: "0 3 * * *", Agent: "helper", Text: "Sum up the day.", Tenant: tenant,
		Context: map[string]string{runs.MetaProject: "Field", "created_by": "ada"}}, platform.About{}); err != nil {
		t.Fatal(err)
	}
	r, err := p.Invoke(t.Context(), scheduled("nightly-1", "helper", "Field"))
	if err != nil {
		t.Fatal(err)
	}
	if r.Thread == r.ID {
		t.Fatalf("the run is still its own conversation: %+v", r)
	}
	th, err := p.Threads.Get(tenant, r.Thread)
	if err != nil {
		t.Fatal(err)
	}
	if th.Session == nil || th.Session.Parent != platform.ScheduleParent("nightly") || th.Session.Agent != "helper" ||
		th.Project != "Field" || th.Visibility != threads.Shared || th.CreatedBy != "ada" || th.Title != "hello" {
		t.Fatalf("the session = %+v (identity %+v)", th, th.Session)
	}
	inField, err := p.ListSessions(t.Context(), admin, "", "", "Field", "")
	if err != nil || len(inField) != 1 || inField[0].ID != th.ID {
		t.Fatalf("Field's sessions = %+v, %v", inField, err)
	}
	again, err := p.Invoke(t.Context(), scheduled("nightly-1", "helper", "Field"))
	if err != nil || again.ID != r.ID || again.Thread != r.Thread {
		t.Fatalf("the same occurrence again = %+v, %v", again, err)
	}
	if _, err := p.Invoke(t.Context(), scheduled("nightly-2", "helper", "Field")); err != nil {
		t.Fatal(err)
	}
	if inField, _ = p.ListSessions(t.Context(), admin, "", "", "Field", ""); len(inField) != 2 {
		t.Fatalf("after two occurrences Field has %d sessions", len(inField))
	}
	under, err := p.ListSessions(t.Context(), admin, platform.ScheduleParent("nightly"), "", "", "")
	if err != nil || len(under) != 2 {
		t.Fatalf("the schedule's sessions = %+v, %v", under, err)
	}
	if _, err := p.ListSessions(t.Context(), admin, platform.ScheduleParent("weekly"), "", "", ""); !errors.Is(err, threads.ErrNotFound) {
		t.Fatalf("the sessions of a schedule that does not exist = %v", err)
	}
	// "~" names the workspace itself: only what is filed there.
	if got, err := p.ListSessions(t.Context(), admin, "", "", "~", ""); err != nil || len(got) != 0 {
		t.Fatalf("the workspace's own sessions = %+v, %v", got, err)
	}
	if _, err := p.Invoke(t.Context(), scheduled("nightly-3", "helper", "")); err != nil {
		t.Fatal(err)
	}
	if got, err := p.ListSessions(t.Context(), admin, "", "", "~", ""); err != nil || len(got) != 1 {
		t.Fatalf("the workspace's own sessions = %+v, %v", got, err)
	}
	if got, _ := p.ListSessions(t.Context(), admin, "", "", "", ""); len(got) != 3 {
		t.Fatalf("every session = %d, want 3", len(got))
	}
}

// A thread the tag names is never believed for a schedule's run, and an
// occurrence admission refuses leaves no session behind.
func TestAScheduledRunNamesNoThreadOfItsOwnAndARefusalLeavesNoSession(t *testing.T) {
	p := start(t, call.Limits{})
	field(t, p)
	tg := scheduled("nightly-1", "helper", "Field")
	wire.SetMeta(&tg, runs.MetaThread, "someone-elses")
	r, err := p.Invoke(t.Context(), tg)
	if err != nil {
		t.Fatal(err)
	}
	if r.Thread == "someone-elses" {
		t.Fatal("a scheduled run joined the thread its tag named")
	}
	setAgents(t, p, "Initech", platform.AgentsInput{Enable: []string{}, Allow: []string{"lead"}})
	if _, err := p.Invoke(t.Context(), scheduled("nightly-2", "helper", "Initech")); !errors.Is(err, platform.ErrNotAvailable) {
		t.Fatalf("an occurrence the allow list refuses = %v", err)
	}
	if got, _ := p.ListSessions(t.Context(), admin, "", "", "Initech", ""); len(got) != 0 {
		t.Fatalf("a refused occurrence left sessions: %+v", got)
	}
}
