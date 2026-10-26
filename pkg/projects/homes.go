package projects

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// keyHome keeps where an agent lives: a:<agent>.
const keyHome = "a:"

// What a home change records, besides the agent.
const (
	// HomeChanged is an agent coming to live in a project, or leaving it.
	HomeChanged = "home"
	// AgentsChanged is a project's Agents changing.
	AgentsChanged = "agents"
)

// Notes on a home change, on the project it names.
const (
	LivesHereNote = "lives here"
	LeftNote      = "no longer lives here"
)

// home is where one agent lives, as it is written.
type home struct {
	Agent   string    `json:"agent"`
	Project string    `json:"project"`
	By      string    `json:"by,omitempty"`
	At      time.Time `json:"at"`
}

// homeNames is the agent-to-project index a Tree is built with.
func homeNames(homes map[string]home) map[string]string {
	out := make(map[string]string, len(homes))
	for a, h := range homes {
		out[a] = h.Project
	}
	return out
}

// SetHome makes an agent live in a project: available there and in every
// project below it, and nowhere else, whatever "*" or an enable list says.
// project Root makes it a platform agent again, available wherever it is
// enabled. The agent's existence is the registry's to check, before calling.
//
// It is refused (ErrInUse, naming them) while anything would lose the agent:
// a default, an enable list or an allow list in the tree that names it, a
// channel's default (an archived channel's aside), and whatever inUse names. inUse is given the tree as it is and as it would be,
// runs under the store's lock, and must not call the store. Homing in or out
// of a company project needs projects:govern. The record and its history are
// one write: a change on the new home, and one on the old.
func (s *Store) SetHome(tenantName, agent, project string, w Writer, at time.Time, inUse func(before, after *Tree) []string) error {
	if !ValidAgent(agent) {
		return fmt.Errorf("%w: %q is not an agent that can live in a project", ErrInvalid, agent)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx := s.begin(tenantName)
	old, had := tx.t.homes[agent]
	if had {
		if owner := tx.t.projects[old.Project].PersonalOwner; owner != "" && owner != w.By {
			return fmt.Errorf("%w: only the personal owner changes this agent home", ErrForbidden)
		}
	}
	if had && old.Project == project {
		return nil
	}
	if project != Root {
		p, ok := tx.t.projects[project]
		if !ok {
			return fmt.Errorf("%w: %q", ErrNotFound, project)
		}
		if p.PersonalOwner != "" && p.PersonalOwner != w.By {
			return fmt.Errorf("%w: only the personal owner places an agent here", ErrForbidden)
		}
		if p.Kind == Company && !w.Govern {
			return fmt.Errorf("%w: an agent lives in a company project only with projects:govern", ErrForbidden)
		}
	}
	if had && old.Project != Root && tx.t.projects[old.Project].Kind == Company && !w.Govern {
		return fmt.Errorf("%w: an agent leaves a company project only with projects:govern", ErrForbidden)
	}
	before := build(tx.t.projects, homeNames(tx.t.homes))
	{
		h := home{Agent: agent, Project: project, By: w.By, At: at}
		tx.t.homes[agent] = h
		tx.put(keyHome+agent, h)
	}
	after := build(tx.t.projects, homeNames(tx.t.homes))
	users := append(losing(before, after, agent), channelsLosing(tx.t, before, after, agent)...)
	if inUse != nil {
		users = append(users, inUse(before, after)...)
	}
	if len(users) > 0 {
		return fmt.Errorf("%w: %s would lose %s: %s", ErrInUse, plural(len(users), "place"), agent, strings.Join(users, ", "))
	}
	tx.record(Change{At: at, By: w.By, Project: project, What: HomeChanged, Agent: agent, Note: LivesHereNote})
	if had {
		tx.record(Change{At: at, By: w.By, Project: old.Project, What: HomeChanged, Agent: agent, Note: LeftNote})
	}
	return tx.commit()
}

// DropHome makes an agent a platform agent again: SetHome with no project.
func (s *Store) DropHome(tenantName, agent string, w Writer, at time.Time, inUse func(before, after *Tree) []string) error {
	return s.SetHome(tenantName, agent, Root, w, at, inUse)
}

// losing names what in the tree would lose an agent when it moves from
// before to after: a default or an enable entry where it stops being
// available, and an allow entry under which it stops being available
// anywhere. An allow entry above the agent's new home still decides there,
// so it is not counted.
func losing(before, after *Tree, agent string) []string {
	var out []string
	lost := func(name string) bool { return before.IsAvailable(name, agent) && !after.IsAvailable(name, agent) }
	anywhere := func(t *Tree, name string) bool {
		return slices.ContainsFunc(t.Subtree(name), func(n string) bool { return t.IsAvailable(n, agent) })
	}
	for _, name := range after.order {
		a := after.nodes[name].p.Agents
		if a.Default == agent && lost(name) {
			out = append(out, "the default of "+display(name))
		}
		if slices.Contains(a.Enable, agent) && lost(name) {
			out = append(out, "the enable list of "+display(name))
		}
		if slices.Contains(a.Allow, agent) && anywhere(before, name) && !anywhere(after, name) {
			out = append(out, "the allow list of "+display(name))
		}
	}
	return out
}

// channelsLosing names the channels whose default is an agent that would
// stop being available in the channel's project when it moves from before
// to after, as losing names a project's default. An archived channel takes
// no new threads and its default cannot change, so it holds no move back.
func channelsLosing(t *tenant, before, after *Tree, agent string) []string {
	var channels []Channel
	for _, c := range t.channels {
		if c.Default == agent && !c.Archived && before.IsAvailable(c.Project, agent) && !after.IsAvailable(c.Project, agent) {
			channels = append(channels, c)
		}
	}
	slices.SortFunc(channels, func(a, b Channel) int {
		return strings.Compare(channelKey(a.Project, a.Name), channelKey(b.Project, b.Name))
	})
	out := make([]string, 0, len(channels))
	for _, c := range channels {
		out = append(out, fmt.Sprintf("the default of #%s in %s", c.Name, display(c.Project)))
	}
	return out
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// Tenants lists the workspaces that have written anything, by name.
func (s *Store) Tenants() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, 0, len(s.tenants))
	for name, t := range s.tenants {
		if t.stored() {
			out = append(out, name)
		}
	}
	slices.Sort(out)
	return out
}

