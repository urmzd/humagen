// Package agents keeps each tenant's agent definitions on a state.Store. It
// is a definition.Source: saige definitions with their fiber policy, insert
// only, and held to the definitiontest suite like fiber's own sources.
//
// It reads and writes the database per request (pkg/state, per request): it
// holds no definition in memory, so any number of processes on one database
// agree, and it does not need pkg/state's single-writer lock.
package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/urmzd/humagen/fiber/pkg/definition"
	"github.com/urmzd/humagen/pkg/state"
)

// Namespaces in the state store.
const (
	// VersionsNamespace holds one record per version, keyed
	// "<name>@<version>", and one head per agent, keyed "h:<name>". A name
	// is a saige name, which holds neither "@" nor ":".
	VersionsNamespace = "agents.version"
	// RetiredNamespace held one record per retired name, keyed by the name,
	// before a retirement was kept on the agent's head. It is read once, to
	// move what it holds onto the heads.
	RetiredNamespace = "agents.retired"
)

// MaxVersions is the most versions one agent may have.
const MaxVersions = 1000

const (
	headPrefix = "h:"
	// converted marks, in the tenant "", that every agent written before
	// heads were kept has one.
	converted = "converted:v1"
	// maxScan bounds how many pages of heads one List reads to fill a page
	// past retired names.
	maxScan = 8
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

// head is what is kept about one agent beside its versions: its highest
// version, how many it has, and its retirement. It is written in the same
// write as every version, retirement and revival, so it never disagrees
// with them.
type head struct {
	Latest  definition.Record `json:"latest"`
	Count   int               `json:"count"`
	Retired *Retirement       `json:"retired,omitempty"`
}

// Store is a definition.Source on a state.Store.
type Store struct {
	checks   definition.Checks
	clock    func() time.Time
	st       state.Store
	versions *state.Table[definition.Record]
	heads    *state.Table[head]
}

var _ definition.Source = (*Store)(nil)

// New returns a store held only in this process's memory.
func New(opts ...Option) *Store {
	s, err := Open(state.NewMemory(), opts...)
	if err != nil {
		panic(fmt.Sprintf("agents: a store in memory: %v", err))
	}
	return s
}

// Open returns a store on st, which may be shared with other processes. A
// nil st keeps definitions in this process's memory.
//
// Agents written by a release before heads were kept get theirs the first
// time any process opens the store; a process that opens it at the same time
// writes the same heads.
func Open(st state.Store, opts ...Option) (*Store, error) {
	if st == nil {
		st = state.NewMemory()
	}
	s := &Store{
		clock: time.Now, st: st,
		versions: state.NewTable[definition.Record](st, VersionsNamespace),
		heads:    state.NewTable[head](st, VersionsNamespace),
	}
	for _, opt := range opts {
		opt(s)
	}
	if err := s.convert(); err != nil {
		return nil, fmt.Errorf("agents: convert: %w", err)
	}
	return s, nil
}

func versionKey(name, version string) string { return name + "@" + version }
func headKey(name string) string             { return headPrefix + name }

func ctx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), state.Timeout)
}

func op(tenant, key string, v any, version int64) (state.Op, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return state.Op{}, err
	}
	o := state.Put(tenant, key, raw)
	o.If = version
	return o, nil
}

func (s *Store) write(ops ...state.Op) error {
	c, cancel := ctx()
	defer cancel()
	return s.st.Write(c, VersionsNamespace, ops...)
}

// higher reports whether version a is above b, in the order Versions gives.
func higher(a, b string) bool {
	c, err := definition.CompareVersions(a, b)
	if err != nil {
		return a > b
	}
	return c > 0
}

