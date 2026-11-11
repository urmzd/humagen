package platform_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/urmzd/saige/agent/agenttest"
	saigetypes "github.com/urmzd/saige/agent/types"
	"github.com/urmzd/saige/flow/event"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/inbox"
	"github.com/urmzd/humagen/pkg/operations"
	"github.com/urmzd/humagen/pkg/runs"
)

// together is one model message that makes several tool calls: the parts of
// each response, renumbered to play as one stream.
func together(responses ...[]saigetypes.Delta) []saigetypes.Delta {
	renumber := agenttest.Reindexer()
	var out []saigetypes.Delta
	for _, r := range responses {
		for _, d := range r {
			out = append(out, renumber(d))
		}
	}
	return out
}

// questionsFor waits for n questions of a run to be open in the inbox, and
// returns their messages by question id.
func questionsFor(t *testing.T, p *platform.Platform, runID string, n int) map[string]inbox.Message {
	t.Helper()
	found := map[string]inbox.Message{}
	within(t, "every question of the batch reached the inbox", func() bool {
		clear(found)
		for _, kind := range []inbox.Kind{inbox.KindApproval, inbox.KindClarification} {
			for _, m := range p.Inbox.OpenOf(tenant, kind) {
				if m.Source.Run == runID && m.Parts[0].Question != nil {
					found[m.Parts[0].Question.ID] = m
				}
			}
		}
		return len(found) == n
	})
	return found
}

// lifecycleOf is the events of one lifecycle type a run published.
func lifecycleOf(p *platform.Platform, runID, typ string) []event.Envelope {
	var out []event.Envelope
	for _, e := range kept(p.Journal.Events(tenant, []string{runID})) {
		if e.Type == typ {
			out = append(out, e)
		}
	}
	return out
}

