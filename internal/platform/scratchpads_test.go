package platform_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/threads"
)

func TestScratchpadsArePeoplesNotesAgentsRead(t *testing.T) {
	p := start(t, call.Limits{})
	run, req := turn(t, p, "scratch-read", "")
	pad, err := p.CreateScratchpad(t.Context(), operator, run.Thread, platform.ScratchpadInput{Name: "Launch plan", Content: "# Ship Tuesday"})
	if err != nil {
		t.Fatal(err)
	}
	list, err := p.ConversationScratchpads(t.Context(), operator, run.Thread)
	if err != nil || len(list.Scratchpads) != 1 || !list.Writable || list.Limit != 50 {
		t.Fatalf("list = %+v %v", list, err)
	}
	// The conversation's agents read it by name and see it listed.
	got, err := use(t, p, req, platform.ToolFileRead, map[string]any{"path": "scratch:launch plan"})
	if err != nil || got != "# Ship Tuesday" {
		t.Fatalf("file_read = %q %v", got, err)
	}
	if got, err := use(t, p, req, platform.ToolFileList, nil); err != nil || !strings.Contains(got, "scratch:Launch plan") {
		t.Fatalf("file_list = %q %v", got, err)
	}
	if _, err := use(t, p, req, platform.ToolFileRead, map[string]any{"path": "scratch:missing"}); !errors.Is(err, platform.ErrSandbox) {
		t.Fatalf("missing scratchpad = %v", err)
	}
	// A rename is read under the new name only.
	if _, err := p.UpdateScratchpad(t.Context(), operator, run.Thread, pad.ID, platform.ScratchpadInput{Name: "Plan", Content: "# Ship Wednesday", Version: pad.Version}); err != nil {
		t.Fatal(err)
	}
	if got, _ := use(t, p, req, platform.ToolFileRead, map[string]any{"path": "scratch:Plan"}); got != "# Ship Wednesday" {
		t.Fatalf("renamed = %q", got)
	}
	// Changing one needs the version read, and runs:write.
	if _, err := p.UpdateScratchpad(t.Context(), operator, run.Thread, pad.ID, platform.ScratchpadInput{Name: "Plan", Content: "x"}); !errors.Is(err, platform.ErrWorkspaceConflict) {
		t.Fatalf("no version = %v", err)
	}
	reader := auth.Principal{Subject: "opal", Tenant: tenant, Kind: auth.User, Scopes: []auth.Permission{auth.RunsRead}}
	if _, err := p.CreateScratchpad(t.Context(), reader, run.Thread, platform.ScratchpadInput{Name: "x"}); err == nil {
		t.Fatal("a reader created a scratchpad")
	}
	// Another workspace sees no such conversation.
	other := auth.Principal{Subject: "opal", Tenant: "elsewhere", Kind: auth.User, Roles: []auth.Role{auth.Operator}}
	if _, err := p.ConversationScratchpad(t.Context(), other, run.Thread, pad.ID); !errors.Is(err, threads.ErrNotFound) {
		t.Fatalf("another tenant = %v", err)
	}
	if err := p.DeleteScratchpad(t.Context(), operator, run.Thread, pad.ID, 1); err != nil {
		t.Fatal(err)
	}
}

func TestArchivedConversationScratchpadsAreReadOnly(t *testing.T) {
	p := start(t, call.Limits{})
	th := startThread(t, p, operator, platform.ThreadInput{Title: "Notes", Text: "Notes", PostOnly: true, Visibility: threads.Private}).Thread
	if _, err := p.CreateScratchpad(t.Context(), operator, th.ID, platform.ScratchpadInput{Name: "Kept"}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.ArchiveThread(operator, th.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, err := p.CreateScratchpad(t.Context(), operator, th.ID, platform.ScratchpadInput{Name: "New"}); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("archived create = %v", err)
	}
	list, err := p.ConversationScratchpads(t.Context(), operator, th.ID)
	if err != nil || list.Writable || len(list.Scratchpads) != 1 {
		t.Fatalf("archived list = %+v %v", list, err)
	}
}
