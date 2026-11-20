// Package graph keeps each workspace's knowledge graph: people, teams,
// projects, systems, topics and documents, and how they relate. Corpora are
// slices of it: each corpus contributes the nodes and edges built from its
// members, and every node and edge records which corpora it came from.
//
// This package holds the structural layer, built from source metadata with
// no model calls: documents, who added and owns them, the systems they sit
// in, and how stale each is. It keeps one slice per corpus and the owner
// decisions people make, and merges slices on read. Who may read what is the
// platform's to decide: a slice holds what its members hold, and a read
// keeps only what the reader may see.
//
// It reads and writes per request on pkg/state and holds nothing in memory.
package graph

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/urmzd/humagen/pkg/state"
)

// Core types every workspace graph has. A corpus ontology adds its own.
const (
	Person   = "person"
	Team     = "team"
	Project  = "project"
	System   = "system"
	Topic    = "topic"
	Document = "document"
)

// Core relations.
const (
	Authored    = "authored"
	Edited      = "edited"
	Owns        = "owns"
	About       = "about"
	Mentions    = "mentions"
	Supersedes  = "supersedes"
	Contradicts = "contradicts"
	DependsOn   = "depends_on"
	PartOf      = "part_of"
)

// Shared reports whether a node type resolves once across the workspace,
// whatever corpus it came from: people, teams and projects.
func Shared(typ string) bool { return typ == Person || typ == Team || typ == Project }

// Bounds on one corpus's slice and on a workspace's owner decisions.
const (
	MaxSliceNodes = 4000
	MaxSliceEdges = 8000
	MaxDecisions  = 10000
)

var (
	ErrInvalid  = errors.New("graph: invalid")
	ErrNotFound = errors.New("graph: not found")
	ErrLimit    = errors.New("graph: limit reached")
)

// Node is one entity. Its ID is a short hash of its key, safe in a path.
type Node struct {
	ID   string `json:"id"`
	Key  string `json:"key"`
	Type string `json:"type"`
	Name string `json:"name"`
	URI  string `json:"uri,omitempty"`
	// Member is the corpus member it was built from, as "source/<name>" or
	// "catalog/<name>"; empty for people, teams and projects.
	Member string            `json:"member,omitempty"`
	Props  map[string]string `json:"props,omitempty"`
	// ChangedAt is when its content last changed, as its source says.
	ChangedAt *time.Time `json:"changed_at,omitempty"`
	// Suggested is the owner the build infers: who added it, or whose
	// connection it came through. A person's decision overrides it.
	Suggested string `json:"suggested_owner,omitempty"`
	// OutOfDate is set when its source's last sync failed or went stale.
	OutOfDate bool `json:"out_of_date,omitempty"`
	// Corpora are the corpora it came from, filled on read.
	Corpora []string `json:"corpora,omitempty"`
}

// Edge is one relation between two nodes, by their IDs.
type Edge struct {
	From    string   `json:"from"`
	To      string   `json:"to"`
	Type    string   `json:"type"`
	Corpora []string `json:"corpora,omitempty"`
}

// ID is the node ID for a key.
func ID(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:10])
}

// NewNode is a node of a type and key, with its ID.
func NewNode(typ, key, name string) Node {
	return Node{ID: ID(key), Key: key, Type: typ, Name: name}
}

// Slice is what one corpus contributes, built whole.
type Slice struct {
	Corpus    string    `json:"corpus"`
	Nodes     []Node    `json:"nodes"`
	Edges     []Edge    `json:"edges"`
	BuiltAt   time.Time `json:"built_at"`
	Truncated bool      `json:"truncated,omitempty"`
}

// Decision is what a person decided about a node's owner.
type Decision struct {
	Node string `json:"node"`
	// Owner is who owns it; empty when declined and not handed on.
	Owner string    `json:"owner,omitempty"`
	State string    `json:"state"`
	By    string    `json:"by"`
	At    time.Time `json:"at"`
}

// Decision states.
const (
	Confirmed = "confirmed"
	Declined  = "declined"
)

func check(s Slice) error {
	if s.Corpus == "" || len(s.Nodes) > MaxSliceNodes || len(s.Edges) > MaxSliceEdges {
		return fmt.Errorf("%w: a slice names its corpus and holds at most %d nodes and %d edges", ErrInvalid, MaxSliceNodes, MaxSliceEdges)
	}
	ids := make(map[string]bool, len(s.Nodes))
	for _, n := range s.Nodes {
		if n.Key == "" || n.Type == "" || n.ID != ID(n.Key) || ids[n.ID] {
			return fmt.Errorf("%w: node %q", ErrInvalid, n.Key)
		}
		ids[n.ID] = true
	}
	for _, e := range s.Edges {
		if e.Type == "" || !ids[e.From] || !ids[e.To] {
			return fmt.Errorf("%w: an edge joins two nodes of its slice", ErrInvalid)
		}
	}
	return nil
}

