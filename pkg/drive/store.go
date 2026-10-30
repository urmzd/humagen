package drive

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/urmzd/humagen/pkg/blob"
	"github.com/urmzd/humagen/pkg/state"
)

// The namespaces the store writes. Trees holds heads and open proposals and
// is read whole when the store opens; versions are read one at a time.
const (
	nsTrees    = "drive.trees"
	nsVersions = "drive.versions"
)

// Head, as a Commit's Base, applies the change to whatever the head is,
// without checking it against an earlier version.
const Head int64 = -1

// Screen decides whether content may be kept in a tree that is not a
// person's own. A non-nil error refuses it.
type Screen func(tree Tree, path string, data []byte) error

// Event reports a change to a tree, after it is written.
type Event struct {
	Tenant   string
	Tree     Tree
	Version  int64
	Origin   string
	Proposal string
}

// Store keeps every tenant's trees.
type Store struct {
	mu        sync.Mutex
	st        state.Store
	blobs     blob.Store
	heads     map[string]headRecord
	proposals map[string]map[string]Proposal
	cache     *lru
	screen    Screen
	now       func() time.Time
	watchMu   sync.RWMutex
	watchers  []func(Event)
	indexMu   sync.Mutex
	indexes   map[string]*index
}

type headRecord struct {
	Tree    Tree  `json:"tree"`
	Version int64 `json:"version"`
	Oldest  int64 `json:"oldest"`
}

// Option configures a Store.
type Option func(*Store)

// WithScreen screens what is written to any tree but a person's own.
func WithScreen(fn Screen) Option { return func(s *Store) { s.screen = fn } }

// WithClock sets the clock versions are stamped with when the caller gives no
// time.
func WithClock(now func() time.Time) Option { return func(s *Store) { s.now = now } }

// WithCache sets how many bytes of versions and content the store keeps in
// memory. The default is 64 MiB.
func WithCache(bytes int64) Option { return func(s *Store) { s.cache = newLRU(bytes) } }

// Open returns a store on st and blobs, holding the heads and proposals st
// already has.
func Open(st state.Store, blobs blob.Store, opts ...Option) (*Store, error) {
	if st == nil || blobs == nil {
		return nil, fmt.Errorf("%w: a state store and a blob store are required", ErrInvalid)
	}
	s := &Store{st: st, blobs: blobs, heads: map[string]headRecord{}, proposals: map[string]map[string]Proposal{}, cache: newLRU(64 << 20), now: time.Now, indexes: map[string]*index{}}
	for _, o := range opts {
		o(s)
	}
	ctx, cancel := context.WithTimeout(context.Background(), state.Timeout)
	defer cancel()
	recs, err := st.Load(ctx, nsTrees)
	if err != nil {
		return nil, fmt.Errorf("drive: load: %w", err)
	}
	for _, r := range recs {
		switch {
		case strings.HasSuffix(r.Key, "#h"):
			var h headRecord
			if err := json.Unmarshal(r.Value, &h); err != nil {
				return nil, fmt.Errorf("drive: load %q: %w", r.Key, err)
			}
			s.heads[tk(r.Tenant, h.Tree)] = h
		case strings.Contains(r.Key, "#p/"):
			var p Proposal
			if err := json.Unmarshal(r.Value, &p); err != nil {
				return nil, fmt.Errorf("drive: load %q: %w", r.Key, err)
			}
			k := tk(r.Tenant, p.Tree)
			if s.proposals[k] == nil {
				s.proposals[k] = map[string]Proposal{}
			}
			s.proposals[k][p.ID] = p
		}
	}
	return s, nil
}

// Watch calls fn after every change to a tree. It is called without the
// store's lock held, and must not block.
func (s *Store) Watch(fn func(Event)) {
	s.watchMu.Lock()
	defer s.watchMu.Unlock()
	s.watchers = append(s.watchers, fn)
}

func (s *Store) notify(e Event) {
	s.watchMu.RLock()
	defer s.watchMu.RUnlock()
	for _, fn := range s.watchers {
		fn(e)
	}
}

func tk(tenant string, t Tree) string { return tenant + "\x00" + t.Key() }

// Every key starts with its tree's key, the owner, which is the shard key a
// distributed store places it by (RFC 0004, layer 9). "#" never appears in a
// tree's key, whose parts are escaped, so it separates the two.
func versionKey(t Tree, n int64) string { return t.Key() + "#v/" + state.Seq(n) }

