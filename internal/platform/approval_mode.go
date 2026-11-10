package platform

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/urmzd/orchagent/flow/event"

	"github.com/urmzd/humagen/internal/legacy"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/audit"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/inbox"
	"github.com/urmzd/humagen/pkg/operations"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
)

// SetApprovalMode sets how a conversation's gated tool actions are approved:
// threads.ApprovalAuto approves for who what they may approve themselves,
// within policy, and threads.ApprovalAsk asks a person again. It takes a
// person who may post in the conversation, and it is audited.
func (p *Platform) SetApprovalMode(who auth.Principal, id, mode string) (threads.Thread, error) {
	th, err := p.ThreadRecord(who.Tenant, id)
	if err != nil {
		return threads.Thread{}, err
	}
	if mode != threads.ApprovalAsk && mode != threads.ApprovalAuto {
		return threads.Thread{}, fmt.Errorf("%w: an approval mode is %s or %s", threads.ErrInvalid, threads.ApprovalAsk, threads.ApprovalAuto)
	}
	if err := p.changesThread(who, th); err != nil {
		return threads.Thread{}, err
	}
	if who.Kind != auth.User {
		return threads.Thread{}, fmt.Errorf("%w: only a person chooses to approve for themselves", ErrForbidden)
	}
	if err := p.MayPost(who, Place{Project: th.Project, Thread: th.ID}); err != nil {
		return threads.Thread{}, err
	}
	if _, err := p.Threads.Get(who.Tenant, id); errors.Is(err, threads.ErrNotFound) {
		// A conversation from before threads has no record to keep it in.
		if _, err := p.Threads.Create(who.Tenant, th, p.now()); err != nil && !errors.Is(err, threads.ErrExists) {
			return threads.Thread{}, err
		}
	}
	before := threads.ApprovalAsk
	if th.Approval.Auto() {
		before = threads.ApprovalAuto
	}
	attempt, err := p.BeginAudit(who, audit.Change{
		Action: "conversation.approval_mode", Resource: "conversation", ID: id, Project: th.Project, Thread: id,
		Before: map[string]string{"mode": before}, After: map[string]string{"mode": mode},
	})
	if err != nil {
		return threads.Thread{}, err
	}
	roles := make([]string, 0, len(who.Roles))
	for _, r := range who.Roles {
		roles = append(roles, string(r))
	}
	got, err := p.Threads.SetApproval(who.Tenant, id, mode, who.Subject, roles, p.now())
	if ferr := attempt.Finish(err); ferr != nil {
		p.LogAuditCompletionFailure(ferr)
	}
	return got, err
}

// autoApprovable reports whether a question a run parked on may be answered
// by its conversation's approval mode, and if not, why. Only an approval of
// a tool action the policy asks about qualifies: not a budget request or
// any other question, not an action the agent itself always asks about
// (its spec's Approvals), not a run forced to ask, and not one the policy
// would refuse.
func autoApprovable(q event.Question, alwaysAsk []string, forced bool, level operations.Level) (bool, string) {
	switch {
	case q.Kind != event.QuestionApproval:
		return false, "not an approval"
	case q.Hold != nil:
		return false, "a budget request"
	case q.Tool == nil || q.Tool.Name == "":
		return false, "names no tool"
	case slices.Contains(alwaysAsk, q.Tool.Name):
		return false, "the agent always asks about " + q.Tool.Name
	case forced:
		return false, "the run asks about every action"
	case level != operations.Ask:
		return false, "the policy does not ask about " + q.Tool.Name
	}
	return true, ""
}

// autoApprove hands an approval a run parked on to the approval agent, when
// its conversation is set to Run (approval_agent.go). The agent answers as
// the person who chose Run, through the path every answer takes, so
// self-approval, the approvers the question names and the runtime's own
// checks all hold. Whatever it may not decide stays in the inbox for a
// person, and every decision is audited.
func (p *Platform) autoApprove(ctx context.Context, run runs.Run, q event.Question, m inbox.Message) {
	if run.Thread == "" || m.ID == "" {
		return
	}
	th, err := p.Threads.Get(run.Tenant, run.Thread)
	if err != nil || !th.Approval.Auto() {
		return
	}
	rev, err := p.Catalog().Get(ctx, run.Tenant, run.Agent, 0)
	if err != nil {
		// What the agent always asks about cannot be read: ask.
		return
	}
	alwaysAsk := agentApprovals(rev.Spec.Approvals)
	if q.Tool != nil && legacy.ToolName(q.Tool.Name) == ToolAgent && slices.Contains(alwaysAsk, tagTool+targetOf(q.Tool.Arguments)) {
		// The agent holds its calls to this one agent itself.
		alwaysAsk = append(alwaysAsk, q.Tool.Name)
	}
	forced := wire.Meta(run.Tag())[operations.MetaForceAsk] == "true"
	level := operations.Never
	if q.Tool != nil {
		level = p.ActionDecision(run.Tenant, run.Project, run.Agent, runs.AskedBy(run), p.categoryOf(q.Tool.Name))
	}
	if ok, why := autoApprovable(q, alwaysAsk, forced, level); !ok {
		p.log.Info("platform: an approval was left for a person", "tenant", run.Tenant, "run", run.ID, "question", q.ID, "why", why)
		return
	}
	p.startApproval(ctx, run, q, m, rev.Spec, p.approvingFor(run.Tenant, *th.Approval))
}

// approvingFor is the person an approval mode answers as: the roles they
// held when they chose it, less any they no longer hold when last seen, so
// it never approves more than they could themselves now.
func (p *Platform) approvingFor(tenant string, a threads.Approval) auth.Principal {
	held := a.Roles
	if p.People != nil {
		if seen, err := p.People.Get(tenant, a.By); err == nil {
			held = slices.DeleteFunc(slices.Clone(held), func(r string) bool { return !slices.Contains(seen.Roles, r) })
		}
	}
	roles := make([]auth.Role, 0, len(held))
	for _, r := range held {
		roles = append(roles, auth.Role(r))
	}
	pol := p.cfg.Policy
	if pol == nil {
		pol = auth.DefaultPolicy()
	}
	return auth.Principal{Subject: a.By, Name: a.By, Tenant: tenant, Kind: auth.User, Roles: roles}.WithPolicy(pol)
}
