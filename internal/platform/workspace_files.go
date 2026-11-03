package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/urmzd/humagen/pkg/artifacts"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
	drun "github.com/urmzd/orchagent/durable/run"
)

// ErrWorkspaceConflict refuses stale edits and changes while a run owns files.
var ErrWorkspaceConflict = errors.New("workspace files: conflict")

type lockedWorkspaceArea struct {
	fileArea
	unlock func()
}

func (a lockedWorkspaceArea) close() error {
	defer a.unlock()
	return a.fileArea.close()
}

type WorkspaceFile struct {
	Path     string `json:"path"`
	Size     int64  `json:"size"`
	Revision string `json:"revision"`
}

type WorkspaceFileContent struct {
	Path     string `json:"path"`
	Content  string `json:"content"`
	Revision string `json:"revision"`
}

type WorkspaceFileList struct {
	Files         []WorkspaceFile `json:"files"`
	Writable      bool            `json:"writable"`
	Reason        string          `json:"reason,omitempty"`
	RetentionDays int             `json:"retention_days"`
}

type WorkspaceFileWrite struct {
	Path     string `json:"path"`
	Content  string `json:"content"`
	Revision string `json:"revision"`
}

type WorkspaceFilePublish struct {
	Path     string `json:"path"`
	Revision string `json:"revision"`
	Title    string `json:"title,omitempty"`
}