type record struct {
	Slice    *Slice    `json:"slice,omitempty"`
	Decision *Decision `json:"decision,omitempty"`
}

// namespace is where graphs are kept. The tenant is the shard key; within
// it:
//
//	s/<corpus>  one corpus's slice
//	o/<node>    an owner decision
const (
	namespace      = "graph"
	slicePrefix    = "s/"
	decisionPrefix = "o/"
)

// Store keeps each tenant's graph on a state.Store, per request.
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

// Open returns a store on st. A nil st keeps the graph in this process's
// memory.
func Open(st state.Store) (*Store, error) {
	if st == nil {
		st = state.NewMemory()
	}
	dir, err := state.OpenTenants(st, namespace)
	if err != nil {
		return nil, fmt.Errorf("graph: tenants: %w", err)
	}
	return &Store{kept: state.NewTable[record](st, namespace), dir: dir}, nil
}

// PutSlice replaces a corpus's slice whole.
func (s *Store) PutSlice(tenant string, sl Slice) error {
	if err := check(sl); err != nil {
		return err
	}
	if err := s.dir.Mark(tenant); err != nil {
		return err
	}
	sl.BuiltAt = sl.BuiltAt.UTC()
	return s.kept.Put(tenant, slicePrefix+sl.Corpus, record{Slice: &sl})
}

// Slice reads a corpus's slice.
func (s *Store) Slice(tenant, corpus string) (Slice, error) {
	it, err := s.kept.Lookup(tenant, slicePrefix+corpus)
	if errors.Is(err, state.ErrNotFound) || err == nil && it.Value.Slice == nil {
		return Slice{}, ErrNotFound
	}
	if err != nil {
		return Slice{}, err
	}
	return *it.Value.Slice, nil
}

// DeleteSlice removes a corpus's slice, as when the corpus is deleted.
func (s *Store) DeleteSlice(tenant, corpus string) error {
	err := s.kept.Delete(tenant, slicePrefix+corpus)
	if errors.Is(err, state.ErrNotFound) {
		return nil
	}
	return err
}

// Decide keeps a person's decision about a node's owner.
func (s *Store) Decide(tenant string, d Decision) error {
	if d.Node == "" || (d.State != Confirmed && d.State != Declined) || d.By == "" || (d.State == Confirmed && d.Owner == "") {
		return fmt.Errorf("%w: a decision names a node, a state and who decided", ErrInvalid)
	}
	if _, err := s.kept.Lookup(tenant, decisionPrefix+d.Node); errors.Is(err, state.ErrNotFound) {
		have, err := s.kept.All(tenant, decisionPrefix, MaxDecisions+1)
		if err != nil {
			return err
		}
		if len(have) >= MaxDecisions {
			return fmt.Errorf("%w: at most %d owner decisions", ErrLimit, MaxDecisions)
		}
	} else if err != nil {
		return err
	}
	if err := s.dir.Mark(tenant); err != nil {
		return err
	}
	d.At = d.At.UTC()
	return s.kept.Put(tenant, decisionPrefix+d.Node, record{Decision: &d})
}

// Decisions reads a tenant's owner decisions by node.
func (s *Store) Decisions(tenant string) (map[string]Decision, error) {
	items, err := s.kept.All(tenant, decisionPrefix, MaxDecisions+1)
	if err != nil {
		return nil, err
	}
	out := make(map[string]Decision, len(items))
	for _, it := range items {
		if d := it.Value.Decision; d != nil {
			out[d.Node] = *d
		}
	}
	return out, nil
}

// EachTenant calls fn for every tenant with a graph.
func (s *Store) EachTenant(fn func(tenant string) error) error { return s.dir.Each(fn) }

// Graph is slices merged: each node and edge once, naming every corpus it
// came from.
type Graph struct {
	Nodes []Node `json:"nodes"`
	Edges []Edge `json:"edges"`
}

