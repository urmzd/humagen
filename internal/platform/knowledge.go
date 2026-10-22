package platform

import (
	"fmt"
	"slices"

	"github.com/urmzd/fiber/pkg/agentrt"
	"github.com/urmzd/fiber/pkg/envelope"
	"github.com/urmzd/fiber/pkg/runtime"

	"github.com/urmzd/warp/pkg/auth"
	"github.com/urmzd/warp/pkg/call"
	"github.com/urmzd/warp/pkg/knowledge"
	"github.com/urmzd/warp/pkg/projects"
	"github.com/urmzd/warp/pkg/runs"
)

// Knowledge sources inherited down the tree. A project names knowledge
// sources, and a run filed in it is grounded in them, and in those of every
// project above it, besides its agent's own:
//
//   - at admission, pin writes the chain's sources into the run's tag
//     (runs.MetaSources), nearest first, each once, at most
//     projects.MaxRunKnowledge, so every attempt of the run, after a restart
//     too, is grounded in the same sources whatever the projects name later;
//   - before fiber reads the pinned revision, resolveRevision adds them to the
//     request's copy of its Sources, which fiber's knowledge hook turns into
//     the run's retriever, searched in the run's own tenant.
//
// So a project's sources reach only the runs filed in it and below it: a
// sibling's, a child's and a deleted project's never do. A source is the
// workspace's, read with knowledge:read wherever it is named, so naming one
// in a project takes knowledge:read too, and a reader without it is shown no
// source's name.

// sourceURI is how a source a project names is written in a revision's
// Sources, as the console writes an agent's.
func sourceURI(name string) string { return "knowledge://" + name }

// sourcesFor is the knowledge sources a run is given for where it is filed:
// its project's chain's (projects.Tree.Knowledge), read once, at admission. A
// called run is given its root's, as it counts toward its root's ancestors,
// so a whole conversation is grounded alike. A housekeeping run is given
// none: it is filed at the workspace whatever turn it serves.
func (p *Platform) sourcesFor(tag envelope.Tag, edge call.Edge, project string) []string {
	if tag.Origin == runs.OriginSystem {
		return nil
	}
	if edge.Parent != "" {
		if root, err := p.Runs.Get(tag.Tenant, runtime.RunID(tag.Tenant, edge.Root)); err == nil {
			return runs.SourcesOf(root.Tag().Meta)
		}
	}
	var out []string
	for _, s := range p.Projects.Tree(tag.Tenant).Knowledge(project) {
		out = append(out, s.Name)
	}
	return out
}

// resolveRevision puts in place, on the request's copy of the pinned
// revision, what the platform decides about it before fiber reads it: the
// workspace's model policy for the one the agent names (resolveModel), and the
// knowledge sources the run was pinned with (withSources). The pinned
// definition itself is never changed.
func (p *Platform) resolveRevision(req runtime.Request) (runtime.Request, error) {
	req, err := p.resolveModel(req)
	if err != nil {
		return req, err
	}
	return withSources(req), nil
}

// withSources adds the knowledge sources a run's tag was pinned with to its
// revision's Sources, after the agent's own and each once.
func withSources(req runtime.Request) runtime.Request {
	pinned := runs.SourcesOf(req.Meta)
	if len(pinned) == 0 {
		return req
	}
	spec := req.Revision.Spec
	sources := slices.Clone(spec.Sources)
	for _, name := range pinned {
		if !slices.ContainsFunc(sources, func(s agentrt.Source) bool { return s.Name == name }) {
			sources = append(sources, agentrt.Source{Name: name, URI: sourceURI(name)})
		}
	}
	spec.Sources = sources
	req.Revision.Spec = spec
	return req
}

// ProjectKnowledge is the knowledge sources a project names, at the project's
// version, and what a run filed in it is given.
type ProjectKnowledge struct {
	Version int `json:"version"`
	// Knowledge is the sources the project names itself, in its order.
	Knowledge []string `json:"knowledge"`
	// Given is what a run filed here is given besides its agent's own: the
	// project's sources, then each ancestor's, nearest first, each once and
	// from the nearest project that names it, at most
	// projects.MaxRunKnowledge.
	Given []projects.KnowledgeSource `json:"given"`
}

// KnowledgeInput replaces the knowledge sources a project names, from the
// version read.
type KnowledgeInput struct {
	Version   int      `json:"version"`
	Knowledge []string `json:"knowledge"`
}

// ProjectKnowledgeAs is a project's knowledge sources, "~" for the
// workspace, as who reads them: a reader who may not read knowledge is shown
// none.
func (p *Platform) ProjectKnowledgeAs(who auth.Principal, project string) (ProjectKnowledge, error) {
	name := projects.NameOf(project)
	tree := p.Projects.Tree(who.Tenant)
	pr, ok := tree.Get(name)
	if !ok {
		return ProjectKnowledge{}, fmt.Errorf("%w: %q", projects.ErrNotFound, project)
	}
	seer := p.Seer(who)
	out := ProjectKnowledge{Version: pr.Version, Knowledge: seer.Sources(pr.Knowledge), Given: []projects.KnowledgeSource{}}
	if out.Knowledge == nil {
		out.Knowledge = []string{}
	}
	if seer.Knowledge() {
		out.Given = append(out.Given, tree.Knowledge(name)...)
	}
	return out, nil
}

// SetProjectKnowledge replaces the knowledge sources a project names, "~" for
// the workspace. Naming sources, or changing which are named, takes
// knowledge:read, which reads them anyway (403), and each must be a source
// the workspace has (400). The rest is the store's: the version read, the
// file's fields, and projects:govern for the workspace and company projects.
// The caller holds ownership of the project.
func (p *Platform) SetProjectKnowledge(who auth.Principal, project string, w projects.Writer, in KnowledgeInput) (ProjectKnowledge, error) {
	if !p.Seer(who).Knowledge() {
		return ProjectKnowledge{}, fmt.Errorf("%w: naming a project's knowledge sources takes %s", ErrForbidden, auth.KnowledgeRead)
	}
	have := p.Knowledge.List(who.Tenant)
	for _, n := range in.Knowledge {
		if !projects.ValidSource(n) {
			continue // the store says what is wrong with it
		}
		if !slices.ContainsFunc(have, func(s knowledge.Source) bool { return s.Name == n }) {
			return ProjectKnowledge{}, fmt.Errorf("%w: there is no knowledge source called %q", projects.ErrInvalid, n)
		}
	}
	if _, err := p.Projects.SetKnowledge(who.Tenant, projects.NameOf(project), in.Version, in.Knowledge, w, p.now()); err != nil {
		return ProjectKnowledge{}, err
	}
	return p.ProjectKnowledgeAs(who, project)
}

// Knowledge reports whether the reader sees the workspace's knowledge
// sources: with knowledge:read, which every route that reads one takes. A
// source lives in no project, so the permission that reads it is the rule.
func (s *Seer) Knowledge() bool { return s.who.Can(auth.KnowledgeRead) }

// Sources is a list of knowledge sources as the reader is shown it: whole to
// a reader who sees knowledge, and nothing to anyone else.
func (s *Seer) Sources(names []string) []string {
	if !s.Knowledge() {
		return nil
	}
	return slices.Clone(names)
}
