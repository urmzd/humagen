package usage

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/urmzd/humagen/internal/legacy"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

// admitUnder admits e, refusing it when the tenant has started max runs
// today; max 0 is no limit.
func admitUnder(l *Ledger, e Entry, max int) (bool, error) {
	if max == 0 {
		return l.Admit(e, nil)
	}
	return l.Admit(e, nil, Quota{Scope: all, Kind: QuotaRuns, Since: t0, Max: max})
}

var benchSchemas atomic.Int64

// benchProcesses is n ledgers, each on its own pool of 16, on one new schema.
func benchProcesses(b *testing.B, n int) []*Ledger {
	b.Helper()
	url := legacy.Getenv(statetest.URLEnv)
	if url == "" {
		b.Skip("set " + statetest.URLEnv)
	}
	ctx := context.Background()
	schema := fmt.Sprintf("humagenbench_%d_%d", time.Now().UnixNano(), benchSchemas.Add(1))
	var pools []*pgxpool.Pool
	for i := range n {
		cfg, err := pgxpool.ParseConfig(url)
		if err != nil {
			b.Fatal(err)
		}
		cfg.MaxConns = 16
		cfg.ConnConfig.RuntimeParams["search_path"] = schema
		p, err := pgxpool.NewWithConfig(ctx, cfg)
		if err != nil {
			b.Fatal(err)
		}
		if i == 0 {
			if _, err := p.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
				b.Fatal(err)
			}
		}
		pools = append(pools, p)
	}
	b.Cleanup(func() {
		for _, p := range pools[1:] {
			p.Close()
		}
		_, _ = pools[0].Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
		pools[0].Close()
	})
	out := make([]*Ledger, n)
	for i, p := range pools {
		c, err := OpenCounters(ctx, p)
		if err != nil {
			b.Fatal(err)
		}
		st, err := state.OpenPostgresShared(ctx, p)
		if err != nil {
			b.Fatal(err)
		}
		if out[i], err = Open(st, WithCounters(c)); err != nil {
			b.Fatal(err)
		}
	}
	return out
}
