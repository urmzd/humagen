package platform

import (
	"github.com/urmzd/humagen/pkg/operations"
)

// Keep the existing scheduler's admission switch aligned with durable work.
// A failed activation becomes blocked instead of falsely reporting Watching.
func (p *Platform) syncAssignmentSchedules() {
	err := p.Operations.EachTenant(func(tenant string) error {
		p.syncTenantAssignmentSchedules(tenant)
		return nil
	})
	if err != nil {
		p.log.Error("platform: operation tenants could not be read", "error", err)
	}
}

func (p *Platform) syncTenantAssignmentSchedules(tenant string) {
	{
		after := ""
		if v, ok := p.operationScheduleCursor.Load(tenant); ok {
			after = v.(string)
		}
		assignments, next, err := p.Operations.AssignmentPage(tenant, after, operations.MaxResults, func(a operations.Assignment) bool { return a.Schedule != "" })
		if err != nil {
			p.log.Error("platform: assignments could not be read", "error", err)
			return
		}
		p.operationScheduleCursor.Store(tenant, next)
		for _, a := range assignments {
			watching := a.State == operations.Watching || a.State == operations.Accepted
			if watching && !p.standingOwner(tenant, a.Project, a.Owner) {
				p.blockUnknownAssignment(tenant, a.ID, "The standing assignment owner no longer has the project access this contract requires.")
				watching = false
			}
			paused := !watching || p.operationAdmission(tenant, a.Agent) != nil || (!a.DueAt.IsZero() && !a.DueAt.After(p.now()))
			_, current, err := p.Schedules.Get(tenant, a.Schedule)
			if err == nil && current == paused {
				continue
			}
			if err == nil {
				err = p.Schedules.SetPaused(tenant, a.Schedule, paused)
			}
			if err != nil {
				p.log.Error("platform: assignment scheduler state could not be kept", "error", err)
				if watching {
					p.blockUnknownAssignment(tenant, a.ID, "The standing assignment schedule needs repair before it can continue.")
				}
			}
		}
	}
}
