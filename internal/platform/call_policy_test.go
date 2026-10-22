package platform_test

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/fiber/pkg/agentrt/payload"
	"github.com/urmzd/fiber/pkg/agentspec"
	"github.com/urmzd/fiber/pkg/bus"
	"github.com/urmzd/fiber/pkg/envelope"
	"github.com/urmzd/fiber/pkg/registry"
	"github.com/urmzd/fiber/pkg/runtime"
	saigetypes "github.com/urmzd/saige/agent/types"

	"github.com/urmzd/warp/internal/platform"
	"github.com/urmzd/warp/pkg/auth"
	"github.com/urmzd/warp/pkg/call"
	"github.com/urmzd/warp/pkg/callpolicy"
	"github.com/urmzd/warp/pkg/inbox"
	"github.com/urmzd/warp/pkg/runs"
)

func setPolicy(t *testing.T, p *platform.Platform, agent string, pol callpolicy.Policy) callpolicy.Policy {
	t.Helper()
	got, err := p.SetCallPolicy(t.Context(), tenant, agent, "olu", pol)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// A call needs both sides: the caller's spawn allowlist and the callee's
// policy. Each refusal says which side refused. The zero limits refuse every
// call that gets past both, with ErrDepth, so no run is started here.
func TestACallNeedsTheCallerAndTheCallee(t *testing.T) {
	p := start(t, call.Limits{})
	ctx := t.Context()
	lead, err := p.Invoke(ctx, tag("run-lead", "lead"))
	if err != nil {
		t.Fatal(err)
	}
	helper, err := p.Invoke(ctx, tag("run-helper", "helper"))
	if err != nil {
		t.Fatal(err)
	}
	try := func(from runs.Run, to string) error {
		_, err := p.Call(ctx, tenant, from.ID, call.Request{Key: "k", Agent: to, Text: "x", Mode: call.Delegate})
		return err
	}
	passes := func(t *testing.T, err error) {
		t.Helper()
		if !errors.Is(err, call.ErrDepth) {
			t.Fatalf("got %v, want the call to pass both sides and stop at the limits", err)
		}
	}
	refusedByCallee := func(t *testing.T, err error, says string) {
		t.Helper()
		if !errors.Is(err, platform.ErrCallRefused) || !errors.Is(err, platform.ErrForbidden) || !strings.Contains(err.Error(), says) {
			t.Fatalf("got %v, want the callee's refusal saying %q", err, says)
		}
	}

	// No policy is today's behavior.
	passes(t, try(lead, "helper"))
	// The caller's side: helper's spec names nobody.
	if err := try(helper, "lead"); !errors.Is(err, platform.ErrForbidden) || errors.Is(err, platform.ErrCallRefused) || !strings.Contains(err.Error(), "refused by the caller") {
		t.Fatalf("got %v, want the caller's refusal", err)
	}
	// A policy admitting anyone does not let in a caller whose spec does not
	// name the callee.
	setPolicy(t, p, "lead", callpolicy.Policy{Callers: callpolicy.Anyone, Approval: callpolicy.Never})
	if err := try(helper, "lead"); errors.Is(err, call.ErrDepth) || !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("got %v, want a refusal", err)
	}

	setPolicy(t, p, "helper", callpolicy.Policy{Callers: callpolicy.Nobody, Approval: callpolicy.Never})
	refusedByCallee(t, try(lead, "helper"), "accepts calls from no agent")
	setPolicy(t, p, "helper", callpolicy.Policy{Callers: callpolicy.Listed, Allow: []string{"helper"}, Approval: callpolicy.Never})
	refusedByCallee(t, try(lead, "helper"), `does not accept calls from "lead"`)
	setPolicy(t, p, "helper", callpolicy.Policy{Callers: callpolicy.Listed, Allow: []string{"lead"}, Approval: callpolicy.Never})
	passes(t, try(lead, "helper"))

	// Approval: refused until a person approved the call.
	setPolicy(t, p, "helper", callpolicy.Policy{Callers: callpolicy.Anyone, Approval: callpolicy.Always, Approvers: []string{"role:manager", "olu"}, ExpiresSeconds: 600})
	refusedByCallee(t, try(lead, "helper"), "requires a person to approve")
	if _, err := p.CallApproved(ctx, tenant, lead.ID, call.Request{Key: "k", Agent: "helper", Text: "x", Mode: call.Delegate}); !errors.Is(err, call.ErrDepth) {
		t.Fatalf("an approved call got %v", err)
	}
	needed, approvers, self, expires := p.CallApproval(tenant, "lead", "helper")
	if !needed || !slices.Equal(approvers, []string{"role:manager", "olu"}) || self || expires != 10*time.Minute {
		t.Fatalf("callApproval = %v %v %v %v", needed, approvers, self, expires)
	}
	setPolicy(t, p, "helper", callpolicy.Policy{Callers: callpolicy.Anyone, Approval: callpolicy.Unlisted, Trusted: []string{"lead"}})
	passes(t, try(lead, "helper"))
	if needed, _, _, _ := p.CallApproval(tenant, "lead", "helper"); needed {
		t.Fatal("a trusted caller needs approval")
	}
	if needed, _, _, expires := p.CallApproval(tenant, "helper", "helper"); !needed || expires != callpolicy.DefaultExpiry {
		t.Fatalf("an unlisted caller: needed %v, expires %v", needed, expires)
	}
}

