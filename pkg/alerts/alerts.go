// Package alerts turns what runs do into things a person should look at.
//
// A rule is a condition on a run that has just ended. When it holds, the engine
// raises an alert, which stays open until someone acknowledges it. A tenant
// with no rules gets no alerts. A rule may be scoped to an agent and a
// project, paused, held quiet for a cooldown after it raises, and addressed
// to some people or roles rather than everyone who may read alerts.
//
// Rules and alerts are in memory and are lost on restart.
package alerts

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/urmzd/warp/pkg/evals"
	"github.com/urmzd/warp/pkg/runs"
)

// Errors.
var (
	ErrInvalid  = errors.New("alerts: invalid")
	ErrExists   = errors.New("alerts: rule already exists")
	ErrNotFound = errors.New("alerts: not found")
)

// Bounds. maxAlerts is what a tenant keeps, the oldest dropped first. The
// rest bound what one rule may ask for.
const (
	maxAlerts      = 1000
	maxRules       = 200
	maxDescription = 500
	maxNotify      = 20
	maxRecipient   = 254
	// MaxWindowSeconds is the furthest back a rule may look: 30 days, the
	// same as analytics.
	MaxWindowSeconds = 30 * 24 * 60 * 60
	// MaxCooldownSeconds is the longest a rule may stay quiet after it
	// raises: 30 days.
	MaxCooldownSeconds = 30 * 24 * 60 * 60
)

// RolePrefix marks a notify entry that names a role, as in "role:operator".
// Any other entry is a person's subject.
const RolePrefix = "role:"

// Kind is the condition a rule checks.
type Kind string

const (
	// RunFailed fires once for every run that fails.
	RunFailed Kind = "run_failed"
	// FailureRate fires when the share of failed runs in a window passes a
	// threshold. It stays quiet while its last alert is still open.
	FailureRate Kind = "failure_rate"
	// SlowRun fires when a run took longer than a threshold to end.
	SlowRun Kind = "slow_run"
	// EvalBelow fires when an eval run's pass rate ends under a threshold.
	EvalBelow Kind = "eval_below"
	// Spend fires when what the runs in scope cost over a window reaches
	// ThresholdMicros. Each run counts its own model calls, priced in US
	// dollars. It stays quiet while its last alert is still open.
	Spend Kind = "spend"
	// Budget is raised by a billing policy, not by a rule: a scope is close
	// to a limit, or a run was refused at one.
	Budget Kind = "budget"
)

// Severity ranks an alert.
type Severity string

const (
	Warning  Severity = "warning"
	Critical Severity = "critical"
)

// Rule is one condition, scoped to an agent, a project, both, or everything.
type Rule struct {
	Name string `json:"name"`
	Kind Kind   `json:"kind"`
	// Description says what the rule is for, in a person's words.
	Description string `json:"description,omitempty"`
	// Agent limits the rule to one agent. Empty means every agent.
	Agent string `json:"agent,omitempty"`
	// Project limits the rule to runs filed under one project. Empty means
	// every run, in a project or not. It narrows together with Agent.
	Project  string   `json:"project,omitempty"`
	Severity Severity `json:"severity"`
	// Threshold is a percentage for FailureRate and EvalBelow, and seconds
	// for SlowRun. Spend uses ThresholdMicros instead.
	Threshold float64 `json:"threshold,omitempty"`
	// ThresholdMicros is the spend, in micro-USD, at which a Spend rule fires.
	ThresholdMicros int64 `json:"threshold_micros,omitempty"`
	// WindowSeconds is how far back FailureRate and Spend look.
	WindowSeconds int `json:"window_seconds,omitempty"`
	// MinRuns is how many runs FailureRate needs before it can fire, so one
	// failure out of one run is not a 100% failure rate worth paging on.
	MinRuns int `json:"min_runs,omitempty"`
	// CooldownSeconds is how long the rule stays quiet after it raises an
	// alert, whether or not that alert is still open. Zero means no cooldown.
	CooldownSeconds int `json:"cooldown_seconds,omitempty"`
	// Paused rules are not evaluated. A rule is enabled unless it says it is
	// paused, so a rule written before this field existed stays enabled.
	Paused bool `json:"paused,omitempty"`
	// Notify is who the inbox message about an alert is for: subjects, and
	// roles as "role:<name>". Empty means everyone who may read alerts.
	// Either way, only people who may read alerts see it.
	Notify    []string  `json:"notify,omitempty"`
	CreatedBy string    `json:"created_by,omitempty"`
	CreatedAt time.Time `json:"created_at,omitzero"`
	UpdatedBy string    `json:"updated_by,omitempty"`
	UpdatedAt time.Time `json:"updated_at,omitzero"`
}

