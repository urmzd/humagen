package projects

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// Height is how many levels a project has below it: 0 for a project with no
// children, -1 for a name the tree does not hold.
func (t *Tree) Height(name string) int {
	n, ok := t.nodes[name]
	if !ok {
		return -1
	}
	h := 0
	for _, c := range n.children {
		h = max(h, t.Height(c)+1)
	}
	return h
}

// ArchivedOn returns the nearest project on a chain that is archived: the
// project itself or an ancestor. Nothing new goes under it.
func (t *Tree) ArchivedOn(name string) (string, bool) {
	for _, p := range t.Chain(name) {
		if p.Archived {
			return p.Name, true
		}
	}
	return "", false
}

// open refuses a parent that is archived, or under one.
func (t *Tree) open(parent string) error {
	if at, ok := t.ArchivedOn(parent); ok {
		return fmt.Errorf("%w: %s is archived; nothing new goes under it", ErrInvalid, at)
	}
	return nil
}

// movable checks a project going under a new parent: the parent is there, is
// not archived, is not the project or below it, and the project and
// everything under it still fit within the depth and the parent's children.
func (s *Store) movable(tree *Tree, name, parent string) error {
	if !tree.Has(parent) {
		return fmt.Errorf("%w: there is no project %q to put it under", ErrInvalid, parent)
	}
	if slices.Contains(tree.Subtree(name), parent) {
		return fmt.Errorf("%w: %s cannot go under itself or a project below it", ErrInvalid, name)
	}
	if err := tree.open(parent); err != nil {
		return err
	}
	if tree.Depth(parent)+1+tree.Height(name) > s.caps.Depth {
		return fmt.Errorf("%w: %s and the %d levels below it would sit more than %d levels below the workspace", ErrInvalid, name, tree.Height(name), s.caps.Depth)
	}
	if len(tree.Children(parent)) >= s.caps.Children {
		return fmt.Errorf("%w: a project has at most %d sub-projects", ErrInvalid, s.caps.Children)
	}
	return nil
}

// admit checks a new version of a project that exists against the tree it
// is in: a move, the kind rules, the company cap, the workspace's kinds that
// projects use, and openness against the parent and the children. The
// caller holds the lock.
func (s *Store) admit(t *tenant, cur, next Project) error {
	tree := t.tree()
	children := tree.Children(next.Name)
	if next.IsRoot() {
		if err := kindsHeld(t.projects, cur.Kinds, next.Kinds); err != nil {
			return err
		}
		return checkOpen(next, nil, children)
	}
	if next.Parent != cur.Parent {
		if err := s.movable(tree, next.Name, next.Parent); err != nil {
			return err
		}
	}
	parent, _ := tree.Get(next.Parent)
	if err := tree.kindRules(next, parent, children); err != nil {
		return err
	}
	if next.Kind == Company && cur.Kind != Company {
		if err := s.companyRoom(tree); err != nil {
			return err
		}
	}
	return checkOpen(next, &parent, children)
}

// companyRoom refuses one more company project past the cap.
func (s *Store) companyRoom(tree *Tree) error {
	n := 0
	for _, q := range tree.Projects() {
		if q.Kind == Company {
			n++
		}
	}
	if n >= s.caps.Company {
		return fmt.Errorf("%w: a workspace has at most %d company projects", ErrLimit, s.caps.Company)
	}
	return nil
}

// leftBehind is the ancestors a project leaves when it goes under parent:
// those on its chain now that are not on the new one. The root is on both.
func (t *Tree) leftBehind(name, parent string) []string {
	stays := map[string]bool{parent: true}
	for _, a := range t.Ancestors(parent) {
		stays[a] = true
	}
	var out []string
	for _, a := range t.Ancestors(name) {
		if !stays[a] {
			out = append(out, a)
		}
	}
	return out
}

