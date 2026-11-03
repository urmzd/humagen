package runs

import "slices"

// Bounds on what a run's memory record keeps.
const (
	// MaxMemoryTrees is the most memory trees one run's record names.
	MaxMemoryTrees = 16
	// MaxMemoryProposals is the most proposals one tree's record names.
	MaxMemoryProposals = 16
)

// MemoryUse is one memory tree a run's instance was given (RFC 0004, "Where
// the files live"): which tree, where, at which version, and what the run
// kept of its changes when the instance was released.
type MemoryUse struct {
	// Kind is person, project or agent; Person, Project and Agent name the
	// tree as pkg/drive does.
	Kind     string `json:"kind"`
	Person   string `json:"person,omitempty"`
	Project  string `json:"project,omitempty"`
	Agent    string `json:"agent,omitempty"`
	Dir      string `json:"dir"`
	ReadOnly bool   `json:"read_only,omitempty"`
	// Version is the version installed, 0 for a tree with none yet; Files
	// is how many files it held.
	Version int64 `json:"version"`
	Files   int   `json:"files"`
	// Committed is the version the run's changes made, when it could write
	// the tree. Proposals are the proposals it made where it could not.
	// Dropped counts changes that were not kept.
	Committed int64    `json:"committed,omitempty"`
	Proposals []string `json:"proposals,omitempty"`
	Dropped   int      `json:"dropped,omitempty"`
	Finished  bool     `json:"finished,omitempty"`
}

// Key names the tree a use is of.
func (m MemoryUse) Key() string {
	return m.Kind + "\x00" + m.Person + "\x00" + m.Project + "\x00" + m.Agent
}

// RecordMemory keeps the memory trees a run used and what it wrote to them:
// installed is recorded when the instance is leased, finished when it is
// released. A use of a tree already recorded is replaced, field by field,
// by what is newer. A run with no record is skipped.
func (x *Index) RecordMemory(tenant, runID string, uses []MemoryUse) error {
	if len(uses) == 0 {
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
		next := slices.Clone(r.Memory)
		for _, u := range uses {
			if len(u.Proposals) > MaxMemoryProposals {
				u.Proposals = u.Proposals[:MaxMemoryProposals]
			}
			u.Proposals = slices.Clone(u.Proposals)
			i := slices.IndexFunc(next, func(m MemoryUse) bool { return m.Key() == u.Key() })
			if i < 0 {
				if len(next) >= MaxMemoryTrees {
					continue
				}
				next = append(next, u)
				continue
			}
			if u.Finished {
				// A release keeps what the lease recorded of the install.
				u.Version, u.Files = next[i].Version, next[i].Files
			}
			next[i] = u
		}
		r.Memory = next
		t.touch(r)
		return nil
	})
}
