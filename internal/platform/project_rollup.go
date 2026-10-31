package platform

import (
	"cmp"
	"slices"
	"time"

	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/inbox"
	"github.com/urmzd/humagen/pkg/memory"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
)

const MaxRollupProjects = 100
const MaxRollupNotes = 100
const MaxRollupNeeds = 50

type ProjectRollupRow struct {
	Name          string `json:"name"`
	Title         string `json:"title"`
	Parent        string `json:"parent"`
	Active        int    `json:"active"`
	Conversations int    `json:"conversations"`
	Unread        int    `json:"unread"`
	Needs         int    `json:"needs"`
	Runs30Days    int    `json:"runs_30_days"`
}
type ProjectRollupNote struct {
	ID      string      `json:"id"`
	Project string      `json:"project"`
	Text    string      `json:"text"`
	Kind    memory.Kind `json:"kind"`
}
type ProjectRollupNeed struct {
	ID      string     `json:"id"`
	Project string     `json:"project"`
	Subject string     `json:"subject"`
	Kind    inbox.Kind `json:"kind"`
}
type ProjectRollup struct {
	Project      string              `json:"project"`
	Since        time.Time           `json:"since"`
	Projects     []ProjectRollupRow  `json:"projects"`
	Notes        []ProjectRollupNote `json:"notes"`
	Needs        []ProjectRollupNeed `json:"needs"`
	MoreProjects bool                `json:"more_projects"`
	MoreNotes    bool                `json:"more_notes"`
	MoreNeeds    bool                `json:"more_needs"`
}

// RollupProject exposes only readable descendants, selected reviewable note
// kinds and this reader's actionable inbox. Accounting attribution is untouched.
func (p *Platform) RollupProject(who auth.Principal, name string) (ProjectRollup, error) {
	tree := p.ProjectTree(who.Tenant)
	name = projects.NameOf(name)
	seer := p.Seer(who)
	if !who.Can(auth.ProjectsRead) || !tree.Has(name) || !seer.Project(name) {
		return ProjectRollup{}, projects.ErrNotFound
	}
	now := p.now()
	out := ProjectRollup{Project: name, Since: now.Add(-30 * 24 * time.Hour), Projects: []ProjectRollupRow{}, Notes: []ProjectRollupNote{}, Needs: []ProjectRollupNeed{}}
	visible := map[string]bool{}
	children := tree.Subtree(name)
	slices.Sort(children)
	for _, child := range children {
		if seer.Project(child) {
			visible[child] = true
		}
	}
	counts := p.ProjectCounts(who)
	monthly := map[string]int{}
	// Resolve passive source chains before entering the run index read lock:
	// legacy source records can require an index lookup of their own.
	inheritedAccess := map[string]bool{}
	for _, th := range p.Threads.Records(who.Tenant, func(th threads.Thread) bool { return th.Fork != nil }) {
		inheritedAccess[th.ID] = seer.Place(Place{Project: th.Project, Thread: th.ID})
	}
	if who.Can(auth.RunsRead) {
		_, err := p.Runs.SummarizeWhere(who.Tenant, runs.SummaryQuery{Since: out.Since, Until: now.Add(time.Nanosecond), Bucket: 24 * time.Hour, AllProjects: true, Keep: func(run runs.Run) bool {
			place := p.RunPlace(who.Tenant, run)
			readable, inherited := inheritedAccess[place.Thread]
			if !inherited {
				readable = seer.Run(run)
			}
			if !visible[place.Project] || !readable {
				return false
			}
			// The summary widens buckets to UTC days; retain the exact rolling window.
			if !run.CreatedAt.Before(out.Since) {
				monthly[place.Project]++
			}
			return true
		}})
		if err != nil {
			return ProjectRollup{}, err
		}
	}
	for _, child := range children {
		if !visible[child] {
			continue
		}
		pr, _ := tree.Get(child)
		if !seer.Project(pr.Parent) {
			pr.Parent = ""
		}
		c := counts[child]
		unread, err := p.Inbox.CountProject(who.Tenant, child, p.inboxQuery(tree, who, inbox.Query{}))
		if err != nil {
			p.log.Warn("platform: a project's unread count could not be read", "tenant", who.Tenant, "project", child, "error", err)
		}
		if child != name {
			if len(out.Projects) < MaxRollupProjects {
				out.Projects = append(out.Projects, ProjectRollupRow{Name: child, Title: cmp.Or(pr.Title, child), Parent: pr.Parent, Conversations: c.Threads, Active: c.Active, Unread: unread.Unread + unread.Updates, Needs: unread.Open, Runs30Days: monthly[child]})
			} else {
				out.MoreProjects = true
			}
		}
		for _, note := range p.Memory.List(who.Tenant, memory.ProjectOwner(child)) {
			if note.Status != memory.Approved || !note.ExpiresAt.IsZero() && !note.ExpiresAt.After(now) || note.Kind != memory.Decision && note.Kind != memory.Risk {
				continue
			}
			if len(out.Notes) < MaxRollupNotes {
				out.Notes = append(out.Notes, ProjectRollupNote{ID: note.ID, Project: child, Text: note.Text, Kind: note.Kind})
			} else {
				out.MoreNotes = true
			}
		}
	}
	reader := p.inboxReader(tree, who)
	messages, next, err := p.ProjectInbox(who, name, true, inbox.Query{Needs: true, Limit: MaxRollupNeeds})
	if err != nil {
		return ProjectRollup{}, err
	}
	for _, message := range messages {
		out.Needs = append(out.Needs, ProjectRollupNeed{ID: message.ID, Project: reader.ProjectOf(message.Source), Subject: message.Subject, Kind: message.Kind})
	}
	out.MoreNeeds = next != ""
	return out, nil
}
