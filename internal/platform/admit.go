package platform

import (
	"fmt"
	"time"

	"github.com/urmzd/fiber/pkg/envelope"
	"github.com/urmzd/fiber/pkg/runtime"

	"github.com/urmzd/humagen/pkg/alerts"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/inbox"
	"github.com/urmzd/humagen/pkg/policy"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/usage"
)

// Denied reports a run a billing policy refused. It carries the decision, so a
// caller can say which limit, whose, and how much was left.
type Denied struct {
	Decision policy.Decision
}

func (e *Denied) Error() string { return "platform: " + e.Decision.Reason }

// admit decides whether a run may start, and counts it if so. It is called
// with p.admission held, which is what makes the check and the count one step:
// two runs cannot both fit under a limit that only one of them fits under.
//
// Every run passes through here, whatever raised it: a person, a schedule, an
// eval, or another agent. Only the platform's own housekeeping is exempt, since
// refusing it would leave a conversation unnamed for a reason nobody chose.
//
// The run's agent must be available in its project (ErrNotAvailable), which
// is the one place availability is enforced.
//
// A run counts toward its project and every ancestor it is within, and is
// refused by the strictest of their limits, so nothing below a project escapes
// the project's limit.
func (p *Platform) admit(tag envelope.Tag, edge call.Edge, project string, within []string, client string) error {
	if err := p.operationAdmissionTag(tag); err != nil {
		return err
	}
	if th, err := p.Threads.Get(tag.Tenant, tag.Meta[runs.MetaThread]); err == nil && th.ReadOnly {
		return ErrForbidden
	}
	tree := p.Projects.Tree(tag.Tenant)
	if tree.PendingFork(project) {
		return projects.ErrNotFound
	}
	if home, ok := tree.Home(tag.Agent); ok && tree.PendingFork(home) {
		return projects.ErrNotFound
	}
	if tag.Origin == runs.OriginSystem {
		return nil
	}
	// The agent must be available where the run is filed, before any limit
	// is judged or anything counted.
	if err := p.admitAgent(tag, project); err != nil {
		return err
	}
	if err := p.admitAssignment(tag); err != nil {
		return err
	}
	now := p.now()
	d := policy.Decide(p.applied(tag.Tenant, chainOf(project, within), tag.Agent, client, now), policy.Request{Lease: edge.Lease})
	entry := usage.Entry{Tenant: tag.Tenant, Project: project, Within: within, Agent: tag.Agent, Client: client, RunID: runtime.RunID(tag.Tenant, tag.ID), At: now}
	if d.Policy != nil {
		entry.Scope, entry.Version, entry.Limit, entry.Reason = &d.Policy.Scope, d.Policy.Version, d.Limit, d.Reason
	}

	switch d.Verdict {
	case policy.Deny, policy.NeedsApproval:
		entry.Kind = usage.Refused
		p.appendUsage(entry)
		p.budgetAlert(tag.Tenant, d, alerts.Critical, "Runs are being refused: ", string(d.Verdict))
		p.log.Warn("platform: run refused by policy", "tenant", tag.Tenant, "agent", tag.Agent, "project", project, "reason", d.Reason)
		return &Denied{Decision: d}
	case policy.Warn:
		warned := entry
		warned.Kind = usage.Warned
		p.appendUsage(warned)
		p.budgetAlert(tag.Tenant, d, alerts.Warning, "", string(d.Verdict))
	}
	entry.Kind, entry.Scope, entry.Version, entry.Limit, entry.Reason = usage.Admitted, nil, 0, "", ""
	// A run whose admission could not be kept is not let in: a restart
	// would forget it was counted.
	if _, err := p.Usage.Append(entry); err != nil {
		return fmt.Errorf("platform: the run could not be counted: %w", err)
	}
	return nil
}

// appendUsage appends an entry the platform has nobody to report a failed
// write to. The ledger has counted it all the same and writes it later.
func (p *Platform) appendUsage(e usage.Entry) {
	if _, err := p.Usage.Append(e); err != nil {
		p.log.Error("platform: usage entry could not be written yet", "kind", e.Kind, "run", e.RunID, "error", err)
	}
}

