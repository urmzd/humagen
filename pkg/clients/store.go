package clients

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/urmzd/humagen/pkg/state"
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
//
// A durable store goes behind these six methods: one table keyed by tenant and
// id, with Create's count taken in the same transaction as its insert.
type Store interface {
	// Create adds a client if the caps leave room. The count and the insert
	// are one step, so two creates cannot both take the last place.
	Create(tenant string, c Client, caps Caps, at time.Time) (Client, error)
	// Update replaces a client's record, keeping its id, surface and origin.
	Update(tenant string, c Client, at time.Time) (Client, error)
	Get(tenant, id string) (Client, error)
	// List returns a tenant's clients, oldest first, never more than Ceiling.
	List(tenant string) []Client
	Delete(tenant, id string) error
	// On returns every enabled client on a surface, in every tenant, oldest
	// first. It exists for ingress, which has no tenant until a client's
	// secret has verified the request.
	On(surface Surface) []Client
}

// Memory is a Store in memory. Built with Open, it writes every change
// through a state.Store and reads the records back when it is built again.
//
// A client the environment configures (Managed is ManagedEnv) is never
// written: the environment is where it lives, and it is created again from
// there at every start.
type Memory struct {
	mu sync.RWMutex
	// all is every client, oldest first.
	all []Client
	// order is each client's place in all, as it was written, so a restart
	// keeps the order clients were created in.
	order map[[2]string]int64
	seq   int64
	kept  *state.Table[storedClient]
}

// storedClient is a client as it is written.
type storedClient struct {
	Seq    int64  `json:"seq"`
	Client Client `json:"client"`
}

// NewMemory returns an empty store in memory.
func NewMemory() *Memory { return &Memory{order: map[[2]string]int64{}} }

