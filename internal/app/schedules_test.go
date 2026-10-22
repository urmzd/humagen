package app

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/fiber/pkg/connectors/cron"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/spec"
)

// fileWithRecap is a workspace that turns every agent on, with Field and one
// schedule in it.
func fileWithRecap(text string) spec.Workspace {
	return spec.Workspace{
		Agents: &spec.Agents{Enable: []string{spec.EveryAgent}},
		Projects: []spec.Project{{Name: "Field", AgentPosts: spec.AgentPostsReview, Schedules: []spec.Schedule{{
			Name: "field-recap", Cron: "41 8 * * 1", Agent: "warp-analyst", Text: text, Deliver: spec.DeliverProject,
		}}}},
	}
}

func applyAll(t *testing.T, p *platform.Platform, tenant string, w spec.Workspace) {
	t.Helper()
	if err := applyProjects(p, tenant, w); err != nil {
		t.Fatal(err)
	}
	if err := applySchedules(p, tenant, w); err != nil {
		t.Fatal(err)
	}
}

// A schedule the file declares is filed in its project and is the file's:
// the API cannot change or delete it, only pause it. An unchanged file writes
// nothing, a changed one keeps it paused, and one that leaves the file stays,
// the console's again.
func TestTheFilesSchedulesAreTheFiles(t *testing.T) {
	p := newProjectsPlatform(t)
	const tenant = "acme"
	applyAll(t, p, tenant, fileWithRecap("What changed in Field this week?"))
	sc, _, err := p.Schedules.Get(tenant, "field-recap")
	if err != nil {
		t.Fatal(err)
	}
	about := p.Schedules.About("field-recap")
	if sc.Meta[runs.MetaProject] != "Field" || sc.Meta[platform.MetaDeliver] != platform.DeliverProject || about.Managed != platform.ManagedFile || about.CreatedAt.IsZero() {
		t.Fatalf("the file's schedule = %+v, %+v", sc, about)
	}
	if mustGet(t, p, tenant, "Field").AgentPosts != spec.AgentPostsReview {
		t.Fatal("the file's agent_posts did not land")
	}

	applyAll(t, p, tenant, fileWithRecap("What changed in Field this week?"))
	if again := p.Schedules.About("field-recap"); !again.UpdatedAt.IsZero() {
		t.Fatalf("an unchanged file wrote the schedule again: %+v", again)
	}
	edit := sc
	edit.Text = "mine"
	if err := p.Schedules.Replace(edit, platform.About{}); !errors.Is(err, platform.ErrScheduleManaged) {
		t.Fatalf("changing the file's schedule = %v", err)
	}
	if err := p.Schedules.Remove(tenant, "field-recap"); !errors.Is(err, platform.ErrScheduleManaged) {
		t.Fatalf("deleting the file's schedule = %v", err)
	}
	if err := p.Schedules.SetPaused(tenant, "field-recap", true); err != nil {
		t.Fatalf("pausing the file's schedule: %v", err)
	}

	applyAll(t, p, tenant, fileWithRecap("What changed in Field?"))
	if sc, paused, _ := p.Schedules.Get(tenant, "field-recap"); sc.Text != "What changed in Field?" || !paused {
		t.Fatalf("after the file changed = %+v, paused %v", sc, paused)
	}

	w := fileWithRecap("")
	w.Projects[0].Schedules = nil
	applyAll(t, p, tenant, w)
	if left := p.Schedules.About("field-recap"); left.Managed != "" {
		t.Fatalf("a schedule that left the file = %+v", left)
	}
	if err := p.Schedules.Remove(tenant, "field-recap"); err != nil {
		t.Fatalf("deleting a schedule the file let go of: %v", err)
	}
}

// A schedule the console made under a name the file declares is taken over,
// and a name another workspace holds fails the start, naming the schedule.
func TestTheFileTakesOverAConsoleSchedule(t *testing.T) {
	p := newProjectsPlatform(t)
	if err := p.Schedules.Add(cron.Schedule{Name: "field-recap", Expression: "0 9 * * *", Agent: "lead", Text: "x", Tenant: "acme",
		Meta: map[string]string{"created_by": "ada"}}, platform.About{CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	applyAll(t, p, "acme", fileWithRecap("What changed?"))
	if sc, _, _ := p.Schedules.Get("acme", "field-recap"); sc.Agent != "warp-analyst" || p.Schedules.About("field-recap").Managed != platform.ManagedFile {
		t.Fatalf("the console's schedule was not taken over: %+v", sc)
	}
	if err := applyProjects(p, "globex", fileWithRecap("x")); err != nil {
		t.Fatal(err)
	}
	err := applySchedules(p, "globex", fileWithRecap("x"))
	if err == nil || !strings.Contains(err.Error(), "workspace.projects[0].schedules[0]") || !errors.Is(err, platform.ErrScheduleExists) {
		t.Fatalf("another workspace's name = %v", err)
	}
}

// A schedule whose agent is not available in its project fails the start,
// rather than every occurrence later.
func TestAFileScheduleNeedsItsAgentAvailable(t *testing.T) {
	p := newProjectsPlatform(t)
	w := fileWithRecap("x")
	w.Agents = &spec.Agents{Enable: []string{"lead"}}
	if err := applyProjects(p, "acme", w); err != nil {
		t.Fatal(err)
	}
	err := applySchedules(p, "acme", w)
	if err == nil || !strings.Contains(err.Error(), "workspace.projects[0].schedules[0].agent") || !errors.Is(err, platform.ErrNotAvailable) {
		t.Fatalf("an agent Field does not have = %v", err)
	}
}
