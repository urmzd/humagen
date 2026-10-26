package platform

import (
	"context"
	"errors"
	"strconv"

	"github.com/urmzd/fiber/pkg/envelope"
	"github.com/urmzd/fiber/pkg/runtime"
	"github.com/urmzd/humagen/pkg/operations"
	"github.com/urmzd/humagen/pkg/runs"
)

func (p *Platform) recoverAssignmentInvocation(ctx context.Context, tenant string, a operations.Assignment) {
	var tag envelope.Tag
	if invocation, ok := p.Operations.Invocation(tenant, a.ID); ok {
		if invocation.Meta[metaAssignment] != a.ID || runs.ProjectOf(invocation) != a.Project || invocation.Agent != a.Agent || runtime.RunID(tenant, invocation.ID) != a.RunID {
			p.blockUnknownAssignment(tenant, a.ID, "The reserved invocation does not match its assignment. Its owner must review it.")
			return
		}
		tag = invocation
	} else if a.Trigger == operations.SourceTrigger && a.TagID != "" && a.RunID == runtime.RunID(tenant, a.TagID) {
		tag = envelope.Tag{ID: a.TagID, Tenant: tenant, Agent: a.Agent, Origin: runs.OriginAssignment, Text: a.Goal + "\nDone when: " + a.DoneWhen, Meta: map[string]string{metaAssignment: a.ID, metaAssignmentSource: strconv.FormatUint(a.SourceVersion, 10), runs.MetaProject: a.Project}}
	} else if a.Cron != "" {
		// Legacy manual reservations have a known deterministic console identity.
		// An old clock reservation without its exact tag must never be guessed.
		manual := "assignment-" + assignmentSchedule(tenant, a.ID) + "-" + strconv.Itoa(a.Version-1)
		if a.RunID != runtime.RunID(tenant, manual) {
			p.blockUnknownAssignment(tenant, a.ID, "This older clock reservation has no exact invocation record. Its owner must review it before starting new work.")
		}
		return
	} else {
		return
	}
	// Invoke rechecks the current owner, placement, source dependency, pause and
	// budget policy under the ordinary admission lock. No new runtime path exists.
	if _, err := p.Invoke(ctx, tag); err != nil {
		if errors.Is(err, operations.ErrDenied) || errors.Is(err, ErrForbidden) || errors.Is(err, ErrProjectDeleted) || errors.Is(err, ErrNotAvailable) {
			p.blockUnknownAssignment(tenant, a.ID, "The reserved invocation no longer meets its current access or dependency requirements. Its owner must review it.")
		} else if !errors.Is(err, operations.ErrPaused) {
			p.log.Error("platform: reserved assignment invocation could not be recovered", "error", err)
		}
	}
}
func (p *Platform) blockUnknownAssignment(tenant, id, reason string) {
	if err := p.Operations.BlockAssignment(tenant, id, reason, p.now()); err != nil {
		p.log.Error("platform: assignment recovery blocker could not be kept", "error", err)
	}
}
