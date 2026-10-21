package people

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/urmzd/warp/pkg/state"
)

// namespace is where every tenant's people are kept, one record each, by
// subject.
const namespace = "people"

// tenant is one workspace's people.
type tenant struct {
	by      map[string]Person // by subject
	handles map[string]string // handle to subject
}

func newTenant() *tenant {
	return &tenant{by: map[string]Person{}, handles: map[string]string{}}
}

// Store keeps every workspace's directory. It is in memory, and built with
// Open it writes every change through a state.Store and reads them back when
// it is built again. It is one process's: it reads the store once and then
// trusts its memory.
type Store struct {
	mu      sync.RWMutex
	tenants map[string]*tenant
	kept    *state.Table[Person]
	taken   func(tenant, handle string) bool
}

// Option configures a Store.
type Option func(*Store)

// WithTaken counts more handles as taken in a workspace, such as its agents'
// names, so no person is given one. fn runs under the store's lock and must
// not call the store.
func WithTaken(fn func(tenant, handle string) bool) Option {
	return func(s *Store) { s.taken = fn }
}

// NewStore returns an empty directory in memory.
func NewStore(opts ...Option) *Store {
	s := &Store{tenants: map[string]*tenant{}}
	for _, o := range opts {
		o(s)
	}
	return s
}

