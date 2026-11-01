package platform

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/inbox"
	"github.com/urmzd/humagen/pkg/memory"
	"github.com/urmzd/humagen/pkg/policy"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
)

// Route permission, then project role. Every project route checks one tenant
// permission; the platform then reads the caller's role on the one project
// the route names. A role only narrows: the caller may act when they hold the
// route's permission and their role is at least the one the action needs. A
// project the caller has no role in answers as one that does not exist, and
// a visible one with too low a role says which role it needs.

// ReaderOf is who a caller is to the project tree: their subject, the policy
// roles they hold (for "role:<name>" entries), whether they hold
// projects:govern, and whether they are a machine, which matches only "*"
// and its own subject.
func ReaderOf(who auth.Principal) projects.Reader {
	roles := make([]string, 0, len(who.Roles))
	for _, r := range who.Roles {
		roles = append(roles, string(r))
	}
	return projects.Reader{Subject: who.Subject, Roles: roles, Govern: who.Can(auth.ProjectsGovern), Machine: who.Kind == auth.Machine}
}

// WriterOf is who a change to the tree is made by, and whether they may
// change the workspace and company projects.
func WriterOf(who auth.Principal) projects.Writer {
	return projects.Writer{By: who.Subject, Govern: who.Can(auth.ProjectsGovern)}
}

// ProjectRole reads a caller's role on one project, "~" for the workspace,
// and holds it to need. A project the tenant does not have, or one the caller
// has no role in, is projects.ErrNotFound, with the body of a missing one; a
// role below need is projects.ErrForbidden, saying which role it needs ("you
// need to own Globex").
func (p *Platform) ProjectRole(who auth.Principal, name string, need projects.Role) (projects.Project, projects.Access, error) {
	return p.projectRole(p.Projects.Tree(who.Tenant), who, projects.NameOf(name), need)
}

func (p *Platform) projectRole(tree *projects.Tree, who auth.Principal, name string, need projects.Role) (projects.Project, projects.Access, error) {
	pr, ok := tree.Get(name)
	a := tree.Resolve(name, ReaderOf(who))
	if !ok || a.Role == projects.None {
		return projects.Project{}, projects.Access{}, fmt.Errorf("%w: %q", projects.ErrNotFound, name)
	}
	if !a.Role.AtLeast(need) {
		return pr, a, fmt.Errorf("%w: %s", projects.ErrForbidden, roleNeeded(need, titleOf(pr, p.Settings(who.Tenant).WorkspaceName)))
	}
	return pr, a, nil
}

// roleNeeded says in words which role a caller needs.
func roleNeeded(role projects.Role, title string) string {
	switch role {
	case projects.Owner:
		return "you need to own " + title
	case projects.Member:
		return "you need to be a member of " + title
	}
	return "you need to see " + title
}

// SetProjectMembers replaces who holds a role in a project. It needs
// ownership of the project. Every project keeps an entry for everyone of at
// least viewer, a company project one of at least member, and no project is
// more open than its parent or less open than a child. A list the platform
// file sets answers projects.ErrManaged.
func (p *Platform) SetProjectMembers(_ context.Context, who auth.Principal, name string, version int, members []projects.Entry) (projects.Project, error) {
	name = projects.NameOf(name)
	if _, _, err := p.ProjectRole(who, name, projects.Owner); err != nil {
		return projects.Project{}, err
	}
	if members == nil {
		members = []projects.Entry{}
	}
	return p.Projects.Update(who.Tenant, name, version, WriterOf(who), p.now(), func(pr *projects.Project) error {
		pr.Members = slices.Clone(members)
		return nil
	})
}

