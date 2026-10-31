package platform_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/urmzd/saige/agent/agenttest"
	saigetypes "github.com/urmzd/saige/agent/types"

	"github.com/urmzd/humagen/fiber/pkg/event"
	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/inbox"
	"github.com/urmzd/humagen/pkg/policy"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/usage"
)

// billing may grant a run more than its limit: an admin holds billing:write.
var billing = auth.Principal{Tenant: tenant, Subject: "ana", Kind: auth.User, Roles: []auth.Role{auth.Admin}}

// waitsUntil answers its first n calls at once and each later one only once
// ready says so, as a real model's seconds-long call gives the platform time
// to judge the call before it.
type waitsUntil struct {
	saigetypes.Provider
	n     int
	ready func() bool
	mu    sync.Mutex
	calls int
}

func (g *waitsUntil) Stream(ctx context.Context, req saigetypes.Request) (<-chan saigetypes.Delta, error) {
	g.mu.Lock()
	g.calls++
	n := g.calls
	g.mu.Unlock()
	for deadline := time.Now().Add(10 * time.Second); n > g.n && !g.ready() && time.Now().Before(deadline); {
		time.Sleep(5 * time.Millisecond)
	}
	return g.Provider.Stream(ctx, req)
}

// approving runs "lead" over priced calls: two probes that take the
// workspace past its spend limit of 5,000, and a third whose batch the hold
// parks: a hold is met at the next batch of tool calls, one model call after
// the one that reached the limit. The third call waits until the run is
// held. Its policy holds a run at the limit for approval, granting 10,000 at
// a time, and the turn continued after a grant answers for 45 more.
func approving(t *testing.T) *platform.Platform {
	t.Helper()
	var p *platform.Platform
	p = metered(t, platform.Config{}, map[string]agentModel{
		"lead": {tools: probes, model: func(n int) saigetypes.Provider {
			if n > 1 {
				// The turn continued after the hold.
				return script(spends(say("Done."), 10, 1))
			}
			return priced{Provider: &waitsUntil{n: 2, ready: func() bool {
				for _, e := range noticesOf(p, tenant, 0) {
					if e.Kind == usage.Held {
						return true
					}
				}
				return false
			}, Provider: &agenttest.ScriptedProvider{Responses: [][]saigetypes.Delta{
				spends(probe("p1"), 1000, 100), // 4,500
				spends(probe("p2"), 1000, 100), // 9,000: over the limit, so the run is held
				spends(probe("p3"), 1000, 100), // 13,500: the batch the hold parks
			}}}, pricing: card}
		}},
	})
	if _, err := p.Policies.Put(tenant, policy.Policy{
		Scope: policy.Scope{Kind: policy.Tenant}, Period: policy.Day, SpendLimit: 5000, Action: policy.Approve, MaxGrant: 10_000,
	}, "ada", time.Now()); err != nil {
		t.Fatal(err)
	}
	return p
}

// heldOn waits for a run to be held at a limit and returns its question's
// inbox message.
func heldOn(t *testing.T, p *platform.Platform, runID string) inbox.Message {
	t.Helper()
	held := await(t, p, runID, runs.Parked)
	if held.Question == nil || held.Question.Kind != event.QuestionHold || held.Question.Hold == nil || held.Question.Hold.Topic != "budget" {
		t.Fatalf("the run is parked on %+v, want a budget hold", held.Question)
	}
	var m inbox.Message
	within(t, "the budget question is in the inbox", func() bool {
		for _, msg := range p.Inbox.OpenOf(tenant, inbox.KindApproval) {
			if msg.Source.Run == runID {
				m = msg
				return true
			}
		}
		return false
	})
	return m
}

// A run that reaches the spend limit of a policy asking for approval is held
// rather than stopped, on a budget question addressed as an approval and
// offered only to billing:write. A grant is the policy's amount, recorded in
// the usage ledger with who made it and the policy version, and the run
// carries on, judged against the limit plus the grant.
func TestARunHeldAtASpendLimitGoesOnWhenBillingGrantsIt(t *testing.T) {
	p := approving(t)
	r, err := p.Invoke(t.Context(), tag("evt-1", "lead"))
	if err != nil {
		t.Fatal(err)
	}
	m := heldOn(t, p, r.ID)
	q := m.Parts[0].Question
	if m.Parts[0].Type != inbox.PartApproval || q.Kind != "budget" || q.Budget == nil || q.Budget.Limit != "spend" ||
		q.Budget.Max != 5000 || q.Budget.Grant != 10_000 || q.Budget.Version != 1 || q.Default != "deny" {
		t.Fatalf("the budget question is %+v (budget %+v)", q, q.Budget)
	}
	if !strings.Contains(q.Prompt, "Grant this run $0.01 more") {
		t.Errorf("the question asks %q", q.Prompt)
	}
	if got, err := p.InboxGet(operator, m.ID); err != nil || len(got.Parts[0].Actions) != 0 {
		t.Errorf("an operator is offered %v (%v), want nothing: granting is billing's", got.Parts[0].Actions, err)
	}
	if got, err := p.InboxGet(billing, m.ID); err != nil || !slices.Equal(got.Parts[0].Actions, []string{platform.ActApprove, platform.ActDeny}) {
		t.Errorf("billing is offered %v (%v), want approve and deny", got.Parts[0].Actions, err)
	}
	if _, err := p.AnswerQuestion(t.Context(), operator, r.ID, q.ID, platform.QuestionAnswer{Action: platform.ActApprove}); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("an operator's grant: %v, want ErrForbidden", err)
	}

	got, err := p.AnswerQuestion(t.Context(), billing, r.ID, q.ID, platform.QuestionAnswer{Action: platform.ActApprove})
	if err != nil {
		t.Fatal(err)
	}
	if o := got.Parts[0].Outcome; o == nil || o.Action != platform.ActApprove || !strings.Contains(o.Note, "Granted $0.01 more") {
		t.Fatalf("the question settled as %+v", got.Parts[0].Outcome)
	}
	done := await(t, p, r.ID, runs.Completed)
	if done.Answer != "Done." {
		t.Fatalf("the run answered %q", done.Answer)
	}
	var grants []usage.Entry
	for _, e := range noticesOf(p, tenant, 0) {
		if e.Kind == usage.Granted && e.RunID == r.ID {
			grants = append(grants, e)
		}
	}
	if len(grants) != 1 || grants[0].By != "ana" || grants[0].Version != 1 || grants[0].Grant == nil || grants[0].Grant.Spend != 10_000 ||
		grants[0].Scope == nil || grants[0].Scope.Kind != policy.Tenant {
		t.Fatalf("grants = %+v, want one of 10,000 by ana under version 1", grants)
	}
	if g := grantedOf(p, tenant, r.ID, policy.Scope{Kind: policy.Tenant}, policy.PeriodStart(policy.Day, time.Now())); g.Spend != 10_000 {
		t.Errorf("the ledger holds %+v granted to the run", g)
	}
}

