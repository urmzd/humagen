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

// The namespaces the store writes. The tenant is the shard key of every
// record, and every key starts with its tree's key, the owner:
//
//	drive.trees     <tree>#h          the head: its version, the oldest kept,
//	                                  its generation and its open proposals
//	drive.trees     <tree>#v/<seq>    a version, written with the head
//	drive.trees     <tree>#p/<id>     an open proposal, written with the head
//	drive.dir       <tree>            the tree, so a tenant's trees can be paged
//	drive.versions  <tree>#v/<seq>    a version written before versions were
//	                                  kept beside their head; read, never written
//
// Every change to a tree writes its head at the version it read, in the same
// write as the version and the proposals it adds or removes, so two
// processes changing one tree cannot both win from the same head.
const (
	nsTrees    = "drive.trees"
	nsVersions = "drive.versions"
	nsDir      = "drive.dir"
)

// listed marks, in the tenant "" of drive.dir, that every tree written before
// the directory existed is in it.
const listed = "listed:v1"

// Head, as a Commit's Base, applies the change to whatever the head is,
// without checking it against an earlier version.
const Head int64 = -1

// MaxTrees is the most trees one page of a tenant's trees holds.
const MaxTrees = state.MaxList

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

// Store keeps every tenant's trees. It holds no tree in memory: every call
// reads the head it needs, and the cache holds only what never changes once
// written (a version of one generation of a tree, content by hash, and the
// search index built from a version).
type Store struct {
	st       state.Store
	blobs    blob.Store
	cache    *lru
	screen   Screen
	now      func() time.Time
	watchMu  sync.RWMutex
	watchers []func(Event)
	indexMu  sync.Mutex
	indexes  map[string]*index
}