// MoveProject puts a project, and everything under it, under another
// parent. It needs ownership of the project and membership of the new
// parent. A move never escapes a limit, so without billing:write it is
// refused when it leaves an ancestor that holds one, and when the project or
// one below it holds a limit an owner set that the caller could not change
// before the move: an owner's limit is changed by whoever owns the project
// above it, and a move puts new projects above.
func (p *Platform) MoveProject(_ context.Context, who auth.Principal, name string, version int, parent string) (projects.Project, error) {
	name, parent = projects.NameOf(name), projects.NameOf(parent)
	tree := p.Projects.Tree(who.Tenant)
	if _, _, err := p.projectRole(tree, who, name, projects.Owner); err != nil {
		return projects.Project{}, err
	}
	if _, _, err := p.projectRole(tree, who, parent, projects.Member); err != nil {
		return projects.Project{}, err
	}
	reader := ReaderOf(who)
	leaving := func(before, after *projects.Tree, left []string) error {
		if err := p.keepsAllowLists(before, reader, name, parent, left); err != nil {
			return err
		}
		if !who.Can(auth.BillingWrite) {
			for _, a := range left {
				limited, err := p.hasProjectLimit(who.Tenant, a)
				if err != nil {
					return err
				}
				if limited {
					return fmt.Errorf("%w: %s has a limit, and leaving it needs billing:write, so a move never escapes a limit", projects.ErrForbidden, a)
				}
			}
			owned, err := p.ownerLimited(who.Tenant, before.Subtree(name))
			if err != nil {
				return err
			}
			for _, limited := range owned {
				lp, _ := before.Get(limited)
				if before.Resolve(lp.Parent, reader).Role != projects.Owner {
					above, _ := before.Get(lp.Parent)
					return fmt.Errorf("%w: %s has a limit only an owner of %s changes, and moving it needs billing:write, so a move never escapes a limit",
						projects.ErrForbidden, limited, display(above))
				}
			}
		}
		lost, err := p.lostByMove(who.Tenant, before, after, before.Subtree(name))
		if err != nil {
			return err
		}
		if len(lost) > 0 {
			under := parent
			if under == projects.Root {
				under = "the workspace"
			}
			return fmt.Errorf("%w: under %s, %s", projects.ErrInUse, under, strings.Join(lost, ", "))
		}
		return nil
	}
	return p.Projects.Move(who.Tenant, name, version, parent, WriterOf(who), p.now(), leaving)
}

// maxLostThreads is how many threads a refused move names, of those whose
// default it would leave unavailable.
const maxLostThreads = 5

// lostByMove names what outside the project tree would lose an agent if the
// projects moved, names, went from before to after: a schedule filed in one
// of them, whose every occurrence admission would refuse, and a thread there
// whose own default would answer nothing, as a default that stopped being
// available is refused rather than passed over. The tree's own defaults and
// homes are the projects store's to check (projects.Store.Move).
func (p *Platform) lostByMove(tenant string, before, after *projects.Tree, names []string) ([]string, error) {
	moved := make(map[string]bool, len(names))
	for _, n := range names {
		moved[n] = true
	}
	lost := func(project, agent string) bool {
		return agent != "" && agent != Open && before.IsAvailable(project, agent) && !after.IsAvailable(project, agent)
	}
	var out []string
	scheds, err := p.Schedules.List(tenant)
	if err != nil {
		out = append(out, "the schedules, which could not be read, might be refused")
	}
	for _, sc := range scheds {
		if at := sc.Context[runs.MetaProject]; moved[at] && lost(at, sc.Agent) {
			out = append(out, fmt.Sprintf("the schedule %s, which runs %s in %s, would be refused every time", sc.Name, sc.Agent, at))
		}
	}
	ths, err := p.Threads.Records(tenant, func(th threads.Thread) bool { return moved[th.Project] && lost(th.Project, th.Default) })
	if err != nil {
		return nil, err
	}
	slices.SortFunc(ths, func(a, b threads.Thread) int { return strings.Compare(a.ID, b.ID) })
	for i, th := range ths {
		if i == maxLostThreads {
			out = append(out, fmt.Sprintf("and the defaults of %d more threads", len(ths)-i))
			break
		}
		name := th.ID
		if th.Title != "" {
			name = fmt.Sprintf("%q (%s)", th.Title, th.ID)
		}
		out = append(out, fmt.Sprintf("the default of the thread %s in %s, %s, would not be available", name, th.Project, th.Default))
	}
	return out, nil
}

// keepsAllowLists refuses a move that would let an agent run in the moved
// project, or below it, that an allow list it leaves kept out: an allow list
// only tightens, and the ancestor that set it never agreed to less. Whoever
// owns every ancestor left behind that sets one could widen those lists
// themselves, and so may move out from under them; projects:govern owns
// every project. A move under a chain that allows no more is never refused.
func (p *Platform) keepsAllowLists(before *projects.Tree, reader projects.Reader, name, parent string, left []string) error {
	cur, _ := before.Get(name)
	if !projects.Widens(before.Allowed(cur.Parent), before.Allowed(parent)) {
		return nil
	}
	for _, a := range left {
		above, _ := before.Get(a)
		if above.Agents.Allow != nil && before.Resolve(a, reader).Role != projects.Owner {
			return fmt.Errorf("%w: %s lets only some agents run under it, so moving %s out from under it needs owning %s: a move never widens which agents may run",
				projects.ErrForbidden, display(above), display(cur), display(above))
		}
	}
	return nil
}