func TestCallPolicyIsPerTenantAndValidated(t *testing.T) {
	p := start(t, call.Limits{})
	ctx := t.Context()

	got, err := p.CallPolicy(ctx, tenant, "helper")
	if err != nil || got.Version != 0 || got.Callers != callpolicy.Anyone || got.Approval != callpolicy.Never {
		t.Fatalf("default policy = %+v, %v", got, err)
	}
	stored := setPolicy(t, p, "helper", callpolicy.Policy{Agent: "ignored", Callers: callpolicy.Listed, Allow: []string{" lead "}, Approval: callpolicy.Never})
	if stored.Agent != "helper" || stored.Version != 1 || stored.UpdatedBy != "olu" || !slices.Equal(stored.Allow, []string{"lead"}) {
		t.Fatalf("stored %+v", stored)
	}

	// Another tenant's agent does not exist for it, to read or to write.
	if _, err := p.CallPolicy(ctx, "globex", "helper"); !errors.Is(err, registry.ErrNotFound) {
		t.Fatalf("another tenant read the policy: %v", err)
	}
	if _, err := p.SetCallPolicy(ctx, "globex", "helper", "eve", callpolicy.Policy{Callers: callpolicy.Nobody, Approval: callpolicy.Never}); !errors.Is(err, registry.ErrNotFound) {
		t.Fatalf("another tenant wrote the policy: %v", err)
	}
	if _, err := p.CallPolicy(ctx, tenant, "ghost"); !errors.Is(err, registry.ErrNotFound) {
		t.Fatalf("an unknown agent has a policy: %v", err)
	}
	// An agent of another tenant is not an agent here.
	if _, err := p.Agents.Create(ctx, "globex", agentspec.AgentSpec{Name: "spy", Model: "offline", Provider: "offline", SystemPrompt: "x"}, "test"); err != nil {
		t.Fatal(err)
	}
	for name, pol := range map[string]callpolicy.Policy{
		"unknown callers mode":  {Callers: "some", Approval: callpolicy.Never},
		"unknown approval mode": {Callers: callpolicy.Anyone, Approval: "maybe"},
		"unknown agent":         {Callers: callpolicy.Listed, Allow: []string{"ghost"}, Approval: callpolicy.Never},
		"another tenant's":      {Callers: callpolicy.Listed, Allow: []string{"spy"}, Approval: callpolicy.Never},
		"unknown trusted":       {Callers: callpolicy.Anyone, Approval: callpolicy.Unlisted, Trusted: []string{"ghost"}},
	} {
		if _, err := p.SetCallPolicy(ctx, tenant, "helper", "olu", pol); !errors.Is(err, callpolicy.ErrInvalid) {
			t.Errorf("%s: got %v", name, err)
		}
	}
	if got, _ := p.CallPolicy(ctx, tenant, "helper"); got.Version != 1 {
		t.Fatalf("a refused policy was stored: %+v", got)
	}
}

