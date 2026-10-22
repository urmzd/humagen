package policy

import (
	"fmt"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/urmzd/warp/pkg/state"
)

// Change is one entry in a tenant's policy history: who changed which scope's
// policy, when, and what it was before and after. A delete has no After, and a
// first write has no Before.
type Change struct {
	Scope  Scope     `json:"scope"`
	By     string    `json:"by"`
	At     time.Time `json:"at"`
	Before *Policy   `json:"before,omitempty"`
	After  *Policy   `json:"after,omitempty"`
}

// maxHistory bounds a tenant's history. The oldest changes are dropped first.
const maxHistory = 1000

type tenant struct {
	policies []Policy
	history  []Change
	// versions outlive a delete, so a policy set again continues its count
	// and a version always names one state.
	versions map[Scope]int
}

// Store holds every tenant's policies and the history of their changes. It is
// in memory. Built with Open, it writes every change through a state.Store
// and reads them back when it is built again.
type Store struct {
	mu      sync.RWMutex
	tenants map[string]*tenant
	// kept is where each tenant's policies are written, all of a tenant's
	// in one record. Nil keeps them only in memory.
	kept *state.Table[stored]
}

// stored is a tenant as it is written.
type stored struct {
	Policies []Policy  `json:"policies"`
	History  []Change  `json:"history"`
	Versions []version `json:"versions"`
}

type version struct {
	Scope   Scope `json:"scope"`
	Version int   `json:"version"`
}

// tenantKey is the one key a tenant's record is kept under.
const tenantKey = "policies"

// NewStore returns an empty store in memory.
func NewStore() *Store { return &Store{tenants: map[string]*tenant{}} }

