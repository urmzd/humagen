// Package projects keeps a workspace's tree of projects.
//
// A workspace is the root project, written "~" in addresses and "" here.
// Every other project has a parent fixed when it is created, at most Depth
// levels below the root. A project's name is its identity: it never changes,
// and everything filed under a project (runs, usage, policies, memory,
// schedules, alert rules) is keyed by it, so nothing moves when the tree
// grows. A title is what people see.
//
// # One consistent tree
//
// Every write replaces an immutable Tree, so admission, Converse and the API
// each read one consistent tree with no lock held while they do their own
// work. A write and its history entry are one state.Write: both are kept or
// neither is.
//
// # Default deny
//
// A Writer with no Govern changes no company project and not the root, and
// cannot reuse the name of a deleted project. A project the platform file
// declares cannot be deleted through the API, and the fields the file sets
// cannot be changed there.
package projects

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Errors a caller can match.
var (
	// ErrNotFound reports a project the tenant does not have.
	ErrNotFound = errors.New("projects: not found")
	// ErrExists reports a name already taken, ignoring case.
	ErrExists = errors.New("projects: already exists")
	// ErrInvalid reports a record, a kind rule or a depth the tree refuses.
	ErrInvalid = errors.New("projects: invalid")
	// ErrLimit reports a workspace that has as many projects, or company
	// projects, as it may.
	ErrLimit = errors.New("projects: limit reached")
	// ErrManaged reports a change through the API to what the platform file
	// owns.
	ErrManaged = errors.New("projects: managed by the platform file")
	// ErrInUse reports a project something still points at.
	ErrInUse = errors.New("projects: in use")
	// ErrVersion reports a write that names a version that is not the
	// current one.
	ErrVersion = errors.New("projects: changed since it was read")
	// ErrForbidden reports a change that needs projects:govern.
	ErrForbidden = errors.New("projects: forbidden")
)

// Root is the root project's name: the workspace itself.
const Root = ""

// RootAddress is how the root is written in an address, such as
// /api/projects/~. No project's name can start with it.
const RootAddress = "~"

// NameOf is the project an address segment names: "~" is the root.
func NameOf(segment string) string {
	if segment == RootAddress {
		return Root
	}
	return segment
}

// Kind is what a project is for.
type Kind string

const (
	// Company is company-wide: under the root or another company project.
	// It is created and changed only with projects:govern.
	Company Kind = "company"
	// Team is the default kind, and sits anywhere.
	Team Kind = "team"
	// Account is a customer account, never under another account.
	Account Kind = "account"
)

// ManagedFile marks a project the platform file declares.
const ManagedFile = "file"

// Fields the platform file can own. A field it owns is changed only by the
// file, and a change through the API answers ErrManaged.
const (
	FieldTitle        = "title"
	FieldDescription  = "description"
	FieldKind         = "kind"
	FieldParent       = "parent"
	FieldAccount      = "account"
	FieldInstructions = "instructions"
)

// Bounds.
const (
	MaxTitle        = 80
	MaxDescription  = 500
	MaxInstructions = 4000
	MaxCustomer     = 120
	MaxExternalID   = 120
	MaxURL          = 500
	// MaxHistory is how many changes a tenant keeps. The oldest are
	// dropped first, in the same write as the newest.
	MaxHistory = 1000
	// MaxTombstones is how many deleted names a tenant remembers.
	MaxTombstones = 1000
	// MaxPage bounds one page of history.
	MaxPage = 200
	// DefaultPage is a page of history when none is asked for.
	DefaultPage = 50
)

// Caps bound a tenant's tree.
type Caps struct {
	// Projects is the most projects a tenant may have, the root aside.
	Projects int
	// Depth is how many levels below the root a project may sit.
	Depth int
	// Children is the most projects one parent may have.
	Children int
	// Company is the most company projects a tenant may have.
	Company int
}

