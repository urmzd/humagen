package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/urmzd/humagen/fiber/pkg/event"
	"github.com/urmzd/humagen/fiber/pkg/runtime"
	"github.com/urmzd/humagen/fiber/pkg/turn"

	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/inbox"
	"github.com/urmzd/humagen/pkg/policy"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/usage"
)

// Asking a person to approve a run at a limit.
//
// A policy whose action is approve does not stop a run that reaches its spend
// or token limit. The run is held instead: the platform asks the runtime to
// park it on a question of its own (runtime.Hold), which the run meets at its
// next batch of tool calls, before any of them runs. The question is a
// budget question, raised in the inbox as an approval and in Slack the same
// way, for the people who hold billing:write.
//
//   - Grant: the run may use the policy's max_grant (or max_grant_tokens)
//     more in that scope, in the policy's period. The grant is a usage
//     ledger entry naming the run, the scope, the limit, the policy version
//     and who granted it, and every later call of the run is judged against
//     the limit plus what was granted to it. The run carries on, and is held
//     again if it reaches that.
//   - Stop, or nobody answering in time: the run is stopped, through the
//     runtime, and what the batch would have run never runs.
//
// The amount is the policy's, read again when the grant is made: a person
// grants what the policy allows, never a number of their own, and a policy
// changed to block since the run was held grants nothing. A run is held once
// per scope, limit and grant: holding it again before anyone answered is the
// same hold. A hold the runtime cannot record stops the run as before, so a
// limit never goes unenforced.

// budgetTopic is the topic of the runtime's holds this file makes, and the
// kind of their questions in the inbox.
const budgetTopic = "budget"

// budgetHold is a budget hold's detail, carried on its question.
type budgetHold struct {
	Scope   policy.Scope  `json:"scope"`
	Limit   policy.Limit  `json:"limit"`
	Period  policy.Period `json:"period"`
	Version int           `json:"version"`
	// Used and Max are where the scope stood when the run was held: micro-USD
	// or tokens, and Max its limit.
	Used int64 `json:"used"`
	Max  int64 `json:"max"`
	// Granted is what the run had already been granted in the scope this
	// period, and Grant what approving this hold grants.
	Granted int64 `json:"granted,omitempty"`
	Grant   int64 `json:"grant"`
}

// isBudget reports whether a question is a budget hold's.
func isBudget(q event.Question) bool {
	return q.Kind == event.QuestionHold && q.Hold != nil && q.Hold.Topic == budgetTopic
}

// budgetOf reads a budget question's detail.
func budgetOf(q event.Question) (budgetHold, bool) {
	if !isBudget(q) {
		return budgetHold{}, false
	}
	var b budgetHold
	if err := json.Unmarshal(q.Hold.Data, &b); err != nil {
		return budgetHold{}, false
	}
	return b, true
}

// grantOf is what one approval at a limit grants under a policy: nothing when
// the policy does not ask for approval.
func grantOf(pol policy.Policy, limit policy.Limit) int64 {
	if pol.Action != policy.Approve {
		return 0
	}
	switch limit {
	case policy.LimitSpend:
		return pol.MaxGrant
	case policy.LimitTokens:
		return pol.MaxGrantTokens
	}
	return 0
}

// budgetHoldID is the runtime hold's id for a run held in a scope at a limit,
// after n grants there: the same while nobody has granted it more, so holding
// it twice is one hold, and new once a grant has been used up.
func budgetHoldID(scope policy.Scope, limit policy.Limit, n int) string {
	sum := sha256.Sum256([]byte(string(scope.Kind) + "\x00" + scope.Name + "\x00" + string(limit)))
	return fmt.Sprintf("budget.%s.%d", hex.EncodeToString(sum[:8]), n)
}

// pendingGrants holds grants being answered, before their ledger entry is
// written, so a run woken by the answer is judged with them.
type pendingGrants struct {
	mu sync.Mutex
	by map[string]map[policy.Scope]policy.Grant
}

func (g *pendingGrants) add(run string, scope policy.Scope, grant policy.Grant) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.by == nil {
		g.by = map[string]map[policy.Scope]policy.Grant{}
	}
	if g.by[run] == nil {
		g.by[run] = map[policy.Scope]policy.Grant{}
	}
	cur := g.by[run][scope]
	g.by[run][scope] = policy.Grant{Spend: cur.Spend + grant.Spend, Tokens: cur.Tokens + grant.Tokens}
}

func (g *pendingGrants) drop(run string, scope policy.Scope) {
	delete(g.by[run], scope)
	if len(g.by[run]) == 0 {
		delete(g.by, run)
	}
}

