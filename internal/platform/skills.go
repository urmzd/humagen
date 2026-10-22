package platform

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/urmzd/fiber/pkg/runtime"

	"github.com/urmzd/warp/pkg/auth"
	"github.com/urmzd/warp/pkg/projects"
	"github.com/urmzd/warp/pkg/runs"
	"github.com/urmzd/warp/pkg/threads"
)

// Skills inherited down the tree. A project declares skills, named
// instructions, and a message posted in it, or below it, names one with
// $<name> or @skill:<name> to have the agents it starts use it:
//
//   - the tag grammar reads the skill (threads.Parse), looked up on the
//     project's chain, the nearest declaration first (projects.Tree.Skill);
//     a skill nobody on the chain declares refuses the message before
//     anything is posted (threads.UnknownSkill);
//   - a message that names a skill and tags no agent starts @auto
//     (startsForSkills), which picks the agent;
//   - when the message is accepted, answerMessage pins each skill's
//     instructions into the run's tag (runs.MetaSkills), so every attempt of
//     the run, after a restart too, is told the same whatever the projects
//     declare later; admission drops the key from every other tag (pin);
//   - the key is private (runs.MetaSkills), so fiber writes it into no
//     prompt and no record of one, a run stopped before it starts included;
//   - before fiber reads the request, withSkills takes the pinned skills out
//     of the request's copy of the tag's meta and puts their instructions
//     ahead of the turn's text, under a line that says they rank below the
//     agent's own instructions.
//
// A Slack client's command whose name is a skill's on the chain uses that
// skill too: the client posts "/<name> ..." as the message, and the runs it
// starts are told to use the skill, without starting @auto, so the client's
// commands work as they did.
//
// A skill is read by whoever reads its project: its instructions are the
// project's, like the project's own instructions.

// pinnedSkill is one skill as a run's tag carries it.
type pinnedSkill struct {
	Name string `json:"name"`
	// From is the project that declared it when the message was accepted.
	From         string `json:"from"`
	Instructions string `json:"instructions"`
	// Description helps @auto pick the agent; it is not pinned.
	Description string `json:"-"`
}

// pickText is what @auto picks an agent for: the message, then what each
// skill it names is for.
func pickText(text string, skills []pinnedSkill) string {
	for _, s := range skills {
		text += "\n" + s.Description
	}
	return text
}

// startsForSkills is what a console message's tags start, with skills
// counted: one that names a skill and tags no agent starts @auto, which is
// added to its tags (threads.WithSkillAgent), and one with no room for it is
// refused. skillTags calls it for a message a person posts in the console as
// themselves, before threads.Decide. A path that sends a message elsewhere,
// such as to the agents a conversation selected, skips it: the runs a message
// starts are told its skills whichever agents they are (answerMessage).
func startsForSkills(ms []threads.Mention) ([]threads.Mention, error) {
	if err := threads.RoomForSkillAgent(ms); err != nil {
		return nil, err
	}
	return threads.WithSkillAgent(ms), nil
}

// skillTags settles a message's skills before it is posted: a skill nobody
// on the chain declares refuses a message typed in the console, which
// nothing is posted for (threads.UnknownSkill); a message a surface posted,
// which is there already, keeps it, refused, so its writer is told. Then a
// message a person posted in the console as themselves (console) that names
// a skill and tags no agent starts @auto (startsForSkills). A message posted
// only is not one: the @auto a skill starts is not written, so a person who
// chose to post only is taken at their word. A message a client posts keeps
// the client's defaults, as its commands do: the default answers with the
// skill, and a client that lists its agents is never sent to @auto. In a
// thread with selected agents (routed) the skill goes to them instead
// (threads.Decide), so no @auto is added.
func skillTags(ms []threads.Mention, surfaced, console, routed bool) ([]threads.Mention, error) {
	if !surfaced {
		if err := threads.UnknownSkill(ms); err != nil {
			return nil, err
		}
	}
	if !console || routed {
		return ms, nil
	}
	return startsForSkills(ms)
}

// skillsOfMessage is the skills the runs a message starts are told to use:
// the ones it names, then the client command it starts with, each found on
// the chain of the thread's project now. One deleted since the message was
// parsed is left out.
func (p *Platform) skillsOfMessage(tenant string, th threads.Thread, m threads.Message, via string) []pinnedSkill {
	var names []string
	for _, mn := range m.Mentions {
		if mn.Kind == threads.Skill && mn.Refused == "" && !slices.Contains(names, mn.Name) {
			names = append(names, mn.Name)
		}
	}
	// The client is the one that posted it now, or for a retry, which
	// passes none, the one the message records it came through.
	if via == "" && m.Via != threads.ViaConsole {
		via = m.Via
	}
	if name, ok := p.commandSkill(tenant, th, m.Text, via); ok && !slices.Contains(names, name) {
		names = append(names, name)
	}
	tree := p.Projects.Tree(tenant)
	var out []pinnedSkill
	for _, n := range names {
		if s, from, ok := tree.Skill(th.Project, n); ok {
			out = append(out, pinnedSkill{Name: n, From: from, Instructions: s.Instructions, Description: s.Description})
		}
	}
	return out
}

