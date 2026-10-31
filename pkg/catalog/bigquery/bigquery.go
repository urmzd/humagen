// Package bigquery is the BigQuery connector kind: it lists a project's
// datasets, tables and their schemas for the catalog, and runs bounded,
// read-only, cost-capped queries in place.
//
// Every query is dry-run first. One that is not a single SELECT, that reads
// a dataset outside the connector's list, or that would read more than the
// connector's byte cap is refused before it runs. The query that does run
// carries the same cap as BigQuery's maximum bytes billed, so the estimate
// being wrong cannot cost more than the cap.
//
// The credential is a reference (pkg/credref) to a service account key,
// read on every call. With none, the client uses Application Default
// Credentials: on GCP, the VM's service account.
package bigquery

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/urmzd/humagen/pkg/catalog"
)

// System is what the catalog calls BigQuery.
const System = "bigquery"

// Defaults and ceilings for a connector's limits.
const (
	DefaultMaxBytesBilled int64 = 1 << 30 // 1 GiB
	DefaultMaxRows              = 200
	DefaultTimeout              = 60 * time.Second
	MaxRows                     = 10_000
	MaxTimeout                  = 10 * time.Minute
	// MaxSQL bounds one query's text.
	MaxSQL = 20_000
	// MaxResultBytes bounds the rows one answer carries, measured as text.
	MaxResultBytes = 256 << 10
	// crawlers is how many tables are read at once during a crawl.
	crawlers = 8
)

// Errors a caller can match.
var (
	ErrInvalid     = errors.New("bigquery: invalid")
	ErrNotReadOnly = errors.New("bigquery: only a single SELECT may run")
	ErrOutOfScope  = errors.New("bigquery: query reads a dataset outside this connector")
	ErrTooCostly   = errors.New("bigquery: query would read more than this connector allows")
)

// Config is one connector's settings.
type Config struct {
	// Project is the project listed and billed for queries.
	Project string
	// Datasets limits the catalog and every query to these datasets. Empty
	// is every dataset the credential can list.
	Datasets []string
	// Location is where queries run, such as US or EU. Empty lets
	// BigQuery choose from the tables read.
	Location string
	// Credential is a reference to a service account key in JSON. Empty is
	// Application Default Credentials.
	Credential string
	// MaxBytesBilled caps what one query may read. Zero is
	// DefaultMaxBytesBilled.
	MaxBytesBilled int64
	// MaxRows caps the rows one answer returns. Zero is DefaultMaxRows.
	MaxRows int
	// Timeout bounds one query. Zero is DefaultTimeout.
	Timeout time.Duration
}

// withDefaults fills zero limits and checks the rest.
func (c Config) withDefaults() (Config, error) {
	if c.Project == "" {
		return c, fmt.Errorf("%w: a project is required", ErrInvalid)
	}
	if c.MaxBytesBilled == 0 {
		c.MaxBytesBilled = DefaultMaxBytesBilled
	}
	if c.MaxRows == 0 {
		c.MaxRows = DefaultMaxRows
	}
	if c.Timeout == 0 {
		c.Timeout = DefaultTimeout
	}
	switch {
	case c.MaxBytesBilled < 0:
		return c, fmt.Errorf("%w: max_bytes_billed must be positive", ErrInvalid)
	case c.MaxRows < 0 || c.MaxRows > MaxRows:
		return c, fmt.Errorf("%w: max_rows is 1 to %d", ErrInvalid, MaxRows)
	case c.Timeout < 0 || c.Timeout > MaxTimeout:
		return c, fmt.Errorf("%w: timeout is at most %s", ErrInvalid, MaxTimeout)
	}
	return c, nil
}

// api is the part of BigQuery the connector uses. client is the real one;
// tests give a fake.
type api interface {
	Datasets(ctx context.Context) ([]dataset, error)
	Tables(ctx context.Context, dataset string) ([]string, error)
	Table(ctx context.Context, dataset, table string) (table, error)
	DryRun(ctx context.Context, sql string) (plan, error)
	Run(ctx context.Context, sql string, maxBytes int64, maxRows int) (answer, error)
	Close() error
}

type dataset struct {
	ID, Description string
}

type table struct {
	Kind        string // table, view, materialized_view, external or snapshot
	Title       string
	Description string
	Schema      []catalog.Column
	Stats       catalog.Stats
}

// plan is what a dry run says about a query.
type plan struct {
	StatementType string
	Bytes         int64
	// Tables are the tables read, as project.dataset.table. BigQuery lists
	// at most 50.
	Tables []string
}

type answer struct {
	Columns   []string
	Rows      [][]any
	Truncated bool
	Billed    int64
}

// maxListedTables is how many referenced tables BigQuery reports. A plan
// with that many may have more, so scope cannot be proved.
const maxListedTables = 50

// Connector is one BigQuery connection.
type Connector struct {
	cfg  Config
	open func(ctx context.Context) (api, error)
}

// New returns a connector. Nothing is read until it is used.
func New(cfg Config) (*Connector, error) { return newWith(cfg, dial) }

func newWith(cfg Config, open func(ctx context.Context, cfg Config) (api, error)) (*Connector, error) {
	cfg, err := cfg.withDefaults()
	if err != nil {
		return nil, err
	}
	return &Connector{cfg: cfg, open: func(ctx context.Context) (api, error) { return open(ctx, cfg) }}, nil
}

