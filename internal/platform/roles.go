package platform

import (
	"context"
	"fmt"
	"slices"

	"github.com/urmzd/warp/pkg/auth"
	"github.com/urmzd/warp/pkg/inbox"
	"github.com/urmzd/warp/pkg/memory"
	"github.com/urmzd/warp/pkg/policy"
	"github.com/urmzd/warp/pkg/projects"
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
// parent. A move never escapes a limit: leaving an ancestor that holds one
// needs billing:write too.
func (p *Platform) MoveProject(_ context.Context, who auth.Principal, name string, version int, parent string) (projects.Project, error) {
	name, parent = projects.NameOf(name), projects.NameOf(parent)
	tree := p.Projects.Tree(who.Tenant)
	if _, _, err := p.projectRole(tree, who, name, projects.Owner); err != nil {
		return projects.Project{}, err
	}
	if _, _, err := p.projectRole(tree, who, parent, projects.Member); err != nil {
		return projects.Project{}, err
	}
	leaving := func(left []string) error {
		if who.Can(auth.BillingWrite) {
			return nil
		}
		for _, a := range left {
			if p.hasProjectLimit(who.Tenant, a) {
				return fmt.Errorf("%w: %s has a limit, and leaving it needs billing:write, so a move never escapes a limit", projects.ErrForbidden, a)
			}
		}
		return nil
	}
	return p.Projects.Move(who.Tenant, name, version, parent, WriterOf(who), p.now(), leaving)
}

// hasProjectLimit reports whether a project has a limit of its own.
func (p *Platform) hasProjectLimit(tenant, name string) bool {
	return slices.ContainsFunc(p.Policies.For(tenant, []string{name}, "", ""), func(pol policy.Policy) bool {
		return pol.Scope == policy.Scope{Kind: policy.Project, Name: name}
	})
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

// AddProjectNote adds a note to a project's shared memory. An owner's note is
// in use at once. A member's is a proposal: it waits, and the project's
// owners are asked in the inbox to keep or discard it. So is an owner's in
// the workspace or a company project without projects:govern.
func (p *Platform) AddProjectNote(who auth.Principal, name, text string) (memory.Note, error) {
	name = projects.NameOf(name)
	pr, access, err := p.ProjectRole(who, name, projects.Member)
	if err != nil {
		return memory.Note{}, err
	}
	// The workspace's notes and a company project's, which every agent in a
	// wide part of the tree is sent, are changed with projects:govern: an
	// owner without it proposes, as a member does.
	owner := memory.ProjectOwner(name)
	if access.Role == projects.Owner && (!governedNotes(pr) || who.Can(auth.ProjectsGovern)) {
		return p.Memory.Add(who.Tenant, owner, text, memory.Person, memory.Approved, who.Subject, p.now())
	}
	note, err := p.Memory.Add(who.Tenant, owner, text, memory.Person, memory.Pending, who.Subject, p.now())
	if err != nil {
		return memory.Note{}, err
	}
	title := titleOf(pr, p.Settings(who.Tenant).WorkspaceName)
	p.raiseInbox(who.Tenant, "memory:note:"+owner+":"+note.ID, inbox.Message{
		Kind: inbox.KindMemory, Subject: fmt.Sprintf("%s proposed a note for %s", who.Subject, title),
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

// ownsNotes reports whether a caller may decide a project's notes: the right
// to change projects, and ownership of the project.
func ownsNotes(tree *projects.Tree, who auth.Principal, project string) bool {
	return who.Can(auth.ProjectsWrite) && tree.Resolve(project, ReaderOf(who)).Role == projects.Owner
}

// OwnsHome holds a caller to owning the project an agent lives in, when it
// lives in one: changing a project agent, or where it lives, is changing that
// project. A platform agent, which lives nowhere, needs nothing more.
func (p *Platform) OwnsHome(who auth.Principal, agent string) error {
	home, homed := p.Projects.Tree(who.Tenant).Home(agent)
	if !homed {
		return nil
	}
	_, _, err := p.ProjectRole(who, home, projects.Owner)
	return err
}
