// Package corpora keeps each workspace's corpora. A
// corpus is a named body of knowledge, such as "HR policies": a slice of the
// workspace graph that draws on sources. It records which sources it holds,
// who owns it, who may read it and whether it links to other corpora.
//
// A corpus holds no content. Indexed text stays in pkg/knowledge, warehouse
// schemas in pkg/catalog, and warehouse rows in the warehouse. Holding a
// source never widens who may read it: every read still checks the reader's
// own access to the source.
//
// It reads and writes per request on pkg/state and holds nothing in memory,
// so any number of processes on one database agree.
package corpora

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/urmzd/humagen/pkg/state"
)

// Bounds on one workspace's corpora.
const (
	MaxCorpora     = 200
	MaxMembers     = 50
	MaxReaders     = 50
	MaxAgents      = 50
	MaxDescription = 500
	MaxTitle       = 120
	MaxOntology    = 64 << 10
)

var (
	ErrInvalid  = errors.New("corpora: invalid")
	ErrNotFound = errors.New("corpora: not found")
	ErrExists   = errors.New("corpora: already exists")
	ErrConflict = errors.New("corpora: changed")
	ErrLimit    = errors.New("corpora: limit reached")
)

// MemberKind says what a member points at.
type MemberKind string

const (
	// Source is a knowledge source (pkg/knowledge): files it indexes, or
	// references it searches live with the reader's credential.
	Source MemberKind = "source"
	// Catalog is a catalog connector (pkg/catalog): warehouse tables and
	// views kept as schema and owners only, queried live.
	Catalog MemberKind = "catalog"
)

// Member is one source a corpus draws on, by its kind and name. A member
// whose source was deleted stays listed until someone removes it, so a
// corpus never changes silently.
type Member struct {
	Kind MemberKind `json:"kind"`
	Name string     `json:"name"`
}

// ReaderKind says who a reader entry names.
type ReaderKind string

const (
	// Person is one person, by subject.
	Person ReaderKind = "person"
	// Project is everyone who can see a project.
	Project ReaderKind = "project"
	// Workspace is everyone in the workspace who can read knowledge.
	Workspace ReaderKind = "workspace"
)

// Reader is one entry of who may read a corpus and grant it to agents.
type Reader struct {
	Kind ReaderKind `json:"kind"`
	Name string     `json:"name,omitempty"`
}

// Links says whether a corpus's graph links to other corpora.
type Links string

const (
	// Isolated links only within the corpus. The default.
	Isolated Links = "isolated"
	// Shared links to other shared corpora through shared entities.
	Shared Links = "shared"
)

