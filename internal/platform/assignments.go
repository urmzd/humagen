package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"

	"fmt"
	"strconv"

	"github.com/urmzd/fiber/pkg/connectors/cron"
	"github.com/urmzd/fiber/pkg/envelope"
	"github.com/urmzd/fiber/pkg/runtime"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/operations"

	"github.com/urmzd/humagen/pkg/runs"
)

const metaAssignment = "humagen.assignment"

func assignmentSchedule(tenant, id string) string {
	sum := sha256.Sum256([]byte(tenant + "\x00" + id))
	return "assignment-" + hex.EncodeToString(sum[:12])
}
func (p *Platform) SeesAssignment(who auth.Principal, a operations.Assignment) bool {
	seer := p.Seer(who)
	return seer.Place(Place{Project: a.Project, Thread: a.Thread}) && seer.Agent(a.Agent)
}
func (p *Platform) Assignments(who auth.Principal, limit int) []operations.Assignment {
	seer := p.Seer(who)
	return p.Operations.Assignments(who.Tenant, limit, func(a operations.Assignment) bool {
		return seer.Place(Place{Project: a.Project, Thread: a.Thread}) && seer.Agent(a.Agent)
	})
}
func (p *Platform) Assignment(who auth.Principal, id string) (operations.Assignment, error) {
	a, ok := p.Operations.Assignment(who.Tenant, id)
	if !ok || !p.SeesAssignment(who, a) {
		return operations.Assignment{}, operations.ErrNotFound
	}
	return a, nil
}
func (p *Platform) SetAssignment(ctx context.Context, who auth.Principal, a operations.Assignment, version int) (operations.Assignment, error) {
	if !p.SeesAssignment(who, a) {
		return operations.Assignment{}, operations.ErrNotFound
	}
	if _, err := p.Agents.Get(ctx, who.Tenant, a.Agent, 0); err != nil {
		return operations.Assignment{}, err
	}
	if err := p.MayPost(who, Place{Project: a.Project, Thread: a.Thread}); err != nil {
		return operations.Assignment{}, err
	}
	if a.Owner != who.Subject && !who.Can(auth.ProjectsGovern) {
		return operations.Assignment{}, ErrForbidden
	}
	if a.Thread != "" {
		th, err := p.Threads.Get(who.Tenant, a.Thread)
		if err != nil || th.Project != a.Project {
			return operations.Assignment{}, operations.ErrNotFound
		}
	}
	if err := p.Available(who.Tenant, a.Project, a.Agent); err != nil {
		return operations.Assignment{}, err
	}
	old, exists := p.Operations.Assignment(who.Tenant, a.ID)
	if exists {
		if old.Version != version {
			return operations.Assignment{}, operations.ErrConflict
		}
		if !p.SeesAssignment(who, old) {
			return operations.Assignment{}, operations.ErrNotFound
		}
		if old.Owner != who.Subject && !who.Can(auth.ProjectsGovern) {
			return operations.Assignment{}, ErrForbidden
		}
		if old.Cron != a.Cron || old.Agent != a.Agent || old.Project != a.Project || old.Goal != a.Goal || old.DoneWhen != a.DoneWhen {
			return operations.Assignment{}, fmt.Errorf("%w: create a new assignment to change its execution contract", operations.ErrInvalid)
		}
	}
	if a.Cron != "" {
		a.Schedule = assignmentSchedule(who.Tenant, a.ID)
		sc := cron.Schedule{Name: a.Schedule, Tenant: who.Tenant, Agent: a.Agent, Expression: a.Cron, Text: a.Goal + "\nDone when: " + a.DoneWhen, Meta: map[string]string{metaAssignment: a.ID, runs.MetaProject: a.Project, "created_by": who.Subject}}
		if err := sc.Validate(); err != nil {
			return operations.Assignment{}, err
		}
		if !exists {
			if err := p.Schedules.AddPaused(sc, About{CreatedAt: p.now(), WrittenBy: who.Subject}); err != nil {
				return operations.Assignment{}, err
			}
		}
		// Pause first, then save. A failed record write leaves no active orphan.
		if err := p.Schedules.SetPaused(who.Tenant, a.Schedule, true); err != nil {
			return operations.Assignment{}, err
		}
	}
	out, err := p.Operations.SetAssignment(who.Tenant, a, version, who.Subject, p.now())
	if err != nil {
		return out, err
	}
	if out.Schedule != "" && (out.State == operations.Watching || out.State == operations.Accepted) {
		if err := p.Schedules.SetPaused(who.Tenant, out.Schedule, false); err != nil {
			return out, err
		}
	}
	return out, nil
}

