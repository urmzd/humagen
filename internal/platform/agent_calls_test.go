package platform_test

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/urmzd/humagen/internal/legacy"
	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/inbox"
	"github.com/urmzd/humagen/pkg/operations"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/orchagent/flow/event"
	"github.com/urmzd/saige/agent/agenttest"
	saigetypes "github.com/urmzd/saige/agent/types"
)

// The agent tool is the one way a run calls an agent. A background call is
// that same call not waited on: it answers at once that the work was
// launched, with the run it started, and that run carries the call's id.
func TestABackgroundAgentCallAnswersAtOnceThatTheWorkWasLaunched(t *testing.T) {
	c := &cast{
		says: map[string][][]saigetypes.Delta{
			"lead":   {handing("c1", "worker", "Write the long report"), say("Handed over.")},
			"worker": {say("The report is written.")},
		},
		spawn: map[string][]string{"lead": {"worker"}},
		gates: map[string]chan struct{}{"worker": make(chan struct{})},
	}
	p := stage(t, c, platform.Config{Limits: call.Limits{MaxDepth: 2, MaxFanOut: 4}})
	first, err := p.Converse(t.Context(), tenant, platform.Turn{ID: "api-1", Agent: "lead", Text: "Start"})
	if err != nil {
		t.Fatal(err)
	}
	// The caller finishes while the agent it handed the work to still runs.
	done := await(t, p, first.ID, runs.Completed)
	worker := called(t, p, first.ID, "worker", runs.Running)
	if done.Answer != "Handed over." || worker.Lifecycle != runs.Delegated || worker.CallID != "htu_"+first.ID+"_1" {
		t.Fatalf("lead = %q, worker = %+v", done.Answer, worker)
	}
	held, _ := p.Journal.Events(tenant, []string{first.ID})
	var result string
	for _, e := range held {
		if d, ok := e.Data.(event.ToolDone); ok && d.Name == platform.ToolAgent {
			result = d.Result
		}
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(result), &out); err != nil || len(out) != 3 || out["status"] != "launched" || out["agent"] != "worker" || out["run"] != worker.ID {
		t.Fatalf("the call answered %q", result)
	}
	if offered := c.offered("lead"); !slices.Contains(offered, platform.ToolAgent) || slices.Contains(offered, legacy.DelegateTool) {
		t.Fatalf("lead is offered %v", offered)
	}
	c.release("worker")
	await(t, p, worker.ID, runs.Completed)
}

// A background call passes the checks a waited call does: a callee the
// caller may not call is refused, and the refusal is the call's result.
func TestABackgroundAgentCallIsHeldToTheSameChecks(t *testing.T) {
	c := &cast{
		says: map[string][][]saigetypes.Delta{
			"lead":   {handing("c1", "stranger", "Do it"), say("It was refused.")},
			"worker": {say("unreachable")},
		},
		spawn: map[string][]string{"lead": {"worker"}},
	}
	p := stage(t, c, platform.Config{Limits: call.Limits{MaxDepth: 2, MaxFanOut: 4}})
	first, err := p.Converse(t.Context(), tenant, platform.Turn{ID: "api-1", Agent: "lead", Text: "Start"})
	if err != nil {
		t.Fatal(err)
	}
	await(t, p, first.ID, runs.Completed)
	if tree, _ := p.Runs.Tree(tenant, first.ID); len(tree) != 1 {
		t.Fatalf("a refused background call started a run: %+v", tree)
	}
	held, _ := p.Journal.Events(tenant, []string{first.ID})
	refused := false
	for _, e := range held {
		if d, ok := e.Data.(event.ToolDone); ok && d.Name == platform.ToolAgent {
			refused = strings.Contains(d.Error+d.Result, "may not call")
		}
	}
	if !refused {
		t.Fatal("the refusal was not the call's result")
	}
}

