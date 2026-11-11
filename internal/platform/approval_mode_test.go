package platform_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	saigetypes "github.com/urmzd/saige/agent/types"
	"github.com/urmzd/saige/flow/event"

	"github.com/urmzd/humagen/internal/legacy"
	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/audit"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/inbox"
	"github.com/urmzd/humagen/pkg/operations"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
)

// judged stages lead, which says hello, then calls helper, in a
// conversation whose calls the workspace's policy asks about. The built-in
// approval agent gives answers, in order, to the calls it is asked about;
// with none given, the workspace has no approval agent. shape sets anything
// else on lead's spec.
func judged(t *testing.T, shape func(*wire.AgentSpec), answers ...string) (*platform.Platform, *cast, runs.Run) {
	t.Helper()
	var script [][]saigetypes.Delta
	if answers != nil {
		script = [][]saigetypes.Delta{}
		for _, a := range answers {
			script = append(script, say(a))
		}
	}
	return judgedBy(t, shape, script)
}

// judgedBy is judged with the approval agent's model scripted turn by turn,
// tool calls included.
func judgedBy(t *testing.T, shape func(*wire.AgentSpec), script [][]saigetypes.Delta) (*platform.Platform, *cast, runs.Run) {
	t.Helper()
	c := &cast{continues: true,
		says: map[string][][]saigetypes.Delta{
			"lead":   {say("Hello."), tagging("helper", "Check it."), say("Done.")},
			"helper": {say("Checked.")},
		},
		spawn: map[string][]string{"lead": {"helper"}},
		tools: map[string][]string{"lead": {legacy.CallAgentTool}},
		shape: map[string]func(*wire.AgentSpec){"lead": shape},
	}
	cfg := platform.Config{Limits: call.Limits{MaxDepth: 2, MaxFanOut: 4}, OfflineHooks: true}
	if script != nil {
		c.says[platform.BuiltinApprover] = script
		cfg.TestBuiltinProvider, cfg.TestBuiltinModel = "offline", "offline"
	}
	p := stage(t, c, cfg)
	if _, err := p.SetActionPolicy(admin, operations.Scope{Kind: "workspace"}, map[operations.Category]operations.Level{operations.CallAgents: operations.Ask}, 0); err != nil {
		t.Fatal(err)
	}
	first, err := p.Converse(t.Context(), tenant, platform.Turn{ID: "api-hello", Agent: "lead", Text: "Hi", Actor: event.Actor{ID: admin.Subject}})
	if err != nil {
		t.Fatal(err)
	}
	return p, c, await(t, p, first.ID, runs.Completed)
}

// selfApproving lets the person who asked approve lead's actions: a spec
// allows that only beside an action it always asks about, here one lead
// never takes.
func selfApproving(s *wire.AgentSpec) {
	s.Tools = append(s.Tools, platform.ToolFileWrite)
	s.Approvals = []string{platform.ToolFileWrite}
	s.SelfApproval = true
}

