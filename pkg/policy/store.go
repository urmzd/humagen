package policy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/urmzd/humagen/pkg/state"
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

// atPage is how many changes At reads at a time, newest first.
const atPage = 100

// namespace is where policies are kept. The tenant is the shard key; within
// it:
//
//	policies    the tenant's policies, their versions and the history's head
//	h:<seq>     one change, numbered from 1 in the order it was made
//
// Every change writes "policies" at the version it read, its change and the
// removal of the change maxHistory before it, in one write.
const namespace = "policy"

// tenantKey is the key a tenant's policies are kept under.
const tenantKey = "policies"

// historyPrefix starts the key of every change.
const historyPrefix = "h:"

func historyKey(n int64) string { return historyPrefix + state.Seq(n) }

// Store holds every tenant's policies and the history of their changes in a
// state.Store, read and written per request.
type Store struct {
	st state.Store
}

// stored is a tenant as it is written.
type stored struct {
	Policies []Policy  `json:"policies"`
	Versions []version `json:"versions"`
	// History is the history as a release before h: keys kept it, inside
	// this record. The first change after moves it to h: keys.
	History []Change `json:"history,omitempty"`
	// Head is the number of the last change kept under h:.
	Head int64 `json:"head,omitempty"`
}

type version struct {
	Scope   Scope `json:"scope"`
	Version int   `json:"version"`
}

// NewStore returns an empty store in this process's memory.
func NewStore() *Store { return &Store{st: state.NewMemory()} }

// Open returns a store on st, which may be shared with other processes. A nil
// st keeps policies in this process's memory.
func Open(st state.Store) (*Store, error) {
	if st == nil {
		st = state.NewMemory()
	}
	return &Store{st: st}, nil
}

func ctx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), state.Timeout)
}

// load reads a tenant's record and its version, zero when it has none.
func (s *Store) load(tenant string) (stored, int64, error) {
	c, cancel := ctx()
	defer cancel()
	r, err := s.st.Lookup(c, namespace, tenant, tenantKey)
	if errors.Is(err, state.ErrNotFound) {
		return stored{}, 0, nil
	}
	if err != nil {
		return stored{}, 0, fmt.Errorf("policy: read: %w", err)
	}
	var out stored
	if err := json.Unmarshal(r.Value, &out); err != nil {
		return stored{}, 0, fmt.Errorf("policy: %q: %w", tenant, err)
	}
	return out, r.Version, nil
}

func (r *stored) bump(scope Scope) int {
	for i, v := range r.Versions {
		if v.Scope == scope {
			r.Versions[i].Version++
			return r.Versions[i].Version
		}
	}
	r.Versions = append(r.Versions, version{Scope: scope, Version: 1})
	slices.SortFunc(r.Versions, func(a, b version) int {
		if c := strings.Compare(string(a.Scope.Kind), string(b.Scope.Kind)); c != 0 {
			return c
		}
		return strings.Compare(a.Scope.Name, b.Scope.Name)
	})
	return 1
}

// change reads a tenant, lets fn change its record and name the change made,
// and writes both at the version read, reading again when another writer
// moved it in between. A record still holding its history inside is moved
// to h: keys in the same write.
func (s *Store) change(tenant string, fn func(r *stored) (Change, error)) error {
	return state.Retry(func() error {
		r, at, err := s.load(tenant)
		if err != nil {
			return err
		}
		r.Policies = slices.Clone(r.Policies)
		r.Versions = slices.Clone(r.Versions)
		c, err := fn(&r)
		if err != nil {
			return err
		}
		var ops []state.Op
		put := func(key string, v any, version int64) error {
			raw, err := json.Marshal(v)
			if err != nil {
				return err
			}
			op := state.Put(tenant, key, raw)
			op.If = version
			ops = append(ops, op)
			return nil
		}
		for _, old := range r.History {
			r.Head++
			if err := put(historyKey(r.Head), old, 0); err != nil {
				return err
			}
		}
		r.History = nil
		r.Head++
		if err := put(historyKey(r.Head), c, 0); err != nil {
			return err
		}
		if gone := r.Head - maxHistory; gone > 0 {
			ops = append(ops, state.Delete(tenant, historyKey(gone)))
		}
		if at == 0 {
			at = state.Missing
		}
		if err := put(tenantKey, r, at); err != nil {
			return err
		}
		cx, cancel := ctx()
		defer cancel()
		if err := s.st.Write(cx, namespace, ops...); err != nil {
			if errors.Is(err, state.ErrConflict) {
				return err
			}
			return fmt.Errorf("policy: write: %w", err)
		}
		return nil
	})
}

