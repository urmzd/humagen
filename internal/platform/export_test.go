package platform

import (
	"context"
	"os"
	"time"

	"github.com/urmzd/fiber/pkg/agentrt"
	"github.com/urmzd/fiber/pkg/agentrt/payload"
	"github.com/urmzd/fiber/pkg/envelope"
	"github.com/urmzd/fiber/pkg/runtime"
	saigetypes "github.com/urmzd/saige/agent/types"

	"github.com/urmzd/warp/pkg/auth"
	"github.com/urmzd/warp/pkg/call"
	"github.com/urmzd/warp/pkg/clients"
	"github.com/urmzd/warp/pkg/pick"
	"github.com/urmzd/warp/pkg/runs"
)

// UseTool calls one of a turn's file tools the way the agent loop would, and
// returns what the tool reported on the run's stream.
func (p *Platform) UseTool(ctx context.Context, req runtime.Request, name string, args map[string]any) (string, []payload.Action, error) {
	var reported []payload.Action
	tools, err := p.fileTools(nil)(req, func(a payload.Action) error {
		reported = append(reported, a)
		return nil
	})
	if err != nil {
		return "", nil, err
	}
	tool, err := tools.Lookup(name)
	if err != nil {
		return "", nil, err
	}
	out, err := tool.Execute(ctx, args)
	return out, reported, err
}

// SandboxDir is where a conversation's files are.
func (p *Platform) SandboxDir(tenant, thread string) string { return p.sandboxDir(tenant, thread) }

// SweepSandboxes runs the retention sweep now.
func (p *Platform) SweepSandboxes() { p.sweepSandboxes() }

// Age makes a conversation's files look untouched since a time.
func (p *Platform) Age(tenant, thread string, at time.Time) error {
	return os.Chtimes(p.sandboxDir(tenant, thread), at, at)
}

// ConnectorTools is the client actions a turn would be given.
func (p *Platform) ConnectorTools(req runtime.Request) (agentrt.Tools, error) {
	return p.connectorTools(req, func(payload.Action) error { return nil })
}

// Redeliver hands the platform an event again, as the bus may.
func (p *Platform) Redeliver(ctx context.Context, e envelope.Event) { p.observe(ctx, e) }

// Reconcile runs one pass of the ledger reconciler now.
func (p *Platform) Reconcile(ctx context.Context) { p.reconcile(ctx) }

// Retry asks a failed turn again, as pressing Retry on it in the inbox does,
// without the inbox's check that the part is still open: as two presses
// racing each other would.
func (p *Platform) Retry(ctx context.Context, who auth.Principal, runID string) (runs.Run, error) {
	return p.retry(ctx, who, runID)
}

// Ended acts on a run's ending again, as a second report of it would.
func (p *Platform) Ended(ctx context.Context, run runs.Run) { p.ended(ctx, run) }

// CallApproval is callApproval, the decision the call tool will ask fiber to
// hold a call on.
func (p *Platform) CallApproval(tenant, caller, callee string) (bool, []string, bool, time.Duration) {
	return p.callApproval(tenant, caller, callee)
}

// CallApproved makes a call as the call tool will once a person approved it.
func (p *Platform) CallApproved(ctx context.Context, tenant, runID string, req call.Request) (runs.Run, error) {
	return p.call(withApprovedCall(ctx, req.Agent), tenant, runID, req, false)
}

// CallTargets is what the call_agent tool offers a caller.
func (p *Platform) CallTargets(tenant, caller string, spawn []string) []string {
	return p.callTargets(tenant, caller, spawn)
}

// CallToolApproval asks the call tool a turn would get whether a call needs
// approval, as the runtime's approval gate does.
func (p *Platform) CallToolApproval(req runtime.Request, args map[string]any) (agentrt.ApprovalRequest, bool) {
	t := p.callAgentTool(req, func(payload.Action) error { return nil })
	return t.(agentrt.Approving).Approval(saigetypes.ToolCallPart{Name: ToolCallAgent, Arguments: args})
}

// AutoFor is @auto for a message a client raised, as stamp resolves it.
func (p *Platform) AutoFor(ctx context.Context, c clients.Client, text string) (pick.Choice, error) {
	return p.autoFor(ctx, c, text)
}

// DelayRecord holds every run between being accepted and being recorded, as
// a loaded machine can, so what a run does at once comes first. It is set
// before any run starts.
func (p *Platform) DelayRecord(d time.Duration) {
	p.afterAccept = func(envelope.Tag) { time.Sleep(d) }
}
