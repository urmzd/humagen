package platform

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/catalog"
	"github.com/urmzd/humagen/pkg/corpora"
	"github.com/urmzd/humagen/pkg/graph"
	"github.com/urmzd/humagen/pkg/knowledge"
	"github.com/urmzd/humagen/pkg/state"
)

// The workspace graph's structural layer: built from what each corpus's
// members already say about themselves, with no model calls. A document
// node per indexed document and per warehouse table, the person who added
// it or owns its connection, the system a table sits in, and how stale each
// document is. A corpus that is isolated keeps its documents and systems to
// itself; people always resolve once across the workspace.

const (
	// graphEvery is how often the leader looks for slices to rebuild.
	graphEvery = 10 * time.Minute
	// graphStale is how old a slice may get before it is rebuilt.
	graphStale = 30 * time.Minute
	// fileHalfLife and recordHalfLife are how fast a document and a
	// warehouse table age.
	fileHalfLife   = 365 * 24 * time.Hour
	recordHalfLife = 90 * 24 * time.Hour
	// maxGraphView bounds how many nodes one answer shows.
	maxGraphView = 1000
)

// GraphStore keeps each workspace's knowledge graph (pkg/graph).
type GraphStore = graph.Store

// openGraph opens the workspace graph on a state store.
func openGraph(st state.Store) (*GraphStore, error) { return graph.Open(st) }

// Owner states a node shows.
const (
	OwnerSuggested = "suggested"
	OwnerConfirmed = "confirmed"
	OwnerUnowned   = "unowned"
)

func memberKey(m corpora.Member) string { return string(m.Kind) + "/" + m.Name }

func memberOf(key string) (corpora.Member, bool) {
	kind, name, ok := strings.Cut(key, "/")
	if !ok {
		return corpora.Member{}, false
	}
	return corpora.Member{Kind: corpora.MemberKind(kind), Name: name}, true
}

