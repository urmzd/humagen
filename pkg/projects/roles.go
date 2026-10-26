package projects

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
)

// Role is what a member may do in a project: None < Viewer < Member < Owner.
// A role only narrows what a tenant permission allows: an owner without
// projects:write still cannot write.
type Role string

const (
	// None is no role: the project is not the reader's to see.
	None Role = ""
	// Viewer sees the project, its inbox, its context and its people.
	Viewer Role = "viewer"
	// Member works in it: creates projects under it, proposes notes.
	Member Role = "member"
	// Owner changes it and everything below it.
	Owner Role = "owner"
)

func (r Role) rank() int {
	switch r {
	case Viewer:
		return 1
	case Member:
		return 2
	case Owner:
		return 3
	}
	return 0
}

func (r Role) valid() bool { return r == Viewer || r == Member || r == Owner }

// AtLeast reports whether the role is need or above. Every role is at least
// None.
func (r Role) AtLeast(need Role) bool { return r.rank() >= need.rank() }

// Valid reports whether a role can be granted: viewer, member or owner.
func (r Role) Valid() bool { return r.valid() }

// Everyone ("*", declared with the agent lists) is also the member entry
// that matches every reader who can see the parent; at the root, everyone who
// can see projects at all.

// RolePrefix (with the experts) also starts a member entry that matches the
// holders of a policy role, as "role:field-eng".

// How an Access says a role was reached, besides the entry that matched.
const (
	// ViaOwnerAbove is a role that comes from owning an ancestor.
	ViaOwnerAbove = "owner above"
	// ViaGovern is the break-glass permission, owner of every project.
	ViaGovern = "projects:govern"
)

// Entry is one member: who, and the role it grants. Who is Everyone,
// RolePrefix and a role's name, or a subject. A subject that starts with
// RolePrefix is refused, so the two never meet.
type Entry struct {
	Who  string `json:"who"`
	Role Role   `json:"role"`
}

// Bounds on an entry.
const (
	maxSubject = 255
	maxRole    = 64
)

