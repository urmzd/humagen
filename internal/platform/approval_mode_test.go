package platform_test

import (
	"errors"
	"testing"
	"time"

	"github.com/urmzd/orchagent/flow/event"
	saigetypes "github.com/urmzd/saige/agent/types"

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

// approveForMe stages lead, which says hello, then calls helper, in a
// conversation whose calls the workspace's policy asks about. shape sets
// anything else on lead's spec.
func approveForMe(t *testing.T, shape func(*wire.AgentSpec)) (*platform.Platform, runs.Run) {
	t.Helper()
	c := &cast{continues: true,
		says: map[string][][]saigetypes.Delta{
			"lead":   {say("Hello."), tagging("helper", "Check it."), say("Done.")},
			"helper": {say("Checked.")},
		},
		spawn: map[string][]string{"lead": {"helper"}},
		tools: map[string][]string{"lead": {platform.ToolCallAgent}},
		shape: map[string]func(*wire.AgentSpec){"lead": shape},
	}
	p := stage(t, c, platform.Config{Limits: call.Limits{MaxDepth: 2, MaxFanOut: 4}})
	if _, err := p.SetActionPolicy(admin, operations.Scope{Kind: "workspace"}, map[operations.Category]operations.Level{operations.CallAgents: operations.Ask}, 0); err != nil {
		t.Fatal(err)
	}
	first, err := p.Converse(t.Context(), tenant, platform.Turn{ID: "api-hello", Agent: "lead", Text: "Hi", Actor: event.Actor{ID: admin.Subject}})
	if err != nil {
		t.Fatal(err)
	}
	return p, await(t, p, first.ID, runs.Completed)
}

func next(t *testing.T, p *platform.Platform, after runs.Run) runs.Run {
	t.Helper()
	r, err := p.Converse(t.Context(), tenant, platform.Turn{ID: "api-check", Agent: "lead", Text: "Check it", After: after.ID, Actor: event.Actor{ID: admin.Subject}})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func autoAudits(t *testing.T, p *platform.Platform) []audit.Record {
	t.Helper()
	rows, err := p.Audit.List(tenant, audit.Query{From: time.Now().Add(-time.Hour), To: time.Now().Add(time.Hour), Limit: 50}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var out []audit.Record
	for _, r := range rows {
		if r.Action == "approval.auto" {
			out = append(out, r)
		}
	}
	return out
}

// Approve for Me answers a call the policy asks about, as the person who
// chose it, and the approval is audited naming them.
func TestApproveForMeAnswersAPolicyAskAndIsAudited(t *testing.T) {
	// The person who asked may approve lead's actions: a spec allows that
	// only beside an action it always asks about, here one lead never takes.
	p, first := approveForMe(t, func(s *wire.AgentSpec) {
		s.Tools = append(s.Tools, platform.ToolFileWrite)
		s.Approvals = []string{platform.ToolFileWrite}
		s.SelfApproval = true
	})
	th, err := p.SetApprovalMode(admin, first.Thread, threads.ApprovalAuto)
	if err != nil || !th.Approval.Auto() || th.Approval.By != admin.Subject {
		t.Fatalf("mode = %+v, %v", th.Approval, err)
	}
	second := next(t, p, first)
	await(t, p, second.ID, runs.Completed)
	var got []audit.Record
	within(t, "the automatic approval was audited", func() bool {
		got = autoAudits(t, p)
		return len(got) == 1
	})
	if got[0].Actor != admin.Subject || got[0].After["enabled_by"] != admin.Subject || got[0].After["tool"] != platform.ToolCallAgent {
		t.Fatalf("audit = %+v", got[0])
	}
	if _, err := p.SetApprovalMode(olu, first.Thread, "sometimes"); err == nil {
		t.Fatal("an unknown mode was kept")
	}
}

// What the agent itself always asks about still goes to a person, whatever
// the conversation's mode.
func TestApproveForMeLeavesWhatTheAgentAlwaysAsksAbout(t *testing.T) {
	p, first := approveForMe(t, func(s *wire.AgentSpec) {
		s.SelfApproval = true
		s.Approvals = []string{platform.ToolCallAgent}
	})
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
	if got := autoAudits(t, p); len(got) != 0 {
		t.Fatalf("audited %+v", got)
	}
}

// Only a person chooses to approve for themselves.
func TestOnlyAPersonChoosesApproveForMe(t *testing.T) {
	p, first := approveForMe(t, nil)
	machine := admin
	machine.Kind = auth.Machine
	if _, err := p.SetApprovalMode(machine, first.Thread, threads.ApprovalAuto); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("a machine set the mode: %v", err)
	}
}