// normalized trims what a person typed, so " role:ops " and "role:ops" are
// the same entry.
func (r Rule) normalized() Rule {
	r.Description = strings.TrimSpace(r.Description)
	if r.Notify != nil {
		notify := make([]string, len(r.Notify))
		for i, n := range r.Notify {
			notify[i] = strings.TrimSpace(n)
		}
		r.Notify = notify
	}
	return r
}

func (r Rule) validate() error {
	switch {
	case strings.TrimSpace(r.Name) == "":
		return fmt.Errorf("%w: a rule needs a name", ErrInvalid)
	case r.Severity != Warning && r.Severity != Critical:
		return fmt.Errorf("%w: severity must be warning or critical", ErrInvalid)
	case len(r.Description) > maxDescription:
		return fmt.Errorf("%w: a description is at most %d characters", ErrInvalid, maxDescription)
	case r.WindowSeconds < 0 || r.WindowSeconds > MaxWindowSeconds:
		return fmt.Errorf("%w: a window is from 1 to %d seconds", ErrInvalid, MaxWindowSeconds)
	case r.CooldownSeconds < 0 || r.CooldownSeconds > MaxCooldownSeconds:
		return fmt.Errorf("%w: a cooldown is from 0 to %d seconds", ErrInvalid, MaxCooldownSeconds)
	case r.MinRuns < 0:
		return fmt.Errorf("%w: a minimum number of runs cannot be negative", ErrInvalid)
	case r.ThresholdMicros < 0:
		return fmt.Errorf("%w: a spend threshold cannot be negative", ErrInvalid)
	case r.Kind != Spend && r.ThresholdMicros != 0:
		return fmt.Errorf("%w: threshold_micros is for spend rules", ErrInvalid)
	}
	if err := validNotify(r.Notify); err != nil {
		return err
	}
	switch r.Kind {
	case RunFailed:
	case FailureRate:
		if r.Threshold <= 0 || r.Threshold > 100 {
			return fmt.Errorf("%w: a failure rate threshold is a percentage from 1 to 100", ErrInvalid)
		}
		if r.WindowSeconds <= 0 {
			return fmt.Errorf("%w: a failure rate rule needs a window", ErrInvalid)
		}
		if r.MinRuns < 1 {
			return fmt.Errorf("%w: a failure rate rule needs a minimum number of runs", ErrInvalid)
		}
	case SlowRun:
		if r.Threshold <= 0 {
			return fmt.Errorf("%w: a slow run threshold is a number of seconds above zero", ErrInvalid)
		}
	case EvalBelow:
		if r.Threshold <= 0 || r.Threshold > 100 {
			return fmt.Errorf("%w: an eval threshold is a pass rate from 1 to 100", ErrInvalid)
		}
		if r.Project != "" {
			return fmt.Errorf("%w: eval runs are not filed under a project, so an eval rule cannot be scoped to one", ErrInvalid)
		}
	case Spend:
		if r.ThresholdMicros <= 0 {
			return fmt.Errorf("%w: a spend rule needs threshold_micros, in micro-USD, above zero", ErrInvalid)
		}
		if r.Threshold != 0 {
			return fmt.Errorf("%w: a spend rule takes threshold_micros, not threshold", ErrInvalid)
		}
		if r.WindowSeconds <= 0 {
			return fmt.Errorf("%w: a spend rule needs a window", ErrInvalid)
		}
	default:
		return fmt.Errorf("%w: unknown rule kind %q", ErrInvalid, r.Kind)
	}
	return nil
}

