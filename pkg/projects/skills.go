package projects

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// Skill bounds.
const (
	// MaxSkills is how many skills one project declares.
	MaxSkills = 32
	// MaxSkillInstructions bounds a skill's instructions, in characters.
	MaxSkillInstructions = 8000
	// MaxSkillDescription bounds a skill's description, in characters, as a
	// SKILL.md's frontmatter bounds it.
	MaxSkillDescription = 1024
	// MaxArgumentHint bounds what a skill says it takes after its name.
	MaxArgumentHint = 100
	// MaxSkillName bounds a skill's name.
	MaxSkillName = 64
	// MaxChainSkills is how many skills of a project's chain are listed
	// where it is used: the nearest first.
	MaxChainSkills = 128
)

// FieldSkills is the skills a project declares, when the platform file sets
// them.
const FieldSkills = "skills"

// SkillsChanged is what a change to a project's skills records.
const SkillsChanged = "skills"

// Skill is named instructions an agent can be told to use: a person writes
// $<name> in a message, and the runs it starts are given the instructions as
// part of the turn. It is a SKILL.md's frontmatter and body as fields: a name
// and a description a skill package takes, an argument hint, and the
// instructions.
type Skill struct {
	Name         string `json:"name"`
	Description  string `json:"description"`
	ArgumentHint string `json:"argument_hint,omitempty"`
	Instructions string `json:"instructions"`
}

// validSkillName is a skill's name: lower case letters and digits in words
// joined by single dashes, starting with a letter, at most 64. It is a name
// a SKILL.md takes, and a letter first keeps a price, such as $5, from
// reading as one.
var validSkillName = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)

// ValidSkill reports whether a name can name a skill.
func ValidSkill(name string) bool {
	return len(name) <= MaxSkillName && validSkillName.MatchString(name)
}

// validSkills checks the skills a project declares: at most MaxSkills, each
// named once, each with a description and instructions within their bounds.
func validSkills(ss []Skill) error {
	if len(ss) > MaxSkills {
		return fmt.Errorf("%w: a project declares at most %d skills", ErrInvalid, MaxSkills)
	}
	for i, s := range ss {
		switch {
		case !ValidSkill(s.Name):
			return fmt.Errorf("%w: a skill's name is lower case letters and digits in words joined by single dashes, starting with a letter, at most %d; not %q", ErrInvalid, MaxSkillName, s.Name)
		case slices.ContainsFunc(ss[:i], func(o Skill) bool { return o.Name == s.Name }):
			return fmt.Errorf("%w: the skill %s is declared twice", ErrInvalid, s.Name)
		case strings.TrimSpace(s.Description) == "" || utf8.RuneCountInString(s.Description) > MaxSkillDescription || !oneLine(s.Description):
			return fmt.Errorf("%w: the skill %s says what it is for on one line of at most %d characters", ErrInvalid, s.Name, MaxSkillDescription)
		case utf8.RuneCountInString(s.ArgumentHint) > MaxArgumentHint || !oneLine(s.ArgumentHint):
			return fmt.Errorf("%w: the skill %s's argument hint is one line of at most %d characters", ErrInvalid, s.Name, MaxArgumentHint)
		case strings.TrimSpace(s.Instructions) == "" || utf8.RuneCountInString(s.Instructions) > MaxSkillInstructions || !utf8.ValidString(s.Instructions):
			return fmt.Errorf("%w: the skill %s has instructions of at most %d characters", ErrInvalid, s.Name, MaxSkillInstructions)
		}
	}
	return nil
}

// SkillRef is a skill a project's chain offers, without its instructions, and
// the project on the chain that declares it.
type SkillRef struct {
	Name         string `json:"name"`
	Description  string `json:"description"`
	ArgumentHint string `json:"argument_hint"`
	// From is the project that declares it: the project itself, an
	// ancestor, or "" for the workspace.
	From string `json:"from"`
}

// Skills returns the skills a message posted in a project can name: the ones
// the project declares, then each ancestor's, nearest first, the workspace's
// last. A name declared on the way more than once is the nearest project's,
// and past MaxChainSkills the farthest are left out. A sibling's or a child's
// are never offered. It is nil for a name the tree does not hold.
func (t *Tree) Skills(name string) []SkillRef {
	return SkillsOf(t.Chain(name))
}

// SkillsOf is the skills a chain of projects offers, the chain nearest first
// as Tree.Chain returns it: each name once, the nearest project's, at most
// MaxChainSkills.
func SkillsOf(chain []Project) []SkillRef {
	var out []SkillRef
	for _, p := range chain {
		for _, s := range p.Skills {
			if len(out) == MaxChainSkills {
				return out
			}
			if !slices.ContainsFunc(out, func(r SkillRef) bool { return r.Name == s.Name }) {
				out = append(out, SkillRef{Name: s.Name, Description: s.Description, ArgumentHint: s.ArgumentHint, From: p.Name})
			}
		}
	}
	return out
}

// Skill finds a skill by name on a project's chain: the nearest project's
// that declares it, and that project's name.
func (t *Tree) Skill(project, name string) (Skill, string, bool) {
	for _, p := range t.Chain(project) {
		for _, s := range p.Skills {
			if s.Name == name {
				return s, p.Name, true
			}
		}
	}
	return Skill{}, "", false
}

// SetSkills replaces the skills a project declares, in the order given.
// version must be the current one (ErrVersion); skills the file sets are
// changed only by the file (ErrManaged); the root and company projects need
// projects:govern. A change that changes nothing writes nothing and keeps the
// version.
func (s *Store) SetSkills(tenantName, name string, version int, skills []Skill, w Writer, at time.Time) (Project, error) {
	skills = slices.Clone(skills)
	if len(skills) == 0 {
		skills = nil
	}
	if err := validSkills(skills); err != nil {
		return Project{}, err
	}
	return s.update(tenantName, name, version, w, at, SkillsChanged, func(p *Project) error {
		p.Skills = skills
		return nil
	})
}