// RunAssignment reserves a deterministic run before entering the same turn path
// used by console messages. Repeated requests join the reserved run.
func (p *Platform) RunAssignment(ctx context.Context, who auth.Principal, id string) (runs.Run, error) {
	a, err := p.Assignment(who, id)
	if err != nil {
		return runs.Run{}, err
	}
	if a.Owner != who.Subject && !who.Can(auth.ProjectsGovern) {
		return runs.Run{}, ErrForbidden
	}
	if err = p.MayPost(who, Place{Project: a.Project, Thread: a.Thread}); err != nil {
		return runs.Run{}, err
	}
	if err = p.operationAdmission(who.Tenant, a.Agent); err != nil {
		return runs.Run{}, err
	}
	tagID := "assignment-" + assignmentSchedule(who.Tenant, id) + "-" + strconv.Itoa(a.Version)
	if a.State == operations.InProgress {
		if run, err := p.Runs.Get(who.Tenant, a.RunID); err == nil {
			return run, nil
		}
		// The stored reservation is authoritative after a crash between reserve
		// and admission. Recover the tag identity from its deterministic run ID.
		tagID = "assignment-" + assignmentSchedule(who.Tenant, id) + "-" + strconv.Itoa(a.Version-1)
	}
	after := ""
	if a.Thread != "" {
		turns, err := p.Runs.Conversation(who.Tenant, a.Thread)
		if err != nil || len(turns) == 0 {
			return runs.Run{}, fmt.Errorf("%w: the assigned conversation has no recorded turn", operations.ErrInvalid)
		}
		after = turns[len(turns)-1].ID
	}
	runID := runtime.RunID(who.Tenant, tagID)
	a, err = p.Operations.Reserve(who.Tenant, id, runID, a.Version, p.now())
	if err != nil {
		return runs.Run{}, err
	}
	run, err := p.Converse(ctx, who.Tenant, Turn{ID: tagID, Agent: a.Agent, Text: a.Goal + "\nDone when: " + a.DoneWhen, Project: a.Project, After: after, Actor: ActorOf(who), AssignmentID: a.ID, Sees: p.Seer(who).Agent})
	if err != nil {
		// An acceptance could have succeeded before a transport error. Never
		// infer terminal state from the error: retain the reservation for retry.
		return runs.Run{}, err
	}
	return run, nil
}
func (p *Platform) admitAssignment(tag envelope.Tag) error {
	id := tag.Meta[metaAssignment]
	if id == "" {
		return nil
	}
	a, ok := p.Operations.Assignment(tag.Tenant, id)
	if !ok {
		return operations.ErrNotFound
	}
	if a.Agent != tag.Agent || a.Project != runs.ProjectOf(tag) {
		return operations.ErrDenied
	}
	if tag.Origin == "cron" {
		if a.State != operations.Watching && a.State != operations.Accepted {
			return operations.ErrConflict
		}
		_, err := p.Operations.Reserve(tag.Tenant, id, runtime.RunID(tag.Tenant, tag.ID), a.Version, p.now())
		return err
	}
	if a.State != operations.InProgress || a.RunID != runtime.RunID(tag.Tenant, tag.ID) {
		return operations.ErrConflict
	}
	return nil
}
func (p *Platform) settleOperations() {
	// Reservations survive a crash; only a ledger-backed terminal run changes
	// them. The bounded store caps this recovery scan.
	for _, tenant := range p.Operations.Tenants() {
		if notices, err := p.Operations.Due(tenant, p.now()); err != nil {
			p.log.Error("platform: operation deadlines could not be kept", "error", err)
		} else {
			for _, n := range notices {
				p.operationEscalation(tenant, n.ID, n.Project, n.Thread, n.Run, n.Agent, n.Owner, n.Reason)
			}
		}
		for _, a := range p.Operations.Assignments(tenant, operations.MaxResults, func(a operations.Assignment) bool { return a.State == operations.InProgress }) {
			run, err := p.Runs.Get(tenant, a.RunID)
			if err == nil && run.Status.Terminal() {
				if err := p.Operations.Settle(tenant, run.ID, run.Status == runs.Completed, p.now()); err != nil {
					p.log.Error("platform: assignment recovery failed", "error", err)
				}
			}
		}
	}
}
