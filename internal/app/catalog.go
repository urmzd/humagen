package app

import (
	"context"
	"fmt"
	"time"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/catalog"
	"github.com/urmzd/humagen/pkg/catalog/bigquery"
	"github.com/urmzd/humagen/pkg/catalog/objectstorage"
	"github.com/urmzd/humagen/pkg/catalog/snowflake"
	"github.com/urmzd/humagen/pkg/spec"
)

// openConnectors builds every catalog connector the Platform documents
// declare. A kind's capabilities are the interfaces it implements: a
// registered kind's factory returns a catalog.Crawler, a catalog.Querier or
// both. Nothing is reached until a connector is crawled or queried.
func openConnectors(ctx context.Context, o Options, cfg spec.Config) ([]platform.CatalogConnector, error) {
	var out []platform.CatalogConnector
	for _, doc := range cfg.Platforms {
		for _, c := range doc.Spec.Connectors {
			where := fmt.Sprintf("platform %q: connector %q", doc.Metadata.Name, c.Name)
			var impl any
			switch c.Kind {
			case snowflake.System:
				b, err := spec.Block[spec.SnowflakeConnector](c.Slot)
				if err != nil {
					return nil, err
				}
				config := snowflake.Config{Account: b.Account, Database: b.Database, Warehouse: b.Warehouse, Role: b.Role, MaxRows: b.MaxRows}
				if b.Credential.Set() {
					config.Credential = b.Credential.Ref
				}
				if b.Timeout != "" {
					config.Timeout, err = time.ParseDuration(b.Timeout)
					if err != nil {
						return nil, err
					}
				}
				impl, err = snowflake.New(config)
				if err != nil {
					return nil, fmt.Errorf("%s: %w", where, err)
				}
			case "gcs", "s3":
				b, err := spec.Block[spec.StorageConnector](c.Slot)
				if err != nil {
					return nil, err
				}
				config := objectstorage.Config{System: c.Kind, Bucket: b.Bucket, Prefix: b.Prefix, Region: b.Region}
				if b.Credential.Set() {
					config.Credential = b.Credential.Ref
				}
				impl, err = objectstorage.New(config)
				if err != nil {
					return nil, fmt.Errorf("%s: %w", where, err)
				}
			case bigquery.System:
				b, err := spec.Block[spec.BigQueryConnector](c.Slot)
				if err != nil {
					return nil, fmt.Errorf("%s: %w", where, err)
				}
				bq := bigquery.Config{Project: b.Project, Datasets: b.Datasets, Location: b.Location, MaxBytesBilled: b.MaxBytesBilled, MaxRows: b.MaxRows}
				if b.Credential.Set() {
					bq.Credential = b.Credential.Ref
				}
				if b.Timeout != "" {
					if bq.Timeout, err = time.ParseDuration(b.Timeout); err != nil {
						return nil, fmt.Errorf("%s: timeout: %w", where, err)
					}
				}
				if impl, err = bigquery.New(bq); err != nil {
					return nil, fmt.Errorf("%s: %w", where, err)
				}
			default:
				v, err := build[any](ctx, o, spec.SlotConnectors, c.Slot)
				if err != nil {
					return nil, fmt.Errorf("%s: %w", where, err)
				}
				impl = v
			}
			cc := platform.CatalogConnector{
				Tenant:    doc.Metadata.Name,
				Connector: catalog.Connector{Name: c.Name, System: c.Kind, Owner: c.Owner, Project: c.Project},
			}
			if c.Refresh != "" {
				d, err := time.ParseDuration(c.Refresh)
				if err != nil {
					return nil, fmt.Errorf("%s: refresh: %w", where, err)
				}
				cc.Refresh = d
			}
			cc.Crawler, _ = impl.(catalog.Crawler)
			cc.Querier, _ = impl.(catalog.Querier)
			cc.Reader, _ = impl.(catalog.ReaderQuerier)
			if cc.Crawler == nil && cc.Querier == nil {
				return nil, fmt.Errorf("%s: kind %q can neither list a catalog nor answer a query", where, c.Kind)
			}
			out = append(out, cc)
		}
	}
	return out, nil
}
