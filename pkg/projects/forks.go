package projects

import (
	"fmt"
	"time"

	"github.com/urmzd/humagen/pkg/state"
)

// ForkOrigin is immutable lineage and the authoritative publication gate for
// a staged project fork. Ready is changed only by CompleteFork.
type ForkOrigin struct {
	Operation string    `json:"operation"`
	Source    string    `json:"source,omitempty"`
	By        string    `json:"by"`
	At        time.Time `json:"at"`
	Ready     bool      `json:"ready"`
}

// PendingFork denies generic reads and execution while any ancestor is staged.
func (t *Tree) PendingFork(name string) bool {
	for _, pr := range t.Chain(name) {
		if pr.Fork != nil && !pr.Fork.Ready {
			return true
		}
	}
	return false
}

// CompleteFork publishes a bounded prepared subtree in one durable write. The
// operation creator must own every staged record; no ordinary update can flip it.
func (s *Store) CompleteFork(tenantName, operation string, names []string, w Writer, at time.Time) error {
	if operation == "" || len(names) == 0 || len(names) > 20 {
		return fmt.Errorf("%w: a fork publishes at most 20 projects", ErrInvalid)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return state.Retry(func() error { return s.completeFork(tenantName, operation, names, w, at) })
}

// completeFork is one attempt at CompleteFork.
func (s *Store) completeFork(tenantName, operation string, names []string, w Writer, at time.Time) error {
	tx, err := s.begin(tenantName)
	if err != nil {
		return err
	}
	selected := map[string]bool{}
	for _, name := range names {
		if selected[name] {
			return ErrInvalid
		}
		selected[name] = true
	}
	for name, pr := range tx.t.projects {
		if pr.Fork != nil && pr.Fork.Operation == operation && !selected[name] {
			return ErrInvalid
		}
	}
	for _, name := range names {
		pr, ok := tx.t.projects[name]
		if !ok || pr.Fork == nil || pr.Fork.Operation != operation || pr.Fork.By != w.By {
			return ErrNotFound
		}
		if pr.Fork.Ready {
			continue
		}
		before := pr.clone()
		pr = pr.clone()
		pr.Fork.Ready = true
		pr.Version++
		pr.UpdatedAt = at
		pr.UpdatedBy = w.By
		tx.project(pr)
		tx.record(Change{Project: pr.Name, By: w.By, At: at, What: Updated, Before: ptr(before), After: ptr(pr), Note: "Fork ready"})
	}
	return tx.commit()
}

func sameForkOrigin(a, b *ForkOrigin) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
