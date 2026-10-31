package library

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/urmzd/humagen/pkg/state"
)

// converted marks, in the tenant "" of a namespace, that the index and
// counter records for what an earlier release kept are written.
const converted = "converted:v1"

// kv reads and writes one namespace of a state.Store as JSON, per request.
type kv struct {
	st        state.Store
	namespace string
}

func ctx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), state.Timeout)
}

// lookup reads one record into v, and its version. A missing record is
// state.ErrNotFound.
func (k kv) lookup(tenant, key string, v any) (int64, error) {
	c, cancel := ctx()
	defer cancel()
	r, err := k.st.Lookup(c, k.namespace, tenant, key)
	if err != nil {
		return 0, err
	}
	if err := json.Unmarshal(r.Value, v); err != nil {
		return 0, fmt.Errorf("library: %s %q/%q: %w", k.namespace, tenant, key, err)
	}
	return r.Version, nil
}

// lookupMaybe is lookup with a missing record as version 0 and no error.
func (k kv) lookupMaybe(tenant, key string, v any) (int64, error) {
	ver, err := k.lookup(tenant, key, v)
	if errors.Is(err, state.ErrNotFound) {
		return 0, nil
	}
	return ver, err
}

func (k kv) write(ops ...state.Op) error {
	if len(ops) == 0 {
		return nil
	}
	c, cancel := ctx()
	defer cancel()
	return k.st.Write(c, k.namespace, ops...)
}

// ops builds one write's ops as JSON.
type ops struct {
	list []state.Op
	err  error
}

// put adds a put at version: 0 creates the record, any other replaces it
// only at that version.
func (o *ops) put(tenant, key string, v any, version int64) {
	raw, err := json.Marshal(v)
	if err != nil {
		o.err = cmp.Or(o.err, err)
		return
	}
	if version == 0 {
		o.list = append(o.list, state.Create(tenant, key, raw))
		return
	}
	o.list = append(o.list, state.PutAt(tenant, key, raw, version))
}

// set adds an unconditional put.
func (o *ops) set(tenant, key string, v any) {
	raw, err := json.Marshal(v)
	if err != nil {
		o.err = cmp.Or(o.err, err)
		return
	}
	o.list = append(o.list, state.Put(tenant, key, raw))
}

func (o *ops) del(tenant, key string) { o.list = append(o.list, state.Delete(tenant, key)) }

func (o *ops) commit(k kv) error {
	if o.err != nil {
		return o.err
	}
	return k.write(o.list...)
}

// convertOnce runs fill unless the namespace's marker says it already ran,
// then writes the marker. fill must be safe to run in several processes at
// once and to run again after a crash.
func (k kv) convertOnce(fill func(recs []state.Record) error) error {
	var done int
	if _, err := k.lookup("", converted, &done); err == nil {
		return nil
	} else if !errors.Is(err, state.ErrNotFound) {
		return err
	}
	c, cancel := ctx()
	recs, err := k.st.Load(c, k.namespace)
	cancel()
	if err != nil {
		return err
	}
	kept := recs[:0:0]
	for _, r := range recs {
		if r.Tenant != "" {
			kept = append(kept, r)
		}
	}
	if err := fill(kept); err != nil {
		return err
	}
	o := &ops{}
	o.put("", converted, 1, 0)
	if err := o.commit(k); err != nil && !errors.Is(err, state.ErrConflict) {
		return err
	}
	return nil
}

// createAll writes each op, which creates a record, and leaves one whose
// record another process created first.
func (k kv) createAll(list []state.Op) error {
	for len(list) > 0 {
		n := min(len(list), state.MaxList)
		err := k.write(list[:n]...)
		if errors.Is(err, state.ErrConflict) {
			for _, op := range list[:n] {
				if err := k.write(op); err != nil && !errors.Is(err, state.ErrConflict) {
					return err
				}
			}
		} else if err != nil {
			return err
		}
		list = list[n:]
	}
	return nil
}
