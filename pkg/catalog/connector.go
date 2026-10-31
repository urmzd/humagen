package catalog

import "context"

// Capabilities a connector kind may have (RFC 0005). A kind implements the
// interfaces for the ones it has.
const (
	// CapabilityCatalog lists what exists, with schema and descriptions.
	CapabilityCatalog = "catalog"
	// CapabilityLive searches or reads in place.
	CapabilityLive = "live"
)

// Crawler is the catalog capability: it lists what a system holds. A crawl
// that cannot list everything returns an error, so nothing partial is kept.
type Crawler interface {
	Crawl(ctx context.Context) ([]Entry, error)
}

// Querier is the live capability for a system that answers SQL: a bounded,
// read-only query run in place.
type Querier interface {
	Query(ctx context.Context, sql string) (Result, error)
}

// Result is a live query's answer, bounded by the connector's limits.
type Result struct {
	Columns []string `json:"columns"`
	Rows    [][]any  `json:"rows"`
	// Truncated is set when the query had more rows than were returned.
	Truncated bool `json:"truncated,omitempty"`
	// EstimatedBytes is what the dry run said the query would read, and
	// BilledBytes what the system billed.
	EstimatedBytes int64 `json:"estimated_bytes"`
	BilledBytes    int64 `json:"billed_bytes"`
}
