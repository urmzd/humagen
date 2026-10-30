package drive

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Target is the file system a tree is installed into and diffed from: a run's
// instance, or a directory on the host. Names are slash separated and
// relative to the target's root.
type Target interface {
	ReadFile(name string) ([]byte, error)
	// WriteFile writes a file, creating its directories.
	WriteFile(name string, data []byte) error
	Remove(name string) error
	// Walk calls fn for every regular file under dir. A missing dir walks
	// nothing.
	Walk(dir string, fn func(name string, size int64) error) error
}

// RootTarget is a directory on the host, opened as an os.Root so no name
// reaches outside it.
func RootTarget(root *os.Root) Target { return rootTarget{root} }

type rootTarget struct{ r *os.Root }

func (t rootTarget) ReadFile(name string) ([]byte, error) {
	return t.r.ReadFile(filepath.FromSlash(name))
}

func (t rootTarget) WriteFile(name string, data []byte) error {
	if err := t.r.MkdirAll(filepath.Dir(filepath.FromSlash(name)), 0o700); err != nil {
		return err
	}
	return t.r.WriteFile(filepath.FromSlash(name), data, 0o600)
}

func (t rootTarget) Remove(name string) error { return t.r.Remove(filepath.FromSlash(name)) }

func (t rootTarget) Walk(dir string, fn func(string, int64) error) error {
	err := fs.WalkDir(t.r.FS(), dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return fn(p, info.Size())
	})
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

// HostCache keeps content on a host, by tenant and hash, so that runs on one
// host fetch a tree's files once. A miss is never an error.
type HostCache interface {
	Get(tenant, hash string) ([]byte, bool)
	Put(tenant, hash string, data []byte)
}

// DiskCache is a HostCache in a directory. What it reads is checked against
// its hash, so a damaged file is a miss.
type DiskCache struct{ root *os.Root }

// NewDiskCache opens a cache in dir, creating it.
func NewDiskCache(dir string) (*DiskCache, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	r, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	return &DiskCache{root: r}, nil
}

// Close releases the directory.
func (c *DiskCache) Close() error { return c.root.Close() }

func (c *DiskCache) name(tenant, hash string) string {
	return filepath.Join(blobTenant(tenant), hash[:2], hash)
}

// Get implements HostCache.
func (c *DiskCache) Get(tenant, hash string) ([]byte, bool) {
	if len(hash) != 64 {
		return nil, false
	}
	b, err := c.root.ReadFile(c.name(tenant, hash))
	if err != nil || hashOf(b) != hash {
		return nil, false
	}
	return b, true
}

// Put implements HostCache.
func (c *DiskCache) Put(tenant, hash string, data []byte) {
	if len(hash) != 64 || hashOf(data) != hash {
		return
	}
	name := c.name(tenant, hash)
	if err := c.root.MkdirAll(filepath.Dir(name), 0o700); err != nil {
		return
	}
	tmp := name + ".part"
	if c.root.WriteFile(tmp, data, 0o600) == nil {
		_ = c.root.Rename(tmp, name)
	}
}

// Mount is a tree installed at a directory of a target.
type Mount struct {
	Tree Tree   `json:"tree"`
	Dir  string `json:"dir"`
	// ReadOnly mounts are installed, and what a run changes in them is
	// reported and dropped.
	ReadOnly bool `json:"read_only,omitempty"`
}

// Installed is what was installed: the base a run's changes are diffed
// against and committed on.
type Installed struct {
	Tenant  string  `json:"tenant"`
	Mount   Mount   `json:"mount"`
	Version int64   `json:"version"`
	Entries []Entry `json:"entries"`
}

// Install writes a tree's head into a target's directory, removing what else
// is there, and returns the base to diff against. Content comes from the
// host cache when it has it.
func (s *Store) Install(ctx context.Context, tenant string, who Actor, m Mount, t Target, cache HostCache) (Installed, error) {
	dir, err := CleanPath(m.Dir)
	if err != nil {
		return Installed{}, err
	}
	m.Dir = dir
	v, err := s.Head(ctx, tenant, who, m.Tree)
	if err != nil {
		return Installed{}, err
	}
	want := map[string]bool{}
	for _, e := range v.Entries {
		want[path.Join(dir, e.Path)] = true
	}
	var stale []string
	if err := t.Walk(dir, func(name string, _ int64) error {
		if !want[name] {
			stale = append(stale, name)
		}
		return nil
	}); err != nil {
		return Installed{}, fmt.Errorf("drive: install: %w", err)
	}
	for _, name := range stale {
		if err := t.Remove(name); err != nil {
			return Installed{}, fmt.Errorf("drive: install: %w", err)
		}
	}
	for _, e := range v.Entries {
		b, ok := []byte(nil), false
		if cache != nil {
			b, ok = cache.Get(tenant, e.Hash)
		}
		if !ok {
			if b, err = s.Content(ctx, tenant, e.Hash); err != nil {
				return Installed{}, err
			}
			if cache != nil {
				cache.Put(tenant, e.Hash, b)
			}
		}
		if err := t.WriteFile(path.Join(dir, e.Path), b); err != nil {
			return Installed{}, fmt.Errorf("drive: install %s: %w", e.Path, err)
		}
	}
	return Installed{Tenant: tenant, Mount: m, Version: v.N, Entries: v.Entries}, nil
}

