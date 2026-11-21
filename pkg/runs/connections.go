package runs

import "slices"

// MaxConnectionNeeds is the most connections one run's record names.
const MaxConnectionNeeds = 8

// ConnectionNeed is a connection a run needed and the person it acts for had
// not made: a tool group the run was granted could not be opened without
// that person's own credential. Provider is the provider's name, as
// Connections names it; Name is how people know it, when known.
type ConnectionNeed struct {
	Provider string `json:"provider"`
	Name     string `json:"name,omitempty"`
}

// RecordConnectionNeed keeps on a run's record that it needed a connection.
// A provider already recorded is kept once, and at most MaxConnectionNeeds
// are kept. A run with no record is ErrNotFound; an empty provider is
// nothing to record.
func (x *Index) RecordConnectionNeed(tenant, runID string, need ConnectionNeed) error {
	if need.Provider == "" {
		return nil
	}
	return x.do(tenant, func(t *txn) error {
		r, err := t.run(runID)
		if err != nil {
			return err
		}
		if r == nil {
			return ErrNotFound
		}
		if slices.ContainsFunc(r.NeedsConnection, func(n ConnectionNeed) bool { return n.Provider == need.Provider }) ||
			len(r.NeedsConnection) >= MaxConnectionNeeds {
			return nil
		}
		r.NeedsConnection = append(slices.Clone(r.NeedsConnection), need)
		t.touch(r)
		return nil
	})
}