// Open returns a store that keeps its records in st, holding what st already
// has. A nil st is NewMemory.
func Open(st state.Store) (*Memory, error) {
	m := NewMemory()
	m.kept = state.NewTable[storedClient](st, "clients")
	var loaded []storedClient
	err := m.kept.Load(func(tenant, id string, r storedClient) error {
		r.Client.Tenant, r.Client.ID = tenant, id
		loaded = append(loaded, r)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("clients: load: %w", err)
	}
	slices.SortStableFunc(loaded, func(a, b storedClient) int { return cmp.Compare(a.Seq, b.Seq) })
	for _, r := range loaded {
		m.all = append(m.all, r.Client)
		m.order[[2]string{r.Client.Tenant, r.Client.ID}] = r.Seq
		m.seq = max(m.seq, r.Seq)
	}
	return m, nil
}

var _ Store = (*Memory)(nil)

// write puts a client's record, unless the environment manages it.
func (m *Memory) write(c Client, seq int64) error {
	if c.Managed == ManagedEnv {
		return nil
	}
	if err := m.kept.Put(c.Tenant, c.ID, storedClient{Seq: seq, Client: c}); err != nil {
		return fmt.Errorf("clients: write: %w", err)
	}
	return nil
}

func (m *Memory) index(tenant, id string) int {
	return slices.IndexFunc(m.all, func(c Client) bool { return c.Tenant == tenant && c.ID == id })
}

// Create implements Store.
func (m *Memory) Create(tenant string, c Client, caps Caps, at time.Time) (Client, error) {
	c = c.clone()
	c.Tenant = tenant
	if tenant == "" {
		return Client{}, fmt.Errorf("%w: a client belongs to a workspace", ErrInvalid)
	}
	if err := c.Validate(); err != nil {
		return Client{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.index(tenant, c.ID) >= 0 {
		return Client{}, fmt.Errorf("%w: %q", ErrExists, c.ID)
	}
	total, onSurface := 0, 0
	for _, other := range m.all {
		if other.Tenant != tenant {
			continue
		}
		total++
		if other.Surface == c.Surface {
			onSurface++
			if other.overlaps(c) {
				return Client{}, fmt.Errorf("%w: %q already answers for that %s identity", ErrExists, other.ID, c.Surface)
			}
		}
	}
	switch {
	case total >= min(caps.Max, Ceiling):
		return Client{}, fmt.Errorf("%w: the workspace may have %d clients", ErrLimit, min(caps.Max, Ceiling))
	case onSurface >= caps.MaxPerSurface:
		return Client{}, fmt.Errorf("%w: the workspace may have %d %s clients", ErrLimit, caps.MaxPerSurface, c.Surface)
	}
	c.Version, c.CreatedAt, c.UpdatedAt = 1, at, at
	seq := m.seq + 1
	if err := m.write(c, seq); err != nil {
		return Client{}, err
	}
	m.seq = seq
	m.order[[2]string{tenant, c.ID}] = seq
	m.all = append(m.all, c)
	return c.clone(), nil
}

// Update implements Store.
func (m *Memory) Update(tenant string, c Client, at time.Time) (Client, error) {
	c = c.clone()
	c.Tenant = tenant
	if err := c.Validate(); err != nil {
		return Client{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	i := m.index(tenant, c.ID)
	if i < 0 {
		return Client{}, fmt.Errorf("%w: %q", ErrNotFound, c.ID)
	}
	old := m.all[i]
	if old.Surface != c.Surface {
		return Client{}, fmt.Errorf("%w: a client stays on the surface it was created on", ErrInvalid)
	}
	for _, other := range m.all {
		if other.Tenant == tenant && other.ID != c.ID && other.overlaps(c) {
			return Client{}, fmt.Errorf("%w: %q already answers for that %s identity", ErrExists, other.ID, c.Surface)
		}
	}
	c.Managed, c.CreatedBy, c.CreatedAt = old.Managed, old.CreatedBy, old.CreatedAt
	c.Version, c.UpdatedAt = old.Version+1, at
	if err := m.write(c, m.order[[2]string{tenant, c.ID}]); err != nil {
		return Client{}, err
	}
	m.all[i] = c
	return c.clone(), nil
}

// Get implements Store.
func (m *Memory) Get(tenant, id string) (Client, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	i := m.index(tenant, id)
	if i < 0 {
		return Client{}, fmt.Errorf("%w: %q", ErrNotFound, id)
	}
	return m.all[i].clone(), nil
}

// List implements Store.
func (m *Memory) List(tenant string) []Client {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := []Client{}
	for _, c := range m.all {
		if c.Tenant == tenant && len(out) < Ceiling {
			out = append(out, c.clone())
		}
	}
	return out
}

// Delete implements Store.
func (m *Memory) Delete(tenant, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	i := m.index(tenant, id)
	if i < 0 {
		return fmt.Errorf("%w: %q", ErrNotFound, id)
	}
	if m.all[i].Managed != ManagedEnv {
		if err := m.kept.Delete(tenant, id); err != nil {
			return fmt.Errorf("clients: write: %w", err)
		}
	}
	m.all = slices.Delete(m.all, i, i+1)
	delete(m.order, [2]string{tenant, id})
	return nil
}

// On implements Store.
func (m *Memory) On(surface Surface) []Client {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []Client
	for _, c := range m.all {
		if c.Surface == surface && c.Enabled {
			out = append(out, c.clone())
		}
	}
	return out
}

// Secrets keeps the secret values written for clients, apart from the records.
// A durable deployment puts a secret manager behind it.
type Secrets interface {
	Put(tenant, client, name, value string) error
	Get(tenant, client, name string) (string, bool)
	// Drop forgets every secret of a client.
	Drop(tenant, client string) error
}

// MemorySecrets is Secrets in memory. Without a state.Store it is lost on
// restart, so a client whose secrets were written as values has to be given
// them again. Built with OpenSecrets, it writes each value through the store
// as it was given: the database then holds the secrets in the clear, and is
// to be guarded as they are. A reference (env:NAME) keeps no value at all.
type MemorySecrets struct {
	mu     sync.RWMutex
	values map[[3]string]string
	kept   *state.Table[string]
}

// NewMemorySecrets returns an empty secret store in memory.
func NewMemorySecrets() *MemorySecrets { return &MemorySecrets{values: map[[3]string]string{}} }

// OpenSecrets returns a secret store that keeps its values in st, holding
// what st already has. A nil st is NewMemorySecrets.
func OpenSecrets(st state.Store) (*MemorySecrets, error) {
	s := NewMemorySecrets()
	s.kept = state.NewTable[string](st, "clients.secret")
	err := s.kept.Load(func(tenant, key string, v string) error {
		client, name, ok := splitSecretKey(key)
		if !ok {
			return fmt.Errorf("clients: a secret is kept under %q", key)
		}
		s.values[[3]string{tenant, client, name}] = v
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("clients: load secrets: %w", err)
	}
	return s, nil
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
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.kept.Put(tenant, secretKey(client, name), value); err != nil {
		return fmt.Errorf("clients: write secret: %w", err)
	}
	s.values[[3]string{tenant, client, name}] = value
	return nil
}

// Get implements Secrets.
func (s *MemorySecrets) Get(tenant, client, name string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.values[[3]string{tenant, client, name}]
	return v, ok
}

// Drop implements Secrets.
func (s *MemorySecrets) Drop(tenant, client string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	batch := s.kept.Batch()
	for k := range s.values {
		if k[0] == tenant && k[1] == client {
			batch.Delete(tenant, secretKey(client, k[2]))
		}
	}
	if err := batch.Commit(); err != nil {
		return fmt.Errorf("clients: write secret: %w", err)
	}
	for k := range s.values {
		if k[0] == tenant && k[1] == client {
			delete(s.values, k)
		}
	}
	return nil
}