func headKey(t Tree) string { return t.Key() + "#h" }

func proposalKey(t Tree, id string) string { return t.Key() + "#p/" + id }

func blobKey(hash string) string { return "drive/sha256/" + hash[:2] + "/" + hash }

func hashOf(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// blobTenant is the tenant's directory in the blob store: a tenant name is
// not always a safe path segment, and the store refuses a "/" in one.
func blobTenant(tenant string) string {
	h := sha256.Sum256([]byte(tenant))
	return "t" + hex.EncodeToString(h[:16])
}

func (s *Store) check(who Actor, tree Tree, need func(Grant) bool) error {
	if err := tree.Valid(); err != nil {
		return err
	}
	if !need(who.grant(tree)) {
		// A tree the actor may not read does not exist for them.
		if !who.grant(tree).Read {
			return fmt.Errorf("%w: %s", ErrNotFound, tree.Key())
		}
		return fmt.Errorf("%w: %s", ErrForbidden, tree.Key())
	}
	return nil
}

// Trees returns every tree a tenant holds, by key.
func (s *Store) Trees(tenant string) []Tree {
	s.mu.Lock()
	defer s.mu.Unlock()
	seen := map[string]Tree{}
	prefix := tenant + "\x00"
	for k, h := range s.heads {
		if strings.HasPrefix(k, prefix) {
			seen[h.Tree.Key()] = h.Tree
		}
	}
	for k, ps := range s.proposals {
		if strings.HasPrefix(k, prefix) {
			for _, p := range ps {
				seen[p.Tree.Key()] = p.Tree
			}
		}
	}
	out := make([]Tree, 0, len(seen))
	for _, t := range seen {
		out = append(out, t)
	}
	slices.SortFunc(out, func(a, b Tree) int { return strings.Compare(a.Key(), b.Key()) })
	return out
}

// Tenants returns every tenant that holds a tree.
func (s *Store) Tenants() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	seen := map[string]bool{}
	for k := range s.heads {
		seen[strings.SplitN(k, "\x00", 2)[0]] = true
	}
	for k := range s.proposals {
		seen[strings.SplitN(k, "\x00", 2)[0]] = true
	}
	out := make([]string, 0, len(seen))
	for t := range seen {
		out = append(out, t)
	}
	slices.Sort(out)
	return out
}

// version reads one version, from the cache when it is there. Version 0 is
// the empty tree.
func (s *Store) version(ctx context.Context, tenant string, tree Tree, n int64) (Version, error) {
	if n == 0 {
		return Version{Tree: tree, Entries: []Entry{}}, nil
	}
	ck := "v\x00" + tk(tenant, tree) + "\x00" + state.Seq(n)
	if v, ok := s.cache.get(ck); ok {
		return v.(Version), nil
	}
	raw, err := s.st.Get(ctx, nsVersions, tenant, versionKey(tree, n))
	if errors.Is(err, state.ErrNotFound) {
		return Version{}, fmt.Errorf("%w: version %d of %s", ErrNotFound, n, tree.Key())
	}
	if err != nil {
		return Version{}, fmt.Errorf("drive: read version: %w", err)
	}
	var v Version
	if err := json.Unmarshal(raw, &v); err != nil {
		return Version{}, fmt.Errorf("drive: read version: %w", err)
	}
	s.cache.put(ck, v, int64(256+96*len(v.Entries)))
	return v, nil
}

func (s *Store) headOf(tenant string, tree Tree) headRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.heads[tk(tenant, tree)]
}

// Head returns a tree's latest version. A tree never written is version 0,
// empty.
func (s *Store) Head(ctx context.Context, tenant string, who Actor, tree Tree) (Version, error) {
	if err := s.check(who, tree, func(g Grant) bool { return g.Read }); err != nil {
		return Version{}, err
	}
	return s.version(ctx, tenant, tree, s.headOf(tenant, tree).Version)
}

// Version returns one version of a tree.
func (s *Store) Version(ctx context.Context, tenant string, who Actor, tree Tree, n int64) (Version, error) {
	if err := s.check(who, tree, func(g Grant) bool { return g.Read }); err != nil {
		return Version{}, err
	}
	h := s.headOf(tenant, tree)
	if n < 0 || n > h.Version || n != 0 && n < h.Oldest {
		return Version{}, fmt.Errorf("%w: version %d of %s", ErrNotFound, n, tree.Key())
	}
	return s.version(ctx, tenant, tree, n)
}

