package catalog

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	dense "github.com/urmzd/humagen/fiber/pkg/knowledge"
	"github.com/urmzd/humagen/pkg/state"
)

var at = time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)

func table(path, desc string, cols ...Column) Entry {
	return Entry{System: "bigquery", Kind: "table", Path: path, Description: desc, Schema: cols}
}

func seed(t *testing.T, s *Store) {
	t.Helper()
	if _, err := s.Declare("acme", Connector{Name: "warehouse", System: "bigquery", Owner: "ana", Project: "data"}); err != nil {
		t.Fatal(err)
	}
	_, err := s.Replace("acme", "warehouse", []Entry{
		table("analytics.billing.churn_monthly", "Customers lost each month",
			Column{Name: "region", Type: "STRING", Description: "Sales region"},
			Column{Name: "churned", Type: "INT64"}),
		table("analytics.billing.invoices", "Every invoice sent",
			Column{Name: "amount_micros", Type: "INT64"}),
		table("analytics.web.sessions", "Site visits",
			Column{Name: "user", Type: "RECORD", Fields: []Column{{Name: "country", Description: "Where the visitor is"}}}),
	}, at)
	if err != nil {
		t.Fatal(err)
	}
}

func paths(hits []Hit) []string {
	var out []string
	for _, h := range hits {
		out = append(out, h.Entry.Path)
	}
	return out
}

func TestSearchFindsTablesByColumnsAndDescriptions(t *testing.T) {
	s := NewStore()
	seed(t, s)
	hits, err := s.Search("acme", []string{"warehouse"}, "churn by region", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 || hits[0].Entry.Path != "analytics.billing.churn_monthly" {
		t.Fatalf("hits = %v", paths(hits))
	}
	if h := hits[0].Entry; h.Connector != "warehouse" || !h.SeenAt.Equal(at) || len(h.Schema) != 2 {
		t.Fatalf("entry = %+v", h)
	}
	// Nested fields are searched by their dotted names.
	if hits, _ := s.Search("acme", []string{"warehouse"}, "visitor country", 5); len(hits) != 1 || hits[0].Entry.Path != "analytics.web.sessions" {
		t.Fatalf("nested hits = %v", paths(hits))
	}
}

func TestSearchIsDefaultDeny(t *testing.T) {
	s := NewStore()
	seed(t, s)
	for _, sel := range [][]string{nil, {}, {"other"}} {
		if hits, err := s.Search("acme", sel, "churn", 5); err != nil || len(hits) != 0 {
			t.Fatalf("selection %v = %v, %v", sel, paths(hits), err)
		}
	}
	if hits, _ := s.Search("globex", []string{"warehouse"}, "churn", 5); len(hits) != 0 {
		t.Fatalf("another tenant found %v", paths(hits))
	}
	if got := s.List("acme", nil, 10); len(got) != 0 {
		t.Fatalf("list with no selection = %v", got)
	}
}

func TestSearchRefusesBadQueries(t *testing.T) {
	s := NewStore()
	seed(t, s)
	for _, q := range []string{"", "  ...  ", strings.Repeat("a", MaxQuery+1)} {
		if _, err := s.Search("acme", []string{"warehouse"}, q, 5); !errors.Is(err, ErrInvalid) {
			t.Fatalf("query %.10q: %v", q, err)
		}
	}
}

func TestReplaceIsWholeOrNothing(t *testing.T) {
	s := NewStore()
	seed(t, s)
	// A crawl that names a path twice keeps nothing.
	_, err := s.Replace("acme", "warehouse", []Entry{table("a.b.c", ""), table("a.b.c", "")}, at.Add(time.Hour))
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("duplicate = %v", err)
	}
	if got := s.List("acme", []string{"warehouse"}, 10); len(got) != 3 {
		t.Fatalf("after a refused crawl: %d entries", len(got))
	}
	// A kept crawl drops what it no longer lists.
	c, err := s.Replace("acme", "warehouse", []Entry{table("analytics.billing.invoices", "Every invoice sent")}, at.Add(time.Hour))
	if err != nil || c.Entries != 1 || c.Generation != 2 {
		t.Fatalf("replace = %+v, %v", c, err)
	}
	if hits, _ := s.Search("acme", []string{"warehouse"}, "churn", 5); len(hits) != 0 {
		t.Fatalf("dropped table still found: %v", paths(hits))
	}
	if _, err := s.Replace("acme", "missing", nil, at); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing connector = %v", err)
	}
	if _, err := s.Replace("acme", "warehouse", make([]Entry, MaxEntries+1), at); !errors.Is(err, ErrInvalid) {
		t.Fatalf("too many = %v", err)
	}
}