func TestReachableSaysWhomAnAgentMayCallAndWhy(t *testing.T) {
	p := start(t, call.Limits{})
	ctx := t.Context()
	if _, err := p.Agents.Create(ctx, tenant, agentspec.AgentSpec{Name: "closed", Model: "offline", Provider: "offline", SystemPrompt: "x"}, "test"); err != nil {
		t.Fatal(err)
	}
	setPolicy(t, p, "closed", callpolicy.Policy{Callers: callpolicy.Nobody, Approval: callpolicy.Never})
	setPolicy(t, p, "helper", callpolicy.Policy{Callers: callpolicy.Anyone, Approval: callpolicy.Always})

	got, truncated, err := p.Reachable(ctx, tenant, "lead")
	if err != nil || truncated {
		t.Fatal(err, truncated)
	}
	by := map[string]platform.Reach{}
	for _, r := range got {
		by[r.Agent] = r
	}
	if _, self := by["lead"]; self || len(got) != 2 {
		t.Fatalf("reachable = %+v", got)
	}
	if r := by["helper"]; !r.Admits || !r.InSpawn || r.Approval != "required" || r.Reason == "" {
		t.Fatalf("helper = %+v", r)
	}
	if r := by["closed"]; r.Admits || r.InSpawn || r.Approval != "never" || !strings.Contains(r.Reason, "spawn allowlist") {
		t.Fatalf("closed = %+v", r)
	}
	if _, _, err := p.Reachable(ctx, "globex", "lead"); !errors.Is(err, registry.ErrNotFound) {
		t.Fatalf("another tenant: %v", err)
	}
}

// The call tool offers only the agents both sides agree to. One that needs
// approval is offered: the call waits for a person.
func TestTheCallToolOffersOnlyCalleesThatAdmitTheCaller(t *testing.T) {
	c := &cast{says: map[string][][]saigetypes.Delta{
		"lead":   {say("Nothing to do.")},
		"helper": {say("x")}, "closed": {say("x")}, "gated": {say("x")},
	}, spawn: map[string][]string{"lead": {"helper", "closed", "gated"}}}
	p := stage(t, c, platform.Config{Limits: call.Limits{MaxDepth: 2, MaxFanOut: 4}})
	setPolicy(t, p, "closed", callpolicy.Policy{Callers: callpolicy.Listed, Allow: []string{"helper"}, Approval: callpolicy.Never})
	setPolicy(t, p, "gated", callpolicy.Policy{Callers: callpolicy.Anyone, Approval: callpolicy.Always})

	if got := p.CallTargets(tenant, "", "lead", []string{"helper", "closed", "gated"}); !slices.Equal(got, []string{"helper", "gated"}) {
		t.Fatalf("targets = %v", got)
	}
	first, err := p.Converse(t.Context(), tenant, platform.Turn{ID: "enum", Agent: "lead", Text: "Go"})
	if err != nil {
		t.Fatal(err)
	}
	await(t, p, first.ID, runs.Completed)
	requests := modelFor(t, c, "lead").Requests()
	if len(requests) == 0 {
		t.Fatal("lead's model was never asked")
	}
	found := false
	for _, tool := range requests[0].Tools {
		if tool.Name != platform.ToolCallAgent {
			continue
		}
		found = true
		if got := tool.Parameters.Properties["agent"].Enum; !slices.Equal(got, []string{"gated", "helper"}) {
			t.Fatalf("call_agent offers %v", got)
		}
		if strings.Contains(tool.Description, "closed") {
			t.Fatalf("call_agent names a callee that would refuse: %s", tool.Description)
		}
	}
	if !found {
		t.Fatal("lead was not offered call_agent")
	}
}

// An approval to call another agent is in that agent's queue: the inbox
// message names the callee, and the agent filter finds it.
func TestACallWaitingForApprovalIsInTheCalleesQueue(t *testing.T) {
	c := &cast{continues: true,
		says: map[string][][]saigetypes.Delta{
			"lead":   {tagging("helper", "Check it."), say("Done.")},
			"helper": {say("Checked.")},
		},
		spawn: map[string][]string{"lead": {"helper"}},
		tools: map[string][]string{"lead": {platform.ToolCallAgent}},
		shape: map[string]func(*agentspec.AgentSpec){"lead": func(s *agentspec.AgentSpec) { s.Approvals = []string{platform.ToolCallAgent} }},
	}
	p := stage(t, c, platform.Config{Limits: call.Limits{MaxDepth: 2, MaxFanOut: 4}})
	run := ask(t, p, "api-queue", "lead")
	m := questionFor(t, p, run.ID)
	if m.Kind != inbox.KindApproval || m.Source.Agent != "lead" || m.Source.Target != "helper" {
		t.Fatalf("message = %+v", m.Source)
	}
	queue := func(agent string) []inbox.Message {
		page, _, err := p.InboxPage(olu, inbox.Query{Kind: inbox.KindApproval, Agent: agent, State: inbox.Open})
		if err != nil {
			t.Fatal(err)
		}
		return page
	}
	for _, agent := range []string{"helper", "lead"} {
		if got := queue(agent); len(got) != 1 || got[0].ID != m.ID {
			t.Fatalf("%s's queue = %+v", agent, got)
		}
	}
	if got := queue("someone-else"); len(got) != 0 {
		t.Fatalf("another agent's queue holds %d", len(got))
	}
	// Approving it lets the call through.
	if _, err := p.AnswerQuestion(t.Context(), olu, run.ID, run.Question.ID, platform.QuestionAnswer{Action: platform.ActApprove}); err != nil {
		t.Fatal(err)
	}
	await(t, p, run.ID, runs.Completed)
	if got := queue("helper"); len(got) != 0 {
		t.Fatalf("an answered call is still queued: %+v", got)
	}
}

