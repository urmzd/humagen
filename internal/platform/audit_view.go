package platform

import (
	"fmt"
	"slices"
	"strconv"

	"github.com/urmzd/humagen/pkg/audit"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/callpolicy"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/readaudit"
)

// AuditRecords adapts retained legacy records without inventing missing rules
// or before values. Policy snapshots are explicitly snapshots, not full history.
func (p *Platform) AuditRecords(who auth.Principal, q audit.Query) ([]audit.Record, error) {
	seer := p.Seer(who)
	visible := func(record audit.Record) bool {
		if record.Resource == "source" || record.Resource == "source_connection" {
			if !seer.Knowledge() {
				return false
			}
			for _, snapshot := range []map[string]string{record.Before, record.After} {
				if project, recorded := snapshot["project"]; recorded && !seer.Project(project) {
					return false
				}
			}
			if _, err := p.Knowledge.Get(who.Tenant, record.Change.ID); err == nil && !p.SourceVisible(who, record.Change.ID) {
				return false
			}
		}
		if record.Resource == "client" && !who.Can(auth.ClientsRead) {
			return false
		}
		if record.Resource == "source_link_rule" {
			if !who.Can(auth.ClientsRead) || !seer.Knowledge() {
				return false
			}
			for _, snapshot := range []map[string]string{record.Before, record.After} {
				if source := snapshot["source"]; source != "" && !p.SourceVisible(who, source) {
					return false
				}
				if project := snapshot["project"]; project != "" && !seer.Project(project) {
					return false
				}
			}
		}
		if (record.Resource == "source" || record.Resource == "knowledge" || record.Resource == "document") && !seer.Knowledge() {
			return false
		}
		if record.Thread != "" && !who.Can(auth.RunsRead) {
			return false
		}
		if record.Agent != "" && !who.Can(auth.AgentsRead) {
			return false
		}
		if record.Subject != "" && record.Subject != who.Subject {
			return false
		}
		if record.Agent != "" && !seer.Agent(record.Agent) {
			return false
		}
		if record.Scope == "workspace" {
			return true
		}
		return seer.Place(Place{Project: record.Project, Thread: record.Thread})
	}
	rows, err := p.Audit.List(who.Tenant, q, visible)
	if err != nil {
		return nil, err
	}
	limit := min(max(q.Limit, 1), audit.MaxRows)
	if q.Limit == 0 {
		limit = 100
	}
	cut := p.now().Add(-p.inboxRetention(who.Tenant))
	from := q.From
	if from.Before(cut) {
		from = cut
	}
	reads, err := p.ReadAudit.ListVisible(who.Tenant, from, q.To, limit, func(r readaudit.Record) bool {
		return (q.Actor == "" || q.Actor == r.Subject) && (q.Resource == "" || q.Resource == "project") && seer.Place(Place{Project: r.Project, Thread: r.Thread})
	})
	if err != nil {
		return nil, err
	}
	for _, r := range reads {
		id, err := p.AuditReferences.ID(who.Tenant, "read", fmt.Sprintf("%s:%s:%s:%d", r.Subject, r.Project, r.Thread, r.At.UnixNano()))
		if err != nil {
			return nil, err
		}
		rows = append(rows, audit.Record{ID: id, Actor: r.Subject, At: r.At, FinishedAt: r.At, Outcome: audit.Succeeded, Change: audit.Change{Action: "project.break_glass_read", Resource: "project", ID: r.Project, Scope: "project", Project: r.Project, Thread: r.Thread, Route: r.Route, Rule: "projects:govern"}})
	}
	for _, r := range p.Projects.RecentHistory(who.Tenant) {
		if q.Actor != "" && r.By != q.Actor || q.Resource != "" && q.Resource != "project" || r.At.Before(from) || !r.At.Before(q.To) || !seer.Project(r.Project) {
			continue
		}
		before := projectAuditSnapshot(seer, r.Before)
		after := projectAuditSnapshot(seer, r.After)
		id, err := p.AuditReferences.ID(who.Tenant, "project", strconv.FormatInt(r.Seq, 10))
		if err != nil {
			return nil, err
		}
		rows = append(rows, audit.Record{ID: id, Actor: r.By, At: r.At, FinishedAt: r.At, Outcome: audit.Succeeded, Change: audit.Change{Action: "project." + r.What, Resource: "project", ID: r.Project, Project: r.Project, Scope: "project", Before: before, After: after}})
	}
	for _, r := range p.CallPolicies.Snapshots(who.Tenant, from, q.To, limit, func(pol callpolicy.Policy) bool {
		return (q.Actor == "" || q.Actor == pol.UpdatedBy) && (q.Resource == "" || q.Resource == "call_policy") && seer.Agent(pol.Agent)
	}) {
		id, err := p.AuditReferences.ID(who.Tenant, "call-policy", fmt.Sprintf("%s:%d", r.Agent, r.Version))
		if err != nil {
			return nil, err
		}
		rows = append(rows, audit.Record{ID: id, Actor: r.UpdatedBy, At: r.UpdatedAt, FinishedAt: r.UpdatedAt, Outcome: audit.Succeeded, Change: audit.Change{Action: "call_policy.snapshot", Resource: "call_policy", ID: r.Agent, Scope: "agent", Agent: r.Agent, After: map[string]string{"version": strconv.Itoa(r.Version), "callers": string(r.Callers), "approval": string(r.Approval)}}})
	}
	rows = slices.DeleteFunc(rows, func(r audit.Record) bool {
		return q.Actor != "" && r.Actor != q.Actor || q.Resource != "" && r.Resource != q.Resource
	})
	slices.SortFunc(rows, func(a, b audit.Record) int {
		if n := b.At.Compare(a.At); n != 0 {
			return n
		}
		return -stringsCompare(a.ID, b.ID)
	})
	if len(rows) > limit {
		rows = rows[:limit]
	}
	return rows, nil
}
func stringsCompare(a, b string) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}
func projectAuditSnapshot(seer *Seer, pr *projects.Project) map[string]string {
	if pr == nil {
		return nil
	}
	out := map[string]string{"name": pr.Name, "title": pr.Title, "version": strconv.Itoa(pr.Version)}
	if pr.Agents.Default != "" && seer.Agent(pr.Agents.Default) {
		out["default"] = pr.Agents.Default
	}
	safe, err := audit.RedactedSnapshot(out)
	if err != nil {
		return nil
	}
	return safe
}
