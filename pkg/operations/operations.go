// Package operations records durable agent controls, tightening action policies,
// assignments and handoff contracts. It never starts a run or grants a tool.
//
// It reads and writes the database per request (pkg/state, per request): it
// holds no record in memory, changes each record only at the version it read,
// so any number of processes on one database agree, and it does not need
// pkg/state's single-writer lock.
package operations

import (
	"errors"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/urmzd/humagen/internal/legacy"
)

var (
	ErrInvalid    = errors.New("operations: invalid")
	ErrConflict   = errors.New("operations: version conflict")
	ErrNotFound   = errors.New("operations: not found")
	ErrPaused     = errors.New("operations: agent is paused")
	ErrDenied     = errors.New("operations: action denied")
	ErrEscalation = errors.New("operations: handoff needs a person: cycle or more than three hops")
)

// MetaForceAsk is an internal tightening marker, preserved in durable run tags.
const MetaForceAsk = "humagen.operations.ask"

// MetaTools is a JSON array of exact tool names restricting a pinned skill.
// An absent key preserves grants; an explicit empty array allows no actions.
const MetaTools = "humagen.operations.tools"

const MaxRecords = 10000
const MaxResults = 200

type Level string

const (
	Unspecified Level = ""
	Allow       Level = "allow"
	Ask         Level = "ask"
	Never       Level = "never"
)

type Category string

const (
	ReadContext      Category = "read_context"
	WriteFiles       Category = "write_files"
	PublishArtifacts Category = "publish_artifacts"
	ExternalMessages Category = "external_messages"
	CallAgents       Category = "call_agents"
	Other            Category = "other"
)

var Categories = []Category{ReadContext, WriteFiles, PublishArtifacts, ExternalMessages, CallAgents, Other}

type Scope struct {
	Kind string `json:"kind"`
	Name string `json:"name,omitempty"`
}

func (s Scope) key() string { return s.Kind + ":" + s.Name }
func (s Scope) valid() bool {
	switch s.Kind {
	case "workspace":
		return s.Name == ""
	case "project", "agent", "personal":
		return identifier(s.Name)
	}
	return false
}

type Control struct {
	Agent                       string    `json:"agent,omitempty"`
	Paused                      bool      `json:"paused"`
	Manager                     string    `json:"manager,omitempty"`
	StopRules                   string    `json:"stop_rules,omitempty"`
	ReminderAfterSeconds        *int      `json:"reminder_after_seconds,omitempty"`
	BlockedReminderAfterSeconds *int      `json:"blocked_reminder_after_seconds,omitempty"`
	Version                     int       `json:"version"`
	UpdatedBy                   string    `json:"updated_by"`
	UpdatedAt                   time.Time `json:"updated_at"`
}
type Policy struct {
	Scope     Scope              `json:"scope"`
	Rules     map[Category]Level `json:"rules"`
	Version   int                `json:"version"`
	UpdatedBy string             `json:"updated_by"`
	UpdatedAt time.Time          `json:"updated_at"`
}
type WorkState string

const (
	Requested   WorkState = "requested"
	Accepted    WorkState = "accepted"
	InProgress  WorkState = "in_progress"
	Watching    WorkState = "watching"
	Result      WorkState = "result"
	Blocked     WorkState = "blocked"
	NeedsPerson WorkState = "needs_person"
	Cancelled   WorkState = "cancelled"
)