// A model message whose tool calls each wait on a person parks the run once,
// on all of them: the run carries every question, each is its own inbox
// message addressed by its own rules, a reader is told how they stand to
// each, the trace shows them all, and the run resumes once, when the last is
// answered, with every answer.
func TestABatchOfWaitingCallsParksOnceOnEveryQuestion(t *testing.T) {
	c := &cast{continues: true,
		says: map[string][][]saigetypes.Delta{
			"batcher": {
				together(
					agenttest.ToolCallResponse("call-colour", "ask_person", map[string]any{"question": "Which colour?", "options": []any{"red", "blue"}}),
					agenttest.ToolCallResponse("call-ticket", "ask_person", map[string]any{"question": "What is the ticket?"}),
					agenttest.ToolCallResponse("call-write", platform.ToolFileWrite, map[string]any{"path": "plan.md", "content": "ship"}),
				),
				say("Blue, OPS-42, written."),
			},
		},
		tools: map[string][]string{"batcher": {"ask_person", platform.ToolFileWrite}},
		shape: map[string]func(*wire.AgentSpec){
			"batcher": func(s *wire.AgentSpec) {
				s.Approvals, s.Approvers = []string{platform.ToolFileWrite}, []string{"role:operator"}
				s.Answerers = []string{"grace"}
			},
		},
	}
	p := stage(t, c, platform.Config{})
	ctx := t.Context()

	run := ask(t, p, "api-batcher", "batcher")
	asked := run.Waits()
	if len(asked) != 3 || run.Question == nil || run.Question.ID != asked[0].ID || len(run.Questions) != 3 {
		t.Fatalf("the parked run waits on %d questions (question %+v, questions %d), want the three its calls asked", len(asked), run.Question, len(run.Questions))
	}
	kinds := []string{asked[0].Kind, asked[1].Kind, asked[2].Kind}
	if !slices.Equal(kinds, []string{event.QuestionChoice, event.QuestionReply, event.QuestionApproval}) {
		t.Fatalf("the questions are %v, want them in the order of the calls", kinds)
	}
	colour, ticket, write := asked[0], asked[1], asked[2]

	// One park announces all three.
	parks := lifecycleOf(p, run.ID, event.TypeParked)
	if len(parks) != 1 {
		t.Fatalf("the run parked %d times, want once", len(parks))
	}
	if b, _ := wire.LifecycleOf(parks[0]); len(b.Questions) != 3 || b.Question == nil || b.Question.ID != colour.ID {
		t.Fatalf("the park carries %+v", b)
	}

	// Each question is its own message, addressed by its own rules: the
	// answerer is asked the two questions, the operators the approval.
	messages := questionsFor(t, p, run.ID, 3)
	for id, who := range map[string]struct{ sees, not []string }{
		colour.ID: {[]string{"grace"}, []string{"olu"}},
		ticket.ID: {[]string{"grace"}, []string{"olu"}},
		write.ID:  {[]string{"olu", "grace"}, []string{"vic"}},
	} {
		m, ok := messages[id]
		if !ok {
			t.Fatalf("question %s has no message: %v", id, messages)
		}
		people := map[string]bool{"olu": sees(p, olu, m.ID), "grace": sees(p, grace, m.ID), "vic": sees(p, vic, m.ID)}
		for _, name := range who.sees {
			if !people[name] {
				t.Errorf("%s does not see question %s (%s)", name, id, m.Subject)
			}
		}
		for _, name := range who.not {
			if people[name] {
				t.Errorf("%s sees question %s (%s), which is not theirs", name, id, m.Subject)
			}
		}
	}

	// A reader is told how they stand to each question, not only the first.
	turns := p.AsksIn(ctx, grace, []platform.ThreadTurn{{Kind: platform.TurnRun, Run: &run}})
	if got := turns[0].Asks; len(got) != 3 || turns[0].Ask == nil || turns[0].Ask.Question != colour.ID ||
		!got[0].Yours || !got[1].Yours || !got[2].Yours || got[1].Question != ticket.ID || got[2].Question != write.ID {
		t.Fatalf("grace's asks = %+v (first %+v)", turns[0].Asks, turns[0].Ask)
	}
	if got := p.AsksIn(ctx, olu, []platform.ThreadTurn{{Kind: platform.TurnRun, Run: &run}})[0].Asks; len(got) != 3 || got[0].Yours || got[1].Yours || !got[2].Yours {
		t.Fatalf("olu's asks = %+v, want only the approval to be theirs", got)
	}

	// Each is answered on its own, in any order. The run waits until the
	// last answer, and what was answered is settled meanwhile.
	if _, err := p.AnswerQuestion(ctx, olu, run.ID, write.ID, platform.QuestionAnswer{Action: platform.ActApprove}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.AnswerQuestion(ctx, grace, run.ID, colour.ID, platform.QuestionAnswer{Action: platform.ActChoose, Choice: "blue"}); err != nil {
		t.Fatal(err)
	}
	if o := outcomeOf(t, p, messages[write.ID].ID); o == nil || o.Action != platform.ActApprove || o.By != "olu" {
		t.Fatalf("the approval settled as %+v", o)
	}
	if o := outcomeOf(t, p, messages[ticket.ID].ID); o != nil {
		t.Fatalf("the unanswered question settled as %+v", o)
	}
	if still, err := p.Runs.Get(tenant, run.ID); err != nil || still.Status != runs.Parked {
		t.Fatalf("the run is %q (%v) with a question still open, want parked", still.Status, err)
	}
	if n := len(lifecycleOf(p, run.ID, event.TypeResumed)); n != 0 {
		t.Fatalf("the run resumed %d times with a question still open", n)
	}
	// An answer to a question of the batch other than the first or the
	// latest is found too.
	if _, err := p.AnswerQuestion(ctx, grace, run.ID, ticket.ID, platform.QuestionAnswer{Action: platform.ActReply, Text: "OPS-42"}); err != nil {
		t.Fatal(err)
	}
	done := await(t, p, run.ID, runs.Completed)
	if done.Question != nil || len(done.Questions) != 0 || done.Answer != "Blue, OPS-42, written." {
		t.Fatalf("the completed run = question %+v, questions %v, answer %q", done.Question, done.Questions, done.Answer)
	}

	// It resumed once, with every answer, and parked once.
	resumes := lifecycleOf(p, run.ID, event.TypeResumed)
	if len(resumes) != 1 {
		t.Fatalf("the run resumed %d times, want once", len(resumes))
	}
	b, _ := wire.LifecycleOf(resumes[0])
	if len(b.Answers) != 3 || b.Answers[0].QuestionID != colour.ID || b.Answers[0].Choice != "blue" ||
		b.Answers[1].QuestionID != ticket.ID || b.Answers[1].Text != "OPS-42" ||
		b.Answers[2].QuestionID != write.ID || b.Answers[2].Decision != event.DecisionApprove {
		t.Fatalf("the resume carries %+v", b.Answers)
	}
	if n := len(lifecycleOf(p, run.ID, event.TypeParked)); n != 1 {
		t.Fatalf("the run parked %d times in all, want once", n)
	}
	for id, m := range messages {
		if o := outcomeOf(t, p, m.ID); o == nil {
			t.Errorf("question %s is still open after the run ended", id)
		}
	}
	// The model was sent every answer, and the approved call ran once.
	sent := strings.Join(c.requests("batcher"), "\n")
	if !strings.Contains(sent, "blue") || !strings.Contains(sent, "OPS-42") {
		t.Fatalf("the model was not sent both answers: %s", sent)
	}
	wrote := 0
	for _, e := range kept(p.Journal.Events(tenant, []string{run.ID})) {
		if d, ok := e.Data.(event.ToolDone); ok && d.Name == platform.ToolFileWrite {
			wrote++
		}
	}
	if wrote != 1 {
		t.Fatalf("the approved write ran %d times, want once", wrote)
	}
}

// Stopping a run parked on a batch settles every one of its questions, and a
// question of the batch that expires takes its default without ending the
// wait for the others.
func TestStoppingARunParkedOnABatchSettlesEveryQuestion(t *testing.T) {
	c := &cast{continues: true,
		says: map[string][][]saigetypes.Delta{
			"batcher": {
				together(
					agenttest.ToolCallResponse("call-colour", "ask_person", map[string]any{"question": "Which colour?", "options": []any{"red", "blue"}}),
					agenttest.ToolCallResponse("call-write", platform.ToolFileWrite, map[string]any{"path": "plan.md", "content": "ship"}),
				),
				say("Never said."),
			},
		},
		tools: map[string][]string{"batcher": {"ask_person", platform.ToolFileWrite}},
		shape: map[string]func(*wire.AgentSpec){
			"batcher": func(s *wire.AgentSpec) { s.Approvals = []string{platform.ToolFileWrite} },
		},
	}
	p := stage(t, c, platform.Config{})
	run := ask(t, p, "api-batcher", "batcher")
	messages := questionsFor(t, p, run.ID, 2)
	if _, err := p.StopRun(t.Context(), admin, run.ID); err != nil {
		t.Fatal(err)
	}
	await(t, p, run.ID, runs.Stopped)
	for id, m := range messages {
		within(t, "question "+id+" settled as stopped", func() bool {
			o := outcomeOf(t, p, m.ID)
			return o != nil && o.Action == platform.OutcomeStopped
		})
	}
}

// A message's trace shows every question of a batch, each at the call that
// asked it, while the run waits and once each is answered.
func TestATraceShowsEveryQuestionOfABatch(t *testing.T) {
	c := &cast{continues: true,
		says: map[string][][]saigetypes.Delta{
			"batcher": {
				together(
					agenttest.ToolCallResponse("call-colour", "ask_person", map[string]any{"question": "Which colour?", "options": []any{"red", "blue"}}),
					agenttest.ToolCallResponse("call-ticket", "ask_person", map[string]any{"question": "What is the ticket?"}),
					agenttest.ToolCallResponse("call-write", platform.ToolFileWrite, map[string]any{"path": "plan.md", "content": "ship"}),
				),
				say("Blue, OPS-42, written."),
			},
		},
		tools: map[string][]string{"batcher": {"ask_person", platform.ToolFileWrite}},
		shape: map[string]func(*wire.AgentSpec){
			"batcher": func(s *wire.AgentSpec) {
				s.Approvals, s.SelfApproval = []string{platform.ToolFileWrite}, true
			},
		},
	}
	p := stage(t, c, platform.Config{})
	ctx := t.Context()
	setAgents(t, p, "~", platform.AgentsInput{Enable: []string{"*"}, Default: "*"})
	seePeople(t, p, admin)
	v, err := p.StartSession(ctx, admin, platform.SessionInput{Project: "~", Agent: "batcher", Text: "Go"})
	if err != nil {
		t.Fatal(err)
	}
	run := await(t, p, sessionRun(t, v).ID, runs.Parked)
	asked := run.Waits()
	if len(asked) != 3 {
		t.Fatalf("the run waits on %d questions, want 3", len(asked))
	}
	colour, ticket, write := asked[0], asked[1], asked[2]
	questionsFor(t, p, run.ID, 3)

	tr, err := p.MessageTrace(ctx, admin, v.ID)
	if err != nil || len(tr.Nodes) != 1 {
		t.Fatalf("trace = %+v, %v", tr, err)
	}
	node := tr.Nodes[0]
	if len(node.Questions) != 2 || len(node.Approvals) != 1 {
		t.Fatalf("the trace shows %d questions and %d approvals, want 2 and 1: %+v", len(node.Questions), len(node.Approvals), node)
	}
	if node.Questions[0].ID != colour.ID || node.Questions[1].ID != ticket.ID || node.Questions[0].Tool == node.Questions[1].Tool ||
		node.Questions[0].Tool == "" || node.Questions[1].Tool == "" || node.Questions[0].Status != platform.TraceAsked {
		t.Fatalf("the trace's questions = %+v", node.Questions)
	}
	if a := node.Approvals[0]; a.ID != write.ID || a.Decision != platform.TraceAsk || a.Action != platform.ToolFileWrite || a.Tool == "" {
		t.Fatalf("the trace's approval = %+v", a)
	}

	for _, a := range []struct {
		id     string
		answer platform.QuestionAnswer
	}{
		{ticket.ID, platform.QuestionAnswer{Action: platform.ActReply, Text: "OPS-42"}},
		{write.ID, platform.QuestionAnswer{Action: platform.ActApprove}},
		{colour.ID, platform.QuestionAnswer{Action: platform.ActChoose, Choice: "blue"}},
	} {
		if _, err := p.AnswerQuestion(ctx, admin, run.ID, a.id, a.answer); err != nil {
			t.Fatal(err)
		}
	}
	await(t, p, run.ID, runs.Completed)
	tr, err = p.MessageTrace(ctx, admin, v.ID)
	if err != nil || len(tr.Nodes) != 1 {
		t.Fatalf("trace = %+v, %v", tr, err)
	}
	node = tr.Nodes[0]
	if len(node.Questions) != 2 || node.Questions[0].Answer != "blue" || node.Questions[1].Answer != "OPS-42" ||
		len(node.Approvals) != 1 || node.Approvals[0].Decision != platform.TraceAllow {
		t.Fatalf("the settled trace = questions %+v, approvals %+v", node.Questions, node.Approvals)
	}
}

// Two calls to other agents in one model message, each held for a person by
// the action policy, park the caller once on both approvals. One is approved
// and one denied: the approved agent is called, the denied one is not, and
// the caller resumes once.
func TestABatchOfHeldAgentCallsParksOnceOnEveryApproval(t *testing.T) {
	c := entryCast([][]saigetypes.Delta{
		together(
			agentCall("ask-ops", "ops", "Explain the deploy failure", "Why did the deploy fail?"),
			agentCall("ask-analyst", "analyst", "Cost of the failure", "What did the failed deploy cost?"),
		),
		say("ops found a missing migration."),
	}, map[string][][]saigetypes.Delta{
		"ops":     {say("A migration was missing.")},
		"analyst": {say("It cost three dollars.")},
	})
	p := stageEntry(t, c)
	if _, err := p.SetActionPolicy(admin, operations.Scope{Kind: "workspace"}, map[operations.Category]operations.Level{operations.CallAgents: operations.Ask}, 0); err != nil {
		t.Fatal(err)
	}
	v, err := p.StartSession(t.Context(), admin, platform.SessionInput{Project: "~", Text: "Why did the deploy fail and what did it cost?"})
	if err != nil {
		t.Fatal(err)
	}
	run := await(t, p, sessionRun(t, v).ID, runs.Parked)
	asked := run.Waits()
	if len(asked) != 2 {
		t.Fatalf("the caller waits on %d questions, want one approval per held call: %+v", len(asked), asked)
	}
	for i, target := range []string{"ops", "analyst"} {
		q := asked[i]
		if q.Kind != event.QuestionApproval || q.Tool == nil || q.Tool.Name != platform.ToolAgent || !strings.Contains(string(q.Tool.Arguments), `"`+target+`"`) {
			t.Fatalf("question %d = %+v, want the approval of the call to %s", i, q, target)
		}
	}
	questionsFor(t, p, run.ID, 2)
	// Nobody is called before the approvals.
	if tree, _ := p.Runs.Tree(tenant, run.ID); len(tree) != 1 {
		t.Fatalf("an agent was called before anyone approved: %+v", tree)
	}
	if _, err := p.AnswerQuestion(t.Context(), admin, run.ID, asked[1].ID, platform.QuestionAnswer{Action: platform.ActDeny, Note: "not this week"}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.AnswerQuestion(t.Context(), admin, run.ID, asked[0].ID, platform.QuestionAnswer{Action: platform.ActApprove}); err != nil {
		t.Fatal(err)
	}
	done := await(t, p, run.ID, runs.Completed)
	if child := called(t, p, done.ID, "ops", runs.Completed); child.Answer != "A migration was missing." {
		t.Fatalf("ops = %+v", child)
	}
	if len(c.requests("analyst")) != 0 {
		t.Fatal("a call a person denied ran")
	}
	if parks, resumes := len(lifecycleOf(p, run.ID, event.TypeParked)), len(lifecycleOf(p, run.ID, event.TypeResumed)); parks != 1 || resumes != 1 {
		t.Fatalf("the caller parked %d times and resumed %d times, want once each", parks, resumes)
	}
}