// Corpus is one named body of knowledge.
type Corpus struct {
	Name string `json:"name"`
	// Title is what people call it, such as "HR policies". Empty shows
	// the name.
	Title       string   `json:"title,omitempty"`
	Description string   `json:"description,omitempty"`
	Owner       string   `json:"owner"`
	Readers     []Reader `json:"readers"`
	Members     []Member `json:"members"`
	// Agents are the agents granted the corpus: they may search it and
	// query its warehouses, always as the person a run acts for, who must
	// be a reader too.
	Agents []string `json:"agents"`
	Links  Links    `json:"links"`
	// GraphSearch adds the workspace graph to its search: documents by who
	// owns them, the systems they sit in and how stale they are.
	GraphSearch bool `json:"graph_search,omitempty"`
	// Ontology is the types the corpus adds to the workspace graph, a JSON
	// Schema the platform checks (pkg/graph). Proposed is one inferred for
	// the owner to accept; it adds nothing until accepted.
	Ontology json.RawMessage `json:"ontology,omitempty"`
	Proposed json.RawMessage `json:"proposed_ontology,omitempty"`
	// OntologySeen marks the last inferred proposal, so one discarded or
	// accepted is not proposed again.
	OntologySeen string    `json:"ontology_seen,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	// Version is the stored record's version. An update or delete that
	// names it fails with ErrConflict once someone else changed the corpus.
	Version int64 `json:"version"`
}

// Grants reports whether the corpus is granted to an agent.
func (c Corpus) Grants(agent string) bool { return slices.Contains(c.Agents, agent) }

// Holds reports whether the corpus has a member.
func (c Corpus) Holds(m Member) bool { return slices.Contains(c.Members, m) }

var validName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// ValidName reports whether a name can name a corpus or a member: lower
// case letters, digits and dashes, starting with a letter or digit, at most
// 63. A corpus name prefixes the tools it gives agents.
func ValidName(name string) bool { return validName.MatchString(name) }

// normal checks a corpus and returns it with its lists deduplicated, in the
// order given, and its links defaulted.
func normal(c Corpus) (Corpus, error) {
	c.Description, c.Title = strings.TrimSpace(c.Description), strings.TrimSpace(c.Title)
	switch {
	case !ValidName(c.Name):
		return c, fmt.Errorf("%w: a corpus name is lower case letters, digits and dashes, at most 63", ErrInvalid)
	case len(c.Description) > MaxDescription || !utf8.ValidString(c.Description):
		return c, fmt.Errorf("%w: a description is at most %d bytes", ErrInvalid, MaxDescription)
	case len(c.Title) > MaxTitle || !utf8.ValidString(c.Title) || strings.ContainsFunc(c.Title, unicode.IsControl):
		return c, fmt.Errorf("%w: a title is one line of at most %d bytes", ErrInvalid, MaxTitle)
	case len(c.Ontology) > MaxOntology || len(c.Proposed) > MaxOntology || (len(c.Ontology) > 0 && !json.Valid(c.Ontology)) || (len(c.Proposed) > 0 && !json.Valid(c.Proposed)):
		return c, fmt.Errorf("%w: an ontology is a JSON object of at most %d bytes", ErrInvalid, MaxOntology)
	case c.Owner == "" || len(c.Owner) > 200:
		return c, fmt.Errorf("%w: a corpus needs an owner", ErrInvalid)
	}
	if c.Links == "" {
		c.Links = Isolated
	}
	if c.Links != Isolated && c.Links != Shared {
		return c, fmt.Errorf("%w: links is isolated or shared", ErrInvalid)
	}
	members := []Member{}
	for _, m := range c.Members {
		if (m.Kind != Source && m.Kind != Catalog) || !ValidName(m.Name) {
			return c, fmt.Errorf("%w: a member is a source or a catalog connector, by name", ErrInvalid)
		}
		if !slices.Contains(members, m) {
			members = append(members, m)
		}
	}
	if len(members) > MaxMembers {
		return c, fmt.Errorf("%w: at most %d members", ErrInvalid, MaxMembers)
	}
	readers := []Reader{}
	for _, r := range c.Readers {
		r.Name = strings.TrimSpace(r.Name)
		switch r.Kind {
		case Person, Project:
			if r.Name == "" || len(r.Name) > 200 {
				return c, fmt.Errorf("%w: a %s reader needs a name", ErrInvalid, r.Kind)
			}
		case Workspace:
			if r.Name != "" {
				return c, fmt.Errorf("%w: the workspace reader has no name", ErrInvalid)
			}
		default:
			return c, fmt.Errorf("%w: a reader is a person, a project or the workspace", ErrInvalid)
		}
		if !slices.Contains(readers, r) {
			readers = append(readers, r)
		}
	}
	if len(readers) > MaxReaders {
		return c, fmt.Errorf("%w: at most %d readers", ErrInvalid, MaxReaders)
	}
	agents := []string{}
	for _, a := range c.Agents {
		if a == "" || len(a) > 64 || strings.ContainsFunc(a, unicode.IsSpace) || !utf8.ValidString(a) {
			return c, fmt.Errorf("%w: an agent is named by its name", ErrInvalid)
		}
		if !slices.Contains(agents, a) {
			agents = append(agents, a)
		}
	}
	if len(agents) > MaxAgents {
		return c, fmt.Errorf("%w: at most %d agents", ErrInvalid, MaxAgents)
	}
	c.Members, c.Readers, c.Agents = members, readers, agents
	return c, nil
}

// record is one stored document: a corpus, or the stamp every new corpus
// moves.
type record struct {
	Corpus *Corpus `json:"corpus,omitempty"`
	Stamp  int64   `json:"stamp,omitempty"`
}

// namespace is where corpora are kept. The tenant is the shard key; within
// it:
//
//	c/<name>  a corpus
//	h         a stamp every new corpus moves, so two processes creating
//	          corpora at once cannot both count under MaxCorpora
const (
	namespace = "corpora"
	headKey   = "h"
	prefix    = "c/"
)

func key(name string) string { return prefix + name }

// Store keeps each tenant's corpora on a state.Store, read and written per
// request.
type Store struct {
	kept *state.Table[record]
	dir  *state.Tenants
}

// New returns a store in this process's memory.
func New() *Store {
	s, err := Open(nil)
	if err != nil {
		panic(err) // a store in memory cannot fail to open
	}
	return s
}

// Open returns a store on st, which may be shared with other processes. A
// nil st keeps corpora in this process's memory.
func Open(st state.Store) (*Store, error) {
	if st == nil {
		st = state.NewMemory()
	}
	dir, err := state.OpenTenants(st, namespace)
	if err != nil {
		return nil, fmt.Errorf("corpora: tenants: %w", err)
	}
	return &Store{kept: state.NewTable[record](st, namespace), dir: dir}, nil
}

// EachTenant calls fn for every tenant that has ever made a corpus.
func (s *Store) EachTenant(fn func(tenant string) error) error { return s.dir.Each(fn) }

func shown(it state.Item[record]) (Corpus, bool) {
	if it.Value.Corpus == nil {
		return Corpus{}, false
	}
	c := *it.Value.Corpus
	c.Readers = slices.Clone(c.Readers)
	c.Members = slices.Clone(c.Members)
	c.Ontology, c.Proposed = bytes.Clone(c.Ontology), bytes.Clone(c.Proposed)
	c.Agents = slices.Clone(c.Agents)
	if c.Agents == nil {
		c.Agents = []string{}
	}
	c.Version = it.Version
	return c, true
}

// Get reads one corpus.
func (s *Store) Get(tenant, name string) (Corpus, error) {
	if !ValidName(name) {
		return Corpus{}, ErrNotFound
	}
	it, err := s.kept.Lookup(tenant, key(name))
	if errors.Is(err, state.ErrNotFound) {
		return Corpus{}, ErrNotFound
	}
	if err != nil {
		return Corpus{}, err
	}
	c, ok := shown(it)
	if !ok {
		return Corpus{}, ErrNotFound
	}
	return c, nil
}

// List reads a tenant's corpora by name.
func (s *Store) List(tenant string) ([]Corpus, error) {
	items, err := s.kept.All(tenant, prefix, MaxCorpora+1)
	if err != nil {
		return nil, err
	}
	out := []Corpus{}
	for _, it := range items {
		if c, ok := shown(it); ok {
			out = append(out, c)
		}
	}
	slices.SortFunc(out, func(a, b Corpus) int { return strings.Compare(a.Name, b.Name) })
	return out, nil
}

// Containing lists the names of a tenant's corpora that hold a member.
func (s *Store) Containing(tenant string, m Member) ([]string, error) {
	all, err := s.List(tenant)
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, c := range all {
		if c.Holds(m) {
			out = append(out, c.Name)
		}
	}
	return out, nil
}

// Create adds a corpus. It fails with ErrExists when the name is taken and
// ErrLimit at MaxCorpora.
func (s *Store) Create(tenant string, c Corpus, at time.Time) (Corpus, error) {
	if tenant == "" {
		return Corpus{}, fmt.Errorf("%w: no tenant", ErrInvalid)
	}
	c, err := normal(c)
	if err != nil {
		return Corpus{}, err
	}
	c.CreatedAt, c.UpdatedAt, c.Version = at.UTC(), at.UTC(), 0
	if err := s.dir.Mark(tenant); err != nil {
		return Corpus{}, err
	}
	err = state.Retry(func() error {
		if _, err := s.kept.Lookup(tenant, key(c.Name)); err == nil {
			return ErrExists
		} else if !errors.Is(err, state.ErrNotFound) {
			return err
		}
		// Read the head before counting: a corpus created after this read
		// moves the head, so the write below conflicts and counts again.
		var head int64
		if it, err := s.kept.Lookup(tenant, headKey); err == nil {
			head = it.Version
		} else if !errors.Is(err, state.ErrNotFound) {
			return err
		}
		have, err := s.kept.All(tenant, prefix, MaxCorpora+1)
		if err != nil {
			return err
		}
		if len(have) >= MaxCorpora {
			return fmt.Errorf("%w: at most %d corpora", ErrLimit, MaxCorpora)
		}
		b := s.kept.Batch()
		b.Create(tenant, key(c.Name), record{Corpus: &c})
		if head == 0 {
			b.Create(tenant, headKey, record{Stamp: 1})
		} else {
			b.PutAt(tenant, headKey, record{Stamp: head + 1}, head)
		}
		return b.Commit()
	})
	if err != nil {
		return Corpus{}, err
	}
	return s.Get(tenant, c.Name)
}

// Update changes a corpus with fn, which may change anything but its name
// and dates. A version other than zero must be the corpus's current one,
// else it fails with ErrConflict; zero retries over concurrent changes.
func (s *Store) Update(tenant, name string, version int64, at time.Time, fn func(*Corpus) error) (Corpus, error) {
	err := state.Retry(func() error {
		it, err := s.kept.Lookup(tenant, key(name))
		if errors.Is(err, state.ErrNotFound) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		old, ok := shown(it)
		if !ok {
			return ErrNotFound
		}
		if version != 0 && version != it.Version {
			return ErrConflict
		}
		next := old
		if err := fn(&next); err != nil {
			return err
		}
		next.Name, next.CreatedAt, next.UpdatedAt = old.Name, old.CreatedAt, at.UTC()
		if next, err = normal(next); err != nil {
			return err
		}
		next.Version = 0
		b := s.kept.Batch()
		b.PutAt(tenant, key(name), record{Corpus: &next}, it.Version)
		err = b.Commit()
		if errors.Is(err, state.ErrConflict) && version != 0 {
			return ErrConflict
		}
		return err
	})
	if err != nil {
		return Corpus{}, err
	}
	return s.Get(tenant, name)
}

// Delete removes a corpus. A version other than zero must be its current
// one, else it fails with ErrConflict.
func (s *Store) Delete(tenant, name string, version int64) error {
	return state.Retry(func() error {
		it, err := s.kept.Lookup(tenant, key(name))
		if errors.Is(err, state.ErrNotFound) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if _, ok := shown(it); !ok {
			return ErrNotFound
		}
		if version != 0 && version != it.Version {
			return ErrConflict
		}
		b := s.kept.Batch()
		b.DeleteAt(tenant, key(name), it.Version)
		err = b.Commit()
		if errors.Is(err, state.ErrConflict) && version != 0 {
			return ErrConflict
		}
		return err
	})
}
