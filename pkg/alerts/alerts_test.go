package alerts

import (
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/warp/pkg/evals"
	"github.com/urmzd/warp/pkg/runs"
)

var t0 = time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)

func ended(agent string, status runs.Status, took time.Duration) runs.Run {
	return runs.Run{ID: "r-" + agent, Tenant: "acme", Agent: agent, Status: status, Error: "boom", CreatedAt: t0, UpdatedAt: t0.Add(took)}
}

func TestNoRulesNoAlerts(t *testing.T) {
	e := NewEngine()
	if got := must(e.Evaluate(ended("lead", runs.Failed, time.Second), nil)); len(got) != 0 {
		t.Fatalf("raised %v with no rules", got)
	}
}

func TestRunFailed(t *testing.T) {
	e := NewEngine()
	if err := e.AddRule("acme", Rule{Name: "any", Kind: RunFailed, Severity: Critical}); err != nil {
		t.Fatal(err)
	}
	if got := must(e.Evaluate(ended("lead", runs.Completed, time.Second), nil)); len(got) != 0 {
		t.Fatalf("a completed run raised %v", got)
	}
	got := must(e.Evaluate(ended("lead", runs.Failed, time.Second), nil))
	if len(got) != 1 || got[0].Agent != "lead" || got[0].Body != "boom" || !got[0].Open() {
		t.Fatalf("got %+v", got)
	}
}

func TestRuleScopedToAnAgent(t *testing.T) {
	e := NewEngine()
	_ = e.AddRule("acme", Rule{Name: "lead-only", Kind: RunFailed, Agent: "lead", Severity: Warning})
	if got := must(e.Evaluate(ended("helper", runs.Failed, time.Second), nil)); len(got) != 0 {
		t.Fatalf("raised for another agent: %v", got)
	}
}

func TestRulesBelongToOneTenant(t *testing.T) {
	e := NewEngine()
	_ = e.AddRule("globex", Rule{Name: "any", Kind: RunFailed, Severity: Critical})
	if got := must(e.Evaluate(ended("lead", runs.Failed, time.Second), nil)); len(got) != 0 {
		t.Fatalf("another tenant's rule fired: %v", got)
	}
	if len(e.Alerts("globex")) != 0 || len(e.Rules("acme")) != 0 {
		t.Fatal("tenants share state")
	}
}

func TestSlowRun(t *testing.T) {
	e := NewEngine()
	_ = e.AddRule("acme", Rule{Name: "slow", Kind: SlowRun, Threshold: 5, Severity: Warning})
	if got := must(e.Evaluate(ended("lead", runs.Completed, 4*time.Second), nil)); len(got) != 0 {
		t.Fatalf("a fast run raised %v", got)
	}
	if got := must(e.Evaluate(ended("lead", runs.Completed, 6*time.Second), nil)); len(got) != 1 {
		t.Fatalf("a slow run raised %v", got)
	}
}

func TestFailureRate(t *testing.T) {
	e := NewEngine()
	_ = e.AddRule("acme", Rule{Name: "rate", Kind: FailureRate, Threshold: 50, WindowSeconds: 600, MinRuns: 4, Severity: Critical})
	counts := func(total, failed int) Recent {
		return func(string, string, string, time.Time) runs.Window { return runs.Window{Total: total, Failed: failed} }
	}
	if got := must(e.Evaluate(ended("lead", runs.Failed, time.Second), counts(1, 1))); len(got) != 0 {
		t.Fatalf("one failure of one run paged: %v", got)
	}
	if got := must(e.Evaluate(ended("lead", runs.Failed, time.Second), counts(10, 2))); len(got) != 0 {
		t.Fatalf("20%% fired a 50%% rule: %v", got)
	}
	if got := must(e.Evaluate(ended("lead", runs.Failed, time.Second), counts(10, 6))); len(got) != 1 {
		t.Fatalf("60%% did not fire: %v", got)
	}
	// While that alert is unread, the rule stays quiet.
	if got := must(e.Evaluate(ended("lead", runs.Failed, time.Second), counts(10, 7))); len(got) != 0 {
		t.Fatalf("fired again while open: %v", got)
	}
	open := e.Alerts("acme")[0]
	if _, err := e.Ack("acme", open.ID, "ada", t0); err != nil {
		t.Fatal(err)
	}
	if got := must(e.Evaluate(ended("lead", runs.Failed, time.Second), counts(10, 7))); len(got) != 1 {
		t.Fatalf("did not fire after the ack: %v", got)
	}
}

