package clients

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/urmzd/humagen/pkg/state"

	"github.com/urmzd/humagen/internal/legacy"
)

// Caps is how many clients a workspace may have. Zero means none: a caller
// that wants the defaults says so.
type Caps struct {
	// Max is the most clients in the workspace.
	Max int `json:"max"`
	// MaxPerSurface is the most on any one surface.
	MaxPerSurface int `json:"max_per_surface"`
}

// DefaultCaps are the caps of a workspace whose policy sets none.
func DefaultCaps() Caps { return Caps{Max: DefaultMax, MaxPerSurface: DefaultMaxPerSurface} }

// Store keeps client records. Every read and write names a tenant, and a
// client in another tenant is reported as ErrNotFound.
type Store interface {
	// Create adds a client if the caps leave room. The count and the insert
	// are one step, so two creates cannot both take the last place.
	Create(tenant string, c Client, caps Caps, at time.Time) (Client, error)
	// Update replaces a client's record, keeping its id, surface and origin.
	Update(tenant string, c Client, at time.Time) (Client, error)
	Get(tenant, id string) (Client, error)
	// List returns a tenant's clients, oldest first, never more than Ceiling.
	List(tenant string) ([]Client, error)
	Delete(tenant, id string) error
	// On returns every enabled client on a surface, in every tenant, tenant
	// by tenant and oldest first within each. It exists for ingress, which
	// has no tenant until a client's secret has verified the request.
	On(surface Surface) ([]Client, error)
}

// Namespaces and keys in the state store.
//
//	clients        <tenant>/<id>     a client (storedClient)
//	clients        <tenant>/#head    the tenant's last Seq (head), written
//	                                 with every create and update
//	clients        ""/tenant:<t>     the tenant directory (state.Tenants)
//	clients.secret <tenant>/<client>/<name>  a secret's value
//
// An id holds only lowercase letters, digits and dashes, so "#head" is no
// client's.
const (
	namespace       = "clients"
	secretNamespace = "clients.secret"
	headKey         = "#head"
)

// Memory is a Store on a state.Store. It reads and writes per request: it
// holds no client in memory but those the environment configures, so any
// number of processes on one database agree.
//
// A client the environment configures (Managed is ManagedEnv) is never
// written: the environment is where it lives, and every process creates it
// again from there at every start. Those are kept in this process only.
type Memory struct {
	st      state.Store
	tenants *state.Tenants

	mu sync.RWMutex
	// env are the clients the environment configures, by tenant, oldest
	// first.
	env map[string][]Client
}

// storedClient is a client as it is written.
type storedClient struct {
	Seq    int64  `json:"seq"`
	Client Client `json:"client"`
}

// head is a tenant's last Seq. Every create and update writes it at the
// version it read, so two writers in one tenant check caps and identities
// one after the other.
type head struct {
	Seq int64 `json:"seq"`
}

// NewMemory returns an empty store in this process's memory.
func NewMemory() *Memory {
	m, err := Open(nil)
	if err != nil {
		panic(err) // a store in memory does not fail to open
	}
	return m
}

// Open returns a store on st, which may be shared with other processes. A
// nil st keeps clients in this process's memory.
func Open(st state.Store) (*Memory, error) {
	if st == nil {
		st = state.NewMemory()
	}
	dir, err := state.OpenTenants(st, namespace)
	if err != nil {
		return nil, fmt.Errorf("clients: open: %w", err)
	}
	return &Memory{st: st, tenants: dir, env: map[string][]Client{}}, nil
}

var _ Store = (*Memory)(nil)

func ctx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), state.Timeout)
}

// held is what a tenant keeps: its stored clients, oldest first, with their
// seq and version, and its head.
type held struct {
	clients  []storedClient
	versions map[string]int64
	head     head
	headAt   int64 // 0 when there is no head
}

func (h held) client(id string) (storedClient, bool) {
	for _, r := range h.clients {
		if r.Client.ID == id {
			return r, true
		}
	}
	return storedClient{}, false
}

// next is the seq a new client takes.
func (h held) next() int64 {
	n := h.head.Seq
	for _, r := range h.clients {
		n = max(n, r.Seq)
	}
	return n + 1
}