func fileRevision(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func editableBytes(data []byte) bool {
	return utf8.Valid(data) && !strings.ContainsRune(string(data), 0)
}

// workspaceThread checks the current audience before opening any directory.
func (p *Platform) workspaceThread(who auth.Principal, id string) (threads.Thread, error) {
	th, err := p.ThreadRecord(who.Tenant, id)
	if err != nil || !p.Seer(who).Conversation(id, th.Project) {
		return threads.Thread{}, threads.ErrNotFound
	}
	return th, nil
}

func (p *Platform) workspaceMayWrite(ctx context.Context, who auth.Principal, th threads.Thread) error {
	if !who.Can(auth.RunsWrite) {
		return auth.ErrForbidden
	}
	if th.Archive != nil || th.ReadOnly {
		return fmt.Errorf("%w: this conversation is archived or read only", ErrForbidden)
	}
	if err := p.MayPost(who, Place{Project: th.Project, Thread: th.ID}); err != nil {
		return err
	}
	return p.workspaceIdle(ctx, who.Tenant, th.ID)
}

// workspaceIdle uses the execution ledger, including after a terminal event
// reached the index but before the worker finished saving its files.
func (p *Platform) workspaceIdle(ctx context.Context, tenant, id string) error {
	busy := func() error {
		return fmt.Errorf("%w: wait for active and waiting work to finish before changing files", ErrWorkspaceConflict)
	}
	if p.Runs.AnyActive(tenant, func(run runs.Run) bool { return placeOfRun(run).Thread == id }) {
		return busy()
	}
	turns, err := p.Runs.Conversation(tenant, id)
	if errors.Is(err, runs.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(turns) >= runs.MaxTree {
		return busy()
	}
	for _, turn := range turns {
		tree, err := p.Runs.Tree(tenant, turn.ID)
		if err != nil {
			return err
		}
		if len(tree) >= runs.MaxTree {
			return busy()
		}
		for _, run := range tree {
			rec, err := p.ledger.GetRun(ctx, run.ID)
			if err != nil || (rec.Status != drun.StatusSucceeded && rec.Status != drun.StatusFailed) {
				return busy()
			}
		}
	}
	return nil
}

// workspacePath also rejects links to locations inside the root. os.Root
// independently prevents links from escaping it.
func workspacePath(root *os.Root, raw string) (string, error) {
	clean, err := cleanPath(raw)
	if err != nil {
		return "", err
	}
	parts := strings.Split(clean, "/")
	for i := range parts {
		st, err := root.Lstat(strings.Join(parts[:i+1], "/"))
		if errors.Is(err, fs.ErrNotExist) {
			break
		}
		if err != nil {
			return "", err
		}
		if st.Mode()&fs.ModeSymlink != 0 {
			return "", fmt.Errorf("%w: symbolic links cannot be edited", ErrSandbox)
		}
	}
	return clean, nil
}

func (p *Platform) WorkspaceFiles(ctx context.Context, who auth.Principal, id string) (WorkspaceFileList, error) {
	if !who.Can(auth.RunsRead) {
		return WorkspaceFileList{}, auth.ErrForbidden
	}
	mu := p.moves.of(who.Tenant, id)
	mu.RLock()
	defer mu.RUnlock()
	th, err := p.workspaceThread(who, id)
	if err != nil {
		return WorkspaceFileList{}, err
	}
	out := WorkspaceFileList{Files: []WorkspaceFile{}, RetentionDays: 7}
	if err := p.workspaceMayWrite(ctx, who, th); err != nil {
		out.Reason = err.Error()
	} else {
		out.Writable = true
	}
	files, err := p.workspaceSnapshot(ctx, who.Tenant, id)
	out.Files = files
	return out, err
}

func (p *Platform) workspaceSnapshot(ctx context.Context, tenant, id string) ([]WorkspaceFile, error) {
	p.workspaceMu.Lock()
	defer p.workspaceMu.Unlock()
	out := []WorkspaceFile{}
	root, err := p.openSandbox(tenant, id)
	if err != nil {
		return out, err
	}
	defer func() { _ = root.Close() }()
	area := rootArea{root}
	files := []sandboxFile{}
	var total int64
	visited := 0
	err = fs.WalkDir(root.FS(), ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		visited++
		if visited > MaxSandboxFiles*240 {
			return fmt.Errorf("workspace file scan exceeded its directory bound")
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		if len(files) >= MaxSandboxFiles || info.Size() > MaxFileBytes || total > MaxSandboxBytes || len(name) > 240 {
			return fmt.Errorf("workspace files exceed the file, path or byte bound")
		}
		files = append(files, sandboxFile{Path: name, Size: info.Size()})
		return nil
	})
	if err != nil {
		return out, err
	}
	for _, file := range files {
		data, err := area.read(ctx, file.Path)
		if err != nil {
			return out, fmt.Errorf("workspace file unavailable: %v", err)
		}
		out = append(out, WorkspaceFile{Path: file.Path, Size: int64(len(data)), Revision: fileRevision(data)})
	}
	slices.SortFunc(out, func(a, b WorkspaceFile) int { return strings.Compare(a.Path, b.Path) })
	return out, nil
}

func (p *Platform) WorkspaceFile(ctx context.Context, who auth.Principal, id, name string) (WorkspaceFileContent, error) {
	if !who.Can(auth.RunsRead) {
		return WorkspaceFileContent{}, auth.ErrForbidden
	}
	mu := p.moves.of(who.Tenant, id)
	mu.RLock()
	defer mu.RUnlock()
	if _, err := p.workspaceThread(who, id); err != nil {
		return WorkspaceFileContent{}, err
	}
	p.workspaceMu.Lock()
	defer p.workspaceMu.Unlock()
	root, err := p.openSandbox(who.Tenant, id)
	if err != nil {
		return WorkspaceFileContent{}, err
	}
	defer func() { _ = root.Close() }()
	clean, err := workspacePath(root, name)
	if err != nil {
		return WorkspaceFileContent{}, err
	}
	data, err := (rootArea{root}).read(ctx, clean)
	if err != nil {
		return WorkspaceFileContent{}, err
	}
	if !editableBytes(data) {
		return WorkspaceFileContent{}, fmt.Errorf("%w: this binary file cannot be edited as text", ErrSandbox)
	}
	return WorkspaceFileContent{Path: clean, Content: string(data), Revision: fileRevision(data)}, nil
}

// changeWorkspace holds the same admission lock that starts runs, and the
// file lock used by run copyback. A stale content hash never overwrites data.
func (p *Platform) changeWorkspace(ctx context.Context, who auth.Principal, id, name, revision string, change func(threads.Thread, rootArea, string) error) error {
	mu := p.moves.of(who.Tenant, id)
	mu.Lock()
	defer mu.Unlock()
	p.admission.Lock()
	defer p.admission.Unlock()
	th, err := p.workspaceThread(who, id)
	if err != nil {
		return err
	}
	if err := p.workspaceMayWrite(ctx, who, th); err != nil {
		return err
	}
	p.workspaceMu.Lock()
	defer p.workspaceMu.Unlock()
	root, err := p.openSandbox(who.Tenant, id)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	clean, err := workspacePath(root, name)
	if err != nil {
		return err
	}
	area := rootArea{root}
	st, err := root.Lstat(clean)
	if errors.Is(err, fs.ErrNotExist) {
		if revision != "" {
			return fmt.Errorf("%w: the file was removed; refresh before saving", ErrWorkspaceConflict)
		}
	} else {
		if err != nil {
			return err
		}
		if !st.Mode().IsRegular() {
			return fmt.Errorf("%w: path is not a regular file", ErrSandbox)
		}
		data, err := area.read(ctx, clean)
		if err != nil {
			return err
		}
		if revision == "" || fileRevision(data) != revision {
			return fmt.Errorf("%w: the file changed; refresh before saving", ErrWorkspaceConflict)
		}
	}
	return change(th, area, clean)
}

func (p *Platform) WriteWorkspaceFile(ctx context.Context, who auth.Principal, id string, in WorkspaceFileWrite) (WorkspaceFileContent, error) {
	if len(in.Content) > MaxFileBytes || !editableBytes([]byte(in.Content)) {
		return WorkspaceFileContent{}, fmt.Errorf("%w: provide UTF-8 text of at most %d bytes", ErrSandbox, MaxFileBytes)
	}
	var out WorkspaceFileContent
	err := p.changeWorkspace(ctx, who, id, in.Path, in.Revision, func(_ threads.Thread, area rootArea, clean string) error {
		if in.Revision != "" {
			data, err := area.read(ctx, clean)
			if err != nil {
				return err
			}
			if !editableBytes(data) {
				return fmt.Errorf("%w: this binary file cannot be edited as text", ErrSandbox)
			}
		}
		_, err := writeSandbox(ctx, area, clean, in.Content)
		out = WorkspaceFileContent{Path: clean, Content: in.Content, Revision: fileRevision([]byte(in.Content))}
		return err
	})
	return out, err
}

func (p *Platform) DeleteWorkspaceFile(ctx context.Context, who auth.Principal, id, name, revision string) error {
	if revision == "" {
		return fmt.Errorf("%w: a current revision is required", ErrWorkspaceConflict)
	}
	return p.changeWorkspace(ctx, who, id, name, revision, func(_ threads.Thread, area rootArea, clean string) error { return area.remove(clean) })
}

func (p *Platform) PublishWorkspaceFile(ctx context.Context, who auth.Principal, id string, in WorkspaceFilePublish) (artifacts.Artifact, error) {
	if !who.Can(auth.ArtifactsWrite) {
		return artifacts.Artifact{}, auth.ErrForbidden
	}
	if in.Revision == "" {
		return artifacts.Artifact{}, fmt.Errorf("%w: a current revision is required", ErrWorkspaceConflict)
	}
	var out artifacts.Artifact
	err := p.changeWorkspace(ctx, who, id, in.Path, in.Revision, func(th threads.Thread, area rootArea, clean string) error {
		var err error
		out, err = p.publishFrom(ctx, area, Publication{Tenant: who.Tenant, Thread: id, Project: th.Project, By: who.Subject, Path: clean, Title: in.Title})
		return err
	})
	return out, err
}
