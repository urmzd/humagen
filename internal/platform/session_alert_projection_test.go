package platform

import (
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/alerts"
	"github.com/urmzd/humagen/pkg/inbox"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/orchagent/flow/event"
)

func TestSessionAlertResolutionRequiresRecordedAnswer(t *testing.T) {
	cfg := Config{Workspace: t.TempDir(), Backends: Backends{State: state.NewMemory()}}
	p, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	at := time.Now().UTC()
	const tenant, sid = "acme", "s_00000000000000000000000000000001"
	tag := wire.NewEvent(wire.Raise{ID: "request", Tenant: tenant, Agent: "lead", At: at, Text: "Plan", Context: map[string]string{wire.MetaSession: sid}})
	if err := p.Runs.Record("r1", tag, 1); err != nil {
		t.Fatal(err)
	}
	p.Runs.Observe(event.Envelope{Tenant: tenant, Run: "r1", Type: event.TypeCompleted, Time: at.Add(time.Second), Data: event.Completed{Text: "Awaiting your choice", Usage: &event.RunUsage{}}})
	var manual []alerts.Alert
	for _, action := range []string{"answer", "approve"} {
		a, err := p.Alerts.CreateSession(tenant, alerts.Alert{Session: sid, RunID: "r1", Title: "Please choose", Priority: alerts.Normal, Action: action, At: at})
		if err != nil {
			t.Fatal(err)
		}
		manual = append(manual, a)
	}
	m, _, err := p.Inbox.RaiseOnce(tenant, "question:r1:q1", inbox.Message{Kind: inbox.KindApproval, To: "ada", Subject: "Approve tool", Source: inbox.Source{Run: "r1", Thread: sid}, Parts: []inbox.Part{{Type: inbox.PartApproval, Question: &inbox.QuestionPart{Run: "r1", ID: "q1"}}}}, at)
	if err != nil {
		t.Fatal(err)
	}
	projected, err := p.Alerts.ProjectSession(tenant, "question:r1:q1", alerts.Alert{Projection: m.ID, Session: sid, RunID: "r1", Kind: alerts.Kind(inbox.KindApproval), Title: "Approve tool", Priority: alerts.Normal, Action: "approve", At: at})
	if err != nil {
		t.Fatal(err)
	}
	// Reload the authoritative question-to-alert association, not process memory.
	first := p
	p, err = New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = first.Close() })
	if err := p.settleSessionAlerts(tenant, sid); err != nil {
		t.Fatal(err)
	}
	for _, a := range manual {
		got, err := p.Alerts.Alert(tenant, a.ID)
		if err != nil || !got.Open() {
			t.Fatalf("manual alert resolved without human action: %+v %v", got, err)
		}
	}
	got, err := p.Alerts.Alert(tenant, projected.ID)
	if err != nil || !got.Open() {
		t.Fatalf("completion without answer resolved alert: %+v %v", got, err)
	}
	if _, _, err := p.Inbox.Resolve(tenant, m.ID, m.Parts[0].ID, inbox.Outcome{Action: ActApprove, By: "ada", At: at.Add(2 * time.Second)}); err != nil {
		t.Fatal(err)
	}
	if err := p.settleSessionAlerts(tenant, sid); err != nil {
		t.Fatal(err)
	}
	got, err = p.Alerts.Alert(tenant, projected.ID)
	if err != nil || got.Open() {
		t.Fatalf("answered approval remains open: %+v %v", got, err)
	}
	for _, action := range []string{OutcomeExpired, "stopped", "dismissed", "failed"} {
		m.Parts[0].Outcome = &inbox.Outcome{Action: action, By: "system", At: at}
		if answeredSessionAlert(m) {
			t.Fatalf("%s treated as an answer", action)
		}
	}
}