// read reads a tenant's records, never more than Ceiling clients.
func (m *Memory) read(tenant string) (held, error) {
	h := held{versions: map[string]int64{}}
	if tenant == "" {
		return h, nil
	}
	c, cancel := ctx()
	defer cancel()
	after := ""
	for {
		recs, next, err := m.st.List(c, namespace, tenant, state.Query{After: after, Limit: Ceiling + 2})
		if err != nil {
			return held{}, fmt.Errorf("clients: read: %w", err)
		}
		for _, r := range recs {
			if r.Key == headKey {
				if err := json.Unmarshal(r.Value, &h.head); err != nil {
					return held{}, fmt.Errorf("clients: %q/%q: %w", tenant, r.Key, err)
				}
				h.headAt = r.Version
				continue
			}
			if strings.HasPrefix(r.Key, "#") {
				continue
			}
			var sc storedClient
			if err := json.Unmarshal(r.Value, &sc); err != nil {
				return held{}, fmt.Errorf("clients: %q/%q: %w", tenant, r.Key, err)
			}
			sc.Client.Tenant, sc.Client.ID = tenant, r.Key
			sc.Client.DefaultAgent = legacy.Default(sc.Client.DefaultAgent)
			h.clients = append(h.clients, sc)
			h.versions[r.Key] = r.Version
		}
		if len(h.clients) > Ceiling {
			return held{}, fmt.Errorf("clients: read: %w: %q holds more than %d clients", state.ErrTooMany, tenant, Ceiling)
		}
		if next == "" {
			break
		}
		after = next
	}
	slices.SortStableFunc(h.clients, func(a, b storedClient) int {
		return cmp.Or(cmp.Compare(a.Seq, b.Seq), strings.Compare(a.Client.ID, b.Client.ID))
	})
	return h, nil
}

// envOf is a copy of a tenant's environment clients.
func (m *Memory) envOf(tenant string) []Client {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return slices.Clone(m.env[tenant])
}

// all is every client of a tenant: the environment's, then the stored ones,
// oldest first.
func all(env []Client, h held) []Client {
	out := slices.Clone(env)
	for _, r := range h.clients {
		out = append(out, r.Client)
	}
	return out
}

// write is one Write's ops, built as JSON.
type write struct {
	ops []state.Op
	err error
}

func (w *write) put(tenant, key string, v any, version int64) {
	raw, err := json.Marshal(v)
	if err != nil {
		w.err = cmp.Or(w.err, err)
		return
	}
	switch version {
	case 0:
		w.ops = append(w.ops, state.Create(tenant, key, raw))
	default:
		w.ops = append(w.ops, state.PutAt(tenant, key, raw, version))
	}
}

func (w *write) commit(st state.Store) error {
	if w.err != nil {
		return w.err
	}
	c, cancel := ctx()
	defer cancel()
	return st.Write(c, namespace, w.ops...)
}

// Create implements Store.
func (m *Memory) Create(tenant string, c Client, caps Caps, at time.Time) (Client, error) {
	c = c.clone()
	c.Tenant = tenant
	c.DefaultAgent = legacy.Default(c.DefaultAgent)
	if tenant == "" {
		return Client{}, fmt.Errorf("%w: a client belongs to a workspace", ErrInvalid)
	}
	if err := c.Validate(); err != nil {
		return Client{}, err
	}
	c.Version, c.CreatedAt, c.UpdatedAt = 1, at, at
	if c.Managed == ManagedEnv {
		m.mu.Lock()
		defer m.mu.Unlock()
		h, err := m.read(tenant)
		if err != nil {
			return Client{}, err
		}
		if err := admit(all(m.env[tenant], h), c, caps); err != nil {
			return Client{}, err
		}
		m.env[tenant] = append(m.env[tenant], c)
		return c.clone(), nil
	}
	if err := m.tenants.Mark(tenant); err != nil {
		return Client{}, fmt.Errorf("clients: write: %w", err)
	}
	err := state.Retry(func() error {
		h, err := m.read(tenant)
		if err != nil {
			return err
		}
		if err := admit(all(m.envOf(tenant), h), c, caps); err != nil {
			return err
		}
		seq := h.next()
		w := &write{}
		w.put(tenant, c.ID, storedClient{Seq: seq, Client: c}, 0)
		w.put(tenant, headKey, head{Seq: seq}, h.headAt)
		return w.commit(m.st)
	})
	if err != nil {
		if errors.Is(err, ErrExists) || errors.Is(err, ErrLimit) {
			return Client{}, err
		}
		return Client{}, fmt.Errorf("clients: write: %w", err)
	}
	return c.clone(), nil
}

// admit checks that c may join a tenant's clients under caps.
func admit(have []Client, c Client, caps Caps) error {
	total, onSurface := 0, 0
	for _, other := range have {
		if other.ID == c.ID {
			return fmt.Errorf("%w: %q", ErrExists, c.ID)
		}
	}
	for _, other := range have {
		total++
		if other.Surface == c.Surface {
			onSurface++
			if other.overlaps(c) {
				return fmt.Errorf("%w: %q already answers for that %s identity", ErrExists, other.ID, c.Surface)
			}
		}
	}
	switch {
	case total >= min(caps.Max, Ceiling):
		return fmt.Errorf("%w: the workspace may have %d clients", ErrLimit, min(caps.Max, Ceiling))
	case onSurface >= caps.MaxPerSurface:
		return fmt.Errorf("%w: the workspace may have %d %s clients", ErrLimit, caps.MaxPerSurface, c.Surface)
	}
	return nil
}

