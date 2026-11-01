package platform

import (
	"github.com/urmzd/humagen/pkg/inbox"
	"github.com/urmzd/humagen/pkg/operations"
)

// clearOperationAttention retains the matching current cause when keepKey is
// set. A later work-state version replaces old attention without a duplicate
// open item. Failed inbox writes are retried by reconciliation.
func (p *Platform) clearOperationAttention(tenant, kind, id, keepKey string) error {
	for _, m := range p.Inbox.OpenOf(tenant, inbox.KindOperation) {
		if keepKey != "" && m.Key == keepKey {
			continue
		}
		for _, part := range m.Parts {
			if part.Operation == nil || part.Operation.Kind != kind || part.Operation.ID != id || part.State != inbox.PartOpen {
				continue
			}
			if _, _, err := p.Inbox.Resolve(tenant, m.ID, part.ID, inbox.Outcome{Action: ActAcknowledge, By: "system", Note: "The work state changed; this attention item is no longer current.", At: p.now()}); err != nil {
				return err
			}
		}
	}
	return nil
}
func attentionHandled(state operations.WorkState) bool {
	return state == operations.Accepted || state == operations.InProgress || state == operations.Watching || state == operations.Result || state == operations.Cancelled
}
func (p *Platform) reconcileOperationAttention(tenant string) {
	// Only this subsystem raises this kind, capped by operations.MaxRecords.
	for _, m := range p.Inbox.OpenOf(tenant, inbox.KindOperation) {
		for _, part := range m.Parts {
			if part.Operation == nil {
				continue
			}
			op := part.Operation
			handled := false
			switch op.Kind {
			case "assignment":
				a, ok, err := p.Operations.Assignment(tenant, op.ID)
				handled = err == nil && ok && attentionHandled(a.State)
			case "handoff":
				h, ok, err := p.Operations.Handoff(tenant, op.ID)
				handled = err == nil && ok && attentionHandled(h.State)
			}
			if handled {
				if err := p.clearOperationAttention(tenant, op.Kind, op.ID, ""); err != nil {
					p.log.Error("platform: handled operation attention could not be settled", "error", err)
				}
			}
		}
	}
}
