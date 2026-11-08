package platform_test

import (
	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/alerts"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/saige/flow/event"
	"testing"
	"time"
)

func TestSessionSourceAlertPinsContinuationsAndStaysWithinSourceTree(t *testing.T) {
	p := start(t, call.Limits{})
	field(t, p)
	p.SeePerson(admin)
	root, err := p.StartSession(t.Context(), admin, platform.SessionInput{Project: "Field", Agent: "lead", Text: "Inspect the service"})
	if err != nil {
		t.Fatal(err)
	}
	a, err := p.Alerts.CreateSession(admin.Tenant, alerts.Alert{Session: root.ID, Project: "Field", Title: "Needs a fix", Priority: alerts.Normal, At: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	fix, err := p.StartSession(t.Context(), admin, platform.SessionInput{SourceAlert: a.ID, Agent: "helper", Text: "Fix this issue"})
	if err != nil {
		t.Fatal(err)
	}
	if fix.Parent != root.ID || fix.Project != "Field" || fix.Thread.Session.SourceAlert != a.ID {
		t.Fatalf("wrong fix anchor: %+v", fix)
	}
	fix, err = p.PostSession(t.Context(), admin, fix.ID, platform.MessageInput{Text: "Continue the fix"})
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, msg := range fix.Messages {
		if msg.Run == nil {
			continue
		}
		run := await(t, p, msg.Run.ID, runs.Completed)
		if wire.Meta(run.Tag())[wire.MetaSourceAlert] != a.ID {
			t.Fatalf("missing trusted alert on %s", run.ID)
		}
		count++
	}
	if count != 2 {
		t.Fatalf("fix did not admit both turns: %d", count)
	}
	if _, err = p.StartSession(t.Context(), admin, platform.SessionInput{SourceAlert: a.ID, Project: "Elsewhere", Agent: "helper", Text: "Move the fix"}); err == nil {
		t.Fatal("source project changed")
	}
	forged, err := p.Invoke(t.Context(), wire.NewEvent(wire.Raise{ID: "forged-source-alert", Tenant: admin.Tenant, Origin: runs.OriginAPI, Agent: "helper", Text: "An unrelated request", Actor: event.Actor{ID: admin.Subject, User: admin.Subject}, Context: map[string]string{wire.MetaSourceAlert: a.ID}, At: time.Now()}))
	if err != nil {
		t.Fatal(err)
	}
	if wire.Meta(forged.Tag())[wire.MetaSourceAlert] != "" {
		t.Fatal("ingress source alert pin survived")
	}

	foreign := admin
	foreign.Tenant = "elsewhere"
	if _, err = p.StartSession(t.Context(), foreign, platform.SessionInput{SourceAlert: a.ID, Agent: "helper", Text: "Read source"}); err == nil {
		t.Fatal("foreign alert accepted")
	}
}