// headRecord is a tree's head. A tree with no head record is version 0,
// empty, with no proposals.
type headRecord struct {
	Tree    Tree  `json:"tree"`
	Version int64 `json:"version"`
	Oldest  int64 `json:"oldest"`
	// Gen names this life of the tree: a tree dropped and written again
	// starts its versions at 1 under a new one, so a cached version is
	// never taken for another life's. A head written before generations
	// has "".
	Gen string `json:"gen,omitempty"`
	// Open are the ids of the tree's open proposals, once Indexed. A head
	// written before it kept them is read with its proposals listed.
	Open    []string `json:"open,omitempty"`
	Indexed bool     `json:"indexed,omitempty"`
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

// Open returns a store on st and blobs. It reads nothing but, the first time
// any process opens the store, the heads and proposals written before the
// directory of trees existed, to list them in it; a process that opens it at
// the same time lists the same trees.
func Open(st state.Store, blobs blob.Store, opts ...Option) (*Store, error) {
	if st == nil || blobs == nil {
		return nil, fmt.Errorf("%w: a state store and a blob store are required", ErrInvalid)
	}
	s := &Store{st: st, blobs: blobs, cache: newLRU(64 << 20), now: time.Now, indexes: map[string]*index{}}
	for _, o := range opts {
		o(s)
	}
	if err := s.list(); err != nil {
		return nil, fmt.Errorf("drive: list trees: %w", err)
	}
	return s, nil
}

// list fills the directory of trees from the heads and proposals written
// before it existed, once.
func (s *Store) list() error {
	ctx, cancel := context.WithTimeout(context.Background(), state.Timeout)
	defer cancel()
	if _, err := s.st.Lookup(ctx, nsDir, "", listed); err == nil {
		return nil
	} else if !errors.Is(err, state.ErrNotFound) {
		return err
	}
	recs, err := s.st.Load(ctx, nsTrees)
	if err != nil {
		return err
	}
	var ops []state.Op
	seen := map[string]bool{}
	for _, r := range recs {
		var t Tree
		switch {
		case strings.HasSuffix(r.Key, "#h"):
			var h headRecord
			if err := json.Unmarshal(r.Value, &h); err != nil {
				return fmt.Errorf("%q: %w", r.Key, err)
			}
			t = h.Tree
		case strings.Contains(r.Key, "#p/"):
			var p Proposal
			if err := json.Unmarshal(r.Value, &p); err != nil {
				return fmt.Errorf("%q: %w", r.Key, err)
			}
			t = p.Tree
		default:
			continue
		}
		if r.Tenant == "" || seen[tk(r.Tenant, t)] {
			continue
		}
		seen[tk(r.Tenant, t)] = true
		raw, _ := json.Marshal(t)
		ops = append(ops, state.Put(r.Tenant, t.Key(), raw))
	}
	ops = append(ops, state.Put("", listed, []byte{}))
	for len(ops) > 0 {
		n := min(len(ops), state.MaxList)
		if err := s.st.Write(ctx, nsDir, ops[:n]...); err != nil {
			return err
		}
		ops = ops[n:]
	}
	return nil
}

// Watch calls fn after every change to a tree made through this store, in
// this process. It is called without any lock held, and must not block.
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

func proposalPrefix(t Tree) string { return t.Key() + "#p/" }

func proposalKey(t Tree, id string) string { return proposalPrefix(t) + id }

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

// Trees returns one page of the trees a tenant holds, by key, after a
// cursor, and the cursor of the next page, "" when none follow. A tree that
// was dropped may still be listed, and reads as an empty tree.
func (s *Store) Trees(ctx context.Context, tenant, after string, limit int) ([]Tree, string, error) {
	if tenant == "" {
		return nil, "", nil
	}
	if limit <= 0 || limit > MaxTrees {
		limit = MaxTrees
	}
	recs, next, err := s.st.List(ctx, nsDir, tenant, state.Query{After: after, Limit: limit})
	if err != nil {
		return nil, "", fmt.Errorf("drive: list trees: %w", err)
	}
	out := make([]Tree, 0, len(recs))
	for _, r := range recs {
		var t Tree
		if err := json.Unmarshal(r.Value, &t); err != nil {
			return nil, "", fmt.Errorf("drive: list trees %q: %w", r.Key, err)
		}
		out = append(out, t)
	}
	return out, next, nil
}

// head reads a tree's head and the version of its record, state.Missing
// when it has none.
func (s *Store) head(ctx context.Context, tenant string, tree Tree) (headRecord, int64, error) {
	r, err := s.st.Lookup(ctx, nsTrees, tenant, headKey(tree))
	if errors.Is(err, state.ErrNotFound) {
		return headRecord{Tree: tree}, state.Missing, nil
	}
	if err != nil {
		return headRecord{}, 0, fmt.Errorf("drive: read head: %w", err)
	}
	var h headRecord
	if err := json.Unmarshal(r.Value, &h); err != nil {
		return headRecord{}, 0, fmt.Errorf("drive: read head: %w", err)
	}
	return h, r.Version, nil
}

// version reads one version of a head's generation, from the cache when it
// is there. Version 0 is the empty tree.
func (s *Store) version(ctx context.Context, tenant string, h headRecord, n int64) (Version, error) {
	tree := h.Tree
	if n == 0 {
		return Version{Tree: tree, Entries: []Entry{}}, nil
	}
	ck := "v\x00" + tk(tenant, tree) + "\x00" + h.Gen + "\x00" + state.Seq(n)
	if v, ok := s.cache.get(ck); ok {
		return v.(Version), nil
	}
	raw, err := s.st.Get(ctx, nsTrees, tenant, versionKey(tree, n))
	if errors.Is(err, state.ErrNotFound) && h.Gen == "" {
		// Written before versions were kept beside their head.
		raw, err = s.st.Get(ctx, nsVersions, tenant, versionKey(tree, n))
	}
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

// Head returns a tree's latest version. A tree never written is version 0,
// empty.
func (s *Store) Head(ctx context.Context, tenant string, who Actor, tree Tree) (Version, error) {
	if err := s.check(who, tree, func(g Grant) bool { return g.Read }); err != nil {
		return Version{}, err
	}
	h, _, err := s.head(ctx, tenant, tree)
	if err != nil {
		return Version{}, err
	}
	return s.version(ctx, tenant, h, h.Version)
}

// Version returns one version of a tree.
func (s *Store) Version(ctx context.Context, tenant string, who Actor, tree Tree, n int64) (Version, error) {
	if err := s.check(who, tree, func(g Grant) bool { return g.Read }); err != nil {
		return Version{}, err
	}
	h, _, err := s.head(ctx, tenant, tree)
	if err != nil {
		return Version{}, err
	}
	if n < 0 || n > h.Version || n != 0 && n < h.Oldest {
		return Version{}, fmt.Errorf("%w: version %d of %s", ErrNotFound, n, tree.Key())
	}
	return s.version(ctx, tenant, h, n)
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
	h, _, err := s.head(ctx, tenant, tree)
	if err != nil {
		return nil, err
	}
	n := h.Version
	if before > 0 && before-1 < n {
		n = before - 1
	}
	out := []Version{}
	for ; n >= max(h.Oldest, 1) && len(out) < limit; n-- {
		v, err := s.version(ctx, tenant, h, n)
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

// treeState is a tree as one change reads it.
type treeState struct {
	head headRecord
	// rev is the version of the head's record, state.Missing for none.
	rev int64
	cur Version
}

func (t treeState) open(id string) bool { return slices.Contains(t.head.Open, id) }

// change is what one change to a tree writes. A nil entries leaves the
// files as they are.
type change struct {
	entries  []Entry
	meta     Version
	propose  []Proposal
	discard  []string
	restored int64
}

// result is what a change wrote.
type result struct {
	version Version
	// wrote is false when the change left the tree as it was.
	wrote bool
}

// mutate reads a tree, asks fn what to change, and writes it at the head it
// read: the version, the head and the proposals in one write. When another
// writer changed the tree in between, it reads again and asks fn again.
func (s *Store) mutate(ctx context.Context, tenant string, tree Tree, fn func(t treeState) (change, error)) (result, error) {
	var out result
	err := state.Retry(func() error {
		out = result{}
		h, rev, err := s.head(ctx, tenant, tree)
		if err != nil {
			return err
		}
		if !h.Indexed {
			if h.Open, err = s.openIDs(ctx, tenant, tree); err != nil {
				return err
			}
			h.Indexed = true
		}
		cur, err := s.version(ctx, tenant, h, h.Version)
		if err != nil {
			return err
		}
		t := treeState{head: h, rev: rev, cur: cur}
		c, err := fn(t)
		if err != nil {
			return err
		}
		next := h
		next.Open = slices.Clone(h.Open)
		if rev == state.Missing {
			next.Gen = rand.Text()
		}
		var ops, pruned []state.Op
		v := cur
		if c.entries != nil && !slices.Equal(c.entries, cur.Entries) {
			if len(c.entries) > MaxEntries {
				return fmt.Errorf("%w: a tree holds at most %d files", ErrTooLarge, MaxEntries)
			}
			m := c.meta
			if m.At.IsZero() {
				m.At = s.now()
			}
			v = Version{Tree: tree, N: h.Version + 1, Parent: h.Version, Entries: c.entries, By: m.By, At: m.At.UTC(), Message: m.Message, Origin: m.Origin, Restored: c.restored, Resolves: m.Resolves}
			raw, _ := json.Marshal(v)
			ops = append(ops, state.Put(tenant, versionKey(tree, v.N), raw))
			next.Version, next.Oldest = v.N, max(h.Oldest, 1)
			for next.Version-next.Oldest+1 > MaxVersions {
				ops = append(ops, state.Delete(tenant, versionKey(tree, next.Oldest)))
				pruned = append(pruned, state.Delete(tenant, versionKey(tree, next.Oldest)))
				next.Oldest++
			}
		}
		closed := slices.Concat(c.meta.Resolves, c.discard)
		for _, id := range closed {
			ops = append(ops, state.Delete(tenant, proposalKey(tree, id)))
			next.Open = slices.DeleteFunc(next.Open, func(x string) bool { return x == id })
		}
		for _, p := range c.propose {
			raw, _ := json.Marshal(p)
			ops = append(ops, state.Put(tenant, proposalKey(tree, p.ID), raw))
			if !slices.Contains(next.Open, p.ID) {
				next.Open = append(next.Open, p.ID)
			}
		}
		if len(next.Open) > MaxProposals {
			return fmt.Errorf("%w: a tree holds at most %d open proposals", ErrTooLarge, MaxProposals)
		}
		if len(ops) == 0 {
			out.version = cur
			return nil
		}
		raw, _ := json.Marshal(next)
		if rev == state.Missing {
			// The directory lists the tree before its head exists, so a
			// tree with a head is always listed.
			if err := s.mark(ctx, tenant, tree); err != nil {
				return err
			}
			ops = append(ops, state.Create(tenant, headKey(tree), raw))
		} else {
			ops = append(ops, state.PutAt(tenant, headKey(tree), raw, rev))
		}
		if err := s.st.Write(ctx, nsTrees, ops...); err != nil {
			if errors.Is(err, state.ErrConflict) {
				return err
			}
			return fmt.Errorf("drive: write: %w", err)
		}
		if v.N != cur.N {
			s.cache.put("v\x00"+tk(tenant, tree)+"\x00"+next.Gen+"\x00"+state.Seq(v.N), v, int64(256+96*len(v.Entries)))
		}
		if len(pruned) > 0 && next.Gen == "" {
			// Best effort: a version written before versions were kept
			// beside their head, left behind, is unreachable, not wrong.
			_ = s.st.Write(ctx, nsVersions, pruned...)
		}
		out = result{version: v, wrote: true}
		return nil
	})
	if errors.Is(err, state.ErrBusy) {
		return result{}, fmt.Errorf("drive: %s: %w", tree.Key(), err)
	}
	return out, err
}

// mark lists a tree in the directory. Listing it again writes the same.
func (s *Store) mark(ctx context.Context, tenant string, tree Tree) error {
	raw, _ := json.Marshal(tree)
	if err := s.st.Write(ctx, nsDir, state.Put(tenant, tree.Key(), raw)); err != nil {
		return fmt.Errorf("drive: list tree: %w", err)
	}
	return nil
}

// openIDs lists a tree's proposals, for a head written before it kept them.
func (s *Store) openIDs(ctx context.Context, tenant string, tree Tree) ([]string, error) {
	ps, err := s.listProposals(ctx, tenant, tree)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(ps))
	for _, p := range ps {
		ids = append(ids, p.ID)
	}
	return ids, nil
}

// listProposals reads a tree's proposals: at most MaxProposals, which every
// write enforces.
func (s *Store) listProposals(ctx context.Context, tenant string, tree Tree) ([]Proposal, error) {
	recs, next, err := s.st.List(ctx, nsTrees, tenant, state.Query{Prefix: proposalPrefix(tree), Limit: MaxProposals + 1})
	if err != nil {
		return nil, fmt.Errorf("drive: read proposals: %w", err)
	}
	if len(recs) > MaxProposals || next != "" && len(recs) == MaxProposals+1 {
		return nil, fmt.Errorf("%w: %s holds more than %d proposals", ErrTooLarge, tree.Key(), MaxProposals)
	}
	out := make([]Proposal, 0, len(recs))
	for _, r := range recs {
		var p Proposal
		if err := json.Unmarshal(r.Value, &p); err != nil {
			return nil, fmt.Errorf("drive: read proposal %q: %w", r.Key, err)
		}
		out = append(out, p)
	}
	return out, nil
}

func (s *Store) readProposal(ctx context.Context, tenant string, tree Tree, id string) (Proposal, error) {
	raw, err := s.st.Get(ctx, nsTrees, tenant, proposalKey(tree, id))
	if errors.Is(err, state.ErrNotFound) {
		return Proposal{}, fmt.Errorf("%w: proposal %q", ErrNotFound, id)
	}
	if err != nil {
		return Proposal{}, fmt.Errorf("drive: read proposal: %w", err)
	}
	var p Proposal
	if err := json.Unmarshal(raw, &p); err != nil {
		return Proposal{}, fmt.Errorf("drive: read proposal: %w", err)
	}
	return p, nil
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
	r, err := s.mutate(ctx, tenant, tree, func(t treeState) (change, error) {
		for _, id := range in.Resolves {
			if !t.open(id) {
				return change{}, fmt.Errorf("%w: proposal %q", ErrNotFound, id)
			}
		}
		entries, err := s.rebase(ctx, tenant, t, in.Base, changes)
		if err != nil {
			return change{}, err
		}
		return change{entries: entries, meta: meta(in)}, nil
	})
	if err != nil {
		return Version{}, err
	}
	if r.wrote {
		s.notify(Event{Tenant: tenant, Tree: tree, Version: r.version.N, Origin: in.Origin})
	}
	return r.version, nil
}

func meta(in CommitInput) Version {
	return Version{By: in.By, At: in.At, Message: in.Message, Origin: in.Origin, Resolves: in.Resolves}
}

// rebase applies changes made against base to the head, refusing any path
// changed since base.
func (s *Store) rebase(ctx context.Context, tenant string, t treeState, base int64, changes []Change) ([]Entry, error) {
	if base != Head && base != t.head.Version {
		if base < 0 || base > t.head.Version || base != 0 && base < t.head.Oldest {
			return nil, fmt.Errorf("%w: base version %d", ErrConflict, base)
		}
		was, err := s.version(ctx, tenant, t.head, base)
		if err != nil {
			return nil, fmt.Errorf("%w: base version %d", ErrConflict, base)
		}
		for _, c := range changes {
			a, _ := was.Lookup(c.Path)
			b, _ := t.cur.Lookup(c.Path)
			if a != b {
				return nil, fmt.Errorf("%w: %s", ErrConflict, c.Path)
			}
		}
	}
	return applyChanges(t.cur.Entries, changes), nil
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
	p, err := s.proposal(ctx, tenant, tree, in)
	if err != nil {
		return Proposal{}, err
	}
	r, err := s.mutate(ctx, tenant, tree, func(t treeState) (change, error) {
		q := p
		if q.Base == Head {
			q.Base = t.head.Version
		}
		p = q
		return change{propose: []Proposal{q}}, nil
	})
	if err != nil {
		return Proposal{}, err
	}
	if r.wrote {
		s.notify(Event{Tenant: tenant, Tree: tree, Origin: in.Origin, Proposal: p.ID})
	}
	return p, nil
}

// proposal checks a proposal and uploads its content. Its Base is left as
// given, Head included.
func (s *Store) proposal(ctx context.Context, tenant string, tree Tree, in ProposeInput) (Proposal, error) {
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
	return Proposal{ID: in.ID, Tree: tree, Base: in.Base, Changes: changes, By: in.By, At: at.UTC(), Message: in.Message, Origin: in.Origin}, nil
}

// Proposals returns a tree's open proposals, oldest first.
func (s *Store) Proposals(tenant string, who Actor, tree Tree) ([]Proposal, error) {
	if err := s.check(who, tree, func(g Grant) bool { return g.Read }); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), state.Timeout)
	defer cancel()
	out, err := s.listProposals(ctx, tenant, tree)
	if err != nil {
		return nil, err
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
	var origin string
	r, err := s.mutate(ctx, tenant, tree, func(t treeState) (change, error) {
		if !t.open(id) {
			return change{}, fmt.Errorf("%w: proposal %q", ErrNotFound, id)
		}
		p, err := s.readProposal(ctx, tenant, tree, id)
		if err != nil {
			return change{}, err
		}
		entries, err := s.rebase(ctx, tenant, t, p.Base, p.Changes)
		if err != nil {
			return change{}, err
		}
		origin = p.Origin
		return change{entries: entries, meta: Version{By: by, At: at, Message: p.Message, Origin: p.Origin, Resolves: []string{id}}}, nil
	})
	if err != nil {
		return Version{}, err
	}
	s.notify(Event{Tenant: tenant, Tree: tree, Version: r.version.N, Origin: origin})
	return r.version, nil
}

// Discard closes a proposal without applying it.
// Origin is reported to watchers.
func (s *Store) Discard(ctx context.Context, tenant string, who Actor, tree Tree, id, origin string) error {
	if err := s.check(who, tree, func(g Grant) bool { return g.Approve }); err != nil {
		return err
	}
	_, err := s.mutate(ctx, tenant, tree, func(t treeState) (change, error) {
		if !t.open(id) {
			return change{}, fmt.Errorf("%w: proposal %q", ErrNotFound, id)
		}
		return change{discard: []string{id}}, nil
	})
	if err != nil {
		return err
	}
	s.notify(Event{Tenant: tenant, Tree: tree, Proposal: id, Origin: origin})
	return nil
}

// Restore checks out an earlier version as a new one. The history is kept.
// A person restores their own tree; any other takes Approve.
func (s *Store) Restore(ctx context.Context, tenant string, who Actor, tree Tree, version int64, by string, at time.Time) (Version, error) {
	if err := s.check(who, tree, func(g Grant) bool { return g.Approve }); err != nil {
		return Version{}, err
	}
	r, err := s.mutate(ctx, tenant, tree, func(t treeState) (change, error) {
		if version <= 0 || version > t.head.Version || version < t.head.Oldest {
			return change{}, fmt.Errorf("%w: version %d of %s", ErrNotFound, version, tree.Key())
		}
		old, err := s.version(ctx, tenant, t.head, version)
		if err != nil {
			return change{}, err
		}
		return change{entries: slices.Clone(old.Entries), restored: version, meta: Version{By: by, At: at, Message: fmt.Sprintf("restore version %d", version), Origin: "restore"}}, nil
	})
	if err != nil {
		return Version{}, err
	}
	if r.wrote {
		s.notify(Event{Tenant: tenant, Tree: tree, Version: r.version.N, Origin: "restore"})
	}
	return r.version, nil
}

// EditInput is what an Edit writes to a tree in one change: files committed
// to the head, proposals held or replaced, and proposals closed.
type EditInput struct {
	// Changes are committed to the head as it is.
	Changes []Change
	// Resolves close proposals as applied by this version; Discard closes
	// them without it.
	Resolves []string
	Discard  []string
	Propose  []ProposeInput
	By       string
	At       time.Time
	Message  string
	Origin   string
}

// Edit reads a tree's head and open proposals, passes them to fn, and writes
// what fn returns in one change, at the head it read: when another writer
// changed the tree in between, it reads again and calls fn again, so fn must
// not keep anything from a call that did not write. It takes every grant
// what it writes needs. It returns the head after the change.
func (s *Store) Edit(ctx context.Context, tenant string, who Actor, tree Tree, fn func(head Version, open []Proposal) (EditInput, error)) (Version, error) {
	if err := s.check(who, tree, func(g Grant) bool { return g.Read }); err != nil {
		return Version{}, err
	}
	var origin string
	r, err := s.mutate(ctx, tenant, tree, func(t treeState) (change, error) {
		open, err := s.listProposals(ctx, tenant, tree)
		if err != nil {
			return change{}, err
		}
		in, err := fn(t.cur, open)
		if err != nil {
			return change{}, err
		}
		g := who.grant(tree)
		switch {
		case (len(in.Changes) > 0 || len(in.Resolves) > 0) && !g.Write,
			len(in.Propose) > 0 && !g.Propose,
			len(in.Discard) > 0 && !g.Approve:
			return change{}, fmt.Errorf("%w: %s", ErrForbidden, tree.Key())
		}
		for _, id := range slices.Concat(in.Resolves, in.Discard) {
			if !t.open(id) {
				return change{}, fmt.Errorf("%w: proposal %q", ErrNotFound, id)
			}
		}
		c := change{discard: in.Discard, meta: Version{By: in.By, At: in.At, Message: in.Message, Origin: in.Origin, Resolves: in.Resolves}}
		if len(in.Changes) > 0 {
			changes, err := s.upload(ctx, tenant, tree, in.Changes)
			if err != nil {
				return change{}, err
			}
			c.entries = applyChanges(t.cur.Entries, changes)
		} else if len(in.Resolves) > 0 {
			// Only proposals are closed: no version is written.
			c.discard = slices.Concat(c.discard, in.Resolves)
			c.meta.Resolves = nil
		}
		if c.entries != nil && slices.Equal(c.entries, t.cur.Entries) && len(c.meta.Resolves) > 0 {
			c.discard = slices.Concat(c.discard, c.meta.Resolves)
			c.meta.Resolves = nil
		}
		for _, pin := range in.Propose {
			p, err := s.proposal(ctx, tenant, tree, pin)
			if err != nil {
				return change{}, err
			}
			if p.Base == Head {
				p.Base = t.head.Version
			}
			c.propose = append(c.propose, p)
		}
		origin = in.Origin
		return c, nil
	})
	if err != nil {
		return Version{}, err
	}
	if r.wrote {
		s.notify(Event{Tenant: tenant, Tree: tree, Version: r.version.N, Origin: origin})
	}
	return r.version, nil
}

// Drop forgets a tree: its head, its proposals and its versions, in one
// write at the head it read. The content stays in the blob store, where
// other trees may share it. The tree stays listed, as an empty tree. Origin
// is reported to watchers.
func (s *Store) Drop(ctx context.Context, tenant string, tree Tree, origin string) error {
	if err := tree.Valid(); err != nil {
		return err
	}
	var dropped headRecord
	held := false
	err := state.Retry(func() error {
		h, rev, err := s.head(ctx, tenant, tree)
		if err != nil {
			return err
		}
		open := h.Open
		if !h.Indexed {
			if open, err = s.openIDs(ctx, tenant, tree); err != nil {
				return err
			}
		}
		var ops []state.Op
		for _, id := range open {
			ops = append(ops, state.Delete(tenant, proposalKey(tree, id)))
		}
		if rev == state.Missing {
			if len(ops) == 0 {
				held = false
				return nil
			}
			// Proposals written before a proposal wrote a head: the write
			// requires the head to be missing still, by creating it and
			// removing it again.
			ops = append(ops, state.Create(tenant, headKey(tree), []byte("{}")), state.Delete(tenant, headKey(tree)))
		} else {
			for n := max(h.Oldest, 1); n <= h.Version; n++ {
				ops = append(ops, state.Delete(tenant, versionKey(tree, n)))
			}
			ops = append(ops, state.DeleteAt(tenant, headKey(tree), rev))
		}
		if err := s.st.Write(ctx, nsTrees, ops...); err != nil {
			if errors.Is(err, state.ErrConflict) {
				return err
			}
			return fmt.Errorf("drive: drop: %w", err)
		}
		dropped, held = h, true
		return nil
	})
	if err != nil {
		return err
	}
	if !held {
		return nil
	}
	if dropped.Gen == "" && dropped.Version > 0 {
		// Best effort: a version left behind is unreachable, not wrong.
		var prune []state.Op
		for n := max(dropped.Oldest, 1); n <= dropped.Version; n++ {
			prune = append(prune, state.Delete(tenant, versionKey(tree, n)))
		}
		_ = s.st.Write(ctx, nsVersions, prune...)
	}
	s.notify(Event{Tenant: tenant, Tree: tree, Origin: origin})
	return nil
}
