package platform

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/urmzd/humagen/pkg/state"
)

// recordSet is one namespace of a state.Store that the platform's own stores
// read and write per request, holding values of more than one type: a store
// keeps its records and the counters and markers beside them in one write.
type recordSet struct {
	st        state.Store
	namespace string
}

func storeCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), state.Timeout)
}

// lookup reads one record into v, and returns its version.
func (r recordSet) lookup(tenant, key string, v any) (int64, error) {
	c, cancel := storeCtx()
	defer cancel()
	rec, err := r.st.Lookup(c, r.namespace, tenant, key)
	if err != nil {
		return 0, err
	}
	if err := json.Unmarshal(rec.Value, v); err != nil {
		return 0, fmt.Errorf("platform: %s %q/%q: %w", r.namespace, tenant, key, err)
	}
	return rec.Version, nil
}

// list reads one page of a tenant's records.
func (r recordSet) list(tenant string, q state.Query) ([]state.Record, string, error) {
	c, cancel := storeCtx()
	defer cancel()
	return r.st.List(c, r.namespace, tenant, q)
}

// write applies ops all or none.
func (r recordSet) write(ops ...state.Op) error {
	if len(ops) == 0 {
		return nil
	}
	c, cancel := storeCtx()
	defer cancel()
	return r.st.Write(c, r.namespace, ops...)
}

// recordOp returns an op that writes v under a key at version: state.Missing to
// create it, zero for whatever is there.
func recordOp(tenant, key string, v any, version int64) (state.Op, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return state.Op{}, err
	}
	op := state.Put(tenant, key, raw)
	op.If = version
	return op, nil
}

// versionOf is the version a write names for a record read at version, or
// state.Missing when it was not found (version 0).
func versionOf(version int64) int64 {
	if version == 0 {
		return state.Missing
	}
	return version
}