var validRoleName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]{0,63}$`)

// ValidRole reports whether a name can name a policy role, as a member entry
// role:<name> names it.
func ValidRole(name string) bool { return validRoleName.MatchString(name) }

// validWho checks one entry's who.
func validWho(who string) error {
	switch {
	case who == Everyone:
		return nil
	case strings.HasPrefix(who, RolePrefix):
		if name := strings.TrimPrefix(who, RolePrefix); !validRoleName.MatchString(name) {
			return fmt.Errorf("%w: %q names no role: a role is letters, digits, dashes and underscores, at most %d", ErrInvalid, who, maxRole)
		}
		return nil
	case who == "" || len(who) > maxSubject || !utf8.ValidString(who) || strings.TrimSpace(who) != who || !oneLine(who):
		return fmt.Errorf("%w: a member is *, role:<name> or a subject of at most %d bytes", ErrInvalid, maxSubject)
	}
	return nil
}

// validMembers checks a project's entries: present, at most MaxMembers, each
// well formed and granting a role, and no one named twice.
func validMembers(ms []Entry) error {
	if ms == nil {
		return fmt.Errorf("%w: a project lists its members", ErrInvalid)
	}
	if len(ms) > MaxMembers {
		return fmt.Errorf("%w: a project has at most %d members", ErrInvalid, MaxMembers)
	}
	seen := make(map[string]bool, len(ms))
	for _, m := range ms {
		if err := validWho(m.Who); err != nil {
			return err
		}
		if !m.Role.valid() {
			return fmt.Errorf("%w: %s is given %q; a role is viewer, member or owner", ErrInvalid, m.Who, m.Role)
		}
		if seen[m.Who] {
			return fmt.Errorf("%w: %s is listed twice", ErrInvalid, m.Who)
		}
		seen[m.Who] = true
	}
	return nil
}

// DefaultMembers is who a new project has when its creator names nobody:
// everyone as a member and the creator as owner. An account-like project is
// members only: its creator owns it and nobody else is in it, since a
// customer account's work is its team's. A creator that cannot be an entry,
// such as the platform file, is left out, so only projects:govern owns the
// project; an account the file declares with no members is opened to
// everyone as a viewer, or nobody but projects:govern would see it.
func DefaultMembers(like Kind, creator string) []Entry {
	named := creator != Everyone && !strings.HasPrefix(creator, RolePrefix) && validWho(creator) == nil
	var out []Entry
	switch {
	case like != Account:
		out = append(out, Entry{Who: Everyone, Role: Member})
	case !named:
		out = append(out, Entry{Who: Everyone, Role: Viewer})
	}
	if named {
		out = append(out, Entry{Who: creator, Role: Owner})
	}
	return out
}

// legacyMembers is what a record from before roles reads as: everyone owns a
// project, which is what any operator could do to it, and everyone is a
// member of the root, which projects:govern owns.
func legacyMembers(name string) []Entry {
	if name == Root {
		return []Entry{{Who: Everyone, Role: Member}}
	}
	return []Entry{{Who: Everyone, Role: Owner}}
}

// everyoneOf is the role a project's Everyone entry grants, None without one.
func everyoneOf(ms []Entry) Role {
	for _, m := range ms {
		if m.Who == Everyone {
			return m.Role
		}
	}
	return None
}

// openness ranks how widely an Everyone entry opens a project: none, viewer,
// member. Owner opens no wider than member; it is ownership, not reach.
func openness(r Role) int { return min(r.rank(), Member.rank()) }

// checkOpen holds a project's Everyone entry to the tree around it. A project
// with none is members only: read by its members alone. A company project,
// the root among them, is open to everyone as a member; no project is more
// open than its parent, or less open than a child; and ownership flows down,
// so a project everyone owns has no members-only project anywhere below it,
// and a members-only project has no project everyone owns above it. The
// projects below p go with it, so a move under a project everyone owns is
// held to every members-only project it carries, however deep.
func checkOpen(tree *Tree, p Project, parent *Project, children []Project) error {
	if p.PersonalOwner != "" {
		return nil
	}
	star := everyoneOf(p.Members)
	if owned, ok := ownedAbove(tree, parent); ok {
		if star == None {
			return fmt.Errorf("%w: everyone owns %s, and owners above own everything below, so %s cannot be members only; name its owners first", ErrInvalid, display(owned), display(p.Name))
		}
		for _, below := range tree.Subtree(p.Name) {
			if q, ok := tree.Get(below); ok && below != p.Name && q.PersonalOwner == "" && everyoneOf(q.Members) == None {
				return fmt.Errorf("%w: everyone owns %s, and owners above own everything below, so %s cannot go under it while %s is members only; name its owners first", ErrInvalid, display(owned), display(p.Name), display(q.Name))
			}
		}
	}
	if star == Owner {
		for _, below := range tree.Subtree(p.Name) {
			if q, ok := tree.Get(below); ok && below != p.Name && q.PersonalOwner == "" && everyoneOf(q.Members) == None {
				return fmt.Errorf("%w: %s is members only, so everyone cannot own %s above it", ErrInvalid, display(q.Name), display(p.Name))
			}
		}
	}
	switch {
	case p.Kind == Company && !star.AtLeast(Member):
		return fmt.Errorf("%w: %s is company-wide, so everyone (*) is at least a member", ErrInvalid, display(p.Name))
	case parent != nil && openness(star) > openness(everyoneOf(parent.Members)):
		return fmt.Errorf("%w: %s would be more open than %s; a project is never more open than its parent", ErrInvalid, display(p.Name), display(parent.Name))
	}
	for _, c := range children {
		if openness(everyoneOf(c.Members)) > openness(star) {
			return fmt.Errorf("%w: the sub-project %s would be more open than %s; narrow it first", ErrInvalid, c.Name, display(p.Name))
		}
	}
	return nil
}

// ownedAbove is the nearest project everyone owns on the chain from parent
// up to the root, parent included. A nil parent (the root) has none above.
func ownedAbove(tree *Tree, parent *Project) (string, bool) {
	if parent == nil {
		return "", false
	}
	if everyoneOf(parent.Members) == Owner {
		return parent.Name, true
	}
	above := tree.Ancestors(parent.Name)
	if !parent.IsRoot() {
		above = append(above, Root)
	}
	for _, name := range above {
		if q, ok := tree.Get(name); ok && everyoneOf(q.Members) == Owner {
			return q.Name, true
		}
	}
	return "", false
}

// limitOpenness lowers an Everyone entry to what a parent allows, so a
// project made with the default members fits where it is put.
func limitOpenness(ms []Entry, parent Project) []Entry {
	most := everyoneOf(parent.Members)
	if most == Owner {
		most = Member
	}
	if most == None {
		// Under a members-only project, a new one is members only too.
		return slices.DeleteFunc(ms, func(m Entry) bool { return m.Who == Everyone })
	}
	for i, m := range ms {
		if m.Who == Everyone && openness(m.Role) > openness(most) {
			ms[i].Role = most
		}
	}
	return ms
}

// openUnderOwned keeps a new project with the default members out of being
// members only where everyone owns a project above it, which owns everything
// below and so can have nothing members only there: it is opened to everyone
// as a viewer instead, what the account kind gives where it can.
func openUnderOwned(tree *Tree, ms []Entry, parent Project) []Entry {
	if everyoneOf(ms) != None {
		return ms
	}
	for _, q := range tree.Chain(parent.Name) {
		if everyoneOf(q.Members) == Owner {
			return append([]Entry{{Who: Everyone, Role: Viewer}}, ms...)
		}
	}
	return ms
}

// Reader is who is asking: their subject, the policy roles they hold,
// whether they hold projects:govern, and whether they are a machine. A
// machine matches only Everyone and its own subject. A reader with no
// subject matches nothing.
type Reader struct {
	Subject string
	Roles   []string
	Govern  bool
	Machine bool
}

func (r Reader) matches(who string) bool {
	switch {
	case r.Subject == "":
		return false
	case who == Everyone:
		return true
	case strings.HasPrefix(who, RolePrefix):
		return !r.Machine && slices.Contains(r.Roles, strings.TrimPrefix(who, RolePrefix))
	}
	return who == r.Subject
}

// specificity orders entries that grant the same role, so an Access names the
// most particular one: a subject, then a role, then everyone.
func specificity(who string) int {
	switch {
	case who == Everyone:
		return 0
	case strings.HasPrefix(who, RolePrefix):
		return 1
	}
	return 2
}

// Access is a reader's role in one project: the role, the project it comes
// from, and the entry or rule that gave it (an entry's who, ViaOwnerAbove or
// ViaGovern).
type Access struct {
	Role Role   `json:"role"`
	From string `json:"from"`
	Via  string `json:"via"`
}

// step is a reader's access at one project given their access at its parent.
// top is true at the root, which has no parent.
func step(p Project, above Access, r Reader, top bool) Access {
	if p.PersonalOwner != "" {
		if !r.Machine && r.Subject == p.PersonalOwner {
			return Access{Role: Owner, From: p.Name, Via: r.Subject}
		}
		return Access{}
	}
	if above.Role == Owner {
		return Access{Role: Owner, From: above.From, Via: ViaOwnerAbove}
	}
	if !top && above.Role == None {
		return Access{}
	}
	best, bestWho := Access{}, -1
	for _, m := range p.Members {
		if !r.matches(m.Who) {
			continue
		}
		if m.Role.rank() > best.Role.rank() || m.Role == best.Role && specificity(m.Who) > bestWho {
			best, bestWho = Access{Role: m.Role, From: p.Name, Via: m.Who}, specificity(m.Who)
		}
	}
	return best
}

// Resolve returns a reader's role in a project. It is read along the chain,
// root first: at each level the best entry that matches the reader; an owner
// above makes every level below owner; no role at a level means no role
// below it. projects:govern is owner everywhere. A project the tree does not
// hold, or a reader with no subject, has no role.
func (t *Tree) Resolve(name string, r Reader) Access {
	if t.PendingFork(name) {
		return Access{}
	}
	chain := t.Chain(name)
	if chain == nil || r.Subject == "" || (r.Machine && chain[0].PersonalOwner != "") {
		return Access{}
	}
	var a Access
	for i := len(chain) - 1; i >= 0; i-- {
		a = step(chain[i], a, r, i == len(chain)-1)
	}
	if a.Role != Owner && r.Govern {
		return Access{Role: Owner, From: Root, Via: ViaGovern}
	}
	return a
}

// DeletedRole returns a reader's role in a project that was deleted, from its
// tombstone: its members when it was deleted, under its parent as the tree
// holds the parent now, so a parent made members only since narrows it as it
// would have. A parent deleted too is read from its own tombstone, through
// tomb, at most deletedHops levels up. A tombstone that kept no members, a
// reader with no subject, or a parent found nowhere, has no role.
// projects:govern is owner, as everywhere.
func (t *Tree) DeletedRole(x Tombstone, r Reader, tomb func(name string) (Tombstone, bool)) Access {
	if r.Govern && r.Subject != "" {
		return Access{Role: Owner, From: Root, Via: ViaGovern}
	}
	chain := []Tombstone{x}
	for len(chain) <= deletedHops && !t.Has(chain[len(chain)-1].Parent) {
		up, ok := tomb(chain[len(chain)-1].Parent)
		if !ok {
			return Access{}
		}
		chain = append(chain, up)
	}
	last := chain[len(chain)-1]
	if !t.Has(last.Parent) || r.Subject == "" {
		return Access{}
	}
	a := t.Resolve(last.Parent, r)
	for i := len(chain) - 1; i >= 0; i-- {
		if !chain[i].Kept {
			return Access{}
		}
		a = step(Project{Name: chain[i].Name, Parent: chain[i].Parent, Members: chain[i].Members, PersonalOwner: chain[i].PersonalOwner}, a, r, false)
	}
	return a
}

// deletedHops bounds how many deleted parents DeletedRole reads up through.
const deletedHops = 16

// InheritedOwner is an owner entry on an ancestor, which owns the project too.
type InheritedOwner struct {
	Who  string `json:"who"`
	From string `json:"from"`
}

// InheritedOwners returns the owner entries of a project's ancestors, nearest
// first, the root's last. It is empty for the root and for a name the tree
// does not hold.
func (t *Tree) InheritedOwners(name string) []InheritedOwner {
	out := []InheritedOwner{}
	chain := t.Chain(name)
	if len(chain) < 2 || chain[0].PersonalOwner != "" {
		return out
	}
	for _, p := range chain[1:] {
		for _, m := range p.Members {
			if m.Role == Owner {
				out = append(out, InheritedOwner{Who: m.Who, From: p.Name})
			}
		}
	}
	return out
}

// Roles returns the reader's access to every project the tree holds, the
// root's "" among them, as Resolve would answer for each: one walk of the
// tree, for a listing that shows each row's role.
func (t *Tree) Roles(r Reader) map[string]Access {
	access := make(map[string]Access, len(t.order))
	for _, name := range t.order {
		n := t.nodes[name]
		a := Access{}
		if r.Subject != "" {
			a = step(n.p, access[n.p.Parent], r, name == Root)
		}
		if t.PendingFork(name) {
			a = Access{}
		}
		access[name] = a
	}
	if r.Govern && r.Subject != "" {
		for name, a := range access {
			if !t.PendingFork(name) && a.Role != Owner && (!r.Machine || t.nodes[name].p.PersonalOwner == "") {
				access[name] = Access{Role: Owner, From: Root, Via: ViaGovern}
			}
		}
	}
	return access
}

// Owned returns, in tree order, every project the reader owns: through an
// owner entry there or above, or projects:govern. A reader with no subject
// owns nothing.
func (t *Tree) Owned(r Reader) []string {
	out := []string{}
	if r.Subject == "" {
		return out
	}
	access := t.Roles(r)
	for _, name := range t.order {
		if access[name].Role == Owner {
			out = append(out, name)
		}
	}
	return out
}

// Memberships returns, in tree order, every project where the reader is at
// least a member, and every ancestor of those: the projects whose
// announcements reach them. projects:govern is left out: it owns every
// project for break-glass, and is a member of none for it.
func (t *Tree) Memberships(r Reader) []string {
	r.Govern = false
	if r.Subject == "" {
		return []string{}
	}
	access := t.Roles(r)
	in := map[string]bool{}
	for _, name := range t.order {
		if !access[name].Role.AtLeast(Member) {
			continue
		}
		for at := name; !in[at]; at = t.nodes[at].p.Parent {
			in[at] = true
			if at == Root {
				break
			}
		}
	}
	out := []string{}
	for _, name := range t.order {
		if in[name] {
			out = append(out, name)
		}
	}
	return out
}

// Named returns, in tree order, every project that names the reader: one
// where they are a member or an owner through an entry naming them, their
// subject or a role they hold, on the project itself or as an owner of a
// project above it. A role reached only through Everyone, or only through
// projects:govern, names nobody, and neither does an entry on a project the
// reader could not see without projects:govern. These are a person's own
// projects, as Home lists them. A reader with no subject is named nowhere.
func (t *Tree) Named(r Reader) []string {
	r.Govern = false
	out := []string{}
	if r.Subject == "" {
		return out
	}
	access := t.Roles(r)
	// owns holds the projects the reader owns through an entry naming them,
	// there or above: the parent always comes before its children.
	owns := map[string]bool{}
	for _, name := range t.order {
		if access[name].Role == None {
			continue
		}
		p := t.nodes[name].p
		above := !p.IsRoot() && owns[p.Parent]
		named, owner := above, above
		for _, m := range p.Members {
			if m.Who == Everyone || !r.matches(m.Who) {
				continue
			}
			named = named || m.Role.AtLeast(Member)
			owner = owner || m.Role == Owner
		}
		owns[name] = owner
		if named {
			out = append(out, name)
		}
	}
	return out
}

// MembersOnly reports a project with no entry for everyone: read by its
// members alone.
func MembersOnly(p Project) bool { return everyoneOf(p.Members) == None }
