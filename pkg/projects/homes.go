package projects

import (
	"fmt"
	"strings"
	"time"

	"github.com/urmzd/humagen/pkg/state"
)

// keyHome keeps an agent's home, which says who sees it: a:<agent>. The key
// is kept for the records already stored.
const keyHome = "a:"

// What a home change records, besides the agent.
const (
	// HomeChanged is an agent shared with a project's readers, or no longer.
	HomeChanged = "home"
	// AgentsChanged is a project's Agents changing.
	AgentsChanged = "agents"
)

// Notes on a home change, on the project it names. Older entries read "lives
// here" and "no longer lives here".
const (
	LivesHereNote = "its readers see this agent"
	LeftNote      = "its readers no longer see this agent"
)

// Unreadable is the home of an agent whose stored home could not be read: a
// name no project can have, so nobody but projects:govern sees the agent.
const Unreadable = "\x00unreadable"

// home is who sees one agent, as it is written.
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

// SetHome sets who sees an agent, as its home: a Personal project keeps it
// to that person, another project to that project's readers, and Root to the
// whole workspace. A home decides nothing about where the agent answers: any
// agent answers in any project its allow lists permit (Tree.IsAvailable).
// The agent's existence is the registry's to check, before calling.
//
// It is refused (ErrInUse, naming them) while whatever inUse names would
// lose the agent. inUse is given the tree as it is and as it would be, runs
// under the store's lock, and must not call the store. Homing in or out of a
// company project needs projects:govern. The record and its history are one
// write: a change on the new home, and one on the old.
func (s *Store) SetHome(tenantName, agent, project string, w Writer, at time.Time, inUse func(before, after *Tree) []string) error {
	if !ValidAgent(agent) {
		return fmt.Errorf("%w: %q is not an agent that can live in a project", ErrInvalid, agent)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return state.Retry(func() error { return s.setHome(tenantName, agent, project, w, at, inUse) })
}

// setHome is one attempt at SetHome.
func (s *Store) setHome(tenantName, agent, project string, w Writer, at time.Time, inUse func(before, after *Tree) []string) error {
	tx, err := s.begin(tenantName)
	if err != nil {
		return err
	}
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
	if !had && len(tx.t.homes) >= MaxHomes {
		return fmt.Errorf("%w: at most %d agents live in projects in one workspace", ErrLimit, MaxHomes)
	}
	before := build(tx.t.projects, homeNames(tx.t.homes))
	{
		h := home{Agent: agent, Project: project, By: w.By, At: at}
		tx.t.homes[agent] = h
		tx.put(keyHome+agent, h)
	}
	after := build(tx.t.projects, homeNames(tx.t.homes))
	var users []string
	if inUse != nil {
		users = inUse(before, after)
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

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// EachTenant calls fn for every workspace that has written anything, by
// name in order, a page of the directory at a time. A workspace whose first
// write failed may be among them, and reads as one that wrote nothing.
func (s *Store) EachTenant(fn func(tenant string) error) error {
	return s.dir.Each(fn)
}

// EnsureRoot writes a tenant's root if it has not been written: the root as
// it reads until then, with its history entry. It changes nothing once the
// root is written.
func (s *Store) EnsureRoot(tenantName, by string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return state.Retry(func() error {
		tx, err := s.begin(tenantName)
		if err != nil {
			return err
		}
		if _, ok := tx.t.projects[Root]; ok {
			return nil
		}
		tx.root(by, at)
		return tx.commit()
	})
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
	return state.Retry(func() error { return s.ensureCompanyHomes(tenantName, agents, at) })
}

// ensureCompanyHomes is one attempt at EnsureCompanyHomes.
func (s *Store) ensureCompanyHomes(tenantName string, agents []string, at time.Time) error {
	tx, err := s.begin(tenantName)
	if err != nil {
		return err
	}
	changed := false
	for _, agent := range agents {
		if !ValidAgent(agent) {
			return fmt.Errorf("%w: invalid agent home", ErrInvalid)
		}
		if _, exists := tx.t.homes[agent]; exists {
			continue
		}
		if len(tx.t.homes) >= MaxHomes {
			return fmt.Errorf("%w: at most %d agents live in projects in one workspace", ErrLimit, MaxHomes)
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