// Open returns a store that keeps every policy in st, holding what st
// already has. A nil st is NewStore.
func Open(st state.Store) (*Store, error) {
	s := NewStore()
	s.kept = state.NewTable[stored](st, "policy")
	err := s.kept.Load(func(name, _ string, r stored) error {
		t := s.of(name)
		t.policies, t.history = r.Policies, r.History
		for _, v := range r.Versions {
			t.versions[v.Scope] = v.Version
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("policy: load: %w", err)
	}
	return s, nil
}

func (t *tenant) copy() tenant {
	return tenant{policies: slices.Clone(t.policies), history: slices.Clone(t.history), versions: maps.Clone(t.versions)}
}

// save writes a tenant, or puts back how it was if the write fails. It is
// called with the lock held.
func (s *Store) save(name string, t *tenant, before tenant) error {
	if !s.kept.Enabled() {
		return nil
	}
	r := stored{Policies: t.policies, History: t.history}
	for scope, v := range t.versions {
		r.Versions = append(r.Versions, version{Scope: scope, Version: v})
	}
	slices.SortFunc(r.Versions, func(a, b version) int {
		if a.Scope.Kind != b.Scope.Kind {
			if a.Scope.Kind < b.Scope.Kind {
				return -1
			}
			return 1
		}
		if a.Scope.Name < b.Scope.Name {
			return -1
		}
		if a.Scope.Name > b.Scope.Name {
			return 1
		}
		return 0
	})
	if err := s.kept.Put(name, tenantKey, r); err != nil {
		*t = before
		return fmt.Errorf("policy: write: %w", err)
	}
	return nil
}

func (s *Store) of(name string) *tenant {
	t, ok := s.tenants[name]
	if !ok {
		t = &tenant{versions: map[Scope]int{}}
		s.tenants[name] = t
	}
	return t
}

// Put sets a scope's policy and records the change. A narrower policy must fit
// within the tenant's, and tightening the tenant's must not leave a narrower
// one claiming more than it.
func (s *Store) Put(tenantName string, p Policy, by string, at time.Time) (Policy, error) {
	if err := p.Validate(); err != nil {
		return Policy{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.of(tenantName)
	for _, other := range t.policies {
		switch {
		case p.Scope.Kind != Tenant && other.Scope.Kind == Tenant:
			if err := p.Within(other); err != nil {
				return Policy{}, err
			}
		case p.Scope.Kind == Tenant && other.Scope.Kind != Tenant:
			if err := other.Within(p); err != nil {
				return Policy{}, fmt.Errorf("%w. Lower that one first", err)
			}
		}
	}
	before := t.copy()
	t.versions[p.Scope]++
	p.Version, p.UpdatedBy, p.UpdatedAt = t.versions[p.Scope], by, at
	change := Change{Scope: p.Scope, By: by, At: at, After: &p}
	if i := slices.IndexFunc(t.policies, func(x Policy) bool { return x.Scope == p.Scope }); i >= 0 {
		before := t.policies[i]
		change.Before = &before
		t.policies[i] = p
	} else {
		t.policies = append(t.policies, p)
	}
	t.record(change)
	if err := s.save(tenantName, t, before); err != nil {
		return Policy{}, err
	}
	return p, nil
}

func (t *tenant) record(c Change) {
	t.history = append(t.history, c)
	if len(t.history) > maxHistory {
		t.history = t.history[len(t.history)-maxHistory:]
	}
}

// Delete removes a scope's policy and records the change.
func (s *Store) Delete(tenantName string, scope Scope, by string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.of(tenantName)
	i := slices.IndexFunc(t.policies, func(x Policy) bool { return x.Scope == scope })
	if i < 0 {
		return fmt.Errorf("%w: no policy for %s", ErrNotFound, scope)
	}
	saved := t.copy()
	before := t.policies[i]
	t.policies = slices.Delete(slices.Clone(t.policies), i, i+1)
	t.record(Change{Scope: scope, By: by, At: at, Before: &before})
	return s.save(tenantName, t, saved)
}

// List returns a tenant's policies: the tenant's first, then by kind and name.
func (s *Store) List(tenantName string) []Policy {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.tenants[tenantName]
	if !ok {
		return []Policy{}
	}
	out := slices.Clone(t.policies)
	order := map[Kind]int{Tenant: 0, Project: 1, Agent: 2, Client: 3}
	slices.SortFunc(out, func(a, b Policy) int {
		if d := order[a.Scope.Kind] - order[b.Scope.Kind]; d != 0 {
			return d
		}
		if a.Scope.Name < b.Scope.Name {
			return -1
		}
		if a.Scope.Name > b.Scope.Name {
			return 1
		}
		return 0
	})
	return out
}

// For returns the policies a run falls under, most specific first: its
// agent's, the client's that raised it, then its project's and each of the
// project's ancestors' nearest first, then the tenant's. chain is the project
// and its ancestors, nearest first, the root's "" left out or skipped. An empty
// agent or client means the run has no such scope. Decide applies them all,
// so the strictest wins, and a project's limit holds everything below it.
func (s *Store) For(tenantName string, chain []string, agent, client string) []Policy {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.tenants[tenantName]
	if !ok {
		return nil
	}
	want := make([]Scope, 0, len(chain)+3)
	want = append(want, Scope{Agent, agent}, Scope{Client, client})
	for _, project := range chain {
		want = append(want, Scope{Project, project})
	}
	want = append(want, Scope{Kind: Tenant})
	var out []Policy
	for _, w := range want {
		if w.Kind != Tenant && w.Name == "" {
			continue
		}
		if i := slices.IndexFunc(t.policies, func(x Policy) bool { return x.Scope == w }); i >= 0 {
			out = append(out, t.policies[i])
		}
	}
	return out
}

// History returns a tenant's policy changes, newest first.
func (s *Store) History(tenantName string) []Change {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.tenants[tenantName]
	if !ok {
		return []Change{}
	}
	out := slices.Clone(t.history)
	slices.Reverse(out)
	return out
}

// At returns the policy a scope had at a moment, so "what was the limit when
// this run was refused" has an answer for as long as the history reaches.
func (s *Store) At(tenantName string, scope Scope, when time.Time) (Policy, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.tenants[tenantName]
	if !ok {
		return Policy{}, false
	}
	var found *Policy
	for _, c := range t.history {
		if c.Scope != scope || c.At.After(when) {
			continue
		}
		found = c.After
	}
	if found == nil {
		return Policy{}, false
	}
	return *found, true
}
