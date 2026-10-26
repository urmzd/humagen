package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/urmzd/fiber/pkg/agentspec"
	"github.com/urmzd/fiber/pkg/mcptools"
	"github.com/urmzd/fiber/pkg/registry"
	"github.com/urmzd/humagen/pkg/audit"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/credentials"
	"github.com/urmzd/humagen/pkg/inbox"
	"github.com/urmzd/humagen/pkg/knowledge"
	"github.com/urmzd/humagen/pkg/projects"
)

// SourceHealth is safe for internal assignment admission. Public callers must filter source access first.

// SourceVisible applies the same project confinement as other project objects.

// SourcesAs returns a bounded, confined source inventory.
func (p *Platform) SourcesAs(who auth.Principal) []knowledge.Source {
	out := []knowledge.Source{}
	for _, src := range p.Knowledge.List(who.Tenant) {
		if p.sourceRecordVisible(who, src) {
			out = append(out, src)
			if len(out) == 200 {
				break
			}
		}
	}
	return out
}

// ConfigureSource never treats entering an origin as a successful connection.
func (p *Platform) ConfigureSource(who auth.Principal, name string, cfg knowledge.SourceConfig) (knowledge.Source, error) {
	current, readErr := p.SourceAs(who, name)
	if readErr != nil {
		return knowledge.Source{}, knowledge.ErrNotFound
	}
	var configErr error
	cfg, configErr = p.authorizeSourceConfig(who, cfg)
	if configErr != nil {
		return knowledge.Source{}, configErr
	}
	var expected *knowledge.SourceConfig
	if current.Lifecycle != nil {
		copy := current.Lifecycle.SourceConfig
		expected = &copy
	}
	attempt, err := p.beginSourceAudit(who, "source.configure", current, map[string]string{"mode": string(cfg.Mode), "provider": cfg.Provider, "origin": cfg.Origin, "project": cfg.Project})
	if err != nil {
		return knowledge.Source{}, err
	}
	result, err := p.Knowledge.ConfigureIfCurrent(who.Tenant, name, cfg, p.now(), expected)
	p.finishSourceAudit(attempt, err)
	return result, err
}

func (p *Platform) sourceProvider(kind string) knowledge.Provider {
	if provider := p.cfg.Ext.SourceProviders[kind]; provider != nil {
		return provider
	}
	if kind == "drive" || kind == "notion" {
		return knowledge.HTTPProvider{Kind: kind}
	}
	return nil
}

// sourceCredentialID binds consent to the selected provider, origin and project.
// A changed source cannot silently reuse a credential supplied for another resource.
func sourceCredentialID(src knowledge.Source) string {
	lc := src.Lifecycle
	hash := sha256.Sum256([]byte(src.Name + "\x00" + lc.Provider + "\x00" + lc.Origin + "\x00" + lc.Project))
	return "hsrc-" + hex.EncodeToString(hash[:13])
}

// ConnectSource stores a reference for this person only. It resolves nothing until use.
func (p *Platform) ConnectSource(who auth.Principal, name, ref string) error {
	src, readErr := p.SourceAs(who, name)
	if readErr != nil {
		return knowledge.ErrNotFound
	}
	if who.Kind == auth.Machine || who.Subject == "" || !p.refAllowed(who.Tenant, who.Subject, ref) {
		return errPersonRef
	}
	if src.Lifecycle == nil || p.sourceProvider(src.Lifecycle.Provider) == nil {
		return fmt.Errorf("%w: configure an external source first", knowledge.ErrInvalid)
	}
	attempt, err := p.BeginAudit(who, audit.Change{Action: "source.reference.save", Resource: "source_connection", ID: name, Scope: "personal", Subject: who.Subject, After: map[string]string{"reference_saved": "true"}})
	if err != nil {
		return err
	}
	_, err = p.Credentials.Put(who.Tenant, who.Subject, sourceCredentialID(src), credentials.Input{Ref: ref}, p.now())
	p.finishSourceAudit(attempt, err)
	return err
}

func (p *Platform) sourceSecret(ctx context.Context, who auth.Principal, src knowledge.Source) (mcptools.Secret, error) {
	if who.Kind == auth.Machine || who.Subject == "" {
		return mcptools.Secret{}, knowledge.ProviderError{Cause: knowledge.AuthExpired}
	}
	c, ok := p.Credentials.Get(who.Tenant, who.Subject, sourceCredentialID(src))
	if !ok || c.Kind != credentials.Reference || !p.refAllowed(who.Tenant, who.Subject, c.Ref) {
		return mcptools.Secret{}, knowledge.ProviderError{Cause: knowledge.AuthExpired}
	}
	resolver := p.cfg.Ext.Resolver
	if resolver == nil {
		resolver = mcptools.EnvResolver()
	}
	secret, err := resolver.Resolve(ctx, c.Ref)
	if ctx.Err() != nil {
		return mcptools.Secret{}, knowledge.ProviderError{Cause: knowledge.Interrupted}
	}
	if err != nil || secret.Empty() {
		return mcptools.Secret{}, knowledge.ProviderError{Cause: knowledge.AuthExpired}
	}
	return secret, nil
}

