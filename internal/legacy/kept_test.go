package legacy_test

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/urmzd/humagen/fiber/pkg/runtime"
	"github.com/urmzd/humagen/internal/legacy"
	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/spec"
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
	if !slices.Equal(spec.BuiltinAgents, []string{"humagen-writer", "humagen-analyst", "humagen-builder"}) {
		t.Errorf("spec.BuiltinAgents = %q", spec.BuiltinAgents)
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
	"fiber_dispatch_runs",
	"fiber_event_runs",
	"fiber_event_tenants",
	"fiber_events",
	"fiber_knowledge_chunks",
	"fiber_knowledge_documents",
	"fiber_memberships",
	"fiber_run_queue",
	"fiber_run_records",
	"fiber_runs",
	"fiber_thread_entries",
	"fiber_threads",
	"fiber_tool_providers",
	"fiber_tool_snapshots",
}

var createTable = regexp.MustCompile(`CREATE TABLE IF NOT EXISTS (fiber_\w+)`)

// The tables are read from the stores' source, so a renamed or a new table
// fails here until it is listed.
func TestFiberTablesAreKept(t *testing.T) {
	root, err := filepath.Abs("../../fiber")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	err = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
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
	})
	if err != nil {
		t.Fatal(err)
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
