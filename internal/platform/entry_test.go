package platform_test

import (
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/orchagent/flow/runtime"
	"github.com/urmzd/orchagent/flow/turn/native/agenttest"
	saigetypes "github.com/urmzd/saige/agent/types"
)

// withEntry runs a platform that ships its own agents, the entry agent among
// them, on the offline model.
func withEntry(t *testing.T, cfg platform.Config) *platform.Platform {
	t.Helper()
	cfg.TestBuiltinProvider, cfg.TestBuiltinModel = "offline", "offline"
	p := startWith(t, cfg)
	if err := p.EnsureBuiltins(t.Context(), tenant); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestTheEntryAgentOwnsAConsoleSessionThatNamesNobody(t *testing.T) {
	p := withEntry(t, platform.Config{})
	specialists(t, p)
	v, err := p.StartSession(t.Context(), admin, platform.SessionInput{Project: "~", Text: "Why did the deploy fail last night?"})
	if err != nil {
		t.Fatal(err)
	}
	if v.Agent != platform.BuiltinEntry {
		t.Fatalf("the session belongs to %q, want the entry agent %q", v.Agent, platform.BuiltinEntry)
	}
	// The offline model answers no pick, so the best search match answers.
	if m := pickOf(t, p, v.ID); len(m.Parts) != 2 || m.Parts[1].Agent != "ops" {
		t.Fatalf("pick = %+v, want ops answering as the best match", m.Parts)
	}
}

func TestAMentionOrANamedDefaultStillAnswersBeforeTheEntryAgent(t *testing.T) {
	p := withEntry(t, platform.Config{})
	specialists(t, p)
	v, err := p.StartSession(t.Context(), admin, platform.SessionInput{Project: "~", Text: "@analyst what did it cost?"})
	if err != nil || v.Agent != "analyst" {
		t.Fatalf("a mention went to %q (%v), want analyst", v.Agent, err)
	}
	setAgents(t, p, "~", platform.AgentsInput{Enable: []string{"*"}, Default: "writer"})
	v, err = p.StartSession(t.Context(), admin, platform.SessionInput{Project: "~", Text: "Why did the deploy fail?"})
	if err != nil || v.Agent != "writer" {
		t.Fatalf("with a named default, answered by %q (%v), want writer", v.Agent, err)
	}
}

func TestWithoutTheEntryAgentRoutingStillLands(t *testing.T) {
	p := start(t, call.Limits{})
	specialists(t, p)
	v, err := p.StartSession(t.Context(), admin, platform.SessionInput{Project: "~", Text: "Why did the deploy fail?"})
	if err != nil || v.Agent != "ops" {
		t.Fatalf("without built-in agents, answered by %q (%v), want ops by routing", v.Agent, err)
	}
}

func TestTheEntryAgentMayCallEveryOtherAgentAndFollowsNewOnes(t *testing.T) {
	p := withEntry(t, platform.Config{})
	specialists(t, p)
	if err := p.EnsureBuiltins(t.Context(), tenant); err != nil {
		t.Fatal(err)
	}
	r, err := p.Agents.Get(t.Context(), tenant, platform.BuiltinEntry, 0)
	if err != nil {
		t.Fatal(err)
	}
	spawn := r.Spec.Access.Spawn
	for _, want := range []string{"ops", "analyst", "writer", "lead", "helper", platform.BuiltinCodeReviewer} {
		if !slices.Contains(spawn, want) {
			t.Fatalf("the entry agent may not call %s: %v", want, spawn)
		}
	}
	if slices.Contains(spawn, platform.BuiltinEntry) {
		t.Fatalf("the entry agent may call itself: %v", spawn)
	}
	before := r.Rev
	if _, err := p.Agents.Create(t.Context(), tenant, wire.AgentSpec{Name: "biller", Model: "offline", Provider: "offline", SystemPrompt: "Bills."}, "test"); err != nil {
		t.Fatal(err)
	}
	if err := p.EnsureBuiltins(t.Context(), tenant); err != nil {
		t.Fatal(err)
	}
	r, _ = p.Agents.Get(t.Context(), tenant, platform.BuiltinEntry, 0)
	if !slices.Contains(r.Spec.Access.Spawn, "biller") || r.Rev != before+1 {
		t.Fatalf("a new agent: spawn %v at revision %d (was %d)", r.Spec.Access.Spawn, r.Rev, before)
	}
	// Nothing changed, nothing is revised.
	if err := p.EnsureBuiltins(t.Context(), tenant); err != nil {
		t.Fatal(err)
	}
	if again, _ := p.Agents.Get(t.Context(), tenant, platform.BuiltinEntry, 0); again.Rev != r.Rev {
		t.Fatalf("revised with nothing changed: %d then %d", r.Rev, again.Rev)
	}
}

func TestTheEntryAgentIsNamedForTheWorkspace(t *testing.T) {
	p := withEntry(t, platform.Config{EntryAgent: "acme-desk", Wording: func(string) platform.Wording { return platform.Wording{Name: "Acme"} }})
	name, display := p.EntryAgent(tenant)
	if name != "acme-desk" || display != "Acme" {
		t.Fatalf("EntryAgent = %q, %q", name, display)
	}
	if _, err := p.Agents.Get(t.Context(), tenant, "acme-desk", 0); err != nil {
		t.Fatalf("the renamed entry agent was not created: %v", err)
	}
	if _, err := platform.New(platform.Config{Workspace: t.TempDir(), EntryAgent: "Not A Name"}); err == nil {
		t.Fatal("an entry agent name that is no agent name was accepted")
	}
}

// pickJSON is a pick the entry agent's model returns.
func pickJSON(decision, reason string, agents ...string) []saigetypes.Delta {
	if agents == nil {
		agents = []string{}
	}
	b, _ := json.Marshal(platform.Pick{Decision: decision, Agents: agents, Reason: reason, Matched: []string{"deploy"}})
	return say(string(b))
}

// entryCast is a cast whose entry agent keeps one script across its runs:
// its pick, then its answer.
func entryCast(entry [][]saigetypes.Delta, others map[string][][]saigetypes.Delta) *cast {
	says := map[string][][]saigetypes.Delta{platform.BuiltinEntry: entry}
	maps.Copy(says, others)
	return &cast{says: says, continues: true, shape: map[string]func(*wire.AgentSpec){
		"ops":     func(s *wire.AgentSpec) { s.SystemPrompt = "You run deployments and explain why a deploy failed." },
		"analyst": func(s *wire.AgentSpec) { s.SystemPrompt = "You answer questions about cost and spend." },
	}}
}

func stageEntry(t *testing.T, c *cast) *platform.Platform {
	t.Helper()
	p := stage(t, c, platform.Config{TestBuiltinProvider: "offline", TestBuiltinModel: "offline", Limits: call.Limits{MaxDepth: 2, MaxFanOut: 4}})
	setAgents(t, p, "~", platform.AgentsInput{Enable: []string{"*"}, Default: "*"})
	if err := p.EnsureBuiltins(t.Context(), tenant); err != nil {
		t.Fatal(err)
	}
	return p
}

// pickOf is the root's pick message for its first person message.
func pickOf(t *testing.T, p *platform.Platform, root string) platform.SessionMessage {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		v, err := p.ReadSession(t.Context(), admin, root, "", "", false)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range v.Messages {
			if len(m.Parts) > 0 && m.Parts[0].Type == "pick" && m.Status == "done" {
				return m
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("no finished pick in %s: %+v", root, v.Messages)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestTheEntryAgentPicksThenAnswersItselfAndMayStillCallASpecialist(t *testing.T) {
	c := entryCast([][]saigetypes.Delta{
		pickJSON(platform.PickAnswerMyself, "A short question I can take."),
		agenttest.ToolCallResponse("ask-ops", "session_call", map[string]any{"agent": "ops", "text": "Why did last night's deploy fail?"}),
		say("ops found a missing migration."),
	}, map[string][][]saigetypes.Delta{"ops": {say("A migration was missing.")}})
	p := stageEntry(t, c)
	v, err := p.StartSession(t.Context(), admin, platform.SessionInput{Project: "~", Text: "Why did the deploy fail last night?"})
	if err != nil {
		t.Fatal(err)
	}
	if v.Agent != platform.BuiltinEntry {
		t.Fatalf("the root belongs to %q", v.Agent)
	}
	answer := await(t, p, sessionRun(t, v).ID, runs.Completed)
	if answer.Answer != "ops found a missing migration." || answer.Picked != "A short question I can take (matched deploy)." {
		t.Fatalf("answer %q, line %q", answer.Answer, answer.Picked)
	}
	called(t, p, answer.ID, "ops", runs.Completed)
	sent := c.requests(platform.BuiltinEntry)
	if len(sent) < 2 || !strings.Contains(sent[0], "Decide who answers this message") || !strings.Contains(sent[0], "@ops (matched deploy") {
		t.Fatalf("the pick was not given the search's references: %v", sent)
	}
	// The pick is a run of its own, kept and metered, outside the session.
	pick, err := p.Runs.Get(tenant, runtime.RunID(tenant, "pick-"+answer.TagID))
	if err != nil || pick.Agent != platform.BuiltinEntry || pick.Status != runs.Completed || pick.Thread == v.ID {
		t.Fatalf("pick run = %+v, %v", pick, err)
	}
	if m := pickOf(t, p, v.ID); m.Author.Display == platform.BuiltinEntry || m.Author.Display == "" {
		t.Fatalf("the entry agent is shown as %q, want the workspace's name", m.Author.Display)
	}
}

func TestTheEntryAgentDelegatesAndTheRootShowsEachAnswerInline(t *testing.T) {
	c := entryCast([][]saigetypes.Delta{
		pickJSON(platform.PickDelegate, "Deploys are ops's.", "ops", "nobody-by-that-name"),
	}, map[string][][]saigetypes.Delta{"ops": {say("A migration was missing."), say("It is fixed now.")}})
	p := stageEntry(t, c)
	v, err := p.StartSession(t.Context(), admin, platform.SessionInput{Project: "~", Text: "Why did the deploy fail last night?"})
	if err != nil {
		t.Fatal(err)
	}
	m := pickOf(t, p, v.ID)
	if m.Parts[0].Text != "Deploys are ops's (matched deploy)." || len(m.Parts) != 2 {
		t.Fatalf("pick message = %+v", m.Parts)
	}
	child := m.Parts[1]
	if child.Type != "call" || child.Agent != "ops" || child.Text != "A migration was missing." || child.Status != "done" {
		t.Fatalf("the child's answer is not inline: %+v", child)
	}
	view, err := p.ReadSession(t.Context(), admin, v.ID, "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Children) != 1 || view.Children[0].ID != child.Session || view.Children[0].Agent != "ops" {
		t.Fatalf("children = %+v", view.Children)
	}
	childView, err := p.ReadSession(t.Context(), admin, child.Session, "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	var line string
	for _, msg := range childView.Messages {
		if msg.Run != nil {
			line = msg.Run.Picked
		}
	}
	if line != "Deploys are ops's (matched deploy)." {
		t.Fatalf("the child's answer does not carry the pick's reason: %q", line)
	}
}

func TestAPickThatIsNotAPickFallsBackToTheBestSearchMatch(t *testing.T) {
	c := entryCast([][]saigetypes.Delta{say("I think ops should take it.")}, map[string][][]saigetypes.Delta{"ops": {say("A migration was missing.")}})
	p := stageEntry(t, c)
	v, err := p.StartSession(t.Context(), admin, platform.SessionInput{Project: "~", Text: "Why did the deploy fail last night?"})
	if err != nil {
		t.Fatal(err)
	}
	m := pickOf(t, p, v.ID)
	if len(m.Parts) != 2 || m.Parts[1].Agent != "ops" || !strings.HasPrefix(m.Parts[0].Text, "Picked by search: matched") {
		t.Fatalf("fallback pick = %+v", m.Parts)
	}
}

// When the pick cannot be made, a question about hashing passwords goes to
// the security reviewer, found by what it is for, never to another agent on
// the filler of the question.
func TestAPickThatFailsHandsAPasswordQuestionToTheSecurityReviewer(t *testing.T) {
	c := entryCast([][]saigetypes.Delta{say("Security should take it.")}, map[string][][]saigetypes.Delta{platform.BuiltinSecurityReviewer: {say("No: use argon2id or bcrypt.")}})
	p := stageEntry(t, c)
	v, err := p.StartSession(t.Context(), admin, platform.SessionInput{Project: "~", Text: "In one line: is MD5 acceptable for hashing passwords, and what should I use instead?"})
	if err != nil {
		t.Fatal(err)
	}
	m := pickOf(t, p, v.ID)
	if len(m.Parts) != 2 || m.Parts[1].Agent != platform.BuiltinSecurityReviewer || !strings.HasPrefix(m.Parts[0].Text, "Picked by search: matched") || strings.Contains(m.Parts[0].Text, "instead") {
		t.Fatalf("fallback pick = %+v", m.Parts)
	}
	// The pick it could not make was told what every other agent is for.
	if sent := c.requests(platform.BuiltinEntry); len(sent) == 0 || !strings.Contains(sent[0], "@"+platform.BuiltinWriter+": Writes documents") {
		t.Fatalf("the pick was not told what the other agents are for: %v", sent)
	}
}

// When the pick cannot be made and search finds only a passing word, the
// entry agent answers itself: a weak match is never handed the message.
func TestAPickThatFailsOnAWeakMatchIsAnsweredByTheEntryAgent(t *testing.T) {
	c := entryCast([][]saigetypes.Delta{say("Not sure."), say("It is in the root of the repository.")}, nil)
	p := stageEntry(t, c)
	v, err := p.StartSession(t.Context(), admin, platform.SessionInput{Project: "~", Text: "Where does the repository keep its readme?"})
	if err != nil {
		t.Fatal(err)
	}
	m := pickOf(t, p, v.ID)
	if len(m.Parts) != 1 {
		t.Fatalf("a weak match was handed the message: %+v", m.Parts)
	}
	if answer := await(t, p, sessionRun(t, v).ID, runs.Completed); answer.Agent != platform.BuiltinEntry {
		t.Fatalf("answered by %q, want the entry agent", answer.Agent)
	}
}

func TestARootLeavesOutADelegatedAnswerItsReaderMayNotRead(t *testing.T) {
	c := entryCast([][]saigetypes.Delta{
		pickJSON(platform.PickDelegate, "Deploys are ops's and spend is analyst's.", "ops", "analyst"),
	}, map[string][][]saigetypes.Delta{"ops": {say("A migration was missing.")}, "analyst": {say("It cost three dollars.")}})
	p := stageEntry(t, c)
	seePeople(t, p, operator)
	v, err := p.StartSession(t.Context(), operator, platform.SessionInput{Project: "~", Text: "Why did the deploy fail and what did it cost?"})
	if err != nil {
		t.Fatal(err)
	}
	if m := pickOf(t, p, v.ID); len(m.Parts) != 3 {
		t.Fatalf("before, the pick shows %d parts", len(m.Parts))
	}
	if _, err := p.CreateProject(t.Context(), tenant, projects.Writer{By: "olu"}, platform.ProjectInput{Name: "Vault", Kind: projects.Account}); err != nil {
		t.Fatal(err)
	}
	if err := p.SetAgentHome(t.Context(), tenant, "analyst", "Vault", govern); err != nil {
		t.Fatal(err)
	}
	view, err := p.ReadSession(t.Context(), operator, v.ID, "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range view.Messages {
		for _, part := range m.Parts {
			if part.Agent == "analyst" || strings.Contains(part.Text, "three dollars") {
				t.Fatalf("the reader is shown analyst's answer: %+v", m.Parts)
			}
		}
	}
}
