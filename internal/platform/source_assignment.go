package platform

import (
	"errors"
	"slices"

	"github.com/urmzd/fiber/pkg/runtime"
	"github.com/urmzd/humagen/pkg/knowledge"
	"github.com/urmzd/humagen/pkg/runs"
)

// ErrSourceStop requires operator repair and an explicit assignment resume.
// No source name or hidden dependency count is included in this error.
var ErrSourceStop = errors.New("platform: required source is unavailable or past its freshness stop rule")

// CheckAssignmentSourceHealth must run before withSources strips the private
// admission pin. It checks current source metadata and the exact pinned sources,
// not a fresh read of the project's mutable source selection.
func (p *Platform) CheckAssignmentSourceHealth(req runtime.Request) error {
	if req.Meta["humagen.assignment"] == "" {
		return nil
	}
	names := runs.SourcesOf(req.Meta)
	for _, source := range req.Revision.Spec.Sources {
		if !slices.Contains(names, source.Name) {
			names = append(names, source.Name)
		}
	}
	return p.CheckAssignmentSources(req.Tenant, names)
}

// CheckAssignmentSources returns a stop decision. It neither restarts work nor
// grants access; the assignment owner persists the pause until a person resumes.
func (p *Platform) CheckAssignmentSources(tenant string, names []string) error {
	if len(names) > 50 {
		return ErrSourceStop
	}
	for _, name := range names {
		source, err := p.Knowledge.Get(tenant, name)
		if err != nil || source.HardStale(p.now()) {
			return ErrSourceStop
		}
		if source.Documents == 0 && (source.Lifecycle == nil || source.Lifecycle.LastGoodAt == nil) {
			return ErrSourceStop
		}
		if source.Lifecycle != nil && source.Lifecycle.Provider != "manual" && source.Lifecycle.Mode != knowledge.Live && source.Lifecycle.LastGoodAt == nil {
			return ErrSourceStop
		}
		if source.Lifecycle != nil && source.Lifecycle.Mode == knowledge.Live && source.Lifecycle.Status == knowledge.NeedsAttention {
			return ErrSourceStop
		}
	}
	return nil
}

// SourceChangeVersion is resident durable state for the assignment reconciler.
// Unchanged refreshes never advance the cursor and no provider request is made.
func (p *Platform) SourceChangeVersion(tenant, name string) (uint64, bool) {
	source, err := p.Knowledge.Get(tenant, name)
	if err != nil {
		return p.Knowledge.ContentCursor(tenant, name), false
	}
	if source.Documents == 0 && (source.Lifecycle == nil || source.Lifecycle.LastGoodAt == nil) {
		return source.ContentVersion, false
	}
	if source.Lifecycle != nil && source.Lifecycle.Provider != "manual" && source.Lifecycle.Mode != knowledge.Live && source.Lifecycle.LastGoodAt == nil {
		return source.ContentVersion, false
	}
	if source.HardStale(p.now()) || (source.Lifecycle != nil && source.Lifecycle.Mode == knowledge.Live) {
		return source.ContentVersion, false
	}
	return source.ContentVersion, true
}