// History returns at most limit versions older than before, newest first.
// Before 0 starts at the head. Entries are left out.
func (s *Store) History(ctx context.Context, tenant string, who Actor, tree Tree, before int64, limit int) ([]Version, error) {
	if err := s.check(who, tree, func(g Grant) bool { return g.Read }); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > MaxPage {
		limit = MaxPage
	}
	h := s.headOf(tenant, tree)
	n := h.Version
	if before > 0 && before-1 < n {
		n = before - 1
	}
	out := []Version{}
	for ; n >= max(h.Oldest, 1) && len(out) < limit; n-- {
		v, err := s.version(ctx, tenant, tree, n)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		v.Entries = nil
		out = append(out, v)
	}
	return out, nil
}

// ReadFile returns a file's content at a version, 0 for the head.
func (s *Store) ReadFile(ctx context.Context, tenant string, who Actor, tree Tree, version int64, p string) ([]byte, Entry, error) {
	p, err := CleanPath(p)
	if err != nil {
		return nil, Entry{}, err
	}
	var v Version
	if version == 0 {
		v, err = s.Head(ctx, tenant, who, tree)
	} else {
		v, err = s.Version(ctx, tenant, who, tree, version)
	}
	if err != nil {
		return nil, Entry{}, err
	}
	e, ok := v.Lookup(p)
	if !ok {
		return nil, Entry{}, fmt.Errorf("%w: %s", ErrNotFound, p)
	}
	b, err := s.Content(ctx, tenant, e.Hash)
	return b, e, err
}

// Content returns stored bytes by hash, checked against it. It is for a
// caller that already read the hash from a manifest the actor may read.
func (s *Store) Content(ctx context.Context, tenant, hash string) ([]byte, error) {
	ck := "b\x00" + tenant + "\x00" + hash
	if b, ok := s.cache.get(ck); ok {
		return bytes.Clone(b.([]byte)), nil
	}
	if len(hash) != 64 {
		return nil, fmt.Errorf("%w: hash %q", ErrInvalid, hash)
	}
	rc, _, err := s.blobs.Get(ctx, blobTenant(tenant), blobKey(hash))
	if errors.Is(err, blob.ErrNotFound) {
		return nil, fmt.Errorf("%w: content %s", ErrNotFound, hash)
	}
	if err != nil {
		return nil, fmt.Errorf("drive: read content: %w", err)
	}
	defer rc.Close()
	b, err := io.ReadAll(io.LimitReader(rc, MaxFileBytes+1))
	if err != nil {
		return nil, fmt.Errorf("drive: read content: %w", err)
	}
	if hashOf(b) != hash {
		return nil, fmt.Errorf("drive: content %s does not match its hash", hash)
	}
	s.cache.put(ck, bytes.Clone(b), int64(len(b)+64))
	return b, nil
}

// upload stores what changes write, once per hash, and fills in each
// change's hash and size.
func (s *Store) upload(ctx context.Context, tenant string, tree Tree, changes []Change) ([]Change, error) {
	if len(changes) == 0 || len(changes) > MaxEntries {
		return nil, fmt.Errorf("%w: a change writes 1 to %d files", ErrInvalid, MaxEntries)
	}
	out := make([]Change, 0, len(changes))
	seen := map[string]bool{}
	for _, c := range changes {
		p, err := CleanPath(c.Path)
		if err != nil {
			return nil, err
		}
		if seen[p] {
			return nil, fmt.Errorf("%w: %s is changed twice", ErrInvalid, p)
		}
		seen[p] = true
		if c.Delete {
			out = append(out, Change{Path: p, Delete: true})
			continue
		}
		if len(c.Data) > MaxFileBytes {
			return nil, fmt.Errorf("%w: %s is over %d bytes", ErrTooLarge, p, MaxFileBytes)
		}
		if tree.Kind != PersonKind && s.screen != nil {
			if err := s.screen(tree, p, c.Data); err != nil {
				return nil, fmt.Errorf("%w: %s: %v", ErrRefused, p, err)
			}
		}
		h := hashOf(c.Data)
		if err := s.putContent(ctx, tenant, h, c.Data); err != nil {
			return nil, err
		}
		out = append(out, Change{Path: p, Hash: h, Size: int64(len(c.Data))})
	}
	return out, nil
}