func validNotify(notify []string) error {
	if len(notify) > maxNotify {
		return fmt.Errorf("%w: a rule notifies at most %d people or roles", ErrInvalid, maxNotify)
	}
	for i, n := range notify {
		switch {
		case n == "":
			return fmt.Errorf("%w: notify entry %d is empty", ErrInvalid, i)
		case len(n) > maxRecipient:
			return fmt.Errorf("%w: notify entry %d is longer than %d characters", ErrInvalid, i, maxRecipient)
		case n == RolePrefix || strings.HasPrefix(n, RolePrefix) && strings.TrimSpace(n[len(RolePrefix):]) != n[len(RolePrefix):]:
			return fmt.Errorf("%w: notify entry %q names no role; write role:<name>", ErrInvalid, n)
		case slices.Contains(notify[:i], n):
			return fmt.Errorf("%w: %q is in notify twice", ErrInvalid, n)
		}
	}
	return nil
}

// applies reports whether a rule watches a run by an agent in a project.
func (r Rule) applies(agent, project string) bool {
	return !r.Paused && (r.Agent == "" || r.Agent == agent) && (r.Project == "" || r.Project == project)
}

// scope names what a rule watches, for an alert's title.
func (r Rule) scope() string {
	switch {
	case r.Agent != "" && r.Project != "":
		return r.Agent + " in " + r.Project
	case r.Agent != "":
		return r.Agent
	case r.Project != "":
		return "Project " + r.Project
	}
	return "All agents"
}

// Alert is one firing of a rule.
type Alert struct {
	ID       string   `json:"id"`
	Rule     string   `json:"rule"`
	Kind     Kind     `json:"kind"`
	Severity Severity `json:"severity"`
	Agent    string   `json:"agent,omitempty"`
	// Project is the project of the run that raised the alert, if any.
	Project string `json:"project,omitempty"`
	RunID   string `json:"run_id,omitempty"`
	// Suite names the eval suite behind an EvalBelow alert.
	Suite string    `json:"suite,omitempty"`
	Title string    `json:"title"`
	Body  string    `json:"body,omitempty"`
	At    time.Time `json:"at"`
	// AckedBy and AckedAt are set once a person has seen it.
	AckedBy string    `json:"acked_by,omitempty"`
	AckedAt time.Time `json:"acked_at,omitzero"`
	// Notify is the rule's notify list when the alert was raised: who the
	// message about it is for. It is not part of the alert's wire form.
	Notify []string `json:"-"`
}

// Open reports whether nobody has acknowledged the alert.
func (a Alert) Open() bool { return a.AckedBy == "" }

type tenant struct {
	rules  []Rule
	alerts []Alert
	next   int
	// last is when each rule last raised an alert, for its cooldown.
	last map[string]time.Time
}

// add records an alert, numbering it and dropping the oldest past the cap.
func (t *tenant) add(a Alert) Alert {
	t.next++
	a.ID = "al-" + strconv.Itoa(t.next)
	t.alerts = append(t.alerts, a)
	if len(t.alerts) > maxAlerts {
		t.alerts = t.alerts[len(t.alerts)-maxAlerts:]
	}
	return a
}

// cooling reports whether a rule raised an alert less than its cooldown
// before at.
func (t *tenant) cooling(r Rule, at time.Time) bool {
	last, ok := t.last[r.Name]
	return ok && r.CooldownSeconds > 0 && at.Before(last.Add(time.Duration(r.CooldownSeconds)*time.Second))
}

// fired raises an alert for a rule and starts its cooldown.
func (t *tenant) fired(r Rule, a Alert) Alert {
	a.Rule, a.Kind, a.Severity, a.Notify = r.Name, r.Kind, r.Severity, slices.Clone(r.Notify)
	if t.last == nil {
		t.last = map[string]time.Time{}
	}
	t.last[r.Name] = a.At
	return t.add(a)
}

// openFor reports whether a rule has an alert nobody has acknowledged yet
// for an agent. One open alert per rule and scope is enough: a second one
// while the first is unread says nothing new.
func (t *tenant) openFor(r Rule, agent string) bool {
	return slices.ContainsFunc(t.alerts, func(a Alert) bool {
		return a.Open() && a.Rule == r.Name && (r.Agent == "" || a.Agent == agent)
	})
}