// A saved spec that names delegate_task keeps loading and is read as
// granting the agent tool: the old name is not offered, and the approval it
// required is required of the agent tool, for a background call as for a
// waited one.
func TestASpecThatNamesDelegateTaskIsReadAsGrantingTheAgentTool(t *testing.T) {
	c := &cast{continues: true,
		says: map[string][][]saigetypes.Delta{
			"lead":   {handing("c1", "helper", "Check it."), say("Done.")},
			"helper": {say("Checked.")},
		},
		spawn: map[string][]string{"lead": {"helper"}},
		tools: map[string][]string{"lead": {legacy.DelegateTool}},
		shape: map[string]func(*wire.AgentSpec){"lead": func(s *wire.AgentSpec) { s.Approvals = []string{legacy.DelegateTool} }},
	}
	p := stage(t, c, platform.Config{Limits: call.Limits{MaxDepth: 2, MaxFanOut: 4}})
	run := ask(t, p, "api-old", "lead")
	m := questionFor(t, p, run.ID)
	if q := m.Parts[0].Question; m.Kind != inbox.KindApproval || q.Tool == nil || q.Tool.Name != platform.ToolAgent || m.Source.Target != "helper" {
		t.Fatalf("question = %+v", m.Parts[0].Question)
	}
	if offered := c.offered("lead"); !slices.Contains(offered, platform.ToolAgent) || slices.Contains(offered, legacy.DelegateTool) {
		t.Fatalf("lead is offered %v", offered)
	}
	if tree, _ := p.Runs.Tree(tenant, run.ID); len(tree) != 1 {
		t.Fatalf("the call ran before anyone approved: %+v", tree)
	}
	if _, err := p.AnswerQuestion(t.Context(), olu, run.ID, run.Question.ID, platform.QuestionAnswer{Action: platform.ActApprove}); err != nil {
		t.Fatal(err)
	}
	await(t, p, run.ID, runs.Completed)
	if helper := called(t, p, run.ID, "helper", runs.Completed); helper.Lifecycle != runs.Delegated {
		t.Fatalf("helper = %+v", helper)
	}
}

// An approval of the calls to one agent ("tag_<agent>") holds the agent
// tool's calls to that agent and no other, and no tool of that name is
// offered even to a spec that lists it.
func TestAnApprovalOfOneAgentsCallsHoldsOnlyCallsToIt(t *testing.T) {
	c := &cast{continues: true,
		says: map[string][][]saigetypes.Delta{
			"lead":   {tagging("worker", "Count them."), tagging("helper", "Check it."), say("Done.")},
			"helper": {say("Checked.")},
			"worker": {say("Three.")},
		},
		spawn: map[string][]string{"lead": {"helper", "worker"}},
		tools: map[string][]string{"lead": {"tag_helper"}},
		shape: map[string]func(*wire.AgentSpec){"lead": func(s *wire.AgentSpec) { s.Approvals = []string{"tag_helper"} }},
	}
	p := stage(t, c, platform.Config{Limits: call.Limits{MaxDepth: 2, MaxFanOut: 4}})
	run := ask(t, p, "api-one", "lead")
	// The call to the agent nobody holds went through unasked.
	called(t, p, run.ID, "worker", runs.Completed)
	m := questionFor(t, p, run.ID)
	q := m.Parts[0].Question
	if m.Kind != inbox.KindApproval || q.Tool == nil || q.Tool.Name != platform.ToolAgent || m.Source.Target != "helper" || !strings.Contains(q.Prompt, "Allow lead to call helper?") {
		t.Fatalf("question = %+v", q)
	}
	for _, name := range c.offered("lead") {
		if strings.HasPrefix(name, "tag_") {
			t.Fatalf("lead is offered %s", name)
		}
	}
	if _, err := p.AnswerQuestion(t.Context(), mo, run.ID, run.Question.ID, platform.QuestionAnswer{Action: platform.ActApprove}); err != nil {
		t.Fatal(err)
	}
	await(t, p, run.ID, runs.Completed)
	called(t, p, run.ID, "helper", runs.Completed)
}

