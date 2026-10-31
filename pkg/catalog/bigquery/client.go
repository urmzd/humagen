package bigquery

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	bq "cloud.google.com/go/bigquery"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"

	"github.com/urmzd/humagen/pkg/catalog"
	"github.com/urmzd/humagen/pkg/credref"
)

// client is api over the BigQuery client library.
type client struct {
	c   *bq.Client
	cfg Config
}

// dial opens a client for one call. The credential reference is read each
// time, so a rotated key is used on the next call.
func dial(ctx context.Context, cfg Config) (api, error) {
	var opts []option.ClientOption
	if cfg.Credential != "" {
		key, err := credref.Default.Resolve(ctx, cfg.Credential)
		if err != nil {
			return nil, err
		}
		opts = append(opts, option.WithAuthCredentialsJSON(option.ServiceAccount, []byte(key)))
	}
	c, err := bq.NewClient(ctx, cfg.Project, opts...)
	if err != nil {
		return nil, fmt.Errorf("bigquery: connect: %w", err)
	}
	if cfg.Location != "" {
		c.Location = cfg.Location
	}
	return &client{c: c, cfg: cfg}, nil
}

func (k *client) Close() error { return k.c.Close() }

func (k *client) Datasets(ctx context.Context) ([]dataset, error) {
	var out []dataset
	it := k.c.Datasets(ctx)
	for {
		ds, err := it.Next()
		if errors.Is(err, iterator.Done) {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
		md, err := ds.Metadata(ctx)
		if err != nil {
			return nil, err
		}
		out = append(out, dataset{ID: ds.DatasetID, Description: md.Description})
		if len(out) > catalog.MaxEntries {
			return out, nil
		}
	}
}

func (k *client) Tables(ctx context.Context, dataset string) ([]string, error) {
	var out []string
	it := k.c.Dataset(dataset).Tables(ctx)
	for {
		t, err := it.Next()
		if errors.Is(err, iterator.Done) {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
		out = append(out, t.TableID)
		if len(out) > catalog.MaxEntries {
			return out, nil
		}
	}
}

func (k *client) Table(ctx context.Context, dataset, name string) (table, error) {
	md, err := k.c.Dataset(dataset).Table(name).Metadata(ctx)
	if err != nil {
		return table{}, err
	}
	t := table{Kind: kindOf(md.Type), Title: md.Name, Description: md.Description, Schema: columns(md.Schema)}
	if md.Type == bq.RegularTable {
		rows, size := int64(md.NumRows), md.NumBytes
		t.Stats.Rows, t.Stats.Bytes = &rows, &size
	}
	if !md.LastModifiedTime.IsZero() {
		at := md.LastModifiedTime.UTC()
		t.Stats.Modified = &at
	}
	return t, nil
}

func kindOf(t bq.TableType) string {
	switch t {
	case bq.ViewTable:
		return "view"
	case bq.MaterializedView:
		return "materialized_view"
	case bq.ExternalTable:
		return "external"
	case bq.Snapshot:
		return "snapshot"
	}
	return "table"
}

func columns(s bq.Schema) []catalog.Column {
	if len(s) == 0 {
		return nil
	}
	out := make([]catalog.Column, 0, len(s))
	for _, f := range s {
		mode := "NULLABLE"
		switch {
		case f.Repeated:
			mode = "REPEATED"
		case f.Required:
			mode = "REQUIRED"
		}
		out = append(out, catalog.Column{Name: f.Name, Type: string(f.Type), Mode: mode, Description: f.Description, Fields: columns(f.Schema)})
	}
	return out
}

// labels mark every job Humagen runs, so its cost can be told apart.
var labels = map[string]string{"humagen": "catalog"}

func (k *client) query(sql string) *bq.Query {
	q := k.c.Query(sql)
	q.Labels = labels
	return q
}

func (k *client) DryRun(ctx context.Context, sql string) (plan, error) {
	q := k.query(sql)
	q.DryRun = true
	job, err := q.Run(ctx)
	if err != nil {
		return plan{}, err
	}
	st := job.LastStatus()
	if st == nil || st.Statistics == nil {
		return plan{}, errors.New("no statistics from the dry run")
	}
	if err := st.Err(); err != nil {
		return plan{}, err
	}
	details, ok := st.Statistics.Details.(*bq.QueryStatistics)
	if !ok {
		return plan{}, errors.New("no query statistics from the dry run")
	}
	p := plan{StatementType: details.StatementType, Bytes: st.Statistics.TotalBytesProcessed}
	for _, t := range details.ReferencedTables {
		p.Tables = append(p.Tables, t.ProjectID+"."+t.DatasetID+"."+t.TableID)
	}
	return p, nil
}

func (k *client) Run(ctx context.Context, sql string, maxBytes int64, maxRows int) (answer, error) {
	q := k.query(sql)
	q.MaxBytesBilled = maxBytes
	if dl, ok := ctx.Deadline(); ok {
		q.JobTimeout = time.Until(dl)
	}
	job, err := q.Run(ctx)
	if err != nil {
		return answer{}, err
	}
	st, err := job.Wait(ctx)
	if err != nil {
		_ = job.Cancel(context.WithoutCancel(ctx))
		return answer{}, err
	}
	if err := st.Err(); err != nil {
		return answer{}, err
	}
	it, err := job.Read(ctx)
	if err != nil {
		return answer{}, err
	}
	var a answer
	if st.Statistics != nil {
		if d, ok := st.Statistics.Details.(*bq.QueryStatistics); ok {
			a.Billed = d.TotalBytesBilled
		}
	}
	a.Rows = [][]any{}
	for {
		var row []bq.Value
		err := it.Next(&row)
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return answer{}, err
		}
		if len(a.Rows) == maxRows {
			a.Truncated = true
			break
		}
		vals := make([]any, len(row))
		for i, v := range row {
			vals[i] = plain(v)
		}
		a.Rows = append(a.Rows, vals)
	}
	for _, f := range it.Schema {
		a.Columns = append(a.Columns, f.Name)
	}
	return a, nil
}

// plain turns a BigQuery value into one that encodes to JSON as people
// read it: numerics as decimals, dates and times as text, and records and
// arrays element by element.
func plain(v bq.Value) any {
	switch x := v.(type) {
	case nil, bool, int64, float64, string, []byte, time.Time:
		return x
	case *big.Rat:
		return strings.TrimRight(strings.TrimRight(x.FloatString(9), "0"), ".")
	case []bq.Value:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = plain(e)
		}
		return out
	case map[string]bq.Value:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[k] = plain(e)
		}
		return out
	case fmt.Stringer:
		return x.String()
	}
	return fmt.Sprint(v)
}
