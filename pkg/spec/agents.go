package spec

import (
	"fmt"
	"slices"
	"strings"

	"github.com/urmzd/humagen/internal/legacy"
	"github.com/urmzd/humagen/internal/wire"
)

// Names an agent list holds besides agents.
const (
	// OpenDefault, as a default, lets every available agent decide for
	// itself whether to take an untagged message. "auto" reads as it.
	OpenDefault = EveryAgent
	// EveryAgent, alone in an enable list, is every platform agent.
	EveryAgent = "*"
)

// ReservedNames are the tags no agent can be named: the built-in router and
// the tag for a project's experts. A test holds them to the directory's.
var ReservedNames = []string{legacy.AutoDefault, "sme"}

// maxAgentList bounds an enable and an allow list, as the project store
// does.
const maxAgentList = 64

// BuiltinAgents are the platform's own agents, which a file may name without
// declaring and which live in no project. A test holds them to the
// platform's.
var BuiltinAgents = []string{"humagen-writer", "humagen-analyst", "humagen-builder", "code-reviewer", "security-reviewer", "architect-reviewer", "qa-reviewer"}

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
//   - an agent has at most one home, one in this file; a home says only who
//     sees the agent;
//   - the workspace default is allowed at the root. A project's default and
//     every enable list are not read (unreadAgentFields notes them), since
//     any agent answers in any project its allow lists permit.
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
	for old := range legacy.BuiltinAgents() {
		known[old] = true
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
			name = wire.NormalizeName(a.Spec.Name)
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
		case slices.Contains(BuiltinAgents, name) || legacy.IsBuiltinAgent(name):
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
		if err := checkAgentLists(*l.agents, known, blind); err != nil {
			return fmt.Errorf("%s.%w", l.path, err)
		}
	}

	parents := map[string]string{}
	byName := map[string]*Agents{"": w.Agents}
	for _, p := range w.Projects {
		parents[p.Name] = ParentOf(p)
		byName[p.Name] = p.Agents
	}
	if d := w.Agents; d != nil && d.Default != "" && d.Default != OpenDefault {
		if why := unavailable(d.Default, "", parents, byName); why != "" {
			return fmt.Errorf("workspace.agents.default: %s is not available in %s: %s", d.Default, display(""), why)
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
func checkAgentLists(a Agents, known map[string]bool, blind bool) error {
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
			case n == legacy.AutoDefault:
				return fmt.Errorf("%s: auto is no agent; \"*\" as a default lets every agent decide", field)
			}
			if err := checkAgentName(n, known, blind); err != nil {
				return fmt.Errorf("%s: %w", field, err)
			}
		}
	}
	switch a.Default {
	case "", OpenDefault:
	default:
		if err := checkAgentName(a.Default, known, blind); err != nil {
			return fmt.Errorf("default: %w", err)
		}
	}
	return nil
}

func checkAgentName(n string, known map[string]bool, blind bool) error {
	if n == "" || n != wire.NormalizeName(n) || strings.ContainsAny(n, " /") {
		return fmt.Errorf("%q is not an agent's name", n)
	}
	if !known[n] && !blind {
		return fmt.Errorf("%q is not an agent in this file or one of the platform's own", n)
	}
	return nil
}

// unavailable says why an agent cannot run in a project, as far as the file
// can tell, or "" when it can or the file cannot tell: an allow list on the
// project's chain leaves it out. Any agent answers in any project the allow
// lists permit.
func unavailable(agent, project string, parents map[string]string, lists map[string]*Agents) string {
	for at, steps := project, 0; steps <= len(parents)+1; steps++ {
		if a := lists[at]; a != nil && a.Allow != nil && !slices.Contains(a.Allow, agent) {
			return fmt.Sprintf("the allow list of %s leaves it out", display(at))
		}
		if at == "" {
			break
		}
		at = parents[at]
	}
	return ""
}

// unreadAgentFields returns a note for each field the file sets that is no
// longer read: an enable list, a project's default and a channel's default.
// Agents are independent of projects, so none of them decides who answers.
// They are still accepted, so a file that sets them keeps starting.
func (sp *PlatformSpec) unreadAgentFields() []string {
	var notes []string
	const why = "is no longer read: any agent answers in any project its allow lists permit"
	w := sp.Workspace
	if w.Agents != nil && w.Agents.Enable != nil {
		notes = append(notes, "workspace.agents.enable "+why)
	}
	for i, c := range w.Channels {
		if c.Default != "" {
			notes = append(notes, fmt.Sprintf("workspace.channels[%d].default %s", i, why))
		}
	}
	for i, p := range w.Projects {
		if p.Agents != nil && p.Agents.Enable != nil {
			notes = append(notes, fmt.Sprintf("workspace.projects[%d].agents.enable %s", i, why))
		}
		if p.Agents != nil && p.Agents.Default != "" {
			notes = append(notes, fmt.Sprintf("workspace.projects[%d].agents.default %s", i, why))
		}
		for j, c := range p.Channels {
			if c.Default != "" {
				notes = append(notes, fmt.Sprintf("workspace.projects[%d].channels[%d].default %s", i, j, why))
			}
		}
	}
	return notes
}

// adoptAutoDefaults reads every default written as "auto", the router that
// picked one agent, as OpenDefault, and returns a note for each.
func (sp *PlatformSpec) adoptAutoDefaults() []string {
	var notes []string
	adopt := func(at string, d *string) {
		if *d == legacy.AutoDefault {
			*d = OpenDefault
			notes = append(notes, at+": auto still works; write \"*\"")
		}
	}
	w := &sp.Workspace
	if w.Agents != nil {
		adopt("workspace.agents.default", &w.Agents.Default)
	}
	for i := range w.Channels {
		adopt(fmt.Sprintf("workspace.channels[%d].default", i), &w.Channels[i].Default)
	}
	for i := range w.Projects {
		p := &w.Projects[i]
		if p.Agents != nil {
			adopt(fmt.Sprintf("workspace.projects[%d].agents.default", i), &p.Agents.Default)
		}
		for j := range p.Channels {
			adopt(fmt.Sprintf("workspace.projects[%d].channels[%d].default", i, j), &p.Channels[j].Default)
		}
	}
	return notes
}