// A run with a session is offered one way to call an agent: the agent tool.
// The runtime's own calling verbs and per-agent tools are not offered, so a
// model that names one anyway starts nothing. The verbs that read and cite
// another session stay.
func TestARunWithASessionIsOfferedOneWayToCallAnAgent(t *testing.T) {
	c := &cast{says: map[string][][]saigetypes.Delta{
		"lead":   {agenttest.ToolCallResponse("inspect", "session_call", map[string]any{"agent": "helper", "text": "Inspect the details"}), say("I could not hand that on.")},
		"helper": {say("unreachable")},
	}, spawn: map[string][]string{"lead": {"helper"}}}
	p := stage(t, c, platform.Config{Limits: call.Limits{MaxDepth: 2, MaxFanOut: 4}})
	root, err := p.StartSession(t.Context(), admin, platform.SessionInput{Project: "~", Agent: "lead", Text: "Delegate the inspection"})
	if err != nil {
		t.Fatal(err)
	}
	parent := await(t, p, sessionRun(t, root).ID, runs.Completed)
	offered := c.offered("lead")
	for _, name := range []string{platform.ToolAgent, "session_read", "session_link"} {
		if !slices.Contains(offered, name) {
			t.Errorf("lead is not offered %s: %v", name, offered)
		}
	}
	for _, name := range offered {
		if slices.Contains([]string{"session_call", "session_listen", "session_status", legacy.DelegateTool, legacy.CallAgentTool}, name) || strings.HasPrefix(name, "tag_") {
			t.Errorf("lead is offered %s, a second way to call an agent: %v", name, offered)
		}
	}
	if tree, _ := p.Runs.Tree(tenant, parent.ID); len(tree) != 1 {
		t.Fatalf("a verb that is not offered started a run: %+v", tree)
	}
	view, err := p.ReadSession(t.Context(), admin, root.ID, "", "", false)
	if err != nil || len(view.Children) != 0 {
		t.Fatalf("a verb that is not offered opened a session: %+v, %v", view.Children, err)
	}
}

// When the action policy asks about calling agents, a call the platform
// makes for a mention waits for a person as the same call made by the model
// does: the root parks on one approval per call, through the wrapper every
// run tool passes, before anyone is asked. An approved call runs; a denied
// one is told to the entry agent instead of an answer.
func TestAMentionCallTheActionPolicyAsksAboutWaitsForApproval(t *testing.T) {
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
			if _, err := p.SetActionPolicy(admin, operations.Scope{Kind: "workspace"}, map[operations.Category]operations.Level{operations.CallAgents: operations.Ask}, 0); err != nil {
				t.Fatal(err)
			}
			v, err := p.StartSession(t.Context(), admin, platform.SessionInput{Project: "~", Text: "@ops @analyst why did the deploy fail and what did it cost?"})
			if err != nil {
				t.Fatal(err)
			}
			root := sessionRun(t, v)
			parked := func(target string) runs.Run {
				t.Helper()
				var run runs.Run
				within(t, "the root parked on the call to "+target, func() bool {
					run, _ = p.Runs.Get(tenant, root.ID)
					if run.Status.Terminal() {
						return true
					}
					return run.Status == runs.Parked && run.Question != nil && strings.Contains(string(run.Question.Tool.Arguments), `"`+target+`"`)
				})
				if run.Status != runs.Parked {
					t.Fatalf("the call to %s did not wait for approval: %+v", target, run)
				}
				return run
			}
			run := parked("ops")
			q := run.Question
			if q.Kind != event.QuestionApproval || q.Tool == nil || q.Tool.Name != platform.ToolAgent || !strings.Contains(q.Prompt, "Allow "+platform.BuiltinEntry+" to use "+platform.ToolAgent+"?") || !slices.Contains(q.Approvers, "role:manager") {
				t.Fatalf("question = %+v", q)
			}
			// Nobody was asked before the approval, and no model was called.
			if tree, _ := p.Runs.Tree(tenant, run.ID); len(tree) != 1 {
				t.Fatalf("a mentioned agent started before anyone approved: %+v", tree)
			}
			if len(c.requests(platform.BuiltinEntry))+len(c.requests("ops"))+len(c.requests("analyst")) != 0 {
				t.Fatal("a model was called before the approval")
			}
			answer := platform.QuestionAnswer{Action: platform.ActDeny, Note: "not this week"}
			if approve {
				answer = platform.QuestionAnswer{Action: platform.ActApprove}
			}
			if _, err := p.AnswerQuestion(t.Context(), admin, run.ID, q.ID, answer); err != nil {
				t.Fatal(err)
			}
			// Each call is asked about, one at a time.
			run = parked("analyst")
			if _, err := p.AnswerQuestion(t.Context(), admin, run.ID, run.Question.ID, platform.QuestionAnswer{Action: platform.ActApprove}); err != nil {
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