func TestReplaceFailsWithoutChangingMemory(t *testing.T) {
	st := &failing{Store: state.NewMemory()}
	s, err := Open(st)
	if err != nil {
		t.Fatal(err)
	}
	seed(t, s)
	st.fail.Store(true)
	if _, err := s.Replace("acme", "warehouse", nil, at); err == nil {
		t.Fatal("a failed write was accepted")
	}
	if got := s.List("acme", []string{"warehouse"}, 10); len(got) != 3 {
		t.Fatalf("after a failed write: %d entries", len(got))
	}
}

type failing struct {
	state.Store
	fail atomic.Bool
}

func (f *failing) Write(ctx context.Context, ns string, ops ...state.Op) error {
	if f.fail.Load() {
		return errors.New("disk full")
	}
	return f.Store.Write(ctx, ns, ops...)
}

func TestVisibility(t *testing.T) {
	member := func(p string) bool { return p == "data" }
	c := Connector{Name: "warehouse", System: "bigquery", Owner: "ana", Project: "data"}
	cases := []struct {
		name    string
		c       Connector
		subject string
		member  func(string) bool
		want    bool
	}{
		{"owner", c, "ana", nil, true},
		{"member before publishing", c, "bo", member, false},
		{"nobody", c, "", member, false},
		{"member once published", withPublished(c), "bo", member, true},
		{"outsider once published", withPublished(c), "bo", func(string) bool { return false }, false},
		{"no owner, unpublished", Connector{Name: "x", System: "bigquery"}, "", nil, false},
	}
	for _, tc := range cases {
		if got := tc.c.VisibleTo(tc.subject, tc.member); got != tc.want {
			t.Errorf("%s: %v", tc.name, got)
		}
	}
}

func withPublished(c Connector) Connector {
	c.Published = &Mark{By: "ana", At: at}
	return c
}

func TestPublishAndWithdrawAreRecorded(t *testing.T) {
	st := state.NewMemory()
	s, _ := Open(st)
	seed(t, s)
	c, err := s.Publish("acme", "warehouse", "ana", at)
	if err != nil || c.Published == nil || c.Published.By != "ana" || !c.Published.At.Equal(at) {
		t.Fatalf("publish = %+v, %v", c, err)
	}
	c, err = s.Withdraw("acme", "warehouse", "admin", at.Add(time.Minute))
	if err != nil || c.Published != nil || c.Withdrawn == nil || c.Withdrawn.By != "admin" {
		t.Fatalf("withdraw = %+v, %v", c, err)
	}
	reopened, err := Open(st)
	if err != nil {
		t.Fatal(err)
	}
	got, err := reopened.Connector("acme", "warehouse")
	if err != nil || got.Published != nil || got.Withdrawn == nil || got.Withdrawn.By != "admin" {
		t.Fatalf("after restart = %+v, %v", got, err)
	}
	if _, err := s.Publish("acme", "missing", "ana", at); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing = %v", err)
	}
}

