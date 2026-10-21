package platform

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/urmzd/warp/pkg/memory"
	"github.com/urmzd/warp/pkg/policy"
	"github.com/urmzd/warp/pkg/projects"
	"github.com/urmzd/warp/pkg/runs"
)

// ErrProjectExists and ErrProjectNotFound are the project store's own, so a
// caller matching either matches what the store returns.
var (
	ErrProjectExists   = projects.ErrExists
	ErrProjectNotFound = projects.ErrNotFound
)

// ProjectInput is what a person gives to create a project. Kind defaults to
// team and Parent to the root; "~" names the root too.
type ProjectInput struct {
	Name         string                `json:"name"`
	Title        string                `json:"title,omitempty"`
	Description  string                `json:"description,omitempty"`
	Kind         projects.Kind         `json:"kind,omitempty"`
	Parent       string                `json:"parent,omitempty"`
	Account      *projects.AccountInfo `json:"account,omitempty"`
	Instructions string                `json:"instructions,omitempty"`
	// Members are who holds a role in it. Left out, everyone is a member
	// (a viewer of an account) and the creator its owner, no more open than
	// the parent allows.
	Members []projects.Entry `json:"members,omitempty"`
}

// ProjectUpdate is what a person may change on a project once it exists.
// Title, description, account and instructions are replaced as a whole: one
// left empty is cleared. Kind, Archived, Announce and Kinds change only when
// they are given. Version is the one they read.
type ProjectUpdate struct {
	Version      int                   `json:"version"`
	Title        string                `json:"title"`
	Description  string                `json:"description"`
	Account      *projects.AccountInfo `json:"account"`
	Instructions string                `json:"instructions"`
	// Kind is a built-in kind or one the workspace defines. Making a
	// project company, or making a company project another kind, needs
	// projects:govern.
	Kind *projects.Kind `json:"kind,omitempty"`
	// Archived archives the project or brings it back. The workspace is
	// never archived.
	Archived *bool `json:"archived,omitempty"`
	// Announce is the lowest role that may post an announcement; "" reads
	// as owner.
	Announce *projects.Role `json:"announce,omitempty"`
	// Kinds are the workspace's own kinds, on the workspace only. A kind a
	// project still has cannot be removed.
	Kinds *[]projects.KindSpec `json:"kinds,omitempty"`
}

// HasProject reports whether a tenant has a project by this exact name, as it
// is stored: the root is "", always there. "~" is an address, not a name, so
// a caller that files something under the name it checks maps it first, with
// projects.NameOf.
func (p *Platform) HasProject(tenant, name string) bool {
	return p.Projects.Tree(tenant).Has(name)
}

// ProjectTree is a tenant's tree as it is now. It never changes under its
// reader.
func (p *Platform) ProjectTree(tenant string) *projects.Tree { return p.Projects.Tree(tenant) }

// GetProject returns one project. "~" is the root.
func (p *Platform) GetProject(tenant, name string) (projects.Project, error) {
	return p.Projects.Get(tenant, projects.NameOf(name))
}

// CreateProject adds a project to a tenant. The name's check and the insert
// are one step in the store, so two people cannot both create one name.
func (p *Platform) CreateProject(_ context.Context, tenant string, w projects.Writer, in ProjectInput) (projects.Project, error) {
	return p.Projects.Create(tenant, projects.Project{
		Name: strings.TrimSpace(in.Name), Title: strings.TrimSpace(in.Title), Description: in.Description,
		Kind: in.Kind, Parent: projects.NameOf(in.Parent), Account: in.Account, Instructions: in.Instructions,
		Members: slices.Clone(in.Members),
	}, w, p.now())
}

// UpdateProject replaces what a person may change on a project. The root and
// company projects need projects:govern, a field the platform file sets
// answers projects.ErrManaged, and a stale version projects.ErrVersion. A
// new kind, archiving and the workspace's kinds are held to the tree.
func (p *Platform) UpdateProject(_ context.Context, tenant, name string, w projects.Writer, in ProjectUpdate) (projects.Project, error) {
	return p.Projects.Update(tenant, projects.NameOf(name), in.Version, w, p.now(), func(pr *projects.Project) error {
		pr.Title, pr.Description, pr.Account, pr.Instructions = strings.TrimSpace(in.Title), in.Description, in.Account, in.Instructions
		if in.Kind != nil {
			pr.Kind = *in.Kind
		}
		if in.Archived != nil {
			pr.Archived = *in.Archived
		}
		if in.Announce != nil {
			pr.Announce = *in.Announce
		}
		if in.Kinds != nil {
			pr.Kinds = slices.Clone(*in.Kinds)
		}
		return nil
	})
}