// SyncSource publishes a full snapshot only after every provider read succeeds.
func (p *Platform) SyncSource(ctx context.Context, who auth.Principal, name string) (knowledge.Source, error) {
	return p.syncSource(ctx, who, name, false)
}
func (p *Platform) syncSource(ctx context.Context, who auth.Principal, name string, replace bool) (knowledge.Source, error) {
	return p.syncSourceExpected(ctx, who, name, replace, nil)
}
func (p *Platform) syncSourceExpected(ctx context.Context, who auth.Principal, name string, replace bool, expected *knowledge.SourceConfig) (result knowledge.Source, err error) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	src, readErr := p.SourceAs(who, name)
	if readErr != nil {
		return knowledge.Source{}, knowledge.ErrNotFound
	}
	if !who.Can(auth.KnowledgeWrite) {
		return knowledge.Source{}, auth.ErrForbidden
	}
	if src.Lifecycle == nil || src.Lifecycle.Mode == knowledge.Live {
		return knowledge.Source{}, fmt.Errorf("%w: indexed or hybrid source required", knowledge.ErrInvalid)
	}
	if expected != nil && !src.Lifecycle.Equal(*expected) {
		return knowledge.Source{}, knowledge.ErrExists
	}
	if src.Lifecycle.Snapshot && !replace {
		return knowledge.Source{}, fmt.Errorf("%w: snapshot is frozen; choose Replace With New Version", knowledge.ErrInvalid)
	}
	if replace && !src.Lifecycle.Snapshot {
		return knowledge.Source{}, fmt.Errorf("%w: replacement requires a frozen snapshot", knowledge.ErrInvalid)
	}
	provider := p.sourceProvider(src.Lifecycle.Provider)
	if provider == nil {
		return knowledge.Source{}, fmt.Errorf("%w: provider is not registered", knowledge.ErrInvalid)
	}
	attempt, auditErr := p.beginSourceAudit(who, "source.refresh", src, map[string]string{"requested": "true"})
	if auditErr != nil {
		return knowledge.Source{}, auditErr
	}
	defer func() {
		outcome := err
		if outcome == nil && result.Lifecycle != nil && result.Lifecycle.Status == knowledge.NeedsAttention {
			outcome = knowledge.ProviderError{Cause: result.Lifecycle.Cause}
		}
		p.finishSourceAudit(attempt, outcome)
	}()

	id, err := p.Knowledge.BeginSyncIfCurrent(who.Tenant, name, p.now(), &src.Lifecycle.SourceConfig)
	if err != nil {
		return knowledge.Source{}, err
	}
	secret, fetchErr := p.sourceSecret(ctx, who, src)
	var docs []knowledge.ImportDocument
	if fetchErr == nil {
		if len(src.Lifecycle.Resources) > 0 {
			if selected, ok := provider.(knowledge.ResourceProvider); ok {
				docs, fetchErr = selected.FetchSelected(ctx, src.Lifecycle.Origin, src.Lifecycle.Resources, secret)
			} else {
				fetchErr = knowledge.ProviderError{Cause: knowledge.Unsupported}
			}
		} else {
			docs, fetchErr = provider.Fetch(ctx, src.Lifecycle.Origin, secret)
		}
	}
	if fetchErr != nil {
		failed, err := p.Knowledge.FailSync(who.Tenant, name, id, knowledge.FailureCause(fetchErr), p.now())
		if err == nil {
			p.sourceAttention(who.Tenant, failed)
		}
		return failed, err
	}
	if err := p.Knowledge.IndexSync(who.Tenant, name, id, p.now()); err != nil {
		return knowledge.Source{}, err
	}
	result, err = p.Knowledge.CompleteSync(who.Tenant, name, id, docs, who.Subject, p.now())
	if err != nil {
		failed, failErr := p.Knowledge.FailSync(who.Tenant, name, id, knowledge.Unsupported, p.now())
		if failErr != nil {
			return knowledge.Source{}, failErr
		}
		p.sourceAttention(who.Tenant, failed)
		if errors.Is(err, knowledge.ErrInvalid) {
			return failed, nil
		}
	}
	if err == nil {
		p.settleSourceAttention(who.Tenant, result)
	}
	return result, err
}

// SearchSources searches only visible sources. Live data is freshly fetched using
// this reader's credential, never the source owner's or the workspace's.
type SourceSearchWarning struct {
	Source string          `json:"source"`
	Cause  knowledge.Cause `json:"cause"`
}
type SourceSearch struct {
	Hits     []knowledge.Hit       `json:"hits"`
	Warnings []SourceSearchWarning `json:"warnings"`
}

