package app

import (
	"fmt"
	"time"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/spec"
)

// expertsOf is the experts a file declares, as the project store keeps them.
func expertsOf(es []spec.Expert) []projects.Expert {
	out := make([]projects.Expert, 0, len(es))
	for _, e := range es {
		out = append(out, projects.Expert{Who: e.Who, Topics: append([]string{}, e.Topics...)})
	}
	return out
}

// skillsOf is the skills a file declares, as the project store keeps them.
func skillsOf(ss []spec.Skill) []projects.Skill {
	if ss == nil {
		return nil
	}
	out := make([]projects.Skill, 0, len(ss))
	for _, s := range ss {
		out = append(out, projects.Skill{Name: s.Name, Description: s.Description, ArgumentHint: s.ArgumentHint, Instructions: s.Instructions})
	}
	return out
}

// fileRoot is the workspace's own record as the file declares it, and the
// fields the file sets: its instructions, experts, knowledge sources,
// skills, members and own kinds.
// None set leaves the root to the console.
func fileRoot(w spec.Workspace) (projects.Project, []string) {
	root := projects.Project{Name: projects.Root}
	var owns []string
	if w.Instructions != nil {
		root.Instructions = *w.Instructions
		owns = append(owns, projects.FieldInstructions)
	}
	if w.Experts != nil {
		root.Experts = expertsOf(w.Experts)
		owns = append(owns, projects.FieldExperts)
	}
	if w.Knowledge != nil {
		root.Knowledge = append([]string{}, w.Knowledge...)
		owns = append(owns, projects.FieldKnowledge)
	}
	if w.Skills != nil {
		root.Skills = skillsOf(w.Skills)
		owns = append(owns, projects.FieldSkills)
	}
	if w.Members != nil {
		root.Members = membersOf(w.Members)
		owns = append(owns, projects.FieldMembers)
	}
	if w.ProjectKinds != nil {
		for _, k := range w.ProjectKinds {
			root.Kinds = append(root.Kinds, projects.KindSpec{Name: projects.Kind(k.Name), Title: k.Title, Like: projects.Kind(k.Like)})
		}
		owns = append(owns, projects.FieldKinds)
	}
	return root, owns
}

// applyChannels merges the file's channels into each project's, as the tree
// is merged: a channel the file declares is created or taken over, with the
// title, purpose, default and Slack binding it sets, which the API then
// answers 409 for. A channel that left the file stays, unmanaged, as does
// every channel of a project that left it. The file's projects are in the
// store by now.
func applyChannels(p *platform.Platform, tenant string, w spec.Workspace) error {
	now := time.Now()
	declared := map[string]map[string]bool{projects.Root: {}}
	put := func(project, at string, cs []spec.Channel) error {
		declared[project] = map[string]bool{}
		for i, c := range cs {
			var bound *projects.SlackBinding
			if c.Slack != nil {
				bound = &projects.SlackBinding{Client: c.Slack.Client, Channel: c.Slack.Channel}
			}
			if _, err := p.Projects.UpsertChannel(tenant, projects.Channel{
				Project: project, Name: c.Name, Title: c.Title, Purpose: c.Purpose, Default: c.Default, Slack: bound,
			}, fileWriter, now); err != nil {
				return fmt.Errorf("platform %q: %s.channels[%d]: %w", tenant, at, i, err)
			}
			declared[project][c.Name] = true
		}
		return nil
	}
	if err := put(projects.Root, "workspace", w.Channels); err != nil {
		return err
	}
	for i, d := range w.Projects {
		if err := put(d.Name, fmt.Sprintf("workspace.projects[%d]", i), d.Channels); err != nil {
			return err
		}
	}
	for _, pr := range append([]projects.Project{{Name: projects.Root}}, p.Projects.List(tenant)...) {
		chans, err := p.Projects.Channels(tenant, pr.Name)
		if err != nil {
			return fmt.Errorf("platform %q: channels of %q: %w", tenant, pr.Name, err)
		}
		for _, c := range chans {
			if c.Managed != projects.ManagedFile || declared[pr.Name][c.Name] {
				continue
			}
			if _, err := p.Projects.ReleaseChannel(tenant, pr.Name, c.Name, fileWriter, now); err != nil {
				return fmt.Errorf("platform %q: let go of #%s in %q: %w", tenant, c.Name, pr.Name, err)
			}
		}
	}
	return nil
}
