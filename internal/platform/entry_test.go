package platform_test

import (
	"encoding/json"
	"fmt"
	"github.com/urmzd/humagen/pkg/inbox"
	"github.com/urmzd/humagen/pkg/projects"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/callpolicy"
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
	// A workspace with every package from the store, as every workspace
	// had before there was one.
	install(t, p, tenant, "humagen/core", "humagen/review")
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
	// The entry agent answers it in one run, in the session: nothing picks first.
	root := sessionRun(t, v)
	if root.Agent != platform.BuiltinEntry || root.Thread != v.ID || root.Parent != "" {
		t.Fatalf("answered by %+v, want the entry agent's own run in the session", root)
	}
	if _, err := p.Runs.Get(tenant, runtime.RunID(tenant, "pick-"+root.TagID)); err == nil {
		t.Fatal("a pick run was started beside the answer")
	}
	noPick(t, p, v.ID)
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

// entryCast is a cast whose entry agent keeps one script across its runs.
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
	install(t, p, tenant, "humagen/core", "humagen/review")
	return p
}

// noPick fails when a root shows why its answer was picked as a message, or
// hands an answer to a child session.
func noPick(t *testing.T, p *platform.Platform, root string) platform.SessionView {
	t.Helper()
	v, err := p.ReadSession(t.Context(), admin, root, "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range v.Messages {
		if m.Run == nil && m.Message == nil {
			t.Fatalf("the conversation shows a message that is neither a person's nor an answer: %+v", m)
		}
		for _, part := range m.Parts {
			if part.Type != "text" {
				t.Fatalf("the conversation shows a %s part: %+v", part.Type, m.Parts)
			}
		}
	}
	if len(v.Children) != 0 {
		t.Fatalf("an answer opened child sessions: %+v", v.Children)
	}
	return v
}

