package platform

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/alerts"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
	"github.com/urmzd/orchagent/flow/event"
	"github.com/urmzd/orchagent/flow/runtime"
	"github.com/urmzd/orchagent/flow/session"
)

func TestSessionCallChildRetainsTrustedSourceAlert(t *testing.T) {
	p, err := New(Config{Workspace: t.TempDir(), Limits: call.Limits{MaxDepth: 3, MaxFanOut: 4}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	ctx := t.Context()
	who := auth.Principal{Tenant: "source-alert-child", Subject: "owner", Kind: auth.User, Roles: []auth.Role{auth.Admin}}
	p.SeePerson(who)
	for _, spec := range []wire.AgentSpec{
		{Name: "lead", Provider: "offline", Model: "offline", SystemPrompt: "Coordinate", Access: wire.Access{Spawn: []string{"helper"}}},
		{Name: "helper", Provider: "offline", Model: "offline", SystemPrompt: "Help"},
	} {
		if _, err = p.Agents.Create(ctx, who.Tenant, spec, "test"); err != nil {
			t.Fatal(err)
		}
	}
	root, err := p.StartSession(ctx, who, SessionInput{Agent: "lead", Text: "Inspect the service"})
	if err != nil {
		t.Fatal(err)
	}
	alert, err := p.Alerts.CreateSession(who.Tenant, alerts.Alert{Session: root.ID, Title: "Needs a fix", Priority: alerts.Normal, At: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	fix, err := p.StartSession(ctx, who, SessionInput{SourceAlert: alert.ID, Agent: "lead", Text: "Fix the issue"})
	if err != nil {
		t.Fatal(err)
	}
	var runID string
	for _, m := range fix.Messages {
		if m.Run != nil {
			runID = m.Run.ID
		}
	}
	parent, err := p.Runs.Get(who.Tenant, runID)
	if err != nil {
		t.Fatal(err)
	}
	record, err := p.Sessions.Get(ctx, who.Tenant, fix.ID)
	if err != nil {
		t.Fatal(err)
	}
	child, err := p.Sessions.Open(ctx, who.Tenant, session.Open{Parent: fix.ID, Agent: "helper", Label: "repair"})
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := p.startSessionChild(ctx, runtime.Input{Trigger: parent.Tag(), Run: parent.ID, Session: &record}, runtime.SessionCall{ID: "repair-call", Agent: "helper", Text: "Repair the failing path", Session: child})
	if err != nil {
		t.Fatal(err)
	}
	th, err := p.Threads.Get(who.Tenant, child.ID)
	if err != nil || th.Session == nil || th.Session.SourceAlert != alert.ID || th.Session.Parent != fix.ID {
		t.Fatalf("child lost trusted source alert: %+v %v", th, err)
	}
	run, err := p.Runs.Get(who.Tenant, accepted)
	if err != nil || wire.Meta(run.Tag())[wire.MetaSourceAlert] != alert.ID {
		t.Fatalf("child run lost trusted source alert: %+v %v", run, err)
	}
	// Child-supplied actor metadata cannot change the parent person, or
	// authorize a nonexistent, foreign, or unrelated parent record.
	forged := wire.NewEvent(wire.Raise{ID: "forged-person", Tenant: who.Tenant, Agent: "helper", Origin: call.Origin, Text: "Repair", Actor: event.Actor{ID: "lead", User: "stranger", Bot: true}})
	bound := func(parentID string) context.Context {
		return context.WithValue(ctx, sessionCallKey{}, admittedSessionCall{parent: parentID, call: runtime.SessionCall{Session: child}})
	}
	if err := p.pinSessionSourceAlert(bound(parent.ID), &forged); err != nil || wire.Meta(forged)[wire.MetaSourceAlert] != alert.ID {
		t.Fatalf("forged child user replaced authoritative parent person: %v", err)
	}
	foreign := wire.NewEvent(wire.Raise{ID: "foreign-parent", Tenant: "another-tenant", Agent: "lead", Origin: runs.OriginAPI, Actor: ActorOf(who)})
	foreignID := runtime.RunID(foreign.Tenant, foreign.ID)
	if err := p.Runs.Record(foreignID, foreign, 1); err != nil {
		t.Fatal(err)
	}
	for _, parentID := range []string{"missing-parent", foreignID, run.ID} {
		tag := wire.NewEvent(wire.Raise{ID: "forged-parent", Tenant: who.Tenant, Agent: "helper", Origin: call.Origin, Text: "Repair", Actor: event.Actor{ID: "lead", User: who.Subject, Bot: true}})
		if err := p.pinSessionSourceAlert(bound(parentID), &tag); !errors.Is(err, threads.ErrNotFound) {
			t.Fatalf("forged actor authorized parent %q: %v", parentID, err)
		}
	}
}