// hasProjectLimit reports whether a project has a limit of its own.
func (p *Platform) hasProjectLimit(tenant, name string) (bool, error) {
	pols, err := p.Policies.For(tenant, []string{name}, "", "")
	if err != nil {
		return false, err
	}
	return slices.ContainsFunc(pols, func(pol policy.Policy) bool {
		return pol.Scope == policy.Scope{Kind: policy.Project, Name: name}
	}), nil
}

// ownerLimited is the projects among names that hold a limit an owner set.
func (p *Platform) ownerLimited(tenant string, names []string) ([]string, error) {
	pols, err := p.Policies.For(tenant, names, "", "")
	if err != nil {
		return nil, err
	}
	var out []string
	for _, pol := range pols {
		if pol.Scope.Kind == policy.Project && pol.ByOwner() {
			out = append(out, pol.Scope.Name)
		}
	}
	return out, nil
}

// SetProjectLimit sets a tighten-only limit on a project, as an owner of a
// project above it: the limits route for those without billing:write. It is
// written with policy.AuthorityOwner, refused (policy.ErrAdmin) where an
// admin's limit is set, and refused (policy.ErrInvalid), naming the scope
// that binds, when it is looser than any limit over it. The workspace's own
// limits are the tenant scope, set with billing:write.
func (p *Platform) SetProjectLimit(who auth.Principal, name string, pol policy.Policy) (policy.Policy, error) {
	name = projects.NameOf(name)
	above, err := p.ownerAbove(who, name)
	if err != nil {
		return policy.Policy{}, err
	}
	pol.Scope = policy.Scope{Kind: policy.Project, Name: name}
	if pol.Action == "" {
		pol.Action = policy.Block
	}
	var saved policy.Policy
	err = p.InProject(who.Tenant, name, func() error {
		var err error
		saved, err = p.Policies.PutOwned(who.Tenant, pol, above, who.Subject, p.now())
		return err
	})
	return saved, err
}

// DeleteProjectLimit removes a limit an owner of a project above set. An
// admin's limit answers policy.ErrAdmin, and a project with none
// policy.ErrNotFound.
func (p *Platform) DeleteProjectLimit(who auth.Principal, name string) error {
	name = projects.NameOf(name)
	if _, err := p.ownerAbove(who, name); err != nil {
		return err
	}
	return p.Policies.DeleteOwned(who.Tenant, policy.Scope{Kind: policy.Project, Name: name}, who.Subject, p.now())
}

// ownerAbove holds a caller to owning a project above name, the workspace
// included, and returns name's ancestors nearest first, the root left out.
func (p *Platform) ownerAbove(who auth.Principal, name string) ([]string, error) {
	tree := p.Projects.Tree(who.Tenant)
	pr, _, err := p.projectRole(tree, who, name, projects.Viewer)
	if err != nil {
		return nil, err
	}
	if pr.IsRoot() {
		return nil, fmt.Errorf("%w: the workspace's own limits are set with billing:write", projects.ErrForbidden)
	}
	if a := tree.Resolve(pr.Parent, ReaderOf(who)); a.Role != projects.Owner {
		parent, _ := tree.Get(pr.Parent)
		return nil, fmt.Errorf("%w: %s to limit %s", projects.ErrForbidden, roleNeeded(projects.Owner, titleOf(parent, p.Settings(who.Tenant).WorkspaceName)),
			titleOf(pr, p.Settings(who.Tenant).WorkspaceName))
	}
	return tree.Ancestors(name), nil
}

// AddProjectNote adds a note to a project's shared memory. The note of one
// who may change the project's notes (MayChangeNotes) is in use at once. Any
// other member's is a proposal: it waits, and the project's owners are asked
// in the inbox to keep or discard it. So is an owner's note on the workspace
// or a company project without projects:govern.
func (p *Platform) AddProjectNote(who auth.Principal, name, text string, metadata ...memory.Details) (memory.Note, error) {
	details := memory.Details{}
	if len(metadata) > 1 {
		return memory.Note{}, memory.ErrInvalid
	}
	if len(metadata) == 1 {
		details = metadata[0]
	}
	name = projects.NameOf(name)
	pr, _, err := p.ProjectRole(who, name, projects.Member)
	if err != nil {
		return memory.Note{}, err
	}
	// The workspace's notes and a company project's, which every agent in a
	// wide part of the tree is sent, are changed with projects:govern: an
	// owner without it proposes, as a member does.
	owner := memory.ProjectOwner(name)
	if p.MayChangeNotes(who, name) == nil {
		return p.Memory.AddDetailed(who.Tenant, owner, text, memory.Person, memory.Approved, who.Subject, p.now(), details)
	}
	note, err := p.Memory.AddDetailed(who.Tenant, owner, text, memory.Person, memory.Pending, who.Subject, p.now(), details)
	if err != nil {
		return memory.Note{}, err
	}
	title := titleOf(pr, p.Settings(who.Tenant).WorkspaceName)
	p.raiseInbox(who.Tenant, "memory:note:"+owner+":"+note.ID, inbox.Message{
		Kind: inbox.KindMemory, Subject: fmt.Sprintf("%s proposed a note for %s", p.NameOf(who.Tenant, who.Subject), title),
		From: inbox.From{Kind: "person", Name: who.Subject}, Severity: inbox.Info,
		Audience: []string{inbox.OwnersAudience(name)},
		Source:   inbox.Source{Project: name},
		Parts: []inbox.Part{{Type: inbox.PartMemory, Memory: &inbox.MemoryPart{
			Owner: owner, Project: name, Notes: []inbox.NoteRef{{ID: note.ID, Text: note.Text}},
		}}},
	}, p.now())
	return note, nil
}

