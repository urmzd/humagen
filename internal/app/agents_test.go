package app

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/urmzd/humagen/internal/wire"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/spec"
)

func fileAgent(name, project string) spec.Agent {
	return spec.Agent{Project: project, Spec: &wire.AgentSpec{Name: name, Provider: "offline", Model: "offline", SystemPrompt: name + "."}}
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
	if root.Agents.Default != projects.OpenDefault || !slices.Contains(root.FileOwns, projects.FieldAgents) {
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

// A new agent from the file takes no reserved word and no handle a person
// goes by, as through the API, so a tag names one of them. An agent read
// from a file is named only at start, so it is checked there.
func TestAFileAgentNamedLikeAPersonOrAReservedWordFailsTheStart(t *testing.T) {
	p := newProjectsPlatform(t)
	p.SeePerson(auth.Principal{Kind: auth.User, Tenant: "acme", Subject: "okta|00u-dana", Name: "Dana"})
	if person, err := p.People.Get("acme", "okta|00u-dana"); err != nil || person.Handle != "dana" {
		t.Fatalf("Dana = %+v, %v", person, err)
	}
	doc := func(agents ...spec.Agent) spec.Config {
		return spec.Config{Platforms: []spec.Platform{{Metadata: spec.Metadata{Name: "acme"}, Spec: spec.PlatformSpec{Agents: agents}}}}
	}
	err := applyPlatforms(t.Context(), p, doc(fileAgent("dana", "")), nil)
	if err == nil || !strings.Contains(err.Error(), "a person goes by @dana") {
		t.Fatalf("a file agent named like Dana = %v", err)
	}
	dir := t.TempDir()
	for _, name := range []string{"auto", "sme"} {
		path := filepath.Join(dir, name+".json")
		if err := os.WriteFile(path, []byte(`{"name":"`+name+`","provider":"offline","model":"offline","system_prompt":"X."}`), 0o600); err != nil {
			t.Fatal(err)
		}
		err := applyPlatforms(t.Context(), p, doc(spec.Agent{File: path}), nil)
		if err == nil || !strings.Contains(err.Error(), "@"+name+" is reserved") {
			t.Fatalf("an agent file naming %s = %v", name, err)
		}
	}
	for _, name := range []string{"dana", "auto", "sme"} {
		if _, err := p.Agents.Get(t.Context(), "acme", name, 0); err == nil {
			t.Fatalf("the agent %s was created", name)
		}
	}
	// A name nobody holds is taken, and keeps being the agent's at the next
	// start.
	for range 2 {
		if err := applyPlatforms(t.Context(), p, doc(fileAgent("lead", "")), nil); err != nil {
			t.Fatal(err)
		}
	}
}

// The platform's own agents, which a file may name without declaring, are
// the ones the platform provides.
func TestTheLanguagesBuiltinAgentsAreThePlatforms(t *testing.T) {
	if !slices.Equal(spec.BuiltinAgents, platform.Builtins()) {
		t.Fatalf("the spec knows %v, the platform provides %v", spec.BuiltinAgents, platform.Builtins())
	}
}

// An agent file in saige's own format is read with its policy, and lands as
// the same agent a spec would make.
func TestAnAgentMdFileLandsWithItsPolicy(t *testing.T) {
	p := newProjectsPlatform(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "reviewer.agent.md")
	md := "---\napiVersion: saige/v1\nname: reviewer\nversion: 3.0.0\ndescription: Reviews changes.\nmodel:\n  use: offline/offline\n---\nReview the change and list what is risky.\n"
	if err := os.WriteFile(path, []byte(md), 0o600); err != nil {
		t.Fatal(err)
	}
	doc := spec.Platform{Metadata: spec.Metadata{Name: "acme"}, Spec: spec.PlatformSpec{
		Agents: []spec.Agent{{File: path, Policy: &wire.DefinitionPolicy{Answerers: []string{"role:leads"}, TaggedOnly: true}}},
	}}
	if err := applyPlatforms(t.Context(), p, spec.Config{Platforms: []spec.Platform{doc}}, nil); err != nil {
		t.Fatal(err)
	}
	r, err := p.Agents.Get(t.Context(), "acme", "reviewer", 0)
	if err != nil {
		t.Fatal(err)
	}
	if s := r.Spec; s.Provider != "offline" || s.SystemPrompt != "Review the change and list what is risky." || !s.TaggedOnly || !slices.Equal(s.Answerers, []string{"role:leads"}) {
		t.Fatalf("the agent from the file = %+v", s)
	}

	bad := filepath.Join(dir, "bad.agent.md")
	if err := os.WriteFile(bad, []byte("no frontmatter"), 0o600); err != nil {
		t.Fatal(err)
	}
	doc.Spec.Agents = []spec.Agent{{File: bad}}
	if err := applyPlatforms(t.Context(), p, spec.Config{Platforms: []spec.Platform{doc}}, nil); err == nil {
		t.Fatal("an .agent.md file saige cannot read was accepted")
	}
}
