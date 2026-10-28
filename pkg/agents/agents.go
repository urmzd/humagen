// Package agents keeps each tenant's agent definitions on a state.Store. It
// is a definition.Source: saige definitions with their fiber policy, insert
// only, and held to the definitiontest suite like fiber's own sources.
//
// Like every store on pkg/state, it keeps its read model in memory, loads
// it at Open, and writes each change through before it reports success.
package agents

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/urmzd/humagen/fiber/pkg/definition"
	"github.com/urmzd/humagen/pkg/state"
)

// Namespaces in the state store.
const (
	// VersionsNamespace holds one record per version, keyed "<name>@<version>".
	VersionsNamespace = "agents.version"
	// RetiredNamespace holds one record per retired name, keyed by the name.
	RetiredNamespace = "agents.retired"
)

// Option configures a Store.
type Option func(*Store)

// WithChecks sets the deployment facts Put validates against.
func WithChecks(c definition.Checks) Option { return func(s *Store) { s.checks = c } }

// WithClock sets the time source CreatedAt is read from.
func WithClock(now func() time.Time) Option {
	return func(s *Store) {
		if now != nil {
			s.clock = now
		}
	}
}

// Retirement is who retired a name, and when.
type Retirement struct {
	By string    `json:"by"`
	At time.Time `json:"at"`
}

type entry struct {
	versions []definition.Record // lowest first
	retired  *Retirement
}

// Store is a definition.Source on a state.Store.
type Store struct {
	mu       sync.RWMutex
	checks   definition.Checks
	clock    func() time.Time
	versions *state.Table[definition.Record]
	retired  *state.Table[Retirement]
	tenants  map[string]map[string]*entry
}

var _ definition.Source = (*Store)(nil)

