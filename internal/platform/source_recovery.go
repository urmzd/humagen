package platform

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/urmzd/humagen/pkg/audit"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/knowledge"
	"github.com/urmzd/humagen/pkg/projects"
)

func (p *Platform) mayScheduleSource(tenant, owner, project string) bool {
	who := p.byPolicy(tenant, owner)
	if !who.Can(auth.KnowledgeRead) || !who.Can(auth.KnowledgeWrite) {
		return false
	}
	reader := ReaderOf(who)
	reader.Govern = false
	return p.Projects.Tree(tenant).Resolve(project, reader).Role.AtLeast(projects.Member)
}
func (p *Platform) SourceResources(ctx context.Context, who auth.Principal, name string) ([]knowledge.Resource, error) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	src, err := p.SourceAs(who, name)
	if err != nil {
		return nil, err
	}
	if src.Lifecycle == nil {
		return nil, fmt.Errorf("%w: configure an external source first", knowledge.ErrInvalid)
	}
	provider, ok := p.sourceProvider(src.Lifecycle.Provider).(knowledge.ResourceProvider)
	if !ok {
		return nil, fmt.Errorf("%w: provider resource discovery is not supported", knowledge.ErrInvalid)
	}
	secret, err := p.sourceSecret(ctx, who, src)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", knowledge.ErrInvalid, knowledge.FailureCause(err))
	}
	resources, err := provider.Resources(ctx, src.Lifecycle.Origin, secret)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", knowledge.ErrInvalid, knowledge.FailureCause(err))
	}
	resources, err = knowledge.SafeResources(resources, secret)
	if err != nil {
		return nil, fmt.Errorf("%w: resource inventory is unsupported", knowledge.ErrInvalid)
	}
	if len(resources) > knowledge.MaxImportDocuments {
		return nil, fmt.Errorf("%w: resource inventory exceeds limit", knowledge.ErrInvalid)
	}
	current, err := p.SourceAs(who, name)
	if err != nil || current.Lifecycle == nil || !current.Lifecycle.Equal(src.Lifecycle.SourceConfig) {
		return nil, knowledge.ErrNotFound
	}
	if err := p.observeKnowledgeSource(who.Tenant, current); err != nil {
		return nil, err
	}
	return resources, nil
}
func (p *Platform) SnapshotSource(who auth.Principal, name string) (knowledge.Source, error) {
	src, err := p.SourceAs(who, name)
	if err != nil {
		return knowledge.Source{}, err
	}
	if src.Lifecycle == nil {
		return knowledge.Source{}, knowledge.ErrInvalid
	}
	cfg := src.Lifecycle.SourceConfig
	cfg.Snapshot = true
	cfg.Mode = knowledge.Indexed
	cfg.SyncEverySeconds = 0
	if cfg.Project == "" {
		cfg.Project = "~"
	}
	checked, err := p.authorizeSourceConfig(who, cfg)
	if err != nil {
		return knowledge.Source{}, err
	}
	attempt, err := p.beginSourceAudit(who, "source.snapshot", src, map[string]string{"snapshot": "true", "mode": "indexed", "project": checked.Project})
	if err != nil {
		return knowledge.Source{}, err
	}
	result, err := p.Knowledge.KeepSnapshot(who.Tenant, name, checked, p.now(), &src.Lifecycle.SourceConfig)
	p.finishSourceAudit(attempt, err)
	if err == nil {
		p.settleSourceAttention(who.Tenant, result)
	}
	return result, err
}
func (p *Platform) ReplaceSource(ctx context.Context, who auth.Principal, name string) (knowledge.Source, error) {
	return p.syncSource(ctx, who, name, true)
}
func (p *Platform) SetSourceSyncSettings(who auth.Principal, name string, every, stop int) (knowledge.Source, error) {
	src, err := p.SourceAs(who, name)
	if err != nil {
		return knowledge.Source{}, err
	}
	if src.Lifecycle == nil {
		return knowledge.Source{}, knowledge.ErrInvalid
	}
	cfg := src.Lifecycle.SourceConfig
	cfg.SyncEverySeconds = every
	cfg.StopAfterSeconds = stop
	if cfg.Project == "" {
		cfg.Project = "~"
	}
	return p.ConfigureSource(who, name, cfg)
}

func (p *Platform) CreateSource(who auth.Principal, name, description string, cfg *knowledge.SourceConfig) (result knowledge.Source, resultErr error) {
	if !who.Can(auth.KnowledgeWrite) {
		return result, auth.ErrForbidden
	}
	if !knowledge.ValidName(name) {
		return result, knowledge.ErrInvalid
	}
	project := ""
	mode, provider := "indexed", "manual"
	if cfg != nil {
		checked, err := p.authorizeSourceConfig(who, *cfg)
		if err != nil {
			return result, err
		}
		cfg = &checked
		project, mode, provider = checked.Project, string(checked.Mode), checked.Provider
	}
	attempt, err := p.BeginAudit(who, audit.Change{Action: "source.create", Resource: "source", ID: name, Scope: "project", Project: project, After: map[string]string{"mode": mode, "provider": provider, "project": project}})
	if err != nil {
		return result, err
	}
	defer func() { p.finishSourceAudit(attempt, resultErr) }()
	result, resultErr = p.Knowledge.CreateWithConfig(who.Tenant, name, description, who.Subject, p.now(), cfg)
	if errors.Is(resultErr, knowledge.ErrExists) && !p.SourceVisible(who, name) {
		return knowledge.Source{}, knowledge.ErrNotFound
	}
	return result, resultErr
}
