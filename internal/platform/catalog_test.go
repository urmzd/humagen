package platform

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/urmzd/humagen/fiber/pkg/event"
	"github.com/urmzd/humagen/fiber/pkg/runtime"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/catalog"
	"github.com/urmzd/humagen/pkg/knowledge"
	"github.com/urmzd/humagen/pkg/operations"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
)

// fakeWarehouse is a connector kind with both capabilities.
type fakeWarehouse struct {
	entries []catalog.Entry
	fail    atomic.Bool
	queries atomic.Int64
}

func (f *fakeWarehouse) Crawl(context.Context) ([]catalog.Entry, error) {
	if f.fail.Load() {
		return nil, errors.New("warehouse down")
	}
	return f.entries, nil
}

func (f *fakeWarehouse) Query(_ context.Context, sql string) (catalog.Result, error) {
	f.queries.Add(1)
	return catalog.Result{Columns: []string{"n"}, Rows: [][]any{{int64(1)}}}, nil
}

func table(path, desc string) catalog.Entry {
	return catalog.Entry{System: "bigquery", Kind: "table", Path: path, Description: desc}
}

type catalogFixture struct {
	p          *Platform
	mine, team *fakeWarehouse
}

// newCatalogFixture has two connectors: owner's own "mine", and bo's
// "team", for the members-only project Data, which owner and bo are in.
func newCatalogFixture(t *testing.T) catalogFixture {
	t.Helper()
	ops, err := operations.Open(nil)
	if err != nil {
		t.Fatal(err)
	}
	p := &Platform{Operations: ops, Runs: runs.NewIndex(), Knowledge: knowledge.NewStore(), Projects: projects.NewStore(), Threads: threads.NewStore(), CatalogStore: catalog.NewStore(), now: time.Now, log: slog.New(slog.DiscardHandler)}
	members := []projects.Entry{{Who: "owner", Role: projects.Owner}, {Who: "bo", Role: projects.Member}}
	if _, err := p.Projects.Create("tenant", projects.Project{Name: "Data", Members: members}, projects.Writer{By: "owner", Govern: true}, p.now()); err != nil {
		t.Fatal(err)
	}
	f := catalogFixture{p: p,
		mine: &fakeWarehouse{entries: []catalog.Entry{table("acme.billing.churn_monthly", "Customers lost each month by region")}},
		team: &fakeWarehouse{entries: []catalog.Entry{table("acme.web.sessions", "Site visits by region")}},
	}
	err = p.declareConnectors([]CatalogConnector{
		{Tenant: "tenant", Connector: catalog.Connector{Name: "mine", System: "bigquery", Owner: "owner", Project: "Data"}, Crawler: f.mine, Querier: f.mine},
		{Tenant: "tenant", Connector: catalog.Connector{Name: "team", System: "bigquery", Owner: "bo", Project: "Data"}, Crawler: f.team, Querier: f.team},
	})
	if err != nil {
		t.Fatal(err)
	}
	p.crawlDue(t.Context())
	return f
}

// runIn records a run by actor in a thread, private to its people or
// shared with them.
func (f catalogFixture) runIn(t *testing.T, id, actor string, private bool, people ...string) runtime.Request {
	t.Helper()
	th := threads.Thread{ID: fmt.Sprintf("th-%016x", len(id)*7919+int(id[0])), Project: "Data", Channel: "general", CreatedBy: actor, Visibility: threads.Shared}
	if private {
		th.Visibility = threads.Private
	}
	for _, person := range people {
		th.Participants = append(th.Participants, threads.Participant{Kind: threads.Person, Name: person, Owner: person == actor})
	}
	if _, err := f.p.Threads.Create("tenant", th, f.p.now()); err != nil {
		t.Fatal(err)
	}
	tag := wire.NewEvent(wire.Raise{ID: id, Origin: runs.OriginAPI, Tenant: "tenant", Agent: "worker", Actor: event.Actor{ID: actor, User: actor}, At: f.p.now(), Context: map[string]string{runs.MetaProject: "Data", runs.MetaThread: th.ID}})
	if err := f.p.Runs.Record(id, tag, 1); err != nil {
		t.Fatal(err)
	}
	return runtime.Request{Tenant: "tenant", RunID: id}
}