func (s *Store) putContent(ctx context.Context, tenant, h string, data []byte) error {
	ck := "b\x00" + tenant + "\x00" + h
	if _, ok := s.cache.get(ck); ok {
		return nil
	}
	if _, err := s.blobs.Stat(ctx, blobTenant(tenant), blobKey(h)); err == nil {
		s.cache.put(ck, bytes.Clone(data), int64(len(data)+64))
		return nil
	}
	info, err := s.blobs.Put(ctx, blobTenant(tenant), blobKey(h), bytes.NewReader(data), MaxFileBytes)
	if err != nil {
		return fmt.Errorf("drive: write content: %w", err)
	}
	if info.SHA256 != "" && info.SHA256 != h {
		return fmt.Errorf("drive: the blob store kept different bytes for %s", h)
	}
	s.cache.put(ck, bytes.Clone(data), int64(len(data)+64))
	return nil
}

// CommitInput is a change to a tree's head.
type CommitInput struct {
	// Base is the version the changes were made against. A path changed
	// since then is a conflict. Head skips the check.
	Base     int64
	Changes  []Change
	By       string
	At       time.Time
	Message  string
	Origin   string
	Resolves []string
}

// Commit writes changes as a new version. Changes that leave the tree as it
// is write nothing and return the head.
func (s *Store) Commit(ctx context.Context, tenant string, who Actor, tree Tree, in CommitInput) (Version, error) {
	if err := s.check(who, tree, func(g Grant) bool { return g.Write }); err != nil {
		return Version{}, err
	}
	changes, err := s.upload(ctx, tenant, tree, in.Changes)
	if err != nil {
		return Version{}, err
	}
	for _, id := range in.Resolves {
		if _, ok := s.proposal(tenant, tree, id); !ok {
			return Version{}, fmt.Errorf("%w: proposal %q", ErrNotFound, id)
		}
	}
	return s.apply(ctx, tenant, tree, in, changes, 0)
}

func (s *Store) proposal(tenant string, tree Tree, id string) (Proposal, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.proposals[tk(tenant, tree)][id]
	return p, ok
}

// apply writes a new version from the head and changes, or from a restored
// version's entries when restored is set.
func (s *Store) apply(ctx context.Context, tenant string, tree Tree, in CommitInput, changes []Change, restored int64) (Version, error) {
	s.mu.Lock()
	k := tk(tenant, tree)
	h := s.heads[k]
	cur, err := s.version(ctx, tenant, tree, h.Version)
	if err != nil {
		s.mu.Unlock()
		return Version{}, err
	}
	var entries []Entry
	if restored > 0 {
		old, err := s.version(ctx, tenant, tree, restored)
		if err != nil {
			s.mu.Unlock()
			return Version{}, err
		}
		entries = slices.Clone(old.Entries)
	} else {
		if in.Base != Head && in.Base != h.Version {
			base, err := s.version(ctx, tenant, tree, in.Base)
			if err != nil {
				s.mu.Unlock()
				return Version{}, fmt.Errorf("%w: base version %d", ErrConflict, in.Base)
			}
			for _, c := range changes {
				was, _ := base.Lookup(c.Path)
				now, _ := cur.Lookup(c.Path)
				if was != now {
					s.mu.Unlock()
					return Version{}, fmt.Errorf("%w: %s", ErrConflict, c.Path)
				}
			}
		}
		entries = applyChanges(cur.Entries, changes)
	}
	if len(entries) > MaxEntries {
		s.mu.Unlock()
		return Version{}, fmt.Errorf("%w: a tree holds at most %d files", ErrTooLarge, MaxEntries)
	}
	if slices.Equal(entries, cur.Entries) && len(in.Resolves) == 0 {
		s.mu.Unlock()
		return cur, nil
	}
	at := in.At
	if at.IsZero() {
		at = s.now()
	}
	v := Version{Tree: tree, N: h.Version + 1, Parent: h.Version, Entries: entries, By: in.By, At: at.UTC(), Message: in.Message, Origin: in.Origin, Restored: restored, Resolves: in.Resolves}
	if slices.Equal(entries, cur.Entries) {
		// Only proposals are closed: no version is written.
		v = cur
	} else {
		raw, _ := json.Marshal(v)
		if err := s.st.Write(ctx, nsVersions, state.Put(tenant, versionKey(tree, v.N), raw)); err != nil {
			s.mu.Unlock()
			return Version{}, fmt.Errorf("drive: write version: %w", err)
		}
	}
	next := headRecord{Tree: tree, Version: v.N, Oldest: max(h.Oldest, 1)}
	var prune []state.Op
	for next.Version-next.Oldest+1 > MaxVersions {
		prune = append(prune, state.Delete(tenant, versionKey(tree, next.Oldest)))
		next.Oldest++
	}
	rawHead, _ := json.Marshal(next)
	ops := []state.Op{state.Put(tenant, headKey(tree), rawHead)}
	for _, id := range in.Resolves {
		ops = append(ops, state.Delete(tenant, proposalKey(tree, id)))
	}
	if err := s.st.Write(ctx, nsTrees, ops...); err != nil {
		s.mu.Unlock()
		return Version{}, fmt.Errorf("drive: write head: %w", err)
	}
	s.heads[k] = next
	for _, id := range in.Resolves {
		delete(s.proposals[k], id)
	}
	if len(s.proposals[k]) == 0 {
		delete(s.proposals, k)
	}
	if v.N != cur.N || cur.N == 0 {
		s.cache.put("v\x00"+k+"\x00"+state.Seq(v.N), v, int64(256+96*len(v.Entries)))
	}
	s.mu.Unlock()
	if len(prune) > 0 {
		// Best effort: a version left behind is unreachable, not wrong.
		_ = s.st.Write(ctx, nsVersions, prune...)
	}
	s.notify(Event{Tenant: tenant, Tree: tree, Version: v.N, Origin: in.Origin})
	return v, nil
}