// New returns a store held only in memory.
func New(opts ...Option) *Store {
	s := &Store{clock: time.Now, tenants: map[string]map[string]*entry{}}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Open returns a store that writes through st and holds what st already
// keeps. A nil st is New.
func Open(st state.Store, opts ...Option) (*Store, error) {
	s := New(opts...)
	s.versions = state.NewTable[definition.Record](st, VersionsNamespace)
	s.retired = state.NewTable[Retirement](st, RetiredNamespace)
	err := s.versions.Load(func(tenant, key string, rec definition.Record) error {
		if rec.Tenant != tenant || key != versionKey(rec.Name, rec.Version) {
			return fmt.Errorf("agents: %q/%q holds %s@%s of tenant %q", tenant, key, rec.Name, rec.Version, rec.Tenant)
		}
		e := s.entry(tenant, rec.Name, true)
		e.versions = append(e.versions, rec)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("agents: load: %w", err)
	}
	err = s.retired.Load(func(tenant, name string, r Retirement) error {
		s.entry(tenant, name, true).retired = &r
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("agents: load retirements: %w", err)
	}
	for _, names := range s.tenants {
		for _, e := range names {
			definition.SortVersions(e.versions)
		}
	}
	return s, nil
}

func versionKey(name, version string) string { return name + "@" + version }

func (s *Store) entry(tenant, name string, create bool) *entry {
	names := s.tenants[tenant]
	if names == nil {
		if !create {
			return nil
		}
		names = map[string]*entry{}
		s.tenants[tenant] = names
	}
	e := names[name]
	if e == nil && create {
		e = &entry{}
		names[name] = e
	}
	return e
}

// Put implements definition.Source.
func (s *Store) Put(_ context.Context, rec definition.Record) (definition.Record, error) {
	rec, err := definition.Prepare(rec, s.checks, s.clock())
	if err != nil {
		return definition.Record{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out, err := s.insert([]definition.Record{rec}, false)
	if err != nil {
		return definition.Record{}, err
	}
	return out[0], nil
}

// Import inserts the versions of one agent all or none, keeping each
// record's CreatedAt and CreatedBy. It is how definitions written before this
// store are moved into it. A version already held with the same content is
// kept as it is, so an import that is run again changes nothing.
func (s *Store) Import(_ context.Context, recs []definition.Record) ([]definition.Record, error) {
	prepared := make([]definition.Record, 0, len(recs))
	for _, rec := range recs {
		if rec.CreatedAt.IsZero() {
			return nil, fmt.Errorf("%w: an imported definition keeps the time it was written", definition.ErrInvalid)
		}
		p, err := definition.Prepare(rec, s.checks, rec.CreatedAt)
		if err != nil {
			return nil, err
		}
		prepared = append(prepared, p)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.insert(prepared, true)
}

// insert writes prepared records in one batch. s.mu is held.
func (s *Store) insert(recs []definition.Record, importing bool) ([]definition.Record, error) {
	out := make([]definition.Record, len(recs))
	var fresh []definition.Record
	b := s.versions.Batch()
next:
	for i, rec := range recs {
		e := s.entry(rec.Tenant, rec.Name, false)
		var held []definition.Record
		if e != nil {
			held = e.versions
		}
		for _, have := range append(slices.Clone(held), fresh...) {
			if have.Tenant != rec.Tenant || have.Name != rec.Name || have.Version != rec.Version {
				continue
			}
			if definition.Same(have, rec) {
				out[i] = clone(have)
				continue next
			}
			return nil, fmt.Errorf("%w: %s@%s holds other content; a version is never rewritten", definition.ErrExists, rec.Name, rec.Version)
		}
		if e != nil && e.retired != nil && !importing {
			return nil, fmt.Errorf("%w: %s", definition.ErrRetired, rec.Name)
		}
		b.Put(rec.Tenant, versionKey(rec.Name, rec.Version), rec)
		fresh = append(fresh, rec)
		out[i] = clone(rec)
	}
	if err := b.Commit(); err != nil {
		return nil, fmt.Errorf("agents: write: %w", err)
	}
	for _, rec := range fresh {
		e := s.entry(rec.Tenant, rec.Name, true)
		e.versions = append(e.versions, clone(rec))
		definition.SortVersions(e.versions)
	}
	return out, nil
}

// Get implements definition.Source.
func (s *Store) Get(_ context.Context, tenant, name, version string) (definition.Record, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if e := s.entry(tenant, name, false); e != nil {
		for _, r := range e.versions {
			if r.Version == version {
				return clone(r), nil
			}
		}
	}
	return definition.Record{}, fmt.Errorf("%w: %s@%s", definition.ErrNotFound, name, version)
}

// Versions implements definition.Source.
func (s *Store) Versions(_ context.Context, tenant, name string) ([]definition.Record, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e := s.entry(tenant, name, false)
	if e == nil || len(e.versions) == 0 {
		return nil, fmt.Errorf("%w: %s", definition.ErrNotFound, name)
	}
	out := make([]definition.Record, len(e.versions))
	for i, r := range e.versions {
		out[i] = clone(r)
	}
	return out, nil
}

// Latest implements definition.Source.
func (s *Store) Latest(_ context.Context, tenant, name string) (definition.Record, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e := s.entry(tenant, name, false)
	switch {
	case e == nil || len(e.versions) == 0:
		return definition.Record{}, fmt.Errorf("%w: %s", definition.ErrNotFound, name)
	case e.retired != nil:
		return definition.Record{}, fmt.Errorf("%w: %s", definition.ErrRetired, name)
	}
	return clone(e.versions[len(e.versions)-1]), nil
}

// List implements definition.Source.
func (s *Store) List(_ context.Context, tenant string, page definition.Page) ([]definition.Record, string, error) {
	after, err := definition.PageAfter(page.Token, tenant)
	if err != nil {
		return nil, "", err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	names := make([]string, 0, len(s.tenants[tenant]))
	for name, e := range s.tenants[tenant] {
		if e.retired == nil && len(e.versions) > 0 && name > after {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	size := page.Limit()
	next := ""
	if len(names) > size {
		names = names[:size]
		next = definition.PageToken(tenant, names[size-1])
	}
	out := make([]definition.Record, 0, len(names))
	for _, name := range names {
		e := s.tenants[tenant][name]
		out = append(out, clone(e.versions[len(e.versions)-1]))
	}
	return out, next, nil
}

// Retire implements definition.Source.
func (s *Store) Retire(_ context.Context, tenant, name, by string) error {
	by = strings.TrimSpace(by)
	if by == "" {
		return fmt.Errorf("%w: a retirement must say who made it", definition.ErrInvalid)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.entry(tenant, name, false)
	if e == nil || len(e.versions) == 0 {
		return fmt.Errorf("%w: %s", definition.ErrNotFound, name)
	}
	if e.retired != nil {
		return nil
	}
	r := Retirement{By: by, At: s.clock().UTC()}
	if err := s.retired.Put(tenant, name, r); err != nil {
		return fmt.Errorf("agents: write retirement: %w", err)
	}
	e.retired = &r
	return nil
}

// Revive lifts a name's retirement, so new versions may be written under it
// again. Every version written before stays as it was: a revived agent
// continues its versions rather than starting over.
func (s *Store) Revive(_ context.Context, tenant, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.entry(tenant, name, false)
	if e == nil || e.retired == nil {
		return nil
	}
	if err := s.retired.Delete(tenant, name); err != nil {
		return fmt.Errorf("agents: lift retirement: %w", err)
	}
	e.retired = nil
	return nil
}

// Retired reports who retired a name, and whether it is retired.
func (s *Store) Retired(tenant, name string) (Retirement, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if e := s.entry(tenant, name, false); e != nil && e.retired != nil {
		return *e.retired, true
	}
	return Retirement{}, false
}

func clone(r definition.Record) definition.Record {
	r.Policy = r.Policy.Normalize()
	return r
}
