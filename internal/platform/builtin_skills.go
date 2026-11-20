package platform

import (
	_ "embed"
	"errors"
	"fmt"
	"strings"

	"github.com/urmzd/humagen/pkg/library"
	"github.com/urmzd/humagen/pkg/projects"
)

// builtinSkillOwner writes the skills the platform ships, as "platform"
// writes its agents. A revision by anyone else is a person's own.
const builtinSkillOwner = "platform"

// builtinSkillTest stands where a test run's id does on a shipped revision.
// A skill is published only once it is tested; the platform's own are tested
// before they ship, so there is no run of this workspace's to name.
const builtinSkillTest = "builtin"

//go:embed builtin_skills/skill-creator.md
var skillCreatorInstructions string

//go:embed builtin_skills/agent-creator.md
var agentCreatorInstructions string

// builtinSkills are the skills every workspace starts with, in its workspace
// scope, so the chat bar's / menu is never empty: skill-creator drafts a
// skill from a description of a task, for the person to save, and
// agent-creator designs an agent with the person and saves it through the
// reviewed change flow where the agent running it may (Home's Build an Agent).
func builtinSkills() []library.Definition {
	return []library.Definition{{
		Skill: projects.Skill{
			Name:         "skill-creator",
			Description:  "Turns a task you do the same way every time into a skill. Use it when someone wants to create, draft or write a new skill, or asks how to teach the assistant a way of working.",
			ArgumentHint: "the task to turn into a skill",
			Instructions: strings.TrimSpace(skillCreatorInstructions),
		},
	}, {
		Skill: projects.Skill{
			Name:         "agent-creator",
			Description:  "Designs a new agent with you: its job, instructions, tools and tests, then saves it once you approve. Use it when someone wants to create, build or set up an agent, or asks how to get an assistant for a job.",
			ArgumentHint: "what the agent should do",
			Instructions: strings.TrimSpace(agentCreatorInstructions),
		},
	}}
}

// ensureBuiltinSkills gives the workspace each skill the platform ships and
// keeps it current. It follows the rule the built-in agents do: a skill is
// created when the workspace has none of its name, and moved to the shipped
// text only while the platform wrote its latest revision. One a person has
// revised is theirs and is left as it is.
func (p *Platform) ensureBuiltinSkills(tenant string) error {
	// A platform that ships no agents (a test's, an embedder's) ships no skills either.
	if p.SkillLibrary == nil || !p.cfg.ShipBuiltins {
		return nil
	}
	scope := library.Scope{Kind: "workspace"}
	for _, want := range builtinSkills() {
		history, err := p.SkillLibrary.History(tenant, scope, want.Name)
		if err != nil {
			return fmt.Errorf("platform: built-in skill %s: %w", want.Name, err)
		}
		expected := 0
		if n := len(history); n > 0 {
			latest := history[n-1]
			if latest.By != builtinSkillOwner {
				continue
			}
			if latest.Description == want.Description && latest.Instructions == want.Instructions && latest.ArgumentHint == want.ArgumentHint && !latest.PublishedAt.IsZero() {
				continue
			}
			expected = latest.Version
			// A revision written but not published (a start that stopped
			// between the two) is published as it stands when it is current.
			if latest.Description == want.Description && latest.Instructions == want.Instructions && latest.ArgumentHint == want.ArgumentHint {
				if err := p.publishBuiltinSkill(tenant, scope, want.Name, latest.Version); err != nil {
					return err
				}
				continue
			}
		}
		drafted, err := p.SkillLibrary.Draft(tenant, builtinSkillOwner, scope, want, expected, nil, p.now())
		if errors.Is(err, library.ErrConflict) {
			// Another replica, or a person, wrote it first.
			continue
		}
		if err != nil {
			return fmt.Errorf("platform: built-in skill %s: %w", want.Name, err)
		}
		if err := p.publishBuiltinSkill(tenant, scope, want.Name, drafted.Version); err != nil {
			return err
		}
	}
	return nil
}

func (p *Platform) publishBuiltinSkill(tenant string, scope library.Scope, name string, version int) error {
	if err := p.SkillLibrary.Tested(tenant, scope, name, version, builtinSkillTest); err != nil {
		return fmt.Errorf("platform: built-in skill %s: %w", name, err)
	}
	if _, err := p.SkillLibrary.Publish(tenant, builtinSkillOwner, scope, name, version, p.now()); err != nil && !errors.Is(err, library.ErrConflict) {
		return fmt.Errorf("platform: built-in skill %s: %w", name, err)
	}
	return nil
}
