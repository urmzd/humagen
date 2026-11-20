package platform_test

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/urmzd/saige/agent/agenttest"
	saigetypes "github.com/urmzd/saige/agent/types"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/approve"
	"github.com/urmzd/humagen/pkg/inbox"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
)

// judgementOn is the approval agent's judgement on the card for the
// question run asked, once it is there.
func judgementOn(t *testing.T, p *platform.Platform, run string) inbox.Judgement {
	t.Helper()
	var got inbox.Judgement
	within(t, "the judgement reached the card", func() bool {
		all, _ := p.Inbox.Snapshot(tenant)
		for _, m := range all {
			if m.Source.Run != run {
				continue
			}
			for _, part := range m.Parts {
				if part.Question != nil && part.Question.Judgement != nil {
					got = *part.Question.Judgement
					return true
				}
			}
		}
		return false
	})
	return got
}

// The approval agent ships asking its model for the default schema, and
// its decision is read from the answer the run kept as structured data.
func TestTheApprovalAgentAnswersUnderItsDefaultSchema(t *testing.T) {
	// The agent says something first, as a model with tools may, and is
	// then asked for the schema, which its structured reply holds to.
	p, c, first := judged(t, selfApproving, "Looks fine to me.", `{"decision": "allow", "reason": "Asked for.", "references": ["admin: Check it"]}`)
	if err := p.EnsureBuiltins(t.Context(), tenant); err != nil {
		t.Fatal(err)
	}
	rev, err := p.Agents.Get(t.Context(), tenant, platform.BuiltinApprover, 0)
	if err != nil || rev.Spec.Output == nil || string(rev.Spec.Output.Schema) != string(wire.AgentSpec{Output: &wire.Output{Schema: approve.DefaultSchema}}.Normalize().Output.Schema) {
		t.Fatalf("the approval agent's output = %+v, %v", rev.Spec.Output, err)
	}
	if _, err := p.SetApprovalMode(admin, first.Thread, threads.ApprovalAuto); err != nil {
		t.Fatal(err)
	}
	second := next(t, p, first)
	await(t, p, second.ID, runs.Completed)
	if o := answeredFor(t, p, second.ID); o.Action != platform.ActApprove {
		t.Fatalf("outcome = %+v", o)
	}
	c.mu.Lock()
	asked := slices.Clone(c.schemas[platform.BuiltinApprover])
	c.mu.Unlock()
	if len(asked) == 0 || asked[0].Properties["decision"].Type != "string" || !slices.Equal(asked[0].Properties["decision"].Enum, []string{"allow", "ask", "deny"}) {
		t.Fatalf("the model was asked for %+v", asked)
	}
	j := judgementOn(t, p, second.ID)
	if j.Decision != "allow" || j.Reason != "Asked for." || len(j.Fields) != 0 || j.Run == "" {
		t.Fatalf("judgement = %+v", j)
	}
	decided, err := p.Runs.Get(tenant, j.Run)
	if err != nil || !strings.HasPrefix(decided.Answer, "Looks fine to me.") || !json.Valid(decided.Output) || !strings.Contains(string(decided.Output), `"decision":"allow"`) {
		t.Fatalf("the approval run kept %s, %v", decided.Output, err)
	}
}

// A workspace's own schema adds fields to the answer: they are recorded
// with the judgement in the audit and shown on the card, beside the call a
// person is left to decide.
func TestAWorkspacesApprovalSchemaReachesTheAuditAndTheCard(t *testing.T) {
	p, c, first := judged(t, selfApproving, "It reaches another team.", `{"decision": "ask", "reason": "It reaches another team.", "risk": "high", "conditions": ["only on main"], "notify": ["ops"], "expires_in": 3600}`)
	if err := p.EnsureBuiltins(t.Context(), tenant); err != nil {
		t.Fatal(err)
	}
	rev, err := p.Agents.Get(t.Context(), tenant, platform.BuiltinApprover, 0)
	if err != nil {
		t.Fatal(err)
	}
	custom := rev.Spec.Clone()
	custom.Output = &wire.Output{Schema: json.RawMessage(`{"type":"object","required":["decision","reason","risk"],"properties":{"decision":{"type":"string","enum":["allow","ask","deny"]},"reason":{"type":"string"},"risk":{"type":"string","enum":["low","medium","high"]},"conditions":{"type":"array","items":{"type":"string"}},"notify":{"type":"array","items":{"type":"string"}},"expires_in":{"type":"integer"}}}`)}
	if _, err := p.Agents.Revise(t.Context(), tenant, custom, rev.Rev, admin.Subject); err != nil {
		t.Fatal(err)
	}
	if _, err := p.SetApprovalMode(admin, first.Thread, threads.ApprovalAuto); err != nil {
		t.Fatal(err)
	}
	second := await(t, p, next(t, p, first).ID, runs.Parked)
	j := judgementOn(t, p, second.ID)
	want := []inbox.JudgementField{
		{Name: "risk", Label: "Risk", Value: "high"},
		{Name: "conditions", Label: "Conditions", Value: "only on main"},
		{Name: "notify", Label: "Notify", Value: "ops"},
		{Name: "expires_in", Label: "Expires in", Value: "3600"},
	}
	if j.Decision != "ask" || j.Reason != "It reaches another team." || !slices.Equal(j.Fields, want) {
		t.Fatalf("judgement = %+v", j)
	}
	within(t, "the fields were audited", func() bool {
		got := judgements(t, p)
		return len(got) == 1 && got[0].After["verdict"] == "ask" && got[0].After["field.risk"] == "high" && got[0].After["field.expires_in"] == "3600" && got[0].After["field.conditions"] == "only on main"
	})
	c.mu.Lock()
	asked := slices.Clone(c.schemas[platform.BuiltinApprover])
	c.mu.Unlock()
	if len(asked) == 0 || asked[0].Properties["risk"].Type != "string" {
		t.Fatalf("the model was not asked for the workspace's schema: %+v", asked)
	}
	if r, _ := p.Runs.Get(tenant, second.ID); r.Status != runs.Parked {
		t.Fatalf("the call went through: %s", r.Status)
	}
}