// Engine holds every tenant's rules and alerts.
type Engine struct {
	mu      sync.Mutex
	tenants map[string]*tenant
}

// NewEngine returns an engine with no rules.
func NewEngine() *Engine { return &Engine{tenants: map[string]*tenant{}} }

func (e *Engine) of(name string) *tenant {
	t, ok := e.tenants[name]
	if !ok {
		t = &tenant{}
		e.tenants[name] = t
	}
	return t
}

// AddRule stores a rule.
func (e *Engine) AddRule(tenantName string, r Rule) error {
	r = r.normalized()
	if err := r.validate(); err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	t := e.of(tenantName)
	if slices.ContainsFunc(t.rules, func(have Rule) bool { return have.Name == r.Name }) {
		return fmt.Errorf("%w: %q", ErrExists, r.Name)
	}
	if len(t.rules) >= maxRules {
		return fmt.Errorf("%w: a workspace has at most %d rules", ErrInvalid, maxRules)
	}
	t.rules = append(t.rules, r)
	return nil
}

// ReplaceRule replaces a rule with the same name and returns it as stored.
// Who created it and when are kept from the rule it replaces, and so is its
// cooldown. Alerts it already raised stay.
func (e *Engine) ReplaceRule(tenantName string, r Rule) (Rule, error) {
	r = r.normalized()
	e.mu.Lock()
	defer e.mu.Unlock()
	t := e.of(tenantName)
	i := slices.IndexFunc(t.rules, func(have Rule) bool { return have.Name == r.Name })
	if i < 0 {
		return Rule{}, fmt.Errorf("%w: rule %q", ErrNotFound, r.Name)
	}
	if err := r.validate(); err != nil {
		return Rule{}, err
	}
	r.CreatedBy, r.CreatedAt = t.rules[i].CreatedBy, t.rules[i].CreatedAt
	t.rules[i] = r
	return r, nil
}

// Rule returns one of a tenant's rules.
func (e *Engine) Rule(tenantName, name string) (Rule, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, r := range e.of(tenantName).rules {
		if r.Name == name {
			return r, nil
		}
	}
	return Rule{}, fmt.Errorf("%w: rule %q", ErrNotFound, name)
}

// RemoveRule deletes a rule. Alerts it already raised stay.
func (e *Engine) RemoveRule(tenantName, name string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	t := e.of(tenantName)
	i := slices.IndexFunc(t.rules, func(r Rule) bool { return r.Name == name })
	if i < 0 {
		return fmt.Errorf("%w: rule %q", ErrNotFound, name)
	}
	t.rules = slices.Delete(t.rules, i, i+1)
	delete(t.last, name)
	return nil
}

// Rules returns a tenant's rules.
func (e *Engine) Rules(tenantName string) []Rule {
	e.mu.Lock()
	defer e.mu.Unlock()
	return slices.Clone(e.of(tenantName).rules)
}

// Alerts returns a tenant's alerts, newest first.
func (e *Engine) Alerts(tenantName string) []Alert {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := slices.Clone(e.of(tenantName).alerts)
	slices.Reverse(out)
	return out
}

// Ack marks an alert as seen by a person. Acknowledging twice keeps the first.
func (e *Engine) Ack(tenantName, id, by string, at time.Time) (Alert, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	t := e.of(tenantName)
	for i := range t.alerts {
		if t.alerts[i].ID == id {
			if t.alerts[i].Open() {
				t.alerts[i].AckedBy, t.alerts[i].AckedAt = by, at
			}
			return t.alerts[i], nil
		}
	}
	return Alert{}, fmt.Errorf("%w: alert %q", ErrNotFound, id)
}

// Recent sums a tenant's runs in a scope that started since a time. An empty
// agent or project means every agent or project.
type Recent func(tenant, agent, project string, since time.Time) runs.Window

