// Package alerts turns what runs do into things a person should look at.
//
// A rule is a condition on a run that has just ended. When it holds, the engine
// raises an alert, which stays open until someone acknowledges it. A tenant
// with no rules gets no alerts. A rule may be scoped to an agent and a
// project, paused, held quiet for a cooldown after it raises, and addressed
// to some people or roles rather than everyone who may read alerts.
//
// It reads and writes the database per request (pkg/state, per request): it
// holds nothing in memory, so any number of processes on one database agree,
// and it does not need pkg/state's single-writer lock. An alert that stays
// quiet while one is open takes an open key in the write that raises it, so
// two processes raising it at once raise it once.
package alerts

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/urmzd/humagen/pkg/evals"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/state"
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

// Engine keeps every tenant's rules and alerts in a state.Store.
type Engine struct {
	kept *state.Table[stored]
}

// namespace is where rules and alerts are kept. The tenant is the shard key;
// within it:
//
//	rules                  the tenant's rules, the number of its last alert
//	                       and when each rule last raised one
//	alert/<n>              an alert, numbered
//	open/<scope>/<agent>   the open alert of a rule, or of a key the platform
//	                       raises, that stays quiet while one is open
//
// A scope and an agent are hashed: either may hold a "/". At most maxAlerts
// alerts are kept: raising alert n drops alert n-maxAlerts.
//
// A release before open keys kept only the alerts. Their open keys are added
// the first time any process opens the engine, and converted, in the tenant
// "", marks that they were.
const namespace = "alerts"

// stored is one record: a tenant's rules (under rulesKey), one alert, or an
// open key.
type stored struct {
	Rules []Rule               `json:"rules,omitempty"`
	Next  int                  `json:"next,omitempty"`
	Last  map[string]time.Time `json:"last,omitempty"`
	Alert *storedAlert         `json:"alert,omitempty"`
	// Open is, under an open key, the id of the alert that holds it.
	Open string `json:"open,omitempty"`
}

// storedAlert is an alert with what its wire form leaves out.
type storedAlert struct {
	Alert
	Notify []string `json:"notify,omitempty"`
	// Scope is the open key the alert holds until it is acknowledged, ""
	// when it holds none.
	Scope string `json:"scope,omitempty"`
}

func (a *storedAlert) alert() Alert {
	out := a.Alert
	out.Notify = a.Notify
	return out
}

const (
	rulesKey    = "rules"
	alertPrefix = "alert/"
	openPrefix  = "open/"
	converted   = "converted:v2"
)

func alertNumber(id string) int64 {
	n, _ := strconv.ParseInt(strings.TrimPrefix(id, "al-"), 10, 64)
	return n
}

func alertKey(id string) string { return alertPrefix + state.Seq(alertNumber(id)) }

func hash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:16])
}

// openKey is where the open alert of a rule, or of a key the platform
// raises, is named. A rule scoped to an agent has its own; a rule for every
// agent, or a key, has one with no agent.
func openKey(rule, agent string) string { return openPrefix + hash(rule) + "/" + hash(agent) }

// watchesOpen reports whether a rule of this kind stays quiet while its last
// alert is open, and so holds an open key.
func watchesOpen(k Kind) bool { return k == FailureRate || k == Spend }

// NewEngine returns an engine with no rules, in this process's memory.
func NewEngine() *Engine {
	e, err := Open(nil)
	if err != nil {
		// An engine in memory has nothing to convert.
		panic(err)
	}
	return e
}

// Open returns an engine that keeps its rules and alerts in st, which may be
// shared with other processes. A nil st keeps them in this process's memory.
func Open(st state.Store) (*Engine, error) {
	if st == nil {
		st = state.NewMemory()
	}
	e := &Engine{kept: state.NewTable[stored](st, namespace)}
	if err := e.convert(); err != nil {
		return nil, fmt.Errorf("alerts: convert: %w", err)
	}
	return e, nil
}