// granted is what people have granted a run beyond the limits of each policy
// that applies to it and asks for approval, in that policy's period: the
// usage ledger's grants and those being answered now. Nil when no policy
// asks for approval.
func (p *Platform) granted(run runs.Run, applied []policy.Applied) map[policy.Scope]policy.Grant {
	var out map[policy.Scope]policy.Grant
	p.grants.mu.Lock()
	defer p.grants.mu.Unlock()
	for _, a := range applied {
		if a.Policy.Action != policy.Approve {
			continue
		}
		g := p.Usage.Granted(run.Tenant, run.ID, a.Policy.Scope, policy.PeriodStart(a.Policy.Period, p.now()))
		pending := p.grants.by[run.ID][a.Policy.Scope]
		g.Spend, g.Tokens = g.Spend+pending.Spend, g.Tokens+pending.Tokens
		if g == (policy.Grant{}) {
			continue
		}
		if out == nil {
			out = map[policy.Scope]policy.Grant{}
		}
		out[a.Policy.Scope] = g
	}
	return out
}

// holdAtLimit holds a run that reached the limit of a policy that asks for
// approval, once per scope, limit and grant while the request stands, and
// stops it instead when the hold cannot be recorded. Like halt it does not
// wait.
func (p *Platform) holdAtLimit(ctx context.Context, run runs.Run, d policy.Decision, applied []policy.Applied, granted map[policy.Scope]policy.Grant, entry usage.Entry) {
	pol := *d.Policy
	amount := grantOf(pol, d.Limit)
	stop := fmt.Sprintf("Stopped at a limit: %s (%s policy, version %d)", d.Reason, pol.Scope, pol.Version)
	if amount <= 0 {
		p.halt(ctx, run, stop)
		return
	}
	var used int64
	for _, a := range applied {
		if a.Policy.Scope == pol.Scope {
			used = a.Usage.Spend
			if d.Limit == policy.LimitTokens {
				used = a.Usage.Tokens
			}
		}
	}
	b := budgetHold{
		Scope: pol.Scope, Limit: d.Limit, Period: pol.Period, Version: pol.Version,
		Used: used, Max: limitOf(pol, d.Limit), Grant: amount,
	}
	if d.Limit == policy.LimitSpend {
		b.Granted = granted[pol.Scope].Spend
	} else {
		b.Granted = granted[pol.Scope].Tokens
	}
	id := budgetHoldID(pol.Scope, d.Limit, p.Usage.Grants(run.Tenant, run.ID, pol.Scope))
	if _, already := p.holding.LoadOrStore(run.ID+"\x00"+id, true); already {
		return
	}
	data, _ := json.Marshal(b)
	h := turn.Hold{ID: id, Topic: budgetTopic, Prompt: budgetPrompt(run.Agent, d.Reason, d.Limit, amount), Data: data}
	p.log.Warn("platform: run held at a limit for approval", "tenant", run.Tenant, "run", run.ID, "agent", run.Agent, "reason", d.Reason)
	go func() {
		err := p.rt.Hold(context.WithoutCancel(ctx), runtime.HoldRequest{Tenant: run.Tenant, RunID: run.ID, Hold: h})
		switch {
		case err == nil:
			entry.Kind = usage.Held
			p.appendUsage(entry)
		case errors.Is(err, runtime.ErrNotHeld):
			// The run has ended: there is nothing left to hold or stop.
		default:
			p.holding.Delete(run.ID + "\x00" + id)
			p.log.Error("platform: a run at its limit could not be held; stopping it", "tenant", run.Tenant, "run", run.ID, "error", err)
			p.halt(ctx, run, stop)
		}
	}()
}

// budgetPrompt is a budget question, for a person.
func budgetPrompt(agent, reason string, limit policy.Limit, amount int64) string {
	more := money(amount)
	if limit == policy.LimitTokens {
		more = fmt.Sprintf("%d tokens", amount)
	}
	return fmt.Sprintf("%s is held: %s. Grant this run %s more, or stop it.", agent, reason, more)
}

// mayGrant reports whether a person may answer a budget question: granting
// spend or stopping a run at a limit is billing:write's.
func mayGrant(who auth.Principal, reader inbox.Reader) error {
	if !principalOf(who, reader).Can(auth.BillingWrite) {
		return fmt.Errorf("%w: granting a run more than its limit needs %s", ErrForbidden, auth.BillingWrite)
	}
	return nil
}

// grantNow is what approving a budget question grants now: the policy's
// amount as it stands, no more than the question offered. Zero, with why,
// when the policy no longer asks for approval or no longer applies.
func (p *Platform) grantNow(run runs.Run, b budgetHold) (policy.Policy, policy.Grant, error) {
	pols, err := p.Policies.For(run.Tenant, chainOf(run.Project, run.Within), run.Agent, run.Client)
	if err != nil {
		return policy.Policy{}, policy.Grant{}, err
	}
	for _, pol := range pols {
		if pol.Scope != b.Scope {
			continue
		}
		amount := min(grantOf(pol, b.Limit), b.Grant)
		if amount <= 0 {
			return pol, policy.Grant{}, fmt.Errorf("%w: %s's policy no longer lets a person grant more; stop the run or raise the limit", ErrAction, pol.Scope)
		}
		if b.Limit == policy.LimitTokens {
			return pol, policy.Grant{Tokens: amount}, nil
		}
		return pol, policy.Grant{Spend: amount}, nil
	}
	return policy.Policy{}, policy.Grant{}, fmt.Errorf("%w: %s no longer has a policy; the run is not held by it", ErrAction, b.Scope)
}

