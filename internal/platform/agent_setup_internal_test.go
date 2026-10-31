package platform

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/urmzd/humagen/fiber/pkg/event"
	"github.com/urmzd/humagen/fiber/pkg/runtime"
	"github.com/urmzd/humagen/fiber/pkg/turn/native"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/operations"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
	saigetypes "github.com/urmzd/saige/agent/types"
)

func TestSetupCapabilityUsesAuthoritativeAdmissionBeforeRunProjection(t *testing.T) {
	p, err := New(Config{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close() }()
	rev, err := p.Agents.Create(t.Context(), "t", wire.AgentSpec{Name: "draft", Provider: "openai", Model: "gpt-4o-mini", SystemPrompt: "Test the exact capability"}, "ada")
	if err != nil {
		t.Fatal(err)
	}
	record := AgentSetup{Name: "draft", Owner: "ada", Phase: "configured", Version: 1, Revision: rev.Rev, RevisionHash: rev.Hash, TestTag: "reserved-test"}
	if err := p.setups.put("t", record); err != nil {
		t.Fatal(err)
	}
	req := runtime.Request{Tenant: "t", RunID: runtime.RunID("t", record.TestTag), Meta: map[string]string{operations.MetaForceAsk: "true"}, Definition: native.Revision{Spec: native.Spec{Name: "draft"}, Version: wire.VersionOf(rev.Rev), Digest: rev.Hash}}
	gate := setupGate{p: p}
	if gate.AllowRequest(req) {
		t.Fatal("missing actor context admitted a draft test")
	}
	admission := admitted{tenant: "t", actor: event.Actor{ID: "ada", User: "ada"}, operationMeta: map[string]string{operations.MetaForceAsk: "true"}}
	p.admitting.Store(req.RunID, admission)
	if !gate.AllowRequest(req) {
		t.Fatal("immediate executor could not join its exact persisted test capability before Runs.Record")
	}
	if err := p.operationAdmissionRequest(req); err != nil {
		t.Fatalf("central admission=%v", err)
	}
	wrong := req
	wrong.Definition.Version = wire.VersionOf(rev.Rev + 1)
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

func amendmentPlan(name string) AgentSetupPlan {
	return AgentSetupPlan{Spec: wire.AgentSpec{Name: name, Provider: "openai", Model: "gpt-4o-mini", SystemPrompt: "Original reviewed configuration."}, Project: "~", Rules: map[operations.Category]operations.Level{operations.Other: operations.Ask}}
}
func amendmentOwner() auth.Principal {
	return auth.Principal{Tenant: "t", Subject: "owner", Kind: auth.User, Roles: []auth.Role{auth.Admin}}
}

type amendmentRegistry struct {
	wire.AgentStore
	revised func()
}

func (s amendmentRegistry) Revise(ctx context.Context, tenant string, spec wire.AgentSpec, expected int, by string) (wire.Revision, error) {
	rev, err := s.AgentStore.Revise(ctx, tenant, spec, expected, by)
	if err == nil && s.revised != nil {
		s.revised()
	}
	return rev, err
}

func TestSetupAmendmentInvalidatesOldTestAndRecoversPartialRegistryWrite(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		fault := statetest.NewFaulty(st)
		p, err := New(Config{Workspace: t.TempDir(), Backends: Backends{State: fault}})
		if err != nil {
			t.Fatal(err)
		}
		who := amendmentOwner()
		saved, err := p.CreateAgentSetup(t.Context(), who, amendmentPlan("correctable"))
		if err != nil {
			t.Fatal(err)
		}
		old := saved
		old.TestTag = "old-completed-test"
		old.TestRun = runtime.RunID("t", old.TestTag)
		tag := wire.NewEvent(wire.Raise{ID: old.TestTag, Tenant: "t", Agent: old.Name, Origin: runs.OriginAPI, Text: "test", Actor: event.Actor{ID: who.Subject, User: who.Subject}, At: time.Now()})
		if err := p.Runs.Record(old.TestRun, tag, old.Revision); err != nil {
			t.Fatal(err)
		}
		p.Runs.End(old.TestRun, runs.Completed, "done", "", time.Now())
		old.Version++
		if err := p.setups.put("t", old); err != nil {
			t.Fatal(err)
		}
		desired := old.Plan
		desired.Spec.Model = "gpt-4o"
		desired.Spec.SystemPrompt = "Corrected reviewed configuration."
		fault.Fail(statetest.ErrInjected)
		if _, err := p.AmendAgentSetup(t.Context(), who, old.Name, desired, old.Version); !errors.Is(err, statetest.ErrInjected) {
			t.Fatalf("intent write failure=%v", err)
		}
		fault.Fail(nil)
		unchanged, _, _ := p.setups.get("t", old.Name)
		before, _ := p.Agents.Get(t.Context(), "t", old.Name, 0)
		if unchanged.TestRun != old.TestRun || before.Hash != old.RevisionHash {
			t.Fatal("failed intent changed the prior setup")
		}
		underlying := p.Agents
		p.Agents = amendmentRegistry{AgentStore: underlying, revised: func() {
			if p.agentReady("t", old.Name) {
				t.Fatal("amendment exposed an active agent window")
			}
			fault.Fail(statetest.ErrInjected)
		}}
		if _, err := p.AmendAgentSetup(t.Context(), who, old.Name, desired, old.Version); !errors.Is(err, statetest.ErrInjected) {
			t.Fatalf("post-registry write failure=%v", err)
		}
		fault.Fail(nil)
		pending, _, _ := p.setups.get("t", old.Name)
		if pending.Phase != "amending" || pending.TestRun != "" || pending.TestTag != "" {
			t.Fatalf("durable amendment intent=%+v", pending)
		}
		if (setupGate{p: p}).AllowTag(tag) {
			t.Fatal("old test capability survived amendment")
		}
		if err := p.Close(); err != nil {
			t.Fatal(err)
		}
		restarted, err := New(Config{Workspace: t.TempDir(), Backends: Backends{State: st, Agents: underlying}})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = restarted.Close() }()
		resumed, err := restarted.ResumeAgentSetup(t.Context(), who, old.Name)
		if err != nil || resumed.Phase != "configured" || resumed.Revision != old.Revision+1 || resumed.TestRun != "" {
			t.Fatalf("amendment replay=%+v,%v", resumed, err)
		}
		current, _ := restarted.Agents.Get(t.Context(), "t", old.Name, 0)
		if current.Spec.Model != "gpt-4o" || current.Hash != resumed.RevisionHash {
			t.Fatal("restart did not adopt the exact corrected revision")
		}
		if _, err := restarted.ActivateAgentSetup(t.Context(), who, old.Name); err == nil {
			t.Fatal("amended setup activated using the invalidated old test")
		}
	})
}