// convert adds the open key of every open alert kept before open keys, each
// in one write with its alert, conditional on both: a process converting at
// the same time that wrote it first leaves nothing to do.
func (e *Engine) convert() error {
	if _, err := e.kept.Lookup("", converted); err == nil {
		return nil
	} else if !errors.Is(err, state.ErrNotFound) {
		return err
	}
	type open struct {
		tenant, key string
		a           storedAlert
	}
	rules := map[string][]Rule{}
	var opens []open
	err := e.kept.Load(func(tenant, key string, r stored) error {
		switch {
		case tenant == "":
		case key == rulesKey:
			rules[tenant] = r.Rules
		case strings.HasPrefix(key, alertPrefix) && r.Alert != nil && r.Alert.Open() && r.Alert.Scope == "":
			opens = append(opens, open{tenant, key, *r.Alert})
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, o := range opens {
		agent, holds := "", true
		if i := slices.IndexFunc(rules[o.tenant], func(r Rule) bool { return r.Name == o.a.Rule }); i >= 0 {
			r := rules[o.tenant][i]
			agent, holds = r.Agent, watchesOpen(r.Kind)
		}
		if !holds {
			continue
		}
		cur, err := e.kept.Lookup(o.tenant, o.key)
		if errors.Is(err, state.ErrNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		a := *cur.Value.Alert
		if !a.Open() || a.Scope != "" {
			continue
		}
		a.Scope = openKey(a.Rule, agent)
		b := e.kept.Batch()
		b.PutAt(o.tenant, o.key, stored{Alert: &a}, cur.Version)
		b.Create(o.tenant, a.Scope, stored{Open: a.ID})
		if err := b.Commit(); err != nil && !errors.Is(err, state.ErrConflict) {
			return err
		}
	}
	b := e.kept.Batch()
	b.Create("", converted, stored{Next: 2})
	if err := b.Commit(); err != nil && !errors.Is(err, state.ErrConflict) {
		return err
	}
	return nil
}

func wrap(err error) error {
	if err == nil || errors.Is(err, ErrInvalid) || errors.Is(err, ErrExists) || errors.Is(err, ErrNotFound) {
		return err
	}
	return fmt.Errorf("alerts: write: %w", err)
}

// rules reads a tenant's rules record, and its version: zero when it has
// none.
func (e *Engine) rules(tenantName string) (stored, int64, error) {
	cur, err := e.kept.Lookup(tenantName, rulesKey)
	if errors.Is(err, state.ErrNotFound) {
		return stored{}, 0, nil
	}
	if err != nil {
		return stored{}, 0, fmt.Errorf("alerts: read: %w", err)
	}
	return cur.Value, cur.Version, nil
}

// AddRule stores a rule.
func (e *Engine) AddRule(tenantName string, r Rule) error {
	r = r.normalized()
	if err := r.validate(); err != nil {
		return err
	}
	_, err := e.kept.Update(tenantName, rulesKey, func(t stored, _ bool) (stored, error) {
		if slices.ContainsFunc(t.Rules, func(have Rule) bool { return have.Name == r.Name }) {
			return t, fmt.Errorf("%w: %q", ErrExists, r.Name)
		}
		if len(t.Rules) >= maxRules {
			return t, fmt.Errorf("%w: a workspace has at most %d rules", ErrInvalid, maxRules)
		}
		t.Rules = append(slices.Clone(t.Rules), r)
		return t, nil
	})
	return wrap(err)
}

// ReplaceRule replaces a rule with the same name and returns it as stored.
// Who created it and when are kept from the rule it replaces, and so is its
// cooldown. Alerts it already raised stay.
func (e *Engine) ReplaceRule(tenantName string, r Rule) (Rule, error) {
	r = r.normalized()
	var out Rule
	_, err := e.kept.Update(tenantName, rulesKey, func(t stored, _ bool) (stored, error) {
		i := slices.IndexFunc(t.Rules, func(have Rule) bool { return have.Name == r.Name })
		if i < 0 {
			return t, fmt.Errorf("%w: rule %q", ErrNotFound, r.Name)
		}
		if err := r.validate(); err != nil {
			return t, err
		}
		out = r
		out.CreatedBy, out.CreatedAt = t.Rules[i].CreatedBy, t.Rules[i].CreatedAt
		t.Rules = slices.Clone(t.Rules)
		t.Rules[i] = out
		return t, nil
	})
	if err != nil {
		return Rule{}, wrap(err)
	}
	return out, nil
}

// Rule returns one of a tenant's rules.
func (e *Engine) Rule(tenantName, name string) (Rule, error) {
	t, _, err := e.rules(tenantName)
	if err != nil {
		return Rule{}, err
	}
	for _, r := range t.Rules {
		if r.Name == name {
			return r, nil
		}
	}
	return Rule{}, fmt.Errorf("%w: rule %q", ErrNotFound, name)
}

// RemoveRule deletes a rule. Alerts it already raised stay.
func (e *Engine) RemoveRule(tenantName, name string) error {
	_, err := e.kept.Update(tenantName, rulesKey, func(t stored, _ bool) (stored, error) {
		i := slices.IndexFunc(t.Rules, func(r Rule) bool { return r.Name == name })
		if i < 0 {
			return t, fmt.Errorf("%w: rule %q", ErrNotFound, name)
		}
		t.Rules = slices.Delete(slices.Clone(t.Rules), i, i+1)
		if _, ok := t.Last[name]; ok {
			last := maps.Clone(t.Last)
			delete(last, name)
			t.Last = last
		}
		return t, nil
	})
	return wrap(err)
}

// Rules returns a tenant's rules.
func (e *Engine) Rules(tenantName string) ([]Rule, error) {
	t, _, err := e.rules(tenantName)
	return t.Rules, err
}

// Alerts returns a tenant's alerts, newest first.
func (e *Engine) Alerts(tenantName string) ([]Alert, error) {
	items, _, err := e.kept.List(tenantName, state.Query{Prefix: alertPrefix, Desc: true, Limit: maxAlerts})
	if err != nil {
		return nil, fmt.Errorf("alerts: read: %w", err)
	}
	out := make([]Alert, 0, len(items))
	for _, it := range items {
		if it.Value.Alert != nil {
			out = append(out, it.Value.Alert.alert())
		}
	}
	return out, nil
}

// Alert returns one of a tenant's alerts.
func (e *Engine) Alert(tenantName, id string) (Alert, error) {
	cur, err := e.kept.Lookup(tenantName, alertKey(id))
	if errors.Is(err, state.ErrNotFound) || err == nil && (cur.Value.Alert == nil || cur.Value.Alert.ID != id) {
		return Alert{}, fmt.Errorf("%w: alert %q", ErrNotFound, id)
	}
	if err != nil {
		return Alert{}, fmt.Errorf("alerts: read: %w", err)
	}
	return cur.Value.Alert.alert(), nil
}

// Ack marks an alert as seen by a person. Acknowledging twice keeps the first.
// The alert's open key, if it holds one, is let go in the same write.
func (e *Engine) Ack(tenantName, id, by string, at time.Time) (Alert, error) {
	key := alertKey(id)
	var out Alert
	err := state.Retry(func() error {
		cur, err := e.kept.Lookup(tenantName, key)
		if errors.Is(err, state.ErrNotFound) || err == nil && (cur.Value.Alert == nil || cur.Value.Alert.ID != id) {
			return fmt.Errorf("%w: alert %q", ErrNotFound, id)
		}
		if err != nil {
			return err
		}
		a := *cur.Value.Alert
		if !a.Open() {
			out = a.alert()
			return nil
		}
		a.AckedBy, a.AckedAt = by, at
		b := e.kept.Batch()
		b.PutAt(tenantName, key, stored{Alert: &a}, cur.Version)
		if err := e.release(b, tenantName, a); err != nil {
			return err
		}
		if err := b.Commit(); err != nil {
			return err
		}
		out = a.alert()
		return nil
	})
	if err != nil {
		return Alert{}, wrap(err)
	}
	return out, nil
}

// release deletes, in b, the open key an alert holds, if it still names it.
func (e *Engine) release(b *state.Batch[stored], tenantName string, a storedAlert) error {
	if a.Scope == "" {
		return nil
	}
	o, err := e.kept.Lookup(tenantName, a.Scope)
	if errors.Is(err, state.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if o.Value.Open == a.ID {
		b.DeleteAt(tenantName, a.Scope, o.Version)
	}
	return nil
}

// isOpen reports whether an open key names an alert.
func (e *Engine) isOpen(tenantName, key string) (bool, error) {
	_, err := e.kept.Lookup(tenantName, key)
	if errors.Is(err, state.ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}

// raising is one change to a tenant that raises alerts: its rules record as
// read, and the alerts raised so far, written together by commit.
type raising struct {
	e       *Engine
	tenant  string
	t       stored
	version int64
	b       *state.Batch[stored]
	raised  []Alert
}

func (e *Engine) raising(tenantName string) (*raising, error) {
	t, version, err := e.rules(tenantName)
	if err != nil {
		return nil, err
	}
	t.Last = maps.Clone(t.Last)
	return &raising{e: e, tenant: tenantName, t: t, version: version, b: e.kept.Batch()}, nil
}

// cooling reports whether a rule raised an alert less than its cooldown
// before at.
func (w *raising) cooling(r Rule, at time.Time) bool {
	last, ok := w.t.Last[r.Name]
	return ok && r.CooldownSeconds > 0 && at.Before(last.Add(time.Duration(r.CooldownSeconds)*time.Second))
}

// fired raises an alert for a rule and starts its cooldown.
func (w *raising) fired(r Rule, a Alert) error {
	a.Rule, a.Kind, a.Severity, a.Notify = r.Name, r.Kind, r.Severity, slices.Clone(r.Notify)
	if w.t.Last == nil {
		w.t.Last = map[string]time.Time{}
	}
	w.t.Last[r.Name] = a.At
	scope := ""
	if watchesOpen(r.Kind) {
		scope = openKey(r.Name, r.Agent)
	}
	return w.add(a, scope)
}

// add numbers an alert, holds its open key when it has one, and drops the
// alert maxAlerts before it.
func (w *raising) add(a Alert, scope string) error {
	w.t.Next++
	a.ID = "al-" + strconv.Itoa(w.t.Next)
	w.b.Create(w.tenant, alertKey(a.ID), stored{Alert: &storedAlert{Alert: a, Notify: a.Notify, Scope: scope}})
	if scope != "" {
		w.b.Create(w.tenant, scope, stored{Open: a.ID})
	}
	w.raised = append(w.raised, a)
	if n := w.t.Next - maxAlerts; n > 0 {
		key := alertPrefix + state.Seq(int64(n))
		old, err := w.e.kept.Lookup(w.tenant, key)
		if errors.Is(err, state.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		w.b.DeleteAt(w.tenant, key, old.Version)
		if old.Value.Alert != nil && old.Value.Alert.Open() {
			return w.e.release(w.b, w.tenant, *old.Value.Alert)
		}
	}
	return nil
}

// commit writes what was raised and the rules record, at the version read.
func (w *raising) commit() error {
	if len(w.raised) == 0 {
		return nil
	}
	if w.version == 0 {
		w.b.Create(w.tenant, rulesKey, w.t)
	} else {
		w.b.PutAt(w.tenant, rulesKey, w.t, w.version)
	}
	return w.b.Commit()
}

// Recent sums a tenant's runs in a scope that started since a time. An empty
// agent or project means every agent or project.
type Recent func(tenant, agent, project string, since time.Time) runs.Window

// Evaluate checks a run that has just ended against its tenant's rules and
// returns the alerts it raised. When they cannot be written none is raised.
func (e *Engine) Evaluate(run runs.Run, recent Recent) ([]Alert, error) {
	var raised []Alert
	err := state.Retry(func() error {
		raised = nil
		w, err := e.raising(run.Tenant)
		if err != nil {
			return err
		}
		raise := func(r Rule, title, body string) error {
			return w.fired(r, Alert{
				Agent: run.Agent, Project: run.Project, RunID: run.ID, Title: title, Body: body, At: run.UpdatedAt,
			})
		}
		for _, r := range w.t.Rules {
			if !r.applies(run.Agent, run.Project) || w.cooling(r, run.UpdatedAt) {
				continue
			}
			if watchesOpen(r.Kind) {
				if recent == nil || r.Kind == FailureRate && run.Status != runs.Failed {
					continue
				}
				// One open alert per rule and scope is enough: a second
				// one while the first is unread says nothing new.
				open, err := e.isOpen(run.Tenant, openKey(r.Name, r.Agent))
				if err != nil {
					return err
				}
				if open {
					continue
				}
			}
			var err error
			switch r.Kind {
			case RunFailed:
				if run.Status == runs.Failed {
					err = raise(r, run.Agent+" failed", run.Error)
				}
			case SlowRun:
				took := run.UpdatedAt.Sub(run.CreatedAt)
				if took.Seconds() > r.Threshold {
					err = raise(r, fmt.Sprintf("%s took %s", run.Agent, took.Round(time.Millisecond)),
						fmt.Sprintf("The limit is %gs.", r.Threshold))
				}
			case FailureRate:
				window := time.Duration(r.WindowSeconds) * time.Second
				w := recent(run.Tenant, r.Agent, r.Project, run.UpdatedAt.Add(-window))
				if w.Total < r.MinRuns || w.Total == 0 {
					continue
				}
				if rate := 100 * float64(w.Failed) / float64(w.Total); rate >= r.Threshold {
					err = raise(r, fmt.Sprintf("%s: %.0f%% of runs are failing", r.scope(), rate),
						fmt.Sprintf("%d of %d runs failed in the last %s.", w.Failed, w.Total, window))
				}
			case Spend:
				window := time.Duration(r.WindowSeconds) * time.Second
				w := recent(run.Tenant, r.Agent, r.Project, run.UpdatedAt.Add(-window))
				if w.Spent >= r.ThresholdMicros {
					err = raise(r, fmt.Sprintf("%s: %s spent in the last %s", r.scope(), Dollars(w.Spent), window),
						fmt.Sprintf("%d runs cost %s. The rule fires at %s.", w.Total, Dollars(w.Spent), Dollars(r.ThresholdMicros)))
				}
			}
			if err != nil {
				return err
			}
		}
		if err := w.commit(); err != nil {
			return err
		}
		raised = w.raised
		return nil
	})
	if err != nil {
		return nil, wrap(err)
	}
	return raised, nil
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
func (e *Engine) EvaluateEval(tenantName string, run evals.Run) ([]Alert, error) {
	if run.Status == evals.Running {
		return nil, nil
	}
	var raised []Alert
	err := state.Retry(func() error {
		raised = nil
		w, err := e.raising(tenantName)
		if err != nil {
			return err
		}
		for _, r := range w.t.Rules {
			if r.Kind != EvalBelow {
				continue
			}
			agent := ""
			if run.Target == evals.Agent {
				agent = run.Subject
			}
			if !r.applies(agent, "") || w.cooling(r, run.EndedAt) {
				continue
			}
			// A run that could not be carried out scored nothing, which
			// is under any threshold.
			if run.Status != evals.Errored && run.PassRate >= r.Threshold {
				continue
			}
			if err := w.fired(r, Alert{
				Agent: agent, Suite: run.Suite, At: run.EndedAt,
				Title: fmt.Sprintf("Eval %s passed %.0f%% of its cases", run.Suite, run.PassRate),
				Body:  fmt.Sprintf("%d of %d cases passed. The rule expects at least %.0f%%.", run.Passed, run.Total, r.Threshold),
			}); err != nil {
				return err
			}
		}
		if err := w.commit(); err != nil {
			return err
		}
		raised = w.raised
		return nil
	})
	if err != nil {
		return nil, wrap(err)
	}
	return raised, nil
}

// Raise records an alert the platform itself decided on. key names what it is
// about, and while an alert with that key is open a second one is not raised:
// a limit that is refusing every run says so once, not once per run. The
// alert takes the key's open key in the write that raises it, so two
// processes raising the same key at once raise one alert.
func (e *Engine) Raise(tenantName, key string, a Alert) (Alert, bool, error) {
	scope := openKey(key, "")
	var out Alert
	var ok bool
	err := state.Retry(func() error {
		out, ok = Alert{}, false
		open, err := e.isOpen(tenantName, scope)
		if err != nil || open {
			return err
		}
		w, err := e.raising(tenantName)
		if err != nil {
			return err
		}
		a := a
		a.Rule = key
		if err := w.add(a, scope); err != nil {
			return err
		}
		if err := w.commit(); err != nil {
			return err
		}
		out, ok = w.raised[0], true
		return nil
	})
	if err != nil {
		return Alert{}, false, wrap(err)
	}
	return out, ok, nil
}