// revise checks c as the next record of old among a tenant's clients, and
// fills what it keeps from old.
func revise(have []Client, old Client, c Client, at time.Time) (Client, error) {
	if old.Surface != c.Surface {
		return Client{}, fmt.Errorf("%w: a client stays on the surface it was created on", ErrInvalid)
	}
	for _, other := range have {
		if other.ID != c.ID && other.overlaps(c) {
			return Client{}, fmt.Errorf("%w: %q already answers for that %s identity", ErrExists, other.ID, c.Surface)
		}
	}
	c.Managed, c.CreatedBy, c.CreatedAt = old.Managed, old.CreatedBy, old.CreatedAt
	c.Version, c.UpdatedAt = old.Version+1, at
	return c, nil
}

// Update implements Store.
func (m *Memory) Update(tenant string, c Client, at time.Time) (Client, error) {
	c = c.clone()
	c.Tenant = tenant
	c.DefaultAgent = legacy.Default(c.DefaultAgent)
	if err := c.Validate(); err != nil {
		return Client{}, err
	}
	if out, ok, err := m.updateEnv(tenant, c, at); ok || err != nil {
		return out, err
	}
	var out Client
	err := state.Retry(func() error {
		h, err := m.read(tenant)
		if err != nil {
			return err
		}
		old, ok := h.client(c.ID)
		if !ok {
			return fmt.Errorf("%w: %q", ErrNotFound, c.ID)
		}
		next, err := revise(all(m.envOf(tenant), h), old.Client, c, at)
		if err != nil {
			return err
		}
		w := &write{}
		w.put(tenant, c.ID, storedClient{Seq: old.Seq, Client: next}, h.versions[c.ID])
		hd := h.head
		hd.Seq = max(hd.Seq, h.next()-1)
		w.put(tenant, headKey, hd, h.headAt)
		if err := w.commit(m.st); err != nil {
			return err
		}
		out = next
		return nil
	})
	if err != nil {
		if errors.Is(err, ErrNotFound) || errors.Is(err, ErrInvalid) || errors.Is(err, ErrExists) {
			return Client{}, err
		}
		return Client{}, fmt.Errorf("clients: write: %w", err)
	}
	return out.clone(), nil
}

// updateEnv updates a client the environment configures, reporting whether
// c is one.
func (m *Memory) updateEnv(tenant string, c Client, at time.Time) (Client, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	i := slices.IndexFunc(m.env[tenant], func(e Client) bool { return e.ID == c.ID })
	if i < 0 {
		return Client{}, false, nil
	}
	h, err := m.read(tenant)
	if err != nil {
		return Client{}, true, err
	}
	next, err := revise(all(m.env[tenant], h), m.env[tenant][i], c, at)
	if err != nil {
		return Client{}, true, err
	}
	m.env[tenant][i] = next
	return next.clone(), true, nil
}

// Get implements Store.
func (m *Memory) Get(tenant, id string) (Client, error) {
	for _, c := range m.envOf(tenant) {
		if c.ID == id {
			return c.clone(), nil
		}
	}
	if tenant == "" || id == "" || strings.HasPrefix(id, "#") {
		return Client{}, fmt.Errorf("%w: %q", ErrNotFound, id)
	}
	c, cancel := ctx()
	defer cancel()
	r, err := m.st.Lookup(c, namespace, tenant, id)
	if errors.Is(err, state.ErrNotFound) {
		return Client{}, fmt.Errorf("%w: %q", ErrNotFound, id)
	}
	if err != nil {
		return Client{}, fmt.Errorf("clients: read: %w", err)
	}
	var sc storedClient
	if err := json.Unmarshal(r.Value, &sc); err != nil {
		return Client{}, fmt.Errorf("clients: %q/%q: %w", tenant, id, err)
	}
	sc.Client.Tenant, sc.Client.ID = tenant, id
	sc.Client.DefaultAgent = legacy.Default(sc.Client.DefaultAgent)
	return sc.Client.clone(), nil
}

// List implements Store.
func (m *Memory) List(tenant string) ([]Client, error) {
	h, err := m.read(tenant)
	if err != nil {
		return nil, err
	}
	out := []Client{}
	for _, c := range all(m.envOf(tenant), h) {
		if len(out) < Ceiling {
			out = append(out, c.clone())
		}
	}
	return out, nil
}

