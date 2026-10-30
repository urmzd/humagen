package platform

import (
	"context"
	"os"
	"time"

	"github.com/urmzd/humagen/internal/wire"

	"github.com/urmzd/humagen/fiber/pkg/event"
	"github.com/urmzd/humagen/fiber/pkg/isolation"
	"github.com/urmzd/humagen/fiber/pkg/runtime"
	"github.com/urmzd/humagen/fiber/pkg/turn/native"
	saigetypes "github.com/urmzd/saige/agent/types"

	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/join"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
)

// UseTool calls one of a turn's file tools the way the agent loop would, and
// returns what the tool reported on the run's stream.
func (p *Platform) UseTool(ctx context.Context, req runtime.Request, name string, args map[string]any) (string, []event.Action, error) {
	var reported []event.Action
	tools, err := p.fileTools(nil)(req, func(a event.Action) error {
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

// Claim is the claim a turn of req makes on an instance, on ctx, as the
// runtime would put it there; release it when the turn is over.
func (p *Platform) Claim(ctx context.Context, req runtime.Request) (context.Context, *isolation.Run) {
	run := p.runInstances(ctx, req)
	return isolation.WithRun(ctx, run), run
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
func (p *Platform) ConnectorTools(req runtime.Request) (native.Tools, error) {
	return p.connectorTools(req, func(event.Action) error { return nil })
}

// Redeliver hands the platform an event again, as the bus may.
func (p *Platform) Redeliver(ctx context.Context, e event.Envelope) { p.observe(ctx, e) }

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
	return p.call(withApprovedCall(ctx, req.Agent), tenant, runID, req, false, nil)
}

// CallTargets is what the call_agent tool offers a caller's run.
func (p *Platform) CallTargets(tenant, runID, caller string, spawn []string) []string {
	return p.callTargets(tenant, runID, caller, spawn)
}

// CallToolApproval asks the call tool a turn would get whether a call needs
// approval, as the runtime's approval gate does.
func (p *Platform) CallToolApproval(req runtime.Request, args map[string]any) (native.ApprovalRequest, bool) {
	t := p.callAgentTool(req, func(event.Action) error { return nil })
	return t.(native.Approving).Approval(saigetypes.ToolCallPart{Name: ToolCallAgent, Arguments: args})
}

// JoinFor is who takes a message a client raised, as stamp asks, in the
// client's home.
func (p *Platform) JoinFor(ctx context.Context, c clients.Client, text string) ([]join.Decision, error) {
	project, channel, _ := p.filedFor(c.Tenant, c, "")
	return p.joinFor(ctx, c, p.clientPlace(c, project, channel, ""), text)
}

// DelayRecord holds every run between being accepted and being recorded, as
// a loaded machine can, so what a run does at once comes first. It is set
// before any run starts.
func (p *Platform) DelayRecord(d time.Duration) {
	p.afterAccept = func(event.Envelope) { time.Sleep(d) }
}

// Untagged is the agent of a message a client's in-process connector raises
// naming no agent: none, which stamp resolves where the run is filed.
const Untagged = ""

// SeatPeer puts a connector of the test's own in place of the one a
// client's record built, as the platform runs a connector in process.
// What it raises is stamped as that client's.
func (p *Platform) SeatPeer(tenant, id string, peer Peer) error {
	c, err := p.Clients.Get(tenant, id)
	if err != nil {
		return err
	}
	p.seat(c, &seat{client: c, peer: peer})
	return nil
}

// HousekeepingTag is what a hook sends for a turn at a place: a run of
// origin system, read where the turn is.
func HousekeepingTag(tenant, id, agent string, turn Place) event.Envelope {
	return wire.NewEvent(wire.Raise{ID: id, Tenant: tenant, Agent: agent, Origin: runs.OriginSystem, Text: "title it", At: time.Now(), Context: map[string]string{metaPlaceProject: turn.Project, metaPlaceThread: turn.Thread}})
}

// RetainInbox runs one pass of inbox retention, as the platform's loop does
// every hour.
func (p *Platform) RetainInbox() { p.retainInbox(context.Background()) }

// SkillNamesOf is the names of the skills the runs a message starts are told
// to use, as a retry, which names no client, finds them.
func (p *Platform) SkillNamesOf(tenant string, th threads.Thread, m threads.Message) []string {
	var out []string
	for _, s := range p.skillsOfMessage(tenant, th, m, "") {
		out = append(out, s.Name)
	}
	return out
}

// BeforeCarryOut runs fn after each message is posted and before what it
// starts is decided, as a share landing then would.
func (p *Platform) BeforeCarryOut(fn func()) { p.beforeCarryOut = fn }

// BeforeQuestionKept runs fn after a question's message is found missing
// and built, and before it is kept, as a slow raise would.
func (p *Platform) BeforeQuestionKept(fn func()) { p.beforeQuestionKept = fn }
