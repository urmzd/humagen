package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"fmt"
	"strconv"
	"time"

	"github.com/urmzd/humagen/fiber/pkg/connectors/cron"
	"github.com/urmzd/humagen/fiber/pkg/event"
	"github.com/urmzd/humagen/fiber/pkg/runtime"
	"github.com/urmzd/humagen/pkg/audit"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/operations"
	"github.com/urmzd/humagen/pkg/projects"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/runs"
)

const metaAssignment = operations.MetaAssignment

func assignmentSchedule(tenant, id string) string {
	sum := sha256.Sum256([]byte(tenant + "\x00" + id))
	return "assignment-" + hex.EncodeToString(sum[:12])
}
func (p *Platform) SeesAssignment(who auth.Principal, a operations.Assignment) bool {
	seer := p.Seer(who)
	return seer.Project(a.Project) && seer.Place(Place{Project: a.Project, Thread: a.Thread}) && seer.Agent(a.Agent)
}
func (p *Platform) Assignments(who auth.Principal, limit int, agentFilter ...string) []operations.Assignment {
	seer := p.Seer(who)
	agent := ""
	if len(agentFilter) > 0 {
		agent = agentFilter[0]
	}
	out := p.Operations.Assignments(who.Tenant, limit, func(a operations.Assignment) bool {
		return (agent == "" || a.Agent == agent) && seer.Project(a.Project) && seer.Place(Place{Project: a.Project, Thread: a.Thread}) && seer.Agent(a.Agent)
	})
	for i, a := range out {
		out[i] = p.shownAssignment(who, a)
	}
	return out
}
func (p *Platform) Assignment(who auth.Principal, id string) (operations.Assignment, error) {
	a, ok := p.Operations.Assignment(who.Tenant, id)
	if !ok || !p.SeesAssignment(who, a) {
		return operations.Assignment{}, operations.ErrNotFound
	}
	return p.shownAssignment(who, a), nil
}
func (p *Platform) SetAssignment(ctx context.Context, who auth.Principal, a operations.Assignment, version int) (result operations.Assignment, resultErr error) {
	p.admission.Lock()
	defer p.admission.Unlock()
	if !p.SeesAssignment(who, a) {
		return operations.Assignment{}, operations.ErrNotFound
	}
	if a.Cron != "" && a.Thread != "" {
		return operations.Assignment{}, fmt.Errorf("%w: standing assignments cannot target an existing conversation; create project-scoped work instead", operations.ErrInvalid)
	}
	if a.Cron != "" && !p.standingOwner(who.Tenant, a.Project, a.Owner) {
		return operations.Assignment{}, fmt.Errorf("%w: standing work needs an owner with current project membership", operations.ErrDenied)
	}
	if _, err := p.Catalog().Get(ctx, who.Tenant, a.Agent, 0); err != nil {
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
	if err := p.prepareSourceAssignment(ctx, who, &a, old, exists); err != nil {
		return operations.Assignment{}, err
	}
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
		if old.Trigger != a.Trigger || old.Source != a.Source || old.Cron != a.Cron || old.Agent != a.Agent || old.Project != a.Project || old.Thread != a.Thread || old.Goal != a.Goal || old.DoneWhen != a.DoneWhen {
			return operations.Assignment{}, fmt.Errorf("%w: create a new assignment to change its execution contract", operations.ErrInvalid)
		}
	}
	if exists && old.DeadlineMissed && !a.DueAt.Equal(old.DueAt) && (a.DueAt.IsZero() || a.DueAt.After(p.now())) && old.RunID != "" {
		if run, err := p.Runs.Get(who.Tenant, old.RunID); err == nil {
			switch {
			case !run.Status.Terminal():
				a.State = operations.InProgress
			case run.Status == runs.Completed:
				a.State = operations.Result
			default:
				a.State = operations.Blocked
				a.Blocker = "The recorded run did not complete successfully."
			}
		}
	}
	if a.State == operations.InProgress {
		if !exists || old.RunID == "" {
			return operations.Assignment{}, operations.ErrConflict
		}
		run, err := p.Runs.Get(who.Tenant, old.RunID)
		if err != nil || run.Status.Terminal() {
			return operations.Assignment{}, operations.ErrConflict
		}
	}
	if a.State == operations.Watching && a.Trigger != operations.CronTrigger && a.Trigger != operations.SourceTrigger {
		return operations.Assignment{}, operations.ErrInvalid
	}
	if (a.State == operations.Watching || a.State == operations.InProgress) && !a.DueAt.IsZero() && !a.DueAt.After(p.now()) {
		return operations.Assignment{}, fmt.Errorf("%w: review the expired assignment deadline before continuing", operations.ErrInvalid)
	}
	if a.State == operations.Result {
		if !exists || old.RunID == "" {
			return operations.Assignment{}, operations.ErrConflict
		}
		run, err := p.Runs.Get(who.Tenant, old.RunID)
		if err != nil || run.Status != runs.Completed {
			return operations.Assignment{}, operations.ErrConflict
		}
	}
	attempt, err := p.BeginAudit(who, audit.Change{Action: "assignment.save", Resource: "assignment", ID: a.ID, Scope: "project", Project: a.Project, Thread: a.Thread, Agent: a.Agent, Before: map[string]string{"state": string(old.State), "due_at": old.DueAt.Format(time.RFC3339Nano)}, After: map[string]string{"state": string(a.State), "standing": strconv.FormatBool(a.Cron != ""), "due_at": a.DueAt.Format(time.RFC3339Nano)}})
	if err != nil {
		return operations.Assignment{}, err
	}
	defer func() { p.finishOperationsAudit(attempt, resultErr) }()
	if a.Cron != "" {
		a.Schedule = assignmentSchedule(who.Tenant, a.ID)
		sc := cron.Schedule{Name: a.Schedule, Tenant: who.Tenant, Agent: a.Agent, Expression: a.Cron, Text: a.Goal + "\nDone when: " + a.DoneWhen, Context: map[string]string{metaAssignment: a.ID, runs.MetaProject: a.Project, "created_by": who.Subject}}
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
	return p.shownAssignment(who, out), nil
}

// RunAssignment reserves a deterministic run before entering the same turn path
// used by console messages. Repeated requests join the reserved run.
func (p *Platform) RunAssignment(ctx context.Context, who auth.Principal, id string) (result runs.Run, resultErr error) {
	a, err := p.Assignment(who, id)
	if err != nil {
		return runs.Run{}, err
	}
	raw, _ := p.Operations.Assignment(who.Tenant, id)
	a = raw
	if a.Owner != who.Subject && !who.Can(auth.ProjectsGovern) {
		return runs.Run{}, ErrForbidden
	}
	if err = p.MayPost(who, Place{Project: a.Project, Thread: a.Thread}); err != nil {
		return runs.Run{}, err
	}
	if err = p.operationAdmission(who.Tenant, a.Agent); err != nil {
		return runs.Run{}, err
	}
	if a.RunID != "" {
		if active, err := p.Runs.Get(who.Tenant, a.RunID); err == nil && !active.Status.Terminal() {
			if !p.Seer(who).Run(active) {
				return runs.Run{}, operations.ErrNotFound
			}
			return active, nil
		}
	}
	if !a.DueAt.IsZero() && !a.DueAt.After(p.now()) {
		return runs.Run{}, fmt.Errorf("%w: review the expired assignment deadline before starting new work", operations.ErrInvalid)
	}

	attempt, auditErr := p.BeginAudit(who, audit.Change{Action: "assignment.run", Resource: "assignment", ID: a.ID, Scope: "project", Project: a.Project, Thread: a.Thread, Agent: a.Agent, Before: map[string]string{"state": string(a.State)}, After: map[string]string{"action": "request_run"}})
	if auditErr != nil {
		return runs.Run{}, auditErr
	}
	defer func() { p.finishOperationsAudit(attempt, resultErr) }()
	tagID := "assignment-" + assignmentSchedule(who.Tenant, id) + "-" + strconv.Itoa(a.Version)
	if a.State == operations.InProgress {
		if run, err := p.Runs.Get(who.Tenant, a.RunID); err == nil {
			return run, nil
		}
		if a.Trigger == operations.SourceTrigger && a.TagID != "" && a.RunID == runtime.RunID(who.Tenant, a.TagID) {
			// The source cursor and exact trigger identity were committed together.
			// Retry that invocation, never reinterpret it as a new console turn.
			return p.Invoke(ctx, wire.NewEvent(wire.Raise{ID: a.TagID, Tenant: who.Tenant, Agent: a.Agent, Origin: runs.OriginAssignment, Context: map[string]string{metaAssignment: a.ID, metaAssignmentSource: strconv.FormatUint(a.SourceVersion, 10), runs.MetaProject: a.Project}, Text: a.Goal + "\nDone when: " + a.DoneWhen}))
		}
		manualTagID := "assignment-" + assignmentSchedule(who.Tenant, id) + "-" + strconv.Itoa(a.Version-1)
		if a.Cron != "" && a.RunID != runtime.RunID(who.Tenant, manualTagID) {
			invocation, ok := p.Operations.Invocation(who.Tenant, a.ID)
			if !ok {
				if err := p.Operations.BlockAssignment(who.Tenant, a.ID, "This older clock reservation has no exact invocation record. Its owner must review it before starting new work.", p.now()); err != nil {
					return runs.Run{}, err
				}
				return runs.Run{}, operations.ErrConflict
			}
			return p.Invoke(ctx, invocation)
		}
		// The stored reservation is authoritative after a crash between reserve
		// and admission. Recover the tag identity from its deterministic run ID.
		tagID = "assignment-" + assignmentSchedule(who.Tenant, id) + "-" + strconv.Itoa(a.Version-1)
	}
	after := ""
	if a.Thread != "" {
		th, err := p.Threads.Get(who.Tenant, a.Thread)
		if err != nil || th.Project != a.Project {
			return runs.Run{}, operations.ErrConflict
		}
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
func (p *Platform) admitAssignment(tag event.Envelope) error {
	id := wire.Meta(tag)[metaAssignment]
	if id == "" {
		return nil
	}
	a, ok := p.Operations.Assignment(tag.Tenant, id)
	if !ok {
		return operations.ErrNotFound
	}
	if a.Cron != "" && (a.Thread != "" || !p.standingOwner(tag.Tenant, a.Project, a.Owner)) {
		if a.Schedule != "" {
			if err := p.Schedules.SetPaused(tag.Tenant, a.Schedule, true); err != nil {
				return err
			}
		}
		if err := p.Operations.BlockAssignment(tag.Tenant, a.ID, "The assignment owner or conversation no longer has the access this standing contract requires.", p.now()); err != nil {
			return err
		}
		return operations.ErrDenied
	}
	if a.Agent != tag.Agent || a.Project != runs.ProjectOf(tag) {
		return operations.ErrDenied
	}
	if wire.Origin(tag) == runs.OriginAssignment {
		if a.Trigger != operations.SourceTrigger || a.Thread != "" || !p.standingOwner(tag.Tenant, a.Project, a.Owner) || !p.assignmentSourceGrant(context.Background(), tag.Tenant, a.Project, a.Agent, a.Source) {
			return operations.ErrDenied
		}
		version, err := strconv.ParseUint(wire.Meta(tag)[metaAssignmentSource], 10, 64)
		if err != nil {
			return operations.ErrInvalid
		}
		current, fresh := p.assignmentSourceVersion(tag.Tenant, a.Source)
		if !fresh || version != current {
			return operations.ErrDenied
		}
		_, err = p.Operations.ReserveSource(tag.Tenant, id, runtime.RunID(tag.Tenant, tag.ID), tag.ID, a.Version, version, p.now())
		return err
	}
	if wire.Origin(tag) == "cron" {
		if a.State != operations.Watching && a.State != operations.Accepted && (a.State != operations.InProgress || a.RunID != runtime.RunID(tag.Tenant, tag.ID)) {
			return operations.ErrConflict
		}
		// Admission receives the composed request. Keep the original ask so a
		// retry passes through the same composer exactly once.
		invocation := tag
		wire.SetText(&invocation, wire.Meta(tag)[runs.MetaAsk])
		_, err := p.Operations.ReserveInvocation(tag.Tenant, id, invocation, a.Version, p.now())
		return err
	}
	if a.State != operations.InProgress || a.RunID != runtime.RunID(tag.Tenant, tag.ID) {
		return operations.ErrConflict
	}
	return nil
}
func (p *Platform) settleOperations(ctx context.Context) {
	// Reservations survive a crash; only a ledger-backed terminal run changes
	// them. The bounded store caps this recovery scan.
	for _, tenant := range p.Operations.Tenants() {
		p.reconcileOperationAttention(tenant)
		p.pollSourceAssignments(ctx, tenant)
		if notices, err := p.Operations.Due(tenant, p.now()); err != nil {
			p.log.Error("platform: operation deadlines could not be kept", "error", err)
		} else {
			for _, n := range notices {
				if n.Kind == "assignment" {
					if a, ok := p.Operations.Assignment(tenant, n.ID); ok && a.Schedule != "" {
						if err := p.Schedules.SetPaused(tenant, a.Schedule, true); err != nil {
							p.log.Error("platform: assignment attention schedule pause could not be kept", "error", err)
						}
					}
				}
				p.operationEscalation(tenant, n.Kind, n.ID, n.Project, n.Thread, n.Run, n.Agent, n.Owner, n.Reason)
			}
		}
		after := ""
		if v, ok := p.assignmentSettlementCursor.Load(tenant); ok {
			after = v.(string)
		}
		pending, next := p.Operations.AssignmentPage(tenant, after, operations.MaxResults, func(a operations.Assignment) bool {
			return a.State == operations.InProgress || (a.State == operations.NeedsPerson && a.DeadlineMissed && a.RunID != "")
		})
		p.assignmentSettlementCursor.Store(tenant, next)
		for _, a := range pending {
			run, err := p.Runs.Get(tenant, a.RunID)
			if errors.Is(err, runs.ErrNotFound) {
				p.recoverAssignmentInvocation(ctx, tenant, a)
				continue
			}
			if err == nil && run.Status.Terminal() {
				if err := p.Operations.Settle(tenant, run.ID, run.Status == runs.Completed, p.now()); err != nil {
					p.log.Error("platform: assignment recovery failed", "error", err)
				}
			}
		}
	}
}

// AssignmentSchedule reports schedules whose only mutation surface is their
// versioned assignment. The ordinary schedule routes may not bypass its owner.
func AssignmentSchedule(sc cron.Schedule) bool { return sc.Context[metaAssignment] != "" }

func (p *Platform) standingOwner(tenant, project, owner string) bool {
	// A clock has no current sign-in or bearer scope. Only current explicit
	// policy grants and ordinary project membership may authorize its owner.
	reader := ReaderOf(p.byPolicy(tenant, owner))
	reader.Govern = false
	return p.Projects.Tree(tenant).Resolve(project, reader).Role.AtLeast(projects.Member)
}