// Move puts a project, and everything under it, under another parent. The
// checks and the write are one step against one tree, so two moves racing
// cannot make a cycle. The root cannot move; a parent the platform file sets
// answers ErrManaged; the new parent must be there and not archived, and not
// the project or below it; the depth, the parent's children, the kind rules
// and openness must still hold. leaving, when it is not nil, is called under
// the lock with the tree as it is before the move and as it would be after,
// and the ancestors the project leaves, nearest first (none when it goes
// deeper under the chain it has), so the caller can refuse a move that
// escapes a limit, that would hand them a limit they could not change
// before, or that would leave what it keeps outside this store with an agent
// that is no longer available; it must not write to this store. A move that
// would leave an agent unavailable where this store names it is refused here
// (lostByMove). Moving to the parent it has changes nothing.
func (s *Store) Move(tenantName, name string, version int, parent string, w Writer, at time.Time, leaving func(before, after *Tree, left []string) error) (Project, error) {
	if name == Root {
		return Project{}, fmt.Errorf("%w: the workspace cannot move", ErrInvalid)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx := s.begin(tenantName)
	cur, ok := tx.t.projects[name]
	if !ok {
		return Project{}, fmt.Errorf("%w: %q", ErrNotFound, name)
	}
	if cur.Kind == Company && !w.Govern {
		return Project{}, fmt.Errorf("%w: company projects are moved with projects:govern", ErrForbidden)
	}
	if version != cur.Version {
		return Project{}, fmt.Errorf("%w: %q is at version %d, not %d", ErrVersion, name, cur.Version, version)
	}
	if slices.Contains(cur.FileOwns, FieldParent) {
		return Project{}, fmt.Errorf("%w: the platform file sets the parent of %q", ErrManaged, name)
	}
	if parent == cur.Parent {
		return cur.clone(), nil
	}
	next := cur.clone()
	next.Parent = parent
	if err := s.admit(tx.t, cur, next); err != nil {
		return Project{}, err
	}
	before := tx.t.tree()
	then := tx.t.copy()
	then.projects[name] = next
	after := then.tree()
	if lost := lostByMove(tx.t, before, after, before.Subtree(name)); len(lost) > 0 {
		return Project{}, fmt.Errorf("%w: under %s, %s", ErrInUse, display(parent), strings.Join(lost, ", "))
	}
	if leaving != nil {
		if err := leaving(before, after, before.leftBehind(name, parent)); err != nil {
			return Project{}, err
		}
	}
	return s.replace(tx, cur, next, w.By, at, Moved)
}

// lostByMove names what in the projects moved, names, would lose an agent
// that is available there before the move and not after it: a project's
// default, a channel's default, and an agent that lives in one of them,
// which would be available nowhere. A default that stopped being available
// is refused rather than passed over, so every untagged message there would
// be answered by no one, and an agent unavailable in its own home answers
// nothing at all. An archived channel takes no new threads and its default
// cannot change, so it holds no move back.
func lostByMove(t *tenant, before, after *Tree, names []string) []string {
	lost := func(project, agent string) bool {
		return agent != "" && agent != Auto && before.IsAvailable(project, agent) && !after.IsAvailable(project, agent)
	}
	var out []string
	moved := make(map[string]bool, len(names))
	for _, n := range names {
		moved[n] = true
		if d := before.nodes[n].p.Agents.Default; lost(n, d) {
			out = append(out, fmt.Sprintf("the default of %s, %s, would not be available", display(n), d))
		}
		for _, a := range before.Homes(n) {
			if lost(n, a) {
				out = append(out, fmt.Sprintf("%s, which lives in %s, would not be available there", a, display(n)))
			}
		}
	}
	var channels []Channel
	for _, c := range t.channels {
		if moved[c.Project] && !c.Archived && lost(c.Project, c.Default) {
			channels = append(channels, c)
		}
	}
	slices.SortFunc(channels, func(a, b Channel) int {
		return strings.Compare(channelKey(a.Project, a.Name), channelKey(b.Project, b.Name))
	})
	for _, c := range channels {
		out = append(out, fmt.Sprintf("the default of #%s in %s, %s, would not be available", c.Name, display(c.Project), c.Default))
	}
	return out
}