func TestDeclareDropsWhatAnotherOwnerCrawled(t *testing.T) {
	s := NewStore()
	seed(t, s)
	if _, err := s.Publish("acme", "warehouse", "ana", at); err != nil {
		t.Fatal(err)
	}
	// The same declaration keeps everything.
	c, err := s.Declare("acme", Connector{Name: "warehouse", System: "bigquery", Owner: "ana", Project: "data"})
	if err != nil || c.Published == nil || c.Entries != 3 {
		t.Fatalf("same declaration = %+v, %v", c, err)
	}
	// Another audience is not published to.
	c, _ = s.Declare("acme", Connector{Name: "warehouse", System: "bigquery", Owner: "ana", Project: "finance"})
	if c.Published != nil || c.Entries != 3 {
		t.Fatalf("new project = %+v", c)
	}
	// Another owner's credential has crawled nothing yet.
	c, _ = s.Declare("acme", Connector{Name: "warehouse", System: "bigquery", Owner: "bo", Project: "finance"})
	if c.Entries != 0 || c.CrawledAt != nil || len(s.List("acme", []string{"warehouse"}, 10)) != 0 {
		t.Fatalf("new owner = %+v", c)
	}
	if _, err := s.Declare("acme", Connector{Name: "Bad Name", System: "bigquery"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad name = %v", err)
	}
}

func TestRemoveAndRestart(t *testing.T) {
	st := state.NewMemory()
	s, _ := Open(st)
	seed(t, s)
	reopened, err := Open(st)
	if err != nil {
		t.Fatal(err)
	}
	if hits, _ := reopened.Search("acme", []string{"warehouse"}, "invoice", 5); len(hits) != 1 {
		t.Fatalf("after restart: %v", paths(hits))
	}
	if err := reopened.Remove("acme", "warehouse"); err != nil {
		t.Fatal(err)
	}
	again, _ := Open(st)
	if len(again.Connectors("acme")) != 0 {
		t.Fatal("removed connector came back")
	}
	recs, _ := st.Load(context.Background(), "catalog")
	if len(recs) != 0 {
		t.Fatalf("%d records left", len(recs))
	}
}

func TestBoundsAreEnforced(t *testing.T) {
	s := NewStore()
	seed(t, s)
	long := strings.Repeat("é", MaxDescription)
	c, err := s.Replace("acme", "warehouse", []Entry{table("a.b.c", long, Column{Name: "x", Description: long})}, at)
	if err != nil {
		t.Fatal(err)
	}
	e := s.List("acme", []string{c.Name}, 1)[0]
	if len(e.Description) > MaxDescription || len(e.Schema[0].Description) > MaxDescription || !strings.HasPrefix(long, e.Description) {
		t.Fatalf("description not clipped: %d bytes", len(e.Description))
	}
	wide := make([]Column, MaxColumns+1)
	if _, err := s.Replace("acme", "warehouse", []Entry{table("a.b.d", "", wide...)}, at); !errors.Is(err, ErrInvalid) {
		t.Fatalf("too many columns = %v", err)
	}
	if _, err := s.Replace("acme", "warehouse", []Entry{{Path: "x"}}, at); !errors.Is(err, ErrInvalid) {
		t.Fatalf("no kind = %v", err)
	}
}

// counting wraps an embedder and counts the texts it embeds.
type counting struct {
	Embedder
	texts atomic.Int64
	fail  atomic.Bool
}

func (c *counting) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if c.fail.Load() {
		return nil, errors.New("embedder down")
	}
	c.texts.Add(int64(len(texts)))
	return c.Embedder.Embed(ctx, texts)
}

func embedded(t *testing.T, s *Store) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.RLock()
		done := true
		for _, cs := range s.tenants {
			for _, c := range cs {
				for _, e := range c.entries {
					done = done && e.vec != nil
				}
			}
		}
		s.mu.RUnlock()
		if done {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("entries were not embedded in time")
}

func TestHybridSearchAndVectorsSurviveRecrawl(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	st := state.NewMemory()
	s, _ := Open(st)
	e := &counting{Embedder: dense.NewHashing(256)}
	s.UseEmbedder(ctx, e, nil)
	seed(t, s)
	embedded(t, s)
	// "churned" shares no whole word with "churning", but the hashing
	// embedder matches the stem.
	if hits, _ := s.Search("acme", []string{"warehouse"}, "churning", 5); len(hits) == 0 || hits[0].Entry.Path != "analytics.billing.churn_monthly" {
		t.Fatalf("hybrid = %v", paths(hits))
	}
	before := e.texts.Load()
	// A recrawl that changes one entry embeds only that one.
	if _, err := s.Replace("acme", "warehouse", []Entry{
		table("analytics.billing.churn_monthly", "Customers lost each month",
			Column{Name: "region", Type: "STRING", Description: "Sales region"},
			Column{Name: "churned", Type: "INT64"}),
		table("analytics.billing.invoices", "Every invoice ever sent", Column{Name: "amount_micros", Type: "INT64"}),
	}, at.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	embedded(t, s)
	if got := e.texts.Load() - before; got != 1 {
		t.Fatalf("recrawl embedded %d entries, want 1", got)
	}
	// Vectors survive a restart under the new generation.
	reopened, _ := Open(st)
	reopened.mu.RLock()
	for _, x := range reopened.tenants["acme"]["warehouse"].entries {
		if x.vec == nil {
			t.Errorf("%s lost its vector", x.Path)
		}
	}
	reopened.mu.RUnlock()
}

func TestEmbedderOutageSearchesLexically(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := NewStore()
	e := &counting{Embedder: dense.NewHashing(64)}
	e.fail.Store(true)
	s.UseEmbedder(ctx, e, nil)
	seed(t, s)
	hits, err := s.Search("acme", []string{"warehouse"}, "invoice", 5)
	if err != nil || len(hits) != 1 {
		t.Fatalf("lexical fallback = %v, %v", paths(hits), err)
	}
}
