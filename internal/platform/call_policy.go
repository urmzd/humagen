package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/urmzd/fiber/pkg/agentrt/payload"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/callpolicy"
	"github.com/urmzd/humagen/pkg/inbox"
)

// Governing calls between agents.
//
// Two sides agree to every call. The caller's pinned spec names the callee in
// its spawn allowlist, and the callee's call policy admits the caller. Either
// can refuse, and the refusal says which. The callee's policy also says which
// admitted calls wait for a person, and who that person may be: callApproval
// is that decision. The call_agent tool is an agentrt.Approving tool, so the
// runtime asks callApproval's approvers before the call runs, and parks the
// run while they decide. A call that needs approval goes through only when a
// person approved that very tool call (approvals); anything else is refused,
// so a policy asking for a person is never skipped.

// ErrCallRefused reports a call the callee's policy refuses. It is an
// ErrForbidden.
var ErrCallRefused = fmt.Errorf("%w: refused by the callee", ErrForbidden)

// CallPolicy is an agent's call policy: the stored one, or the default.
// Another tenant's agent, or none, is wire.ErrNotFound.
func (p *Platform) CallPolicy(ctx context.Context, tenant, agent string) (callpolicy.Policy, error) {
	if _, err := p.Catalog().Get(ctx, tenant, agent, 0); err != nil {
		return callpolicy.Policy{}, err
	}
	pol, _ := p.CallPolicies.Get(tenant, agent)
	return pol, nil
}

// SetCallPolicy stores an agent's call policy. Every agent it names must be
// one of the tenant's.
func (p *Platform) SetCallPolicy(ctx context.Context, tenant, agent, by string, pol callpolicy.Policy) (callpolicy.Policy, error) {
	if _, err := p.Catalog().Get(ctx, tenant, agent, 0); err != nil {
		return callpolicy.Policy{}, err
	}
	pol.Agent = agent
	known := func(name string) bool {
		_, err := p.Catalog().Get(ctx, tenant, name, 0)
		return err == nil
	}
	return p.CallPolicies.Put(tenant, pol, known, by, p.now())
}

// admitCall is the callee's side of a call: whether its policy lets caller
// call it, and whether the call may go ahead without a person.
func (p *Platform) admitCall(ctx context.Context, tenant, caller, callee string) error {
	pol, _ := p.CallPolicies.Get(tenant, callee)
	if !pol.Admits(caller) {
		if pol.Callers == callpolicy.Nobody {
			return fmt.Errorf("%w: %q accepts calls from no agent", ErrCallRefused, callee)
		}
		return fmt.Errorf("%w: %q does not accept calls from %q", ErrCallRefused, callee, caller)
	}
	if pol.NeedsApproval(caller) && !approvedCall(ctx, callee) {
		return fmt.Errorf("%w: %q requires a person to approve calls from %q, and this call was not approved", ErrCallRefused, callee, caller)
	}
	return nil
}

// fallbackApprovers approve a held call whose policy names nobody. A call
// that needs a person is never open to anyone in the workspace.
var fallbackApprovers = []string{"role:manager", "role:admin"}

// callApproval is whether a call from caller to callee waits for a person, who
// may approve it (subjects and role:<name>; fallbackApprovers when the policy
// names nobody), whether the person whose request led to the call may approve
// it, and how long it waits before it is denied. It does not say whether the
// call is admitted: admitCall does. The call tool's Approval asks it.
func (p *Platform) callApproval(tenant, caller, callee string) (needed bool, approvers []string, selfApproval bool, expires time.Duration) {
	pol, _ := p.CallPolicies.Get(tenant, callee)
	if !pol.NeedsApproval(caller) {
		return false, nil, false, 0
	}
	approvers = slices.Clone(pol.Approvers)
	if len(approvers) == 0 {
		approvers = slices.Clone(fallbackApprovers)
	}
	return true, approvers, pol.SelfApproval, pol.Expiry()
}

// maxApproved bounds the approved calls noted at once, across tenants. Past
// it an approval is still recorded but its call is refused: fail closed.
const maxApproved = 10_000

// approvals are the calls to agents a person approved, by tenant, run and
// tool call, with the callee approved.
type approvals struct {
	mu   sync.Mutex
	byID map[approvalKey]string
}

type approvalKey struct{ tenant, run, call string }

// noteApprovedCall notes that a person is approving a call to an agent, and
// reports whether it did. Only questions about call_agent are noted. The
// callee is read from the arguments approved: edited ones when the person
// edited them.
func (p *Platform) noteApprovedCall(tenant string, q inbox.QuestionPart, ans payload.Answer) bool {
	if q.Kind != payload.QuestionApproval || q.Tool == nil || q.Tool.CallID == "" {
		return false
	}
	args := q.Tool.Arguments
	if len(ans.Arguments) > 0 {
		args = ans.Arguments
	}
	callee := "tool:" + q.Tool.Name
	if q.Tool.Name == ToolCallAgent {
		callee = targetOf(args)
	}
	if callee == "" {
		return false
	}
	a := &p.approved
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.byID == nil {
		a.byID = map[approvalKey]string{}
	}
	if len(a.byID) >= maxApproved {
		p.log.Warn("platform: too many approved calls are waiting to run, so this one will be refused", "tenant", tenant, "run", q.Run)
		return false
	}
	a.byID[approvalKey{tenant, q.Run, q.Tool.CallID}] = callee
	a.byID[approvalKey{tenant, q.Run, q.Tool.CallID + ":action"}] = "tool:" + q.Tool.Name
	return true
}