func applyChanges(entries []Entry, changes []Change) []Entry {
	byPath := map[string]Entry{}
	for _, e := range entries {
		byPath[e.Path] = e
	}
	for _, c := range changes {
		if c.Delete {
			delete(byPath, c.Path)
			continue
		}
		byPath[c.Path] = Entry{Path: c.Path, Hash: c.Hash, Size: c.Size}
	}
	out := make([]Entry, 0, len(byPath))
	for _, e := range byPath {
		out = append(out, e)
	}
	slices.SortFunc(out, func(a, b Entry) int { return strings.Compare(a.Path, b.Path) })
	return out
}

// ProposeInput is a change that waits for approval.
type ProposeInput struct {
	// ID names the proposal. Proposing again under the same ID replaces it.
	// Empty draws a new one.
	ID      string
	Base    int64
	Changes []Change
	By      string
	At      time.Time
	Message string
	Origin  string
}

// Propose holds a change for someone with Approve.
func (s *Store) Propose(ctx context.Context, tenant string, who Actor, tree Tree, in ProposeInput) (Proposal, error) {
	if err := s.check(who, tree, func(g Grant) bool { return g.Propose }); err != nil {
		return Proposal{}, err
	}
	if in.ID == "" {
		in.ID = "p_" + rand.Text()
	}
	if len(in.ID) > 64 || strings.ContainsAny(in.ID, "/\x00") {
		return Proposal{}, fmt.Errorf("%w: proposal id %q", ErrInvalid, in.ID)
	}
	changes, err := s.upload(ctx, tenant, tree, in.Changes)
	if err != nil {
		return Proposal{}, err
	}
	at := in.At
	if at.IsZero() {
		at = s.now()
	}
	s.mu.Lock()
	k := tk(tenant, tree)
	if in.Base == Head {
		in.Base = s.heads[k].Version
	}
	p := Proposal{ID: in.ID, Tree: tree, Base: in.Base, Changes: changes, By: in.By, At: at.UTC(), Message: in.Message, Origin: in.Origin}
	if _, ok := s.proposals[k][p.ID]; !ok && len(s.proposals[k]) >= MaxProposals {
		s.mu.Unlock()
		return Proposal{}, fmt.Errorf("%w: a tree holds at most %d open proposals", ErrTooLarge, MaxProposals)
	}
	raw, _ := json.Marshal(p)
	if err := s.st.Write(ctx, nsTrees, state.Put(tenant, proposalKey(tree, p.ID), raw)); err != nil {
		s.mu.Unlock()
		return Proposal{}, fmt.Errorf("drive: write proposal: %w", err)
	}
	if s.proposals[k] == nil {
		s.proposals[k] = map[string]Proposal{}
	}
	s.proposals[k][p.ID] = p
	s.mu.Unlock()
	s.notify(Event{Tenant: tenant, Tree: tree, Origin: in.Origin, Proposal: p.ID})
	return p, nil
}

