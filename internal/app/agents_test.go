package app

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/urmzd/fiber/pkg/agentspec"

	"github.com/urmzd/warp/internal/platform"
	"github.com/urmzd/warp/pkg/projects"
	"github.com/urmzd/warp/pkg/spec"
)

func fileAgent(name, project string) spec.Agent {
	return spec.Agent{Project: project, Spec: &agentspec.AgentSpec{Name: name, Provider: "offline", Model: "offline", SystemPrompt: name + "."}}
}

// The file's agent lists and homes land in order (the workspace, the
// projects, the agents, then where each lives), they are the file's, and a
// second start changes nothing.
func TestTheFilesAgentsAndHomesLand(t *testing.T) {
	p := newProjectsPlatform(t)
	doc := spec.Platform{Metadata: spec.Metadata{Name: "acme"}, Spec: spec.PlatformSpec{
		Workspace: spec.Workspace{
			Agents: &spec.Agents{Enable: []string{"*"}, Default: "auto"},
			Projects: []spec.Project{
				{Name: "Field", Agents: &spec.Agents{Default: "lead"}},
				{Name: "Globex", Parent: "Field", Kind: "account", Agents: &spec.Agents{Allow: []string{"globex-helper", "lead"}, Default: "globex-helper"}},
			},
		},
		Agents: []spec.Agent{fileAgent("lead", ""), fileAgent("globex-helper", "Globex")},
	}}
	cfg := spec.Config{Platforms: []spec.Platform{doc}}
	for range 2 {
		if err := applyPlatforms(t.Context(), p, cfg, nil); err != nil {
			t.Fatal(err)
		}
	}
	tree := p.ProjectTree("acme")
	if home, _ := tree.Home("globex-helper"); home != "Globex" {
		t.Fatalf("globex-helper lives in %q", home)
	}
	if d, from := tree.DefaultOf("Globex"); d != "globex-helper" || from != "Globex" {
		t.Fatalf("Globex's default = %s from %q", d, from)
	}
	if tree.IsAvailable("Field", "globex-helper") || !tree.IsAvailable("Globex", "lead") {
		t.Fatal("availability does not follow the file")
	}
	root := tree.Root()
	if root.Agents.Default != projects.Auto || !slices.Contains(root.FileOwns, projects.FieldAgents) {
		t.Fatalf("the root = %+v", root)
	}
	if h, _ := p.Projects.History("acme", "Globex", 0, 0); len(h) != 2 {
		t.Fatalf("a second start wrote Globex again: %+v", h)
	}
	field := mustGet(t, p, "acme", "Field")
	if _, err := p.SetProjectAgents(t.Context(), "acme", "Field", projects.Writer{By: "ada"}, platform.AgentsInput{Version: field.Version, Enable: []string{}}); !errors.Is(err, projects.ErrManaged) {
		t.Fatalf("changing the lists the file sets = %v", err)
	}
}

// A name the check could not see, in an agent read from a file, fails the
// start rather than a turn.
func TestAListNamingNoAgentFailsTheStart(t *testing.T) {
	p := newProjectsPlatform(t)
	doc := spec.Platform{Metadata: spec.Metadata{Name: "acme"}, Spec: spec.PlatformSpec{
		Workspace: spec.Workspace{Agents: &spec.Agents{Enable: []string{"*"}, Default: "ghost"}},
	}}
	err := applyPlatforms(t.Context(), p, spec.Config{Platforms: []spec.Platform{doc}}, nil)
	if err == nil || !strings.Contains(err.Error(), `workspace.agents: there is no agent called "ghost"`) {
		t.Fatalf("a default naming no agent = %v", err)
	}
}

// The platform's own agents, which a file may name without declaring, are
// the ones the platform provides.
func TestTheLanguagesBuiltinAgentsAreThePlatforms(t *testing.T) {
	if !slices.Equal(spec.BuiltinAgents, platform.Builtins()) {
		t.Fatalf("the spec knows %v, the platform provides %v", spec.BuiltinAgents, platform.Builtins())
	}
}
