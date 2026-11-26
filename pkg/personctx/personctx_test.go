package personctx

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/toolprovider"

	"github.com/urmzd/humagen/pkg/settings"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

var at = time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)

func input(version int) Input {
	one := 1
	return Input{
		Version: version,
		Run: settings.Layer{
			Ceilings:    settings.Ceilings{Tools: []string{"file_read"}, CallDepth: &one},
			Preferences: settings.Preferences{Model: "anthropic/claude", CompactionTokens: 40_000},
		},
		ToolServers: []ToolServer{{Name: "notes", URL: "https://mcp.example.com/notes", Credential: "env:HUMAGEN_PERSON_ADA_NOTES"}},
	}
}

func TestKeptAcrossRestartAndApartByTenantAndPerson(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		s, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		if got := get(t, s, "acme", "ada"); got.Version != 0 || got.ToolServers == nil || got.Memory != "" {
			t.Fatalf("before any write = %+v", got)
		}
		first, err := s.Set("acme", "ada", input(0), at)
		if err != nil {
			t.Fatal(err)
		}
		if first.Version != 1 || first.Memory == "" {
			t.Fatalf("first write = %+v", first)
		}
		second, err := s.Set("acme", "ada", Input{Version: 1}, at.Add(time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		if second.Memory != first.Memory || second.Version != 2 || len(second.ToolServers) != 0 {
			t.Fatalf("second write = %+v", second)
		}
		if _, err := s.Set("acme", "ada", input(1), at); !errors.Is(err, ErrVersion) {
			t.Fatalf("stale write = %v", err)
		}
		if _, err := s.Set("acme", "bob", input(0), at); err != nil {
			t.Fatal(err)
		}

		again, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		if got := get(t, again, "acme", "ada"); got.Version != 2 || got.Memory != first.Memory || got.Run.Preferences.Model != "" {
			t.Fatalf("after restart = %+v", got)
		}
		if got := get(t, again, "acme", "bob"); got.Version != 1 || got.Memory == first.Memory || len(got.ToolServers) != 1 {
			t.Fatalf("bob after restart = %+v", got)
		}
		if got := get(t, again, "globex", "ada"); got.Version != 0 || len(got.ToolServers) != 0 {
			t.Fatalf("another tenant sees %+v", got)
		}
	})
}

func TestAFailedWriteChangesNothing(t *testing.T) {
	f := statetest.NewFaulty(state.NewMemory())
	s, err := Open(f)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Set("acme", "ada", input(0), at); err != nil {
		t.Fatal(err)
	}
	f.Fail(statetest.ErrInjected)
	if _, err := s.Set("acme", "ada", Input{Version: 1}, at); !errors.Is(err, statetest.ErrInjected) {
		t.Fatalf("Set = %v", err)
	}
	if _, err := s.Set("acme", "new", input(0), at); !errors.Is(err, statetest.ErrInjected) {
		t.Fatalf("Set = %v", err)
	}
	if got := get(t, s, "acme", "ada"); got.Version != 1 || len(got.ToolServers) != 1 {
		t.Fatalf("after a failed write = %+v", got)
	}
	if got := get(t, s, "acme", "new"); got.Version != 0 || got.Memory != "" {
		t.Fatalf("a failed write kept %+v", got)
	}
}

func TestOnlyDefinitionsAndReferencesAreKept(t *testing.T) {
	s := NewStore()
	for name, srv := range map[string]ToolServer{
		"raw secret":        {Name: "a", URL: "https://x.test", Credential: "sk-live-123"},
		"password in url":   {Name: "a", URL: "https://me:pw@x.test"},
		"plain http":        {Name: "a", URL: "http://x.test"},
		"stdio command":     {Name: "a", URL: "npx some-server"},
		"bad name":          {Name: "A B", URL: "https://x.test"},
		"no address at all": {Name: "a"},
	} {
		if _, err := s.Set("acme", "ada", Input{ToolServers: []ToolServer{srv}}, at); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	twice := []ToolServer{{Name: "a", URL: "https://x.test"}, {Name: "a", URL: "https://y.test"}}
	if _, err := s.Set("acme", "ada", Input{ToolServers: twice}, at); !errors.Is(err, ErrInvalid) {
		t.Errorf("twice: %v", err)
	}
	bad := input(0)
	bad.Run.Preferences.Model = "no-provider"
	if _, err := s.Set("acme", "ada", bad, at); !errors.Is(err, ErrInvalid) {
		t.Errorf("bad model: %v", err)
	}
	if _, err := s.Set("acme", "", input(0), at); !errors.Is(err, ErrInvalid) {
		t.Errorf("no subject: %v", err)
	}
	if got := get(t, s, "acme", "ada"); got.Version != 0 {
		t.Fatalf("a refused write kept %+v", got)
	}
}

func get(t *testing.T, s *Store, tenant, subject string) Context {
	t.Helper()
	c, err := s.Get(tenant, subject)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func bigServer(t *testing.T, name string, tools int, schemaBytes, descBytes int) ToolServer {
	t.Helper()
	s := ToolServer{Name: name, URL: "https://" + name + ".example.com/mcp"}
	for i := range tools {
		schema := fmt.Sprintf(`{"type":"object","description":%q}`, strings.Repeat("s", schemaBytes))
		tool, err := toolprovider.NewTool(fmt.Sprintf("tool_%d", i), strings.Repeat("d", descBytes), json.RawMessage(schema))
		if err != nil {
			t.Fatal(err)
		}
		tool.Class = toolprovider.ClassRead
		s.Tools = append(s.Tools, tool)
	}
	return s
}

// Two Google Workspace sized lists (about 48 KiB and 27 KiB, mostly input
// schemas) fit together. Past the caps, a write is refused.
func TestToolServerBytes(t *testing.T) {
	mail, calendar := bigServer(t, "mail", 23, 1400, 560), bigServer(t, "calendar", 9, 2700, 120)
	if err := (Input{ToolServers: []ToolServer{mail, calendar}}).Check(); err != nil {
		t.Fatalf("two workspace lists: %v", err)
	}
	var many []ToolServer
	for i := range 12 {
		many = append(many, bigServer(t, fmt.Sprintf("s%d", i), 64, 600, 10))
	}
	if err := (Input{ToolServers: many}).Check(); !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "512 KiB") {
		t.Fatalf("over the written cap: %v", err)
	}
	var wordy []ToolServer
	for i := range 4 {
		wordy = append(wordy, bigServer(t, fmt.Sprintf("w%d", i), 64, 10, 800))
	}
	if err := (Input{ToolServers: wordy}).Check(); !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "192 KiB") {
		t.Fatalf("over the pinned cap: %v", err)
	}
}

func TestPinnedDropsOnlyInputSchemas(t *testing.T) {
	s := bigServer(t, "mail", 2, 50, 20)
	p := s.Pinned()
	for i, tool := range p.Tools {
		want := s.Tools[i]
		if tool.InputSchema != nil || tool.Name != want.Name || tool.Description != want.Description || tool.SchemaHash != want.SchemaHash || tool.Class != want.Class {
			t.Fatalf("pinned tool %d = %+v, want %+v without its schema", i, tool, want)
		}
	}
	if s.Tools[0].InputSchema == nil {
		t.Fatal("Pinned changed the server it was called on")
	}
	if (ToolServer{Name: "x"}).Pinned().Tools != nil {
		t.Fatal("an unapproved server became approved")
	}
}