var mo = auth.Principal{Subject: "mo", Tenant: tenant, Kind: auth.User, Roles: []auth.Role{auth.Manager}}

// A callee whose policy holds every call for a manager: the call parks the
// caller on an approval addressed to managers. An operator cannot answer it,
// a manager can. Approved, the callee runs; denied, it never does and the
// caller is told.
func TestACallTheCalleeHoldsWaitsForItsApprovers(t *testing.T) {
	for _, approve := range []bool{true, false} {
		name := "denied"
		if approve {
			name = "approved"
		}
		t.Run(name, func(t *testing.T) {
			c := &cast{continues: true,
				says: map[string][][]saigetypes.Delta{
					"lead":   {tagging("helper", "Check the build."), say("Done.")},
					"helper": {say("Checked.")},
				},
				spawn: map[string][]string{"lead": {"helper"}},
			}
			p := stage(t, c, platform.Config{Limits: call.Limits{MaxDepth: 2, MaxFanOut: 4}})
			setPolicy(t, p, "helper", callpolicy.Policy{Callers: callpolicy.Anyone, Approval: callpolicy.Always, Approvers: []string{"role:manager"}})

			first, err := p.Converse(t.Context(), tenant, platform.Turn{ID: "api-gov", Agent: "lead", Text: "Go", Actor: envelope.Actor{ID: admin.Subject}})
			if err != nil {
				t.Fatal(err)
			}
			var run runs.Run
			within(t, "the caller parked or finished", func() bool {
				run, _ = p.Runs.Get(tenant, first.ID)
				return run.Status == runs.Parked || run.Status.Terminal()
			})
			if run.Status != runs.Parked {
				t.Fatalf("a call the callee holds did not wait for approval: %+v", run)
			}
			m := questionFor(t, p, run.ID)
			q := m.Parts[0].Question
			if m.Kind != inbox.KindApproval || m.Source.Target != "helper" || !slices.Equal(q.Approvers, []string{"role:manager"}) ||
				!strings.Contains(q.Prompt, "Allow lead to call helper?") || !strings.Contains(q.Prompt, "Check the build.") {
				t.Fatalf("question = %+v, source %+v", q, m.Source)
			}
			if !slices.Equal(m.Audience, []string{"role:manager"}) {
				t.Fatalf("audience = %v", m.Audience)
			}
			if sees(p, olu, m.ID) || !sees(p, mo, m.ID) {
				t.Fatal("the approval is not in exactly the managers' inboxes")
			}
			if _, err := p.AnswerQuestion(t.Context(), olu, run.ID, run.Question.ID, platform.QuestionAnswer{Action: platform.ActApprove}); !errors.Is(err, platform.ErrForbidden) {
				t.Fatalf("an operator approving = %v", err)
			}
			if tree, _ := p.Runs.Tree(tenant, run.ID); len(tree) != 1 {
				t.Fatalf("the callee started before anyone approved: %+v", tree)
			}
			answer := platform.QuestionAnswer{Action: platform.ActDeny, Note: "not this week"}
			if approve {
				answer = platform.QuestionAnswer{Action: platform.ActApprove}
			}
			if _, err := p.AnswerQuestion(t.Context(), mo, run.ID, run.Question.ID, answer); err != nil {
				t.Fatal(err)
			}
			done := await(t, p, run.ID, runs.Completed)
			if approve {
				child := called(t, p, done.ID, "helper", runs.Completed)
				if child.Answer != "Checked." {
					t.Fatalf("helper answered %q", child.Answer)
				}
				return
			}
			if tree, _ := p.Runs.Tree(tenant, run.ID); len(tree) != 1 {
				t.Fatalf("a denied call started the callee: %+v", tree)
			}
			if sent := strings.Join(c.requests("lead"), "\n"); !strings.Contains(sent, "not this week") {
				t.Fatalf("the caller was not told the call was refused: %s", sent)
			}
		})
	}
}