// Billing may stop a held run instead: the run ends stopped with their note
// as the cause, and nothing is granted. A grant the policy no longer allows,
// because it was changed to block since, is refused and changes nothing.
func TestAHeldRunIsStoppedWhenBillingDeclinesOrThePolicyNoLongerGrants(t *testing.T) {
	t.Run("declined", func(t *testing.T) {
		p := approving(t)
		r, err := p.Invoke(t.Context(), tag("evt-1", "lead"))
		if err != nil {
			t.Fatal(err)
		}
		q := heldOn(t, p, r.ID).Parts[0].Question
		if _, err := p.AnswerQuestion(t.Context(), billing, r.ID, q.ID, platform.QuestionAnswer{Action: platform.ActDeny, Note: "Over budget this month"}); err != nil {
			t.Fatal(err)
		}
		stopped := await(t, p, r.ID, runs.Stopped)
		if stopped.Cause != "Over budget this month" {
			t.Fatalf("the run was stopped for %q", stopped.Cause)
		}
		for _, e := range noticesOf(p, tenant, 0) {
			if e.Kind == usage.Granted {
				t.Fatalf("a declined hold granted %+v", e)
			}
		}
	})
	t.Run("the policy no longer grants", func(t *testing.T) {
		p := approving(t)
		r, err := p.Invoke(t.Context(), tag("evt-1", "lead"))
		if err != nil {
			t.Fatal(err)
		}
		q := heldOn(t, p, r.ID).Parts[0].Question
		if _, err := p.Policies.Put(tenant, policy.Policy{
			Scope: policy.Scope{Kind: policy.Tenant}, Period: policy.Day, SpendLimit: 5000, Action: policy.Block,
		}, "ada", time.Now()); err != nil {
			t.Fatal(err)
		}
		if _, err := p.AnswerQuestion(t.Context(), billing, r.ID, q.ID, platform.QuestionAnswer{Action: platform.ActApprove}); !errors.Is(err, platform.ErrAction) {
			t.Fatalf("a grant the policy no longer allows: %v, want ErrAction", err)
		}
		if run, _ := p.Runs.Get(tenant, r.ID); run.Status != runs.Parked {
			t.Fatalf("the run is %s, want still held", run.Status)
		}
	})
}

// A hold that cannot be asked about falls back to the stop it replaces: a
// policy that blocks stops the run at its limit even when another policy over
// it would have asked for approval.
func TestABlockingLimitStillStopsARunAnApprovingOneWouldHold(t *testing.T) {
	p := metered(t, platform.Config{}, map[string]agentModel{
		"lead": {tools: probes, model: func(int) saigetypes.Provider {
			return priced{Provider: &heldAfter{ScriptedProvider: &agenttest.ScriptedProvider{Responses: [][]saigetypes.Delta{
				spends(probe("p1"), 1000, 100), spends(probe("p2"), 1000, 100),
			}}, n: 2}, pricing: card}
		}},
	})
	for _, pol := range []policy.Policy{
		{Scope: policy.Scope{Kind: policy.Tenant}, Period: policy.Day, SpendLimit: 5000, Action: policy.Approve, MaxGrant: 10_000},
		{Scope: policy.Scope{Kind: policy.Agent, Name: "lead"}, Period: policy.Day, SpendLimit: 5000, Action: policy.Block},
	} {
		if _, err := p.Policies.Put(tenant, pol, "ada", time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	r, err := p.Invoke(t.Context(), tag("evt-1", "lead"))
	if err != nil {
		t.Fatal(err)
	}
	if stopped := await(t, p, r.ID, runs.Stopped); !strings.Contains(stopped.Cause, "agent lead") {
		t.Fatalf("the run was stopped for %q, want the agent's blocking limit", stopped.Cause)
	}
}
