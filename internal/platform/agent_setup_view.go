package platform

import (
	"context"

	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/operations"
)

// AgentSetupView distinguishes a ready agent without a wizard record from an
// unknown agent. Mutation methods continue requiring an owned stored setup.
func (p *Platform) AgentSetupView(who auth.Principal, name string) (AgentSetup, error) {
	record, exists, err := p.setups.get(who.Tenant, name)
	if err != nil {
		return AgentSetup{}, err
	}
	if exists && (record.Phase != "ready" || record.Owner == who.Subject) {
		return p.AgentSetup(who, name)
	}
	if !p.Seer(who).Agent(name) {
		return AgentSetup{}, operations.ErrNotFound
	}
	rev, err := p.Catalog().Get(context.Background(), who.Tenant, name, 0)
	if err != nil {
		return AgentSetup{}, operations.ErrNotFound
	}
	rev = p.Seer(who).Revision(rev)
	return AgentSetup{Name: name, Phase: "ready", Revision: rev.Rev, RevisionHash: rev.Hash, Plan: AgentSetupPlan{Spec: rev.Spec}, CreatedAt: rev.CreatedAt, UpdatedAt: rev.CreatedAt}, nil
}
