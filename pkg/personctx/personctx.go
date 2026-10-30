// Package personctx keeps what one person brings to the runs that act for
// them: their run settings, the tool servers they add for themselves, and
// the memory volume that is theirs.
//
// It belongs to a person in a tenant, and nobody else reads or writes it.
// Like pkg/prefs, it never grants: a person's ceilings only tighten what the
// workspace and the project allow, their preferences choose only within
// those ceilings (pkg/settings), and a tool server of theirs reaches a run
// only on a host the run's ceilings list.
//
// A tool server is kept as a definition and, optionally, a credential
// reference (pkg/credref). The secret a reference names is never read or
// stored here. Whether a reference is the person's own to give is the
// platform's decision, made before Set.
//
// The store is in memory. Built with Open, it writes every change through a
// state.Store and reads them back when it is built again.
package personctx

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"sync"
	"time"

	"github.com/urmzd/humagen/pkg/credref"
	"github.com/urmzd/humagen/pkg/settings"
	"github.com/urmzd/humagen/pkg/state"
)

// Errors a caller can match.
var (
	ErrInvalid = errors.New("personctx: invalid")
	// ErrVersion reports a write naming a version that is no longer current.
	ErrVersion = errors.New("personctx: changed since it was read; reload and try again")
)

// Bounds.
const (
	// MaxToolServers is the most tool servers one person adds.
	MaxToolServers = 16
	// MaxURL bounds a tool server's address.
	MaxURL = 500
	// maxSubject bounds a subject, as pkg/credentials does.
	maxSubject = 200
)

// ToolServer is an MCP server a person adds for themselves.
type ToolServer struct {
	// Name is lower case letters, digits and dashes, unique among the
	// person's servers.
	Name string `json:"name"`
	// URL is the server's https address. It holds no user or password.
	URL string `json:"url"`
	// Credential is a reference to where the server's credential is kept
	// (env:, keychain: or op://). Empty, the server is reached without one.
	Credential string `json:"credential,omitempty"`
}

// Host is the server's host, as a run's tool_servers ceiling lists it.
func (t ToolServer) Host() string { return settings.HostOf(t.URL) }

// Context is one person's own context.
type Context struct {
	// Run is the person's own level of the run settings: ceilings that
	// tighten their runs, and preferences within them.
	Run settings.Layer `json:"run"`
	// ToolServers are the person's own MCP servers. Never nil.
	ToolServers []ToolServer `json:"tool_servers"`
	// Memory names the volume that holds the person's memory. The store
	// gives it on the first write, and it never changes.
	Memory string `json:"memory,omitempty"`
	// Version counts writes, from 1. Zero is a person who has written
	// nothing.
	Version   int       `json:"version"`
	UpdatedAt time.Time `json:"updated_at,omitzero"`
}

// Input is what a person writes: everything but what the store gives.
type Input struct {
	Run         settings.Layer `json:"run"`
	ToolServers []ToolServer   `json:"tool_servers"`
	// Version is the one the change was made from.
	Version int `json:"version"`
}

func empty() Context { return Context{ToolServers: []ToolServer{}} }

func (c Context) clone() Context {
	c.ToolServers = slices.Clone(c.ToolServers)
	if c.ToolServers == nil {
		c.ToolServers = []ToolServer{}
	}
	return c
}

var serverName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// Check reports whether an input can be kept.
func (in Input) Check() error {
	if err := in.Run.Check(); err != nil {
		return errors.Join(ErrInvalid, err)
	}
	if len(in.ToolServers) > MaxToolServers {
		return fmt.Errorf("%w: at most %d tool servers", ErrInvalid, MaxToolServers)
	}
	for i, t := range in.ToolServers {
		if !serverName.MatchString(t.Name) {
			return fmt.Errorf("%w: tool_servers[%d]: a name is lower case letters, digits and dashes, at most 63", ErrInvalid, i)
		}
		if slices.ContainsFunc(in.ToolServers[:i], func(o ToolServer) bool { return o.Name == t.Name }) {
			return fmt.Errorf("%w: tool server %q is named twice", ErrInvalid, t.Name)
		}
		if u, err := url.Parse(t.URL); len(t.URL) > MaxURL || err != nil || u.Scheme != "https" || u.User != nil || u.Host == "" || t.Host() == "" {
			return fmt.Errorf("%w: tool server %q: the address is an https URL with no user or password in it", ErrInvalid, t.Name)
		}
		if t.Credential != "" {
			if _, err := credref.Parse(t.Credential); err != nil {
				return fmt.Errorf("%w: tool server %q: %w", ErrInvalid, t.Name, err)
			}
		}
	}
	return nil
}

// Store holds every person's context.
type Store struct {
	mu   sync.RWMutex
	by   map[string]Context
	kept *state.Table[Context]
	// volume names a new memory volume. Tests replace it.
	volume func() string
}

// NewStore returns an empty store in memory.
func NewStore() *Store { return &Store{by: map[string]Context{}, volume: newVolume} }

// Open returns a store that keeps everything in st, holding what st already
// has. A nil st is NewStore.
func Open(st state.Store) (*Store, error) {
	s := NewStore()
	s.kept = state.NewTable[Context](st, "personctx")
	if err := s.kept.Load(func(tenant, subject string, c Context) error {
		s.by[key(tenant, subject)] = c.clone()
		return nil
	}); err != nil {
		return nil, fmt.Errorf("personctx: load: %w", err)
	}
	return s, nil
}

func key(tenant, subject string) string { return tenant + "\x00" + subject }

func newVolume() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return "vol-" + hex.EncodeToString(b)
}

// Get returns a person's context, empty when they have written none.
func (s *Store) Get(tenant, subject string) Context {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if c, ok := s.by[key(tenant, subject)]; ok {
		return c.clone()
	}
	return empty()
}

// Set replaces what a person chose. in.Version must be the current one
// (ErrVersion). A write that cannot be kept changes nothing.
func (s *Store) Set(tenant, subject string, in Input, at time.Time) (Context, error) {
	if tenant == "" || subject == "" || len(subject) > maxSubject {
		return Context{}, fmt.Errorf("%w: a person is required", ErrInvalid)
	}
	if err := in.Check(); err != nil {
		return Context{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key(tenant, subject)
	cur, existed := s.by[k]
	if !existed {
		cur = empty()
	}
	if in.Version != cur.Version {
		return Context{}, ErrVersion
	}
	next := Context{Run: in.Run, ToolServers: slices.Clone(in.ToolServers), Memory: cur.Memory, Version: cur.Version + 1, UpdatedAt: at.UTC()}
	if next.ToolServers == nil {
		next.ToolServers = []ToolServer{}
	}
	if next.Memory == "" {
		next.Memory = s.volume()
	}
	if err := s.kept.Put(tenant, subject, next); err != nil {
		return Context{}, fmt.Errorf("personctx: write: %w", err)
	}
	s.by[k] = next
	return next.clone(), nil
}