type Assignment struct {
	ID       string `json:"id"`
	Agent    string `json:"agent"`
	Project  string `json:"project"`
	Thread   string `json:"thread,omitempty"`
	Owner    string `json:"owner"`
	Goal     string `json:"goal"`
	DoneWhen string `json:"done_when"`
	// DoneChecks are what the agent's answer must pass before the assignment
	// settles as a result. A completed run whose answer fails one is
	// continued in the same conversation, at most MaxContinue times.
	DoneChecks []Check `json:"done_checks,omitempty"`
	// MaxContinue bounds those continuations. Zero with checks is
	// DefaultContinue.
	MaxContinue int `json:"max_continue,omitempty"`
	// Continued counts the continuations of the work in progress. The
	// server keeps it.
	Continued      int       `json:"continued,omitempty"`
	DueAt          time.Time `json:"due_at,omitempty"`
	DeadlineMissed bool      `json:"deadline_missed,omitempty"`
	Schedule       string    `json:"schedule,omitempty"`
	Cron           string    `json:"cron,omitempty"`
	Trigger        string    `json:"trigger,omitempty"`
	Source         string    `json:"source,omitempty"`
	SourceVersion  uint64    `json:"source_version,omitempty"`
	TagID          string    `json:"tag_id,omitempty"`
	State          WorkState `json:"state"`
	Blocker        string    `json:"blocker,omitempty"`
	RunID          string    `json:"run_id,omitempty"`
	RunStatus      string    `json:"run_status,omitempty"`
	Version        int       `json:"version"`
	UpdatedBy      string    `json:"updated_by"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// The kinds of check an answer is held to. They are the answer checks of an
// eval (pkg/evals), and as there nothing here asks a model.
const (
	Contains    = "contains"
	NotContains = "not_contains"
	Matches     = "matches"
	Equals      = "equals"
)

// Bounds on an assignment's checks.
const (
	MaxDoneChecks   = 8
	MaxCheckValue   = 500
	MaxContinues    = 10
	DefaultContinue = 3
)

// Check is one thing an answer must satisfy: it contains or does not
// contain Value, equals it, or matches it as a regular expression.
type Check struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

func (c Check) valid() bool {
	if c.Value == "" || len(c.Value) > MaxCheckValue || !utf8.ValidString(c.Value) {
		return false
	}
	switch c.Kind {
	case Contains, NotContains, Equals:
		return true
	case Matches:
		_, err := regexp.Compile(c.Value)
		return err == nil
	}
	return false
}

// String says what the check asks for, as a person or an agent reads it.
func (c Check) String() string {
	switch c.Kind {
	case Contains:
		return "the answer contains " + strconv.Quote(c.Value)
	case NotContains:
		return "the answer does not contain " + strconv.Quote(c.Value)
	case Equals:
		return "the answer is exactly " + strconv.Quote(c.Value)
	}
	return "the answer matches " + strconv.Quote(c.Value)
}

func (c Check) passes(answer string) bool {
	switch c.Kind {
	case Contains:
		return strings.Contains(answer, c.Value)
	case NotContains:
		return !strings.Contains(answer, c.Value)
	case Equals:
		return strings.TrimSpace(answer) == strings.TrimSpace(c.Value)
	case Matches:
		re, err := regexp.Compile(c.Value)
		return err == nil && re.MatchString(answer)
	}
	return false
}

// Unmet are the assignment's checks that answer does not pass, in order.
func (a Assignment) Unmet(answer string) []Check {
	var out []Check
	for _, c := range a.DoneChecks {
		if !c.passes(answer) {
			out = append(out, c)
		}
	}
	return out
}

// Continues is how many times the assignment's work may be continued.
func (a Assignment) Continues() int {
	if len(a.DoneChecks) == 0 {
		return 0
	}
	if a.MaxContinue == 0 {
		return DefaultContinue
	}
	return a.MaxContinue
}

type Input struct {
	Type      string `json:"type"`
	Reference string `json:"reference"`
}
type Handoff struct {
	Requester      string    `json:"-"`
	ID             string    `json:"id"`
	From           string    `json:"from"`
	To             string    `json:"to"`
	Project        string    `json:"project"`
	Thread         string    `json:"thread,omitempty"`
	Owner          string    `json:"owner,omitempty"`
	Goal           string    `json:"goal"`
	DoneWhen       string    `json:"done_when"`
	Inputs         []Input   `json:"inputs"`
	Chain          []string  `json:"chain"`
	DueAt          time.Time `json:"due_at,omitempty"`
	DeadlineMissed bool      `json:"deadline_missed,omitempty"`
	State          WorkState `json:"state"`
	Blocker        string    `json:"blocker,omitempty"`
	RunID          string    `json:"run_id,omitempty"`
	Version        int       `json:"version"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}
type record struct {
	HandoffRequester string         `json:"handoff_requester,omitempty"`
	Invocation       *legacy.Stored `json:"invocation,omitempty"`
	Control          *Control       `json:"control,omitempty"`
	Policy           *Policy        `json:"policy,omitempty"`
	Assignment       *Assignment    `json:"assignment,omitempty"`
	Handoff          *Handoff       `json:"handoff,omitempty"`
}