func next(t *testing.T, p *platform.Platform, after runs.Run) runs.Run {
	t.Helper()
	r, err := p.Converse(t.Context(), tenant, platform.Turn{ID: "api-check", Agent: "lead", Text: "Check it", After: after.ID, Actor: event.Actor{ID: admin.Subject}})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func judgements(t *testing.T, p *platform.Platform) []audit.Record {
	t.Helper()
	rows, err := p.Audit.List(tenant, audit.Query{From: time.Now().Add(-time.Hour), To: time.Now().Add(time.Hour), Limit: 50}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var out []audit.Record
	for _, r := range rows {
		if r.Action == "approval.judged" {
			out = append(out, r)
		}
	}
	return out
}

// answeredFor is how the question run asked was answered, once it was.
func answeredFor(t *testing.T, p *platform.Platform, run string) inbox.Outcome {
	t.Helper()
	var got inbox.Outcome
	within(t, "the question was answered", func() bool {
		all, _ := p.Inbox.Snapshot(tenant)
		for _, m := range all {
			if m.Source.Run != run {
				continue
			}
			for _, part := range m.Parts {
				if part.Outcome != nil {
					got = *part.Outcome
					return true
				}
			}
		}
		return false
	})
	return got
}

// Run hands a call the policy asks about to the approval agent, in a run of
// its own, which allows it as the person who chose Run; the session's answer
// and the audit carry its reason and what it rested on.
func TestTheApprovalAgentAllowsACallInASessionSetToRun(t *testing.T) {
	p, c, first := judged(t, selfApproving, `{"decision": "allow", "reason": "The person asked lead to check it.", "references": ["call_agents asks a person", "call_agent to helper", "admin: Check it"]}`)
	th, err := p.SetApprovalMode(admin, first.Thread, threads.ApprovalAuto)
	if err != nil || !th.Approval.Auto() || th.Approval.By != admin.Subject {
		t.Fatalf("mode = %+v, %v", th.Approval, err)
	}
	second := next(t, p, first)
	await(t, p, second.ID, runs.Completed)
	if o := answeredFor(t, p, second.ID); o.Action != platform.ActApprove || o.By != admin.Subject || o.Note != "Allowed by the approval agent: The person asked lead to check it." {
		t.Fatalf("outcome = %+v", o)
	}
	var got []audit.Record
	within(t, "the decision was audited", func() bool {
		got = judgements(t, p)
		return len(got) == 1
	})
	a := got[0].After
	if got[0].Actor != admin.Subject || a["verdict"] != "allow" || a["tool"] != platform.ToolAgent || !strings.Contains(a["references"], "call_agent to helper") {
		t.Fatalf("audit = %+v", got[0])
	}
	// The decision is a run of the approval agent, which read the call.
	decided, err := p.Runs.Get(tenant, a["approval_run"])
	if err != nil || decided.Agent != platform.BuiltinApprover || decided.Status != runs.Completed || decided.Origin != runs.OriginSystem {
		t.Fatalf("approval run = %+v, %v", decided, err)
	}
	if asked := c.requests(platform.BuiltinApprover); len(asked) != 1 || !strings.Contains(asked[0], "tool: "+platform.ToolAgent) || !strings.Contains(asked[0], "need approval before they run") {
		t.Fatalf("the approval agent was sent %q", asked)
	}
	if _, err := p.SetApprovalMode(olu, first.Thread, "sometimes"); err == nil {
		t.Fatal("an unknown mode was kept")
	}
}

// A denial stops the call, and the agent is told why.
func TestTheApprovalAgentDeniesWithAReasonTheAgentReads(t *testing.T) {
	p, _, first := judged(t, selfApproving, `{"decision": "deny", "reason": "Nobody asked for helper.", "references": ["admin: Check it"]}`)
	if _, err := p.SetApprovalMode(admin, first.Thread, threads.ApprovalAuto); err != nil {
		t.Fatal(err)
	}
	second := next(t, p, first)
	await(t, p, second.ID, runs.Completed)
	if o := answeredFor(t, p, second.ID); o.Action != platform.ActDeny || o.Note != "Denied by the approval agent: Nobody asked for helper." {
		t.Fatalf("outcome = %+v", o)
	}
	within(t, "the denial was audited", func() bool {
		got := judgements(t, p)
		return len(got) == 1 && got[0].After["verdict"] == "deny"
	})
}

// A call the approval agent hands to a person, answers unreadably, or that
// no approval agent is there to decide, waits for a person as if the
// session asked; each hand-off is audited with why.
func TestWhatTheApprovalAgentDoesNotDecideWaitsForAPerson(t *testing.T) {
	for name, answers := range map[string][]string{
		"ask":               {`{"decision": "ask", "reason": "It reaches another team.", "references": []}`},
		"unreadable":        {""},
		"no approval agent": nil,
	} {
		t.Run(name, func(t *testing.T) {
			p, _, first := judged(t, selfApproving, answers...)
			if _, err := p.SetApprovalMode(admin, first.Thread, threads.ApprovalAuto); err != nil {
				t.Fatal(err)
			}
			second := await(t, p, next(t, p, first).ID, runs.Parked)
			if m := questionFor(t, p, second.ID); m.Kind != inbox.KindApproval {
				t.Fatalf("message = %+v", m)
			}
			within(t, "the hand-off was audited", func() bool {
				got := judgements(t, p)
				return len(got) == 1 && got[0].After["verdict"] == "ask" && got[0].After["reason"] != ""
			})
			if r, _ := p.Runs.Get(tenant, second.ID); r.Status != runs.Parked {
				t.Fatalf("the call went through: %s", r.Status)
			}
		})
	}
}

// What the agent itself always asks about goes to a person, whatever the
// session's mode, and the approval agent is not asked.
func TestRunLeavesWhatTheAgentAlwaysAsksAbout(t *testing.T) {
	p, c, first := judged(t, func(s *wire.AgentSpec) {
		s.SelfApproval = true
		s.Approvals = []string{legacy.CallAgentTool}
	}, `{"decision": "allow", "reason": "Fine.", "references": []}`)
	if _, err := p.SetApprovalMode(admin, first.Thread, threads.ApprovalAuto); err != nil {
		t.Fatal(err)
	}
	second := await(t, p, next(t, p, first).ID, runs.Parked)
	m := questionFor(t, p, second.ID)
	if m.Kind != inbox.KindApproval {
		t.Fatalf("message = %+v", m)
	}
	time.Sleep(200 * time.Millisecond)
	if r, _ := p.Runs.Get(tenant, second.ID); r.Status != runs.Parked {
		t.Fatalf("an always-ask call went through: %s", r.Status)
	}
	if got := c.requests(platform.BuiltinApprover); len(got) != 0 {
		t.Fatalf("the approval agent was asked %q", got)
	}
}

// Only a person sets a session to Run.
func TestOnlyAPersonSetsRun(t *testing.T) {
	p, _, first := judged(t, nil)
	machine := admin
	machine.Kind = auth.Machine
	if _, err := p.SetApprovalMode(machine, first.Thread, threads.ApprovalAuto); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("a machine set the mode: %v", err)
	}
}
