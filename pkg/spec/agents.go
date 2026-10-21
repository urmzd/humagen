package spec

import (
	"fmt"
	"slices"
	"strings"

	"github.com/urmzd/fiber/pkg/agentspec"
)

// Names an agent list holds besides agents.
const (
	// AutoAgent is the built-in router, @auto: only ever a default.
	AutoAgent = "auto"
	// EveryAgent, alone in an enable list, is every platform agent.
	EveryAgent = "*"
)

// maxAgentList bounds an enable and an allow list, as the project store
// does.
const maxAgentList = 64

// BuiltinAgents are the platform's own agents, which a file may name without
// declaring and which live in no project. A test holds them to the
// platform's.
var BuiltinAgents = []string{"warp-writer", "warp-analyst", "warp-builder"}

// level is one place an agent list is set: the workspace or a project.
type level struct {
	path   string
	name   string // "" is the workspace
	agents *Agents
}

// checkAgents holds the file's agent lists and homes to what the project
// store accepts, each mistake with its path (rule 6):
//
//   - enable, allow and default name agents in this file or the platform's
//     own; "*" only stands alone in enable, and auto is only a default;
//   - an allow list names at least one agent, and a list at most 64;
//   - an agent lives in at most one project, one in this file, and an agent
//     that lives in a project is not enabled anywhere, which would add
//     nothing;
//   - a named default is available where it is set, as far as the file can
//     tell: a level the file leaves to the console may enable anything, and
//     the file's allow lists always apply.
//
// An agent read from a file is named only when the file is read at start,
// so a name the file cannot see is left to the start, which checks again.
func checkAgents(sp PlatformSpec) error {
	w := sp.Workspace
	if w.Baseline != nil {
		return fmt.Errorf("workspace.baseline: baseline is now workspace.agents.default")
	}
	known := map[string]bool{}
	for _, b := range BuiltinAgents {
		known[b] = true
	}
	blind := false
	homes := map[string]string{}
	inFile := map[string]bool{}
	for _, p := range w.Projects {
		inFile[p.Name] = true
	}
	for i, a := range sp.Agents {
		at := fmt.Sprintf("agents[%d]", i)
		name := ""
		if a.Spec != nil {
			name = agentspec.NormalizeName(a.Spec.Name)
			known[name] = true
		} else {
			blind = true
		}
		if a.Project == "" || a.Project == RootParent {
			continue
		}
		switch {
		case !inFile[a.Project]:
			return fmt.Errorf("%s.project: %q is not a project in this file; an agent lives in a project the file declares", at, a.Project)
		case name == "":
			return fmt.Errorf("%s.project: an agent read from a file lives in a project only when written inline, with spec", at)
		case slices.Contains(BuiltinAgents, name):
			return fmt.Errorf("%s.project: %s is one of the platform's own agents, which live nowhere", at, name)
		}
		if other, twice := homes[name]; twice && other != a.Project {
			return fmt.Errorf("%s.project: %s already lives in %q; an agent lives in one project", at, name, other)
		}
		homes[name] = a.Project
	}

	levels := []level{{path: "workspace.agents", agents: w.Agents}}
	for i, p := range w.Projects {
		levels = append(levels, level{path: fmt.Sprintf("workspace.projects[%d].agents", i), name: p.Name, agents: p.Agents})
	}
	for _, l := range levels {
		if l.agents == nil {
			continue
		}
		if err := checkAgentLists(*l.agents, known, blind, homes); err != nil {
			return fmt.Errorf("%s.%w", l.path, err)
		}
	}

	parents := map[string]string{}
	byName := map[string]*Agents{"": w.Agents}
	for _, p := range w.Projects {
		parents[p.Name] = ParentOf(p)
		byName[p.Name] = p.Agents
	}
	for _, l := range levels {
		if l.agents == nil || l.agents.Default == "" || l.agents.Default == AutoAgent {
			continue
		}
		if why := unavailable(l.agents.Default, l.name, parents, byName, homes); why != "" {
			return fmt.Errorf("%s.default: %s is not available in %s: %s", l.path, l.agents.Default, display(l.name), why)
		}
	}
	return nil
}

func display(name string) string {
	if name == "" {
		return "the workspace"
	}
	return fmt.Sprintf("%q", name)
}

// checkAgentLists checks one level's lists on their own. Its error starts
// with the field's name.
func checkAgentLists(a Agents, known map[string]bool, blind bool, homes map[string]string) error {
	switch {
	case len(a.Enable) > maxAgentList:
		return fmt.Errorf("enable: at most %d agents", maxAgentList)
	case a.Allow != nil && len(a.Allow) == 0:
		return fmt.Errorf("allow: names at least one agent; leave it out to narrow nothing")
	case len(a.Allow) > maxAgentList:
		return fmt.Errorf("allow: at most %d agents", maxAgentList)
	case slices.Contains(a.Enable, EveryAgent) && len(a.Enable) != 1:
		return fmt.Errorf("enable: %q stands alone", EveryAgent)
	}
	for field, list := range map[string][]string{"enable": a.Enable, "allow": a.Allow} {
		for _, n := range list {
			switch {
			case n == EveryAgent && field == "enable":
				continue
			case n == EveryAgent:
				return fmt.Errorf("allow: %q is only for enable; leave allow out to narrow nothing", EveryAgent)
			case n == AutoAgent:
				return fmt.Errorf("%s: auto is the built-in router: it is only ever a default", field)
			}
			if err := checkAgentName(n, known, blind); err != nil {
				return fmt.Errorf("%s: %w", field, err)
			}
			if home, homed := homes[n]; homed && field == "enable" {
				return fmt.Errorf("enable: %s lives in %q, so it is available there and below, and enabling it adds nothing", n, home)
			}
		}
	}
	switch a.Default {
	case "", AutoAgent:
	case EveryAgent:
		return fmt.Errorf("default: one agent, or auto")
	default:
		if err := checkAgentName(a.Default, known, blind); err != nil {
			return fmt.Errorf("default: %w", err)
		}
	}
	return nil
}

func checkAgentName(n string, known map[string]bool, blind bool) error {
	if n == "" || n != agentspec.NormalizeName(n) || strings.ContainsAny(n, " /") {
		return fmt.Errorf("%q is not an agent's name", n)
	}
	if !known[n] && !blind {
		return fmt.Errorf("%q is not an agent in this file or one of the platform's own", n)
	}
	return nil
}

// unavailable says why an agent cannot run in a project, as far as the file
// can tell, or "" when it can or the file cannot tell.
func unavailable(agent, project string, parents map[string]string, lists map[string]*Agents, homes map[string]string) string {
	home, homed := homes[agent]
	offered, unknown := false, false
	for at, steps := project, 0; steps <= len(parents)+1; steps++ {
		a := lists[at]
		switch {
		case a == nil:
			unknown = true
		case a.Allow != nil && !slices.Contains(a.Allow, agent):
			return fmt.Sprintf("the allow list of %s leaves it out", display(at))
		case !homed && (slices.Contains(a.Enable, agent) || slices.Contains(a.Enable, EveryAgent)):
			offered = true
		}
		if homed && at == home {
			offered = true
		}
		if at == "" {
			break
		}
		at = parents[at]
	}
	switch {
	case homed && !offered:
		return fmt.Sprintf("it lives in %q, and is available only there and below", home)
	case !offered && !unknown:
		return "nothing on its chain enables it"
	}
	return ""
}
