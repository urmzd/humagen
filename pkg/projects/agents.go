package projects

import (
	"fmt"
	"slices"

	"github.com/urmzd/humagen/internal/legacy"
	"github.com/urmzd/humagen/internal/wire"
)

// OpenDefault is the default that names no agent: every agent available
// where an untagged message is posted decides for itself whether to take it
// (pkg/join). It is no agent: it is not in the registry and is never
// homed or allowed. A default written as "auto", the router that
// picked one agent, reads as it (legacy.Default).
const OpenDefault = Everyone

// Everyone, alone in an enable list, was every agent. It is also the member
// entry for everyone in the workspace.
const Everyone = "*"

// MaxAgentList bounds an enable list and an allow list, as
// clients.Client.Agents is bounded.
const MaxAgentList = 64

// FieldAgents is the platform file owning a project's Agents.
const FieldAgents = "agents"

// Agents is a project's agent policy. Agents are independent of projects:
// any agent answers in any project, unless an allow list on its chain leaves
// it out:
//
//	available(P) = ∩ over Q in Chain(P) with Q.Allow set, of Q.Allow
//
// The zero value narrows nothing. Enable, and Default below the root, were
// how a project chose its agents; they are kept so the records already
// stored read back, and are not read.
type Agents struct {
	// Enable is not read. It is never nil, except on the root as it was
	// written before projects had agents.
	Enable []string `json:"enable"`
	// Allow, when set, is the only agents that may run here and below. Every
	// allow list on the chain applies, so a child can only narrow what its
	// parent allows. Nil narrows nothing; a set list names at least one agent.
	// A set list is written even when empty, as it reads to someone not
	// shown the agents it names.
	Allow []string `json:"allow,omitzero"`
	// Default, on the root, is the workspace default: the agent that answers
	// a message that tags none, or OpenDefault. Below the root it is not
	// read.
	Default string `json:"default,omitempty"`
}

// ValidAgent reports whether a name can stand for an agent in a project's
// lists, as a default or as an agent with a home: a fiber agent name in its
// canonical form. OpenDefault is no agent, and neither is the runtime's
// reserved name (wire.Reserved).
func ValidAgent(name string) bool {
	if name == "" || name != wire.NormalizeName(name) || len(name) > wire.MaxNameLen ||
		name == OpenDefault || name == legacy.AutoDefault || name == wire.Reserved {
		return false
	}
	return wire.Addressable(name) == nil
}

func (a Agents) clone() Agents {
	a.Enable = slices.Clone(a.Enable)
	a.Allow = slices.Clone(a.Allow)
	return a
}

// listed is a with an enable list that is never nil, as every record but
// the upgrade's root is served.
func (a Agents) listed() Agents {
	if a.Enable == nil {
		a.Enable = []string{}
	}
	return a
}

// canonical is the lists as they are kept: names in fiber's canonical form,
// each list sorted, and enable never nil. A set allow list stays set, even
// empty, so validate can refuse it rather than widen it to nil.
func (a Agents) canonical() Agents {
	out := Agents{Enable: canonicalNames(a.Enable), Default: legacy.Default(wire.NormalizeName(a.Default))}
	if out.Enable == nil {
		out.Enable = []string{}
	}
	if a.Allow != nil {
		out.Allow = canonicalNames(a.Allow)
		if out.Allow == nil {
			out.Allow = []string{}
		}
	}
	return out
}

func canonicalNames(names []string) []string {
	if len(names) == 0 {
		return nil
	}
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = wire.NormalizeName(n)
	}
	slices.Sort(out)
	return out
}

// validate checks the lists on their own, not against the tree.
func (a Agents) validate() error {
	bad := func(format string, args ...any) error {
		return fmt.Errorf("%w: "+format, append([]any{ErrInvalid}, args...)...)
	}
	switch {
	case len(a.Enable) > MaxAgentList:
		return bad("a project enables at most %d agents", MaxAgentList)
	case a.Allow != nil && len(a.Allow) == 0:
		return bad("an allow list names at least one agent; leave it out to narrow nothing")
	case len(a.Allow) > MaxAgentList:
		return bad("a project allows at most %d agents", MaxAgentList)
	case slices.Contains(a.Enable, Everyone) && len(a.Enable) != 1:
		return bad("%q stands alone in an enable list", Everyone)
	}
	if err := names("enable", a.Enable, true); err != nil {
		return err
	}
	if err := names("allow", a.Allow, false); err != nil {
		return err
	}
	switch d := a.Default; {
	case d == "" || d == OpenDefault:
	case !ValidAgent(d):
		return bad("%q is not an agent name", d)
	}
	return nil
}

func names(list string, ns []string, everyone bool) error {
	for i, n := range ns {
		switch {
		case n == Everyone && everyone:
		case n == Everyone:
			return fmt.Errorf("%w: %q is only for enable; leave allow out to narrow nothing", ErrInvalid, Everyone)
		case n == legacy.AutoDefault:
			return fmt.Errorf("%w: auto is no agent; %q as a default lets every agent decide", ErrInvalid, OpenDefault)
		case !ValidAgent(n):
			return fmt.Errorf("%w: %q in %s is not an agent name", ErrInvalid, n, list)
		}
		if slices.Contains(ns[:i], n) {
			return fmt.Errorf("%w: %q is in %s twice", ErrInvalid, n, list)
		}
	}
	return nil
}

