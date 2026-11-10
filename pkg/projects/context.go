package projects

import (
	"fmt"
	"slices"
	"time"

	"github.com/urmzd/humagen/pkg/corpora"
)

// Context bounds.
const (
	// MaxCorpora is how many corpora one project attaches.
	MaxCorpora = 20
	// MaxRunCorpora is how many corpora of its project's chain one session
	// searches: the nearest first.
	MaxRunCorpora = 40
)

// ContextChanged is what a change to a project's context records.
const ContextChanged = "context"

// Context is what a project gives every session filed in it and below,
// besides its instructions: the corpora they search. A corpus is the
// knowledge store's (pkg/corpora); a project names it and nothing more, so
// attaching one makes nobody its reader.
type Context struct {
	// Corpora are corpus names, each once, at most MaxCorpora.
	Corpora []string `json:"corpora,omitempty"`
}

func (c Context) clone() Context {
	c.Corpora = slices.Clone(c.Corpora)
	return c
}

// validContext checks a project's context: at most MaxCorpora corpora, each
// a corpus's name, each once. Whether the workspace has them is not asked
// here: one it does not have is searched by nobody.
func validContext(c Context) error {
	if len(c.Corpora) > MaxCorpora {
		return fmt.Errorf("%w: a project attaches at most %d corpora", ErrInvalid, MaxCorpora)
	}
	for i, n := range c.Corpora {
		if !corpora.ValidName(n) {
			return fmt.Errorf("%w: %q is not a corpus's name", ErrInvalid, n)
		}
		if slices.Contains(c.Corpora[:i], n) {
			return fmt.Errorf("%w: %q is attached twice", ErrInvalid, n)
		}
	}
	return nil
}

// CorpusRef is one corpus a session in a project searches, and the project
// on its chain that attaches it.
type CorpusRef struct {
	Name string `json:"name"`
	From string `json:"from"`
}

// Corpora returns the corpora a session filed in a project searches: the
// ones the project attaches, then each ancestor's, nearest first, the
// workspace's last. A corpus attached on the way more than once is given
// once, from the nearest project, and past MaxRunCorpora the farthest are
// left out. A sibling's or a child's are never given. It is nil for a name
// the tree does not hold.
func (t *Tree) Corpora(name string) []CorpusRef {
	var out []CorpusRef
	for _, p := range t.Chain(name) {
		for _, n := range p.Context.Corpora {
			if len(out) == MaxRunCorpora {
				return out
			}
			if !slices.ContainsFunc(out, func(c CorpusRef) bool { return c.Name == n }) {
				out = append(out, CorpusRef{Name: n, From: p.Name})
			}
		}
	}
	return out
}

// SetCorpora replaces the corpora a project attaches, in the order given.
// version must be the current one (ErrVersion); the root and company
// projects need projects:govern. A change that changes nothing writes
// nothing and keeps the version.
func (s *Store) SetCorpora(tenantName, name string, version int, names []string, w Writer, at time.Time) (Project, error) {
	names = slices.Clone(names)
	if len(names) == 0 {
		names = nil
	}
	if err := validContext(Context{Corpora: names}); err != nil {
		return Project{}, err
	}
	return s.update(tenantName, name, version, w, at, ContextChanged, func(p *Project) error {
		p.Context.Corpora = names
		return nil
	})
}