// Proposals returns a tree's open proposals, oldest first.
func (s *Store) Proposals(tenant string, who Actor, tree Tree) ([]Proposal, error) {
	if err := s.check(who, tree, func(g Grant) bool { return g.Read }); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Proposal, 0, len(s.proposals[tk(tenant, tree)]))
	for _, p := range s.proposals[tk(tenant, tree)] {
		out = append(out, p)
	}
	slices.SortFunc(out, func(a, b Proposal) int {
		if c := a.At.Compare(b.At); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
	return out, nil
}

// Approve applies a proposal to the head, checked against the version it was
// proposed on, and closes it.
func (s *Store) Approve(ctx context.Context, tenant string, who Actor, tree Tree, id, by string, at time.Time) (Version, error) {
	if err := s.check(who, tree, func(g Grant) bool { return g.Approve }); err != nil {
		return Version{}, err
	}
	p, ok := s.proposal(tenant, tree, id)
	if !ok {
		return Version{}, fmt.Errorf("%w: proposal %q", ErrNotFound, id)
	}
	return s.apply(ctx, tenant, tree, CommitInput{Base: p.Base, By: by, At: at, Message: p.Message, Origin: p.Origin, Resolves: []string{id}}, p.Changes, 0)
}

// Discard closes a proposal without applying it.
// Origin is reported to watchers.
func (s *Store) Discard(ctx context.Context, tenant string, who Actor, tree Tree, id, origin string) error {
	if err := s.check(who, tree, func(g Grant) bool { return g.Approve }); err != nil {
		return err
	}
	s.mu.Lock()
	k := tk(tenant, tree)
	if _, ok := s.proposals[k][id]; !ok {
		s.mu.Unlock()
		return fmt.Errorf("%w: proposal %q", ErrNotFound, id)
	}
	if err := s.st.Write(ctx, nsTrees, state.Delete(tenant, proposalKey(tree, id))); err != nil {
		s.mu.Unlock()
		return fmt.Errorf("drive: discard: %w", err)
	}
	delete(s.proposals[k], id)
	if len(s.proposals[k]) == 0 {
		delete(s.proposals, k)
	}
	s.mu.Unlock()
	s.notify(Event{Tenant: tenant, Tree: tree, Proposal: id, Origin: origin})
	return nil
}

// Restore checks out an earlier version as a new one. The history is kept.
// A person restores their own tree; any other takes Approve.
func (s *Store) Restore(ctx context.Context, tenant string, who Actor, tree Tree, version int64, by string, at time.Time) (Version, error) {
	if err := s.check(who, tree, func(g Grant) bool { return g.Approve }); err != nil {
		return Version{}, err
	}
	h := s.headOf(tenant, tree)
	if version <= 0 || version > h.Version || version < h.Oldest {
		return Version{}, fmt.Errorf("%w: version %d of %s", ErrNotFound, version, tree.Key())
	}
	return s.apply(ctx, tenant, tree, CommitInput{By: by, At: at, Message: fmt.Sprintf("restore version %d", version), Origin: "restore"}, nil, version)
}

// Drop forgets a tree: its head, its proposals and its versions. The content
// stays in the blob store, where other trees may share it. Origin is reported
// to watchers.
func (s *Store) Drop(ctx context.Context, tenant string, tree Tree, origin string) error {
	if err := tree.Valid(); err != nil {
		return err
	}
	s.mu.Lock()
	k := tk(tenant, tree)
	h, held := s.heads[k]
	ops := []state.Op{}
	if held {
		ops = append(ops, state.Delete(tenant, headKey(tree)))
	}
	for id := range s.proposals[k] {
		ops = append(ops, state.Delete(tenant, proposalKey(tree, id)))
	}
	if len(ops) == 0 {
		s.mu.Unlock()
		return nil
	}
	if err := s.st.Write(ctx, nsTrees, ops...); err != nil {
		s.mu.Unlock()
		return fmt.Errorf("drive: drop: %w", err)
	}
	delete(s.heads, k)
	delete(s.proposals, k)
	s.mu.Unlock()
	if held {
		var prune []state.Op
		for n := max(h.Oldest, 1); n <= h.Version; n++ {
			prune = append(prune, state.Delete(tenant, versionKey(tree, n)))
		}
		_ = s.st.Write(ctx, nsVersions, prune...)
	}
	s.notify(Event{Tenant: tenant, Tree: tree, Origin: origin})
	return nil
}