// Put sets a scope's policy and records the change. A narrower policy must fit
// within the tenant's, and tightening the tenant's must not leave a narrower
// one claiming more than it.
func (s *Store) Put(tenantName string, p Policy, by string, at time.Time) (Policy, error) {
	return s.putIf(tenantName, p, by, at, nil)
}

// putIf is Put, with check called on the tenant's policies as they stand when
// read, before anything is written.
func (s *Store) putIf(tenantName string, p Policy, by string, at time.Time, check func(all []Policy) error) (Policy, error) {
	if err := p.Validate(); err != nil {
		return Policy{}, err
	}
	in := p
	var out Policy
	err := s.change(tenantName, func(r *stored) (Change, error) {
		p := in
		if check != nil {
			if err := check(r.Policies); err != nil {
				return Change{}, err
			}
		}
		for _, other := range r.Policies {
			switch {
			case p.Scope.Kind != Tenant && other.Scope.Kind == Tenant:
				if err := p.Within(other); err != nil {
					return Change{}, err
				}
			case p.Scope.Kind == Tenant && other.Scope.Kind != Tenant:
				if err := other.Within(p); err != nil {
					return Change{}, fmt.Errorf("%w. Lower that one first", err)
				}
			}
		}
		p.Version, p.UpdatedBy, p.UpdatedAt = r.bump(p.Scope), by, at
		change := Change{Scope: p.Scope, By: by, At: at, After: &p}
		if i := slices.IndexFunc(r.Policies, func(x Policy) bool { return x.Scope == p.Scope }); i >= 0 {
			before := r.Policies[i]
			change.Before = &before
			r.Policies[i] = p
		} else {
			r.Policies = append(r.Policies, p)
		}
		out = p
		return change, nil
	})
	if err != nil {
		return Policy{}, err
	}
	return out, nil
}

// PutOwned sets a tighten-only limit a project's owner chose, on a project
// scope: Put, with p.Authority set to AuthorityOwner. On the policies as read
// for the write, it is refused with ErrAdmin where an admin's policy is set,
// and with ErrInvalid, naming the scope that binds, when it is looser than
// any policy over it: those of above (the project's ancestors nearest first,
// the root left out) and the tenant's.
func (s *Store) PutOwned(tenantName string, p Policy, above []string, by string, at time.Time) (Policy, error) {
	p.Authority = AuthorityOwner
	return s.putIf(tenantName, p, by, at, func(all []Policy) error {
		if i := slices.IndexFunc(all, func(x Policy) bool { return x.Scope == p.Scope }); i >= 0 && !all[i].ByOwner() {
			return fmt.Errorf("%w: the limits of %s are an admin's, and change with billing:write", ErrAdmin, p.Scope)
		}
		var over []Policy
		for _, name := range above {
			if i := slices.IndexFunc(all, func(x Policy) bool { return x.Scope == Scope{Project, name} }); i >= 0 {
				over = append(over, all[i])
			}
		}
		return Tighter(p, over)
	})
}

// DeleteOwned removes a limit a project's owner set. One an admin set is
// ErrAdmin, and a scope with none is ErrNotFound.
func (s *Store) DeleteOwned(tenantName string, scope Scope, by string, at time.Time) error {
	return s.remove(tenantName, scope, by, at, true)
}

// Delete removes a scope's policy and records the change.
func (s *Store) Delete(tenantName string, scope Scope, by string, at time.Time) error {
	return s.remove(tenantName, scope, by, at, false)
}

func (s *Store) remove(tenantName string, scope Scope, by string, at time.Time, owned bool) error {
	return s.change(tenantName, func(r *stored) (Change, error) {
		i := slices.IndexFunc(r.Policies, func(x Policy) bool { return x.Scope == scope })
		if i < 0 {
			return Change{}, fmt.Errorf("%w: no policy for %s", ErrNotFound, scope)
		}
		if owned && !r.Policies[i].ByOwner() {
			return Change{}, fmt.Errorf("%w: the limits of %s are an admin's, and change with billing:write", ErrAdmin, scope)
		}
		before := r.Policies[i]
		r.Policies = slices.Delete(r.Policies, i, i+1)
		return Change{Scope: scope, By: by, At: at, Before: &before}, nil
	})
}

