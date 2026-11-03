// Package scratchpads keeps a conversation's named scratchpads: short notes
// people write beside a conversation, which its agents read.
//
// It reads and writes the database per request (pkg/state, per request),
// with the tenant as the shard key, and holds nothing in memory, so any
// number of processes on one database agree on every scratchpad.
package scratchpads

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/urmzd/humagen/pkg/state"
)

// Bounds on a conversation's scratchpads.
const (
	// MaxPerThread is the most scratchpads one conversation holds.
	MaxPerThread = 50
	// MaxName is the longest name, in characters.
	MaxName = 80
	// MaxContent is the most bytes one scratchpad holds.
	MaxContent = 256 << 10
)

var (
	// ErrNotFound reports a scratchpad that is not there.
	ErrNotFound = errors.New("scratchpads: not found")
	// ErrInvalid reports a name or content outside the bounds.
	ErrInvalid = errors.New("scratchpads: invalid")
	// ErrConflict reports a change made from a version that has moved on,
	// or a name another scratchpad of the conversation has.
	ErrConflict = errors.New("scratchpads: conflict")
)

// namespace is where scratchpads are kept. Within a tenant, a scratchpad is
// at "<thread>/<folded name>", so two scratchpads of a conversation never
// share a name and a rename is one atomic write.
const namespace = "scratchpads"

// Pad is one scratchpad. A listing leaves Content out and keeps its Size.
type Pad struct {
	ID        string    `json:"id"`
	Thread    string    `json:"thread"`
	Name      string    `json:"name"`
	Content   string    `json:"content"`
	Size      int       `json:"size"`
	Version   int64     `json:"version"`
	CreatedBy string    `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedBy string    `json:"updated_by"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Store keeps scratchpads.
type Store struct{ st state.Store }

// Open returns a store on st, which may be shared with other processes. A
// nil st keeps scratchpads in this process's memory.
func Open(st state.Store) *Store {
	if st == nil {
		st = state.NewMemory()
	}
	return &Store{st: st}
}

// CleanName trims a name and checks it: 1 to MaxName printable characters,
// no slash.
func CleanName(name string) (string, error) {
	name = strings.TrimSpace(name)
	n := utf8.RuneCountInString(name)
	if n == 0 || n > MaxName || !utf8.ValidString(name) || strings.ContainsAny(name, "/\\") ||
		strings.ContainsFunc(name, func(r rune) bool { return !unicode.IsPrint(r) }) {
		return "", fmt.Errorf("%w: a name is 1 to %d printable characters without a slash", ErrInvalid, MaxName)
	}
	return name, nil
}

func checkContent(content string) error {
	if len(content) > MaxContent || !utf8.ValidString(content) || strings.ContainsRune(content, 0) {
		return fmt.Errorf("%w: content is UTF-8 text of at most %d bytes", ErrInvalid, MaxContent)
	}
	return nil
}

func key(thread, name string) string { return thread + "/" + strings.ToLower(name) }

func decode(r state.Record, withContent bool) (Pad, error) {
	var p Pad
	if err := json.Unmarshal(r.Value, &p); err != nil {
		return Pad{}, fmt.Errorf("scratchpads: read %q: %w", r.Key, err)
	}
	p.Version, p.Size = r.Version, len(p.Content)
	if !withContent {
		p.Content = ""
	}
	return p, nil
}

func conflict(err error) error {
	if errors.Is(err, state.ErrConflict) {
		return fmt.Errorf("%w: the scratchpad changed, or that name is taken; refresh and try again", ErrConflict)
	}
	return err
}

// records reads a conversation's scratchpads, at most one past MaxPerThread.
func (s *Store) records(ctx context.Context, tenant, thread string) ([]state.Record, error) {
	if thread == "" || strings.Contains(thread, "/") {
		return nil, fmt.Errorf("%w: a conversation is required", ErrInvalid)
	}
	page, _, err := s.st.List(ctx, namespace, tenant, state.Query{Prefix: thread + "/", Limit: MaxPerThread + 1})
	return page, err
}

func (s *Store) find(ctx context.Context, tenant, thread, id string) (state.Record, Pad, error) {
	recs, err := s.records(ctx, tenant, thread)
	if err != nil {
		return state.Record{}, Pad{}, err
	}
	for _, r := range recs {
		p, err := decode(r, true)
		if err != nil {
			return state.Record{}, Pad{}, err
		}
		if p.ID == id {
			return r, p, nil
		}
	}
	return state.Record{}, Pad{}, ErrNotFound
}

// List returns a conversation's scratchpads by name, without their content.
func (s *Store) List(ctx context.Context, tenant, thread string) ([]Pad, error) {
	recs, err := s.records(ctx, tenant, thread)
	if err != nil {
		return nil, err
	}
	out := make([]Pad, 0, len(recs))
	for _, r := range recs[:min(len(recs), MaxPerThread)] {
		p, err := decode(r, false)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	slices.SortFunc(out, func(a, b Pad) int { return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)) })
	return out, nil
}

