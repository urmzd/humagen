package platform_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/urmzd/humagen/fiber/pkg/isolation"
	"github.com/urmzd/humagen/fiber/pkg/isolation/isolationtest"
	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
)

func TestWorkspaceFileRevisionsPreventConcurrentLostEdits(t *testing.T) {
	p := start(t, call.Limits{})
	th := startThread(t, p, operator, platform.ThreadInput{Title: "File edits", Text: "Notes", PostOnly: true, Visibility: threads.Private}).Thread
	file, err := p.WriteWorkspaceFile(t.Context(), operator, th.ID, platform.WorkspaceFileWrite{Path: "notes/scratch.md", Content: "initial"})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, text := range []string{"left", "right"} {
		wg.Go(func() {
			_, err := p.WriteWorkspaceFile(t.Context(), operator, th.ID, platform.WorkspaceFileWrite{Path: file.Path, Revision: file.Revision, Content: text})
			results <- err
		})
	}
	wg.Wait()
	close(results)
	won, conflicted := 0, 0
	for err := range results {
		if err == nil {
			won++
		} else if errors.Is(err, platform.ErrWorkspaceConflict) {
			conflicted++
		} else {
			t.Fatal(err)
		}
	}
	if won != 1 || conflicted != 1 {
		t.Fatalf("saved=%d conflicted=%d", won, conflicted)
	}
	for _, path := range []string{"../outside", "/absolute", "dir\\file"} {
		if _, err := p.WriteWorkspaceFile(t.Context(), operator, th.ID, platform.WorkspaceFileWrite{Path: path, Content: "no"}); !errors.Is(err, platform.ErrSandbox) {
			t.Errorf("unsafe path %q: %v", path, err)
		}
	}
	dir := p.SandboxDir(tenant, th.ID)
	if err := os.Symlink("notes", filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	if _, err := p.WorkspaceFile(t.Context(), operator, th.ID, "link/scratch.md"); !errors.Is(err, platform.ErrSandbox) {
		t.Fatalf("internal symlink followed: %v", err)
	}
	if err := p.PutFile(tenant, th.ID, "binary.bin", "\x00\xff"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.WorkspaceFile(t.Context(), operator, th.ID, "binary.bin"); !errors.Is(err, platform.ErrSandbox) {
		t.Fatalf("binary text read: %v", err)
	}
	if _, err := p.ArchiveThread(operator, th.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, err := p.WriteWorkspaceFile(t.Context(), operator, th.ID, platform.WorkspaceFileWrite{Path: "new.md", Content: "no"}); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("archived mutation: %v", err)
	}
}

func TestWorkspaceFileCopybackPreservesChangesMadeAfterLoad(t *testing.T) {
	leaser := &isolationtest.Leaser{Dir: t.TempDir()}
	p := startWith(t, platform.Config{Sandbox: isolation.Sandbox{Leaser: leaser, Name: "test"}})
	run, req := turn(t, p, "file-cas", "")
	for _, name := range []string{"edit.md", "delete.md"} {
		if err := p.PutFile(tenant, run.Thread, name, "original"); err != nil {
			t.Fatal(err)
		}
	}
	ctx, claim := p.Claim(t.Context(), req)
	if _, err := useIn(t, ctx, p, req, platform.ToolFileRead, map[string]any{"path": "edit.md"}); err != nil {
		t.Fatal(err)
	}
	if _, err := useIn(t, ctx, p, req, platform.ToolShell, map[string]any{"command": "echo agent > edit.md; rm delete.md"}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"edit.md", "delete.md"} {
		if err := p.PutFile(tenant, run.Thread, name, "concurrent edit"); err != nil {
			t.Fatal(err)
		}
	}
	if err := claim.Release(t.Context()); err == nil {
		t.Fatal("copyback conflict was hidden")
	}
	for _, name := range []string{"edit.md", "delete.md"} {
		data, err := os.ReadFile(filepath.Join(p.SandboxDir(tenant, run.Thread), name))
		if err != nil || string(data) != "concurrent edit" {
			t.Errorf("%s = %q %v", name, data, err)
		}
	}
	// An accepted turn with no ledger ending cannot be edited under it.
	tag := wire.NewEvent(wire.Raise{ID: "workspace-busy", Tenant: tenant, Agent: "lead", Origin: runs.OriginAPI, Text: "Work", Context: map[string]string{runs.MetaThread: run.Thread}, At: time.Now()})
	if err := p.Runs.Record("workspace-busy", tag, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := p.WriteWorkspaceFile(t.Context(), operator, run.Thread, platform.WorkspaceFileWrite{Path: "blocked.md", Content: "no"}); !errors.Is(err, platform.ErrWorkspaceConflict) {
		t.Fatalf("active mutation = %v", err)
	}
}

func TestWorkspaceFilesAreBoundToPublicationConsent(t *testing.T) {
	p := start(t, call.Limits{})
	th := startThread(t, p, operator, platform.ThreadInput{Title: "Private files", Text: "Draft", PostOnly: true, Visibility: threads.Private}).Thread
	file, err := p.WriteWorkspaceFile(t.Context(), operator, th.ID, platform.WorkspaceFileWrite{Path: "notes.md", Content: "Reviewed"})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := p.PreviewPlacement(t.Context(), operator, th.ID, "~", "general")
	if err != nil {
		t.Fatal(err)
	}
	if preview.WorkspaceFiles != 1 {
		t.Fatalf("file disclosure = %d", preview.WorkspaceFiles)
	}
	if _, err := p.Share(t.Context(), operator, th.ID, platform.ThreadShare{}); err == nil {
		t.Fatal("private files shared without consent")
	}
	if _, err := p.WriteWorkspaceFile(t.Context(), operator, th.ID, platform.WorkspaceFileWrite{Path: file.Path, Revision: file.Revision, Content: "Unreviewed private edit"}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Share(t.Context(), operator, th.ID, platform.ThreadShare{PlacementToken: preview.Token}); err == nil {
		t.Fatal("stale scratch consent accepted")
	}
	preview, err = p.PreviewPlacement(t.Context(), operator, th.ID, "~", "general")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Share(t.Context(), operator, th.ID, platform.ThreadShare{PlacementToken: preview.Token}); err != nil {
		t.Fatal(err)
	}
}

func TestWorkspaceFileListingBoundsFilesCreatedOutsideFileTools(t *testing.T) {
	p := start(t, call.Limits{})
	th := startThread(t, p, operator, platform.ThreadInput{Title: "Bounded files", Text: "Draft", PostOnly: true, Visibility: threads.Private}).Thread
	if _, err := p.WorkspaceFiles(t.Context(), operator, th.ID); err != nil {
		t.Fatal(err)
	}
	// A sandbox shell may create more files than file_write allows.
	for i := range platform.MaxSandboxFiles + 1 {
		if err := os.WriteFile(filepath.Join(p.SandboxDir(tenant, th.ID), fmt.Sprintf("%03d.txt", i)), []byte("x"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := p.WorkspaceFiles(t.Context(), operator, th.ID); err == nil {
		t.Fatal("listing returned more than the bounded file set")
	}
}
