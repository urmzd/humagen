package legacy_test

import (
	"path/filepath"
	"slices"
	"testing"

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