// Config returns the connector's settings with their defaults.
func (c *Connector) Config() Config { return c.cfg }

var (
	_ catalog.Crawler = (*Connector)(nil)
	_ catalog.Querier = (*Connector)(nil)
)

// Crawl lists the connector's datasets and their tables with schemas. It
// fails whole when any listing fails or there are more than
// catalog.MaxEntries objects.
func (c *Connector) Crawl(ctx context.Context) ([]catalog.Entry, error) {
	bq, err := c.open(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = bq.Close() }()
	all, err := bq.Datasets(ctx)
	if err != nil {
		return nil, fmt.Errorf("bigquery: list datasets: %w", err)
	}
	var out []catalog.Entry
	type ref struct{ dataset, table string }
	var refs []ref
	for _, ds := range all {
		if len(c.cfg.Datasets) > 0 && !slices.Contains(c.cfg.Datasets, ds.ID) {
			continue
		}
		out = append(out, catalog.Entry{System: System, Kind: "dataset", Path: c.cfg.Project + "." + ds.ID, Description: ds.Description})
		tables, err := bq.Tables(ctx, ds.ID)
		if err != nil {
			return nil, fmt.Errorf("bigquery: list tables in %s: %w", ds.ID, err)
		}
		for _, t := range tables {
			refs = append(refs, ref{ds.ID, t})
		}
		if len(out)+len(refs) > catalog.MaxEntries {
			return nil, fmt.Errorf("%w: more than %d datasets and tables; list fewer datasets", ErrInvalid, catalog.MaxEntries)
		}
	}
	tables := make([]catalog.Entry, len(refs))
	errs := make([]error, len(refs))
	sem := make(chan struct{}, crawlers)
	var wg sync.WaitGroup
	for i, r := range refs {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			t, err := bq.Table(ctx, r.dataset, r.table)
			if err != nil {
				errs[i] = fmt.Errorf("bigquery: read %s.%s: %w", r.dataset, r.table, err)
				return
			}
			tables[i] = catalog.Entry{
				System: System, Kind: t.Kind, Path: c.cfg.Project + "." + r.dataset + "." + r.table,
				Title: t.Title, Description: t.Description, Schema: t.Schema, Stats: t.Stats,
			}
		}()
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return append(out, tables...), nil
}

// Query runs one read-only SELECT within the connector's limits. It is
// dry-run first, and refused before it runs when it is not a single
// SELECT, reads outside the connector's datasets, or would read more than
// MaxBytesBilled.
func (c *Connector) Query(ctx context.Context, sql string) (catalog.Result, error) {
	sql = strings.TrimSpace(sql)
	if sql == "" || len(sql) > MaxSQL {
		return catalog.Result{}, fmt.Errorf("%w: a query is 1 to %d bytes", ErrInvalid, MaxSQL)
	}
	ctx, cancel := context.WithTimeout(ctx, c.cfg.Timeout)
	defer cancel()
	bq, err := c.open(ctx)
	if err != nil {
		return catalog.Result{}, err
	}
	defer func() { _ = bq.Close() }()
	p, err := bq.DryRun(ctx, sql)
	if err != nil {
		return catalog.Result{}, fmt.Errorf("bigquery: dry run: %w", err)
	}
	if p.StatementType != "SELECT" {
		return catalog.Result{}, fmt.Errorf("%w: this is a %s statement", ErrNotReadOnly, strings.ToUpper(p.StatementType))
	}
	if err := c.inScope(p.Tables); err != nil {
		return catalog.Result{}, err
	}
	if p.Bytes > c.cfg.MaxBytesBilled {
		return catalog.Result{EstimatedBytes: p.Bytes}, fmt.Errorf("%w: it would read %d bytes, and the cap is %d; select fewer columns or filter on the partition", ErrTooCostly, p.Bytes, c.cfg.MaxBytesBilled)
	}
	a, err := bq.Run(ctx, sql, c.cfg.MaxBytesBilled, c.cfg.MaxRows)
	if err != nil {
		return catalog.Result{EstimatedBytes: p.Bytes}, fmt.Errorf("bigquery: query: %w", err)
	}
	rows, cut := clipRows(a.Rows)
	return catalog.Result{
		Columns: a.Columns, Rows: rows, Truncated: a.Truncated || cut,
		EstimatedBytes: p.Bytes, BilledBytes: a.Billed,
	}, nil
}

// inScope checks that every table a query reads is in the connector's
// datasets. With no dataset list, every table the credential reads is.
func (c *Connector) inScope(tables []string) error {
	if len(c.cfg.Datasets) == 0 {
		return nil
	}
	if len(tables) >= maxListedTables {
		return fmt.Errorf("%w: it reads %d or more tables, too many to check", ErrOutOfScope, maxListedTables)
	}
	for _, t := range tables {
		parts := strings.Split(t, ".")
		if len(parts) != 3 || parts[0] != c.cfg.Project || !slices.Contains(c.cfg.Datasets, parts[1]) {
			return fmt.Errorf("%w: %s", ErrOutOfScope, t)
		}
	}
	return nil
}

// clipRows keeps the rows that fit in MaxResultBytes, measured as text.
func clipRows(rows [][]any) ([][]any, bool) {
	if rows == nil {
		return [][]any{}, false
	}
	size := 0
	for i, r := range rows {
		size += len(fmt.Sprint(r...)) + len(r)
		if size > MaxResultBytes {
			return rows[:i], true
		}
	}
	return rows, false
}