func TestAck(t *testing.T) {
	e := NewEngine()
	_ = e.AddRule("acme", Rule{Name: "any", Kind: RunFailed, Severity: Critical})
	a := must(e.Evaluate(ended("lead", runs.Failed, time.Second), nil))[0]
	if _, err := e.Ack("globex", a.ID, "eve", t0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("another tenant acknowledged it: %v", err)
	}
	first, _ := e.Ack("acme", a.ID, "ada", t0)
	second, _ := e.Ack("acme", a.ID, "bob", t0.Add(time.Hour))
	if first.AckedBy != "ada" || second.AckedBy != "ada" {
		t.Fatalf("the first acknowledgement was overwritten: %+v", second)
	}
}

func TestEvalBelow(t *testing.T) {
	e := NewEngine()
	_ = e.AddRule("acme", Rule{Name: "lead-quality", Kind: EvalBelow, Agent: "lead", Threshold: 80, Severity: Warning})
	run := func(subject string, rate float64, status evals.Status) evals.Run {
		return evals.Run{Suite: "smoke", Target: evals.Agent, Subject: subject, Status: status, PassRate: rate, Passed: 1, Total: 2, EndedAt: t0}
	}
	if got := must(e.EvaluateEval("acme", run("lead", 90, evals.Passed))); len(got) != 0 {
		t.Fatalf("90%% fired an 80%% rule: %v", got)
	}
	if got := must(e.EvaluateEval("acme", run("helper", 10, evals.Failed))); len(got) != 0 {
		t.Fatalf("another agent's suite fired: %v", got)
	}
	if got := must(e.EvaluateEval("acme", run("lead", 50, evals.Running))); len(got) != 0 {
		t.Fatalf("a run still in flight fired: %v", got)
	}
	got := must(e.EvaluateEval("acme", run("lead", 50, evals.Failed)))
	if len(got) != 1 || got[0].Suite != "smoke" || got[0].Agent != "lead" {
		t.Fatalf("got %+v", got)
	}
	if got := must(e.EvaluateEval("globex", run("lead", 50, evals.Failed))); len(got) != 0 {
		t.Fatalf("another tenant's rule fired: %v", got)
	}
	// A run of a normal agent does not trip an eval rule.
	if got := must(e.Evaluate(ended("lead", runs.Failed, time.Second), nil)); len(got) != 0 {
		t.Fatalf("a failed run fired an eval rule: %v", got)
	}
}

func TestInvalidRules(t *testing.T) {
	e := NewEngine()
	for name, r := range map[string]Rule{
		"no name":              {Kind: RunFailed, Severity: Warning},
		"no severity":          {Name: "x", Kind: RunFailed},
		"unknown kind":         {Name: "x", Kind: "vibes", Severity: Warning},
		"rate over 100":        {Name: "x", Kind: FailureRate, Threshold: 150, WindowSeconds: 60, MinRuns: 1, Severity: Warning},
		"rate, no window":      {Name: "x", Kind: FailureRate, Threshold: 50, MinRuns: 1, Severity: Warning},
		"rate, no minimum":     {Name: "x", Kind: FailureRate, Threshold: 50, WindowSeconds: 60, Severity: Warning},
		"slow, no limit":       {Name: "x", Kind: SlowRun, Severity: Warning},
		"long description":     {Name: "x", Kind: RunFailed, Severity: Warning, Description: strings.Repeat("a", 501)},
		"window too long":      {Name: "x", Kind: FailureRate, Threshold: 50, WindowSeconds: MaxWindowSeconds + 1, MinRuns: 1, Severity: Warning},
		"cooldown too long":    {Name: "x", Kind: RunFailed, Severity: Warning, CooldownSeconds: MaxCooldownSeconds + 1},
		"negative cooldown":    {Name: "x", Kind: RunFailed, Severity: Warning, CooldownSeconds: -1},
		"spend, no amount":     {Name: "x", Kind: Spend, WindowSeconds: 60, Severity: Warning},
		"spend, no window":     {Name: "x", Kind: Spend, ThresholdMicros: 1, Severity: Warning},
		"spend, a percent":     {Name: "x", Kind: Spend, ThresholdMicros: 1, Threshold: 5, WindowSeconds: 60, Severity: Warning},
		"micros elsewhere":     {Name: "x", Kind: RunFailed, ThresholdMicros: 5, Severity: Warning},
		"eval in a project":    {Name: "x", Kind: EvalBelow, Threshold: 50, Project: "Launch", Severity: Warning},
		"empty recipient":      {Name: "x", Kind: RunFailed, Severity: Warning, Notify: []string{" "}},
		"bare role":            {Name: "x", Kind: RunFailed, Severity: Warning, Notify: []string{"role:"}},
		"twice":                {Name: "x", Kind: RunFailed, Severity: Warning, Notify: []string{"ada", " ada"}},
		"too many":             {Name: "x", Kind: RunFailed, Severity: Warning, Notify: many(21)},
		"budget is not a rule": {Name: "x", Kind: Budget, Severity: Warning},
	} {
		if err := e.AddRule("acme", r); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: got %v", name, err)
		}
	}
	_ = e.AddRule("acme", Rule{Name: "x", Kind: RunFailed, Severity: Warning})
	if err := e.AddRule("acme", Rule{Name: "x", Kind: RunFailed, Severity: Warning}); !errors.Is(err, ErrExists) {
		t.Fatalf("duplicate: %v", err)
	}
}

