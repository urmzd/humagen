package platform

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/urmzd/playscape/pkg/workspace"

	"github.com/urmzd/humagen/fiber/pkg/isolation"
	"github.com/urmzd/humagen/pkg/drive"
	"github.com/urmzd/humagen/pkg/runs"
)

// A run's memory in its instance (RFC 0004, "Where the files live").
//
// When a run leases its instance, runMemory installs the memory trees the
// run may read (MemoryMounts) into the instance's /project and /me. When the
// run releases it, before the instance is destroyed, runMemory diffs them
// and keeps what changed (FinishMemory): committed to the requester's own
// tree on the version that was installed, so a change made meanwhile by
// another run is never overwritten, and proposed for everything else.
// /project is read only inside the instance, so only /me changes there.

// runMemory is the isolation.Files source of a run's memory.
type runMemory struct {
	p           *Platform
	tenant, run string

	mu        sync.Mutex
	loaded    bool
	record    runs.Run
	installed []drive.Installed
}

// Load implements isolation.Files: it installs the run's memory. A run whose
// memory cannot be installed gets no instance, so it never writes over
// memory it did not see.
func (m *runMemory) Load(ctx context.Context, inst isolation.Instance) error {
	run, err := m.p.Runs.Get(m.tenant, m.run)
	if err != nil {
		return fmt.Errorf("%w: this run has no record to read its memory by", ErrSandbox)
	}
	installed, err := m.p.InstallMemory(ctx, run, instanceMemory{ctx: ctx, inst: inst}, nil)
	if err != nil {
		return fmt.Errorf("platform: install the run's memory: %w", err)
	}
	m.mu.Lock()
	m.loaded, m.record, m.installed = true, run, installed
	m.mu.Unlock()
	return nil
}

// Save implements isolation.Files: it keeps what the run changed in its
// memory.
func (m *runMemory) Save(ctx context.Context, inst isolation.Instance) error {
	m.mu.Lock()
	loaded, run, installed := m.loaded, m.record, m.installed
	m.mu.Unlock()
	if !loaded {
		return nil
	}
	done, err := m.p.FinishMemory(ctx, run, installed, instanceMemory{ctx: ctx, inst: inst})
	for _, f := range done {
		if len(f.Dropped) > 0 {
			m.p.log.Info("platform: memory changes a run made were not kept", "run", run.ID, "dir", f.Mount.Dir, "paths", f.Dropped)
		}
	}
	if err != nil {
		return fmt.Errorf("platform: keep the run's memory: %w", err)
	}
	return nil
}

// instanceMemory is an instance's /project and /me as a drive.Target. Names
// start with "project/" or "me/", as drive.Tree.Mount gives them.
type instanceMemory struct {
	ctx  context.Context
	inst isolation.Instance
}

var _ drive.Target = instanceMemory{}

// split returns the workspace a name is in and the name inside it.
func (t instanceMemory) split(name string) (workspace.Workspace, string, error) {
	top, rest, _ := strings.Cut(name, "/")
	switch top {
	case "project":
		return t.inst.Project(), rest, nil
	case "me":
		return t.inst.Me(), rest, nil
	}
	return nil, "", fmt.Errorf("%w: %q is not under project/ or me/", ErrSandbox, name)
}

// ReadFile implements drive.Target. It reads at most one byte more than
// drive.MaxFileBytes, enough for the caller to see a file is too large.
func (t instanceMemory) ReadFile(name string) ([]byte, error) {
	ws, key, err := t.split(name)
	if err != nil {
		return nil, err
	}
	r, err := ws.Read(t.ctx, key)
	if err != nil {
		return nil, err
	}
	defer func() { _ = r.Close() }()
	return io.ReadAll(io.LimitReader(r, drive.MaxFileBytes+1))
}

// WriteFile implements drive.Target.
func (t instanceMemory) WriteFile(name string, data []byte) error {
	ws, key, err := t.split(name)
	if err != nil {
		return err
	}
	return ws.Write(t.ctx, key, bytes.NewReader(data))
}

// Remove implements drive.Target.
func (t instanceMemory) Remove(name string) error {
	ws, key, err := t.split(name)
	if err != nil {
		return err
	}
	return ws.Delete(t.ctx, key)
}

// Walk implements drive.Target. A size is counted up to one byte past
// drive.MaxFileBytes.
func (t instanceMemory) Walk(dir string, fn func(name string, size int64) error) error {
	ws, prefix, err := t.split(dir)
	if err != nil {
		return err
	}
	top, _, _ := strings.Cut(dir, "/")
	keys, err := ws.List(t.ctx, prefix)
	if err != nil {
		return err
	}
	for _, k := range keys {
		r, err := ws.Read(t.ctx, k)
		if err != nil {
			continue // gone since it was listed
		}
		n, err := io.Copy(io.Discard, io.LimitReader(r, drive.MaxFileBytes+1))
		_ = r.Close()
		if err != nil {
			return err
		}
		if err := fn(top+"/"+k, n); err != nil {
			return err
		}
	}
	return nil
}