// Delete implements Store.
func (m *Memory) Delete(tenant, id string) error {
	m.mu.Lock()
	if i := slices.IndexFunc(m.env[tenant], func(e Client) bool { return e.ID == id }); i >= 0 {
		m.env[tenant] = slices.Delete(m.env[tenant], i, i+1)
		m.mu.Unlock()
		return nil
	}
	m.mu.Unlock()
	if tenant == "" || id == "" || strings.HasPrefix(id, "#") {
		return fmt.Errorf("%w: %q", ErrNotFound, id)
	}
	err := state.Retry(func() error {
		c, cancel := ctx()
		defer cancel()
		r, err := m.st.Lookup(c, namespace, tenant, id)
		if errors.Is(err, state.ErrNotFound) {
			return fmt.Errorf("%w: %q", ErrNotFound, id)
		}
		if err != nil {
			return err
		}
		return m.st.Write(c, namespace, state.DeleteAt(tenant, id, r.Version))
	})
	if err != nil && !errors.Is(err, ErrNotFound) {
		return fmt.Errorf("clients: write: %w", err)
	}
	return err
}

// On implements Store. It pages the tenant directory and reads each
// tenant's clients, so it is for a start or a rare sweep, not a request.
func (m *Memory) On(surface Surface) ([]Client, error) {
	byTenant := map[string][]Client{}
	m.mu.RLock()
	names := make([]string, 0, len(m.env))
	for t, cs := range m.env {
		byTenant[t] = slices.Clone(cs)
		names = append(names, t)
	}
	m.mu.RUnlock()
	err := m.tenants.Each(func(tenant string) error {
		h, err := m.read(tenant)
		if err != nil {
			return err
		}
		if _, ok := byTenant[tenant]; !ok {
			names = append(names, tenant)
		}
		byTenant[tenant] = all(byTenant[tenant], h)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("clients: read: %w", err)
	}
	slices.Sort(names)
	var out []Client
	for _, t := range names {
		for _, c := range byTenant[t] {
			if c.Surface == surface && c.Enabled {
				out = append(out, c.clone())
			}
		}
	}
	return out, nil
}

// Secrets keeps the secret values written for clients, apart from the records.
// A durable deployment puts a secret manager behind it.
type Secrets interface {
	Put(tenant, client, name, value string) error
	// Get returns a secret's value and whether one is kept.
	Get(tenant, client, name string) (string, bool)
	// Drop forgets every secret of a client.
	Drop(tenant, client string) error
}

// MemorySecrets is Secrets on a state.Store, read and written per request.
// Without a state.Store it is in this process's memory and lost on restart,
// so a client whose secrets were written as values has to be given them
// again. Built with OpenSecrets, it writes each value through the store as it
// was given: the database then holds the secrets in the clear, and is to be
// guarded as they are. A reference (env:NAME) keeps no value at all.
type MemorySecrets struct {
	kept *state.Table[string]
}

// maxSecrets bounds the secrets one Drop reads under a client's prefix.
const maxSecrets = 1000

// NewMemorySecrets returns an empty secret store in this process's memory.
func NewMemorySecrets() *MemorySecrets {
	s, _ := OpenSecrets(nil)
	return s
}

// OpenSecrets returns a secret store on st, which may be shared with other
// processes. A nil st keeps secrets in this process's memory.
func OpenSecrets(st state.Store) (*MemorySecrets, error) {
	if st == nil {
		st = state.NewMemory()
	}
	return &MemorySecrets{kept: state.NewTable[string](st, secretNamespace)}, nil
}

// secretKey names a secret within its tenant. A secret's name holds no
// slash, so the last one parts it from the client's id.
func secretKey(client, name string) string { return client + "/" + name }

func splitSecretKey(key string) (client, name string, ok bool) {
	i := strings.LastIndexByte(key, '/')
	if i <= 0 {
		return "", "", false
	}
	return key[:i], key[i+1:], true
}

var _ Secrets = (*MemorySecrets)(nil)

// Put implements Secrets.
func (s *MemorySecrets) Put(tenant, client, name, value string) error {
	if err := s.kept.Put(tenant, secretKey(client, name), value); err != nil {
		return fmt.Errorf("clients: write secret: %w", err)
	}
	return nil
}

// Get implements Secrets. A secret that cannot be read reads as missing:
// what needs it fails closed.
func (s *MemorySecrets) Get(tenant, client, name string) (string, bool) {
	it, err := s.kept.Lookup(tenant, secretKey(client, name))
	if err != nil {
		return "", false
	}
	return it.Value, true
}

// Drop implements Secrets.
func (s *MemorySecrets) Drop(tenant, client string) error {
	items, err := s.kept.All(tenant, client+"/", maxSecrets)
	if err != nil {
		return fmt.Errorf("clients: read secrets: %w", err)
	}
	batch := s.kept.Batch()
	for _, it := range items {
		// A person's subject may hold a slash, so another key may share the
		// prefix: only this client's own are dropped.
		if c, _, ok := splitSecretKey(it.Key); ok && c == client {
			batch.Delete(tenant, it.Key)
		}
	}
	if err := batch.Commit(); err != nil {
		return fmt.Errorf("clients: write secret: %w", err)
	}
	return nil
}
