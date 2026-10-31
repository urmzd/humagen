// Package drive keeps versioned file trees: a person's memory, a project's
// memory, and an agent's memory in a project.
//
// A file is two records: its place and its content. The place is a manifest
// entry, a path and the SHA-256 of the content, kept with the tree's version
// history on a state.Store. The content is kept once in a blob.Store, under
// its hash, and is shared by every version and every run that reads it. A
// version is a whole manifest, so restoring is checking out an earlier one:
// a new version with the old entries, the history kept.
//
// # Who may do what
//
// Every call names an Actor. A person's tree is theirs alone: only the person
// it belongs to reads or writes it, whatever else the actor is granted. Any
// other tree is read, written, proposed to or approved only as the actor's
// Grants say, and a zero Actor is granted nothing. A write to a tree the actor
// may only propose to is a Proposal, applied when someone with Approve takes
// it.
//
// # Bounds
//
// A tree holds at most MaxEntries files of at most MaxFileBytes each, keeps
// its last MaxVersions versions, and holds at most MaxProposals open
// proposals. History and search answer at most MaxPage items per call.
//
// # Reads
//
// The store reads and writes the database per request (pkg/state, per
// request): it holds no head and no proposal in memory, so any number of
// processes on one database agree, and it does not need pkg/state's
// single-writer lock. Every change to a tree writes the tree's head at the
// version it read, together with the new version and the proposals it adds
// or closes, so two processes committing to one tree from the same head
// cannot both win: the loser reads the new head and applies its change
// again, or reports a conflict when a path it changed moved.
//
// What never changes once written is kept in a bounded cache: a version of
// one generation of a tree, content by hash, and the search index built from
// a version. A tree dropped and written again is a new generation, so
// dropping the cache never changes an answer. The platform and the leases it
// installs files into fetch a project's memory once, not once per run.
//
// Content is deduplicated within a tenant and not across tenants, for the
// reason fiber's blob store gives: a fast Put must not tell one tenant that
// another holds a file.
package drive

import (
	"errors"
	"fmt"
	"net/url"
	"path"
	"strings"
	"time"
	"unicode/utf8"
)

// Errors a caller can match.
var (
	ErrInvalid   = errors.New("drive: invalid")
	ErrNotFound  = errors.New("drive: not found")
	ErrForbidden = errors.New("drive: forbidden")
	ErrConflict  = errors.New("drive: the file changed since the version this change is based on")
	ErrTooLarge  = errors.New("drive: too large")
	// ErrRefused reports content the store's screen would not keep.
	ErrRefused = errors.New("drive: refused")
)

// Bounds.
const (
	MaxFileBytes = 1 << 20
	MaxEntries   = 1000
	MaxPathBytes = 512
	MaxVersions  = 200
	MaxProposals = 100
	MaxPage      = 100
	// MaxSearchTrees bounds how many trees one search reads.
	MaxSearchTrees = 50
	// maxQuery bounds a search query, in bytes.
	maxQuery = 1000
)

// Kind is what a tree belongs to.
type Kind string

const (
	// PersonKind is one person's own tree, mounted at /me/memory in their
	// runs only.
	PersonKind Kind = "person"
	// ProjectKind is a project's shared memory.
	ProjectKind Kind = "project"
	// AgentKind is one agent's memory in a project, mounted at
	// /project/memory/<agent>.
	AgentKind Kind = "agent"
)

// Tree names a tree. Project "" is the workspace, the root project.
type Tree struct {
	Kind    Kind   `json:"kind"`
	Person  string `json:"person,omitempty"`
	Project string `json:"project,omitempty"`
	Agent   string `json:"agent,omitempty"`
}

// Person is a person's tree.
func Person(subject string) Tree { return Tree{Kind: PersonKind, Person: subject} }

// Project is a project's shared tree.
func Project(name string) Tree { return Tree{Kind: ProjectKind, Project: name} }

// Agent is an agent's tree in a project.
func Agent(project, agent string) Tree { return Tree{Kind: AgentKind, Project: project, Agent: agent} }

// Valid reports whether the tree names exactly one owner.
func (t Tree) Valid() error {
	ok := false
	switch t.Kind {
	case PersonKind:
		ok = t.Person != "" && t.Project == "" && t.Agent == ""
	case ProjectKind:
		ok = t.Person == "" && t.Agent == ""
	case AgentKind:
		ok = t.Person == "" && t.Agent != ""
	}
	for _, s := range []string{t.Person, t.Project, t.Agent} {
		if len(s) > 255 || strings.ContainsRune(s, 0) {
			ok = false
		}
	}
	if !ok {
		return fmt.Errorf("%w: tree %+v", ErrInvalid, t)
	}
	return nil
}