func TestSetupAmendmentChecksOwnerVersionInFlightAndExactHome(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		p, err := New(Config{Workspace: t.TempDir(), Backends: Backends{State: open(t)}})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = p.Close() }()
		who := amendmentOwner()
		saved, err := p.CreateAgentSetup(t.Context(), who, amendmentPlan("reviewed"))
		if err != nil {
			t.Fatal(err)
		}
		plan := saved.Plan
		plan.Spec.Model = "gpt-4o"
		plan.Spec.Tools = []string{ToolFileWrite}
		plan.Spec.Approvals = []string{ToolFileWrite}
		plan.Spec.SelfApproval = true
		stranger := who
		stranger.Subject = "stranger"
		if _, err := p.AmendAgentSetup(t.Context(), stranger, saved.Name, plan, saved.Version); !errors.Is(err, operations.ErrNotFound) {
			t.Fatalf("nonowner amendment=%v", err)
		}
		if _, err := p.AmendAgentSetup(t.Context(), who, saved.Name, plan, saved.Version+1); !errors.Is(err, operations.ErrConflict) {
			t.Fatalf("stale amendment=%v", err)
		}
		unknown := plan
		unknown.Spec.Sources = []wire.Source{{Name: "not-readable", URI: "knowledge://not-readable"}}
		if _, err := p.AmendAgentSetup(t.Context(), who, saved.Name, unknown, saved.Version); err == nil {
			t.Fatal("amendment granted an unreadable source")
		}
		live, _, _ := p.setups.get("t", saved.Name)
		live.TestTag = "in-flight-test"
		live.TestRun = runtime.RunID("t", live.TestTag)
		if err := p.Runs.Record(live.TestRun, wire.NewEvent(wire.Raise{ID: live.TestTag, Tenant: "t", Agent: live.Name, Origin: runs.OriginAPI, Text: "test", At: time.Now()}), live.Revision); err != nil {
			t.Fatal(err)
		}
		live.Version++
		if err := p.setups.put("t", live); err != nil {
			t.Fatal(err)
		}
		if _, err := p.AmendAgentSetup(t.Context(), who, saved.Name, plan, saved.Version); !errors.Is(err, operations.ErrConflict) {
			t.Fatalf("in-flight amendment=%v", err)
		}
		p.Runs.End(live.TestRun, runs.Stopped, "", "stopped", time.Now())
		var wg sync.WaitGroup
		wg.Add(3)
		var amended AgentSetup
		var amendErr, reviseErr, homeErr error
		go func() {
			defer wg.Done()
			amended, amendErr = p.AmendAgentSetup(t.Context(), who, saved.Name, plan, live.Version)
		}()
		go func() { defer wg.Done(); _, reviseErr = p.ReviseBy(t.Context(), who, plan.Spec, saved.Revision) }()
		go func() { defer wg.Done(); homeErr = p.SetAgentHome(t.Context(), "t", saved.Name, "~", WriterOf(who)) }()
		wg.Wait()
		if amendErr != nil || amended.Phase != "configured" {
			t.Fatalf("amendment=%+v,%v", amended, amendErr)
		}
		if !errors.Is(reviseErr, operations.ErrConflict) || !errors.Is(homeErr, operations.ErrConflict) {
			t.Fatalf("normal revision/home bypassed draft lock: %v,%v", reviseErr, homeErr)
		}
		if !amended.Plan.Spec.SelfApproval || amended.TestRun != "" {
			t.Fatal("reviewed native self-approval was lost or old test reused")
		}
		if _, err := p.Operations.SetControl("t", "", operations.Control{Paused: true}, 0, "owner", time.Now()); err != nil {
			t.Fatal(err)
		}
		pausedTest, err := p.TestAgentSetup(t.Context(), who, saved.Name, "test")
		if err == nil && pausedTest.Problem == "" {
			t.Fatal("amended test did not report workspace pause")
		}
		if _, err := p.Runs.Get("t", pausedTest.TestRun); err == nil {
			t.Fatal("amended test executed through workspace pause")
		}
	})
}

