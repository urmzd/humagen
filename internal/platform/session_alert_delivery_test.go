package platform

import (
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/alerts"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/prefs"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/threads"
	"github.com/urmzd/orchagent/flow/event"
	"github.com/urmzd/orchagent/flow/runtime"
	"github.com/urmzd/orchagent/flow/turn/native"
)

func alertTestPlatform(t *testing.T) (*Platform, auth.Principal, *time.Time) {
	t.Helper()
	now := time.Date(2026, 11, 7, 12, 10, 0, 0, time.UTC)
	pol, err := auth.ParsePolicy("alert-test", `role admin { allow * on * } grant admin to subject "ada"`)
	if err != nil {
		t.Fatal(err)
	}
	p, err := New(Config{Workspace: t.TempDir(), Backends: Backends{State: state.NewMemory()}, Policy: pol, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	if _, err := p.Agents.Create(t.Context(), "acme", wire.AgentSpec{Name: "lead", Model: "offline", Provider: "offline", SystemPrompt: "Help"}, "test"); err != nil {
		t.Fatal(err)
	}
	return p, p.byPolicy("acme", "ada"), &now
}
func alertTestSession(t *testing.T, p *Platform, id string, at time.Time) string {
	t.Helper()
	sid := "s_" + strings.Repeat(id, 32)
	_, err := p.Threads.Create("acme", threads.Thread{ID: sid, Channel: "general", Title: "Session", CreatedBy: "ada", Visibility: threads.Private, Default: "lead", Agents: []string{"lead"}, Session: &threads.SessionIdentity{Agent: "lead", Parent: "console://ada"}}, at)
	if err != nil {
		t.Fatal(err)
	}
	return sid
}
func TestSessionAlertPriorityDeliverySurvivesReadSnoozeAndRestart(t *testing.T) {
	p, who, now := alertTestPlatform(t)
	sid := alertTestSession(t, p, "a", *now)
	// Persisted canonical alerts recover the compatibility transport on poll.
	a, err := p.Alerts.CreateSession("acme", alerts.Alert{Session: sid, Title: "Choose", Priority: alerts.Urgent, Who: []string{"ada"}, At: *now})
	if err != nil {
		t.Fatal(err)
	}
	if rows, err := p.NativeNotifications(who); err != nil || len(rows) != 0 {
		t.Fatalf("zero prefs delivered: %+v %v", rows, err)
	}
	if _, err := p.AckAlert("acme", a.ID, "ada"); err != nil {
		t.Fatal(err)
	}
	// A fresh alert gets its current priority policy; legacy kind preference does
	// not authorize this delivery.
	policy := prefs.DefaultPersonal()
	policy.Notifications["urgent"] = prefs.Immediate
	policy.Notifications["normal"] = prefs.Digest
	policy.Notifications["low"] = prefs.Off
	if _, err := p.Prefs.SetPersonal("acme", "ada", policy); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(time.Minute)
	a, err = p.CreateSessionAlert("acme", alerts.Alert{Session: sid, Title: "Approve now", Priority: alerts.Urgent, Who: []string{"ada"}, At: *now})
	if err != nil {
		t.Fatal(err)
	}
	m, ok := p.Inbox.ByKey("acme", alertKey(a.ID))
	if !ok {
		t.Fatal("missing transport")
	}
	if _, err := p.Inbox.Read("acme", m.ID, "ada", true, *now); err != nil {
		t.Fatal(err)
	}
	rows, err := p.NativeNotifications(who)
	if err != nil || len(rows) != 1 || rows[0].Mode != prefs.Immediate || rows[0].Items[0].Thread != sid {
		t.Fatalf("priority/read/anchor: %+v %v", rows, err)
	}
	again, err := New(p.cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = again.Close() })
	replay, err := again.NativeNotifications(who)
	if err != nil || len(replay) != 1 || replay[0].ID != rows[0].ID {
		t.Fatalf("restart delivery: %+v %v", replay, err)
	}
	if _, err := p.Alerts.ChangeState("acme", a.ID, "ada", alerts.Snoozed, now.Add(time.Hour), *now); err != nil {
		t.Fatal(err)
	}
	if rows, err := again.NativeNotifications(who); err != nil || len(rows) != 0 {
		t.Fatalf("snoozed alert delivered: %+v %v", rows, err)
	}
	*now = now.Add(2 * time.Hour)
	if rows, err := again.NativeNotifications(who); err != nil || len(rows) != 1 {
		t.Fatalf("elapsed snooze failed: %+v %v", rows, err)
	}
	if _, err := p.AckAlert("acme", a.ID, "ada"); err != nil {
		t.Fatal(err)
	}
	if rows, err := again.NativeNotifications(who); err != nil || len(rows) != 0 {
		t.Fatalf("resolved alert delivered: %+v %v", rows, err)
	}
}
func TestSessionAlertPriorityOrdersOnlyVisibleOpenAttention(t *testing.T) {
	p, who, now := alertTestPlatform(t)
	ids := []string{alertTestSession(t, p, "a", now.Add(-3*time.Hour)), alertTestSession(t, p, "b", now.Add(-2*time.Hour)), alertTestSession(t, p, "c", *now)}
	for i, priority := range []alerts.Priority{alerts.Urgent, alerts.Normal, alerts.Low} {
		if _, err := p.Alerts.CreateSession("acme", alerts.Alert{Session: ids[i], Title: "Act", Priority: priority, Who: []string{"ada"}, At: *now}); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := p.ListSessions(t.Context(), who, "", "", "", "")
	if err != nil || len(rows) != 3 {
		t.Fatalf("list: %+v %v", rows, err)
	}
	for i, id := range ids {
		if rows[i].ID != id {
			t.Fatalf("wrong priority order: %+v", rows)
		}
	}
	all, _ := p.Alerts.Alerts("acme")
	for _, a := range all {
		if a.Priority == alerts.Urgent {
			if _, err := p.Alerts.ChangeState("acme", a.ID, "ada", alerts.Snoozed, now.Add(time.Hour), *now); err != nil {
				t.Fatal(err)
			}
		}
	}
	rows, err = p.ListSessions(t.Context(), who, "", "", "", "")
	if err != nil || rows[0].ID != ids[1] || rows[2].ID != ids[0] {
		t.Fatalf("snoozed priority remained: %+v %v", rows, err)
	}
}
func TestSessionAlertToolBoundsSessionAudienceAndStandingPermission(t *testing.T) {
	p, _, now := alertTestPlatform(t)
	sid := alertTestSession(t, p, "a", *now)
	tag := wire.NewEvent(wire.Raise{ID: "alert-tool", Tenant: "acme", Agent: "lead", Actor: event.Actor{ID: "ada", User: "ada"}, At: *now, Text: "Help", Context: map[string]string{wire.MetaSession: sid}})
	if err := p.Runs.Record("r1", tag, 1); err != nil {
		t.Fatal(err)
	}
	req := runtime.Request{Tenant: "acme", RunID: "r1", Definition: native.Revision{Spec: native.Spec{Name: "lead"}}}
	tool := p.sessionAlertCreateTool(req)
	good := map[string]any{"session": sid, "title": "Choose", "priority": "normal", "who": []any{"ada"}, "resolve_when": "A choice is recorded"}
	for _, bad := range []map[string]any{{"session": "s_" + strings.Repeat("b", 32), "title": "No", "priority": "normal"}, {"session": sid, "title": "No", "priority": "normal", "who": []any{"missing"}}, {"session": sid, "title": "No", "priority": "normal", "who": make([]any, 21)}} {
		if _, err := tool.Execute(t.Context(), bad); err == nil {
			t.Fatalf("unconfined alert allowed: %+v", bad)
		}
	}
	if _, err := tool.Execute(t.Context(), good); err != nil {
		t.Fatal(err)
	}
	tag = wire.NewEvent(wire.Raise{ID: "unprivileged", Tenant: "acme", Agent: "lead", Actor: event.Actor{ID: "ben", User: "ben"}, At: *now, Text: "Help", Context: map[string]string{wire.MetaSession: sid}})
	if err := p.Runs.Record("r2", tag, 1); err != nil {
		t.Fatal(err)
	}
	req.RunID = "r2"
	if _, err := p.sessionAlertCreateTool(req).Execute(t.Context(), good); err == nil {
		t.Fatal("zero standing permission created alert")
	}
}
