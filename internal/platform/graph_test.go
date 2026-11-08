package platform

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/catalog"
	"github.com/urmzd/humagen/pkg/corpora"
	"github.com/urmzd/humagen/pkg/graph"
	"github.com/urmzd/humagen/pkg/knowledge"
	"github.com/urmzd/humagen/pkg/people"
)

// graphFixture has a handbook ana added a policy to, and owner's warehouse
// "mine" with one table, in corpus hr (read by ana and bo, shared) and in
// corpus vault (read by owner alone, isolated).
func graphFixture(t *testing.T) catalogFixture {
	t.Helper()
	f := newCatalogFixture(t)
	pol, err := auth.ParsePolicy("test", corpusRoles+"grant viewer to subject \"ana\"\n")
	if err != nil {
		t.Fatal(err)
	}
	f.p.cfg.Policy = pol
	f.p.Corpora, f.p.Graph, f.p.People = corpora.New(), graph.New(), people.NewStore()
	for _, s := range []string{"owner", "ana", "bo"} {
		if _, err := f.p.People.Seen("tenant", people.Sighting{Subject: s, Name: s, Roles: []string{"viewer"}, Source: people.SignIn}, f.p.now()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.p.Knowledge.Create("tenant", "handbook", "", "owner", f.p.now()); err != nil {
		t.Fatal(err)
	}
	if _, err := f.p.Knowledge.Add("tenant", "handbook", "Leave policy", "https://wiki.example.com/leave", "Parental leave is sixteen weeks.", "ana", f.p.now()); err != nil {
		t.Fatal(err)
	}
	both := []corpora.Member{{Kind: corpora.Source, Name: "handbook"}, {Kind: corpora.Catalog, Name: "mine"}}
	if _, err := f.p.Corpora.Create("tenant", corpora.Corpus{Name: "hr", Owner: "owner", Links: corpora.Shared, GraphSearch: true, Readers: []corpora.Reader{{Kind: corpora.Person, Name: "ana"}, {Kind: corpora.Person, Name: "bo"}}, Members: both, Agents: []string{"worker"}}, f.p.now()); err != nil {
		t.Fatal(err)
	}
	if _, err := f.p.Corpora.Create("tenant", corpora.Corpus{Name: "vault", Owner: "owner", Members: both}, f.p.now()); err != nil {
		t.Fatal(err)
	}
	return f
}

func graphNames(v GraphView) []string {
	out := []string{}
	for _, n := range v.Nodes {
		out = append(out, n.Type+":"+n.Name)
	}
	slices.Sort(out)
	return out
}

func node(t *testing.T, v GraphView, name string) GraphNodeView {
	t.Helper()
	for _, n := range v.Nodes {
		if n.Name == name {
			return n
		}
	}
	t.Fatalf("no node %q in %v", name, graphNames(v))
	return GraphNodeView{}
}

// A corpus's slice is built from its members; a reader sees only what its
// members let them, and an isolated corpus keeps its documents apart.
func TestTheGraphIsBuiltFromMembersAndReadPerReader(t *testing.T) {
	f := graphFixture(t)
	owner, bo := f.p.invitationReader("tenant", "owner"), f.p.invitationReader("tenant", "bo")
	full, err := f.p.CorpusGraphAs(owner, "hr", "")
	if err != nil {
		t.Fatal(err)
	}
	if got := graphNames(full); !slices.Equal(got, []string{"document:Leave policy", "document:acme.billing.churn_monthly", "person:ana", "person:owner", "system:mine"}) {
		t.Fatalf("owner sees %v", got)
	}
	if full.Health.Nodes != 5 || full.Health.ByType["document"] != 2 || full.Health.BuiltAt == nil {
		t.Fatalf("health %+v", full.Health)
	}
	if leave := node(t, full, "Leave policy"); leave.Owner != "ana" || leave.OwnerState != OwnerSuggested || leave.Staleness == nil || leave.Staleness.Stale {
		t.Fatalf("the policy = %+v", leave)
	}
	// Bo cannot see owner's unpublished warehouse: no system, no table, and
	// not the person known only through it.
	if got, _ := f.p.CorpusGraphAs(bo, "hr", ""); !slices.Equal(graphNames(got), []string{"document:Leave policy", "person:ana"}) {
		t.Fatalf("bo sees %v", graphNames(got))
	}
	// Across corpora: the shared corpus and the isolated one each keep their
	// own document nodes; ana resolves once.
	all, err := f.p.WorkspaceGraphAs(owner, "", "")
	if err != nil {
		t.Fatal(err)
	}
	count := map[string]int{}
	for _, n := range all.Nodes {
		count[n.Type+":"+n.Name]++
	}
	if count["document:Leave policy"] != 2 || count["person:ana"] != 1 {
		t.Fatalf("workspace graph %v", count)
	}
	if got, _ := f.p.WorkspaceGraphAs(owner, "churn", ""); !slices.Contains(graphNames(got), "system:mine") || slices.Contains(graphNames(got), "document:Leave policy") {
		t.Fatalf("a query keeps what it names and its neighbors: %v", graphNames(got))
	}
}

// Who owns a document: the graph suggests, its owner confirms, declines or
// hands it off, and one who has left owns nothing.
func TestOwnersAreSuggestedThenDecided(t *testing.T) {
	f := graphFixture(t)
	ana, bo := f.p.invitationReader("tenant", "ana"), f.p.invitationReader("tenant", "bo")
	view, _ := f.p.CorpusGraphAs(ana, "hr", "")
	leave := node(t, view, "Leave policy")
	if _, err := f.p.DecideOwner(bo, leave.ID, OwnerAction{Action: OwnerConfirm}); err == nil {
		t.Fatal("bo confirmed owning ana's document")
	}
	got, err := f.p.DecideOwner(ana, leave.ID, OwnerAction{Action: OwnerHandOff, To: "bo"})
	if err != nil || got.Owner != "bo" || got.OwnerState != OwnerConfirmed {
		t.Fatalf("handed off = %+v, %v", got, err)
	}
	if got, err := f.p.DecideOwner(bo, leave.ID, OwnerAction{Action: OwnerDecline}); err != nil || got.OwnerState != OwnerUnowned || !slices.Contains(got.Staleness.Reasons, graph.ReasonUnowned) {
		t.Fatalf("declined = %+v, %v", got, err)
	}
	// Unowned now: the corpus's owner may claim it; ana no longer may.
	if _, err := f.p.DecideOwner(ana, leave.ID, OwnerAction{Action: OwnerConfirm}); err == nil {
		t.Fatal("a former owner reclaimed an unowned document")
	}
	if got, err := f.p.DecideOwner(f.p.invitationReader("tenant", "owner"), leave.ID, OwnerAction{Action: OwnerConfirm}); err != nil || got.Owner != "owner" {
		t.Fatalf("the corpus owner's claim = %+v, %v", got, err)
	}
}

// Graph retrieval answers who owns a document, and a stale document ranks
// after a fresh one; a warehouse table changed long ago is stale.
func TestGraphRetrievalNamesOwnersAndStaleness(t *testing.T) {
	f := graphFixture(t)
	long := f.p.now().Add(-400 * 24 * time.Hour)
	f.mine.entries = []catalog.Entry{{System: "bigquery", Kind: "table", Path: "acme.billing.leave_balances", Description: "Leave left per person", Stats: catalog.Stats{Modified: &long}}}
	if _, err := f.p.CrawlConnector(t.Context(), auth.Principal{Tenant: "tenant", Subject: "owner", Kind: auth.User, Scopes: []auth.Permission{auth.KnowledgeRead}}, "mine"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.p.CatalogStore.Publish("tenant", "mine", "owner", f.p.now()); err != nil {
		t.Fatal(err)
	}
	c, _ := f.p.Corpora.Get("tenant", "hr")
	if _, err := f.p.rebuildCorpusGraph("tenant", c); err != nil {
		t.Fatal(err)
	}
	hits, err := f.p.graphHits(f.p.invitationReader("tenant", "bo"), c, "leave", 8)
	if err != nil || len(hits) != 2 {
		t.Fatalf("graph hits %+v, %v", hits, err)
	}
	if hits[0].Title != "Leave policy" || hits[0].Owner != "ana" || hits[0].Stale || !hits[1].Stale || !strings.Contains(hits[1].Text, "Stale: old") {
		t.Fatalf("hits %+v", hits)
	}
	// The tool reads the same graph for a granted agent in a private conversation.
	r := f.runIn(t, "bo-graph", "bo", true, "bo")
	r.Definition.Spec.Name = "worker"
	out, err := f.p.corpusTools(r)[ToolCorpusGraph].Execute(t.Context(), map[string]any{"query": "leave"})
	if err != nil || !strings.Contains(out, `"owner":"ana"`) || !strings.Contains(out, "not instructions") {
		t.Fatalf("corpus_graph = %s, %v", out, err)
	}
	_ = knowledge.Indexed
}

// An ontology inferred from the warehouse, once accepted, types its tables
// in the graph and joins them by its relations.
func TestAnAcceptedOntologyTypesTheGraph(t *testing.T) {
	f := graphFixture(t)
	f.mine.entries = []catalog.Entry{
		{System: "bigquery", Kind: "table", Path: "acme.sales.customers", Schema: []catalog.Column{{Name: "id", Type: "INT64"}}},
		{System: "bigquery", Kind: "table", Path: "acme.sales.orders", Schema: []catalog.Column{{Name: "id", Type: "INT64"}, {Name: "customer_id", Type: "INT64"}}},
	}
	owner := f.p.invitationReader("tenant", "owner")
	owner.Scopes = append(owner.Scopes, auth.KnowledgeWrite)
	if _, err := f.p.CrawlConnector(t.Context(), owner, "mine"); err != nil {
		t.Fatal(err)
	}
	proposed, err := f.p.InferOntology(owner, "hr")
	if err != nil || !strings.Contains(string(proposed.Proposed), `"Order"`) || len(proposed.Ontology) != 0 {
		t.Fatalf("proposed %s, %v", proposed.Proposed, err)
	}
	accepted, err := f.p.AcceptOntology(owner, "hr", nil)
	if err != nil || len(accepted.Proposed) != 0 || !strings.Contains(string(accepted.Ontology), `"customer"`) {
		t.Fatalf("accepted %s, %v", accepted.Ontology, err)
	}
	view, err := f.p.RebuildCorpusGraph(owner, "hr")
	if err != nil {
		t.Fatal(err)
	}
	orders, customers := node(t, view, "acme.sales.orders"), node(t, view, "acme.sales.customers")
	if orders.Props["entity_type"] != "Order" || customers.Props["entity_type"] != "Customer" {
		t.Fatalf("types: %v, %v", orders.Props, customers.Props)
	}
	if !slices.ContainsFunc(view.Edges, func(e graph.Edge) bool { return e.From == orders.ID && e.To == customers.ID && e.Type == "customer" }) {
		t.Fatalf("no customer relation among %+v", view.Edges)
	}
	if _, err := f.p.InferOntology(f.p.invitationReader("tenant", "ana"), "hr"); err == nil {
		t.Fatal("a reader who is not the owner proposed an ontology")
	}
}

// Maintenance proposes types a corpus's warehouse suggests once: a
// proposal discarded or accepted is not asked again.
func TestMaintenanceProposesAnOntologyOnce(t *testing.T) {
	f := graphFixture(t)
	f.mine.entries = []catalog.Entry{{System: "bigquery", Kind: "table", Path: "acme.sales.orders", Schema: []catalog.Column{{Name: "id", Type: "INT64"}}}}
	owner := f.p.invitationReader("tenant", "owner")
	owner.Scopes = append(owner.Scopes, auth.KnowledgeWrite)
	if _, err := f.p.CrawlConnector(t.Context(), owner, "mine"); err != nil {
		t.Fatal(err)
	}
	c, _ := f.p.Corpora.Get("tenant", "hr")
	if got, err := f.p.proposeOntology("tenant", c); err != nil || got != "Order" {
		t.Fatalf("first proposal %q, %v", got, err)
	}
	c, _ = f.p.Corpora.Get("tenant", "hr")
	if got, _ := f.p.proposeOntology("tenant", c); got != "Order" {
		t.Fatalf("an open proposal is still named: %q", got)
	}
	if _, err := f.p.DiscardOntology(owner, "hr"); err != nil {
		t.Fatal(err)
	}
	c, _ = f.p.Corpora.Get("tenant", "hr")
	if got, _ := f.p.proposeOntology("tenant", c); got != "" || len(c.Proposed) != 0 {
		t.Fatalf("a discarded proposal came back: %q", got)
	}
}