func many(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = "person-" + strconv.Itoa(i)
	}
	return out
}

func TestAPausedRuleIsNotEvaluated(t *testing.T) {
	e := NewEngine()
	_ = e.AddRule("acme", Rule{Name: "any", Kind: RunFailed, Severity: Critical, Paused: true})
	_ = e.AddRule("acme", Rule{Name: "quality", Kind: EvalBelow, Threshold: 90, Severity: Warning, Paused: true})
	if got := must(e.Evaluate(ended("lead", runs.Failed, time.Second), nil)); len(got) != 0 {
		t.Fatalf("a paused rule fired: %v", got)
	}
	if got := must(e.EvaluateEval("acme", evals.Run{Suite: "s", Status: evals.Failed, PassRate: 10, EndedAt: t0})); len(got) != 0 {
		t.Fatalf("a paused eval rule fired: %v", got)
	}
	r, _ := e.Rule("acme", "any")
	r.Paused = false
	if _, err := e.ReplaceRule("acme", r); err != nil {
		t.Fatal(err)
	}
	if got := must(e.Evaluate(ended("lead", runs.Failed, time.Second), nil)); len(got) != 1 {
		t.Fatalf("a resumed rule did not fire: %v", got)
	}
}

func TestRuleScopedToAProject(t *testing.T) {
	e := NewEngine()
	_ = e.AddRule("acme", Rule{Name: "launch", Kind: RunFailed, Project: "Launch", Severity: Warning})
	_ = e.AddRule("acme", Rule{Name: "lead-launch", Kind: RunFailed, Agent: "lead", Project: "Launch", Severity: Warning})
	run := ended("helper", runs.Failed, time.Second)
	if got := must(e.Evaluate(run, nil)); len(got) != 0 {
		t.Fatalf("a run in no project fired a project rule: %v", got)
	}
	run.Project = "Other"
	if got := must(e.Evaluate(run, nil)); len(got) != 0 {
		t.Fatalf("a run in another project fired: %v", got)
	}
	run.Project = "Launch"
	got := must(e.Evaluate(run, nil))
	if len(got) != 1 || got[0].Rule != "launch" || got[0].Project != "Launch" {
		t.Fatalf("helper in Launch raised %+v, want only the project rule", got)
	}
	run.Agent = "lead"
	if got := must(e.Evaluate(run, nil)); len(got) != 2 {
		t.Fatalf("lead in Launch raised %+v, want both rules", got)
	}
}

func TestFailureRateAsksForItsProject(t *testing.T) {
	e := NewEngine()
	_ = e.AddRule("acme", Rule{Name: "rate", Kind: FailureRate, Project: "Launch", Threshold: 50, WindowSeconds: 600, MinRuns: 1, Severity: Critical})
	var asked string
	recent := func(_, _, project string, _ time.Time) runs.Window {
		asked = project
		return runs.Window{Total: 2, Failed: 2}
	}
	run := ended("lead", runs.Failed, time.Second)
	run.Project = "Launch"
	if got := must(e.Evaluate(run, recent)); len(got) != 1 || asked != "Launch" {
		t.Fatalf("raised %v, asked for project %q", got, asked)
	}
}

func TestCooldown(t *testing.T) {
	e := NewEngine()
	_ = e.AddRule("acme", Rule{Name: "any", Kind: RunFailed, Severity: Critical, CooldownSeconds: 60})
	at := func(d time.Duration) runs.Run {
		r := ended("lead", runs.Failed, time.Second)
		r.UpdatedAt = t0.Add(d)
		return r
	}
	if got := must(e.Evaluate(at(0), nil)); len(got) != 1 {
		t.Fatalf("first failure: %v", got)
	}
	// Acknowledged or not, the rule is quiet until the cooldown passes.
	_, _ = e.Ack("acme", e.Alerts("acme")[0].ID, "ada", t0)
	if got := must(e.Evaluate(at(30*time.Second), nil)); len(got) != 0 {
		t.Fatalf("fired inside the cooldown: %v", got)
	}
	if got := must(e.Evaluate(at(61*time.Second), nil)); len(got) != 1 {
		t.Fatalf("did not fire after the cooldown: %v", got)
	}
	// Replacing the rule keeps its cooldown; removing it forgets it.
	r, _ := e.Rule("acme", "any")
	if _, err := e.ReplaceRule("acme", r); err != nil {
		t.Fatal(err)
	}
	if got := must(e.Evaluate(at(62*time.Second), nil)); len(got) != 0 {
		t.Fatalf("a replaced rule lost its cooldown: %v", got)
	}
	_ = e.RemoveRule("acme", "any")
	_ = e.AddRule("acme", r)
	if got := must(e.Evaluate(at(63*time.Second), nil)); len(got) != 1 {
		t.Fatalf("a new rule inherited a removed one's cooldown: %v", got)
	}
}

