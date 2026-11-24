package platform_test

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/saige/agent/agenttest"
	saigetypes "github.com/urmzd/saige/agent/types"
	"github.com/urmzd/saige/flow/event"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/library"
	"github.com/urmzd/humagen/pkg/operations"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
)

// skillFound is what skill-runner answers in these tests: one finding,
// with a found_by of the model's own that the host replaces.
const skillFound = `{"summary":"Checked the change.","findings":[{"id":"f1","severity":"warning","title":"Token logged","suggestion":"Drop the log line.","found_by":"someone-else"}]}`

// loading is a platform whose agents are a cast, with skill-runner shipped
// on the offline model, answering skillFound unless the cast says
// otherwise, and the Review package installed. loads sets each agent's
// skills.loadable.
func loading(t *testing.T, c *cast, loads map[string][]string, cfg platform.Config) *platform.Platform {
	t.Helper()
	if c.says == nil {
		c.says = map[string][][]saigetypes.Delta{}
	}
	if c.says[platform.BuiltinSkillRunner] == nil {
		c.says[platform.BuiltinSkillRunner] = [][]saigetypes.Delta{say(skillFound)}
	}
	if c.tools == nil {
		c.tools = map[string][]string{}
	}
	if c.shape == nil {
		c.shape = map[string]func(*wire.AgentSpec){}
	}
	for agent, names := range loads {
		c.tools[agent] = append(c.tools[agent], platform.ToolUseSkill)
		c.shape[agent] = func(s *wire.AgentSpec) { s.Skills = &wire.Skills{Loadable: names} }
	}
	cfg.TestBuiltinProvider, cfg.TestBuiltinModel = "offline", "offline"
	p := stage(t, c, cfg)
	if err := p.EnsureBuiltins(t.Context(), tenant); err != nil {
		t.Fatal(err)
	}
	install(t, p, tenant, "humagen/review")
	t.Cleanup(func() {
		if t.Failed() {
			for _, r := range p.Runs.List(tenant, 20) {
				t.Logf("run %s %s %s err=%q answer=%q", r.ID, r.Agent, r.Status, r.Error, r.Answer)
			}
		}
	})
	return p
}

