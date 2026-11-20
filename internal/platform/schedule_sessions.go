package platform

import (
	"context"
	"errors"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
	"github.com/urmzd/saige/flow/connectors/cron"
	"github.com/urmzd/saige/flow/event"
	"github.com/urmzd/saige/flow/session"
)

// ScheduleParent is the parent of every session a schedule's runs are filed
// in: schedule://<name>. Sessions under it are the schedule's runs, newest
// first, so a schedule lists its runs as any session lists its children.
func ScheduleParent(name string) string { return "schedule://" + name }

// scheduledOrigin reports whether a tag is a schedule's: a cron occurrence or
// a person's run-now. Only those two carry a schedule's name the index
// believes (runs.Record).
func scheduledOrigin(tag event.Envelope) bool {
	o := wire.Origin(tag)
	return (o == cron.Name || o == runs.OriginManual) && wire.SurfaceOf(tag).CronSchedule != ""
}

// fileInScheduleSession files a scheduled run in a session of its own, in
// the schedule's project, so each run is a session there: one per
// occurrence, keyed (schedule://<name>, agent, occurrence), so the same
// occurrence delivered again finds the same session. Its readers are the
// project's, as the run's are, and it belongs to who wrote what the schedule
// runs. It is made once the run is admitted, so a refused occurrence leaves
// no empty session. A session that cannot be made leaves the run its own
// conversation, as every scheduled run was before, rather than lose it.
//
// A thread named in the tag is never believed: the platform names it here.
func (p *Platform) fileInScheduleSession(ctx context.Context, tag event.Envelope, project string) event.Envelope {
	wire.DeleteMeta(&tag, runs.MetaThread)
	name := wire.SurfaceOf(tag).CronSchedule
	agent := wire.AgentOf(tag)
	ch, err := p.Projects.Channel(tag.Tenant, project, projects.General)
	if err != nil || ch.Archived {
		p.log.Warn("platform: a scheduled run stays its own conversation", "schedule", name, "project", project, "error", err)
		return tag
	}
	rec, err := p.Sessions.Open(ctx, tag.Tenant, session.Open{Parent: ScheduleParent(name), Agent: agent, Label: tag.ID, Channel: wire.Channel(tag.Tenant)})
	if err != nil {
		p.log.Warn("platform: a scheduled run stays its own conversation", "schedule", name, "error", err)
		return tag
	}
	// A schedule removed since it fired belongs to nobody.
	_, by, _ := p.Schedules.Authored(tag.Tenant, name)
	title := name
	if ask := wire.Meta(tag)[runs.MetaAsk]; ask != "" {
		title = firstWords(ask, threads.MaxTitle)
	}
	th := threads.Thread{
		ID: rec.ID, Project: project, Channel: ch.Name, Title: title, Visibility: threads.Shared,
		CreatedBy: by, Default: agent, Agents: []string{agent},
		Session: &threads.SessionIdentity{Agent: agent, Parent: rec.Key.Parent, Label: rec.Key.Label},
	}
	if _, err := p.Threads.Create(tag.Tenant, th, p.now()); err != nil && !errors.Is(err, threads.ErrExists) {
		p.log.Warn("platform: a scheduled run stays its own conversation", "schedule", name, "error", err)
		return tag
	}
	wire.SetMeta(&tag, runs.MetaThread, rec.ID)
	return tag
}
