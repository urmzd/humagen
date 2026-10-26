package platform

import (
	"github.com/urmzd/fiber/pkg/agentrt"
	"github.com/urmzd/fiber/pkg/envelope"
	"github.com/urmzd/fiber/pkg/runtime"
	"github.com/urmzd/humagen/pkg/operations"
	"testing"
)

func TestSetupCapabilityUsesAuthoritativeAdmissionBeforeRunProjection(t *testing.T) {
	p, err := New(Config{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close() }()
	record := AgentSetup{Name: "draft", Owner: "ada", Phase: "configured", Version: 1, Revision: 1, RevisionHash: "pinned", TestTag: "reserved-test"}
	if err := p.setups.put("t", record); err != nil {
		t.Fatal(err)
	}
	req := runtime.Request{Tenant: "t", RunID: runtime.RunID("t", record.TestTag), Meta: map[string]string{operations.MetaForceAsk: "true"}, Revision: agentrt.Revision{Spec: agentrt.Spec{Name: "draft"}, Rev: 1, Hash: "pinned"}}
	gate := setupGate{p: p}
	if gate.AllowRequest(req) {
		t.Fatal("missing actor context admitted a draft test")
	}
	admission := admitted{tenant: "t", actor: envelope.Actor{ID: "ada", User: "ada"}, operationMeta: map[string]string{operations.MetaForceAsk: "true"}}
	p.admitting.Store(req.RunID, admission)
	if !gate.AllowRequest(req) {
		t.Fatal("immediate executor could not join its exact persisted test capability before Runs.Record")
	}
	if err := p.operationAdmissionRequest(req); err != nil {
		t.Fatalf("central admission=%v", err)
	}
	wrong := req
	wrong.Revision.Hash = "different"
	if gate.AllowRequest(wrong) {
		t.Fatal("wrong revision reused test capability")
	}
	admission.actor.User = "ben"
	p.admitting.Store(req.RunID, admission)
	if gate.AllowRequest(req) {
		t.Fatal("different actor reused test capability")
	}
	admission.actor.User = "ada"
	admission.actor.Bot = true
	p.admitting.Store(req.RunID, admission)
	if gate.AllowRequest(req) {
		t.Fatal("automation reused a human test capability")
	}
	admission.actor.Bot = false
	admission.tenant = "other"
	p.admitting.Store(req.RunID, admission)
	if gate.AllowRequest(req) {
		t.Fatal("different tenant reused test capability")
	}
	admission.tenant = "t"
	p.admitting.Store(req.RunID, admission)
	if _, err := p.Operations.SetControl("t", "", operations.Control{Paused: true}, 0, "manager", p.now()); err != nil {
		t.Fatal(err)
	}
	if err := p.operationAdmissionRequest(req); err == nil {
		t.Fatal("test capability bypassed workspace pause")
	}
}