// Key is the tree's stored name. Each part is escaped, so no name reaches
// another tree's key.
func (t Tree) Key() string {
	switch t.Kind {
	case PersonKind:
		return "person/" + url.PathEscape(t.Person)
	case AgentKind:
		return "agent/" + url.PathEscape(t.Project) + "/" + url.PathEscape(t.Agent)
	}
	return "project/" + url.PathEscape(t.Project)
}

// ProjectMount is the directory, under /project/memory, that a project's
// shared tree is installed at. An agent's name cannot hold "@", so it never
// collides with an agent's directory.
const ProjectMount = "@project"

// Mount is where the tree is installed in a run's files: /me/memory for a
// person, /project/memory/<agent> for an agent, and
// /project/memory/@project for the project's shared tree.
func (t Tree) Mount() string {
	switch t.Kind {
	case PersonKind:
		return "me/memory"
	case AgentKind:
		return "project/memory/" + t.Agent
	}
	return "project/memory/" + ProjectMount
}

// Entry is one file in a version.
type Entry struct {
	Path string `json:"path"`
	Hash string `json:"hash"`
	Size int64  `json:"size"`
}

// Version is a whole manifest and how it came to be.
type Version struct {
	Tree    Tree      `json:"tree"`
	N       int64     `json:"version"`
	Parent  int64     `json:"parent,omitempty"`
	Entries []Entry   `json:"entries"`
	By      string    `json:"by,omitempty"`
	At      time.Time `json:"at,omitzero"`
	Message string    `json:"message,omitempty"`
	// Origin is what wrote the version: a run's end, the API, a restore, the
	// memory notes.
	Origin string `json:"origin,omitempty"`
	// Restored is the version this one checked out, if it is a restore.
	Restored int64 `json:"restored,omitempty"`
	// Resolves are the proposals this version applied.
	Resolves []string `json:"resolves,omitempty"`
}

// Lookup returns the entry at a path.
func (v Version) Lookup(p string) (Entry, bool) {
	for _, e := range v.Entries {
		if e.Path == p {
			return e, true
		}
	}
	return Entry{}, false
}

// Change is one file written or deleted. Data is the content to write; Hash
// and Size are computed from it by the store.
type Change struct {
	Path   string `json:"path"`
	Hash   string `json:"hash,omitempty"`
	Size   int64  `json:"size,omitempty"`
	Delete bool   `json:"delete,omitempty"`
	Data   []byte `json:"-"`
}

// Proposal is a change to a tree that waits for someone with Approve.
type Proposal struct {
	ID      string    `json:"id"`
	Tree    Tree      `json:"tree"`
	Base    int64     `json:"base"`
	Changes []Change  `json:"changes"`
	By      string    `json:"by,omitempty"`
	At      time.Time `json:"at,omitzero"`
	Message string    `json:"message,omitempty"`
	Origin  string    `json:"origin,omitempty"`
}

// Grant is what an actor may do to a tree. The zero value grants nothing.
type Grant struct {
	Read    bool
	Write   bool
	Propose bool
	Approve bool
}

// Actor is who a call is made for.
type Actor struct {
	// Subject is the person. It alone decides a person's tree.
	Subject string
	// Grants decides every other tree. Nil grants nothing.
	Grants func(Tree) Grant
	system bool
}

// System is the platform's own actor: migrations, housekeeping and the
// memory adapter, which check access themselves before they call.
var System = Actor{system: true}

func (a Actor) grant(t Tree) Grant {
	if a.system {
		return Grant{Read: true, Write: true, Propose: true, Approve: true}
	}
	if t.Kind == PersonKind {
		if a.Subject != "" && a.Subject == t.Person {
			return Grant{Read: true, Write: true, Propose: true, Approve: true}
		}
		return Grant{}
	}
	if a.Grants == nil {
		return Grant{}
	}
	return a.Grants(t)
}

// CleanPath checks a path inside a tree and returns its canonical form.
func CleanPath(p string) (string, error) {
	if p == "" || len(p) > MaxPathBytes || !utf8.ValidString(p) || strings.ContainsAny(p, "\x00\\") || strings.HasPrefix(p, "/") {
		return "", fmt.Errorf("%w: path %q", ErrInvalid, p)
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return "", fmt.Errorf("%w: path %q", ErrInvalid, p)
		}
	}
	return path.Clean(p), nil
}