// slowToRecord is a bus that holds the runtime for a while each time it
// announces a question. The runtime records a question only after announcing
// it, so while it is held the question is in the inbox and not yet recorded.
type slowToRecord struct {
	bus.Bus
	hold time.Duration
}

func (s slowToRecord) Publish(ctx context.Context, e envelope.Event) error {
	err := s.Bus.Publish(ctx, e)
	if e.Kind == envelope.KindParked {
		if body, derr := payload.Decode[payload.Lifecycle](e); derr == nil && body.Question != nil {
			time.Sleep(s.hold)
		}
	}
	return err
}

// A call approved after its question is announced and before it is recorded
// goes through. The answer is retried until the question is recorded, and
// the approval is kept for the call while it is retried.
func TestACallApprovedBeforeItsQuestionIsRecordedGoesThrough(t *testing.T) {
	c := &cast{continues: true,
		says: map[string][][]saigetypes.Delta{
			"lead":   {tagging("helper", "Check the build."), say("Done.")},
			"helper": {say("Checked.")},
		},
		spawn: map[string][]string{"lead": {"helper"}},
	}
	p := stage(t, c, platform.Config{
		Limits: call.Limits{MaxDepth: 2, MaxFanOut: 4},
		Bus:    slowToRecord{Bus: bus.NewMemory(), hold: 200 * time.Millisecond},
	})
	setPolicy(t, p, "helper", callpolicy.Policy{Callers: callpolicy.Anyone, Approval: callpolicy.Always, Approvers: []string{"role:manager"}})
	first, err := p.Converse(t.Context(), tenant, platform.Turn{ID: "api-early", Agent: "lead", Text: "Go", Actor: envelope.Actor{ID: admin.Subject}})
	if err != nil {
		t.Fatal(err)
	}
	m := questionFor(t, p, first.ID)
	if _, err := p.AnswerQuestion(t.Context(), mo, first.ID, m.Parts[0].Question.ID, platform.QuestionAnswer{Action: platform.ActApprove}); err != nil {
		t.Fatal(err)
	}
	done := await(t, p, first.ID, runs.Completed)
	if child := called(t, p, done.ID, "helper", runs.Completed); child.Answer != "Checked." {
		t.Fatalf("helper answered %q", child.Answer)
	}
}

// The call tool tells the runtime which calls need approval and whom to ask.
func TestTheCallToolAsksTheCalleesApprovers(t *testing.T) {
	p := start(t, call.Limits{})
	ctx := t.Context()
	if _, err := p.Agents.Get(ctx, tenant, "lead", 0); err != nil {
		t.Fatal(err)
	}
	var req runtime.Request
	req.Tenant, req.RunID = tenant, "r"
	req.Revision.Spec.Name, req.Revision.Spec.Access.Spawn = "lead", []string{"helper"}
	args := map[string]any{"agent": "helper", "messages": []any{map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": "Check\n the  build."}}}}}
	if _, needed := p.CallToolApproval(req, args); needed {
		t.Fatal("a callee with no policy needs approval")
	}
	setPolicy(t, p, "helper", callpolicy.Policy{Callers: callpolicy.Anyone, Approval: callpolicy.Always, SelfApproval: true})
	got, needed := p.CallToolApproval(req, args)
	if !needed || got.Prompt != "Allow lead to call helper? It asks: Check the build." || !slices.Equal(got.Approvers, []string{"role:manager", "role:admin"}) || !got.SelfApproval {
		t.Fatalf("approval = %+v, %v", got, needed)
	}
	if got.ExpiresIn != callpolicy.DefaultExpiry {
		t.Fatalf("waits %v, want the default %v", got.ExpiresIn, callpolicy.DefaultExpiry)
	}
	again, _ := p.CallToolApproval(req, args)
	if !reflect.DeepEqual(again, got) {
		t.Fatal("the same call is described differently")
	}
	// A policy's own wait is what the question waits.
	setPolicy(t, p, "helper", callpolicy.Policy{Callers: callpolicy.Anyone, Approval: callpolicy.Always, ExpiresSeconds: 4 * 3600})
	if got, _ := p.CallToolApproval(req, args); got.ExpiresIn != 4*time.Hour {
		t.Fatalf("waits %v, want 4h", got.ExpiresIn)
	}
	// A caller the callee does not admit is refused when it runs, not asked about.
	setPolicy(t, p, "helper", callpolicy.Policy{Callers: callpolicy.Nobody, Approval: callpolicy.Always})
	if _, needed := p.CallToolApproval(req, args); needed {
		t.Fatal("a refused call was put to a person")
	}
}