// List returns a tenant's policies: the tenant's first, then by kind and name.
func (s *Store) List(tenantName string) ([]Policy, error) {
	r, _, err := s.load(tenantName)
	if err != nil {
		return nil, err
	}
	out := r.Policies
	if out == nil {
		out = []Policy{}
	}
	order := map[Kind]int{Tenant: 0, Project: 1, Agent: 2, Client: 3}
	slices.SortFunc(out, func(a, b Policy) int {
		if d := order[a.Scope.Kind] - order[b.Scope.Kind]; d != 0 {
			return d
		}
		return strings.Compare(a.Scope.Name, b.Scope.Name)
	})
	return out, nil
}

// For returns the policies a run falls under, most specific first: its
// agent's, the client's that raised it, then its project's and each of the
// project's ancestors' nearest first, then the tenant's. chain is the project
// and its ancestors, nearest first, the root's "" left out or skipped. An empty
// agent or client means the run has no such scope. Decide applies them all,
// so the strictest wins, and a project's limit holds everything below it.
func (s *Store) For(tenantName string, chain []string, agent, client string) ([]Policy, error) {
	r, _, err := s.load(tenantName)
	if err != nil {
		return nil, err
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
		if i := slices.IndexFunc(r.Policies, func(x Policy) bool { return x.Scope == w }); i >= 0 {
			out = append(out, r.Policies[i])
		}
	}
	return out, nil
}

// page reads up to limit changes, newest first, numbered below before (0 for
// the newest), and the number to read the next page below, 0 when none
// remain. A record still holding its history inside answers from it.
func (s *Store) page(tenant string, r stored, before int64, limit int) ([]Change, int64, error) {
	if len(r.History) > 0 {
		n := int64(len(r.History))
		if before == 0 || before > n+1 {
			before = n + 1
		}
		var out []Change
		for i := before - 1; i >= 1 && len(out) < limit; i-- {
			out = append(out, r.History[i-1])
		}
		next := before - int64(len(out))
		if next <= 1 {
			next = 0
		}
		return out, next, nil
	}
	q := state.Query{Prefix: historyPrefix, Limit: limit, Desc: true}
	if before > 0 {
		q.After = historyKey(before)
	}
	c, cancel := ctx()
	defer cancel()
	recs, more, err := s.st.List(c, namespace, tenant, q)
	if err != nil {
		return nil, 0, fmt.Errorf("policy: read history: %w", err)
	}
	out := make([]Change, 0, len(recs))
	var next int64
	for _, rec := range recs {
		var ch Change
		if err := json.Unmarshal(rec.Value, &ch); err != nil {
			return nil, 0, fmt.Errorf("policy: %q/%q: %w", tenant, rec.Key, err)
		}
		out = append(out, ch)
		if _, err := fmt.Sscanf(strings.TrimPrefix(rec.Key, historyPrefix), "%d", &next); err != nil {
			return nil, 0, fmt.Errorf("policy: %q/%q: %w", tenant, rec.Key, err)
		}
	}
	if more == "" {
		next = 0
	}
	return out, next, nil
}

// History returns a tenant's policy changes, newest first: at most
// maxHistory, which is all a tenant keeps.
func (s *Store) History(tenantName string) ([]Change, error) {
	r, _, err := s.load(tenantName)
	if err != nil {
		return nil, err
	}
	out, _, err := s.page(tenantName, r, 0, maxHistory)
	if err != nil {
		return nil, err
	}
	if out == nil {
		out = []Change{}
	}
	return out, nil
}

// At returns the policy a scope had at a moment, so "what was the limit when
// this run was refused" has an answer for as long as the history reaches.
// It reads the history newest first and stops at the latest change to the
// scope made at or before when.
func (s *Store) At(tenantName string, scope Scope, when time.Time) (Policy, bool, error) {
	r, _, err := s.load(tenantName)
	if err != nil {
		return Policy{}, false, err
	}
	var before int64
	for {
		page, next, err := s.page(tenantName, r, before, atPage)
		if err != nil {
			return Policy{}, false, err
		}
		for _, c := range page {
			if c.Scope != scope || c.At.After(when) {
				continue
			}
			if c.After == nil {
				return Policy{}, false, nil
			}
			return *c.After, true, nil
		}
		if next == 0 {
			return Policy{}, false, nil
		}
		before = next
	}
}
