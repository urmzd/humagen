package platform

import (
	"errors"

	"github.com/urmzd/humagen/internal/mcptools"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/runs"
)

// noteConnectionNeed records on a run, and on the turn it belongs to, that a
// tool group it was granted could not be opened because the person it acts
// for has not connected the provider (mcptools.ErrNoPersonCredential). The
// turn a person reads then offers to connect it, from this record rather
// than from what the model said. Any other error records nothing, and a
// failure to record is not the run's: the group is dropped either way.
func (p *Platform) noteConnectionNeed(tenant, runID, grant string, err error) {
	if !errors.Is(err, mcptools.ErrNoPersonCredential) {
		return
	}
	g, ok, perr := wire.ParseMCPGrant(grant)
	if !ok || perr != nil || g.Provider == "" {
		return
	}
	need := runs.ConnectionNeed{Provider: g.Provider, Name: g.Provider}
	if pre, known := preset(g.Provider); known {
		need.Name = pre.Name
	}
	_ = p.Runs.RecordConnectionNeed(tenant, runID, need)
	if run, gerr := p.Runs.Get(tenant, runID); gerr == nil && run.Root != "" && run.Root != runID {
		_ = p.Runs.RecordConnectionNeed(tenant, run.Root, need)
	}
}
