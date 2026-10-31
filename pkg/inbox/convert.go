package inbox

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/urmzd/humagen/pkg/state"
)

// legacy reports a key an older release kept: a change, under its number
// in the order it was made, or the tenant's cut.
func legacy(key string) bool {
	return key == cutKey || key != "" && strings.Trim(key, "0123456789") == ""
}

// convert moves the changes an older release kept, one record each in the
// order they were made with a tenant's cut beside them, into the layout by
// message. Each tenant's changes are applied again, in order, in memory,
// and what they leave is written with the old records' deletion in one
// write, conditional on each old record: when a process converting at the
// same time moved them first, the write fails, the old records are gone,
// and there is nothing to do.
func (s *Store) convert() error {
	c, cancel := ctx()
	_, err := s.st.Lookup(c, namespace, "", converted)
	cancel()
	if err == nil {
		return nil
	} else if !errors.Is(err, state.ErrNotFound) {
		return err
	}
	c, cancel = ctx()
	recs, err := s.st.Load(c, namespace)
	cancel()
	if err != nil {
		return err
	}
	byTenant := map[string][]state.Record{}
	var tenants []string
	for _, r := range recs {
		if r.Tenant == "" || !legacy(r.Key) {
			continue
		}
		if byTenant[r.Tenant] == nil {
			tenants = append(tenants, r.Tenant)
		}
		byTenant[r.Tenant] = append(byTenant[r.Tenant], r)
	}
	for _, t := range tenants {
		if err := s.convertTenant(t, byTenant[t]); err != nil {
			return fmt.Errorf("%q: %w", t, err)
		}
	}
	c, cancel = ctx()
	defer cancel()
	if err := s.st.Write(c, namespace, state.Create("", converted, []byte("2"))); err != nil && !errors.Is(err, state.ErrConflict) {
		return err
	}
	return nil
}

func (s *Store) convertTenant(tenantName string, old []state.Record) error {
	for range state.MaxTries {
		ops, err := moved(tenantName, old)
		if err != nil {
			return err
		}
		c, cancel := ctx()
		err = s.st.Write(c, namespace, ops...)
		cancel()
		if !errors.Is(err, state.ErrConflict) {
			return err
		}
		// Another process moved them, or is moving them: read what is left.
		if old, err = legacyOf(s.st, tenantName); err != nil || len(old) == 0 {
			return err
		}
	}
	return state.ErrBusy
}

// legacyOf reads a tenant's records in the older layout.
func legacyOf(st state.Store, tenantName string) ([]state.Record, error) {
	var out []state.Record
	for _, digit := range "0123456789" {
		recs, err := listAll(st, tenantName, string(digit), 1<<30)
		if err != nil {
			return nil, err
		}
		out = append(out, recs...)
	}
	c, cancel := ctx()
	defer cancel()
	r, err := st.Lookup(c, namespace, tenantName, cutKey)
	if err == nil {
		out = append(out, r)
	} else if !errors.Is(err, state.ErrNotFound) {
		return nil, err
	}
	return out, nil
}

// moved is the write that moves a tenant's old records into the layout by
// message: what replaying them leaves, created, and each old record
// deleted at the version read.
func moved(tenantName string, old []state.Record) ([]state.Op, error) {
	var kept cut
	type entry struct {
		key string
		c   Change
	}
	var entries []entry
	for _, r := range old {
		var sc storedChange
		if err := json.Unmarshal(r.Value, &sc); err != nil {
			return nil, fmt.Errorf("%q: %w", r.Key, err)
		}
		if r.Key == cutKey {
			if sc.Cut != nil {
				kept = *sc.Cut
			}
			continue
		}
		entries = append(entries, entry{r.Key, sc.change(tenantName)})
	}
	slices.SortFunc(entries, func(a, b entry) int { return strings.Compare(a.key, b.key) })
	scratch := &Store{st: state.NewMemory(), watchers: map[string]map[int]chan Notice{}}
	for _, e := range entries {
		// The cut names messages whose records were still being deleted:
		// they are gone.
		if slices.Contains(kept.Pending, e.c.Message) {
			continue
		}
		if _, _, err := scratch.change(tenantName, func(x *tx) (Message, bool, error) {
			_, _, err := x.apply(e.c)
			return Message{}, false, err
		}); err != nil {
			return nil, err
		}
	}
	if _, _, err := scratch.change(tenantName, func(x *tx) (Message, bool, error) {
		var h head
		if _, err := x.get(headKey, &h); err != nil {
			return Message{}, false, err
		}
		if int64(kept.Next) > h.Next {
			x.put(headKey, head{Next: int64(kept.Next)})
		}
		for _, key := range kept.Gone {
			var ref keyRef
			if ok, err := x.get(keyKey(key), &ref); err != nil {
				return Message{}, false, err
			} else if !ok {
				x.put(keyKey(key), keyRef{Key: key, Gone: true})
			}
		}
		return Message{}, false, nil
	}); err != nil {
		return nil, err
	}
	c, cancel := ctx()
	defer cancel()
	recs, err := scratch.st.Load(c, namespace)
	if err != nil {
		return nil, err
	}
	ops := make([]state.Op, 0, len(recs)+len(old))
	for _, r := range recs {
		if r.Tenant == tenantName {
			ops = append(ops, state.Create(tenantName, r.Key, r.Value))
		}
	}
	for _, r := range old {
		ops = append(ops, state.DeleteAt(tenantName, r.Key, r.Version))
	}
	return ops, nil
}
