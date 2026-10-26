package httpapi

import (
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/urmzd/fiber/pkg/envelope"
	"github.com/urmzd/humagen/pkg/runs"
)

func TestAnalyticsSelectedDayUsesExactBoundedInterval(t *testing.T) {
	f := newFixture(t)
	token := f.machine(t, "acme", everything...)
	start := time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC)
	for _, row := range []struct {
		id, tenant string
		at         time.Time
	}{
		{"before", "acme", start.Add(-time.Nanosecond)}, {"inside", "acme", start},
		{"end", "acme", start.Add(24 * time.Hour)}, {"other", "other", start},
	} {
		if err := f.p.Runs.Record(row.id, envelope.Tag{ID: row.id, Tenant: row.tenant, Agent: "writer", Origin: runs.OriginAPI, At: row.at}, 1); err != nil {
			t.Fatal(err)
		}
	}
	query := url.Values{"since": {start.Format(time.RFC3339Nano)}, "until": {start.Add(24 * time.Hour).Format(time.RFC3339Nano)}, "bucket": {"24h"}}
	res, out := f.do(t, nil, "GET", "/api/analytics?"+query.Encode(), token, "")
	if res.StatusCode != http.StatusOK || out["total"].(map[string]any)["runs"] != float64(1) {
		t.Fatalf("day bounds: %d %+v", res.StatusCode, out)
	}
	for _, query := range []string{
		"since=2026-10-17T00:00:00Z", "until=2026-10-18T00:00:00Z",
		"since=bad&until=2026-10-18T00:00:00Z", "since=2026-10-18T00:00:00Z&until=2026-10-17T00:00:00Z",
		"since=2026-01-01T00:00:00Z&until=2026-10-18T00:00:00Z", "window=1h&since=2026-10-17T00:00:00Z&until=2026-10-18T00:00:00Z",
	} {
		if res, _ := f.do(t, nil, "GET", "/api/analytics?"+query, token, ""); res.StatusCode != http.StatusBadRequest {
			t.Errorf("unbounded/ambiguous interval accepted: %s = %d", query, res.StatusCode)
		}
	}
}