// Merge joins slices. A node in several slices keeps the first slice's
// fields and names every corpus.
func Merge(slices_ []Slice) Graph {
	nodes := map[string]*Node{}
	var order []string
	edges := map[[3]string]*Edge{}
	var edgeOrder [][3]string
	for _, sl := range slices_ {
		for _, n := range sl.Nodes {
			if have, ok := nodes[n.ID]; ok {
				if !slices.Contains(have.Corpora, sl.Corpus) {
					have.Corpora = append(have.Corpora, sl.Corpus)
				}
				continue
			}
			n.Corpora = []string{sl.Corpus}
			nodes[n.ID] = &n
			order = append(order, n.ID)
		}
		for _, e := range sl.Edges {
			k := [3]string{e.From, e.Type, e.To}
			if have, ok := edges[k]; ok {
				if !slices.Contains(have.Corpora, sl.Corpus) {
					have.Corpora = append(have.Corpora, sl.Corpus)
				}
				continue
			}
			e.Corpora = []string{sl.Corpus}
			edges[k] = &e
			edgeOrder = append(edgeOrder, k)
		}
	}
	g := Graph{Nodes: make([]Node, 0, len(order)), Edges: make([]Edge, 0, len(edgeOrder))}
	for _, id := range order {
		g.Nodes = append(g.Nodes, *nodes[id])
	}
	for _, k := range edgeOrder {
		g.Edges = append(g.Edges, *edges[k])
	}
	return g
}

// Keep is the part of g whose nodes keep says yes to, with the edges
// between them. A person, team or project left with no edge goes too, so a
// hidden document never shows who wrote it.
func (g Graph) Keep(keep func(Node) bool) Graph {
	in := map[string]bool{}
	for _, n := range g.Nodes {
		if !Shared(n.Type) && keep(n) {
			in[n.ID] = true
		}
	}
	linked := map[string]bool{}
	out := Graph{Nodes: []Node{}, Edges: []Edge{}}
	byID := map[string]Node{}
	for _, n := range g.Nodes {
		byID[n.ID] = n
	}
	for _, e := range g.Edges {
		from, to := byID[e.From], byID[e.To]
		okFrom := in[e.From] || (Shared(from.Type) && keep(from))
		okTo := in[e.To] || (Shared(to.Type) && keep(to))
		// An edge between two shared nodes alone says nothing hidden.
		if okFrom && okTo && (in[e.From] || in[e.To] || (Shared(from.Type) && Shared(to.Type))) {
			out.Edges = append(out.Edges, e)
			linked[e.From], linked[e.To] = true, true
		}
	}
	for _, n := range g.Nodes {
		if in[n.ID] || (Shared(n.Type) && linked[n.ID]) {
			out.Nodes = append(out.Nodes, n)
		}
	}
	return out
}

// Staleness is how stale a node is, and why.
type Staleness struct {
	// Score is 0 when new and nears 1 as its age passes its half-life.
	Score   float64  `json:"score"`
	Stale   bool     `json:"stale"`
	Reasons []string `json:"reasons,omitempty"`
}

// Staleness reasons.
const (
	ReasonOld        = "old"
	ReasonUnowned    = "unowned"
	ReasonOutOfDate  = "out_of_date"
	ReasonSuperseded = "superseded"
)

// StalenessOf scores a document's staleness: its age against its type's
// half-life, whether a newer document supersedes or contradicts it,
// whether anyone owns it, and whether its source is out of date. Stale
// documents rank lower; nothing is hidden for being stale.
func StalenessOf(n Node, owned, superseded bool, halfLife time.Duration, now time.Time) Staleness {
	out := Staleness{}
	if n.ChangedAt != nil && halfLife > 0 {
		age := now.Sub(*n.ChangedAt)
		if age > 0 {
			out.Score = 1 - math.Pow(0.5, float64(age)/float64(halfLife))
		}
		if out.Score >= 0.5 {
			out.Reasons = append(out.Reasons, ReasonOld)
		}
	}
	if superseded {
		out.Reasons = append(out.Reasons, ReasonSuperseded)
	}
	if !owned {
		out.Reasons = append(out.Reasons, ReasonUnowned)
	}
	if n.OutOfDate {
		out.Reasons = append(out.Reasons, ReasonOutOfDate)
	}
	out.Score = math.Round(out.Score*100) / 100
	out.Stale = out.Score >= 0.75 || superseded || n.OutOfDate
	return out
}

// Matches reports how many of a query's words a node's name holds.
func Matches(n Node, words []string) int {
	name := strings.ToLower(n.Name + " " + n.Props["title"])
	count := 0
	for _, w := range words {
		if w != "" && strings.Contains(name, w) {
			count++
		}
	}
	return count
}
