package platform

import (
	"context"

	"github.com/urmzd/humagen/internal/wire"
)

// availableEntry only selects a platform package that is available here.
func (p *Platform) availableEntry(ctx context.Context, tenant, project string) string {
	r, err := p.Catalog().Get(ctx, tenant, p.cfg.EntryAgent, 0)
	if err != nil || !p.isEntry(r) || !p.ProjectTree(tenant).IsAvailable(project, r.Spec.Name) {
		return ""
	}
	return r.Spec.Name
}

func (p *Platform) isEntry(r wire.Revision) bool {
	return r.Spec.Name == p.cfg.EntryAgent && r.Spec.Package != nil && r.Spec.Package.Name == "humagen/core"
}

// entrySpawn resolves the platform entry's grant against its current place.
// Ordinary and customized revisions retain their explicit, default-deny list.
// Callee policy, budget, operation permissions and call limits still apply.
func (p *Platform) entrySpawn(ctx context.Context, tenant, project string, r wire.Revision) ([]string, error) {
	if !p.isEntry(r) || r.CreatedBy != "platform" {
		return r.Spec.Access.Spawn, nil
	}
	tree := p.ProjectTree(tenant)
	var names []string
	page := wire.Page{Size: 100}
	for {
		rows, next, err := p.Catalog().List(ctx, tenant, page)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			name := row.Spec.Name
			if name != r.Spec.Name && tree.IsAvailable(project, name) {
				names = append(names, name)
			}
		}
		if next == "" {
			return CurrentAgentNames(names), nil
		}
		page.Token = next
	}
}