// Diff reads what a run changed under an installed mount: files written,
// changed or removed. It reads at most MaxEntries files of at most
// MaxFileBytes each.
func Diff(in Installed, t Target) ([]Change, error) {
	base := map[string]Entry{}
	for _, e := range in.Entries {
		base[e.Path] = e
	}
	seen := map[string]bool{}
	var changes []Change
	n := 0
	err := t.Walk(in.Mount.Dir, func(name string, size int64) error {
		n++
		if n > MaxEntries {
			return fmt.Errorf("%w: more than %d files under %s", ErrTooLarge, MaxEntries, in.Mount.Dir)
		}
		rel := strings.TrimPrefix(strings.TrimPrefix(name, in.Mount.Dir), "/")
		p, err := CleanPath(rel)
		if err != nil {
			return err
		}
		if size > MaxFileBytes {
			return fmt.Errorf("%w: %s is over %d bytes", ErrTooLarge, p, MaxFileBytes)
		}
		seen[p] = true
		b, err := t.ReadFile(name)
		if err != nil {
			return err
		}
		if e, ok := base[p]; ok && e.Hash == hashOf(b) {
			return nil
		}
		changes = append(changes, Change{Path: p, Data: bytes.Clone(b)})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("drive: diff: %w", err)
	}
	for _, e := range in.Entries {
		if !seen[e.Path] {
			changes = append(changes, Change{Path: e.Path, Delete: true})
		}
	}
	slices.SortFunc(changes, func(a, b Change) int { return strings.Compare(a.Path, b.Path) })
	return changes, nil
}

// Finished is what a run's end did with its changes to one mount.
type Finished struct {
	Mount Mount `json:"mount"`
	// Version is the version committed, for a tree the actor may write.
	Version int64 `json:"version,omitempty"`
	// Proposals are the proposals raised, one per changed file, for a tree
	// the actor may only propose to.
	Proposals []string `json:"proposals,omitempty"`
	// Dropped are the paths changed in a read-only mount, or refused.
	Dropped []string `json:"dropped,omitempty"`
}

// Finish diffs a mount at a run's end and keeps what changed: committed on
// the installed version when the actor may write the tree (a person's own),
// proposed, a file at a time, when they may only propose (a project's).
// Changes the store's screen refuses are dropped and reported.
func (s *Store) Finish(ctx context.Context, who Actor, in Installed, t Target, by, origin string, at time.Time) (Finished, error) {
	out := Finished{Mount: in.Mount}
	changes, err := Diff(in, t)
	if err != nil || len(changes) == 0 {
		return out, err
	}
	if in.Mount.ReadOnly {
		for _, c := range changes {
			out.Dropped = append(out.Dropped, c.Path)
		}
		return out, nil
	}
	g := who.grant(in.Mount.Tree)
	switch {
	case g.Write:
		v, err := s.Commit(ctx, in.Tenant, who, in.Mount.Tree, CommitInput{Base: in.Version, Changes: changes, By: by, At: at, Origin: origin, Message: "run changes"})
		if err != nil {
			return out, err
		}
		out.Version = v.N
	case g.Propose:
		for _, c := range changes {
			p, err := s.Propose(ctx, in.Tenant, who, in.Mount.Tree, ProposeInput{Base: in.Version, Changes: []Change{c}, By: by, At: at, Origin: origin, Message: "run changes " + c.Path})
			if errors.Is(err, ErrRefused) {
				out.Dropped = append(out.Dropped, c.Path)
				continue
			}
			if err != nil {
				return out, err
			}
			out.Proposals = append(out.Proposals, p.ID)
		}
	default:
		for _, c := range changes {
			out.Dropped = append(out.Dropped, c.Path)
		}
	}
	return out, nil
}