func sameAgents(a, b Agents) bool {
	return slices.Equal(a.Enable, b.Enable) && slices.Equal(a.Allow, b.Allow) &&
		(a.Allow == nil) == (b.Allow == nil) && a.Default == b.Default
}

// Availability is which agents may run in a project.
type Availability struct {
	// All is every agent: no allow list on the chain narrows it.
	All bool `json:"all"`
	// Names is, without All, every agent the allow lists permit, sorted.
	// With All it is empty.
	Names []string `json:"names"`
}

// Home returns an agent's home, which says who sees it, by any spelling of
// its name, as the registry finds an agent by any. Root is an explicit home
// for the whole workspace.
func (t *Tree) Home(agent string) (string, bool) {
	p, ok := t.homes[wire.NormalizeName(agent)]
	return p, ok
}

// Homes returns the agents homed in a project, by name: those its readers
// see.
func (t *Tree) Homes(project string) []string {
	return slices.Clone(t.lives[project])
}

// chain walks a project, then its ancestors nearest first, then the root. It
// reports false for a name the tree does not hold.
func (t *Tree) chain(name string, fn func(p *Project) bool) bool {
	n, ok := t.nodes[name]
	if !ok {
		return false
	}
	for {
		if !fn(&n.p) || n.p.IsRoot() {
			return true
		}
		n = t.nodes[n.p.Parent]
	}
}

// Available returns which agents may run in a project: every agent, unless
// an allow list on its chain narrows them. A name the tree does not hold has
// none.
func (t *Tree) Available(project string) Availability {
	if !t.Has(project) {
		return Availability{Names: []string{}}
	}
	allowed := t.Allowed(project)
	if allowed == nil {
		return Availability{All: true, Names: []string{}}
	}
	return Availability{Names: allowed}
}

// Allowed returns the agents every allow list on a project's chain permits,
// the project's own included and sorted, or nil when none of them narrows,
// so every agent. A name the tree does not hold permits none.
func (t *Tree) Allowed(project string) []string {
	var allowed map[string]bool
	held := t.chain(project, func(q *Project) bool {
		if q.Agents.Allow != nil {
			next := map[string]bool{}
			for _, a := range q.Agents.Allow {
				if allowed == nil || allowed[a] {
					next[a] = true
				}
			}
			allowed = next
		}
		return true
	})
	if !held {
		return []string{}
	}
	if allowed == nil {
		return nil
	}
	out := make([]string, 0, len(allowed))
	for a := range allowed {
		out = append(out, a)
	}
	slices.Sort(out)
	return out
}

// Widens reports whether permitting next instead of was lets an agent run
// that was did not: nil permits every agent.
func Widens(was, next []string) bool {
	if was == nil {
		return false
	}
	if next == nil {
		return true
	}
	for _, a := range next {
		if !slices.Contains(was, a) {
			return true
		}
	}
	return false
}

// IsAvailable reports whether an agent may run in a project: any agent may,
// unless an allow list on the chain leaves it out. Agents are independent of
// projects: where an agent's home is, and what a project enables, decide
// nothing. It does not know whether the agent exists; the registry does.
// OpenDefault is no agent, so it is never available itself: a caller
// resolves it to one first.
func (t *Tree) IsAvailable(project, agent string) bool {
	if !ValidAgent(agent) {
		return false
	}
	allowed := true
	held := t.chain(project, func(q *Project) bool {
		if q.Agents.Allow != nil && !slices.Contains(q.Agents.Allow, agent) {
			allowed = false
			return false
		}
		return true
	})
	return held && allowed
}

// DefaultOf returns the default a message in a project inherits: the
// workspace's, set on the root, and the project that set it (always the
// root). A project's own default is no longer read: any agent answers in any
// project, so none is a project's. It returns "" for none, and for a name the
// tree does not hold.
func (t *Tree) DefaultOf(project string) (agent, from string) {
	if !t.Has(project) {
		return "", ""
	}
	if d := t.Root().Agents.Default; d != "" {
		return d, Root
	}
	return "", ""
}

// NamedDefault returns the workspace default when it names an agent rather
// than OpenDefault, and where it is set (the root). It is where OpenDefault
// hands a message that nothing matched.
func (t *Tree) NamedDefault(project string) (agent, from string) {
	if d, from := t.DefaultOf(project); d != OpenDefault {
		return d, from
	}
	return "", ""
}

// checkAgents holds a project's lists to the tree they are written into: the
// workspace default must be available at the root. A project below the root
// chooses no agents, so its enable list and default are not checked: they
// are not read. The platform file is checked as a whole instead
// (Platform.check), so Upsert does not call it.
func checkAgents(t *Tree, p Project) error {
	if !p.IsRoot() {
		return nil
	}
	if d := p.Agents.Default; d != "" && d != OpenDefault && !t.IsAvailable(p.Name, d) {
		return fmt.Errorf("%w: %s is not available in %s, so it cannot answer there", ErrInvalid, d, display(p.Name))
	}
	return nil
}