// convert gives every agent written before heads were kept its head, with
// the retirement the old namespace held.
func (s *Store) convert() error {
	if _, err := s.heads.Lookup("", converted); err == nil {
		return nil
	} else if !errors.Is(err, state.ErrNotFound) {
		return err
	}
	c, cancel := ctx()
	recs, err := s.st.Load(c, VersionsNamespace)
	cancel()
	if err != nil {
		return err
	}
	c, cancel = ctx()
	retired, err := s.st.Load(c, RetiredNamespace)
	cancel()
	if err != nil {
		return err
	}
	type id struct{ tenant, name string }
	heads := map[id]*head{}
	var order []id
	for _, r := range recs {
		if r.Tenant == "" || strings.HasPrefix(r.Key, headPrefix) {
			continue
		}
		var rec definition.Record
		if err := json.Unmarshal(r.Value, &rec); err != nil {
			return fmt.Errorf("%q/%q: %w", r.Tenant, r.Key, err)
		}
		if rec.Tenant != r.Tenant || r.Key != versionKey(rec.Name, rec.Version) {
			return fmt.Errorf("%q/%q holds %s@%s of tenant %q", r.Tenant, r.Key, rec.Name, rec.Version, rec.Tenant)
		}
		k := id{r.Tenant, rec.Name}
		h := heads[k]
		if h == nil {
			h = &head{Latest: rec}
			heads[k] = h
			order = append(order, k)
		} else if higher(rec.Version, h.Latest.Version) {
			h.Latest = rec
		}
		h.Count++
	}
	for _, r := range retired {
		if r.Tenant == "" {
			continue
		}
		if h := heads[id{r.Tenant, r.Key}]; h != nil {
			var ret Retirement
			if err := json.Unmarshal(r.Value, &ret); err != nil {
				return fmt.Errorf("retirement %q/%q: %w", r.Tenant, r.Key, err)
			}
			h.Retired = &ret
		}
	}
	for _, k := range order {
		o, err := op(k.tenant, headKey(k.name), heads[k], state.Missing)
		if err != nil {
			return err
		}
		if err := s.write(o); err != nil && !errors.Is(err, state.ErrConflict) {
			return err
		}
	}
	o, err := op("", converted, head{}, state.Missing)
	if err != nil {
		return err
	}
	if err := s.write(o); err != nil && !errors.Is(err, state.ErrConflict) {
		return err
	}
	return nil
}

// Put implements definition.Source.
func (s *Store) Put(_ context.Context, rec definition.Record) (definition.Record, error) {
	rec, err := definition.Prepare(rec, s.checks, s.clock())
	if err != nil {
		return definition.Record{}, err
	}
	out, err := s.insert([]definition.Record{rec}, false, nil)
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
	prepared, err := s.prepare(recs)
	if err != nil {
		return nil, err
	}
	return s.insert(prepared, true, nil)
}

// ImportRetired is Import for an agent that was deleted where it is moved
// from. The agent is retired in the same write that first creates it here,
// and only then: an agent this store already holds keeps its retirement as
// it is, so a move run again, or by two processes at once, never retires an
// agent revived after the first move.
func (s *Store) ImportRetired(_ context.Context, recs []definition.Record, by string) ([]definition.Record, error) {
	by = strings.TrimSpace(by)
	if by == "" {
		return nil, fmt.Errorf("%w: a retirement must say who made it", definition.ErrInvalid)
	}
	prepared, err := s.prepare(recs)
	if err != nil {
		return nil, err
	}
	return s.insert(prepared, true, &Retirement{By: by, At: s.clock().UTC()})
}

func (s *Store) prepare(recs []definition.Record) ([]definition.Record, error) {
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
	return prepared, nil
}

