package people

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

// namespace is where every tenant's people are kept. Within a tenant:
//
//	<subject>      a person, by subject
//	\x1fh:<handle>  the same person, by handle: what ByHandle reads and
//	               List pages, and what makes a handle one person's
//	\x1fcount       how many people the tenant has recorded
//
// A subject holds no control character, so the index and the count, which
// start with one (the unit separator), are never a person's key. Every
// change to a person writes their record and their handle's together, at
// the versions read, and a new person moves the count too.
const namespace = "people"

const (
	index = "\x1f"
	byKey = index + "h:"
	count = index + "count"
	// indexed marks, in the tenant "", that every tenant's people have
	// their handles and their count, which a release before them did not
	// keep.
	indexed = "indexed:v1"
)

func handleKey(handle string) string { return byKey + handle }

// Store keeps every workspace's directory.
type Store struct {
	st    state.Store
	taken func(tenant, handle string) bool
}

// Option configures a Store.
type Option func(*Store)

// WithTaken counts more handles as taken in a workspace, such as its agents'
// names, so no person is given one. fn must not call the store.
func WithTaken(fn func(tenant, handle string) bool) Option {
	return func(s *Store) { s.taken = fn }
}

// NewStore returns an empty directory in this process's memory.
func NewStore(opts ...Option) *Store {
	s, _ := Open(nil, opts...)
	return s
}

// Open returns a directory on st, which may be shared with other processes.
// A nil st keeps it in this process's memory.
//
// People kept by a release before handles were indexed are indexed the
// first time any process opens the directory; a process that opens it at
// the same time finds them indexed. Indexing refuses a record that is not a
// person, and two people with one handle.
func Open(st state.Store, opts ...Option) (*Store, error) {
	if st == nil {
		st = state.NewMemory()
	}
	s := &Store{st: st}
	for _, o := range opts {
		o(s)
	}
	if err := s.index(); err != nil {
		return nil, fmt.Errorf("people: load: %w", err)
	}
	return s, nil
}

func ctx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), state.Timeout)
}

// checked is a stored person, refused unless it is one, kept under key.
func checked(key string, raw []byte) (Person, error) {
	var p Person
	if err := json.Unmarshal(raw, &p); err != nil {
		return Person{}, fmt.Errorf("%w: %q: %w", ErrInvalid, key, err)
	}
	switch {
	case p.Subject != key || !validSubject(p.Subject):
		return Person{}, fmt.Errorf("%w: a person is kept under %q", ErrInvalid, key)
	case !ValidHandle(p.Handle):
		return Person{}, fmt.Errorf("%w: %q is not a handle", ErrInvalid, p.Handle)
	}
	if p.Roles == nil {
		p.Roles = []string{}
	}
	return p, nil
}

// index writes each tenant's handles and count, from its people, once. A
// tenant is indexed in one write that creates every record, so a process
// that indexes it at the same time, or after, conflicts and leaves it as it
// is: a tenant written by a process that has opened the directory is
// indexed already.
func (s *Store) index() error {
	c, cancel := ctx()
	defer cancel()
	if _, err := s.st.Lookup(c, namespace, "", indexed); err == nil {
		return nil
	} else if !errors.Is(err, state.ErrNotFound) {
		return err
	}
	recs, err := s.st.Load(c, namespace)
	if err != nil {
		return err
	}
	type pending struct {
		ops     []state.Op
		handles map[string]string
	}
	byTenant := map[string]*pending{}
	var order []string
	for _, r := range recs {
		if r.Tenant == "" || strings.HasPrefix(r.Key, index) {
			continue
		}
		p, err := checked(r.Key, r.Value)
		if err != nil {
			return err
		}
		t := byTenant[r.Tenant]
		if t == nil {
			t = &pending{handles: map[string]string{}}
			byTenant[r.Tenant] = t
			order = append(order, r.Tenant)
		}
		if other, dup := t.handles[p.Handle]; dup {
			return fmt.Errorf("%w: %q and %q both go by %s", ErrInvalid, other, p.Subject, p.Handle)
		}
		t.handles[p.Handle] = p.Subject
		t.ops = append(t.ops, state.Create(r.Tenant, handleKey(p.Handle), r.Value))
	}
	for _, tenant := range order {
		t := byTenant[tenant]
		raw, _ := json.Marshal(len(t.handles))
		ops := append(t.ops, state.Create(tenant, count, raw))
		if err := s.st.Write(c, namespace, ops...); err != nil && !errors.Is(err, state.ErrConflict) {
			return err
		}
	}
	return s.st.Write(c, namespace, state.Put("", indexed, []byte("true")))
}

