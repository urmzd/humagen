package projects

import (
	"fmt"
	"slices"
	"time"

	"github.com/urmzd/warp/pkg/knowledge"
)

// Knowledge bounds.
const (
	// MaxKnowledge is how many knowledge sources one project names.
	MaxKnowledge = 8
	// MaxRunKnowledge is how many sources of its project's chain one run is
	// given: the nearest first.
	MaxRunKnowledge = 16
)

// FieldKnowledge is the knowledge sources a project names, when the
// platform file sets them.
const FieldKnowledge = "knowledge"

// KnowledgeChanged is what a change to a project's knowledge sources
// records.
const KnowledgeChanged = "knowledge"

// ValidSource reports whether a name can name a knowledge source, as the
// knowledge store takes it.
func ValidSource(name string) bool { return knowledge.ValidName(name) }

// validKnowledge checks the sources a project names: at most MaxKnowledge,
// each a source's name, each once. Whether the workspace has them is not
// asked here: one it does not have grounds nothing.
func validKnowledge(names []string) error {
	if len(names) > MaxKnowledge {
		return fmt.Errorf("%w: a project names at most %d knowledge sources", ErrInvalid, MaxKnowledge)
	}
	for i, n := range names {
		if !ValidSource(n) {
			return fmt.Errorf("%w: a knowledge source's name is lower case letters, digits and dashes, starting with a letter or digit, at most 63; not %q", ErrInvalid, n)
		}
		if slices.Contains(names[:i], n) {
			return fmt.Errorf("%w: %s is named twice", ErrInvalid, n)
		}
	}
	return nil
}

// KnowledgeSource is a knowledge source a run filed in a project is given,
// and the project on its chain that names it.
type KnowledgeSource struct {
	Name string `json:"name"`
	// From is the project that names it: the project itself, an ancestor,
	// or "" for the workspace.
	From string `json:"from"`
}

// Knowledge returns the knowledge sources a run filed in a project is given:
// the ones the project names, then each ancestor's, nearest first, the
// workspace's last. A source named on the way more than once is given once,
// from the nearest project that names it, and past MaxRunKnowledge the
// farthest are left out. A sibling's or a child's are never given. It is nil
// for a name the tree does not hold.
func (t *Tree) Knowledge(name string) []KnowledgeSource {
	var out []KnowledgeSource
	for _, p := range t.Chain(name) {
		for _, n := range p.Knowledge {
			if len(out) == MaxRunKnowledge {
				return out
			}
			if !slices.ContainsFunc(out, func(s KnowledgeSource) bool { return s.Name == n }) {
				out = append(out, KnowledgeSource{Name: n, From: p.Name})
			}
		}
	}
	return out
}

// SetKnowledge replaces the knowledge sources a project names, in the order
// given. version must be the current one (ErrVersion); sources the file sets
// are changed only by the file (ErrManaged); the root and company projects
// need projects:govern. A change that changes nothing writes nothing and
// keeps the version.
func (s *Store) SetKnowledge(tenantName, name string, version int, names []string, w Writer, at time.Time) (Project, error) {
	names = slices.Clone(names)
	if len(names) == 0 {
		names = nil
	}
	if err := validKnowledge(names); err != nil {
		return Project{}, err
	}
	return s.update(tenantName, name, version, w, at, KnowledgeChanged, func(p *Project) error {
		p.Knowledge = names
		return nil
	})
}