func (p *Platform) dropApprovedCall(tenant string, q inbox.QuestionPart) {
	if q.Tool == nil {
		return
	}
	p.approved.mu.Lock()
	defer p.approved.mu.Unlock()
	delete(p.approved.byID, approvalKey{tenant, q.Run, q.Tool.CallID})
	delete(p.approved.byID, approvalKey{tenant, q.Run, q.Tool.CallID + ":action"})
}

// approvedCallTo reports whether a person approved this tool call of this
// run, to this callee.
func (p *Platform) approvedCallTo(tenant, run, call, callee string) bool {
	p.approved.mu.Lock()
	defer p.approved.mu.Unlock()
	got, ok := p.approved.byID[approvalKey{tenant, run, call}]
	return ok && call != "" && got == callee
}

// forgetApprovedCalls drops what was noted for a run that has ended.
func (p *Platform) forgetApprovedCalls(tenant, run string) {
	p.approved.mu.Lock()
	defer p.approved.mu.Unlock()
	for k := range p.approved.byID {
		if k.tenant == tenant && k.run == run {
			delete(p.approved.byID, k)
		}
	}
}

type approvedCallKey struct{}

// withApprovedCall marks a call to callee as approved by a person, for the
// call that follows. Only the call tool, once approvedCallTo says a person
// approved that tool call, uses it.
func withApprovedCall(ctx context.Context, callee string) context.Context {
	return context.WithValue(ctx, approvedCallKey{}, callee)
}

func approvedCall(ctx context.Context, callee string) bool {
	got, ok := ctx.Value(approvedCallKey{}).(string)
	return ok && got == callee
}

// callTargets is the part of a caller's spawn allowlist the call tool offers
// a run: the agents available where the run's calls are filed, whose policy
// admits the caller. A callee that needs approval is offered: the call waits
// for a person. One not available there would be refused, and lives in a
// project that the readers of the run may not read, so it is never named.
func (p *Platform) callTargets(tenant, runID, caller string, spawn []string) []string {
	at, _ := p.placeOfRunID(tenant, runID)
	tree := p.Projects.Tree(tenant)
	out := []string{}
	for _, callee := range spawn {
		if !tree.IsAvailable(at.Project, callee) {
			continue
		}
		pol, _ := p.CallPolicies.Get(tenant, callee)
		if pol.Admits(caller) {
			out = append(out, callee)
		}
	}
	return out
}

// Reach is whether one agent may call another.
type Reach struct {
	Agent string `json:"agent"`
	// Admits is whether the callee's policy accepts calls from the caller.
	Admits bool `json:"admits"`
	// InSpawn is whether the caller's spec allows calling the callee.
	InSpawn bool `json:"in_spawn"`
	// Approval is never or required.
	Approval string `json:"approval"`
	Reason   string `json:"reason"`
}

// MaxReachable is the most agents Reachable reports on.
const MaxReachable = wire.MaxPageSize

// Reachable reports, for every other agent in the caller's tenant, whether the
// caller may call it and why. truncated is true when the tenant has more
// agents than MaxReachable.
func (p *Platform) Reachable(ctx context.Context, tenant, caller string) (out []Reach, truncated bool, err error) {
	rev, err := p.Catalog().Get(ctx, tenant, caller, 0)
	if err != nil {
		return nil, false, err
	}
	revs, next, err := p.Catalog().List(ctx, tenant, wire.Page{Size: MaxReachable})
	if err != nil {
		return nil, false, err
	}
	out = []Reach{}
	for _, other := range revs {
		callee := other.Spec.Name
		if callee == caller {
			continue
		}
		pol, _ := p.CallPolicies.Get(tenant, callee)
		r := Reach{Agent: callee, Admits: pol.Admits(caller), InSpawn: slices.Contains(wire.PolicyOf(rev.Spec).Access.Spawn, callee), Approval: "never"}
		if needed, _, _, _ := p.callApproval(tenant, caller, callee); needed {
			r.Approval = "required"
		}
		switch {
		case !r.InSpawn:
			r.Reason = fmt.Sprintf("%s's spec does not list %s in its spawn allowlist", caller, callee)
		case !r.Admits && pol.Callers == callpolicy.Nobody:
			r.Reason = callee + " accepts calls from no agent"
		case !r.Admits:
			r.Reason = callee + " accepts calls only from the agents it lists"
		case r.Approval == "required":
			r.Reason = "a person must approve each call"
		default:
			r.Reason = "callable"
		}
		out = append(out, r)
	}
	return out, next != "", nil
}

// questionTarget is the agent a question's tool call would call, when the
// tool calls agents and its arguments name one.
func questionTarget(q payload.Question) string {
	if q.Tool == nil || (q.Tool.Name != ToolCallAgent && q.Tool.Name != ToolDelegate) {
		return ""
	}
	return targetOf(q.Tool.Arguments)
}

// targetOf reads the agent a call tool's arguments name.
func targetOf(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var args struct {
		Agent string `json:"agent"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return ""
	}
	return args.Agent
}
