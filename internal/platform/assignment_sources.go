package platform

import (
	"context"
	"fmt"

	"strconv"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/operations"
	"github.com/urmzd/humagen/pkg/runs"
)

const metaAssignmentSource = "humagen.assignment.source_version"

// RegisterAssignmentSources connects resident durable source versions to the
// existing reconciliation ticker. A missing provider allows no source trigger.
func (p *Platform) RegisterAssignmentSources(provider operations.SourceChanges) error {
	if provider == nil {
		return operations.ErrInvalid
	}
	if _, loaded := p.assignmentSources.LoadOrStore("changes", provider); loaded {
		return operations.ErrConflict
	}
	return nil
}
func (p *Platform) assignmentSourceVersion(tenant, name string) (uint64, bool) {
	v, ok := p.assignmentSources.Load("changes")
	if !ok {
		return 0, false
	}
	return v.(operations.SourceChanges).SourceChangeVersion(tenant, name)
}
func (p *Platform) assignmentSourceGrant(ctx context.Context, tenant, project, agent, source string) bool {
	rev, err := p.Catalog().Get(ctx, tenant, agent, 0)
	if err != nil {
		return false
	}
	for _, ref := range rev.Spec.Sources {
		if ref.URI == sourceURI(source) {
			return true
		}
	}
	for _, s := range p.knowledgeAtPlace(tenant, project, "general") {
		if s.Name == source {
			return true
		}
	}
	return false
}
func (p *Platform) prepareSourceAssignment(ctx context.Context, who auth.Principal, a *operations.Assignment, old operations.Assignment, exists bool) error {
	if a.Trigger == "" {
		if a.Cron != "" {
			a.Trigger = operations.CronTrigger
		} else {
			a.Trigger = operations.ManualTrigger
		}
	}
	if exists && old.Trigger == operations.SourceTrigger && a.Source == "" && !who.Can(auth.KnowledgeRead) {
		a.Source = old.Source
		a.Trigger = old.Trigger
	}
	if exists && a.State == operations.Cancelled {
		return nil
	}
	if a.Trigger != operations.SourceTrigger {
		return nil
	}
	if !who.Can(auth.KnowledgeRead) && !exists {
		return ErrForbidden
	}
	if !p.standingOwner(who.Tenant, a.Project, a.Owner) {
		return operations.ErrDenied
	}
	if a.Cron != "" || a.Thread != "" || !p.assignmentSourceGrant(ctx, who.Tenant, a.Project, a.Agent, a.Source) {
		return fmt.Errorf("%w: source-change work requires an already granted project source and no existing conversation", operations.ErrInvalid)
	}
	if !exists {
		version, fresh := p.assignmentSourceVersion(who.Tenant, a.Source)
		if !fresh {
			return fmt.Errorf("%w: retained source change tracking is unavailable", operations.ErrDenied)
		}
		a.SourceVersion = version
	}
	return nil
}
func (p *Platform) pollSourceAssignments(ctx context.Context, tenant string) {
	after := ""
	if v, ok := p.assignmentSourceCursor.Load(tenant); ok {
		after = v.(string)
	}
	watchers, next, err := p.Operations.AssignmentPage(tenant, after, operations.MaxResults, func(a operations.Assignment) bool {
		return a.Trigger == operations.SourceTrigger && (a.State == operations.Watching || a.State == operations.Accepted)
	})
	if err != nil {
		p.log.Error("platform: source assignments could not be read", "error", err)
		return
	}
	p.assignmentSourceCursor.Store(tenant, next)
	for _, a := range watchers {
		version, fresh := p.assignmentSourceVersion(tenant, a.Source)
		if !fresh || !p.standingOwner(tenant, a.Project, a.Owner) || !p.assignmentSourceGrant(ctx, tenant, a.Project, a.Agent, a.Source) {
			if err := p.Operations.BlockAssignment(tenant, a.ID, "A retained source or the assignment owner's project access needs review.", p.now()); err != nil {
				p.log.Error("platform: source-trigger refusal could not be kept", "error", err)
			}
			continue
		}
		if p.operationAdmission(tenant, a.Agent) != nil {
			continue
		}
		if version == a.SourceVersion {
			continue
		}
		tagID := "assignment-source-" + assignmentSchedule(tenant, a.ID) + "-" + strconv.FormatUint(version, 10)
		_, err := p.Invoke(ctx, wire.NewEvent(wire.Raise{ID: tagID, Tenant: tenant, Agent: a.Agent, Origin: runs.OriginAssignment, Context: map[string]string{metaAssignment: a.ID, metaAssignmentSource: strconv.FormatUint(version, 10), runs.MetaProject: a.Project}, Text: a.Goal + "\nDone when: " + a.DoneWhen}))
		if err != nil {
			if blockErr := p.Operations.BlockAssignment(tenant, a.ID, "The source-change run could not be admitted. A person must review it before resuming.", p.now()); blockErr != nil {
				p.log.Error("platform: source-trigger admission refusal could not be kept", "error", blockErr)
			}
		}
	}
}
func (p *Platform) shownAssignment(who auth.Principal, a operations.Assignment) operations.Assignment {
	a.RunStatus = ""
	if a.RunID != "" {
		if run, err := p.Runs.Get(who.Tenant, a.RunID); err == nil && p.Seer(who).Run(run) {
			a.RunStatus = string(run.Status)
		}
	}

	if !who.Can(auth.KnowledgeRead) {
		a.Source = ""
		a.SourceVersion = 0
	}
	return a
}
