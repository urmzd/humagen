package spec

import (
	"strings"
	"testing"

	"github.com/urmzd/warp/pkg/projects"
)

// platformDoc is a Platform document with a workspace and agents, each
// indented under its key.
func platformDoc(workspace, agents string) string {
	doc := "api_version: warp/v1\nkind: Platform\nmetadata: {name: default}\nspec:\n  workspace:\n" + workspace
	if agents != "" {
		doc += "  agents:\n" + agents
	}
	return doc
}

// platformAgentsYAML are two platform agents, and fileAgentsYAML those and
// one that lives in Globex.
const (
	platformAgentsYAML = `    - spec: {name: lead, provider: p, model: m, system_prompt: Lead.}
    - spec: {name: helper, provider: p, model: m, system_prompt: Help.}
`
	fileAgentsYAML = platformAgentsYAML + `    - project: Globex
      spec: {name: globex-helper, provider: p, model: m, system_prompt: Globex.}
`
)

// Rule 6: each of the agent lists' checks refuses the file, naming the
// document and the path of the list that breaks it.
func TestEachAgentCheckNamesItsPath(t *testing.T) {
	field := `      - {name: Field}
      - {name: Globex, parent: Field, kind: account}
`
	for name, tc := range map[string]struct{ workspace, agents, want string }{
		"the baseline": {"    baseline: lead\n", "", `workspace.baseline: baseline is now workspace.agents.default`},
		"an agent nobody declares": {
			"    agents: {enable: [ghost]}\n", platformAgentsYAML, `workspace.agents.enable: "ghost" is not an agent in this file or one of the platform's own`,
		},
		"star with others":       {"    agents: {enable: [\"*\", lead]}\n", platformAgentsYAML, `workspace.agents.enable: "*" stands alone`},
		"star in allow":          {"    agents: {allow: [\"*\"]}\n", platformAgentsYAML, `workspace.agents.allow: "*" is only for enable`},
		"auto enabled":           {"    agents: {enable: [auto]}\n", platformAgentsYAML, `workspace.agents.enable: auto is the built-in router`},
		"an empty allow list":    {"    agents: {allow: []}\n", platformAgentsYAML, `workspace.agents.allow: names at least one agent`},
		"a default of everyone":  {"    agents: {default: \"*\"}\n", platformAgentsYAML, `workspace.agents.default: one agent, or auto`},
		"a name not in its form": {"    agents: {enable: [Lead]}\n", platformAgentsYAML, `workspace.agents.enable: "Lead" is not an agent's name`},
		"a home not in the file": {
			"    projects:\n" + field, "    - project: Initech\n      spec: {name: x, provider: p, model: m, system_prompt: X.}\n",
			`agents[0].project: "Initech" is not a project in this file`,
		},
		"a home for the platform's own": {
			"    projects:\n" + field, "    - project: Field\n      spec: {name: warp-writer, provider: p, model: m, system_prompt: X.}\n",
			`agents[0].project: warp-writer is one of the platform's own agents`,
		},
		"enabling an agent that lives in a project": {
			"    agents: {enable: [globex-helper]}\n    projects:\n" + field, fileAgentsYAML,
			`workspace.agents.enable: globex-helper lives in "Globex", so it is available there and below, and enabling it adds nothing`,
		},
		"a default outside its home": {
			"    projects:\n      - {name: Field, agents: {default: globex-helper}}\n      - {name: Globex, parent: Field, kind: account}\n", fileAgentsYAML,
			`workspace.projects[0].agents.default: globex-helper is not available in "Field": it lives in "Globex"`,
		},
		"a default an allow list leaves out": {
			"    projects:\n      - {name: Field, agents: {allow: [lead]}}\n      - {name: Globex, parent: Field, kind: account, agents: {default: helper}}\n", fileAgentsYAML,
			`workspace.projects[1].agents.default: helper is not available in "Globex": the allow list of "Field" leaves it out`,
		},
		"a default nothing enables": {
			"    agents: {enable: [lead]}\n    projects:\n      - {name: Field, agents: {default: helper}}\n", platformAgentsYAML,
			`workspace.projects[0].agents.default: helper is not available in "Field": nothing on its chain enables it`,
		},
	} {
		_, err := loadBody(t, platformDoc(tc.workspace, tc.agents))
		if want := `platform "default": ` + tc.want; err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v, want %q", name, err, want)
		}
	}
}

// The design's own example passes, and a default set where the console's
// lists may enable it is left to the start.
func TestAgentListsTheStoreTakesPass(t *testing.T) {
	workspace := `    agents: {enable: ["*"], default: auto}
    projects:
      - {name: Field, agents: {enable: [helper], default: helper}}
      - {name: Globex, parent: Field, kind: account, agents: {allow: [globex-helper, warp-writer], default: globex-helper}}
      - {name: Initech, parent: Field, agents: {default: warp-analyst}}
`
	cfg, err := loadBody(t, platformDoc(workspace, fileAgentsYAML))
	if err != nil {
		t.Fatal(err)
	}
	w := cfg.Platforms[0].Spec.Workspace
	if w.Agents.Default != AutoAgent || w.Projects[1].Agents.Allow[0] != "globex-helper" || cfg.Platforms[0].Spec.Agents[2].Project != "Globex" {
		t.Fatalf("loaded = %+v", w)
	}
	// The workspace's lists are the console's here, so a default on a
	// project may be enabled there.
	if _, err := loadBody(t, platformDoc("    projects:\n      - {name: Field, agents: {default: helper}}\n", platformAgentsYAML)); err != nil {
		t.Fatalf("a default the console may enable: %v", err)
	}
	// An agent read from a file is named only at start, so its name is
	// left to the start.
	if _, err := loadBody(t, platformDoc("    agents: {enable: [\"*\"], default: from-file}\n", "    - file: agents/x.json\n")); err != nil {
		t.Fatalf("a name only an agent file can hold: %v", err)
	}
}

// The language's agent words and bounds are the project store's.
func TestTheLanguagesAgentWordsAreTheProjectStores(t *testing.T) {
	if AutoAgent != projects.Auto || EveryAgent != projects.Everyone || maxAgentList != projects.MaxAgentList {
		t.Fatalf("spec %q %q %d, store %q %q %d", AutoAgent, EveryAgent, maxAgentList, projects.Auto, projects.Everyone, projects.MaxAgentList)
	}
}