// insert writes prepared records of one tenant in one write, with the head
// of each agent they belong to, at the version each head was read: a writer
// that got there first makes it read again. retire, when set, retires each
// agent whose head this write creates.
func (s *Store) insert(recs []definition.Record, importing bool, retire *Retirement) ([]definition.Record, error) {
	if len(recs) == 0 {
		return nil, nil
	}
	tenant := recs[0].Tenant
	var names []string
	for _, rec := range recs {
		if rec.Tenant != tenant {
			return nil, fmt.Errorf("%w: one write holds one tenant's definitions", definition.ErrInvalid)
		}
		if !slices.Contains(names, rec.Name) {
			names = append(names, rec.Name)
		}
	}
	out := make([]definition.Record, len(recs))
	err := state.Retry(func() error {
		var ops []state.Op
		for _, name := range names {
			h, hv, err := s.head(tenant, name)
			if err != nil {
				return err
			}
			found := hv > 0
			var fresh []definition.Record
		next:
			for i, rec := range recs {
				if rec.Name != name {
					continue
				}
				for _, have := range fresh {
					if have.Version == rec.Version {
						if definition.Same(have, rec) {
							out[i] = clone(have)
							continue next
						}
						return fmt.Errorf("%w: %s@%s holds other content; a version is never rewritten", definition.ErrExists, rec.Name, rec.Version)
					}
				}
				if found {
					have, err := s.versions.Lookup(tenant, versionKey(rec.Name, rec.Version))
					if err == nil {
						if definition.Same(have.Value, rec) {
							out[i] = clone(have.Value)
							continue
						}
						return fmt.Errorf("%w: %s@%s holds other content; a version is never rewritten", definition.ErrExists, rec.Name, rec.Version)
					}
					if !errors.Is(err, state.ErrNotFound) {
						return err
					}
					if h.Retired != nil && !importing {
						return fmt.Errorf("%w: %s", definition.ErrRetired, rec.Name)
					}
				}
				o, err := op(tenant, versionKey(rec.Name, rec.Version), rec, state.Missing)
				if err != nil {
					return err
				}
				ops = append(ops, o)
				fresh = append(fresh, rec)
				out[i] = clone(rec)
			}
			if len(fresh) == 0 {
				continue
			}
			for _, rec := range fresh {
				if h.Count == 0 || higher(rec.Version, h.Latest.Version) {
					h.Latest = rec
				}
				h.Count++
			}
			if h.Count > MaxVersions {
				return fmt.Errorf("%w: %s may have at most %d versions", definition.ErrInvalid, name, MaxVersions)
			}
			version := hv
			if !found {
				version = state.Missing
				h.Retired = retire
			}
			o, err := op(tenant, headKey(name), h, version)
			if err != nil {
				return err
			}
			ops = append(ops, o)
		}
		if len(ops) == 0 {
			return nil
		}
		if err := s.write(ops...); err != nil {
			if errors.Is(err, state.ErrConflict) {
				return err
			}
			return fmt.Errorf("agents: write: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// head reads one agent's head and its version, 0 when it has none.
func (s *Store) head(tenant, name string) (head, int64, error) {
	it, err := s.heads.Lookup(tenant, headKey(name))
	if errors.Is(err, state.ErrNotFound) {
		return head{}, 0, nil
	}
	if err != nil {
		return head{}, 0, fmt.Errorf("agents: read: %w", err)
	}
	return it.Value, it.Version, nil
}

func validName(name string) bool {
	return name != "" && !strings.ContainsAny(name, "@:")
}

// Get implements definition.Source.
func (s *Store) Get(_ context.Context, tenant, name, version string) (definition.Record, error) {
	if tenant == "" || !validName(name) {
		return definition.Record{}, fmt.Errorf("%w: %s@%s", definition.ErrNotFound, name, version)
	}
	it, err := s.versions.Lookup(tenant, versionKey(name, version))
	if errors.Is(err, state.ErrNotFound) {
		return definition.Record{}, fmt.Errorf("%w: %s@%s", definition.ErrNotFound, name, version)
	}
	if err != nil {
		return definition.Record{}, fmt.Errorf("agents: read: %w", err)
	}
	return clone(it.Value), nil
}

// Versions implements definition.Source.
func (s *Store) Versions(_ context.Context, tenant, name string) ([]definition.Record, error) {
	if tenant == "" || !validName(name) {
		return nil, fmt.Errorf("%w: %s", definition.ErrNotFound, name)
	}
	items, err := s.versions.All(tenant, name+"@", MaxVersions)
	if err != nil {
		return nil, fmt.Errorf("agents: read: %w", err)
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("%w: %s", definition.ErrNotFound, name)
	}
	out := make([]definition.Record, len(items))
	for i, it := range items {
		out[i] = clone(it.Value)
	}
	definition.SortVersions(out)
	return out, nil
}

// Latest implements definition.Source.
func (s *Store) Latest(_ context.Context, tenant, name string) (definition.Record, error) {
	if tenant == "" || !validName(name) {
		return definition.Record{}, fmt.Errorf("%w: %s", definition.ErrNotFound, name)
	}
	h, hv, err := s.head(tenant, name)
	switch {
	case err != nil:
		return definition.Record{}, err
	case hv == 0 || h.Count == 0:
		return definition.Record{}, fmt.Errorf("%w: %s", definition.ErrNotFound, name)
	case h.Retired != nil:
		return definition.Record{}, fmt.Errorf("%w: %s", definition.ErrRetired, name)
	}
	return clone(h.Latest), nil
}

// List implements definition.Source. It pages the tenant's heads by name,
// leaving retired names out. A page may hold fewer than its size, with a
// token for the next, when many retired names lie in its way.
func (s *Store) List(_ context.Context, tenant string, page definition.Page) ([]definition.Record, string, error) {
	after, err := definition.PageAfter(page.Token, tenant)
	if err != nil {
		return nil, "", err
	}
	size := page.Limit()
	out := make([]definition.Record, 0, size)
	cursor := ""
	if after != "" {
		cursor = headKey(after)
	}
	last := ""
	for range maxScan {
		items, next, err := s.heads.List(tenant, state.Query{Prefix: headPrefix, After: cursor, Limit: size + 1})
		if err != nil {
			return nil, "", fmt.Errorf("agents: read: %w", err)
		}
		for _, it := range items {
			if it.Value.Retired != nil || it.Value.Count == 0 {
				last = strings.TrimPrefix(it.Key, headPrefix)
				continue
			}
			if len(out) == size {
				return out, definition.PageToken(tenant, last), nil
			}
			out = append(out, clone(it.Value.Latest))
			last = strings.TrimPrefix(it.Key, headPrefix)
		}
		if next == "" {
			return out, "", nil
		}
		cursor = next
	}
	return out, definition.PageToken(tenant, last), nil
}

// Retire implements definition.Source.
func (s *Store) Retire(_ context.Context, tenant, name, by string) error {
	by = strings.TrimSpace(by)
	if by == "" {
		return fmt.Errorf("%w: a retirement must say who made it", definition.ErrInvalid)
	}
	if tenant == "" || !validName(name) {
		return fmt.Errorf("%w: %s", definition.ErrNotFound, name)
	}
	return s.updateHead(tenant, name, func(h *head) error {
		if h.Retired != nil {
			return state.ErrUnchanged
		}
		h.Retired = &Retirement{By: by, At: s.clock().UTC()}
		return nil
	}, true)
}

// Revive lifts a name's retirement, so new versions may be written under it
// again. Every version written before stays as it was: a revived agent
// continues its versions rather than starting over.
func (s *Store) Revive(_ context.Context, tenant, name string) error {
	if tenant == "" || !validName(name) {
		return nil
	}
	return s.updateHead(tenant, name, func(h *head) error {
		if h.Retired == nil {
			return state.ErrUnchanged
		}
		h.Retired = nil
		return nil
	}, false)
}

// updateHead changes an agent's head by compare-and-set. An agent with no
// head is ErrNotFound when missing says so, and else nothing to change.
func (s *Store) updateHead(tenant, name string, fn func(h *head) error, missing bool) error {
	return state.Retry(func() error {
		h, hv, err := s.head(tenant, name)
		if err != nil {
			return err
		}
		if hv == 0 || h.Count == 0 {
			if missing {
				return fmt.Errorf("%w: %s", definition.ErrNotFound, name)
			}
			return nil
		}
		if err := fn(&h); errors.Is(err, state.ErrUnchanged) {
			return nil
		} else if err != nil {
			return err
		}
		o, err := op(tenant, headKey(name), h, hv)
		if err != nil {
			return err
		}
		if err := s.write(o); err != nil {
			if errors.Is(err, state.ErrConflict) {
				return err
			}
			return fmt.Errorf("agents: write retirement: %w", err)
		}
		return nil
	})
}

// Retired reports who retired a name, and whether it is retired.
func (s *Store) Retired(tenant, name string) (Retirement, bool, error) {
	if tenant == "" || !validName(name) {
		return Retirement{}, false, nil
	}
	h, _, err := s.head(tenant, name)
	if err != nil || h.Retired == nil {
		return Retirement{}, false, err
	}
	return *h.Retired, true, nil
}

func clone(r definition.Record) definition.Record {
	r.Policy = r.Policy.Normalize()
	return r
}
