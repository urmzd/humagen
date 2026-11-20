package legacy_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/urmzd/humagen/internal/legacy"
	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/runschema"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/spec"
	"github.com/urmzd/saige/flow/runtime"
)

// These names are stored in run records, schedules, the ledger and the
// registry, so the rename keeps them. A change here strands stored data.
func TestStoredNamesAreKept(t *testing.T) {
	for got, want := range map[string]string{
		runs.MetaThread:      "warp.thread",
		runs.MetaPrev:        "warp.prev",
		runs.MetaAsk:         "warp.ask",
		runs.MetaPicked:      "warp.picked",
		runs.MetaProject:     "warp.project",
		runs.MetaRouted:      "_warp.routed",
		runs.MetaChannel:     "warp.channel",
		runs.MetaMessage:     "warp.message",
		runs.MetaWithin:      "warp.within",
		runs.MetaSources:     "_warp.sources",
		runs.MetaSourcesV0:   "warp.sources",
		runs.MetaSkills:      "_warp.skills",
		runs.MetaClient:      "warp.client",
		call.MetaInput:       "warp.call_input",
		platform.MetaDeliver: "warp.deliver",
	} {
		if got != want {
			t.Errorf("kept name is %q, want %q", got, want)
		}
	}
	// Attachments and files stored records point at live here.
	if got := legacy.WorkspaceDir("/cache"); got != filepath.Join("/cache", "warp", "workspace") {
		t.Errorf("WorkspaceDir = %q", got)
	}
	// The first two are stored names; agents added later come after them.
	if len(spec.BuiltinAgents) < 2 || !slices.Equal(spec.BuiltinAgents[:2], []string{"humagen-writer", "humagen-analyst"}) {
		t.Errorf("spec.BuiltinAgents = %q", spec.BuiltinAgents)
	}
	// The builder's stored names read as the entry agent, which took over its work.
	for _, old := range []string{"humagen-builder", "warp-builder"} {
		if got := legacy.BuiltinAgent(old); got != "humagen" {
			t.Errorf("%s reads as %q", old, got)
		}
	}
	if got := legacy.BuiltinAgentKey("humagen"); got != "warp-builder" {
		t.Errorf("the entry agent's color key = %q, want the builder's oldest", got)
	}
}

// Fiber's stored names. A table, an activity or a record key here is what a
// database or a run in flight holds, so a change strands it.
func TestFiberStoredNamesAreKept(t *testing.T) {
	for got, want := range map[string]string{
		runtime.ActivityTurn:         "fiber.turn.v2",
		runtime.ActivityAgent:        "fiber.turn.agent.v2",
		runtime.ActivityResume:       "fiber.turn.resume.v2",
		runtime.ActivityTurnV1:       "fiber.turn",
		runtime.ActivityAgentV1:      "fiber.turn.agent",
		runtime.ActivityResumeV1:     "fiber.turn.resume",
		runtime.RecordStop:           "fiber.stop",
		runtime.RecordQuestionPrefix: "fiber.question.",
		runtime.RecordAnswerPrefix:   "fiber.answer.",
		runtime.RecordChildPrefix:    "fiber.child.",
		runtime.RecordUsagePrefix:    "fiber.usage.a",
	} {
		if got != want {
			t.Errorf("kept name is %q, want %q", got, want)
		}
	}
}

// keptFiberTables are the tables fiber's stores create. Each is in
// docs/guides/upgrading-from-warp.md.
var keptFiberTables = []string{
	"fiber_consumers",
	"fiber_definition_retirements",
	"fiber_definitions",
	"fiber_event_runs",
	"fiber_event_tenants",
	"fiber_events",
	"fiber_memberships",
	"fiber_run_queue",
	"fiber_run_records",
	"fiber_runs",
	"fiber_session_budgets",
	"fiber_session_grants",
	"fiber_session_links",
	"fiber_session_reservations",
	"fiber_session_runs",
	"fiber_session_totals",
	"fiber_session_usage_ids",
	"fiber_sessions",
	"fiber_sink_state",
	"fiber_thread_entries",
	"fiber_threads",
	"fiber_tool_providers",
	"fiber_tool_snapshots",
}

// The new prefixes are matched too, so a table renamed to one of them fails
// here instead of silently leaving the stored fiber_ table behind.
var createTable = regexp.MustCompile(`CREATE TABLE IF NOT EXISTS ((?:fiber|flow|durable|sandbox)_\w+)`)

// The tables are read from the stores' source: flow (which was fiber) in the
// saige version go.mod pins, and Humagen's own run schema and tool provider
// store, so a renamed or a new table fails here until it is listed.
func TestFiberTablesAreKept(t *testing.T) {
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", "github.com/urmzd/saige").Output()
	if err != nil {
		t.Fatalf("locate saige: %v", err)
	}
	dir := strings.TrimSpace(string(out))
	if dir == "" {
		t.Fatal("saige is not downloaded: run go mod download")
	}
	seen := map[string]bool{}
	walk := func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		for _, m := range createTable.FindAllStringSubmatch(string(b), -1) {
			seen[m[1]] = true
		}
		return nil
	}
	for _, root := range []string{filepath.Join(dir, "flow"), filepath.Join("..", "runschema"), filepath.Join("..", "toolprovider")} {
		if err := filepath.WalkDir(root, walk); err != nil {
			t.Fatal(err)
		}
	}
	var got []string
	for name := range seen {
		got = append(got, name)
	}
	sort.Strings(got)
	if !slices.Equal(got, keptFiberTables) {
		t.Errorf("fiber's stores create %q, want %q", got, keptFiberTables)
	}
	doc, err := os.ReadFile("../../docs/guides/upgrading-from-warp.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range keptFiberTables {
		if !strings.Contains(string(doc), "`"+name+"`") {
			t.Errorf("docs/guides/upgrading-from-warp.md does not list %s", name)
		}
	}
}

// The run ledger and queue are opened only through runschema.Open, which maps
// durable's Postgres adapters (whose own defaults are durable_*) onto these
// tables. A deployment's existing runs are read from them.
func TestTheRunTablesKeepTheirFiberNames(t *testing.T) {
	for got, want := range map[string]string{
		runschema.RunsTable:    "fiber_runs",
		runschema.RecordsTable: "fiber_run_records",
		runschema.QueueTable:   "fiber_run_queue",
	} {
		if got != want {
			t.Errorf("runschema names %q, want %q", got, want)
		}
	}
}