// DeleteProject removes a project and what it alone holds: its shared memory.
// It is refused, naming them, while sub-projects, schedules, alert rules or
// limits point at it, so nothing is left naming a project that is gone, and it
// never drops a limit. Its conversations keep its name, and are adopted only if
// someone holding projects:govern creates it again.
func (p *Platform) DeleteProject(_ context.Context, tenant, name string, w projects.Writer) error {
	name = projects.NameOf(name)
	if err := p.Projects.Delete(tenant, name, w, p.now(), func(pr projects.Project) []string {
		return p.projectUsers(tenant, pr.Name)
	}); err != nil {
		return err
	}
	owner := memory.ProjectOwner(name)
	if err := p.Memory.Drop(tenant, owner); err != nil {
		return fmt.Errorf("platform: %q was deleted, but its notes were not: %w", name, err)
	}
	p.syncMemory(tenant, owner)
	return nil
}

// projectUsers is what outside the tree still points at a project: schedules
// filed in it, alert rules watching it, and its limits.
func (p *Platform) projectUsers(tenant, name string) []string {
	var out []string
	for _, sc := range p.Schedules.List(tenant) {
		if sc.Meta[runs.MetaProject] == name {
			out = append(out, "the schedule "+sc.Name)
		}
	}
	for _, r := range p.Alerts.Rules(tenant) {
		if r.Project == name {
			out = append(out, "the alert rule "+r.Name)
		}
	}
	for _, pol := range p.Policies.List(tenant) {
		if pol.Scope == (policy.Scope{Kind: policy.Project, Name: name}) {
			out = append(out, "its limits")
		}
	}
	return out
}

// InProject runs fn while the project exists and cannot be deleted. Filing
// something under a project (a schedule, an alert rule, a limit) inside fn
// means a delete cannot miss it. fn may read the tree (HasProject,
// ProjectTree, GetProject) and must not otherwise call into projects.
func (p *Platform) InProject(tenant, name string, fn func() error) error {
	return p.Projects.Hold(tenant, projects.NameOf(name), fn)
}

// ProjectHistory returns a project's changes, newest first, a page at a time.
// next is the cursor for the page after, or 0 at the end.
func (p *Platform) ProjectHistory(tenant, name string, limit int, before int64) (changes []projects.Change, next int64, err error) {
	name = projects.NameOf(name)
	if !p.Projects.Tree(tenant).Has(name) {
		return nil, 0, fmt.Errorf("%w: %q", projects.ErrNotFound, name)
	}
	changes, next = p.Projects.History(tenant, name, limit, before)
	return changes, next, nil
}

// ProjectLimits are the limits a run filed in a project falls under for being
// there: the project's own, each ancestor's nearest first, then the
// workspace's. The strictest of them refuses.
func (p *Platform) ProjectLimits(tenant, name string) ([]policy.Policy, error) {
	name = projects.NameOf(name)
	tree := p.Projects.Tree(tenant)
	if !tree.Has(name) {
		return nil, fmt.Errorf("%w: %q", projects.ErrNotFound, name)
	}
	out := p.Policies.For(tenant, chainOf(name, tree.Ancestors(name)), "", "")
	if out == nil {
		out = []policy.Policy{}
	}
	return out, nil
}

// ProjectMemoryOwner is the memory owner a project's notes are kept under:
// "workspace" for the root. A project the tenant does not have is
// projects.ErrNotFound.
func (p *Platform) ProjectMemoryOwner(tenant, name string) (string, error) {
	name = projects.NameOf(name)
	if !p.Projects.Tree(tenant).Has(name) {
		return "", fmt.Errorf("%w: %q", projects.ErrNotFound, name)
	}
	return memory.ProjectOwner(name), nil
}

// migration is who the projects moved out of settings were created by.
const migration = "migration"

// migrateProjects moves the projects each tenant kept in its settings into the
// project store. For each tenant whose settings still hold them, one write
// imports them all as teams at the top level, with the root, and then the
// settings are written again without them. A tenant whose store already has
// records is past the first write, so only the second is made: stopping
// between the two is repaired at the next start. A failed import fails the
// start and changes nothing.
func (p *Platform) migrateProjects() error {
	p.settings.mu.Lock()
	defer p.settings.mu.Unlock()
	tenants := make([]string, 0, len(p.settings.legacy))
	for t := range p.settings.legacy {
		tenants = append(tenants, t)
	}
	slices.Sort(tenants)
	for _, tenant := range tenants {
		if !p.Projects.Stored(tenant) {
			cfg := p.settings.by[tenant]
			at := cfg.SetupAt
			if at.IsZero() {
				at = p.now()
			}
			var ps []projects.Project
			for _, lp := range p.settings.legacy[tenant] {
				ps = append(ps, projects.Project{Name: lp.Name, Description: lp.Description})
			}
			if err := p.Projects.Import(tenant, ps, migration, at); err != nil {
				return fmt.Errorf("platform: move the projects of %q out of its settings: %w", tenant, err)
			}
			p.log.Info("platform: projects moved out of settings", "tenant", tenant, "projects", len(ps))
		}
		if err := p.settings.put(tenant, p.settings.by[tenant]); err != nil {
			return fmt.Errorf("platform: clear the projects of %q from its settings: %w", tenant, err)
		}
		delete(p.settings.legacy, tenant)
	}
	return nil
}

