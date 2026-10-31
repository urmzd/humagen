package bigquery

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"cloud.google.com/go/civil"

	"github.com/urmzd/humagen/pkg/catalog"
)

// fake is a BigQuery with two datasets.
type fake struct {
	datasets []dataset
	tables   map[string][]string
	fail     string // a table whose read fails
	plan     plan
	answer   answer
	ran      atomic.Int64
	lastCap  int64
	lastRows int
}

func newFake() *fake {
	return &fake{
		datasets: []dataset{{ID: "billing", Description: "Money in and out"}, {ID: "scratch"}},
		tables:   map[string][]string{"billing": {"churn_monthly", "invoices"}, "scratch": {"tmp"}},
		plan:     plan{StatementType: "SELECT", Bytes: 1000, Tables: []string{"acme-data.billing.invoices"}},
		answer:   answer{Columns: []string{"region", "n"}, Rows: [][]any{{"emea", int64(3)}, {"apac", int64(5)}}, Billed: 10 << 20},
	}
}

func (f *fake) Datasets(context.Context) ([]dataset, error) { return f.datasets, nil }
func (f *fake) Tables(_ context.Context, ds string) ([]string, error) {
	return f.tables[ds], nil
}

func (f *fake) Table(_ context.Context, ds, t string) (table, error) {
	if t == f.fail {
		return table{}, errors.New("permission denied")
	}
	rows := int64(42)
	return table{Kind: "table", Description: "about " + t, Schema: []catalog.Column{{Name: "region", Type: "STRING"}}, Stats: catalog.Stats{Rows: &rows}}, nil
}

func (f *fake) DryRun(context.Context, string) (plan, error) { return f.plan, nil }
func (f *fake) Run(_ context.Context, _ string, maxBytes int64, maxRows int) (answer, error) {
	f.ran.Add(1)
	f.lastCap, f.lastRows = maxBytes, maxRows
	return f.answer, nil
}
func (f *fake) Close() error { return nil }