// applied is every policy a run in this place falls under, most specific
// first, with what its scope has used in the policy's current period. chain is
// the run's project and the ancestors it is within, nearest first.
func (p *Platform) applied(tenant string, chain []string, agent, client string, now time.Time) []policy.Applied {
	var out []policy.Applied
	for _, pol := range p.Policies.For(tenant, chain, agent, client) {
		since := policy.PeriodStart(pol.Period, now)
		out = append(out, policy.Applied{Policy: pol, Usage: p.Usage.Usage(tenant, pol.Scope, since)})
	}
	return out
}

// placeOf is the project a run is filed under, the ancestors of that project
// it counts toward, and the external client that raised it. A run called by an
// agent has its root's of all three, pinned when the root was admitted, so a
// tree counts against the limits of whatever started it. Otherwise the
// ancestors are read from the tree now, and pinned with the run.
//
// A project the tree no longer holds has no ancestors to read, so a run filed
// there would count toward none of the limits it was under: it fails closed
// with ErrProjectDeleted. That covers the next turn of a conversation whose
// project was deleted, and a turn racing the delete of its project.
func (p *Platform) placeOf(tag envelope.Tag, edge call.Edge) (project string, within []string, client string, err error) {
	project, client = runs.ProjectOf(tag), runs.ClientOf(tag)
	if edge.Parent != "" {
		if root, err := p.Runs.Get(tag.Tenant, runtime.RunID(tag.Tenant, edge.Root)); err == nil {
			return root.Project, root.Within, root.Client, nil
		}
	}
	tree := p.Projects.Tree(tag.Tenant)
	if !tree.Has(project) {
		return "", nil, "", projectDeleted(project)
	}
	return project, tree.Ancestors(project), client, nil
}

// chainOf is a run's project followed by the ancestors it is within, nearest
// first: the project scopes whose limits it falls under.
func chainOf(project string, within []string) []string {
	if project == "" {
		return nil
	}
	return append([]string{project}, within...)
}

// limitOf is the number a policy sets for one of its limits.
func limitOf(pol policy.Policy, l policy.Limit) int64 {
	switch l {
	case policy.LimitRuns:
		return int64(pol.MaxRuns)
	case policy.LimitConcurrent:
		return int64(pol.MaxConcurrent)
	case policy.LimitSpend:
		return pol.SpendLimit
	case policy.LimitTokens:
		return pol.MaxTokens
	}
	return pol.MaxLease
}

// budgetAlert raises one alert per scope, limit and what happened at it
// (warned, refused, stopped) while it stays open, and reports whether this
// call raised it.
func (p *Platform) budgetAlert(tenant string, d policy.Decision, severity alerts.Severity, lead, what string) bool {
	// Nearing a limit, being refused at it and being stopped at it are
	// different things to tell someone, so each has its own alert.
	key := fmt.Sprintf("budget: %s, %s, %s", d.Policy.Scope, d.Limit, what)
	a := alerts.Alert{Kind: alerts.Budget, Severity: severity, Title: lead + d.Reason, At: p.now()}
	if d.Policy.Scope.Kind == policy.Agent {
		a.Agent = d.Policy.Scope.Name
	}
	raised, ok, err := p.Alerts.Raise(tenant, key, a)
	if err != nil {
		p.log.Error("platform: alert could not be kept", "rule", key, "error", err)
	}
	if ok {
		p.log.Warn("platform: alert", "rule", key, "severity", severity, "title", a.Title)
		pol := d.Policy
		p.inboxAlert(tenant, raised, inbox.Part{Type: inbox.PartBudget, Budget: &inbox.BudgetPart{
			ScopeKind: string(pol.Scope.Kind), ScopeName: pol.Scope.Name, Limit: string(d.Limit),
			Period: string(pol.Period), Version: pol.Version, Used: limitOf(*pol, d.Limit) - d.Remaining, Max: limitOf(*pol, d.Limit),
			Refused: severity == alerts.Critical,
			Current: inbox.Limits{MaxRuns: pol.MaxRuns, MaxConcurrent: pol.MaxConcurrent, SpendLimit: pol.SpendLimit, MaxTokens: pol.MaxTokens},
		}})
		return true
	}
	return false
}