func clone(r record) record {
	if r.Invocation != nil {
		r.Invocation = &legacy.Stored{Event: cloneInvocation(r.Invocation.Event)}
	}
	if r.Control != nil {
		v := *r.Control
		if v.ReminderAfterSeconds != nil {
			n := *v.ReminderAfterSeconds
			v.ReminderAfterSeconds = &n
		}
		if v.BlockedReminderAfterSeconds != nil {
			n := *v.BlockedReminderAfterSeconds
			v.BlockedReminderAfterSeconds = &n
		}
		r.Control = &v
	}
	if r.Policy != nil {
		v := *r.Policy
		v.Rules = maps.Clone(v.Rules)
		if v.Rules == nil {
			v.Rules = map[Category]Level{}
		}
		r.Policy = &v
	}
	if r.Assignment != nil {
		v := *r.Assignment
		v.DoneChecks = slices.Clone(v.DoneChecks)
		r.Assignment = &v
	}
	if r.Handoff != nil {
		v := *r.Handoff
		v.Inputs = slices.Clone(v.Inputs)
		v.Chain = slices.Clone(v.Chain)
		r.Handoff = &v
	}
	return r
}

func identifier(v string) bool {
	return strings.TrimSpace(v) != "" && len(v) <= 256 && !strings.ContainsAny(v, "\x00\r\n")
}

func text(v string, max int) bool {
	return strings.TrimSpace(v) != "" && len(v) <= max && !strings.ContainsRune(v, 0)
}

func validReminder(seconds *int) bool { return seconds == nil || (*seconds >= 0 && *seconds <= 604800) }

func validPolicy(p Policy) bool {
	if !p.Scope.valid() || len(p.Rules) > len(Categories) {
		return false
	}
	for c, l := range p.Rules {
		if !slices.Contains(Categories, c) || (l != Allow && l != Ask && l != Never) {
			return false
		}
	}
	return true
}

func rank(l Level) int {
	switch l {
	case Allow:
		return 1
	case Ask:
		return 2
	case Never:
		return 3
	}
	return 0
}

func validState(s WorkState) bool {
	return slices.Contains([]WorkState{Requested, Accepted, InProgress, Watching, Result, Blocked, NeedsPerson, Cancelled}, s)
}

func transition(a, b WorkState) bool {
	if a == b {
		return true
	}
	switch a {
	case Requested:
		return b == Accepted || b == Watching || b == InProgress || b == Cancelled || b == NeedsPerson
	case Accepted, Watching, Blocked, NeedsPerson:
		return b == InProgress || b == Watching || b == Cancelled || b == Accepted || b == NeedsPerson || b == Blocked || b == Result
	case InProgress:
		return b == Result || b == Blocked || b == NeedsPerson || b == Watching
	case Result:
		return b == Accepted || b == Watching
	}
	return false
}

func validAssignment(a Assignment) bool {
	if a.Trigger != "" && a.Trigger != ManualTrigger && a.Trigger != CronTrigger && a.Trigger != SourceTrigger {
		return false
	}
	if a.Trigger == SourceTrigger && (!identifier(a.Source) || a.Cron != "") {
		return false
	}
	if a.Trigger != SourceTrigger && a.Source != "" {
		return false
	}
	if a.Trigger == ManualTrigger && a.Cron != "" {
		return false
	}
	if a.Trigger == CronTrigger && a.Cron == "" {
		return false
	}
	if len(a.DoneChecks) > MaxDoneChecks || a.MaxContinue < 0 || a.MaxContinue > MaxContinues || a.Continued < 0 {
		return false
	}
	// A clock or a source starts its own runs. Continuing is for work a
	// person starts.
	if len(a.DoneChecks) > 0 && a.Trigger != ManualTrigger && a.Trigger != "" {
		return false
	}
	if a.MaxContinue > 0 && len(a.DoneChecks) == 0 {
		return false
	}
	for _, c := range a.DoneChecks {
		if !c.valid() {
			return false
		}
	}
	return identifier(a.ID) && identifier(a.Agent) && identifier(a.Owner) && len(a.Project) <= 256 && len(a.Thread) <= 256 && text(a.Goal, 8000) && text(a.DoneWhen, 4000) && validState(a.State) && len(a.Blocker) <= 4000 && len(a.Cron) <= 128 && (a.State != Blocked || text(a.Blocker, 4000))
}