// answers are the runs that answer in a session, oldest first, once n have
// ended.
func answers(t *testing.T, p *platform.Platform, session string, n int) []runs.Run {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		v, err := p.ReadSession(t.Context(), admin, session, "", "", false)
		if err != nil {
			t.Fatal(err)
		}
		var out []runs.Run
		done := 0
		for _, m := range v.Messages {
			if m.Run != nil {
				out = append(out, *m.Run)
				if m.Run.Status.Terminal() {
					done++
				}
			}
		}
		if len(out) >= n && done == len(out) {
			return out
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s has %d answers (%d ended), want %d: %+v", session, len(out), done, n, out)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func agentCall(id, agent, title, brief string) []saigetypes.Delta {
	return agenttest.ToolCallResponse(id, platform.ToolAgent, map[string]any{"agent": agent, "title": title, "brief": brief})
}

func TestTheEntryAgentAnswersInOneRunAndCallsASpecialistWithTheAgentTool(t *testing.T) {
	c := entryCast([][]saigetypes.Delta{
		agentCall("ask-ops", "ops", "Explain the deploy failure", "Why did last night's deploy fail?"),
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
	if answer.Answer != "ops found a missing migration." || answer.Picked != "" {
		t.Fatalf("answer %q, line above it %q", answer.Answer, answer.Picked)
	}
	// The specialist ran as the call's child, under the call's id.
	child := called(t, p, answer.ID, "ops", runs.Completed)
	if child.Parent != answer.ID || child.CallID != "htu_"+answer.ID+"_1" {
		t.Fatalf("the specialist's run = parent %q call %q", child.Parent, child.CallID)
	}
	// One run answers: the first thing the entry agent's model is sent is the
	// message with who may fit it, and no run decides who answers first.
	sent := c.requests(platform.BuiltinEntry)
	if len(sent) != 2 || strings.Contains(sent[0], "Decide who answers") || !strings.Contains(sent[0], "@ops (matched deploy") || !strings.Contains(sent[0], "Why did the deploy fail last night?") {
		t.Fatalf("the entry agent was sent %d requests: %v", len(sent), sent)
	}
	if _, err := p.Runs.Get(tenant, runtime.RunID(tenant, "pick-"+answer.TagID)); err == nil {
		t.Fatal("a pick run was started beside the answer")
	}
	view := noPick(t, p, v.ID)
	for _, m := range view.Messages {
		if m.Run != nil && m.Author.Name != platform.BuiltinEntry {
			t.Fatalf("a called agent's run is a message of the conversation: %+v", m.Author)
		}
	}
}

// The entry agent makes every call with the agent tool: it is told so, and
// the runtime's session tools are not offered to it.
func TestTheEntryAgentIsOfferedTheAgentToolAndNoSessionTools(t *testing.T) {
	c := entryCast([][]saigetypes.Delta{say("Hello.")}, nil)
	p := stageEntry(t, c)
	v, err := p.StartSession(t.Context(), admin, platform.SessionInput{Project: "~", Text: "Hello there"})
	if err != nil {
		t.Fatal(err)
	}
	await(t, p, sessionRun(t, v).ID, runs.Completed)
	offered := c.offered(platform.BuiltinEntry)
	if !slices.Contains(offered, platform.ToolAgent) {
		t.Fatalf("the entry agent is not offered the agent tool: %v", offered)
	}
	for _, name := range offered {
		if strings.HasPrefix(name, "session_") {
			t.Fatalf("the entry agent is offered %s: %v", name, offered)
		}
	}
	r, err := p.Agents.Get(t.Context(), tenant, platform.BuiltinEntry, 0)
	if err != nil {
		t.Fatal(err)
	}
	if prompt := r.Spec.SystemPrompt; strings.Contains(prompt, "session_call") || strings.Contains(prompt, "session_listen") || !strings.Contains(prompt, "agent tool") {
		t.Fatalf("the entry agent's prompt names the wrong way to call: %q", prompt)
	}
}

// Exactly one agent mentioned in the entry agent's root answers the message
// itself, there: it is the response's root, the entry agent does not run,
// and no child session is opened.
func TestTheOneAgentAMessageMentionsIsTheRootOfItsAnswer(t *testing.T) {
	c := entryCast([][]saigetypes.Delta{say("Hello, I am the entry agent.")}, map[string][][]saigetypes.Delta{"ops": {say("A migration was missing.")}})
	p := stageEntry(t, c)
	v, err := p.StartSession(t.Context(), admin, platform.SessionInput{Project: "~", Text: "Hello there"})
	if err != nil {
		t.Fatal(err)
	}
	await(t, p, sessionRun(t, v).ID, runs.Completed)
	before := len(c.requests(platform.BuiltinEntry))
	if _, err := p.PostSession(t.Context(), admin, v.ID, platform.MessageInput{Text: "@ops why did the deploy fail last night?"}); err != nil {
		t.Fatal(err)
	}
	got := answers(t, p, v.ID, 2)
	root := got[1]
	if root.Agent != "ops" || root.Thread != v.ID || root.Parent != "" || root.Answer != "A migration was missing." {
		t.Fatalf("the mention was answered by %+v", root)
	}
	if root.Picked != "" {
		t.Fatalf("why it answers is a line of the conversation: %q", root.Picked)
	}
	if after := len(c.requests(platform.BuiltinEntry)); after != before {
		t.Fatalf("the entry agent ran for a message that mentioned one agent: %d model calls, then %d", before, after)
	}
	noPick(t, p, v.ID)
	// The mentioned agent has no transcript in the entry agent's session, so
	// it is sent the conversation so far.
	if sent := c.requests("ops"); len(sent) != 1 || !strings.Contains(sent[0], "Hello there") || !strings.Contains(sent[0], "why did the deploy fail last night?") {
		t.Fatalf("the mentioned agent was sent %v", sent)
	}
	// Why it answered is the answer's activity.
	msg := lastPersonMessage(t, p, v.ID)
	trace, err := p.MessageTrace(t.Context(), admin, v.ID+":"+msg)
	if err != nil {
		t.Fatal(err)
	}
	if len(trace.Nodes) != 1 || trace.Nodes[0].Run != root.ID || trace.Nodes[0].Pick == nil || trace.Nodes[0].Pick.Reason != "@ops was mentioned." || trace.Nodes[0].Answer != "A migration was missing." {
		t.Fatalf("trace = %+v", trace.Nodes)
	}
	// The entry agent answers the next message that names nobody, and its
	// own transcript does not hold what it never said.
	if _, err := p.PostSession(t.Context(), admin, v.ID, platform.MessageInput{Text: "Thanks."}); err != nil {
		t.Fatal(err)
	}
	if last := answers(t, p, v.ID, 3)[2]; last.Agent != platform.BuiltinEntry {
		t.Fatalf("the next message was answered by %q", last.Agent)
	}
}

func lastPersonMessage(t *testing.T, p *platform.Platform, session string) string {
	t.Helper()
	v, err := p.ReadSession(t.Context(), admin, session, "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	id := ""
	for _, m := range v.Messages {
		if m.Message != nil {
			id = m.Message.ID
		}
	}
	return id
}

// Several agents mentioned: the entry agent is the root. The platform asks
// each of them before its first model step, as agent calls of its run, and
// it is sent what they said as content of the person's turn, never as tool
// calls it made, with the answer to write: each result as given, under its
// own heading, then what it adds.
func TestSeveralMentionedAgentsAreCalledByThePlatformBeforeTheEntryAgentAnswers(t *testing.T) {
	want := "## @ops\n\nA migration was missing.\n\n## @analyst\n\nIt cost **three** dollars.\n\nBoth point at the migration."
	c := entryCast([][]saigetypes.Delta{say(want)}, map[string][][]saigetypes.Delta{
		"ops":     {say("A migration was missing.")},
		"analyst": {say("It cost **three** dollars.")},
	})
	p := stageEntry(t, c)
	v, err := p.StartSession(t.Context(), admin, platform.SessionInput{Project: "~", Text: "@ops @analyst why did the deploy fail and what did it cost?"})
	if err != nil {
		t.Fatal(err)
	}
	if v.Agent != platform.BuiltinEntry {
		t.Fatalf("the root belongs to %q, want the entry agent", v.Agent)
	}
	root := await(t, p, sessionRun(t, v).ID, runs.Completed)
	if root.Agent != platform.BuiltinEntry || root.Answer != want {
		t.Fatalf("answer by %q:\n%s\nwant:\n%s", root.Agent, root.Answer, want)
	}
	// One answer in the conversation, no child sessions, no pick line.
	if got := answers(t, p, v.ID, 1); len(got) != 1 || got[0].Picked != "" {
		t.Fatalf("answers = %+v", got)
	}
	noPick(t, p, v.ID)
	// Each mentioned agent ran as an agent call of the root, in the order
	// written, under the platform's ids.
	for i, agent := range []string{"ops", "analyst"} {
		child := called(t, p, root.ID, agent, runs.Completed)
		if child.Parent != root.ID || child.CallID != fmt.Sprintf("htu_%s_%d", root.ID, i+1) || child.Thread != v.ID {
			t.Fatalf("%s ran as parent %q call %q thread %q", agent, child.Parent, child.CallID, child.Thread)
		}
		if sent := c.requests(agent); len(sent) != 1 || !strings.Contains(sent[0], "why did the deploy fail and what did it cost?") {
			t.Fatalf("%s was asked %v", agent, sent)
		}
	}
	// The entry agent's model made one call, after the agents answered, and
	// was sent their answers as content of the person's turn.
	sent := c.requests(platform.BuiltinEntry)
	if len(sent) != 1 {
		t.Fatalf("the entry agent's model was called %d times: %v", len(sent), sent)
	}
	for _, part := range []string{`<answer agent="ops">`, "A migration was missing.", `<answer agent="analyst">`, "It cost **three** dollars.", "exactly as it is given", `"## @name"`, "why did the deploy fail and what did it cost?"} {
		if !strings.Contains(sent[0], part) {
			t.Fatalf("the entry agent was not sent %q: %s", part, sent[0])
		}
	}
	if turns := c.turns(platform.BuiltinEntry); len(turns) != 1 || turns[0].toolCalls != 0 || turns[0].toolResults != 0 {
		t.Fatalf("the entry agent's model was shown tool calls it did not make: %+v", turns)
	}
	// Activity shows the two calls on the root, each with the run beneath
	// it, and why they were made.
	trace, err := p.MessageTrace(t.Context(), admin, v.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(trace.Nodes) != 1 {
		t.Fatalf("trace = %+v", trace.Nodes)
	}
	n := trace.Nodes[0]
	if n.Run != root.ID || n.Pick == nil || !slices.Equal(n.Pick.Agents, []string{"ops", "analyst"}) || !strings.Contains(n.Pick.Reason, "were mentioned") || n.Answer != want {
		t.Fatalf("root node = %+v, pick %+v", n, n.Pick)
	}
	if len(n.Tools) != 2 || n.Tools[0].Name != platform.ToolAgent || n.Tools[0].ID != "htu_"+root.ID+"_1" || n.Tools[0].Output != "A migration was missing." || n.Tools[1].ID != "htu_"+root.ID+"_2" || n.Tools[1].Output != "It cost **three** dollars." {
		t.Fatalf("root tools = %+v", n.Tools)
	}
	var in struct{ Agent, Title, Brief string }
	if err := json.Unmarshal(n.Tools[0].Input, &in); err != nil || in.Agent != "ops" || in.Title == "" || len([]rune(in.Title)) > platform.MaxAgentCallTitle || !strings.Contains(in.Brief, "why did the deploy fail") {
		t.Fatalf("the call's input = %s (%v)", n.Tools[0].Input, err)
	}
	if len(n.Children) != 2 || n.Children[0].Agent != "ops" || n.Children[1].Agent != "analyst" {
		t.Fatalf("root children = %+v", n.Children)
	}
	// What the agents spent is the root's spending too.
	if root.Usage == nil || root.Usage.Children.ModelCalls != 2 {
		t.Fatalf("the mentioned agents' usage is not rolled up into the root: %+v", root.Usage)
	}
}

// A mentioned agent that cannot be asked never runs: the entry agent is told
// it did not answer and why, and the others still answer.
func TestAMentionedAgentThatCannotBeAskedIsToldToTheEntryAgent(t *testing.T) {
	c := entryCast([][]saigetypes.Delta{say("## @ops\n\n@ops did not answer.\n\n## @analyst\n\nIt cost three dollars.")}, map[string][][]saigetypes.Delta{
		"ops":     {say("This is never said.")},
		"analyst": {say("It cost three dollars.")},
	})
	p := stageEntry(t, c)
	if _, err := p.SetCallPolicy(t.Context(), tenant, "ops", "test", callpolicy.Policy{Agent: "ops", Callers: callpolicy.Nobody, Approval: callpolicy.Never}); err != nil {
		t.Fatal(err)
	}
	v, err := p.StartSession(t.Context(), admin, platform.SessionInput{Project: "~", Text: "@ops @analyst why did the deploy fail and what did it cost?"})
	if err != nil {
		t.Fatal(err)
	}
	root := await(t, p, sessionRun(t, v).ID, runs.Completed)
	if len(c.requests("ops")) != 0 {
		t.Fatal("an agent whose policy refuses the call ran")
	}
	called(t, p, root.ID, "analyst", runs.Completed)
	sent := c.requests(platform.BuiltinEntry)
	if len(sent) != 1 || !strings.Contains(sent[0], `<answer agent="ops" failed="true">`) || !strings.Contains(sent[0], "@ops did not answer: ") || !strings.Contains(sent[0], "<answer agent=\"analyst\">\nIt cost three dollars.") {
		t.Fatalf("the entry agent was sent %v", sent)
	}
	trace, err := p.MessageTrace(t.Context(), admin, v.ID)
	if err != nil || len(trace.Nodes) != 1 || len(trace.Nodes[0].Tools) != 2 || trace.Nodes[0].Tools[0].Error == "" || trace.Nodes[0].Tools[1].Error != "" || len(trace.Nodes[0].Children) != 1 {
		t.Fatalf("trace = %+v, %v", trace, err)
	}
}

// In a place that is not the console, a message through the messages API is
// answered the same way: the one agent it mentions is the root.
func TestAMentionSentThroughTheMessagesAPIAnswersInThePlacesRoot(t *testing.T) {
	c := entryCast([][]saigetypes.Delta{say("Hello.")}, map[string][][]saigetypes.Delta{"ops": {say("A migration was missing.")}})
	p := stageEntry(t, c)
	ref := &platform.ExternalRef{Type: platform.RefGitHub, Repo: "acme/shop", Kind: "pull", Number: 7}
	out, err := p.SendMessages(t.Context(), admin, sayIn("@ops why did the deploy fail last night?", ref))
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Posted.Runs) != 1 || out.Posted.Runs[0].Agent != "ops" || out.Posted.Runs[0].Thread != out.Root {
		t.Fatalf("posted = %+v", out.Posted)
	}
	th, err := p.Threads.Get(tenant, out.Root)
	if err != nil || th.Session == nil || th.Session.Agent != platform.BuiltinEntry {
		t.Fatalf("the place's root = %+v (%v), want the entry agent's", th.Session, err)
	}
	await(t, p, out.Posted.Runs[0].ID, runs.Completed)
	noPick(t, p, out.Root)
}

// unreachableEntry is a workspace whose entry agent cannot run: its models
// are a model policy the workspace does not have.
func unreachableEntry(t *testing.T, c *cast) *platform.Platform {
	t.Helper()
	p := stage(t, c, platform.Config{TestBuiltinProvider: platform.PolicyProvider, TestBuiltinModel: "gone", Limits: call.Limits{MaxDepth: 2, MaxFanOut: 4}})
	setAgents(t, p, "~", platform.AgentsInput{Enable: []string{"*"}, Default: "*"})
	if err := p.EnsureBuiltins(t.Context(), tenant); err != nil {
		t.Fatal(err)
	}
	return p
}

// When the entry agent cannot run, a strong search match answers as the root
// in its place, and activity says why. A weak match is never handed the
// message.
func TestWhenTheEntryAgentCannotRunAStrongSearchMatchIsTheRoot(t *testing.T) {
	c := entryCast(nil, map[string][][]saigetypes.Delta{"ops": {say("A migration was missing.")}})
	p := unreachableEntry(t, c)
	v, err := p.StartSession(t.Context(), admin, platform.SessionInput{Project: "~", Text: "Why did the deploy fail last night?"})
	if err != nil {
		t.Fatal(err)
	}
	if v.Agent != platform.BuiltinEntry {
		t.Fatalf("the root belongs to %q, want the entry agent", v.Agent)
	}
	root := answers(t, p, v.ID, 1)[0]
	if root.Agent != "ops" || root.Thread != v.ID || root.Parent != "" || root.Answer != "A migration was missing." || root.Picked != "" {
		t.Fatalf("answered by %+v, want ops as the root", root)
	}
	noPick(t, p, v.ID)
	trace, err := p.MessageTrace(t.Context(), admin, v.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(trace.Nodes) != 1 || trace.Nodes[0].Run != root.ID || trace.Nodes[0].Pick == nil || !strings.HasPrefix(trace.Nodes[0].Pick.Reason, "Picked by search: matched") {
		t.Fatalf("trace = %+v", trace.Nodes)
	}
	posted, err := p.PostMessage(t.Context(), admin, v.ID, platform.MessageInput{Text: "Where does the repository keep its readme?"})
	if err != nil {
		t.Fatal(err)
	}
	if len(posted.Runs) != 0 || !strings.Contains(posted.Reason, "model policy") {
		t.Fatalf("a weak match: runs %+v, reason %q", posted.Runs, posted.Reason)
	}
	if len(c.requests(platform.BuiltinEntry)) != 0 {
		t.Fatal("an entry agent that cannot run was started")
	}
}

// Several agents mentioned where the entry agent cannot run: each answers
// for itself, in the session.
func TestSeveralMentionedAgentsAnswerThemselvesWhenTheEntryAgentCannotRun(t *testing.T) {
	c := entryCast(nil, map[string][][]saigetypes.Delta{"ops": {say("A migration was missing.")}, "analyst": {say("It cost three dollars.")}})
	p := unreachableEntry(t, c)
	v, err := p.StartSession(t.Context(), admin, platform.SessionInput{Project: "~", Text: "@ops @analyst why did the deploy fail and what did it cost?"})
	if err != nil {
		t.Fatal(err)
	}
	got := answers(t, p, v.ID, 2)
	said := map[string]string{}
	for _, r := range got {
		said[r.Agent] = r.Answer
		if r.Thread != v.ID || r.Parent != "" {
			t.Fatalf("%s answered in %q under %q, want the session itself", r.Agent, r.Thread, r.Parent)
		}
	}
	if len(got) != 2 || said["ops"] != "A migration was missing." || said["analyst"] != "It cost three dollars." {
		t.Fatalf("answers = %v", said)
	}
	noPick(t, p, v.ID)
}

// Stopping the root while a mentioned agent still works stops that agent's
// run with it, before the entry agent's model is ever called.
func TestStoppingTheRootStopsTheMentionedAgentsItWaitsOn(t *testing.T) {
	c := entryCast([][]saigetypes.Delta{say("unreachable")}, map[string][][]saigetypes.Delta{
		"ops":     {},
		"analyst": {say("It cost three dollars.")},
	})
	c.talks = map[string]talk{"ops": {after: 0, text: "Looking at the"}}
	p := stageEntry(t, c)
	v, err := p.StartSession(t.Context(), admin, platform.SessionInput{Project: "~", Text: "@ops @analyst why did the deploy fail and what did it cost?"})
	if err != nil {
		t.Fatal(err)
	}
	root := sessionRun(t, v)
	ops := called(t, p, root.ID, "ops", runs.Running)
	within(t, "ops is talking", func() bool { _, ok := c.talking.Load("ops"); return ok })
	if _, err := p.StopRun(t.Context(), admin, root.ID); err != nil {
		t.Fatal(err)
	}
	await(t, p, ops.ID, runs.Stopped)
	if stopped := await(t, p, root.ID, runs.Stopped); stopped.Answer != "" {
		t.Fatalf("a root stopped before its first model step answered %q", stopped.Answer)
	}
	if len(c.requests(platform.BuiltinEntry)) != 0 {
		t.Fatal("the entry agent's model was called after its run was stopped")
	}
}

// A mentioned agent whose call policy holds calls for a person is asked only
// once a person approves, as the same call made by the model is: the root
// parks on that approval before anyone is called, and a denial is told to
// the entry agent instead of an answer.
func TestAMentionedAgentWhoseCallsAreHeldWaitsForApproval(t *testing.T) {
	for _, approve := range []bool{true, false} {
		name := "denied"
		if approve {
			name = "approved"
		}
		t.Run(name, func(t *testing.T) {
			c := entryCast([][]saigetypes.Delta{say("## @ops\n\nA migration was missing.\n\n## @analyst\n\nIt cost three dollars.")}, map[string][][]saigetypes.Delta{
				"ops":     {say("A migration was missing.")},
				"analyst": {say("It cost three dollars.")},
			})
			p := stageEntry(t, c)
			setPolicy(t, p, "ops", callpolicy.Policy{Callers: callpolicy.Anyone, Approval: callpolicy.Always, Approvers: []string{"role:admin"}, SelfApproval: true})
			v, err := p.StartSession(t.Context(), admin, platform.SessionInput{Project: "~", Text: "@ops @analyst why did the deploy fail and what did it cost?"})
			if err != nil {
				t.Fatal(err)
			}
			root := sessionRun(t, v)
			var run runs.Run
			within(t, "the root parked or finished", func() bool {
				run, _ = p.Runs.Get(tenant, root.ID)
				return run.Status == runs.Parked || run.Status.Terminal()
			})
			if run.Status != runs.Parked || run.Question == nil {
				t.Fatalf("a held mention did not wait for approval: %+v", run)
			}
			m := questionFor(t, p, run.ID)
			q := m.Parts[0].Question
			if m.Kind != inbox.KindApproval || m.Source.Target != "ops" || !slices.Equal(q.Approvers, []string{"role:admin"}) || !strings.Contains(q.Prompt, "Allow "+platform.BuiltinEntry+" to call ops?") {
				t.Fatalf("question = %+v, source %+v", q, m.Source)
			}
			// Nobody was asked before the approval, the agent that needs none
			// included, and the model was not called.
			if tree, _ := p.Runs.Tree(tenant, run.ID); len(tree) != 1 {
				t.Fatalf("a mentioned agent started before anyone approved: %+v", tree)
			}
			if len(c.requests(platform.BuiltinEntry))+len(c.requests("ops"))+len(c.requests("analyst")) != 0 {
				t.Fatal("a model was called before the approval")
			}
			// The approval is at the call, by the platform's id.
			trace, err := p.MessageTrace(t.Context(), admin, v.ID)
			if err != nil || len(trace.Nodes) != 1 || len(trace.Nodes[0].Approvals) != 1 || trace.Nodes[0].Approvals[0].Tool != "htu_"+run.ID+"_1" || trace.Nodes[0].Approvals[0].Decision != platform.TraceAsk {
				t.Fatalf("trace = %+v, %v", trace, err)
			}
			if _, err := p.AnswerQuestion(t.Context(), olu, run.ID, run.Question.ID, platform.QuestionAnswer{Action: platform.ActApprove}); err == nil {
				t.Fatal("an operator approved a call held for admins")
			}
			answer := platform.QuestionAnswer{Action: platform.ActDeny, Note: "not this week"}
			if approve {
				answer = platform.QuestionAnswer{Action: platform.ActApprove}
			}
			if _, err := p.AnswerQuestion(t.Context(), admin, run.ID, run.Question.ID, answer); err != nil {
				t.Fatal(err)
			}
			done := await(t, p, run.ID, runs.Completed)
			called(t, p, done.ID, "analyst", runs.Completed)
			sent := c.requests(platform.BuiltinEntry)
			if len(sent) != 1 {
				t.Fatalf("the entry agent's model was called %d times", len(sent))
			}
			if approve {
				if child := called(t, p, done.ID, "ops", runs.Completed); child.Answer != "A migration was missing." || child.CallID != "htu_"+done.ID+"_1" {
					t.Fatalf("ops = %+v", child)
				}
				if !strings.Contains(sent[0], "<answer agent=\"ops\">\nA migration was missing.") {
					t.Fatalf("the entry agent was sent %s", sent[0])
				}
				return
			}
			if len(c.requests("ops")) != 0 {
				t.Fatal("a call a person denied ran")
			}
			if !strings.Contains(sent[0], `<answer agent="ops" failed="true">`) || !strings.Contains(sent[0], "a person denied the call") {
				t.Fatalf("the entry agent was sent %s", sent[0])
			}
		})
	}
}

// An answer is limited to what its place's readers may read. An agent that
// lives in a project not everyone who reads the conversation's project reads
// is not asked there, alone or beside others: the poster is told why and
// where to ask it, it never runs, and what the others said is all that
// reaches the answer. One that lives where everyone reads is asked.
func TestAMentionedAgentSomeReadersMayNotReadIsNotAskedThere(t *testing.T) {
	stageHome := func(t *testing.T, in platform.ProjectInput) (*cast, *platform.Platform, platform.SessionView) {
		c := entryCast([][]saigetypes.Delta{say("Hello."), say("## @ops\n\nA migration was missing.\n\n## @analyst\n\nIt cost three dollars.")}, map[string][][]saigetypes.Delta{
			"ops":     {say("A migration was missing."), say("It is fixed.")},
			"analyst": {say("It cost three dollars.")},
		})
		p := stageEntry(t, c)
		if _, err := p.CreateProject(t.Context(), tenant, projects.Writer{By: "olu"}, in); err != nil {
			t.Fatal(err)
		}
		if err := p.SetAgentHome(t.Context(), tenant, "analyst", in.Name, govern); err != nil {
			t.Fatal(err)
		}
		v, err := p.StartSession(t.Context(), admin, platform.SessionInput{Project: "~", Text: "Hello there"})
		if err != nil {
			t.Fatal(err)
		}
		await(t, p, sessionRun(t, v).ID, runs.Completed)
		return c, p, v
	}
	t.Run("everyone reads where it lives", func(t *testing.T) {
		c, p, v := stageHome(t, platform.ProjectInput{Name: "Finance"})
		if !p.Seer(operator).Agent("analyst") {
			t.Fatal("the test needs analyst read by everyone")
		}
		posted, err := p.PostMessage(t.Context(), admin, v.ID, platform.MessageInput{Text: "@ops @analyst why did the deploy fail and what did it cost?"})
		if err != nil || len(posted.Runs) != 1 || posted.Runs[0].Agent != platform.BuiltinEntry || len(posted.Refused) != 0 {
			t.Fatalf("posted: runs %+v, refused %+v, %v", posted.Runs, posted.Refused, err)
		}
		root := await(t, p, posted.Runs[0].ID, runs.Completed)
		called(t, p, root.ID, "analyst", runs.Completed)
		if sent := c.requests(platform.BuiltinEntry); !strings.Contains(sent[len(sent)-1], "<answer agent=\"analyst\">\nIt cost three dollars.") {
			t.Fatalf("the entry agent was sent %s", sent[len(sent)-1])
		}
	})
	t.Run("not everyone reads where it lives", func(t *testing.T) {
		c, p, v := stageHome(t, platform.ProjectInput{Name: "Vault", Kind: projects.Account})
		if !p.Seer(admin).Agent("analyst") || p.Seer(operator).Agent("analyst") {
			t.Fatal("the test needs analyst read by the admin and not by the operator")
		}
		// Beside another agent: that one answers, as the root, and the poster
		// is told why analyst was not asked and where it can be.
		posted, err := p.PostMessage(t.Context(), admin, v.ID, platform.MessageInput{Text: "@ops @analyst why did the deploy fail and what did it cost?"})
		if err != nil {
			t.Fatal(err)
		}
		if len(posted.Runs) != 1 || posted.Runs[0].Agent != "ops" {
			t.Fatalf("answered by %+v, want ops alone", posted.Runs)
		}
		if len(posted.Refused) != 1 || posted.Refused[0].Name != "analyst" || !strings.Contains(posted.Refused[0].Refused, "not everyone who reads") || !strings.Contains(posted.Refused[0].Refused, "cannot answer here. Ask it in Vault") {
			t.Fatalf("refused = %+v", posted.Refused)
		}
		await(t, p, posted.Runs[0].ID, runs.Completed)
		// Mentioned alone, it is not asked either, and nobody answers for it.
		alone, err := p.PostMessage(t.Context(), admin, v.ID, platform.MessageInput{Text: "@analyst what did it cost?"})
		if err != nil || len(alone.Runs) != 0 || len(alone.Refused) != 1 || alone.Refused[0].Name != "analyst" {
			t.Fatalf("mentioned alone: runs %+v, refused %+v, %v", alone.Runs, alone.Refused, err)
		}
		if len(c.requests("analyst")) != 0 {
			t.Fatal("an agent not every reader of the conversation may read ran there")
		}
		view, err := p.ReadSession(t.Context(), admin, v.ID, "", "", false)
		if err != nil {
			t.Fatal(err)
		}
		if raw, _ := json.Marshal(view.Messages); strings.Contains(string(raw), "three dollars") {
			t.Fatalf("analyst's answer is in the conversation: %s", raw)
		}
	})
	// Asked for by the platform, it is refused the same way when where it
	// lives stops being read by everyone between the message and the call.
	t.Run("at the call", func(t *testing.T) {
		c, p, v := stageHome(t, platform.ProjectInput{Name: "Finance"})
		p.BeforeMentionCalls(func() {
			if _, err := p.CreateProject(t.Context(), tenant, projects.Writer{By: "olu"}, platform.ProjectInput{Name: "Vault", Kind: projects.Account}); err != nil {
				t.Error(err)
			}
			if err := p.SetAgentHome(t.Context(), tenant, "analyst", "Vault", govern); err != nil {
				t.Error(err)
			}
		})
		posted, err := p.PostMessage(t.Context(), admin, v.ID, platform.MessageInput{Text: "@ops @analyst why did the deploy fail and what did it cost?"})
		if err != nil || len(posted.Runs) != 1 || posted.Runs[0].Agent != platform.BuiltinEntry {
			t.Fatalf("posted: runs %+v, %v", posted.Runs, err)
		}
		root := await(t, p, posted.Runs[0].ID, runs.Completed)
		called(t, p, root.ID, "ops", runs.Completed)
		if len(c.requests("analyst")) != 0 {
			t.Fatal("an agent not every reader of the conversation may read ran there")
		}
		sent := c.requests(platform.BuiltinEntry)
		if last := sent[len(sent)-1]; !strings.Contains(last, `<answer agent="analyst" failed="true">`) || !strings.Contains(last, "cannot answer here. Ask it in Vault") || strings.Contains(last, "three dollars") {
			t.Fatalf("the entry agent was sent %s", last)
		}
	})
}