func TestSpend(t *testing.T) {
	e := NewEngine()
	_ = e.AddRule("acme", Rule{Name: "spend", Kind: Spend, Agent: "lead", ThresholdMicros: 5_000_000, WindowSeconds: 3600, Severity: Warning})
	spent := func(micros int64) Recent {
		return func(_, agent, _ string, since time.Time) runs.Window {
			if agent != "lead" || !since.Equal(t0.Add(time.Second-time.Hour)) {
				t.Fatalf("asked for %q since %v", agent, since)
			}
			return runs.Window{Total: 3, Spent: micros}
		}
	}
	if got := must(e.Evaluate(ended("lead", runs.Completed, time.Second), spent(4_999_999))); len(got) != 0 {
		t.Fatalf("under the threshold fired: %v", got)
	}
	got := must(e.Evaluate(ended("lead", runs.Completed, time.Second), spent(5_000_000)))
	if len(got) != 1 || got[0].Kind != Spend || got[0].Title != "lead: $5.00 spent in the last 1h0m0s" {
		t.Fatalf("at the threshold raised %+v", got)
	}
	// While that alert is open, the rule stays quiet.
	if got := must(e.Evaluate(ended("lead", runs.Completed, time.Second), spent(9_000_000))); len(got) != 0 {
		t.Fatalf("fired again while open: %v", got)
	}
	if got := must(e.Evaluate(ended("helper", runs.Completed, time.Second), spent(9_000_000))); len(got) != 0 {
		t.Fatalf("another agent's run fired: %v", got)
	}
}

func TestDollars(t *testing.T) {
	for micros, want := range map[int64]string{0: "$0.00", 1: "$0.00", 12_345_678: "$12.34", -2_500_000: "-$2.50"} {
		if got := Dollars(micros); got != want {
			t.Errorf("Dollars(%d) = %q, want %q", micros, got, want)
		}
	}
}

func TestReplaceRule(t *testing.T) {
	e := NewEngine()
	_ = e.AddRule("acme", Rule{Name: "any", Kind: RunFailed, Severity: Critical, CreatedBy: "ada", CreatedAt: t0})
	got, err := e.ReplaceRule("acme", Rule{
		Name: "any", Kind: SlowRun, Threshold: 5, Severity: Warning, Description: "  Slow runs  ",
		Notify: []string{" role:operator ", "bob"}, CreatedBy: "eve", UpdatedBy: "bob", UpdatedAt: t0.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.CreatedBy != "ada" || !got.CreatedAt.Equal(t0) || got.UpdatedBy != "bob" || got.Kind != SlowRun ||
		got.Description != "Slow runs" || got.Notify[0] != "role:operator" {
		t.Fatalf("replaced = %+v", got)
	}
	if _, err := e.ReplaceRule("acme", Rule{Name: "any", Kind: "vibes", Severity: Warning}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("an invalid replacement: %v", err)
	}
	if r, _ := e.Rule("acme", "any"); r.Kind != SlowRun {
		t.Fatalf("an invalid replacement changed the rule: %+v", r)
	}
	if _, err := e.ReplaceRule("globex", got); !errors.Is(err, ErrNotFound) {
		t.Fatalf("another tenant's rule: %v", err)
	}
	if _, err := e.ReplaceRule("acme", Rule{Name: "ghost", Kind: RunFailed, Severity: Warning}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("an unknown rule: %v", err)
	}
}

func TestAnAlertCarriesItsRulesRecipients(t *testing.T) {
	e := NewEngine()
	_ = e.AddRule("acme", Rule{Name: "any", Kind: RunFailed, Severity: Critical, Notify: []string{"role:operator", "ada"}})
	got := must(e.Evaluate(ended("lead", runs.Failed, time.Second), nil))
	if len(got) != 1 || len(got[0].Notify) != 2 || got[0].Notify[0] != "role:operator" {
		t.Fatalf("raised %+v", got)
	}
}
