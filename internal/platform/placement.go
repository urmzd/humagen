package platform

import "github.com/urmzd/humagen/pkg/runs"

// CurrentPlace resolves the authoritative current audience of a conversation.
// Run and artifact attribution remains immutable in the execution stores.
func (p *Platform) CurrentPlace(tenant string, at Place) Place {
	if at.Thread != "" {
		if th, err := p.Threads.Get(tenant, at.Thread); err == nil {
			at.Project = th.Project
		}
	}
	return at
}

// RunPlace returns the run's current conversation audience, without changing
// its immutable execution and accounting attribution.
func (p *Platform) RunPlace(tenant string, run runs.Run) Place {
	return p.CurrentPlace(tenant, placeOfRun(run))
}