// lookup reads one record into v, and its version: 0 when there is none.
func (s *Store) lookup(c context.Context, tenant, key string, v any) (int64, error) {
	r, err := s.st.Lookup(c, namespace, tenant, key)
	if errors.Is(err, state.ErrNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if err := json.Unmarshal(r.Value, v); err != nil {
		return 0, fmt.Errorf("people: %q: %w", key, err)
	}
	return r.Version, nil
}

// put is an op that writes v under key at version: created when version is
// 0.
func put(tenant, key string, v any, version int64) state.Op {
	raw, _ := json.Marshal(v)
	if version == 0 {
		return state.Create(tenant, key, raw)
	}
	return state.PutAt(tenant, key, raw, version)
}

// isTaken reports whether a handle is unavailable to the person self.
func (s *Store) isTaken(c context.Context, tenantName, handle, self string) (bool, error) {
	if IsReserved(handle) {
		return true, nil
	}
	var who Person
	version, err := s.lookup(c, tenantName, handleKey(handle), &who)
	if err != nil {
		return false, err
	}
	if version != 0 && who.Subject != self {
		return true, nil
	}
	return s.taken != nil && s.taken(tenantName, handle), nil
}

// handleFor is the first free handle a name or subject makes. Two processes
// may both find one free; the write that claims it decides.
func (s *Store) handleFor(c context.Context, tenantName, name, subject string) (string, error) {
	base := baseHandle(name, subject)
	for n := 1; ; n++ {
		h := candidate(base, n)
		taken, err := s.isTaken(c, tenantName, h, subject)
		if err != nil {
			return "", err
		}
		if !taken {
			return h, nil
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
	var out Person
	err = state.Retry(func() error {
		c, cancel := ctx()
		defer cancel()
		var cur Person
		version, err := s.lookup(c, tenantName, at.Subject, &cur)
		if err != nil {
			return err
		}
		if version == 0 {
			p, err := s.add(c, tenantName, at, name, roles, when)
			out = p
			return err
		}
		next := cur.clone()
		if next.Roles == nil {
			next.Roles = []string{}
		}
		if name != "" && name != cur.Name {
			// A handle made from the subject, before any name was known, is
			// made again from the first name.
			if cur.Name == "" {
				if next.Handle, err = s.handleFor(c, tenantName, name, at.Subject); err != nil {
					return err
				}
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
				out = cur.clone()
				return nil
			}
			next.SeenAt = when
		} else if !changed {
			out = cur.clone()
			return nil
		}
		var byHandle Person
		hv, err := s.lookup(c, tenantName, handleKey(cur.Handle), &byHandle)
		if err != nil {
			return err
		}
		ops := []state.Op{put(tenantName, at.Subject, next, version)}
		switch {
		case next.Handle != cur.Handle:
			if hv != 0 {
				ops = append(ops, state.DeleteAt(tenantName, handleKey(cur.Handle), hv))
			}
			ops = append(ops, put(tenantName, handleKey(next.Handle), next, 0))
		default:
			ops = append(ops, put(tenantName, handleKey(next.Handle), next, hv))
		}
		if err := s.st.Write(c, namespace, ops...); err != nil {
			return writeErr(err)
		}
		out = next.clone()
		return nil
	})
	return out, err
}

// writeErr keeps a conflict a conflict, so Retry reads again, and says what
// any other failure was.
func writeErr(err error) error {
	if errors.Is(err, state.ErrConflict) {
		return err
	}
	return fmt.Errorf("people: write: %w", err)
}

// add records someone new: their record, their handle and the tenant's
// count, in one write.
func (s *Store) add(c context.Context, tenantName string, at Sighting, name string, roles []string, when time.Time) (Person, error) {
	var n int
	cv, err := s.lookup(c, tenantName, count, &n)
	if err != nil {
		return Person{}, err
	}
	if n >= MaxPeople {
		return Person{}, fmt.Errorf("%w: a workspace lists at most %d people", ErrLimit, MaxPeople)
	}
	handle, err := s.handleFor(c, tenantName, name, at.Subject)
	if err != nil {
		return Person{}, err
	}
	p := Person{
		Subject: at.Subject, Name: name, Handle: handle,
		Roles: roles, Sources: []string{at.Source}, CreatedAt: when,
	}
	if p.Roles == nil {
		p.Roles = []string{}
	}
	if at.Source == SignIn {
		p.SeenAt = when
	}
	err = s.st.Write(c, namespace,
		put(tenantName, at.Subject, p, 0),
		put(tenantName, handleKey(handle), p, 0),
		put(tenantName, count, n+1, cv))
	if err != nil {
		return Person{}, writeErr(err)
	}
	return p.clone(), nil
}

// Ensure records that a person exists, seen at source, with nothing else
// known: a client mapping a Slack person, or the policy granting a subject a
// role. It changes nothing for someone already recorded there.
func (s *Store) Ensure(tenantName, subject, source string, when time.Time) (Person, error) {
	return s.Seen(tenantName, Sighting{Subject: subject, Source: source}, when)
}

// Get returns a person by subject.
func (s *Store) Get(tenantName, subject string) (Person, error) {
	if !validSubject(subject) {
		return Person{}, fmt.Errorf("%w: %q", ErrNotFound, subject)
	}
	c, cancel := ctx()
	defer cancel()
	r, err := s.st.Lookup(c, namespace, tenantName, subject)
	if errors.Is(err, state.ErrNotFound) {
		return Person{}, fmt.Errorf("%w: %q", ErrNotFound, subject)
	}
	if err != nil {
		return Person{}, fmt.Errorf("people: read: %w", err)
	}
	return checked(subject, r.Value)
}

// ByHandle returns the person who goes by a handle, ignoring case.
func (s *Store) ByHandle(tenantName, handle string) (Person, error) {
	handle = strings.ToLower(handle)
	if !ValidHandle(handle) {
		return Person{}, fmt.Errorf("%w: nobody goes by @%s", ErrNotFound, handle)
	}
	c, cancel := ctx()
	defer cancel()
	var p Person
	version, err := s.lookup(c, tenantName, handleKey(handle), &p)
	if err != nil {
		return Person{}, fmt.Errorf("people: read: %w", err)
	}
	if version == 0 {
		return Person{}, fmt.Errorf("%w: nobody goes by @%s", ErrNotFound, handle)
	}
	if p.Roles == nil {
		p.Roles = []string{}
	}
	return p, nil
}

// Query is one page of the directory.
type Query struct {
	// Q keeps people whose name or handle holds it, ignoring case. Subjects
	// are not searched: some hold an address.
	Q string
	// Limit is the most to return: at most MaxPage, which is also the page
	// when it is zero.
	Limit int
	// Before is where the page starts: the next a previous page returned.
	Before string
}

// List returns a page of a workspace's people, by handle, and the cursor of
// the page after it, "" when there is none. A cursor that is not one is
// ErrInvalid. A search reads the workspace's handles a page at a time, at
// most MaxPeople of them.
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
	c, cancel := ctx()
	defer cancel()
	page := []Person{}
	after := ""
	if q.Before != "" {
		after = handleKey(q.Before)
	}
	for read := 0; read <= MaxPeople; {
		// One more than the page is read, to know whether another follows.
		n := limit + 1 - len(page)
		if needle != "" {
			n = state.MaxList
		}
		recs, next, err := s.st.List(c, namespace, tenantName, state.Query{Prefix: byKey, After: after, Limit: n})
		if err != nil {
			return nil, "", fmt.Errorf("people: read: %w", err)
		}
		read += len(recs)
		for _, r := range recs {
			var p Person
			if err := json.Unmarshal(r.Value, &p); err != nil {
				return nil, "", fmt.Errorf("people: %q: %w", r.Key, err)
			}
			if needle != "" && !strings.Contains(strings.ToLower(p.Name), needle) && !strings.Contains(p.Handle, needle) {
				continue
			}
			if len(page) == limit {
				return page, page[len(page)-1].Handle, nil
			}
			if p.Roles == nil {
				p.Roles = []string{}
			}
			page = append(page, p)
		}
		if next == "" {
			break
		}
		after = next
	}
	return page, "", nil
}

// Len is how many people a workspace has recorded: 0 when it cannot be read.
func (s *Store) Len(tenantName string) int {
	c, cancel := ctx()
	defer cancel()
	var n int
	if _, err := s.lookup(c, tenantName, count, &n); err != nil {
		return 0
	}
	return n
}