// EnsureRoot writes a tenant's root if it has not been written: the root as
// it reads until then, with its history entry. It changes nothing once the
// root is written.
func (s *Store) EnsureRoot(tenantName, by string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx := s.begin(tenantName)
	if _, ok := tx.t.projects[Root]; ok {
		return nil
	}
	tx.root(by, at)
	return tx.commit()
}

// EnsureCompanyHomes records missing legacy homes in one transaction. Existing
// choices win concurrent migrations, including a move made while a registry
// page was being read. It never changes an agent's availability.
func (s *Store) EnsureCompanyHomes(tenantName string, agents []string, at time.Time) error {
	if len(agents) > MaxPage {
		return fmt.Errorf("%w: too many homes in one migration page", ErrLimit)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx := s.begin(tenantName)
	changed := false
	for _, agent := range agents {
		if !ValidAgent(agent) {
			return fmt.Errorf("%w: invalid agent home", ErrInvalid)
		}
		if _, exists := tx.t.homes[agent]; exists {
			continue
		}
		h := home{Agent: agent, Project: Root, By: "platform", At: at}
		tx.t.homes[agent] = h
		tx.put(keyHome+agent, h)
		tx.record(Change{At: at, By: "platform", Project: Root, What: HomeChanged, Agent: agent, Note: LivesHereNote})
		changed = true
	}
	if !changed {
		return nil
	}
	return tx.commit()
}