// publishSkill publishes a library skill in scope, as author, limited to
// tools when tools is not nil.
func publishSkill(t *testing.T, p *platform.Platform, author string, scope library.Scope, name, instructions string, tools []string) {
	t.Helper()
	draft, err := p.SkillLibrary.Draft(tenant, author, scope, library.Definition{Skill: projects.Skill{Name: name, Description: "Checks " + name, Instructions: instructions}, Tools: tools}, 0, nil, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := p.SkillLibrary.Tested(tenant, scope, draft.Name, draft.Version, "trusted-fixture-test"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.SkillLibrary.Publish(tenant, author, scope, draft.Name, draft.Version, time.Now()); err != nil {
		t.Fatal(err)
	}
}

// loadingSkill is a model turn that loads skill with use_skill.
func loadingSkill(id, skill string) []saigetypes.Delta {
	return agenttest.ToolCallResponse(id, platform.ToolUseSkill, map[string]any{"name": skill, "task": "Check the change to the login handler."})
}

// pinnedNames is the names of the skills a run's tag was pinned with.
func pinnedNames(t *testing.T, r runs.Run) []string {
	t.Helper()
	raw := wire.Meta(r.Tag())[runs.MetaSkills]
	if raw == "" {
		return nil
	}
	var skills []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(raw), &skills); err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, s := range skills {
		out = append(out, s.Name)
	}
	return out
}

// skillRunners is the skill-runner runs under a root.
func skillRunners(t *testing.T, p *platform.Platform, root string) []runs.Run {
	t.Helper()
	tree, err := p.Runs.Tree(tenant, root)
	if err != nil {
		t.Fatal(err)
	}
	return slices.DeleteFunc(tree, func(r runs.Run) bool { return r.Agent != platform.BuiltinSkillRunner })
}

// A loaded skill runs as a child of skill-runner, through the agent call:
// the child is told that one skill, its tools narrowed as the skill says,
// its findings come back found_by skill-runner#<skill>, and the root keeps
// the skills its person's message named, and no other.
func TestALoadedSkillRunsAsSkillRunnerWithThatSkillAlone(t *testing.T) {
	c := &cast{says: map[string][][]saigetypes.Delta{
		"lead": {loadingSkill("load-1", "secure-review"), say("Merged the findings.")},
	}}
	p := loading(t, c, map[string][]string{"lead": {"secure-review"}}, platform.Config{Limits: call.Limits{MaxDepth: 2, MaxFanOut: 4}})
	workspace := library.Scope{Kind: "workspace"}
	publishSkill(t, p, admin.Subject, workspace, "secure-review", "FOLLOW THE SECURE REVIEW CHECKLIST", []string{platform.ToolFileRead})
	publishSkill(t, p, admin.Subject, workspace, "named-one", "THE SKILL THE PERSON NAMED", nil)
	first, err := p.Converse(t.Context(), tenant, platform.Turn{ID: "use-skill", Agent: "lead", Text: "$named-one Review the login change.", NamesSkills: true})
	if err != nil {
		t.Fatal(err)
	}
	lead := await(t, p, first.ID, runs.Completed)
	child := called(t, p, lead.ID, platform.BuiltinSkillRunner, runs.Completed)
	if want := "htu_" + lead.ID + "_1"; child.CallID != want || child.Parent != lead.ID || child.Mode != call.Subagent {
		t.Fatalf("skill-runner = call %q (want %q), parent %q, mode %q", child.CallID, want, child.Parent, child.Mode)
	}
	if got := pinnedNames(t, child); !slices.Equal(got, []string{"secure-review"}) {
		t.Fatalf("skill-runner was pinned with %v", got)
	}
	if got := pinnedNames(t, lead); !slices.Equal(got, []string{"named-one"}) {
		t.Fatalf("the root's skills became %v", got)
	}
	sent := strings.Join(c.requests(platform.BuiltinSkillRunner), "\n")
	if !strings.Contains(sent, "FOLLOW THE SECURE REVIEW CHECKLIST") || !strings.Contains(sent, "login handler") || strings.Contains(sent, "THE SKILL THE PERSON NAMED") {
		t.Fatalf("skill-runner was told %s", sent)
	}
	if offered := c.offered(platform.BuiltinSkillRunner); !slices.Contains(offered, platform.ToolFileRead) || slices.Contains(offered, platform.ToolFileList) || slices.Contains(offered, "github_read_context") {
		t.Fatalf("skill-runner was offered %v, past the skill's tools", offered)
	}
	result := strings.Join(c.requests("lead")[1:], "\n")
	if !strings.Contains(result, `skill-runner#secure-review`) || !strings.Contains(result, "Token logged") || strings.Contains(result, "someone-else") {
		t.Fatalf("the load answered %s", result)
	}
}

// Only a skill the agent's spec lists loads, "*" loads any skill of the
// run's project chain, and a skill the run's person cannot read, or that
// is someone's own, does not load. A refused load starts no run.
func TestOnlyALoadableSkillThePersonMayReadLoads(t *testing.T) {
	c := &cast{says: map[string][][]saigetypes.Delta{
		"named": {loadingSkill("named-1", "go-style"), say("Done.")},
		"any": {
			// A run fails after two failed tool turns in a row, so the
			// refused loads are kept apart.
			loadingSkill("any-1", "bob-notes"), loadingSkill("any-2", "go-style"), loadingSkill("any-3", "elsewhere"), say("Done."),
		},
		"none": {loadingSkill("none-1", "go-style"), say("Done.")},
	}, tools: map[string][]string{"none": {platform.ToolUseSkill}}}
	p := loading(t, c, map[string][]string{"named": {"secure-review"}, "any": {wire.AnySkill}}, platform.Config{Limits: call.Limits{MaxDepth: 2, MaxFanOut: 4}})
	publishSkill(t, p, admin.Subject, library.Scope{Kind: "workspace"}, "go-style", "GO STYLE", nil)
	publishSkill(t, p, "bob", library.Scope{Kind: "personal", Subject: "bob"}, "bob-notes", "BOB'S OWN NOTES", nil)
	if _, err := p.Projects.Create(tenant, projects.Project{Name: "Other", Members: []projects.Entry{{Who: admin.Subject, Role: projects.Owner}}}, platform.WriterOf(admin), time.Now()); err != nil {
		t.Fatal(err)
	}
	publishSkill(t, p, admin.Subject, library.Scope{Kind: "project", Project: "Other"}, "elsewhere", "ANOTHER PROJECT'S SKILL", nil)

	answered := func(agent string, turns int) string {
		t.Helper()
		first, err := p.Converse(t.Context(), tenant, platform.Turn{ID: "load-" + agent, Agent: agent, Text: "Review it."})
		if err != nil {
			t.Fatal(err)
		}
		run := await(t, p, first.ID, runs.Completed)
		if got := len(skillRunners(t, p, run.ID)); got != turns {
			t.Fatalf("%s started %d skill-runner runs, want %d", agent, got, turns)
		}
		sent := c.requests(agent)
		return sent[len(sent)-1]
	}
	if sent := answered("named", 0); !strings.Contains(sent, "is not a skill named may load") {
		t.Fatalf("an unlisted skill: %s", sent)
	}
	if sent := answered("none", 0); !strings.Contains(sent, "is not a skill none may load") || !strings.Contains(c.description("none", platform.ToolUseSkill), "No skill is loadable") {
		t.Fatalf("an agent that lists none: %s", sent)
	}
	sent := answered("any", 1)
	if !strings.Contains(sent, "skill-runner#go-style") {
		t.Fatalf("* did not load a workspace skill: %s", sent)
	}
	if got := strings.Count(sent, "is published where this run is"); got != 2 {
		t.Fatalf("%d of the two unreadable skills were refused: %s", got, sent)
	}
	for _, canary := range []string{"BOB'S OWN NOTES", "ANOTHER PROJECT'S SKILL"} {
		if strings.Contains(strings.Join(c.requests(platform.BuiltinSkillRunner), "\n"), canary) {
			t.Fatalf("skill-runner was told %q", canary)
		}
	}
	// The tool lists only the skills the agent may load, with what each is for.
	if said := c.description("any", platform.ToolUseSkill); !strings.Contains(said, "go-style: Checks go-style") || strings.Contains(said, "bob-notes") || strings.Contains(said, "elsewhere") {
		t.Fatalf("the tool listed: %s", said)
	}
}

// A load is an agent call: the tree's fan-out and depth hold it, and nothing
// starts skill-runner but a load, not even an agent whose spawn list names
// it.
func TestALoadIsHeldToTheCallLimitsAndIsTheOnlyWayToSkillRunner(t *testing.T) {
	c := &cast{
		says: map[string][][]saigetypes.Delta{
			"lead":  {loadingSkill("wide-1", "go-style"), loadingSkill("wide-2", "go-style"), say("Done.")},
			"boss":  {tagging("deep", "Review it."), say("Done.")},
			"deep":  {loadingSkill("deep-1", "go-style"), say("Done.")},
			"rogue": {tagging(platform.BuiltinSkillRunner, "Run whatever you like."), say("Done.")},
		},
		spawn: map[string][]string{"boss": {"deep"}, "rogue": {platform.BuiltinSkillRunner}},
	}
	p := loading(t, c, map[string][]string{"lead": {wire.AnySkill}, "deep": {wire.AnySkill}}, platform.Config{Limits: call.Limits{MaxDepth: 1, MaxFanOut: 1}})
	publishSkill(t, p, admin.Subject, library.Scope{Kind: "workspace"}, "go-style", "GO STYLE", nil)
	for _, agent := range []string{"lead", "boss", "rogue"} {
		first, err := p.Converse(t.Context(), tenant, platform.Turn{ID: "limits-" + agent, Agent: agent, Text: "Review it."})
		if err != nil {
			t.Fatal(err)
		}
		run := await(t, p, first.ID, runs.Completed)
		want := 0
		if agent == "lead" {
			want = 1
		}
		if got := len(skillRunners(t, p, run.ID)); got != want {
			t.Fatalf("%s's tree ran skill-runner %d times, want %d", agent, got, want)
		}
	}
	if sent := strings.Join(c.requests("lead"), "\n"); !strings.Contains(sent, "has made 1 of 1 calls") {
		t.Fatalf("a second load past the fan-out: %s", sent)
	}
	if sent := strings.Join(c.requests("deep"), "\n"); !strings.Contains(sent, "is at depth 1 of 1") {
		t.Fatalf("a load past the depth: %s", sent)
	}
	if sent := strings.Join(c.requests("rogue"), "\n"); !strings.Contains(sent, "runs only a skill another agent loads") {
		t.Fatalf("a plain call to skill-runner: %s", sent)
	}
}

// skillListing is skill-runner listing files, then answering skillFound.
func skillListing() [][]saigetypes.Delta {
	return [][]saigetypes.Delta{
		agenttest.ToolCallResponse("list", platform.ToolFileList, map[string]any{"path": "."}),
		say(skillFound),
	}
}

// reviseLead adds tools to lead's spec and lets change set its approval
// rules.
func reviseLead(t *testing.T, p *platform.Platform, tool string, change func(*wire.AgentSpec)) wire.AgentSpec {
	t.Helper()
	rev, err := p.Catalog().Get(t.Context(), tenant, "lead", 0)
	if err != nil {
		t.Fatal(err)
	}
	spec := rev.Spec
	spec.Tools = append(spec.Tools, tool)
	change(&spec)
	if _, err := p.Agents.Revise(t.Context(), tenant, spec, rev.Rev, "test"); err != nil {
		t.Fatal(err)
	}
	return spec
}

// Only the caller's approvers answer a question its loaded skill asks: a
// manager the caller does not name may not approve it, and one it names may.
func TestTheCallersApproversAnswerItsLoadedSkill(t *testing.T) {
	c := &cast{continues: true, says: map[string][][]saigetypes.Delta{
		"lead":                      {loadingSkill("load-1", "go-style"), say("Done.")},
		platform.BuiltinSkillRunner: skillListing(),
	}}
	p := loading(t, c, map[string][]string{"lead": {wire.AnySkill}}, platform.Config{Limits: call.Limits{MaxDepth: 2, MaxFanOut: 4}})
	spec := reviseLead(t, p, platform.ToolFileRead, func(s *wire.AgentSpec) {
		s.Approvals, s.Approvers = []string{platform.ToolFileRead}, []string{olu.Subject}
	})
	publishSkill(t, p, admin.Subject, library.Scope{Kind: "workspace"}, "go-style", "GO STYLE", nil)
	if _, err := p.Operations.SetPolicy(tenant, operations.Policy{Scope: operations.Scope{Kind: "agent", Name: "lead"}, Rules: map[operations.Category]operations.Level{operations.ReadContext: operations.Ask}}, 0, "owner", time.Now()); err != nil {
		t.Fatal(err)
	}
	first, err := p.Converse(t.Context(), tenant, platform.Turn{ID: "caller-approvers", Agent: "lead", Text: "Review it.", Actor: event.Actor{ID: admin.Subject}})
	if err != nil {
		t.Fatal(err)
	}
	child := called(t, p, first.ID, platform.BuiltinSkillRunner, runs.Parked)
	if got := questionFor(t, p, child.ID).Parts[0].Question.Approvers; !slices.Equal(got, spec.Approvers) {
		t.Fatalf("the loaded skill's question is for %v, want the caller's %v", got, spec.Approvers)
	}
	if _, err := p.AnswerQuestion(t.Context(), mo, child.ID, child.Question.ID, platform.QuestionAnswer{Action: platform.ActApprove}); err == nil {
		t.Fatalf("mo, whom lead does not name, approved its loaded skill's read")
	}
	if _, err := p.AnswerQuestion(t.Context(), olu, child.ID, child.Question.ID, platform.QuestionAnswer{Action: platform.ActApprove}); err != nil {
		t.Fatalf("olu, whom lead names, could not approve: %v", err)
	}
	await(t, p, first.ID, runs.Completed)
}

// A tool the caller's spec holds for a person waits in its loaded skill too.
func TestTheCallersSpecApprovalsHoldItsLoadedSkill(t *testing.T) {
	c := &cast{continues: true, says: map[string][][]saigetypes.Delta{
		"lead":                      {loadingSkill("load-1", "go-style"), say("Done.")},
		platform.BuiltinSkillRunner: skillListing(),
	}}
	p := loading(t, c, map[string][]string{"lead": {wire.AnySkill}}, platform.Config{Limits: call.Limits{MaxDepth: 2, MaxFanOut: 4}})
	reviseLead(t, p, platform.ToolFileList, func(s *wire.AgentSpec) { s.Approvals = []string{platform.ToolFileList} })
	publishSkill(t, p, admin.Subject, library.Scope{Kind: "workspace"}, "go-style", "GO STYLE", nil)
	first, err := p.Converse(t.Context(), tenant, platform.Turn{ID: "caller-approvals", Agent: "lead", Text: "Review it.", Actor: event.Actor{ID: admin.Subject}})
	if err != nil {
		t.Fatal(err)
	}
	child := called(t, p, first.ID, platform.BuiltinSkillRunner, runs.Parked)
	if q := questionFor(t, p, child.ID).Parts[0].Question; q.Tool == nil || q.Tool.Name != platform.ToolFileList {
		t.Fatalf("the loaded skill parked on %+v, want its file_list", q)
	}
}
