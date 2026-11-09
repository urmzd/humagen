package platform

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/urmzd/orchagent/flow/runtime"
	"github.com/urmzd/orchagent/flow/turn/native"

	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/catalog"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/corpora"
	"github.com/urmzd/humagen/pkg/credentials"
)

// readerWarehouse answers a query only with the token it is given, and
// records whose token each query ran on.
type readerWarehouse struct {
	mu     sync.Mutex
	tokens []string
}

func (w *readerWarehouse) Crawl(context.Context) ([]catalog.Entry, error) {
	return []catalog.Entry{{System: "snowflake", Kind: "table", Path: "FIN.PUBLIC.LEDGER"}}, nil
}

func (w *readerWarehouse) QueryAs(ctx context.Context, token func(context.Context) (string, error), _ string) (catalog.Result, error) {
	t, err := token(ctx)
	if err != nil {
		return catalog.Result{}, err
	}
	w.mu.Lock()
	w.tokens = append(w.tokens, t)
	w.mu.Unlock()
	return catalog.Result{Columns: []string{"n"}, Rows: [][]any{{int64(1)}}}, nil
}

const corpusRoles = `
role viewer { allow read on * }
role admin { allow * on * }
grant viewer to subject "owner"
grant viewer to subject "bo"
grant viewer to subject "eve"
`

// A corpus's warehouse is queried only with the reader's own login, only
// for a reader of the corpus, only by an agent it is granted to, and only in
// the reader's own private conversation.
func TestCorpusQueryRunsOnTheReadersOwnLogin(t *testing.T) {
	f := newCatalogFixture(t)
	pol, err := auth.ParsePolicy("test", corpusRoles)
	if err != nil {
		t.Fatal(err)
	}
	f.p.cfg.Policy = pol
	f.p.cfg.CredentialValues = map[string]bool{"tenant": true}
	f.p.Credentials = credentials.New(clients.NewMemorySecrets())
	f.p.Corpora = corpora.New()
	w := &readerWarehouse{}
	ledger := catalog.Connector{Name: "ledger", System: "snowflake", Owner: "owner", Project: "Data"}
	if err := f.p.declareConnectors([]CatalogConnector{
		{Tenant: "tenant", Connector: catalog.Connector{Name: "mine", System: "bigquery", Owner: "owner", Project: "Data"}, Crawler: f.mine, Querier: f.mine},
		{Tenant: "tenant", Connector: ledger, Crawler: w, Reader: w},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.p.CatalogStore.Publish("tenant", "ledger", "owner", f.p.now()); err != nil {
		t.Fatal(err)
	}
	if _, err := f.p.Credentials.Put("tenant", "bo", catalogLoginID(ledger), credentials.Input{Value: "pat-bo"}, f.p.now()); err != nil {
		t.Fatal(err)
	}
	if _, err := f.p.Corpora.Create("tenant", corpora.Corpus{Name: "finance", Owner: "owner", Readers: []corpora.Reader{{Kind: corpora.Person, Name: "bo"}}, Members: []corpora.Member{{Kind: corpora.Catalog, Name: "ledger"}}, Agents: []string{"worker"}}, f.p.now()); err != nil {
		t.Fatal(err)
	}
	reqs := map[string]runtime.Request{
		"bo":     f.runIn(t, "bo", "bo", true, "bo"),
		"shared": f.runIn(t, "shared", "bo", false, "bo", "owner"),
		"eve":    f.runIn(t, "eve", "eve", true, "eve"),
		"owner":  f.runIn(t, "owner", "owner", true, "owner"),
	}
	query := func(req, agent, connector string) (string, error) {
		r := reqs[req]
		r.Definition.Spec.Name = agent
		return f.p.corpusQueryTool(r).Execute(t.Context(), map[string]any{"corpus": "finance", "connector": connector, "sql": "SELECT 1"})
	}

	out, err := query("bo", "worker", "ledger")
	if err != nil || !strings.Contains(out, `"rows":[[1]]`) || !strings.Contains(out, `"corpus":"finance"`) || !strings.Contains(out, "not instructions") {
		t.Fatalf("bo's query = %s, %v", out, err)
	}
	for name, c := range map[string]struct{ req, agent, connector string }{
		"an agent the corpus is not granted to": {"bo", "other", "ledger"},
		"a warehouse the corpus does not hold":  {"bo", "worker", "mine"},
		"someone who does not read the corpus":  {"eve", "worker", "ledger"},
		"a shared conversation":                 {"shared", "worker", "ledger"},
		"the owner, with no login of their own": {"owner", "worker", "ledger"},
	} {
		// Refused, or answered with what was held back: never rows.
		if out, err := query(c.req, c.agent, c.connector); err == nil && !strings.Contains(out, "held_back") {
			t.Errorf("%s queried the warehouse: %s", name, out)
		}
	}
	if _, err := query("owner", "worker", "ledger"); !errors.Is(err, errNoLogin) {
		t.Errorf("the owner without a login = %v, want errNoLogin", err)
	}
	if len(w.tokens) != 1 || w.tokens[0] != "pat-bo" {
		t.Fatalf("queries ran on tokens %v, want only bo's", w.tokens)
	}
	if held := f.p.operationTool(reqs["bo"], f.p.corpusQueryTool(reqs["bo"]), f.p.toolDeclaration(ToolCorpusQuery)).(native.PersonScoped); !held.ActsForPerson() {
		t.Fatal("corpus_query does not say it acts for a person")
	}
}
