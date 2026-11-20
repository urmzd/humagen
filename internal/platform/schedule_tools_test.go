package platform_test

import (
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/saige/flow/connectors/cron"
	"github.com/urmzd/saige/flow/event"
	"github.com/urmzd/saige/flow/runtime"
	"github.com/urmzd/saige/flow/turn/native"

	"github.com/urmzd/humagen/internal/httpapi"
	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
)

// An agent in a conversation sets up a schedule for the person it works
// for, through the API's own routes as that person: the schedule is filed in
// the conversation's project and owned by them, the tool answers with its
// next runs in UTC, and running it once now acts for them. Only the
// schedules of the conversation's project are reached from it, and what the
// person may not do, the agent may not either.
func TestAnAgentSchedulesWorkForThePersonItWorksFor(t *testing.T) {
	pol, err := auth.ParsePolicy("schedules", `role viewer { allow read on * }
role poster extends viewer { allow write on runs, projects }
role operator extends poster { allow write on schedules }
grant operator to subject "ada"
grant poster to subject "bo"`)
	if err != nil {
		t.Fatal(err)
	}
	p := startWith(t, platform.Config{Policy: pol})
	// The server installs the way to its routes the tools call through.
	_ = (&httpapi.Server{P: p, Authn: &httpapi.Authn{}, Log: slog.New(slog.DiscardHandler)}).Handler()
	ctx := t.Context()
	for _, name := range []string{"Field", "Other"} {
		if _, err := p.CreateProject(ctx, tenant, projects.Writer{By: "ada"}, platform.ProjectInput{Name: name}); err != nil {
			t.Fatal(err)
		}
	}
	turn := func(id, who string) runtime.Request {
		t.Helper()
		r, err := p.Converse(ctx, tenant, platform.Turn{ID: id, Agent: "lead", Text: "Check for new recordings every 15 minutes", Project: "Field", Actor: event.Actor{User: who}})
		if err != nil {
			t.Fatal(err)
		}
		await(t, p, r.ID, runs.Completed)
		return runtime.Request{RunID: r.ID, Tenant: tenant, Definition: native.Revision{Spec: native.Spec{Name: "lead"}, Version: wire.VersionOf(1)}}
	}
	req := turn("api-sched-ada", "ada")
	view := func(out string) platform.ScheduleToolView {
		t.Helper()
		var v platform.ScheduleToolView
		if err := json.Unmarshal([]byte(out), &v); err != nil {
			t.Fatalf("%s: %v", out, err)
		}
		return v
	}

	before := time.Now().UTC()
	out, _, err := p.UseTool(ctx, req, platform.ToolCreateSchedule, map[string]any{
		"name": "plaud-new", "cron": "*/15 * * * *", "check": "Look for new Plaud recordings. Answer NONE when there are none.",
	})
	if err != nil {
		t.Fatal(err)
	}
	v := view(out)
	if v.Name != "plaud-new" || v.Every != "every 15 minutes" || v.Timezone != "UTC" || v.Agent != "lead" || v.Project != "Field" || v.Paused {
		t.Fatalf("create_schedule answered %+v", v)
	}
	next, err := time.Parse(time.RFC3339, v.NextRun)
	if err != nil || !strings.HasSuffix(v.NextRun, "Z") || next.Before(before) || next.After(before.Add(15*time.Minute)) || len(v.Next) != 3 || v.Next[0] != v.NextRun {
		t.Fatalf("the next run is not said plainly in UTC: %+v %v", v, err)
	}
	if !strings.Contains(v.Say, v.NextRun) || v.PostsIn != "keeps each answer in its own session" {
		t.Fatalf("what to tell the person: %+v", v)
	}
	sc, _, err := p.Schedules.Get(tenant, "plaud-new")
	if err != nil || sc.Context["created_by"] != "ada" || sc.Context[runs.MetaProject] != "Field" || sc.Agent != "lead" {
		t.Fatalf("the schedule is not ada's, in Field: %+v %v", sc, err)
	}

	// Run once now: a run of the schedule, acting for ada, who wrote it.
	out, _, err = p.UseTool(ctx, req, platform.ToolRunSchedule, map[string]any{"name": "plaud-new"})
	if err != nil {
		t.Fatal(err)
	}
	var ran struct {
		Run      string `json:"run"`
		Schedule string `json:"schedule"`
	}
	if err := json.Unmarshal([]byte(out), &ran); err != nil || ran.Run == "" || ran.Schedule != "plaud-new" {
		t.Fatalf("run_schedule answered %s: %v", out, err)
	}
	once := await(t, p, ran.Run, runs.Completed)
	if once.Origin != runs.OriginManual || once.Actor.User != "ada" || once.Project != "Field" {
		t.Fatalf("the run once now: %+v", once)
	}

	// Pause and resume.
	out, _, err = p.UseTool(ctx, req, platform.ToolPauseSchedule, map[string]any{"name": "plaud-new"})
	if err != nil || !view(out).Paused || view(out).NextRun != "" {
		t.Fatalf("pause_schedule: %s %v", out, err)
	}
	out, _, err = p.UseTool(ctx, req, platform.ToolResumeSchedule, map[string]any{"name": "plaud-new"})
	if err != nil || view(out).Paused || view(out).NextRun == "" {
		t.Fatalf("resume_schedule: %s %v", out, err)
	}
	out, _, err = p.UseTool(ctx, req, platform.ToolUpdateSchedule, map[string]any{"name": "plaud-new", "cron": "0 9 * * 1-5", "check": "Look again."})
	if err != nil || view(out).Every != "weekdays at 09:00 UTC" {
		t.Fatalf("update_schedule: %s %v", out, err)
	}

	// A schedule of another project is not found from this conversation,
	// though ada could reach it in the console.
	if err := p.Schedules.Add(cron.Schedule{Name: "elsewhere", Expression: "0 9 * * *", Agent: "lead", Text: "x", Tenant: tenant, Context: map[string]string{runs.MetaProject: "Other", "created_by": "ada"}}, platform.About{CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{platform.ToolRunSchedule, platform.ToolPauseSchedule, platform.ToolResumeSchedule, platform.ToolUpdateSchedule} {
		if _, _, err := p.UseTool(ctx, req, name, map[string]any{"name": "elsewhere", "cron": "* * * * *", "check": "x"}); !errors.Is(err, platform.ErrScheduleNotFound) {
			t.Errorf("%s reached another project's schedule: %v", name, err)
		}
	}

	// bo may talk in Field but not manage schedules, and neither may an
	// agent working for bo.
	asBo := turn("api-sched-bo", "bo")
	if _, _, err := p.UseTool(ctx, asBo, platform.ToolCreateSchedule, map[string]any{"name": "bos", "cron": "* * * * *", "check": "x"}); err == nil || !strings.Contains(err.Error(), "403") {
		t.Errorf("an agent made a schedule for someone who may not: %v", err)
	}
	if _, _, err := p.UseTool(ctx, asBo, platform.ToolRunSchedule, map[string]any{"name": "plaud-new"}); err == nil || !strings.Contains(err.Error(), "403") {
		t.Errorf("an agent ran a schedule for someone who may not: %v", err)
	}
	if _, _, err := p.Schedules.Get(tenant, "bos"); !errors.Is(err, platform.ErrScheduleNotFound) {
		t.Errorf("bo's schedule exists: %v", err)
	}
}