// Open returns a directory that keeps its records in st, holding what st
// already has. A nil st is NewStore. It refuses a record that is not a
// person, and two people with one handle.
func Open(st state.Store, opts ...Option) (*Store, error) {
	s := NewStore(opts...)
	s.kept = state.NewTable[Person](st, namespace)
	err := s.kept.Load(func(tenantName, key string, p Person) error {
		t := s.tenant(tenantName)
		switch {
		case p.Subject != key || !validSubject(p.Subject):
			return fmt.Errorf("%w: a person is kept under %q", ErrInvalid, key)
		case !ValidHandle(p.Handle):
			return fmt.Errorf("%w: %q is not a handle", ErrInvalid, p.Handle)
		}
		if other, dup := t.handles[p.Handle]; dup {
			return fmt.Errorf("%w: %q and %q both go by %s", ErrInvalid, other, p.Subject, p.Handle)
		}
		if p.Roles == nil {
			p.Roles = []string{}
		}
		t.by[p.Subject] = p
		t.handles[p.Handle] = p.Subject
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("people: load: %w", err)
	}
	return s, nil
}

// tenant returns a workspace's people, adding it. The caller holds the
// write lock, or is Open.
func (s *Store) tenant(name string) *tenant {
	t, ok := s.tenants[name]
	if !ok {
		t = newTenant()
		s.tenants[name] = t
	}
	return t
}

// isTaken reports whether a handle is unavailable to the person self.
func (s *Store) isTaken(t *tenant, tenantName, handle, self string) bool {
	if IsReserved(handle) {
		return true
	}
	if who, ok := t.handles[handle]; ok && who != self {
		return true
	}
	return s.taken != nil && s.taken(tenantName, handle)
}

// handleFor is the first free handle a name or subject makes.
func (s *Store) handleFor(t *tenant, tenantName, name, subject string) string {
	base := baseHandle(name, subject)
	for n := 1; ; n++ {
		if h := candidate(base, n); !s.isTaken(t, tenantName, h, subject) {
			return h
		}
	}
}

// Seen records a sighting: a new person, or what is new about one already
// recorded. A sign-in also marks when they were last seen. A sighting that
// changes nothing writes nothing while the last sign-in is under Quiet old,
// so a busy session does not write on every request. A new person past
// MaxPeople is ErrLimit.
func (s *Store) Seen(tenantName string, at Sighting, when time.Time) (Person, error) {
	if !validSubject(at.Subject) {
		return Person{}, fmt.Errorf("%w: a subject is one line of at most %d characters", ErrInvalid, MaxSubject)
	}
	if !validSource(at.Source) {
		return Person{}, fmt.Errorf("%w: a person is seen at sign-in, through a client's people or in the policy", ErrInvalid)
	}
	name := nameOf(at.Name)
	roles, err := rolesOf(at.Roles)
	if err != nil {
		return Person{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.tenant(tenantName)
	cur, known := t.by[at.Subject]
	if !known {
		if len(t.by) >= MaxPeople {
			return Person{}, fmt.Errorf("%w: a workspace lists at most %d people", ErrLimit, MaxPeople)
		}
		p := Person{
			Subject: at.Subject, Name: name, Handle: s.handleFor(t, tenantName, name, at.Subject),
			Roles: roles, Sources: []string{at.Source}, CreatedAt: when,
		}
		if p.Roles == nil {
			p.Roles = []string{}
		}
		if at.Source == SignIn {
			p.SeenAt = when
		}
		return p.clone(), s.put(tenantName, t, Person{}, p)
	}
	next := cur.clone()
	if name != "" && name != cur.Name {
		// A handle made from the subject, before any name was known, is made
		// again from the first name.
		if cur.Name == "" {
			next.Handle = s.handleFor(t, tenantName, name, at.Subject)
		}
		next.Name = name
	}
	if roles != nil && (at.Source == SignIn || (at.Source == Policy && !slices.Contains(cur.Sources, SignIn))) {
		next.Roles = roles
	}
	if !slices.Contains(next.Sources, at.Source) {
		next.Sources = append(next.Sources, at.Source)
		slices.Sort(next.Sources)
	}
	changed := next.Name != cur.Name || next.Handle != cur.Handle || !slices.Equal(next.Roles, cur.Roles) || !slices.Equal(next.Sources, cur.Sources)
	if at.Source == SignIn {
		if !changed && when.Sub(cur.SeenAt) < Quiet {
			return cur.clone(), nil
		}
		next.SeenAt = when
	} else if !changed {
		return cur.clone(), nil
	}
	return next.clone(), s.put(tenantName, t, cur, next)
}

// Ensure records that a person exists, seen at source, with nothing else
// known: a client mapping a Slack person, or the policy granting a subject a
// role. It changes nothing for someone already recorded there.
func (s *Store) Ensure(tenantName, subject, source string, when time.Time) (Person, error) {
	return s.Seen(tenantName, Sighting{Subject: subject, Source: source}, when)
}

// put writes a person, and only once it is written makes them the
// workspace's. old is who they were, zero for someone new.
func (s *Store) put(tenantName string, t *tenant, old, p Person) error {
	if err := s.kept.Put(tenantName, p.Subject, p); err != nil {
		return fmt.Errorf("people: write: %w", err)
	}
	if old.Handle != "" && old.Handle != p.Handle {
		delete(t.handles, old.Handle)
	}
	t.by[p.Subject] = p
	t.handles[p.Handle] = p.Subject
	return nil
}

// Get returns a person by subject.
func (s *Store) Get(tenantName, subject string) (Person, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if t, ok := s.tenants[tenantName]; ok {
		if p, ok := t.by[subject]; ok {
			return p.clone(), nil
		}
	}
	return Person{}, fmt.Errorf("%w: %q", ErrNotFound, subject)
}

// ByHandle returns the person who goes by a handle, ignoring case.
func (s *Store) ByHandle(tenantName, handle string) (Person, error) {
	handle = strings.ToLower(handle)
	s.mu.RLock()
	defer s.mu.RUnlock()
	if t, ok := s.tenants[tenantName]; ok {
		if who, ok := t.handles[handle]; ok {
			return t.by[who].clone(), nil
		}
	}
	return Person{}, fmt.Errorf("%w: nobody goes by @%s", ErrNotFound, handle)
}

// Query is one page of the directory.
type Query struct {
	// Q keeps people whose name, handle or subject holds it, ignoring case.
	Q string
	// Limit is the most to return: at most MaxPage, which is also the page
	// when it is zero.
	Limit int
	// Before is where the page starts: the next a previous page returned.
	Before string
}

// List returns a page of a workspace's people, by handle, and the cursor of
// the page after it, "" when there is none. A cursor that is not one is
// ErrInvalid.
func (s *Store) List(tenantName string, q Query) ([]Person, string, error) {
	if q.Before != "" && !ValidHandle(q.Before) {
		return nil, "", fmt.Errorf("%w: that is not a cursor this list gave", ErrInvalid)
	}
	if len(q.Q) > MaxQuery {
		return nil, "", fmt.Errorf("%w: a search is at most %d characters", ErrInvalid, MaxQuery)
	}
	limit := q.Limit
	if limit <= 0 || limit > MaxPage {
		limit = MaxPage
	}
	needle := strings.ToLower(strings.TrimSpace(q.Q))
	s.mu.RLock()
	defer s.mu.RUnlock()
	page := []Person{}
	t, ok := s.tenants[tenantName]
	if !ok {
		return page, "", nil
	}
	handles := make([]string, 0, len(t.handles))
	for h := range t.handles {
		if h > q.Before {
			handles = append(handles, h)
		}
	}
	sort.Strings(handles)
	for _, h := range handles {
		p := t.by[t.handles[h]]
		if needle != "" && !strings.Contains(strings.ToLower(p.Name), needle) && !strings.Contains(p.Handle, needle) &&
			!strings.Contains(strings.ToLower(p.Subject), needle) {
			continue
		}
		if len(page) == limit {
			return page, page[len(page)-1].Handle, nil
		}
		page = append(page, p.clone())
	}
	return page, "", nil
}

// Len is how many people a workspace has recorded.
func (s *Store) Len(tenantName string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if t, ok := s.tenants[tenantName]; ok {
		return len(t.by)
	}
	return 0
}