// A model that cannot give structured output is refused loudly: the
// approval run fails saying so, and the call waits for a person.
func TestAnApprovalModelWithoutStructuredOutputIsRefused(t *testing.T) {
	p, c, first := judged(t, selfApproving, `{"decision": "allow", "reason": "Fine.", "references": []}`)
	c.mu.Lock()
	c.freeText = map[string]bool{platform.BuiltinApprover: true}
	c.mu.Unlock()
	if _, err := p.SetApprovalMode(admin, first.Thread, threads.ApprovalAuto); err != nil {
		t.Fatal(err)
	}
	second := await(t, p, next(t, p, first).ID, runs.Parked)
	j := judgementOn(t, p, second.ID)
	if j.Decision != "ask" || !strings.Contains(j.Reason, "could not decide") {
		t.Fatalf("judgement = %+v", j)
	}
	decided, err := p.Runs.Get(tenant, j.Run)
	if err != nil || decided.Status != runs.Failed || !strings.Contains(decided.Error, "structured output") {
		t.Fatalf("the approval run = %s %q, %v", decided.Status, decided.Error, err)
	}
	if got := c.requests(platform.BuiltinApprover); len(got) != 0 {
		t.Fatalf("the model was called %q", got)
	}
	if r, _ := p.Runs.Get(tenant, second.ID); r.Status != runs.Parked {
		t.Fatalf("the call went through: %s", r.Status)
	}
}

// readsTheSession is the approval agent reading the session with a tool
// before it answers, as a model may.
func readsTheSession() []saigetypes.Delta {
	return agenttest.ToolCallResponse("call-read", "session_read", map[string]any{})
}

// An answer after a tool call is the draft followed by the structured
// reply, whose object decides.
func TestTheApprovalAgentDecidesAfterCallingATool(t *testing.T) {
	p, c, first := judgedBy(t, selfApproving, [][]saigetypes.Delta{
		readsTheSession(),
		say("The session asked for this."),
		say(`{"decision": "allow", "reason": "Asked for in the session.", "references": ["admin: Check it"]}`),
	})
	if _, err := p.SetApprovalMode(admin, first.Thread, threads.ApprovalAuto); err != nil {
		t.Fatal(err)
	}
	second := next(t, p, first)
	await(t, p, second.ID, runs.Completed)
	if o := answeredFor(t, p, second.ID); o.Action != platform.ActApprove || o.Note != "Allowed by the approval agent: Asked for in the session." {
		t.Fatalf("outcome = %+v", o)
	}
	j := judgementOn(t, p, second.ID)
	decided, err := p.Runs.Get(tenant, j.Run)
	if err != nil || !strings.Contains(decided.Answer, "The session asked for this.") || !strings.Contains(string(decided.Output), `"decision":"allow"`) {
		t.Fatalf("the approval run answered %q and kept %s, %v", decided.Answer, decided.Output, err)
	}
	c.mu.Lock()
	asked := len(c.schemas[platform.BuiltinApprover])
	c.mu.Unlock()
	if asked == 0 {
		t.Fatal("the structured reply was not asked for under the schema")
	}
	// The tool ran, and the model was told what it returned.
	if sent := c.requests(platform.BuiltinApprover); len(sent) < 3 || !strings.Contains(sent[1], "call-read") {
		t.Fatalf("the approval agent's model was sent %d requests: %q", len(sent), sent)
	}
}

// After a tool call, an answer with no object, or one whose object does
// not hold to the schema, leaves the call for a person.
func TestAnUnreadableAnswerAfterAToolCallWaitsForAPerson(t *testing.T) {
	for name, reply := range map[string]string{
		"no object":        "I cannot tell from the session.",
		"another decision": `{"decision": "maybe", "reason": "Unsure.", "references": []}`,
		"no reason":        `{"decision": "allow", "references": []}`,
	} {
		t.Run(name, func(t *testing.T) {
			p, _, first := judgedBy(t, selfApproving, [][]saigetypes.Delta{
				readsTheSession(),
				say("Let me think."),
				say(reply),
			})
			if _, err := p.SetApprovalMode(admin, first.Thread, threads.ApprovalAuto); err != nil {
				t.Fatal(err)
			}
			second := await(t, p, next(t, p, first).ID, runs.Parked)
			j := judgementOn(t, p, second.ID)
			if j.Decision != "ask" {
				t.Fatalf("judgement = %+v", j)
			}
			within(t, "the hand-off was audited", func() bool {
				got := judgements(t, p)
				return len(got) == 1 && got[0].After["verdict"] == "ask"
			})
			if r, _ := p.Runs.Get(tenant, second.ID); r.Status != runs.Parked {
				t.Fatalf("the call went through: %s", r.Status)
			}
		})
	}
}
