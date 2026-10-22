package projects

import (
	"fmt"
	"slices"

	"github.com/urmzd/fiber/pkg/agentspec"
	"github.com/urmzd/fiber/pkg/topic"
)

// Auto is the built-in router's reserved name. It is no agent: it is not in
// the registry and is never homed, enabled or allowed. As a default it means
// "pick, with pkg/pick, from the agents available here".
const Auto = "auto"

// Everyone, alone in an enable list, is every platform agent: every agent
// with no home. It never reaches an agent that lives in a project.
const Everyone = "*"

// MaxAgentList bounds an enable list and an allow list, as
// clients.Client.Agents is bounded.
const MaxAgentList = 64

// FieldAgents is the platform file owning a project's Agents.
const FieldAgents = "agents"

// Agents is what a project turns on. It adds up down the tree: an agent is
// available in a project when the project or an ancestor offers it, and
// every allow list on the way permits it:
//
//	offered(P)   = ∪ over Q in Chain(P) of (Q.Enable ∪ Homes(Q))
//	allowed(P)   = ∩ over Q in Chain(P) with Q.Allow set, of Q.Allow
//	available(P) = offered(P) ∩ allowed(P)
//
// The zero value adds nothing and narrows nothing.
type Agents struct {
	// Enable is the platform agents available here and below. "*" alone is
	// every platform agent. An entry naming an agent that lives in a project
	// adds nothing: a project agent is available only in its home and below.
	// It is never nil, except on the root as it was written before projects
	// had agents, which the upgrade rewrites at start: any other record from
	// then is read with an empty list, which adds nothing either.
	Enable []string `json:"enable"`
	// Allow, when set, is the only agents that may run here and below. Every
	// allow list on the chain applies, so a child can only narrow what its
	// parent allows. Nil narrows nothing; a set list names at least one agent.
	Allow []string `json:"allow,omitempty"`
	// Default answers a message that tags no agent, here and below: an agent,
	// or Auto. "" inherits the nearest ancestor's.
	Default string `json:"default,omitempty"`
}

// ValidAgent reports whether a name can stand for an agent in a project's
// lists, as a default or as an agent with a home: a fiber agent name in its
// canonical form. Auto is no agent, and neither is fiber's control segment.
func ValidAgent(name string) bool {
	if name == "" || name != agentspec.NormalizeName(name) || len(name) > agentspec.MaxNameLen ||
		name == Auto || name == topic.ControlName {
		return false
	}
	_, err := topic.Agent(name)
	return err == nil
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
	out := Agents{Enable: canonicalNames(a.Enable), Default: agentspec.NormalizeName(a.Default)}
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
		out[i] = agentspec.NormalizeName(n)
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
	case d == "" || d == Auto:
	case d == Everyone:
		return bad("a default is one agent or %s", Auto)
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
		case n == Auto:
			return fmt.Errorf("%w: %s is the built-in router: it is never enabled or allowed, only made a default", ErrInvalid, Auto)
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
	// All is every platform agent: "*" is enabled on the chain and no allow
	// list narrows it.
	All bool `json:"all"`
	// Names is the agents available by name, sorted. With All, it is the
	// project agents that live on the chain, which "*" never reaches; without,
	// it is every agent available.
	Names []string `json:"names"`
}

// Home returns the project an agent lives in. It reports false for a
// platform agent.
func (t *Tree) Home(agent string) (string, bool) {
	p, ok := t.homes[agent]
	return p, ok
}

// Homes returns the agents that live in a project, by name.
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

// Available returns which agents may run in a project. A name the tree does
// not hold has none.
func (t *Tree) Available(project string) Availability {
	star := false
	offered := map[string]bool{}
	var allowed map[string]bool
	held := t.chain(project, func(q *Project) bool {
		for _, n := range q.Agents.Enable {
			if n == Everyone {
				star = true
			} else if _, homed := t.homes[n]; !homed {
				offered[n] = true
			}
		}
		for _, a := range t.lives[q.Name] {
			offered[a] = true
		}
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
	out := Availability{Names: []string{}}
	if !held {
		return out
	}
	if allowed == nil {
		out.All = star
		for a := range offered {
			if _, homed := t.homes[a]; homed || !star {
				out.Names = append(out.Names, a)
			}
		}
	} else {
		for a := range allowed {
			_, homed := t.homes[a]
			if offered[a] || (star && !homed) {
				out.Names = append(out.Names, a)
			}
		}
	}
	slices.Sort(out.Names)
	return out
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

// IsAvailable reports whether an agent may run in a project: offered there,
// and permitted by every allow list on the chain. It does not know whether
// the agent exists; the registry does. Auto is no agent, so it is never
// available itself: a caller resolves it to one first.
func (t *Tree) IsAvailable(project, agent string) bool {
	if !ValidAgent(agent) {
		return false
	}
	home, homed := t.homes[agent]
	offered, allowed := false, true
	t.chain(project, func(q *Project) bool {
		switch {
		case homed:
			offered = offered || q.Name == home
		case slices.Contains(q.Agents.Enable, Everyone), slices.Contains(q.Agents.Enable, agent):
			offered = true
		}
		if q.Agents.Allow != nil && !slices.Contains(q.Agents.Allow, agent) {
			allowed = false
			return false
		}
		return true
	})
	return offered && allowed
}

// DefaultOf returns the default a message in a project inherits: the first
// default set on its chain, and the project that set it ("" is the root). It
// returns "" for none, and for a name the tree does not hold.
func (t *Tree) DefaultOf(project string) (agent, from string) {
	t.chain(project, func(q *Project) bool {
		if q.Agents.Default != "" {
			agent, from = q.Agents.Default, q.Name
			return false
		}
		return true
	})
	return agent, from
}

// NamedDefault returns the nearest default on a project's chain that names an
// agent rather than Auto, and where it is set. It is where Auto hands a
// message that nothing matched. It is not skipped when it has stopped being
// available: admission refuses it, and says why.
func (t *Tree) NamedDefault(project string) (agent, from string) {
	t.chain(project, func(q *Project) bool {
		if d := q.Agents.Default; d != "" && d != Auto {
			agent, from = d, q.Name
			return false
		}
		return true
	})
	return agent, from
}

// checkAgents holds a project's lists to the tree they are written into: a
// named default must be available where it is set, and an agent that lives in
// a project is not enabled, since that would add nothing. The platform file
// is checked as a whole instead (Platform.check), so Upsert does not call it.
func checkAgents(t *Tree, p Project) error {
	for _, n := range p.Agents.Enable {
		if home, homed := t.homes[n]; homed {
			return fmt.Errorf("%w: %s lives in %s, so it is available there and below, and enabling it adds nothing", ErrInvalid, n, home)
		}
	}
	if d := p.Agents.Default; d != "" && d != Auto && !t.IsAvailable(p.Name, d) {
		return fmt.Errorf("%w: %s is not available in %s, so it cannot answer there", ErrInvalid, d, display(p.Name))
	}
	return nil
}
