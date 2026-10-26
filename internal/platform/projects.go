package platform

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/memory"
	"github.com/urmzd/humagen/pkg/policy"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
)

// ErrProjectExists and ErrProjectNotFound are the project store's own, so a
// caller matching either matches what the store returns.
var (
	ErrProjectExists   = projects.ErrExists
	ErrProjectNotFound = projects.ErrNotFound
)

// ErrProjectDeleted reports a run asked for in a project the tree no longer
// holds, such as the next turn of a conversation filed in a project since
// deleted. Nothing says any more which projects above it the run would count
// toward, so it is refused rather than let out from under their limits. The
// conversation can still be read.
var ErrProjectDeleted = errors.New("platform: project deleted")

// projectDeleted is ErrProjectDeleted for one project, said in words.
func projectDeleted(project string) error {
	return fmt.Errorf("%w: the project %q was deleted, so nothing more runs in it; its conversations can still be read", ErrProjectDeleted, project)
}

// ProjectInput is what a person gives to create a project. Kind defaults to
// team and Parent to the root; "~" names the root too.
type ProjectInput struct {
	Template     string                `json:"template,omitempty"`
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
	// AgentPosts is what becomes of what an agent scheduled here posts:
	// publish, review, or "" for the kind's default.
	AgentPosts *string `json:"agent_posts,omitempty"`
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
	record := projects.Project{
		Name: strings.TrimSpace(in.Name), Title: strings.TrimSpace(in.Title), Description: in.Description,
		Kind: in.Kind, Parent: projects.NameOf(in.Parent), Account: in.Account, Instructions: in.Instructions,
		Members: slices.Clone(in.Members),
	}
	if in.Template != "" {
		for _, template := range p.cfg.ProjectTemplates {
			if template.Name == in.Template {
				return p.Projects.CreateTemplate(tenant, record, template, w, p.now())
			}
		}
		return projects.Project{}, fmt.Errorf("%w: unknown project template", projects.ErrInvalid)
	}
	return p.Projects.Create(tenant, record, w, p.now())
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
		if in.AgentPosts != nil {
			pr.AgentPosts = *in.AgentPosts
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
// "project:" for the root. A project the tenant does not have is
// projects.ErrNotFound.
func (p *Platform) ProjectMemoryOwner(tenant, name string) (string, error) {
	name = projects.NameOf(name)
	if !p.Projects.Tree(tenant).Has(name) {
		return "", fmt.Errorf("%w: %q", projects.ErrNotFound, name)
	}
	return memory.ProjectOwner(name), nil
}

// MayChangeNotes reports whether who may add, approve or remove a project's
// notes, by any route: the project's memory routes and the inbox alike. It
// takes projects:write and ownership of the project, and for the workspace and
// company projects, whose notes every agent in a wide part of the tree is
// sent, projects:govern as well, as changing the project does. A project the
// caller cannot see is projects.ErrNotFound, and one they may not change
// projects.ErrForbidden, saying what it needs.
func (p *Platform) MayChangeNotes(who auth.Principal, name string) error {
	return p.mayChangeNotes(p.Projects.Tree(who.Tenant), who, name)
}

func (p *Platform) mayChangeNotes(tree *projects.Tree, who auth.Principal, name string) error {
	name = projects.NameOf(name)
	pr, _, err := p.projectRole(tree, who, name, projects.Viewer)
	if err != nil {
		return err
	}
	if !who.Can(auth.ProjectsWrite) {
		return fmt.Errorf("%w: changing a project's notes needs %s", projects.ErrForbidden, auth.ProjectsWrite)
	}
	if _, _, err := p.projectRole(tree, who, name, projects.Owner); err != nil {
		return err
	}
	if governedNotes(pr) && !who.Can(auth.ProjectsGovern) {
		return fmt.Errorf("%w: the notes of the workspace and of company projects are changed with %s", projects.ErrForbidden, auth.ProjectsGovern)
	}
	return nil
}

// governedNotes reports whether a project's notes are changed only with
// projects:govern: the workspace's and a company project's, which every agent
// in a wide part of the tree is sent.
func governedNotes(pr projects.Project) bool { return pr.IsRoot() || pr.Kind == projects.Company }

// mayKeepUnasked reports whether what the memory agent writes for an owner may
// be kept without a person approving it, when the workspace chose that. An
// agent's own notes may. A project's may only when the person the turn was
// for could have kept them themselves, as Converse pinned on the turn
// (keptBy names that project): a member's turn teaches only proposals, which
// reach its owners, since filing a turn needs no more than a member. A
// project whose notes need projects:govern waits for someone holding it
// whatever the workspace chose, since the turn that taught it needed only
// runs:write. So does a project the tree no longer holds.
func (p *Platform) mayKeepUnasked(tenant, owner, keptBy string) bool {
	name, isProject := memory.OwnerProject(owner)
	if !isProject {
		return true
	}
	pr, ok := p.Projects.Tree(tenant).Get(name)
	return ok && !governedNotes(pr) && keptBy != "" && strings.EqualFold(keptBy, pr.Name)
}

// migration is who the projects moved out of settings were created by.
const migration = "migration"

// migrateProjects moves the projects each tenant kept in its settings into the
// project store. For each tenant whose settings an earlier version wrote, one
// write imports them all as teams at the top level, with the root and with
// every name a schedule, an alert rule or a limit still points at that the
// settings no longer held, and then the settings are written again without
// them. A tenant whose store already has records is past the first write, so
// only the second is made: stopping between the two is repaired at the next
// start. A failed import fails the start and changes nothing, and one past the
// cap names what it would have kept.
func (p *Platform) migrateProjects() error {
	p.settings.mu.Lock()
	defer p.settings.mu.Unlock()
	for _, tenant := range slices.Sorted(maps.Keys(p.settings.legacy)) {
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
			kept := p.stillNamed(tenant, ps)
			// A record whose array was null held no projects: there is
			// nothing to import unless something still names one.
			if p.settings.legacy[tenant] != nil || len(kept) > 0 {
				if err := p.Projects.Import(tenant, ps, kept, migration, at); err != nil {
					if len(kept) > 0 {
						return fmt.Errorf("platform: move the projects of %q out of its settings, keeping %s, which a schedule, an alert rule or a limit still names: %w", tenant, names(kept), err)
					}
					return fmt.Errorf("platform: move the projects of %q out of its settings: %w", tenant, err)
				}
				p.log.Info("platform: projects moved out of settings", "tenant", tenant, "projects", len(ps), "kept", names(kept))
			}
		}
		if err := p.settings.put(tenant, p.settings.by[tenant]); err != nil {
			return fmt.Errorf("platform: clear the projects of %q from its settings: %w", tenant, err)
		}
		delete(p.settings.legacy, tenant)
	}
	return nil
}

// stillNamed is every name a tenant's schedules, alert rules and limits still
// point at that the projects about to be imported lack: projects an earlier
// version dropped from settings, by a delete or at start, while things stayed
// filed in them. Each is kept, so nothing that ran before the upgrade is
// refused after it and nothing is left pointing at a name the tree does not
// hold. A name the tree cannot hold (not a project's name, or one name
// ignoring case with another) is logged and left, and what points at it is
// refused until it is changed.
func (p *Platform) stillNamed(tenant string, ps []projects.Project) []projects.Project {
	by := map[string][]string{}
	add := func(name, what string) {
		if name != projects.Root {
			by[name] = append(by[name], what)
		}
	}
	for _, sc := range p.Schedules.List(tenant) {
		add(sc.Meta[runs.MetaProject], "the schedule "+sc.Name)
	}
	for _, r := range p.Alerts.Rules(tenant) {
		add(r.Project, "the alert rule "+r.Name)
	}
	for _, pol := range p.Policies.List(tenant) {
		if pol.Scope.Kind == policy.Project {
			add(pol.Scope.Name, "its limits")
		}
	}
	taken := map[string]string{}
	for _, pr := range ps {
		taken[strings.ToLower(pr.Name)] = pr.Name
	}
	var kept []projects.Project
	for _, name := range slices.Sorted(maps.Keys(by)) {
		have, clash := taken[strings.ToLower(name)]
		switch {
		case have == name:
			continue
		case clash:
			p.log.Warn("platform: a name something still points at is one name with a project, ignoring case, and is not kept", "tenant", tenant, "project", name, "with", have, "named by", strings.Join(by[name], ", "))
		case !projects.ValidName(name):
			p.log.Warn("platform: a name something still points at cannot name a project, and is not kept", "tenant", tenant, "project", name, "named by", strings.Join(by[name], ", "))
		default:
			taken[strings.ToLower(name)] = name
			kept = append(kept, projects.Project{Name: name})
		}
	}
	return kept
}

// names lists projects' names, quoted, for a message.
func names(ps []projects.Project) string {
	out := make([]string, len(ps))
	for i, pr := range ps {
		out[i] = strconv.Quote(pr.Name)
	}
	return strings.Join(out, ", ")
}

// filedUnder is what the project store asks before it forgets a deleted name:
// for one tenant, whether a name, ignoring case, still has conversations or
// files filed under it, which taking the name back would adopt. It reads the
// run index's and the file index's counts once, and is called with the
// project store's lock held.
func (p *Platform) filedUnder(tenant string) func(string) bool {
	filed := map[string]bool{}
	if p.Runs != nil {
		for name, n := range p.Runs.ProjectCounts(tenant) {
			if n.Threads > 0 {
				filed[strings.ToLower(name)] = true
			}
		}
	}
	if p.Artifacts != nil {
		for _, sh := range p.Artifacts.Usage(tenant).ByProject {
			filed[strings.ToLower(sh.Name)] = true
		}
	}
	return func(name string) bool { return filed[strings.ToLower(name)] }
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

// contextIn composes a turn's text for an agent answering in a project, in
// this order: the instructions of the project chain, root first; the
// project's own notes; its ancestors' notes, nearest first, within a shared
// budget; the agent's own notes; then body, which is the conversation so far
// and the message. The agent's instructions are its pinned system prompt,
// which the runtime sends apart. With nothing to add, it is body unchanged.
//
// A project the tree no longer holds is sent what is still kept under its
// name, and the root's.
func (p *Platform) contextIn(tenant, project, channel, agent, body string) string {
	tree := p.Projects.Tree(tenant)
	chain := tree.Chain(project)
	if chain == nil {
		chain = []projects.Project{{Name: project}, tree.Root()}
	}
	layers := p.turnLayers(tenant, tree, chain, project, channel, agent)
	return renderTurnLayers(layers, body)
}

func renderTurnLayers(layers []ContextLayer, body string) string {
	parts := make([]string, 0, len(layers)+1)
	for _, l := range layers {
		if l.Text == "" {
			continue
		}
		parts = append(parts, l.Text)
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

// instructionLayers renders the chain's instructions, root first. When they pass
// the budget the farthest ancestors are dropped first, and the text says so.
func instructionLayers(chain []projects.Project, workspace string) []ContextLayer {
	var levels []ContextLayer
	used, full := 0, false
	for _, pr := range chain {
		if strings.TrimSpace(pr.Instructions) == "" {
			continue
		}
		n := utf8.RuneCountInString(pr.Instructions)
		l := ContextLayer{Kind: "instructions", From: pr.Name, Budget: maxChainInstructions, Unit: "characters"}
		l.Sent = !full && used+n <= maxChainInstructions
		if l.Sent {
			used += n
			l.Used = n
			l.Text = fmt.Sprintf("Instructions of %s:\n%s", titleOf(pr, workspace), strings.TrimSpace(pr.Instructions))
		} else {
			full = true
			l.Omitted = n
			l.Text = fmt.Sprintf("(instructions of %s not sent)", titleOf(pr, workspace))
		}
		levels = append(levels, l)
	}
	slices.Reverse(levels)
	if len(levels) > 0 {
		levels[0].Text = precedence + "\n\n" + levels[0].Text
	}
	return levels
}

// noteLayers renders the project's own approved notes, then its ancestors',
// nearest first, within maxAncestorNotes. The notes that do not fit are
// counted, so the agent knows something was left out.
func (p *Platform) noteLayers(tenant string, chain []projects.Project, workspace string) []ContextLayer {
	var parts []ContextLayer
	own := chain[0]
	heading := fmt.Sprintf("What everyone working on the project %q knows:", own.Name)
	if own.IsRoot() {
		heading = fmt.Sprintf("What everyone in %s knows:", titleOf(own, workspace))
	}
	if s := p.Memory.PromptAt(tenant, memory.ProjectOwner(own.Name), heading, p.now()); s != "" {
		parts = append(parts, ContextLayer{Kind: "project_notes", From: own.Name, Text: s, Budget: memory.MaxNotes * memory.MaxChars, Used: p.noteCharacters(tenant, memory.ProjectOwner(own.Name)), Sent: true, Unit: "characters"})
	}
	used, full := 0, false
	for _, pr := range chain[1:] {
		var notes []string
		for _, n := range p.Memory.List(tenant, memory.ProjectOwner(pr.Name)) {
			if n.Status == memory.Approved && (n.ExpiresAt.IsZero() || n.ExpiresAt.After(p.now())) {
				notes = append(notes, n.Text)
			}
		}
		if len(notes) == 0 {
			continue
		}
		name := titleOf(pr, workspace)
		var b strings.Builder
		sent, chars := 0, 0
		total := 0
		for _, text := range notes {
			total += utf8.RuneCountInString(text)
		}
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
			chars += n
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
		parts = append(parts, ContextLayer{Kind: "ancestor_notes", From: pr.Name, Text: b.String(), Budget: maxAncestorNotes, Used: chars, Omitted: total - chars, Sent: sent > 0, Unit: "characters"})
	}
	return parts
}