func TestReviewedSetupRejectsOfflinePolicyAndFallback(t *testing.T) {
	p, err := New(Config{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close() }()
	who := amendmentOwner()
	policy := p.Settings("t")
	policy.ModelPolicies = []ModelPolicy{{Name: "offline-fallback", Models: []wire.ModelRef{{Provider: "openai", Model: "gpt-4o-mini"}, {Provider: "offline", Model: "offline"}}}}
	if err := p.SetSettings(t.Context(), "t", policy); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		plan := amendmentPlan(fmt.Sprintf("offline-check-%d", i))
		if i == 0 {
			plan.Spec.Provider = PolicyProvider
			plan.Spec.Model = "offline-fallback"
		} else {
			plan.Spec.Fallbacks = []wire.ModelRef{{Provider: "offline", Model: "offline"}}
		}
		if _, err := p.CreateAgentSetup(t.Context(), who, plan); err == nil {
			t.Fatal("offline fallback was accepted as a reviewed real-model setup")
		}
	}
}

func TestReviewedSetupCannotActivateAnOfflineNoticeWithoutAModelCall(t *testing.T) {
	p, err := New(Config{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close() }()
	who := amendmentOwner()
	saved, err := p.CreateAgentSetup(t.Context(), who, amendmentPlan("notice-only"))
	if err != nil {
		t.Fatal(err)
	}
	record, _, _ := p.setups.get("t", saved.Name)
	record.TestTag = "notice"
	record.Version++
	record.TestRun = runtime.RunID("t", record.TestTag)
	if err := p.Runs.Record(record.TestRun, wire.NewEvent(wire.Raise{ID: record.TestTag, Tenant: "t", Agent: record.Name, Origin: runs.OriginAPI, Text: "test", At: time.Now()}), record.Revision); err != nil {
		t.Fatal(err)
	}
	p.Runs.End(record.TestRun, runs.Completed, "Offline notice", "", time.Now())
	if err := p.setups.put("t", record); err != nil {
		t.Fatal(err)
	}
	if _, err := p.ActivateAgentSetup(t.Context(), who, record.Name); err == nil {
		t.Fatal("a completed notice without a real model call activated the agent")
	}
}

type setupStrictApprovalProbe struct{ *saigetypes.ToolFunc }

func (*setupStrictApprovalProbe) Approval(saigetypes.ToolCallPart) (native.ApprovalRequest, bool) {
	return native.ApprovalRequest{Prompt: "Stricter tool policy", Approvers: []string{"strict-reviewer"}, SelfApproval: false}, true
}
func TestSetupAskApprovalPreservesExplicitAudienceAndUnderlyingRestrictions(t *testing.T) {
	p, err := New(Config{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close() }()
	req := runtime.Request{Tenant: "t", Definition: native.Revision{Spec: native.Spec{Name: "a", Approvers: []string{"designated-reviewer"}, SelfApproval: true}}}
	if _, err := p.Operations.SetPolicy("t", operations.Policy{Scope: operations.Scope{Kind: "workspace"}, Rules: map[operations.Category]operations.Level{operations.Other: operations.Ask}}, 0, "owner", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Operations.SetControl("t", "a", operations.Control{Manager: "outside-manager"}, 0, "owner", time.Now()); err != nil {
		t.Fatal(err)
	}
	calls := 0
	base := &saigetypes.ToolFunc{Def: saigetypes.ToolDef{Name: "action"}, Fn: func(context.Context, map[string]any) (string, error) { calls++; return "ok", nil }}
	tool := p.operationTool(req, base, operations.Other)
	approval, needed := tool.(native.Approving).Approval(saigetypes.ToolCallPart{Name: "action"})
	if !needed || !approval.SelfApproval || !slices.Equal(approval.Approvers, []string{"designated-reviewer"}) {
		t.Fatalf("declared approval audience widened: %+v", approval)
	}
	strict := p.operationTool(req, &setupStrictApprovalProbe{ToolFunc: base}, operations.Other)
	approval, needed = strict.(native.Approving).Approval(saigetypes.ToolCallPart{Name: "action"})
	if !needed || approval.SelfApproval || !slices.Equal(approval.Approvers, []string{"strict-reviewer"}) {
		t.Fatalf("underlying tool approval weakened: %+v", approval)
	}
	if _, err := p.Operations.SetPolicy("t", operations.Policy{Scope: operations.Scope{Kind: "workspace"}, Rules: map[operations.Category]operations.Level{operations.Other: operations.Never}}, 1, "owner", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := tool.Execute(t.Context(), nil); !errors.Is(err, operations.ErrDenied) || calls != 0 {
		t.Fatalf("self approval bypassed Never: %v, %d", err, calls)
	}
	if _, err := p.Operations.SetControl("t", "", operations.Control{Paused: true}, 0, "owner", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := tool.Execute(t.Context(), nil); !errors.Is(err, operations.ErrPaused) || calls != 0 {
		t.Fatalf("self approval bypassed workspace pause: %v, %d", err, calls)
	}
}
