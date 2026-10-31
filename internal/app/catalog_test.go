package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/catalog"
	"github.com/urmzd/humagen/pkg/catalog/bigquery"
	"github.com/urmzd/humagen/pkg/spec"
)

func connectorsFrom(cs ...spec.Connector) spec.Config {
	return spec.Config{Platforms: []spec.Platform{{Metadata: spec.Metadata{Name: "acme"}, Spec: spec.PlatformSpec{Connectors: cs}}}}
}

type crawlOnly struct{}

func (crawlOnly) Crawl(context.Context) ([]catalog.Entry, error) { return nil, nil }

func TestTheSpecDeclaresConnectors(t *testing.T) {
	ctx := context.Background()
	bq := spec.Connector{
		Slot:            spec.Slot{Kind: "bigquery", Block: spec.MustJSON(spec.BigQueryConnector{Project: "acme-data", Datasets: []string{"billing"}, MaxRows: 50, Timeout: "30s"})},
		ConnectorShared: spec.ConnectorShared{Name: "warehouse", Owner: "ana", Project: "data", Refresh: "6h"},
	}
	got, err := openConnectors(ctx, Options{}, connectorsFrom(bq))
	if err != nil || len(got) != 1 {
		t.Fatalf("connectors = %+v, %v", got, err)
	}
	c := got[0]
	if c.Tenant != "acme" || c.Name != "warehouse" || c.System != "bigquery" || c.Owner != "ana" || c.Project != "data" || c.Refresh != 6*time.Hour {
		t.Fatalf("connector = %+v", c)
	}
	conn, ok := c.Querier.(*bigquery.Connector)
	if !ok || c.Crawler == nil {
		t.Fatalf("bigquery is both capabilities: %T %T", c.Crawler, c.Querier)
	}
	if cfg := conn.Config(); cfg.Project != "acme-data" || cfg.MaxRows != 50 || cfg.Timeout != 30*time.Second || cfg.MaxBytesBilled != bigquery.DefaultMaxBytesBilled {
		t.Fatalf("config = %+v", cfg)
	}

	// A registered kind's capabilities are the interfaces it implements.
	ext := spec.Connector{Slot: spec.Slot{Kind: "folders"}, ConnectorShared: spec.ConnectorShared{Name: "files", Owner: "ana"}}
	o := Options{Kinds: map[string]map[string]Factory{spec.SlotConnectors: {"folders": func(context.Context, json.RawMessage) (any, error) { return crawlOnly{}, nil }}}}
	got, err = openConnectors(ctx, o, connectorsFrom(ext))
	if err != nil || got[0].Crawler == nil || got[0].Querier != nil {
		t.Fatalf("extension = %+v, %v", got, err)
	}
	o.Kinds[spec.SlotConnectors]["folders"] = func(context.Context, json.RawMessage) (any, error) { return struct{}{}, nil }
	if _, err := openConnectors(ctx, o, connectorsFrom(ext)); err == nil || !strings.Contains(err.Error(), "neither") {
		t.Fatalf("a kind with no capability = %v", err)
	}
}