// commandSkill is the skill a message posted through a Slack client names
// with one of the client's commands: text that starts with "/<name>", where
// the client offers the command <name>. The client is the one whose
// credential posted the message, or the one whose surface the thread is on.
func (p *Platform) commandSkill(tenant string, th threads.Thread, text, via string) (string, bool) {
	rest, ok := strings.CutPrefix(text, "/")
	if !ok {
		return "", false
	}
	name, _, _ := strings.Cut(rest, " ")
	name, _, _ = strings.Cut(name, "\n")
	if !projects.ValidSkill(name) {
		return "", false
	}
	client := via
	if client == "" && th.Surface != nil {
		client = th.Surface.Client
	}
	if client == "" {
		return "", false
	}
	c, err := p.Clients.Get(tenant, client)
	if err != nil || c.Slack == nil {
		return "", false
	}
	for _, cmd := range c.Slack.Commands {
		if cmd.Name == name {
			return name, true
		}
	}
	return "", false
}

// pinSkills writes the skills a run is told to use into its tag's meta.
func pinSkills(meta map[string]string, skills []pinnedSkill) {
	delete(meta, runs.MetaSkills)
	if len(skills) == 0 {
		return
	}
	raw, err := json.Marshal(skills)
	if err != nil {
		return
	}
	meta[runs.MetaSkills] = string(raw)
}

// skillsPrecedence opens the skills layer of a turn.
const skillsPrecedence = "The person asked you to use these skills for this turn. Each is instructions for a kind of task, written for this workspace: follow them in this answer. " +
	"Your own instructions win over them, and limits, approvals and which agents may run are enforced by the platform, whatever they say."

// withSkills puts the instructions of the skills a run's tag was pinned with
// ahead of the request's text, and takes them out of the request's copy of
// the meta, so nothing past this seam carries them twice. The run's tag keeps
// them, so a later attempt reads them again. A pinned value that does not
// read is refused: only the platform writes it.
func withSkills(req runtime.Request) (runtime.Request, error) {
	raw, ok := req.Meta[runs.MetaSkills]
	if !ok {
		return req, nil
	}
	meta := maps.Clone(req.Meta)
	delete(meta, runs.MetaSkills)
	req.Meta = meta
	var skills []pinnedSkill
	if err := json.Unmarshal([]byte(raw), &skills); err != nil {
		return req, fmt.Errorf("platform: the skills this run was pinned with do not read: %w", err)
	}
	if len(skills) == 0 {
		return req, nil
	}
	parts := []string{skillsPrecedence}
	for _, s := range skills {
		parts = append(parts, fmt.Sprintf("Skill $%s:\n%s", s.Name, strings.TrimSpace(s.Instructions)))
	}
	req.Text = strings.Join(append(parts, req.Text), "\n\n")
	return req, nil
}

// ProjectSkills is the skills a project declares, at the project's version,
// and what a message posted in it can name.
type ProjectSkills struct {
	Version int `json:"version"`
	// Skills is the skills the project declares itself, in its order, with
	// their instructions.
	Skills []projects.Skill `json:"skills"`
	// Offered is what a message posted here can name: the project's skills,
	// then each ancestor's, nearest first, each name once and from the
	// nearest project that declares it, at most projects.MaxChainSkills.
	Offered []projects.SkillRef `json:"offered"`
}

// SkillsInput replaces the skills a project declares, from the version read.
type SkillsInput struct {
	Version int              `json:"version"`
	Skills  []projects.Skill `json:"skills"`
}

// ProjectSkillsOf is a project's skills, "~" for the workspace. The caller
// may read the project.
func (p *Platform) ProjectSkillsOf(tenant, project string) (ProjectSkills, error) {
	name := projects.NameOf(project)
	tree := p.Projects.Tree(tenant)
	pr, ok := tree.Get(name)
	if !ok {
		return ProjectSkills{}, fmt.Errorf("%w: %q", projects.ErrNotFound, project)
	}
	return skillsOf(pr, tree), nil
}

// SetProjectSkills replaces the skills a project declares, "~" for the
// workspace. The rest is the store's: the version read, the file's fields,
// and projects:govern for the workspace and company projects. The caller
// holds ownership of the project.
func (p *Platform) SetProjectSkills(who auth.Principal, project string, w projects.Writer, in SkillsInput) (ProjectSkills, error) {
	pr, err := p.Projects.SetSkills(who.Tenant, projects.NameOf(project), in.Version, in.Skills, w, p.now())
	if err != nil {
		return ProjectSkills{}, err
	}
	return skillsOf(pr, p.Projects.Tree(who.Tenant)), nil
}

// skillsOf is a project's skills and what its chain offers, from the
// version given, not read back, so a write that follows does not change
// what an earlier one answers.
func skillsOf(pr projects.Project, tree *projects.Tree) ProjectSkills {
	out := ProjectSkills{Version: pr.Version, Skills: slices.Clone(pr.Skills), Offered: []projects.SkillRef{}}
	if out.Skills == nil {
		out.Skills = []projects.Skill{}
	}
	chain := []projects.Project{pr}
	if !pr.IsRoot() {
		chain = append(chain, tree.Chain(pr.Parent)...)
	}
	out.Offered = append(out.Offered, projects.SkillsOf(chain)...)
	return out
}

// SkillsIn is the skills a message posted in a project can name, "~" or ""
// for the workspace, without their instructions. The caller may read the
// project.
func (p *Platform) SkillsIn(tenant, project string) ([]projects.SkillRef, error) {
	name := projects.NameOf(project)
	tree := p.Projects.Tree(tenant)
	if !tree.Has(name) {
		return nil, fmt.Errorf("%w: %q", projects.ErrNotFound, project)
	}
	return append([]projects.SkillRef{}, tree.Skills(name)...), nil
}
