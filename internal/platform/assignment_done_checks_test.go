package platform_test

import (
	"strings"
	"testing"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/operations"
	"github.com/urmzd/humagen/pkg/runs"
	saigetypes "github.com/urmzd/saige/agent/types"
)

func doneChecks() []operations.Check {
	return []operations.Check{{Kind: operations.Contains, Value: "DONE:"}, {Kind: operations.NotContains, Value: "TODO"}}
}

func TestAnAssignmentIsContinuedUntilItsAnswerPassesItsChecks(t *testing.T) {
	eachBackend(t, func(t *testing.T, b platform.Backends, _ func() platform.Backends) {
		c := &cast{continues: true, says: map[string][][]saigetypes.Delta{"worker": {say("A first draft. TODO: the summary."), say("DONE: but TODO remains."), say("DONE: the report is filed.")}}}
		p := stage(t, c, platform.Config{Backends: b})
		a, err := p.SetAssignment(t.Context(), admin, operations.Assignment{ID: "report", Agent: "worker", Owner: admin.Subject, Goal: "File the report", DoneWhen: "The answer starts DONE", DoneChecks: doneChecks(), State: operations.Requested}, 0)
		if err != nil {
			t.Fatal(err)
		}
		first, err := p.RunAssignment(t.Context(), admin, a.ID)
		if err != nil {
			t.Fatal(err)
		}
		within(t, "the assignment settles as a result", func() bool {
			got, _, _ := p.Operations.Assignment(tenant, a.ID)
			return got.State == operations.Result
		})
		got, _, _ := p.Operations.Assignment(tenant, a.ID)
		if got.Continued != 2 || got.RunID == first.ID {
			t.Fatalf("assignment = %+v", got)
		}
		last, err := p.Runs.Get(tenant, got.RunID)
		if err != nil || last.Status != runs.Completed || last.Answer != "DONE: the report is filed." {
			t.Fatalf("last run = %+v, %v", last, err)
		}
		// One conversation holds the three turns, and each continuation
		// says which checks failed.
		turns, err := p.Runs.Conversation(tenant, first.ID)
		if err != nil || len(turns) != 3 {
			t.Fatalf("%d turns: %v", len(turns), err)
		}
		if second := turns[1].Text; !strings.Contains(second, `the answer contains "DONE:"`) || !strings.Contains(second, `the answer does not contain "TODO"`) || !strings.Contains(second, "File the report") {
			t.Fatalf("second turn asked %q", second)
		}
		if third := turns[2].Text; strings.Contains(third, `the answer contains "DONE:"`) || !strings.Contains(third, `the answer does not contain "TODO"`) {
			t.Fatalf("third turn asked %q", third)
		}
	})
}

func TestAnAssignmentThatNeverPassesIsBlockedForItsOwner(t *testing.T) {
	eachBackend(t, func(t *testing.T, b platform.Backends, _ func() platform.Backends) {
		c := &cast{continues: true, says: map[string][][]saigetypes.Delta{"worker": {say("Not yet."), say("Still not."), say("Never asked for.")}}}
		p := stage(t, c, platform.Config{Backends: b})
		a, err := p.SetAssignment(t.Context(), admin, operations.Assignment{ID: "stuck", Agent: "worker", Owner: admin.Subject, Goal: "File the report", DoneWhen: "The answer starts DONE", DoneChecks: doneChecks()[:1], MaxContinue: 1, State: operations.Requested}, 0)
		if err != nil {
			t.Fatal(err)
		}
		first, err := p.RunAssignment(t.Context(), admin, a.ID)
		if err != nil {
			t.Fatal(err)
		}
		within(t, "the assignment is blocked", func() bool {
			got, _, _ := p.Operations.Assignment(tenant, a.ID)
			return got.State == operations.Blocked
		})
		got, _, _ := p.Operations.Assignment(tenant, a.ID)
		if got.Continued != 1 || !strings.Contains(got.Blocker, "continued 1 times") || !strings.Contains(got.Blocker, `the answer contains "DONE:"`) {
			t.Fatalf("assignment = %+v", got)
		}
		if turns, _ := p.Runs.Conversation(tenant, first.ID); len(turns) != 2 {
			t.Fatalf("%d turns, want the run and one continuation", len(turns))
		}
	})
}

func TestAnAssignmentWhoseFirstAnswerPassesSettlesAtOnce(t *testing.T) {
	eachBackend(t, func(t *testing.T, b platform.Backends, _ func() platform.Backends) {
		c := &cast{says: map[string][][]saigetypes.Delta{"worker": {say("DONE: filed.")}}}
		p := stage(t, c, platform.Config{Backends: b})
		a, err := p.SetAssignment(t.Context(), admin, operations.Assignment{ID: "easy", Agent: "worker", Owner: admin.Subject, Goal: "File the report", DoneWhen: "The answer starts DONE", DoneChecks: doneChecks(), State: operations.Requested}, 0)
		if err != nil {
			t.Fatal(err)
		}
		first, err := p.RunAssignment(t.Context(), admin, a.ID)
		if err != nil {
			t.Fatal(err)
		}
		within(t, "the assignment settles", func() bool {
			got, _, _ := p.Operations.Assignment(tenant, a.ID)
			return got.State == operations.Result
		})
		if got, _, _ := p.Operations.Assignment(tenant, a.ID); got.Continued != 0 || got.RunID != first.ID {
			t.Fatalf("assignment = %+v", got)
		}
	})
}

func TestAnAssignmentsChecksAreItsContract(t *testing.T) {
	c := &cast{says: map[string][][]saigetypes.Delta{"worker": {say("x")}}}
	p := stage(t, c, platform.Config{})
	base := operations.Assignment{ID: "contract", Agent: "worker", Owner: admin.Subject, Goal: "g", DoneWhen: "d", DoneChecks: doneChecks(), State: operations.Requested}
	a, err := p.SetAssignment(t.Context(), admin, base, 0)
	if err != nil {
		t.Fatal(err)
	}
	// A change that names no checks keeps them.
	plain := base
	plain.DoneChecks, plain.State = nil, operations.Accepted
	kept, err := p.SetAssignment(t.Context(), admin, plain, a.Version)
	if err != nil || len(kept.DoneChecks) != 2 {
		t.Fatalf("kept = %+v, %v", kept, err)
	}
	// Other checks are another contract.
	other := base
	other.DoneChecks, other.State = doneChecks()[:1], operations.Accepted
	if _, err := p.SetAssignment(t.Context(), admin, other, kept.Version); err == nil || !strings.Contains(err.Error(), "create a new assignment") {
		t.Fatalf("err = %v", err)
	}
	for name, bad := range map[string]operations.Assignment{
		"an unknown kind":        {DoneChecks: []operations.Check{{Kind: "judged", Value: "x"}}},
		"a bad pattern":          {DoneChecks: []operations.Check{{Kind: operations.Matches, Value: "("}}},
		"an empty value":         {DoneChecks: []operations.Check{{Kind: operations.Contains}}},
		"continues and no check": {MaxContinue: 2},
		"too many continues":     {DoneChecks: doneChecks(), MaxContinue: operations.MaxContinues + 1},
		"checks on a clock":      {DoneChecks: doneChecks(), Cron: "0 9 * * 1", Trigger: operations.CronTrigger},
	} {
		bad.ID, bad.Agent, bad.Owner, bad.Goal, bad.DoneWhen, bad.State = "bad", "worker", admin.Subject, "g", "d", operations.Requested
		if _, err := p.SetAssignment(t.Context(), admin, bad, 0); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}
