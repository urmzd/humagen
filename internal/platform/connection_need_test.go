package platform

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/urmzd/saige/flow/runtime"

	"github.com/urmzd/humagen/internal/mcptools"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/runs"
)

// A delegated run whose person had not connected a provider: the run and the
// turn it belongs to both say which connection it needed, by the provider's
// name, so the turn a person reads offers Connect for it.
func TestAToolGroupWithoutThePersonsConnectionIsRecordedOnTheRunAndItsTurn(t *testing.T) {
	at := time.Unix(1_800_000_000, 0)
	p := &Platform{Runs: runs.NewIndex()}
	root := wire.NewEvent(wire.Raise{ID: "root", Tenant: "acme", Agent: "lead", Origin: "api", Text: "check my repos", At: at})
	child, err := call.New(root, call.Request{Key: "k1", Agent: "github-reviewer", Text: "check CI", Mode: call.Delegate}, call.State{}, call.Limits{MaxDepth: 3, MaxFanOut: 3}, at)
	if err != nil {
		t.Fatal(err)
	}
	rootID, childID := runtime.RunID("acme", "root"), runtime.RunID("acme", child.ID)
	if err := p.Runs.Record(rootID, root, 1); err != nil {
		t.Fatal(err)
	}
	if err := p.Runs.Record(childID, child, 1); err != nil {
		t.Fatal(err)
	}
	missing := fmt.Errorf("mcptools: grant %q: %w", "mcp:github", mcptools.ErrNoPersonCredential)
	p.noteConnectionNeed("acme", childID, wire.MCPGrant{Provider: "github"}.String(), missing)
	p.noteConnectionNeed("acme", childID, wire.MCPGrant{Provider: "notion", Write: true}.String(), missing)
	want := []runs.ConnectionNeed{{Provider: "github", Name: "GitHub"}, {Provider: "notion", Name: "Notion"}}
	for _, id := range []string{childID, rootID} {
		r, err := p.Runs.Get("acme", id)
		if err != nil {
			t.Fatal(err)
		}
		if fmt.Sprint(r.NeedsConnection) != fmt.Sprint(want) {
			t.Fatalf("%s needs %+v, want %+v", id, r.NeedsConnection, want)
		}
	}
}

// Only a missing credential is a connection to make: an unreachable server, a
// grant that is not a tool group, or a run that is not recorded names none.
func TestOtherExpansionFailuresNameNoConnection(t *testing.T) {
	at := time.Unix(1_800_000_000, 0)
	p := &Platform{Runs: runs.NewIndex()}
	root := wire.NewEvent(wire.Raise{ID: "root", Tenant: "acme", Agent: "lead", Origin: "api", Text: "go", At: at})
	id := runtime.RunID("acme", "root")
	if err := p.Runs.Record(id, root, 1); err != nil {
		t.Fatal(err)
	}
	p.noteConnectionNeed("acme", id, "mcp:github", errors.New("mcptools: dial: connection refused"))
	p.noteConnectionNeed("acme", id, "file_read", fmt.Errorf("x: %w", mcptools.ErrNoPersonCredential))
	p.noteConnectionNeed("acme", "r-unknown", "mcp:github", fmt.Errorf("x: %w", mcptools.ErrNoPersonCredential))
	p.noteConnectionNeed("other", id, "mcp:github", fmt.Errorf("x: %w", mcptools.ErrNoPersonCredential))
	if r, _ := p.Runs.Get("acme", id); len(r.NeedsConnection) != 0 {
		t.Fatalf("needs = %+v", r.NeedsConnection)
	}
}
