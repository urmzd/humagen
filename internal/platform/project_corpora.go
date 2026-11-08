package platform

import (
	"fmt"
	"slices"

	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/projects"
)

// ProjectCorpus is one corpus a project attaches, as its reader is shown it.
type ProjectCorpus struct {
	Name  string `json:"name"`
	Title string `json:"title,omitempty"`
	// Sources is how many of its members the reader may see.
	Sources int `json:"sources"`
}

// ProjectCorpora is a project's context as a person reads it: the corpora it
// attaches, and what a session filed there searches, the project's own
// first and its parent's chain after. Each leaves out a corpus the reader
// may not read, as if it were not there.
type ProjectCorpora struct {
	Version int                  `json:"version"`
	Corpora []ProjectCorpus      `json:"corpora"`
	Given   []projects.CorpusRef `json:"given"`
}

// CorporaInput is what a project's owner sets as its corpora, as a whole.
// Version is the project's, as they read it.
type CorporaInput struct {
	Version int      `json:"version"`
	Corpora []string `json:"corpora"`
}

// ProjectCorporaAs reads a project's corpora as who may read them. "~" is
// the workspace.
func (p *Platform) ProjectCorporaAs(who auth.Principal, project string) (ProjectCorpora, error) {
	name := projects.NameOf(project)
	tree := p.Projects.Tree(who.Tenant)
	pr, ok := tree.Get(name)
	if !ok {
		return ProjectCorpora{}, fmt.Errorf("%w: %q", projects.ErrNotFound, project)
	}
	return p.corporaOf(who, pr, tree), nil
}

// SetProjectCorpora replaces the corpora a project attaches, "~" for the
// workspace. Each corpus newly attached must be one the caller may read
// (400, in the words for one that does not exist): attaching a corpus makes
// no one its reader, and a session searches it only as each of its readers
// may. One attached already may stay, even if the caller may not read it or
// it was deleted, so saving what they were shown never drops what they were
// not shown. The rest is the store's: the version read, and projects:govern
// for the workspace and company projects. The caller holds ownership of the
// project.
func (p *Platform) SetProjectCorpora(who auth.Principal, project string, w projects.Writer, in CorporaInput) (ProjectCorpora, error) {
	name := projects.NameOf(project)
	var attached []string
	if pr, err := p.Projects.Get(who.Tenant, name); err == nil {
		attached = pr.Context.Corpora
	}
	for _, n := range in.Corpora {
		if slices.Contains(attached, n) {
			continue
		}
		if !p.CorpusReadable(who, n) {
			return ProjectCorpora{}, fmt.Errorf("%w: there is no corpus called %q", projects.ErrInvalid, n)
		}
	}
	names := slices.Clone(in.Corpora)
	for _, n := range attached {
		if !p.CorpusReadable(who, n) && !slices.Contains(names, n) {
			names = append(names, n)
		}
	}
	pr, err := p.Projects.SetCorpora(who.Tenant, name, in.Version, names, w, p.now())
	if err != nil {
		return ProjectCorpora{}, err
	}
	return p.corporaOf(who, pr, p.Projects.Tree(who.Tenant)), nil
}

// corporaOf is a project's corpora as who may read them, from the version
// given, so a write's answer is the version it wrote.
func (p *Platform) corporaOf(who auth.Principal, pr projects.Project, tree *projects.Tree) ProjectCorpora {
	out := ProjectCorpora{Version: pr.Version, Corpora: []ProjectCorpus{}, Given: []projects.CorpusRef{}}
	for _, n := range pr.Context.Corpora {
		if view, err := p.CorpusAs(who, n); err == nil {
			out.Corpora = append(out.Corpora, ProjectCorpus{Name: n, Title: view.Title, Sources: len(view.Members)})
		}
	}
	for _, c := range tree.Corpora(pr.Name) {
		if p.CorpusReadable(who, c.Name) {
			out.Given = append(out.Given, c)
		}
	}
	return out
}

// ShownCorpora is the corpora of a project record as who may read them, for
// views that carry the record.
func (p *Platform) ShownCorpora(who auth.Principal, names []string) []string {
	var out []string
	for _, n := range names {
		if p.CorpusReadable(who, n) {
			out = append(out, n)
		}
	}
	return out
}