// Evaluate checks a run that has just ended against its tenant's rules and
// returns the alerts it raised.
func (e *Engine) Evaluate(run runs.Run, recent Recent) []Alert {
	e.mu.Lock()
	defer e.mu.Unlock()
	t := e.of(run.Tenant)
	var raised []Alert
	raise := func(r Rule, title, body string) {
		raised = append(raised, t.fired(r, Alert{
			Agent: run.Agent, Project: run.Project, RunID: run.ID, Title: title, Body: body, At: run.UpdatedAt,
		}))
	}

	for _, r := range t.rules {
		if !r.applies(run.Agent, run.Project) || t.cooling(r, run.UpdatedAt) {
			continue
		}
		switch r.Kind {
		case RunFailed:
			if run.Status == runs.Failed {
				raise(r, run.Agent+" failed", run.Error)
			}
		case SlowRun:
			took := run.UpdatedAt.Sub(run.CreatedAt)
			if took.Seconds() > r.Threshold {
				raise(r, fmt.Sprintf("%s took %s", run.Agent, took.Round(time.Millisecond)),
					fmt.Sprintf("The limit is %gs.", r.Threshold))
			}
		case FailureRate:
			if run.Status != runs.Failed || recent == nil || t.openFor(r, run.Agent) {
				continue
			}
			window := time.Duration(r.WindowSeconds) * time.Second
			w := recent(run.Tenant, r.Agent, r.Project, run.UpdatedAt.Add(-window))
			if w.Total < r.MinRuns || w.Total == 0 {
				continue
			}
			if rate := 100 * float64(w.Failed) / float64(w.Total); rate >= r.Threshold {
				raise(r, fmt.Sprintf("%s: %.0f%% of runs are failing", r.scope(), rate),
					fmt.Sprintf("%d of %d runs failed in the last %s.", w.Failed, w.Total, window))
			}
		case Spend:
			if recent == nil || t.openFor(r, run.Agent) {
				continue
			}
			window := time.Duration(r.WindowSeconds) * time.Second
			w := recent(run.Tenant, r.Agent, r.Project, run.UpdatedAt.Add(-window))
			if w.Spent >= r.ThresholdMicros {
				raise(r, fmt.Sprintf("%s: %s spent in the last %s", r.scope(), Dollars(w.Spent), window),
					fmt.Sprintf("%d runs cost %s. The rule fires at %s.", w.Total, Dollars(w.Spent), Dollars(r.ThresholdMicros)))
			}
		}
	}
	return raised
}

// Dollars writes micro-USD as dollars and cents, rounded down.
func Dollars(micros int64) string {
	sign := ""
	if micros < 0 {
		sign, micros = "-", -micros
	}
	return fmt.Sprintf("%s$%d.%02d", sign, micros/1_000_000, micros%1_000_000/10_000)
}

// EvaluateEval checks a finished eval run against its tenant's rules. A rule
// scoped to an agent applies to suites that test that agent, and a rule with no
// agent applies to every suite.
func (e *Engine) EvaluateEval(tenantName string, run evals.Run) []Alert {
	if run.Status == evals.Running {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	t := e.of(tenantName)
	var raised []Alert
	for _, r := range t.rules {
		if r.Kind != EvalBelow {
			continue
		}
		agent := ""
		if run.Target == evals.Agent {
			agent = run.Subject
		}
		if !r.applies(agent, "") || t.cooling(r, run.EndedAt) {
			continue
		}
		// A run that could not be carried out scored nothing, which is
		// under any threshold.
		if run.Status != evals.Errored && run.PassRate >= r.Threshold {
			continue
		}
		raised = append(raised, t.fired(r, Alert{
			Agent: agent, Suite: run.Suite, At: run.EndedAt,
			Title: fmt.Sprintf("Eval %s passed %.0f%% of its cases", run.Suite, run.PassRate),
			Body:  fmt.Sprintf("%d of %d cases passed. The rule expects at least %.0f%%.", run.Passed, run.Total, r.Threshold),
		}))
	}
	return raised
}

// Raise records an alert the platform itself decided on. key names what it is
// about, and while an alert with that key is open a second one is not raised:
// a limit that is refusing every run says so once, not once per run.
func (e *Engine) Raise(tenantName, key string, a Alert) (Alert, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	t := e.of(tenantName)
	if slices.ContainsFunc(t.alerts, func(x Alert) bool { return x.Open() && x.Rule == key }) {
		return Alert{}, false
	}
	a.Rule = key
	return t.add(a), true
}