func connect(t *testing.T, cfg Config, f *fake) *Connector {
	t.Helper()
	c, err := newWith(cfg, func(context.Context, Config) (api, error) { return f, nil })
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestCrawlListsDatasetsAndTables(t *testing.T) {
	f := newFake()
	entries, err := connect(t, Config{Project: "acme-data"}, f).Crawl(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range entries {
		got = append(got, e.Kind+" "+e.Path)
	}
	want := "dataset acme-data.billing, dataset acme-data.scratch, table acme-data.billing.churn_monthly, table acme-data.billing.invoices, table acme-data.scratch.tmp"
	if strings.Join(got, ", ") != want {
		t.Fatalf("entries = %s", strings.Join(got, ", "))
	}
	if e := entries[2]; e.System != System || e.Description != "about churn_monthly" || len(e.Schema) != 1 || *e.Stats.Rows != 42 {
		t.Fatalf("table = %+v", e)
	}
	// The crawl is accepted by the store as it is.
	s := catalog.NewStore()
	if _, err := s.Declare("acme", catalog.Connector{Name: "wh", System: System, Owner: "ana"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Replace("acme", "wh", entries, time.Now()); err != nil {
		t.Fatal(err)
	}
}

func TestCrawlKeepsToItsDatasets(t *testing.T) {
	entries, err := connect(t, Config{Project: "acme-data", Datasets: []string{"billing"}}, newFake()).Crawl(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Path, "scratch") {
			t.Fatalf("listed %s outside the connector's datasets", e.Path)
		}
	}
	if len(entries) != 3 {
		t.Fatalf("%d entries", len(entries))
	}
}

func TestCrawlFailsWhole(t *testing.T) {
	f := newFake()
	f.fail = "invoices"
	if entries, err := connect(t, Config{Project: "acme-data"}, f).Crawl(context.Background()); err == nil || entries != nil {
		t.Fatalf("partial crawl = %d entries, %v", len(entries), err)
	}
	f = newFake()
	f.tables["billing"] = make([]string, catalog.MaxEntries)
	for i := range f.tables["billing"] {
		f.tables["billing"][i] = fmt.Sprint("t", i)
	}
	if _, err := connect(t, Config{Project: "acme-data"}, f).Crawl(context.Background()); !errors.Is(err, ErrInvalid) {
		t.Fatalf("too many = %v", err)
	}
}

func TestQueryRunsWithinTheCap(t *testing.T) {
	f := newFake()
	c := connect(t, Config{Project: "acme-data", MaxBytesBilled: 1 << 20, MaxRows: 50}, f)
	r, err := c.Query(context.Background(), "SELECT region, COUNT(*) n FROM billing.invoices GROUP BY 1")
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Rows) != 2 || r.Columns[0] != "region" || r.EstimatedBytes != 1000 || r.BilledBytes != 10<<20 {
		t.Fatalf("result = %+v", r)
	}
	// The cap the dry run was held to is the one BigQuery enforces.
	if f.lastCap != 1<<20 || f.lastRows != 50 {
		t.Fatalf("ran with cap %d and %d rows", f.lastCap, f.lastRows)
	}
}

func TestQueryIsRefusedBeforeItRuns(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
		plan plan
		want error
	}{
		{"dml", Config{Project: "acme-data"}, plan{StatementType: "DELETE"}, ErrNotReadOnly},
		{"script", Config{Project: "acme-data"}, plan{StatementType: "SCRIPT"}, ErrNotReadOnly},
		{"ddl", Config{Project: "acme-data"}, plan{StatementType: "CREATE_TABLE_AS_SELECT"}, ErrNotReadOnly},
		{"too costly", Config{Project: "acme-data", MaxBytesBilled: 999}, plan{StatementType: "SELECT", Bytes: 1000}, ErrTooCostly},
		{"other dataset", Config{Project: "acme-data", Datasets: []string{"billing"}}, plan{StatementType: "SELECT", Tables: []string{"acme-data.hr.salaries"}}, ErrOutOfScope},
		{"other project", Config{Project: "acme-data", Datasets: []string{"billing"}}, plan{StatementType: "SELECT", Tables: []string{"public.billing.x"}}, ErrOutOfScope},
		{"too many tables to check", Config{Project: "acme-data", Datasets: []string{"billing"}}, plan{StatementType: "SELECT", Tables: make([]string, maxListedTables)}, ErrOutOfScope},
	}
	for _, tc := range cases {
		f := newFake()
		f.plan = tc.plan
		_, err := connect(t, tc.cfg, f).Query(context.Background(), "SELECT 1")
		if !errors.Is(err, tc.want) {
			t.Errorf("%s: %v", tc.name, err)
		}
		if f.ran.Load() != 0 {
			t.Errorf("%s: the query ran", tc.name)
		}
	}
	c := connect(t, Config{Project: "acme-data"}, newFake())
	for _, sql := range []string{"", "   ", strings.Repeat("x", MaxSQL+1)} {
		if _, err := c.Query(context.Background(), sql); !errors.Is(err, ErrInvalid) {
			t.Errorf("sql %.10q: %v", sql, err)
		}
	}
}

func TestQueryInScopeRuns(t *testing.T) {
	f := newFake()
	if _, err := connect(t, Config{Project: "acme-data", Datasets: []string{"billing"}}, f).Query(context.Background(), "SELECT 1"); err != nil {
		t.Fatal(err)
	}
	if f.ran.Load() != 1 {
		t.Fatal("did not run")
	}
}

func TestResultsAreBounded(t *testing.T) {
	f := newFake()
	big := strings.Repeat("x", 1024)
	f.answer.Rows = nil
	for range 1000 {
		f.answer.Rows = append(f.answer.Rows, []any{big})
	}
	r, err := connect(t, Config{Project: "acme-data", MaxRows: MaxRows}, f).Query(context.Background(), "SELECT 1")
	if err != nil {
		t.Fatal(err)
	}
	if !r.Truncated || len(r.Rows) >= 1000 || len(r.Rows)*len(big) > MaxResultBytes {
		t.Fatalf("%d rows, truncated %v", len(r.Rows), r.Truncated)
	}
}

func TestConfigLimits(t *testing.T) {
	for _, cfg := range []Config{{}, {Project: "p", MaxRows: MaxRows + 1}, {Project: "p", MaxBytesBilled: -1}, {Project: "p", Timeout: MaxTimeout + 1}} {
		if _, err := New(cfg); !errors.Is(err, ErrInvalid) {
			t.Errorf("%+v: %v", cfg, err)
		}
	}
	c, err := New(Config{Project: "p"})
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Config(); got.MaxBytesBilled != DefaultMaxBytesBilled || got.MaxRows != DefaultMaxRows || got.Timeout != DefaultTimeout {
		t.Fatalf("defaults = %+v", got)
	}
}

func TestPlainValues(t *testing.T) {
	cases := []struct {
		in   any
		want any
	}{
		{big.NewRat(21, 2), "10.5"},
		{big.NewRat(100, 1), "100"},
		{civil.Date{Year: 2026, Month: 10, Day: 30}, "2026-10-30"},
		{int64(7), int64(7)},
	}
	for _, tc := range cases {
		if got := plain(tc.in); got != tc.want {
			t.Errorf("plain(%v) = %#v", tc.in, got)
		}
	}
}