// grantNote is how a grant settles its question.
func grantNote(b budgetHold, g policy.Grant) string {
	if b.Limit == policy.LimitTokens {
		return fmt.Sprintf("Granted %d more tokens", g.Tokens)
	}
	return "Granted " + money(g.Spend) + " more"
}

// budgetMessage is the inbox message a budget question is raised as: an
// approval, from the platform, with the limit the run is held at. Anyone who
// may read the run sees it, and only billing:write is offered its actions.
func (p *Platform) budgetMessage(run runs.Run, q event.Question) inbox.Message {
	part := &inbox.QuestionPart{
		Run: run.ID, ID: q.ID, Kind: budgetTopic, Agent: run.Agent, Prompt: q.Prompt,
		ExpiresAt: q.ExpiresAt, AskedAt: q.At, Default: event.DecisionDeny,
	}
	b, ok := budgetOf(q)
	if ok {
		part.Budget = &inbox.QuestionBudget{
			ScopeKind: string(b.Scope.Kind), ScopeName: b.Scope.Name, Limit: string(b.Limit), Period: string(b.Period),
			Version: b.Version, Used: b.Used, Max: b.Max, Granted: b.Granted, Grant: b.Grant,
		}
	}
	subject := run.Agent + " is held at a limit"
	if ok {
		subject = fmt.Sprintf("%s is held at the %s limit of %s", run.Agent, b.Limit, b.Scope)
	}
	return inbox.Message{
		Kind: inbox.KindApproval, Subject: subject, From: inbox.From{Kind: "system", Name: "Limits"}, Severity: inbox.Warning,
		Source: inbox.Source{Run: run.ID, Thread: run.Thread, Agent: run.Agent, Project: run.Project},
		Parts:  []inbox.Part{{Type: inbox.PartApproval, Question: part}},
	}
}

// heldGrant is a grant being answered.
type heldGrant struct {
	run    runs.Run
	budget budgetHold
	pol    policy.Policy
	grant  policy.Grant
}

// beginGrant decides what approving a budget question grants, from the
// policy as it stands, and counts it for the run until endGrant settles it.
func (p *Platform) beginGrant(tenant string, q inbox.QuestionPart) (heldGrant, error) {
	if q.Budget == nil {
		return heldGrant{}, fmt.Errorf("%w: this question does not say what it grants", ErrAction)
	}
	run, err := p.Runs.Get(tenant, q.Run)
	if err != nil {
		return heldGrant{}, err
	}
	b := budgetHold{
		Scope: policy.Scope{Kind: policy.Kind(q.Budget.ScopeKind), Name: q.Budget.ScopeName}, Limit: policy.Limit(q.Budget.Limit),
		Period: policy.Period(q.Budget.Period), Version: q.Budget.Version, Used: q.Budget.Used, Max: q.Budget.Max,
		Granted: q.Budget.Granted, Grant: q.Budget.Grant,
	}
	pol, g, err := p.grantNow(run, b)
	if err != nil {
		return heldGrant{}, err
	}
	p.grants.add(run.ID, b.Scope, g)
	return heldGrant{run: run, budget: b, pol: pol, grant: g}, nil
}

// endGrant settles a grant being answered: written to the usage ledger, with
// who granted it under which policy version, when the approval was the one
// recorded, and forgotten otherwise.
func (p *Platform) endGrant(g heldGrant, recorded bool, by string) {
	p.grants.mu.Lock()
	defer p.grants.mu.Unlock()
	if recorded {
		scope := g.pol.Scope
		e := usage.Entry{
			Kind: usage.Granted, Tenant: g.run.Tenant, Project: g.run.Project, Within: g.run.Within, Agent: g.run.Agent,
			Client: g.run.Client, RunID: g.run.ID, At: p.now(), Scope: &scope, Version: g.pol.Version, Limit: g.budget.Limit,
			Reason: grantNote(g.budget, g.grant), Grant: &g.grant, By: by,
		}
		if _, _, err := p.Usage.Record(e); err != nil {
			p.log.Error("platform: usage entry could not be written yet", "kind", usage.Granted, "run", g.run.ID, "error", err)
		}
		p.log.Info("platform: a run held at a limit was granted more", "tenant", g.run.Tenant, "run", g.run.ID,
			"scope", scope.String(), "limit", g.budget.Limit, "by", by, "spend", g.grant.Spend, "tokens", g.grant.Tokens)
	}
	p.grants.drop(g.run.ID, g.budget.Scope)
}