func (p *Platform) SearchSources(ctx context.Context, who auth.Principal, names []string, query, variant string) ([]knowledge.Hit, error) {
	out, err := p.SearchSourcesDetailed(ctx, who, names, query, variant)
	return out.Hits, err
}

func (p *Platform) SearchSourcesDetailed(ctx context.Context, who auth.Principal, names []string, query, variant string) (SourceSearch, error) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	out := SourceSearch{Hits: []knowledge.Hit{}, Warnings: []SourceSearchWarning{}}
	if len(names) > 50 {
		return out, fmt.Errorf("%w: at most 50 sources", knowledge.ErrInvalid)
	}
	if len(names) == 0 {
		for _, s := range p.SourcesAs(who) {
			names = append(names, s.Name)
			if len(names) == 50 {
				break
			}
		}
	}
	indexed := []string{}
	expected := map[string]*knowledge.SourceConfig{}
	hits := []knowledge.Hit{}
	for _, name := range names {
		src, err := p.SourceAs(who, name)
		if err != nil {
			return out, err
		}
		if src.Lifecycle == nil || src.Lifecycle.Mode != knowledge.Live {
			indexed = append(indexed, name)
			if src.Lifecycle != nil {
				cfg := src.Lifecycle.SourceConfig
				expected[name] = &cfg
			} else {
				expected[name] = nil
			}
		}
		if src.Lifecycle == nil || src.Lifecycle.Mode == knowledge.Indexed {
			continue
		}
		found, err := p.searchSourceLive(ctx, who, src, query)
		if err != nil {
			if errors.Is(err, knowledge.ErrInvalid) {
				return out, err
			}
			out.Warnings = append(out.Warnings, SourceSearchWarning{Source: name, Cause: knowledge.FailureCause(err)})
			continue
		}
		hits = append(hits, found...)
	}

	if len(indexed) > 0 {
		found, err := p.Knowledge.SearchForScopes(who.Tenant, indexed, query, 6, variant, expected)
		if err != nil {
			return out, err
		}
		for _, hit := range found {
			hit.Mode = knowledge.Indexed
			hits = append(hits, hit)
		}
	}
	slices.SortStableFunc(hits, func(a, b knowledge.Hit) int {
		if a.Score > b.Score {
			return -1
		}
		if a.Score < b.Score {
			return 1
		}
		return 0
	})
	out.Hits = hits[:min(len(hits), 12)]
	return out, nil
}

func sourceAttentionKey(name string, cause knowledge.Cause) string {
	return "source:" + name + ":" + string(cause)
}
func (p *Platform) sourceAttention(tenant string, src knowledge.Source) {
	lc := src.Lifecycle
	if lc == nil || lc.Cause == "" {
		return
	}
	p.raiseInbox(tenant, sourceAttentionKey(src.Name, lc.Cause), inbox.Message{
		Kind: inbox.KindSource, Subject: "Source needs attention: " + src.Name, From: inbox.From{Kind: "system", Name: SystemSender}, Severity: inbox.Warning,
		Source: inbox.Source{Knowledge: src.Name, Project: lc.Project},
		Parts:  []inbox.Part{{Type: inbox.PartSource, Source: &inbox.SourcePart{Name: src.Name, Cause: string(lc.Cause)}}},
	}, p.now())
}
func (p *Platform) settleSourceAttention(tenant string, src knowledge.Source) {
	for _, cause := range []knowledge.Cause{knowledge.AuthExpired, knowledge.Quota, knowledge.Unsupported, knowledge.Unavailable, knowledge.Interrupted} {
		if m, ok := p.Inbox.ByKey(tenant, sourceAttentionKey(src.Name, cause)); ok && m.State != inbox.Resolved && m.State != inbox.Dismissed {
			for _, part := range m.Parts {
				if part.Type == inbox.PartSource {
					_, _, _ = p.Inbox.Resolve(tenant, m.ID, part.ID, inbox.Outcome{Action: "retry", By: "system", At: p.now(), Note: "Source refreshed"})
				}
			}
		}
	}
}

// runtimeSourceAllowed confines grounding to the run's project chain. It is
// independent of any source names supplied by an incoming message.

// SourceDependencies is a bounded projection of visible declarations, never a
// count of hidden projects or agents. Readers need each directory permission.
type SourceDependencies struct {
	Agents   []string `json:"agents"`
	Projects []string `json:"projects"`
}

