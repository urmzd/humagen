package usage

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/urmzd/humagen/pkg/state"
)

// Settle records a run's total and returns its original contribution even on
// retry, so a crash before the run index is updated cannot lose that spend.
func (l *Ledger) Settle(e Entry) (Spend, error) {
	if e.Kind != Settled || e.Key != "run:"+e.RunID || e.Spend == nil {
		return Spend{}, fmt.Errorf("usage: invalid settlement")
	}
	if _, _, err := l.Record(e); err != nil {
		return Spend{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), state.Timeout)
	defer cancel()
	raw, err := l.st.Get(ctx, namespace, e.Tenant, "settlement:"+e.RunID)
	if err != nil {
		return Spend{}, err
	}
	var r record
	if err := json.Unmarshal(raw, &r); err != nil {
		return Spend{}, err
	}
	if r.Added == nil {
		return Spend{}, fmt.Errorf("usage: settlement has no contribution")
	}
	return *r.Added, nil
}
