package sourcelinks

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"

	"github.com/urmzd/humagen/pkg/state"
)

func ctx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), state.Timeout)
}

// lookup reads one record into v, and its version.
func lookup(st state.Store, namespace, tenant, key string, v any) (int64, error) {
	c, cancel := ctx()
	defer cancel()
	r, err := st.Lookup(c, namespace, tenant, key)
	if err != nil {
		return 0, err
	}
	if err := json.Unmarshal(r.Value, v); err != nil {
		return 0, fmt.Errorf("source links: %q: %w", key, err)
	}
	return r.Version, nil
}

// all reads every record of a tenant under a prefix, failing with
// state.ErrTooMany past most.
func all(st state.Store, namespace, tenant, prefix string, most int) ([]state.Record, error) {
	var out []state.Record
	after := ""
	for {
		c, cancel := ctx()
		page, next, err := st.List(c, namespace, tenant, state.Query{Prefix: prefix, After: after, Limit: min(most+1-len(out), state.MaxList)})
		cancel()
		if err != nil {
			return nil, err
		}
		out = append(out, page...)
		if len(out) > most {
			return nil, fmt.Errorf("%w: %s %q under %q holds more than %d", state.ErrTooMany, namespace, tenant, prefix, most)
		}
		if next == "" {
			return out, nil
		}
		after = next
	}
}

// write is one write's ops, built as JSON.
type write struct {
	ops []state.Op
	err error
}

// put adds a put at version: state.Missing to create, 0 for any.
func (w *write) put(tenant, key string, v any, version int64) {
	raw, err := json.Marshal(v)
	if err != nil {
		w.err = cmp.Or(w.err, err)
		return
	}
	op := state.Put(tenant, key, raw)
	op.If = version
	w.ops = append(w.ops, op)
}

// bump moves a stamp read at version (0 when missing), so a write that
// counted under it conflicts with another that did too.
func (w *write) bump(tenant, key string, version int64) {
	if version == 0 {
		version = state.Missing
	}
	w.put(tenant, key, version+1, version)
}

func (w *write) commit(st state.Store, namespace string) error {
	if w.err != nil {
		return w.err
	}
	if len(w.ops) == 0 {
		return nil
	}
	c, cancel := ctx()
	defer cancel()
	return st.Write(c, namespace, w.ops...)
}