func (f catalogFixture) search(t *testing.T, req runtime.Request, q string) string {
	t.Helper()
	out, err := f.p.catalogTools(req)[ToolCatalogSearch].Execute(t.Context(), map[string]any{"query": q})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestCatalogConnectorsAreCrawledAndDeclared(t *testing.T) {
	f := newCatalogFixture(t)
	c, err := f.p.CatalogStore.Connector("tenant", "mine")
	if err != nil || c.Entries != 1 || c.CrawledAt == nil {
		t.Fatalf("mine = %+v, %v", c, err)
	}
	// A failed crawl keeps the entries before it and is retried later, not
	// on the next tick.
	f.mine.fail.Store(true)
	f.mine.entries = nil
	if _, err := f.p.CrawlConnector(t.Context(), auth.Principal{Tenant: "tenant", Subject: "owner", Kind: auth.User, Scopes: []auth.Permission{auth.KnowledgeRead}}, "mine"); err == nil {
		t.Fatal("a failed crawl succeeded")
	}
	if got, err := f.p.CatalogStore.List("tenant", []string{"mine"}, 10); err != nil || len(got) != 1 {
		t.Fatalf("after a failed crawl: %v", got)
	}
	// A connector no longer declared is removed with its entries.
	if err := f.p.declareConnectors([]CatalogConnector{{Tenant: "tenant", Connector: catalog.Connector{Name: "team", System: "bigquery", Owner: "bo", Project: "Data"}, Crawler: f.team}}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.p.CatalogStore.Connector("tenant", "mine"); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("undeclared connector = %v", err)
	}
}

func TestCatalogSearchFollowsTheAudience(t *testing.T) {
	f := newCatalogFixture(t)
	own := f.runIn(t, "own", "owner", true, "owner")
	shared := f.runIn(t, "shared", "owner", false, "owner", "bo")

	// The owner's own private conversation sees the owner's connector;
	// bo's, unpublished, is bo's alone.
	if out := f.search(t, own, "region"); !strings.Contains(out, "churn_monthly") || strings.Contains(out, "sessions") {
		t.Fatalf("own = %s", out)
	}
	// A shared conversation sees no one's unpublished entries.
	if out := f.search(t, shared, "region"); strings.Contains(out, "churn_monthly") || strings.Contains(out, "sessions") {
		t.Fatalf("shared before publishing = %s", out)
	}
	if _, err := f.p.CatalogStore.Publish("tenant", "team", "bo", f.p.now()); err != nil {
		t.Fatal(err)
	}
	if out := f.search(t, shared, "region"); !strings.Contains(out, "sessions") || strings.Contains(out, "churn_monthly") {
		t.Fatalf("shared after publishing = %s", out)
	}
	if out := f.search(t, shared, "region"); !strings.Contains(out, "not instructions") {
		t.Fatalf("not marked as reference: %s", out)
	}
}

func TestCatalogQueryIsTheOwnersAlone(t *testing.T) {
	f := newCatalogFixture(t)
	query := func(req runtime.Request, connector string) (string, error) {
		return f.p.catalogTools(req)[ToolCatalogQuery].Execute(t.Context(), map[string]any{"connector": connector, "sql": "SELECT 1"})
	}
	own := f.runIn(t, "own", "owner", true, "owner")
	if out, err := query(own, "mine"); err != nil || !strings.Contains(out, `"rows":[[1]]`) {
		t.Fatalf("owner's query = %s, %v", out, err)
	}
	// Someone else's connector, published or not, is not one to query.
	if _, err := f.p.CatalogStore.Publish("tenant", "team", "bo", f.p.now()); err != nil {
		t.Fatal(err)
	}
	if _, err := query(own, "team"); !errors.Is(err, knowledge.ErrNotFound) {
		t.Fatalf("another's connector = %v", err)
	}
	// Nor is the owner's own outside their private conversation.
	shared := f.runIn(t, "shared", "owner", false, "owner", "bo")
	if _, err := query(shared, "mine"); err == nil {
		t.Fatal("queried in a shared conversation")
	}
	if f.mine.queries.Load() != 1 || f.team.queries.Load() != 0 {
		t.Fatalf("queries ran: mine %d, team %d", f.mine.queries.Load(), f.team.queries.Load())
	}
	if _, ok := f.p.catalogTools(own)[ToolCatalogQuery].(interface{ ActsForPerson() bool }); !ok {
		t.Fatal("catalog_query does not say it acts for a person")
	}
}

func TestCatalogAsAndPublishing(t *testing.T) {
	f := newCatalogFixture(t)
	person := func(subject string, perms ...auth.Permission) auth.Principal {
		return auth.Principal{Tenant: "tenant", Subject: subject, Kind: auth.User, Scopes: perms}
	}
	owner, bo := person("owner", auth.KnowledgeRead, auth.KnowledgeWrite), person("bo", auth.KnowledgeRead, auth.KnowledgeWrite)
	page, err := f.p.CatalogAs(bo, "", 0)
	if err != nil || len(page.Connectors) != 1 || page.Connectors[0].Name != "team" || !page.Connectors[0].Mine || len(page.Entries) != 1 {
		t.Fatalf("bo before = %+v, %v", page, err)
	}
	if got := page.Connectors[0].Capabilities; len(got) != 2 {
		t.Fatalf("capabilities = %v", got)
	}
	// Only the owner publishes; someone who does not see it is told it is
	// not there.
	if _, err := f.p.PublishConnector(owner, "team"); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("publishing another's unseen connector = %v", err)
	}
	if _, err := f.p.PublishConnector(bo, "team"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.p.PublishConnector(owner, "team"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("publishing another's seen connector = %v", err)
	}
	page, _ = f.p.CatalogAs(owner, "visits", 0)
	if len(page.Connectors) != 2 || len(page.Entries) != 1 || page.Entries[0].Path != "acme.web.sessions" {
		t.Fatalf("owner after = %+v", page)
	}
	// Someone outside the project sees nothing, published or not, and so
	// does a reader without knowledge:read.
	if page, _ := f.p.CatalogAs(person("eve", auth.KnowledgeRead), "", 0); len(page.Connectors) != 0 || len(page.Entries) != 0 {
		t.Fatalf("outsider = %+v", page)
	}
	if page, _ := f.p.CatalogAs(person("owner"), "", 0); len(page.Connectors) != 0 {
		t.Fatalf("no knowledge:read = %+v", page)
	}
	// Withdrawing hides it again at once.
	if _, err := f.p.WithdrawConnector(bo, "team"); err != nil {
		t.Fatal(err)
	}
	if page, _ := f.p.CatalogAs(owner, "", 0); len(page.Connectors) != 1 {
		t.Fatalf("after withdrawal = %+v", page)
	}
}