// memoryProject is the project a memory part keeps notes for, if it is a
// project's rather than an agent's.
func memoryProject(part inbox.Part) (string, bool) {
	if part.Memory == nil {
		return "", false
	}
	return memory.OwnerProject(part.Memory.Owner)
}

// MayFileIn holds a caller to being at least a member of the project work is
// filed in: a new conversation, a turn in one, or a schedule and what it runs.
// A viewer reads a project and starts nothing in it. The workspace, "" or "~",
// asks nothing more than the route's permission. A project the caller cannot
// see is projects.ErrNotFound.
func (p *Platform) MayFileIn(who auth.Principal, project string) error {
	project = projects.NameOf(project)
	if pr, ok := p.Projects.Tree(who.Tenant).Get(project); ok && pr.PersonalOwner != "" && (who.Kind != auth.User || pr.PersonalOwner != who.Subject) {
		if !p.Seer(who).Project(project) {
			return projects.ErrNotFound
		}
		return ErrForbidden
	}
	if project == projects.Root {
		return nil
	}
	_, _, err := p.ProjectRole(who, project, projects.Member)
	return err
}

// MayTry holds a caller to what trying an agent in an eval takes: the eval
// files its cases where the agent lives, so a project agent's takes a member
// of its home, as any run filed there does. A platform agent asks nothing
// more than the route's permission.
func (p *Platform) MayTry(who auth.Principal, agent string) error {
	home, homed := p.Projects.Tree(who.Tenant).Home(wire.NormalizeName(agent))
	if !homed || home == projects.Root {
		return nil
	}
	return p.MayFileIn(who, home)
}

// MayStop holds a caller to what stopping a run takes: whoever asked for it
// (a person, or a credential as itself), the machine of the client that
// raised it, whose surface shows a stop control, or one who may add a turn
// where it lives (MayFollow). A viewer reads a project's runs and stops none
// of anyone else's.
func (p *Platform) MayStop(who auth.Principal, run runs.Run) error {
	if who.Subject != "" && run.Actor.ID == who.Subject && run.Actor.Bot == (who.Kind == auth.Machine) {
		return nil
	}
	if run.Client != "" {
		if c, err := p.Clients.Get(who.Tenant, run.Client); err == nil && machineOf(c, who) {
			return nil
		}
	}
	return p.MayFollow(who, run)
}

// Acting is the person a surface's credential asks for, as a principal: the
// person its client maps the surface's user to, with the roles the
// workspace's policy grants their subject, so their own role on a project is
// what is checked, never the credential's. Anyone it does not map is refused.
func (p *Platform) Acting(who auth.Principal, as ActAs) (auth.Principal, error) {
	person, _, err := p.answerer(who, &as)
	if err != nil {
		return auth.Principal{}, err
	}
	return principalOf(who, person), nil
}

// OwnsHome holds a caller to owning the project an agent lives in, when it
// lives in one: changing a project agent, what it may call, what it remembers,
// or where it lives, is changing that project. A platform agent, which lives
// nowhere, needs nothing more.
func (p *Platform) OwnsHome(who auth.Principal, agent string) error {
	return p.ownsHome(p.Projects.Tree(who.Tenant), who, agent)
}

func (p *Platform) ownsHome(tree *projects.Tree, who auth.Principal, agent string) error {
	// Homes are kept by the agent's canonical name, which is what the
	// registry and every store resolve a name to, so a name in another
	// case or with spaces around it is the same agent here too.
	home, homed := tree.Home(wire.NormalizeName(agent))
	if !homed || home == projects.Root {
		return nil
	}
	_, _, err := p.projectRole(tree, who, home, projects.Owner)
	return err
}