// buildCorpusGraph builds a corpus's slice from its members as they stand.
// It sees every member: readers are filtered when the graph is read.
func (p *Platform) buildCorpusGraph(tenant string, c corpora.Corpus) graph.Slice {
	sl := graph.Slice{Corpus: c.Name, Nodes: []graph.Node{}, Edges: []graph.Edge{}, BuiltAt: p.now()}
	seen := map[string]bool{}
	// scoped keeps an isolated corpus's documents and systems its own.
	scoped := func(key string) string {
		if c.Links == corpora.Shared {
			return key
		}
		return "corpus:" + c.Name + "/" + key
	}
	add := func(n graph.Node) bool {
		if seen[n.ID] {
			return true
		}
		if len(sl.Nodes) >= graph.MaxSliceNodes {
			sl.Truncated = true
			return false
		}
		seen[n.ID] = true
		sl.Nodes = append(sl.Nodes, n)
		return true
	}
	link := func(from, to, typ string) {
		if len(sl.Edges) >= graph.MaxSliceEdges {
			sl.Truncated = true
			return
		}
		sl.Edges = append(sl.Edges, graph.Edge{From: from, To: to, Type: typ})
	}
	o := ontologyOf(c)
	// typed tags a node with the ontology type its name matches, and that
	// type's half-life.
	typed := func(n *graph.Node, name string) string {
		typ := o.TypeOf(name)
		if typ == "" {
			return ""
		}
		n.Props["entity_type"] = typ
		if days := o.Types[typ].HalfLife; days > 0 {
			n.Props["half_life_days"] = strconv.Itoa(days)
		}
		return typ
	}
	person := func(subject string) (graph.Node, bool) {
		if subject == "" {
			return graph.Node{}, false
		}
		n := graph.NewNode(graph.Person, "person:"+subject, subject)
		return n, add(n)
	}
	for _, m := range c.Members {
		switch m.Kind {
		case corpora.Source:
			src, err := p.Knowledge.Get(tenant, m.Name)
			// A provider's retained copy is withheld until the reader's own
			// access to each file is checked, so it adds no documents.
			if err != nil || src.RequiresUpstreamRead() {
				continue
			}
			docs, err := p.Knowledge.Documents(tenant, m.Name)
			if err != nil {
				continue
			}
			owner, outOfDate := src.CreatedBy, false
			if lc := src.Lifecycle; lc != nil {
				owner = cmp.Or(lc.Owner, owner)
				outOfDate = lc.Status == knowledge.NeedsAttention || lc.Cause == knowledge.Stale
			}
			for _, d := range docs {
				if d.Excluded {
					continue
				}
				n := graph.NewNode(graph.Document, scoped("document:source/"+m.Name+"/"+d.ID), d.Title)
				n.URI, n.Member, n.OutOfDate = d.URI, memberKey(m), outOfDate
				n.Props = map[string]string{"holds": HoldsFiles}
				typed(&n, d.Title)
				at := d.AddedAt
				n.ChangedAt = &at
				n.Suggested = cmp.Or(d.AddedBy, owner)
				if !add(n) {
					break
				}
				if who, ok := person(d.AddedBy); ok {
					link(who.ID, n.ID, graph.Authored)
				}
			}
		case corpora.Catalog:
			conn, err := p.CatalogStore.Connector(tenant, m.Name)
			if err != nil {
				continue
			}
			sys := graph.NewNode(graph.System, scoped("system:catalog/"+m.Name), m.Name)
			sys.Member, sys.Suggested = memberKey(m), conn.Owner
			sys.Props = map[string]string{"system": conn.System}
			if !add(sys) {
				break
			}
			if who, ok := person(conn.Owner); ok {
				link(who.ID, sys.ID, graph.Owns)
			}
			entries, err := p.CatalogStore.List(tenant, []string{m.Name}, catalog.MaxEntries)
			if err != nil {
				continue
			}
			byType := map[string][]string{}
			for _, e := range entries {
				n := graph.NewNode(graph.Document, scoped("document:catalog/"+m.Name+"/"+e.Path), e.Path)
				n.Member, n.Suggested = memberKey(m), conn.Owner
				n.Props = map[string]string{"holds": HoldsRecords, "kind": e.Kind}
				if typ := typed(&n, e.Path); typ != "" {
					byType[typ] = append(byType[typ], n.ID)
				}
				if e.Title != "" {
					n.Props["title"] = e.Title
				}
				if e.Stats.Modified != nil {
					at := *e.Stats.Modified
					n.ChangedAt = &at
				}
				if !add(n) {
					break
				}
				link(n.ID, sys.ID, graph.PartOf)
			}
			// The ontology's relations between this warehouse's typed
			// tables, such as an Order table's customer.
			for _, r := range o.Relations {
				for _, from := range byType[r.From] {
					for _, to := range byType[r.To] {
						link(from, to, r.Name)
					}
				}
			}
		}
	}
	return sl
}

// rebuildCorpusGraph builds and keeps a corpus's slice.
func (p *Platform) rebuildCorpusGraph(tenant string, c corpora.Corpus) (graph.Slice, error) {
	sl := p.buildCorpusGraph(tenant, c)
	return sl, p.Graph.PutSlice(tenant, sl)
}