// DefaultCaps are phase 1's: 100 projects, 4 levels, 100 children and 10
// company projects.
func DefaultCaps() Caps { return Caps{Projects: 100, Depth: 4, Children: 100, Company: 10} }

// AccountInfo is what an account project records about its customer.
type AccountInfo struct {
	Customer   string `json:"customer,omitempty"`
	ExternalID string `json:"external_id,omitempty"`
	URL        string `json:"url,omitempty"`
}

// Project is one place in the tree.
type Project struct {
	// Name is the identity: immutable, unique ignoring case. "" is the root.
	Name string `json:"name"`
	// Title is what people see. The root shows the workspace's name
	// instead, so it has none.
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	// Kind defaults to team. The root is company.
	Kind Kind `json:"kind"`
	// Parent is fixed at creation. "" is the root.
	Parent string `json:"parent,omitempty"`
	// Account is set on account projects only.
	Account *AccountInfo `json:"account,omitempty"`
	// Instructions are sent to every agent answering here and below.
	Instructions string `json:"instructions,omitempty"`
	// Experts are who answers for some topics here and below. Never nil.
	Experts []Expert `json:"experts"`

	// Managed is ManagedFile when the platform file declares the project.
	Managed string `json:"managed,omitempty"`
	// FileOwns is the fields the platform file sets.
	FileOwns []string `json:"-"`
	// Version counts writes, from 1. Every write names the one it read.
	// The root reads 0 until it is first written.
	Version   int       `json:"version"`
	CreatedBy string    `json:"created_by,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedBy string    `json:"updated_by,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

// IsRoot reports whether the project is the workspace itself.
func (p Project) IsRoot() bool { return p.Name == Root }

func (p Project) clone() Project {
	if p.Account != nil {
		a := *p.Account
		p.Account = &a
	}
	p.FileOwns = slices.Clone(p.FileOwns)
	p.Experts = cloneExperts(p.Experts)
	return p
}

// Writer is who makes a change: the subject the change records, and whether
// they hold projects:govern, which the root, company projects and a deleted
// name need.
type Writer struct {
	By     string
	Govern bool
}

// What a change was.
const (
	Created = "created"
	Updated = "updated"
	Deleted = "deleted"
	// Managed is the platform file taking a project over or letting it go.
	Managed = "managed"
)

// AdoptsNote is recorded on a project created under a deleted name.
const AdoptsNote = "adopts its old conversations"

// Change is one entry of the record of what changed.
type Change struct {
	Seq     int64     `json:"seq"`
	At      time.Time `json:"at"`
	By      string    `json:"by"`
	Project string    `json:"project"`
	What    string    `json:"what"`
	// Note says more, such as that a project adopts its old conversations.
	Note   string   `json:"note,omitempty"`
	Before *Project `json:"before,omitempty"`
	After  *Project `json:"after,omitempty"`
	// Channel is set on a change to one of the project's channels.
	Channel *ChannelChange `json:"channel,omitempty"`
}

// Tombstone is a deleted name. It is reused only with projects:govern.
type Tombstone struct {
	Name string    `json:"name"`
	By   string    `json:"by,omitempty"`
	At   time.Time `json:"at"`
}

var validName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 _-]{0,62}$`)

// ValidName reports whether a name can name a project. The root's "" is not
// one: the root is never created.
func ValidName(name string) bool { return validName.MatchString(name) }

func validKind(k Kind) bool { return k == Company || k == Team || k == Account }

// accountLike is a kind that carries an account and never nests in another.
func accountLike(k Kind) bool { return k == Account }

var validField = map[string]bool{
	FieldTitle: true, FieldDescription: true, FieldKind: true, FieldParent: true, FieldAccount: true, FieldInstructions: true,
	FieldExperts: true,
}

// validate checks a project on its own: its fields and bounds, not its place.
func (p Project) validate() error {
	chars := utf8.RuneCountInString
	switch {
	case p.IsRoot() && p.Kind != Company:
		return fmt.Errorf("%w: the workspace is a company project", ErrInvalid)
	case p.IsRoot() && (p.Title != "" || p.Parent != "" || p.Account != nil):
		return fmt.Errorf("%w: the workspace has no title, parent or account of its own", ErrInvalid)
	case !p.IsRoot() && !ValidName(p.Name):
		return fmt.Errorf("%w: a project's name is letters, digits, spaces, dashes and underscores, starting with a letter or digit, at most 63", ErrInvalid)
	case !validKind(p.Kind):
		return fmt.Errorf("%w: a project's kind is company, team or account", ErrInvalid)
	case p.Parent != Root && !ValidName(p.Parent):
		return fmt.Errorf("%w: no project can be called %q", ErrInvalid, p.Parent)
	case strings.EqualFold(p.Parent, p.Name) && !p.IsRoot():
		return fmt.Errorf("%w: a project cannot be its own parent", ErrInvalid)
	case chars(p.Title) > MaxTitle || !oneLine(p.Title):
		return fmt.Errorf("%w: a title is one line of at most %d characters", ErrInvalid, MaxTitle)
	case chars(p.Description) > MaxDescription || !plain(p.Description):
		return fmt.Errorf("%w: a description is at most %d characters", ErrInvalid, MaxDescription)
	case chars(p.Instructions) > MaxInstructions || !plain(p.Instructions):
		return fmt.Errorf("%w: instructions are at most %d characters", ErrInvalid, MaxInstructions)
	case p.Account != nil && !accountLike(p.Kind):
		return fmt.Errorf("%w: only an account project carries an account", ErrInvalid)
	case p.Managed != "" && p.Managed != ManagedFile:
		return fmt.Errorf("%w: a project is managed by the file or not at all", ErrInvalid)
	}
	if a := p.Account; a != nil {
		switch {
		case chars(a.Customer) > MaxCustomer || !oneLine(a.Customer):
			return fmt.Errorf("%w: a customer is one line of at most %d characters", ErrInvalid, MaxCustomer)
		case chars(a.ExternalID) > MaxExternalID || !oneLine(a.ExternalID):
			return fmt.Errorf("%w: an external id is one line of at most %d characters", ErrInvalid, MaxExternalID)
		case a.URL != "" && !httpsURL(a.URL):
			return fmt.Errorf("%w: an account's link is an https URL of at most %d characters", ErrInvalid, MaxURL)
		}
	}
	for _, f := range p.FileOwns {
		if !validField[f] {
			return fmt.Errorf("%w: the file cannot own %q", ErrInvalid, f)
		}
	}
	return validExperts(p.Experts)
}

func oneLine(s string) bool {
	if !utf8.ValidString(s) {
		return false
	}
	return !strings.ContainsFunc(s, unicode.IsControl)
}

// plain is text of several lines with no other control characters.
func plain(s string) bool {
	if !utf8.ValidString(s) {
		return false
	}
	return !strings.ContainsFunc(s, func(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\t' && r != '\r' })
}

func httpsURL(s string) bool {
	if len(s) > MaxURL {
		return false
	}
	u, err := url.Parse(s)
	return err == nil && u.Scheme == "https" && u.Host != "" && u.User == nil
}

// Tree is one tenant's projects at one moment. It is never changed: a write
// replaces it, so a reader holds a consistent tree for as long as it likes.
type Tree struct {
	nodes map[string]*node
	// order is every name in tree order, the root first: each project
	// before its children, siblings by name ignoring case.
	order []string
}

type node struct {
	p        Project
	depth    int
	children []string
}

// virtualRoot is the root of a tenant that has written nothing yet.
func virtualRoot() Project { return Project{Name: Root, Kind: Company} }

// build makes a tree. Every parent must be in projects; build does not check.
func build(projects map[string]Project) *Tree {
	t := &Tree{nodes: make(map[string]*node, len(projects)+1)}
	root, ok := projects[Root]
	if !ok {
		root = virtualRoot()
	}
	t.nodes[Root] = &node{p: root}
	for name, p := range projects {
		if name != Root {
			t.nodes[name] = &node{p: p}
		}
	}
	for name, n := range t.nodes {
		if name == Root {
			continue
		}
		if parent, ok := t.nodes[n.p.Parent]; ok {
			parent.children = append(parent.children, name)
		}
	}
	for _, n := range t.nodes {
		slices.SortFunc(n.children, compareNames)
	}
	var walk func(name string, depth int)
	walk = func(name string, depth int) {
		n := t.nodes[name]
		n.depth = depth
		t.order = append(t.order, name)
		for _, c := range n.children {
			walk(c, depth+1)
		}
	}
	walk(Root, 0)
	return t
}

func compareNames(a, b string) int {
	if c := strings.Compare(strings.ToLower(a), strings.ToLower(b)); c != 0 {
		return c
	}
	return strings.Compare(a, b)
}

// Has reports whether the tree holds a project by this exact name. The root
// is always there.
func (t *Tree) Has(name string) bool {
	_, ok := t.nodes[name]
	return ok
}

// Get returns a project by its exact name.
func (t *Tree) Get(name string) (Project, bool) {
	n, ok := t.nodes[name]
	if !ok {
		return Project{}, false
	}
	return n.p.clone(), true
}

// Root returns the workspace's own record.
func (t *Tree) Root() Project { return t.nodes[Root].p.clone() }

// Len is how many projects the tree holds, the root aside.
func (t *Tree) Len() int { return len(t.nodes) - 1 }

// Projects returns every project but the root, in tree order.
func (t *Tree) Projects() []Project {
	out := make([]Project, 0, len(t.order)-1)
	for _, name := range t.order[1:] {
		out = append(out, t.nodes[name].p.clone())
	}
	return out
}

// Chain returns the project, then its ancestors nearest first, then the
// root. It is nil for a name the tree does not hold.
func (t *Tree) Chain(name string) []Project {
	n, ok := t.nodes[name]
	if !ok {
		return nil
	}
	out := []Project{n.p.clone()}
	for !n.p.IsRoot() {
		n = t.nodes[n.p.Parent]
		out = append(out, n.p.clone())
	}
	return out
}

// Ancestors returns the names of a project's ancestors nearest first, the root
// left out: what a run filed in the project also counts toward. It is nil for
// the root and for a name the tree does not hold.
func (t *Tree) Ancestors(name string) []string {
	n, ok := t.nodes[name]
	if !ok || n.p.IsRoot() {
		return nil
	}
	var out []string
	for n.p.Parent != Root {
		out = append(out, n.p.Parent)
		n = t.nodes[n.p.Parent]
	}
	return out
}

// Subtree returns the project and every project below it, in tree order. For
// the root it is every name, the root's "" first. It is nil for a name the
// tree does not hold.
func (t *Tree) Subtree(name string) []string {
	if _, ok := t.nodes[name]; !ok {
		return nil
	}
	var out []string
	var walk func(string)
	walk = func(n string) {
		out = append(out, n)
		for _, c := range t.nodes[n].children {
			walk(c)
		}
	}
	walk(name)
	return out
}

// Depth is how many levels below the root a project sits: 0 for the root, -1
// for a name the tree does not hold.
func (t *Tree) Depth(name string) int {
	n, ok := t.nodes[name]
	if !ok {
		return -1
	}
	return n.depth
}

// Children returns a project's children, by name ignoring case.
func (t *Tree) Children(name string) []Project {
	n, ok := t.nodes[name]
	if !ok {
		return nil
	}
	out := make([]Project, 0, len(n.children))
	for _, c := range n.children {
		out = append(out, t.nodes[c].p.clone())
	}
	return out
}

// Find returns the project whose name equals this one ignoring case.
func (t *Tree) Find(name string) (Project, bool) {
	for n, node := range t.nodes {
		if n != Root && strings.EqualFold(n, name) {
			return node.p.clone(), true
		}
	}
	return Project{}, false
}
