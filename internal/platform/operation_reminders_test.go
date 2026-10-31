package platform

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/audit"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/inbox"
	"github.com/urmzd/humagen/pkg/operations"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

func TestNativeReminderTimersSurviveRestartWithoutApprovingOrDuplicatingWork(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		base := time.Now().UTC()
		at := base
		p, err := New(Config{Workspace: t.TempDir(), Backends: Backends{State: st}, Now: func() time.Time { return at }})
		if err != nil {
			t.Fatal(err)
		}
		assignment, err := p.Operations.SetAssignment("t", operations.Assignment{ID: "blocked", Agent: "worker", Owner: "owner", Goal: "Review", DoneWhen: "Report", State: operations.Requested}, 0, "owner", base)
		if err != nil {
			t.Fatal(err)
		}
		if err := p.Operations.BlockAssignment("t", assignment.ID, "Needs source repair", base); err != nil {
			t.Fatal(err)
		}
		assignment, _, _ = p.Operations.Assignment("t", assignment.ID)
		p.operationEscalation("t", "assignment", assignment.ID, "", "", "", "worker", "owner", assignment.Blocker)
		at = base.Add(2 * time.Hour)
		p.reconcileOperationReminders(t.Context())
		notices := p.Inbox.OpenOf("t", inbox.KindOperation)
		if len(notices) != 1 || !notices[0].UpdatedAt.Equal(at) {
			t.Fatalf("two-hour blocked reminder=%+v", notices)
		}
		if err := p.Close(); err != nil {
			t.Fatal(err)
		}
		at = base.Add(4 * time.Hour)
		restarted, err := New(Config{Workspace: t.TempDir(), Backends: Backends{State: st}, Now: func() time.Time { return at }})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = restarted.Close() }()
		restarted.reconcileOperationReminders(t.Context())
		notices = restarted.Inbox.OpenOf("t", inbox.KindOperation)
		if len(notices) != 1 || !notices[0].UpdatedAt.Equal(base.Add(2*time.Hour)) {
			t.Fatal("restart repeated the same blocked-work reminder")
		}
		changes := len(restarted.Inbox.History("t", notices[0].ID))
		at = base.Add(5 * time.Hour)
		restarted.reconcileOperationReminders(t.Context())
		if len(restarted.Inbox.History("t", notices[0].ID)) != changes {
			t.Fatal("restart repeated blocked-work reminder")
		}

	})
}

func TestBlockedHandoffKeepsRequesterAndManagerAudienceAcrossRestart(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		base := time.Now().UTC()
		p, err := New(Config{Workspace: t.TempDir(), Backends: Backends{State: st}})
		if err != nil {
			t.Fatal(err)
		}
		h, err := p.Operations.SetHandoff("t", operations.Handoff{ID: "contract", From: "lead", To: "worker", Requester: "requester", Owner: "manager", Goal: "Review", DoneWhen: "Report", State: operations.Blocked, Blocker: "Needs a decision", Chain: []string{"lead", "worker"}, DueAt: base.Add(24 * time.Hour)}, 0, base)
		if err != nil {
			t.Fatal(err)
		}
		if err := p.Close(); err != nil {
			t.Fatal(err)
		}
		restarted, err := New(Config{Workspace: t.TempDir(), Backends: Backends{State: st}})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = restarted.Close() }()
		restarted.operationEscalation("t", "handoff", h.ID, "", "", "", "lead", h.Owner, h.Blocker)
		messages := restarted.Inbox.OpenOf("t", inbox.KindOperation)
		if len(messages) != 1 || !slices.Contains(messages[0].Audience, "requester") || !slices.Contains(messages[0].Audience, "manager") {
			t.Fatalf("accountable participants lost on replay: %+v", messages)
		}
		kept, _, _ := restarted.Operations.Handoff("t", h.ID)
		wire, err := json.Marshal(kept)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(wire), "requester") {
			t.Fatal("internal requester subject leaked into the contract DTO")
		}
	})
}

func TestReminderControlAuditSurvivesRestartAndRecordsRefusedVersion(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		at := time.Now().UTC()
		p, err := New(Config{Workspace: t.TempDir(), Backends: Backends{State: st}, Now: func() time.Time { return at }})
		if err != nil {
			t.Fatal(err)
		}
		who := auth.Principal{Tenant: "t", Subject: "owner"}
		seconds := 300
		control := operations.Control{ReminderAfterSeconds: &seconds, StopRules: "DO-NOT-COPY-PRIVATE-INSTRUCTIONS"}
		if _, err = p.SetAgentControl(who, "", control, 0); err != nil {
			t.Fatal(err)
		}
		seconds = 600
		if _, err = p.SetAgentControl(who, "", control, 0); err == nil {
			t.Fatal("stale timer edit succeeded")
		}
		if err = p.Close(); err != nil {
			t.Fatal(err)
		}
		restarted, err := New(Config{Workspace: t.TempDir(), Backends: Backends{State: st}, Now: func() time.Time { return at }})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = restarted.Close() }()
		kept, _ := restarted.AgentControl("t", "")
		if kept.ReminderAfterSeconds == nil || *kept.ReminderAfterSeconds != 300 {
			t.Fatal("refused timer edit changed saved control")
		}
		rows, err := restarted.Audit.List("t", audit.Query{From: at.Add(-time.Minute), To: at.Add(time.Minute), Limit: 10}, func(audit.Record) bool { return true })
		if err != nil || len(rows) != 2 {
			t.Fatalf("timer audit=%+v, %v", rows, err)
		}
		successes, failures := 0, 0
		for _, row := range rows {
			if row.Actor != "owner" || row.Action != "agent.control" {
				t.Fatal("timer provenance lost")
			}
			if row.Outcome == audit.Succeeded && row.After["reminder_after_seconds"] == "300" {
				successes++
			}
			if row.Outcome == audit.Failed && row.After["reminder_after_seconds"] == "600" {
				failures++
			}
			wire, _ := json.Marshal(row)
			if strings.Contains(string(wire), "DO-NOT-COPY") {
				t.Fatal("audit copied private stop instructions")
			}
		}
		if successes != 1 || failures != 1 {
			t.Fatalf("timer outcomes successes%d failures%d", successes, failures)
		}
	})
}