func (p *Platform) SourceDependenciesAs(ctx context.Context, who auth.Principal, name string) (SourceDependencies, error) {
	out := SourceDependencies{Agents: []string{}, Projects: []string{}}
	if !p.SourceVisible(who, name) {
		return out, knowledge.ErrNotFound
	}
	seer := p.Seer(who)
	if who.Can(auth.ProjectsRead) {
		tree := p.Projects.Tree(who.Tenant)
		for _, project := range append([]projects.Project{tree.Root()}, tree.Projects()...) {
			if seer.Project(project.Name) && slices.ContainsFunc(tree.Knowledge(project.Name), func(src projects.KnowledgeSource) bool { return src.Name == name }) {
				out.Projects = append(out.Projects, project.Name)
				if len(out.Projects) == 200 {
					break
				}
			}
		}
	}
	if who.Can(auth.AgentsRead) {
		page := registry.Page{Size: 100}
		for range 2 {
			rows, next, err := p.Agents.List(ctx, who.Tenant, page)
			if err != nil {
				return out, err
			}
			for _, rev := range rows {
				if seer.Agent(rev.Spec.Name) && slices.ContainsFunc(rev.Spec.Sources, func(src agentspec.Source) bool { return src.Name == name }) {
					out.Agents = append(out.Agents, rev.Spec.Name)
				}
			}
			if next == "" {
				break
			}
			page.Token = next
		}
	}
	return out, nil
}

// searchSourceLive is the one provider/search composition used by the API and
// actor-bound runtime tool. The caller checks access before entering it.
func (p *Platform) searchSourceLive(ctx context.Context, who auth.Principal, src knowledge.Source, query string) ([]knowledge.Hit, error) {
	provider := p.sourceProvider(src.Lifecycle.Provider)
	if provider == nil {
		return nil, knowledge.ProviderError{Cause: knowledge.Unavailable}
	}
	secret, err := p.sourceSecret(ctx, who, src)
	if err != nil {
		return nil, err
	}
	docs, err := provider.Fetch(ctx, src.Lifecycle.Origin, secret)
	if err != nil {
		return nil, knowledge.ProviderError{Cause: knowledge.FailureCause(err)}
	}
	if src.Lifecycle.Mode == knowledge.Hybrid {
		documents, err := p.Knowledge.DocumentsForScope(who.Tenant, src.Name, &src.Lifecycle.SourceConfig)
		if err != nil {
			return nil, err
		}
		pinned := map[string]bool{}
		for _, doc := range documents {
			pinned[doc.ID] = true
		}
		docs = slices.DeleteFunc(slices.Clone(docs), func(doc knowledge.ImportDocument) bool { return pinned[doc.ID] })
		if docs == nil {
			docs = []knowledge.ImportDocument{}
		}
	}
	ephemeral := knowledge.NewStore()
	if _, err := ephemeral.Create(who.Tenant, src.Name, "", who.Subject, p.now()); err != nil {
		return nil, err
	}
	id, err := ephemeral.BeginSync(who.Tenant, src.Name, p.now())
	if err != nil {
		return nil, err
	}
	if _, err := ephemeral.CompleteSync(who.Tenant, src.Name, id, docs, who.Subject, p.now()); err != nil {
		return nil, err
	}
	hits, err := ephemeral.Search(who.Tenant, []string{src.Name}, query, 6, "")
	for i := range hits {
		hits[i].Mode = knowledge.Live
		hits[i].CopyAt = nil
		hits[i].RefreshFailed = false
	}
	return hits, err
}

func (p *Platform) authorizeSourceConfig(who auth.Principal, cfg knowledge.SourceConfig) (knowledge.SourceConfig, error) {
	if !who.Can(auth.KnowledgeWrite) {
		return knowledge.SourceConfig{}, auth.ErrForbidden
	}
	if cfg.Provider != "manual" && cfg.Project == "" {
		return knowledge.SourceConfig{}, fmt.Errorf("%w: choose a project, or ~ for the workspace", knowledge.ErrInvalid)
	}
	cfg.Project = projects.NameOf(cfg.Project)
	if cfg.Project != "" && (!p.Projects.Tree(who.Tenant).Has(cfg.Project) || !p.Sees(who, Place{Project: cfg.Project})) {
		return knowledge.SourceConfig{}, knowledge.ErrNotFound
	}
	if cfg.Provider != "manual" && p.sourceProvider(cfg.Provider) == nil {
		return knowledge.SourceConfig{}, fmt.Errorf("%w: provider is not registered", knowledge.ErrInvalid)
	}
	if cfg.Owner == "" {
		cfg.Owner = who.Subject
	}
	if cfg.SyncEverySeconds != 0 && !p.mayScheduleSource(who.Tenant, cfg.Owner, cfg.Project) {
		return knowledge.SourceConfig{}, fmt.Errorf("%w: automatic refresh requires current subject-based policy grants and project membership", auth.ErrForbidden)
	}
	if cfg.Owner != who.Subject {
		return knowledge.SourceConfig{}, fmt.Errorf("%w: connect a source as yourself", knowledge.ErrInvalid)
	}
	return cfg, nil
}
