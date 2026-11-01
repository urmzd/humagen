package bigquery

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

// TestBigQueryLive runs against a real project with Application Default
// Credentials (or HUMAGEN_BIGQUERY_LIVE_CREDENTIAL, a reference). It skips
// unless HUMAGEN_BIGQUERY_LIVE_PROJECT names the project to bill;
// HUMAGEN_BIGQUERY_LIVE_DATASET, when set, is crawled. The query reads a
// public dataset and costs at most the 10 MB minimum.
func TestBigQueryLive(t *testing.T) {
	project := os.Getenv("HUMAGEN_BIGQUERY_LIVE_PROJECT")
	if project == "" {
		t.Skip("HUMAGEN_BIGQUERY_LIVE_PROJECT is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cfg := Config{Project: project, Credential: os.Getenv("HUMAGEN_BIGQUERY_LIVE_CREDENTIAL"), MaxBytesBilled: 100 << 20, MaxRows: 5}
	if ds := os.Getenv("HUMAGEN_BIGQUERY_LIVE_DATASET"); ds != "" {
		cfg.Datasets = []string{ds}
		c, err := New(cfg)
		if err != nil {
			t.Fatal(err)
		}
		entries, err := c.Crawl(ctx)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("crawled %d entries from %s.%s", len(entries), project, ds)
		cfg.Datasets = nil
	}
	c, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	r, err := c.Query(ctx, "SELECT name, SUM(number) AS n FROM `bigquery-public-data.usa_names.usa_1910_current` WHERE state = 'TX' GROUP BY name ORDER BY n DESC")
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Rows) != 5 || !r.Truncated || r.EstimatedBytes <= 0 {
		t.Fatalf("result = %+v", r)
	}
	t.Logf("top name %v; estimated %d bytes, billed %d", r.Rows[0], r.EstimatedBytes, r.BilledBytes)

	_, err = c.Query(ctx, "CREATE TEMP TABLE x AS SELECT 1")
	if err == nil || (!errors.Is(err, ErrNotReadOnly) && !strings.Contains(err.Error(), "dry run")) {
		t.Fatalf("a non-SELECT was not refused: %v", err)
	}
	tight, _ := New(Config{Project: project, Credential: cfg.Credential, MaxBytesBilled: 1})
	if _, err := tight.Query(ctx, "SELECT * FROM `bigquery-public-data.usa_names.usa_1910_current`"); !errors.Is(err, ErrTooCostly) {
		t.Fatalf("over the cap = %v", err)
	}
}