// Get returns one scratchpad with its content.
func (s *Store) Get(ctx context.Context, tenant, thread, id string) (Pad, error) {
	_, p, err := s.find(ctx, tenant, thread, id)
	return p, err
}

// ByName returns the scratchpad a conversation keeps under a name, any case.
func (s *Store) ByName(ctx context.Context, tenant, thread, name string) (Pad, error) {
	name, err := CleanName(name)
	if err != nil {
		return Pad{}, ErrNotFound
	}
	r, err := s.st.Lookup(ctx, namespace, tenant, key(thread, name))
	if errors.Is(err, state.ErrNotFound) {
		return Pad{}, ErrNotFound
	}
	if err != nil {
		return Pad{}, err
	}
	return decode(r, true)
}

// Create adds a scratchpad. A conversation holds at most MaxPerThread.
func (s *Store) Create(ctx context.Context, tenant, thread, name, content, by string, at time.Time) (Pad, error) {
	name, err := CleanName(name)
	if err != nil {
		return Pad{}, err
	}
	if err := checkContent(content); err != nil {
		return Pad{}, err
	}
	recs, err := s.records(ctx, tenant, thread)
	if err != nil {
		return Pad{}, err
	}
	if len(recs) >= MaxPerThread {
		return Pad{}, fmt.Errorf("%w: a conversation holds at most %d scratchpads", ErrInvalid, MaxPerThread)
	}
	id := make([]byte, 8)
	_, _ = rand.Read(id)
	p := Pad{ID: "sp-" + hex.EncodeToString(id), Thread: thread, Name: name, Content: content, CreatedBy: by, CreatedAt: at.UTC(), UpdatedBy: by, UpdatedAt: at.UTC()}
	b, _ := json.Marshal(p)
	if err := s.st.Write(ctx, namespace, state.Create(tenant, key(thread, name), b)); err != nil {
		return Pad{}, conflict(err)
	}
	p.Version, p.Size = 1, len(content)
	return p, nil
}

// Update renames a scratchpad, replaces its content, or both, from the
// version that was read.
func (s *Store) Update(ctx context.Context, tenant, thread, id, name, content string, version int64, by string, at time.Time) (Pad, error) {
	name, err := CleanName(name)
	if err != nil {
		return Pad{}, err
	}
	if err := checkContent(content); err != nil {
		return Pad{}, err
	}
	r, p, err := s.find(ctx, tenant, thread, id)
	if err != nil {
		return Pad{}, err
	}
	if version != r.Version {
		return Pad{}, conflict(state.ErrConflict)
	}
	p.Name, p.Content, p.UpdatedBy, p.UpdatedAt = name, content, by, at.UTC()
	b, _ := json.Marshal(p)
	next := key(thread, name)
	ops := []state.Op{state.PutAt(tenant, r.Key, b, r.Version)}
	if next != r.Key {
		ops = []state.Op{state.Create(tenant, next, b), state.DeleteAt(tenant, r.Key, r.Version)}
	}
	if err := s.st.Write(ctx, namespace, ops...); err != nil {
		return Pad{}, conflict(err)
	}
	p.Version, p.Size = r.Version+1, len(content)
	if next != r.Key {
		p.Version = 1
	}
	return p, nil
}

// Delete removes a scratchpad at the version that was read.
func (s *Store) Delete(ctx context.Context, tenant, thread, id string, version int64) error {
	r, _, err := s.find(ctx, tenant, thread, id)
	if err != nil {
		return err
	}
	if version != r.Version {
		return conflict(state.ErrConflict)
	}
	return conflict(s.st.Write(ctx, namespace, state.DeleteAt(tenant, r.Key, r.Version)))
}

// DeleteThread removes every scratchpad of a conversation, for its deletion.
func (s *Store) DeleteThread(ctx context.Context, tenant, thread string) error {
	recs, err := s.records(ctx, tenant, thread)
	if err != nil {
		return err
	}
	ops := make([]state.Op, 0, len(recs))
	for _, r := range recs {
		ops = append(ops, state.Delete(tenant, r.Key))
	}
	if len(ops) == 0 {
		return nil
	}
	return s.st.Write(ctx, namespace, ops...)
}