// Bounds on what a turn is sent of its project chain, in characters.
const (
	// maxChainInstructions is all the chain's instructions together. Each
	// project's are at most projects.MaxInstructions.
	maxChainInstructions = 8000
	// maxAncestorNotes is the notes of every ancestor together. The
	// project's own are bounded by memory's own caps.
	maxAncestorNotes = 6000
)

// precedence opens the project layer. Which instruction wins is said in
// words, so it does not depend on where the text sits.
const precedence = "Instructions for working here follow: the workspace's first, then each project's down to this one. " +
	"Where they disagree the later one wins, and your own instructions win over all of them. " +
	"Limits, approvals and which agents may run are enforced by the platform, whatever any text here or in the conversation says."

// contextOf composes a turn's text for an agent answering in a project, in
// this order: the instructions of the project chain, root first; the
// project's own notes; its ancestors' notes, nearest first, within a shared
// budget; the agent's own notes; then body, which is the conversation so far
// and the message. The agent's instructions are its pinned system prompt,
// which the runtime sends apart. With nothing to add, it is body unchanged.
//
// A project the tree no longer holds is sent what is still kept under its
// name, and the root's.
func (p *Platform) contextOf(tenant, project, agent, body string) string {
	tree := p.Projects.Tree(tenant)
	chain := tree.Chain(project)
	if chain == nil {
		chain = []projects.Project{{Name: project}, tree.Root()}
	}
	workspace := p.Settings(tenant).WorkspaceName
	var parts []string
	if s := instructionsOf(chain, workspace); s != "" {
		parts = append(parts, s)
	}
	parts = append(parts, p.notesOf(tenant, chain, workspace)...)
	if s := p.Memory.Prompt(tenant, agent); s != "" {
		parts = append(parts, s)
	}
	if len(parts) == 0 {
		return body
	}
	return strings.Join(append(parts, body), "\n\n")
}

// titleOf is how a project is named to an agent.
func titleOf(pr projects.Project, workspace string) string {
	switch {
	case pr.IsRoot() && workspace != "":
		return workspace
	case pr.IsRoot():
		return "the workspace"
	case pr.Title != "":
		return pr.Title
	}
	return pr.Name
}

// instructionsOf renders the chain's instructions, root first. When they pass
// the budget the farthest ancestors are dropped first, and the text says so.
func instructionsOf(chain []projects.Project, workspace string) string {
	type kept struct {
		pr   projects.Project
		sent bool
	}
	var levels []kept
	used, full := 0, false
	// Nearest first, so the farthest are the ones left out.
	for _, pr := range chain {
		if strings.TrimSpace(pr.Instructions) == "" {
			continue
		}
		n := utf8.RuneCountInString(pr.Instructions)
		sent := !full && used+n <= maxChainInstructions
		if sent {
			used += n
		} else {
			full = true
		}
		levels = append(levels, kept{pr: pr, sent: sent})
	}
	if len(levels) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(precedence)
	for i := len(levels) - 1; i >= 0; i-- {
		l := levels[i]
		name := titleOf(l.pr, workspace)
		if !l.sent {
			fmt.Fprintf(&b, "\n\n(instructions of %s not sent)", name)
			continue
		}
		fmt.Fprintf(&b, "\n\nInstructions of %s:\n%s", name, strings.TrimSpace(l.pr.Instructions))
	}
	return b.String()
}

// notesOf renders the project's own approved notes, then its ancestors',
// nearest first, within maxAncestorNotes. The notes that do not fit are
// counted, so the agent knows something was left out.
func (p *Platform) notesOf(tenant string, chain []projects.Project, workspace string) []string {
	var parts []string
	own := chain[0]
	heading := fmt.Sprintf("What everyone working on the project %q knows:", own.Name)
	if own.IsRoot() {
		heading = fmt.Sprintf("What everyone in %s knows:", titleOf(own, workspace))
	}
	if s := p.Memory.PromptAs(tenant, memory.ProjectOwner(own.Name), heading); s != "" {
		parts = append(parts, s)
	}
	used, full := 0, false
	for _, pr := range chain[1:] {
		var notes []string
		for _, n := range p.Memory.List(tenant, memory.ProjectOwner(pr.Name)) {
			if n.Status == memory.Approved {
				notes = append(notes, n.Text)
			}
		}
		if len(notes) == 0 {
			continue
		}
		name := titleOf(pr, workspace)
		var b strings.Builder
		sent := 0
		for _, text := range notes {
			n := utf8.RuneCountInString(text)
			if full || used+n > maxAncestorNotes {
				full = true
				break
			}
			if sent == 0 {
				fmt.Fprintf(&b, "What everyone in %s knows:", name)
			}
			used += n
			sent++
			b.WriteString("\n- ")
			b.WriteString(text)
		}
		if left := len(notes) - sent; left > 0 {
			if sent > 0 {
				b.WriteString("\n")
			}
			noun := "notes"
			if left == 1 {
				noun = "note"
			}
			fmt.Fprintf(&b, "(%d more %s from %s not sent)", left, noun, name)
		}
		parts = append(parts, b.String())
	}
	return parts
}