// refreshGraphs rebuilds, on the leader, every slice that is missing,
// older than its corpus's last change, or older than graphStale.
func (p *Platform) refreshGraphs(ctx context.Context) error {
	tick := time.NewTicker(graphEvery)
	defer tick.Stop()
	for {
		p.refreshGraphsOnce()
		if err := p.MaintainKnowledge(ctx); err != nil && ctx.Err() == nil {
			p.log.Error("knowledge: maintenance did not finish", "err", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		}
	}
}

func (p *Platform) refreshGraphsOnce() {
	err := p.Corpora.EachTenant(func(tenant string) error {
		cs, err := p.Corpora.List(tenant)
		if err != nil {
			return err
		}
		for _, c := range cs {
			sl, err := p.Graph.Slice(tenant, c.Name)
			if err == nil && sl.BuiltAt.After(c.UpdatedAt) && p.now().Sub(sl.BuiltAt) < graphStale {
				continue
			}
			if _, err := p.rebuildCorpusGraph(tenant, c); err != nil {
				p.log.Error("graph: a corpus could not be built", "tenant", tenant, "corpus", c.Name, "err", err)
			}
		}
		return nil
	})
	if err != nil {
		p.log.Error("graph: corpora could not be listed", "err", err)
	}
}

// sliceOf reads a corpus's slice, building it the first time.
func (p *Platform) sliceOf(tenant string, c corpora.Corpus) (graph.Slice, error) {
	sl, err := p.Graph.Slice(tenant, c.Name)
	if errors.Is(err, graph.ErrNotFound) || err == nil && sl.BuiltAt.Before(c.UpdatedAt) {
		return p.rebuildCorpusGraph(tenant, c)
	}
	return sl, err
}

// readerGraph is the graph of some corpora as who may read it: only nodes
// whose member who may see, and the people linked to them.
func (p *Platform) readerGraph(who auth.Principal, cs []corpora.Corpus) (graph.Graph, []graph.Slice, error) {
	slices_ := make([]graph.Slice, 0, len(cs))
	for _, c := range cs {
		sl, err := p.sliceOf(who.Tenant, c)
		if err != nil {
			return graph.Graph{}, nil, err
		}
		slices_ = append(slices_, sl)
	}
	visible := map[string]bool{}
	g := graph.Merge(slices_).Keep(func(n graph.Node) bool {
		if n.Member == "" {
			return true
		}
		v, ok := visible[n.Member]
		if !ok {
			m, valid := memberOf(n.Member)
			v = valid && p.memberVisible(who, m)
			visible[n.Member] = v
		}
		return v
	})
	return g, slices_, nil
}

// GraphNodeView is a node as a reader sees it: who owns it and, for a
// document, how stale it is.
type GraphNodeView struct {
	graph.Node
	Owner      string           `json:"owner,omitempty"`
	OwnerState string           `json:"owner_state"`
	Staleness  *graph.Staleness `json:"staleness,omitempty"`
}

// GraphHealth sums a graph up.
type GraphHealth struct {
	Nodes     int            `json:"nodes"`
	Edges     int            `json:"edges"`
	ByType    map[string]int `json:"by_type"`
	Unowned   int            `json:"unowned"`
	Stale     int            `json:"stale"`
	BuiltAt   *time.Time     `json:"built_at,omitempty"`
	Truncated bool           `json:"truncated,omitempty"`
}

// GraphView is a graph as a reader sees it, bounded.
type GraphView struct {
	Nodes  []GraphNodeView `json:"nodes"`
	Edges  []graph.Edge    `json:"edges"`
	Health GraphHealth     `json:"health"`
	// More is set when the answer left nodes out.
	More bool `json:"more,omitempty"`
}

// owners resolves who owns each node: a person's decision, else the
// owner the build suggests, while they are still in the workspace.
type owners struct {
	p         *Platform
	tenant    string
	decisions map[string]graph.Decision
	present   map[string]bool
}

func (p *Platform) ownersOf(tenant string) (owners, error) {
	ds, err := p.Graph.Decisions(tenant)
	return owners{p: p, tenant: tenant, decisions: ds, present: map[string]bool{}}, err
}

func (o owners) here(subject string) bool {
	if subject == "" {
		return false
	}
	if v, ok := o.present[subject]; ok {
		return v
	}
	_, err := o.p.People.Get(o.tenant, subject)
	v := err == nil && o.p.invitationReader(o.tenant, subject).Can(auth.KnowledgeRead)
	o.present[subject] = v
	return v
}

func (o owners) of(n graph.Node) (string, string) {
	if graph.Shared(n.Type) {
		return "", ""
	}
	owner, state := n.Suggested, OwnerSuggested
	if d, ok := o.decisions[n.ID]; ok {
		owner, state = d.Owner, OwnerConfirmed
		if d.State == graph.Declined {
			owner = ""
		}
	}
	if !o.here(owner) {
		return "", OwnerUnowned
	}
	return owner, state
}

func (p *Platform) graphView(tenant string, g graph.Graph, slices_ []graph.Slice, keep func(graph.Node) bool, limit int) (GraphView, error) {
	o, err := p.ownersOf(tenant)
	if err != nil {
		return GraphView{}, err
	}
	superseded := map[string]bool{}
	for _, e := range g.Edges {
		if e.Type == graph.Supersedes {
			superseded[e.To] = true
		}
	}
	view := GraphView{Nodes: []GraphNodeView{}, Edges: []graph.Edge{}, Health: GraphHealth{Nodes: len(g.Nodes), Edges: len(g.Edges), ByType: map[string]int{}}}
	for _, sl := range slices_ {
		at := sl.BuiltAt
		if view.Health.BuiltAt == nil || at.Before(*view.Health.BuiltAt) {
			view.Health.BuiltAt = &at
		}
		view.Health.Truncated = view.Health.Truncated || sl.Truncated
	}
	shown := map[string]bool{}
	for _, n := range g.Nodes {
		v := GraphNodeView{Node: n}
		v.Owner, v.OwnerState = o.of(n)
		if n.Type != graph.Person && n.Type != graph.Team && n.Type != graph.Project {
			if v.OwnerState == OwnerUnowned {
				view.Health.Unowned++
			}
		}
		if n.Type == graph.Document {
			half := fileHalfLife
			if n.Props["holds"] == HoldsRecords {
				half = recordHalfLife
			}
			if days, err := strconv.Atoi(n.Props["half_life_days"]); err == nil && days > 0 {
				half = time.Duration(days) * 24 * time.Hour
			}
			s := graph.StalenessOf(n, v.OwnerState != OwnerUnowned, superseded[n.ID], half, p.now())
			v.Staleness = &s
			if s.Stale {
				view.Health.Stale++
			}
		}
		view.Health.ByType[n.Type]++
		if keep != nil && !keep(n) {
			continue
		}
		if len(view.Nodes) >= limit {
			view.More = true
			continue
		}
		view.Nodes = append(view.Nodes, v)
		shown[n.ID] = true
	}
	for _, e := range g.Edges {
		if shown[e.From] && shown[e.To] {
			view.Edges = append(view.Edges, e)
		}
	}
	return view, nil
}

func graphLimit(raw string) (int, error) {
	if raw == "" {
		return 200, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > maxGraphView {
		return 0, fmt.Errorf("%w: limit is a whole number from 1 to %d", corpora.ErrInvalid, maxGraphView)
	}
	return n, nil
}

// CorpusGraphAs is a corpus's slice of the workspace graph as who reads it,
// with its health.
func (p *Platform) CorpusGraphAs(who auth.Principal, name, rawLimit string) (GraphView, error) {
	c, err := p.Corpora.Get(who.Tenant, name)
	if err != nil || !p.corpusVisible(who, c) {
		return GraphView{}, fmt.Errorf("%w: corpus %q", corpora.ErrNotFound, name)
	}
	limit, err := graphLimit(rawLimit)
	if err != nil {
		return GraphView{}, err
	}
	g, sls, err := p.readerGraph(who, []corpora.Corpus{c})
	if err != nil {
		return GraphView{}, err
	}
	return p.graphView(who.Tenant, g, sls, nil, limit)
}

// RebuildCorpusGraph rebuilds a corpus's slice now. Only its owner asks.
func (p *Platform) RebuildCorpusGraph(who auth.Principal, name string) (GraphView, error) {
	c, err := p.ownCorpus(who, name)
	if err != nil {
		return GraphView{}, err
	}
	if _, err := p.rebuildCorpusGraph(who.Tenant, c); err != nil {
		return GraphView{}, err
	}
	return p.CorpusGraphAs(who, name, "")
}

// readableCorpora are the corpora who may read.
func (p *Platform) readableCorpora(who auth.Principal) ([]corpora.Corpus, error) {
	all, err := p.Corpora.List(who.Tenant)
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(all, func(c corpora.Corpus) bool { return !p.corpusVisible(who, c) }), nil
}

// matchGraph keeps the nodes whose names hold a query's words, and their
// neighbors one hop away.
func matchGraph(g graph.Graph, q string) func(graph.Node) bool {
	words := strings.Fields(strings.ToLower(q))
	if len(words) == 0 {
		return nil
	}
	hit := map[string]bool{}
	for _, n := range g.Nodes {
		if graph.Matches(n, words) > 0 {
			hit[n.ID] = true
		}
	}
	near := map[string]bool{}
	for _, e := range g.Edges {
		if hit[e.From] {
			near[e.To] = true
		}
		if hit[e.To] {
			near[e.From] = true
		}
	}
	return func(n graph.Node) bool { return hit[n.ID] || near[n.ID] }
}

// WorkspaceGraphAs is the workspace graph across every corpus who may read,
// narrowed to what a query names and its neighbors when q is set.
func (p *Platform) WorkspaceGraphAs(who auth.Principal, q, rawLimit string) (GraphView, error) {
	if !who.Can(auth.KnowledgeRead) {
		return GraphView{}, auth.ErrForbidden
	}
	if len(q) > maxCorpusQuery {
		return GraphView{}, fmt.Errorf("%w: a query is at most %d bytes", corpora.ErrInvalid, maxCorpusQuery)
	}
	limit, err := graphLimit(rawLimit)
	if err != nil {
		return GraphView{}, err
	}
	cs, err := p.readableCorpora(who)
	if err != nil {
		return GraphView{}, err
	}
	g, sls, err := p.readerGraph(who, cs)
	if err != nil {
		return GraphView{}, err
	}
	return p.graphView(who.Tenant, g, sls, matchGraph(g, q), limit)
}

// Owner actions on a node.
const (
	OwnerConfirm = "confirm"
	OwnerDecline = "decline"
	OwnerHandOff = "hand_off"
)

// OwnerAction is what a person does about a node's owner.
type OwnerAction struct {
	Action string `json:"action"`
	// To is who it is handed to, for hand_off.
	To string `json:"to,omitempty"`
}

// DecideOwner confirms, declines or hands off a node's ownership. Its
// current owner decides; an unowned node may be claimed or handed on by
// the owner of a corpus that holds it.
func (p *Platform) DecideOwner(who auth.Principal, id string, in OwnerAction) (GraphNodeView, error) {
	cs, err := p.readableCorpora(who)
	if err != nil {
		return GraphNodeView{}, err
	}
	g, sls, err := p.readerGraph(who, cs)
	if err != nil {
		return GraphNodeView{}, err
	}
	i := slices.IndexFunc(g.Nodes, func(n graph.Node) bool { return n.ID == id })
	if i < 0 || graph.Shared(g.Nodes[i].Type) {
		return GraphNodeView{}, fmt.Errorf("%w: node %q", graph.ErrNotFound, id)
	}
	n := g.Nodes[i]
	if who.Kind != auth.User || who.Subject == "" {
		return GraphNodeView{}, auth.ErrForbidden
	}
	o, err := p.ownersOf(who.Tenant)
	if err != nil {
		return GraphNodeView{}, err
	}
	owner, state := o.of(n)
	holder := slices.ContainsFunc(cs, func(c corpora.Corpus) bool { return c.Owner == who.Subject && slices.Contains(n.Corpora, c.Name) })
	if owner != who.Subject && (state != OwnerUnowned || !holder) {
		return GraphNodeView{}, fmt.Errorf("%w: only its owner decides who owns it", ErrForbidden)
	}
	d := graph.Decision{Node: n.ID, By: who.Subject, At: p.now(), State: graph.Confirmed}
	switch in.Action {
	case OwnerConfirm:
		d.Owner = who.Subject
	case OwnerDecline:
		d.State = graph.Declined
	case OwnerHandOff:
		if !o.here(in.To) || in.To == who.Subject {
			return GraphNodeView{}, fmt.Errorf("%w: hand it to another person in the workspace", corpora.ErrInvalid)
		}
		d.Owner = in.To
	default:
		return GraphNodeView{}, fmt.Errorf("%w: action is confirm, decline or hand_off", corpora.ErrInvalid)
	}
	if err := p.Graph.Decide(who.Tenant, d); err != nil {
		return GraphNodeView{}, err
	}
	view, err := p.graphView(who.Tenant, g, sls, func(x graph.Node) bool { return x.ID == n.ID }, 1)
	if err != nil || len(view.Nodes) != 1 {
		return GraphNodeView{}, cmp.Or(err, fmt.Errorf("%w: node %q", graph.ErrNotFound, id))
	}
	return view.Nodes[0], nil
}
