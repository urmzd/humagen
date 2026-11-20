// Package background keeps the work an agent does for a person while nobody
// watches: a loop that runs one agent turn at a time until a condition holds,
// then tells the person. There are three kinds.
//
//   - A watch checks a condition every interval ("tell me when the deploy is
//     green"), in a private session of its own.
//   - A goal keeps working toward an end in the session that set it, one turn
//     after another, until the agent says it is met.
//   - A task is one piece of work run in a background session of its own (a
//     long command, a search), so the session that started it stays free.
//
// The package keeps the records and decides what each run's outcome does to
// its task. It starts no run and sends nothing: internal/platform does both.
//
// It reads and writes the database per request (pkg/state, per request): a
// change reads the task and writes it at the version it read, so of two
// processes starting the same run, one wins. Its records, in the namespace
// "background":
//
//	<tenant>/active/<id>   a task still running
//	<tenant>/ended/<id>    a task that ended, kept for Retention
//	<tenant>/run/<run>     the task whose run is in flight
//	""/tenant:<tenant>     the directory of tenants (state.Tenants)
//
// A tenant holds at most MaxActive tasks still running, and a person at most
// MaxPerPerson, so a sweep reads one page per tenant.
package background

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/urmzd/humagen/pkg/state"
)

// Errors a caller can match.
var (
	ErrInvalid  = errors.New("background: invalid")
	ErrNotFound = errors.New("background: not found")
	ErrLimit    = errors.New("background: limit reached")
	// ErrBusy reports a run that another process started first, or a task
	// that changed while it was read.
	ErrBusy = errors.New("background: busy")
)

// Kind is what a task does on each run.
type Kind string

const (
	Watch Kind = "watch"
	Goal  Kind = "goal"
	Task  Kind = "task"
)

// Bounds on every task.
const (
	// MaxActive is the most tasks a tenant holds still running.
	MaxActive = 500
	// MaxPerPerson is the most tasks one person holds still running.
	MaxPerPerson = 20
	// MaxFailures is how many runs in a row may fail before a watch or a
	// goal ends as failed. A task's one run failing ends it.
	MaxFailures = 3
	// MaxText bounds what a task is asked; MaxHow how to check it;
	// MaxResult what the person is told.
	MaxText   = 2000
	MaxHow    = 2000
	MaxResult = 4000
	// Retention is how long an ended task is kept.
	Retention = 30 * 24 * time.Hour
)

// Bounds on a watch.
const (
	MinEvery     = 5 * time.Minute
	MaxEvery     = 24 * time.Hour
	MaxWatchFor  = 7 * 24 * time.Hour
	DefaultEvery = 15 * time.Minute
	DefaultWatch = 24 * time.Hour
	MaxChecks    = 200
)

// Bounds on a goal.
const (
	MaxGoalFor   = 24 * time.Hour
	DefaultGoal  = 4 * time.Hour
	MaxTurns     = 25
	DefaultTurns = 10
)

// TaskFor is how long a task's one run may take before it counts as failed.
const TaskFor = 24 * time.Hour

// State is where a task is.
type State string

const (
	// Active is a task still running.
	Active State = "active"
	// Done is a task whose condition held, or whose work finished: the
	// person was told.
	Done State = "done"
	// Expired is a task that ran out of time or runs first.
	Expired State = "expired"
	// Stopped is a task a person or an agent stopped.
	Stopped State = "stopped"
	// Failed is a task whose runs kept failing, or a goal the agent said
	// it could not reach.
	Failed State = "failed"
)

// Item is one piece of background work.
type Item struct {
	ID   string `json:"id"`
	Kind Kind   `json:"kind"`
	// Owner is the person told, and the one each run acts for.
	Owner string `json:"owner"`
	// Agent does the work, for the session that asked (Session), filed in
	// Project.
	Agent   string `json:"agent"`
	Session string `json:"session"`
	Project string `json:"project,omitempty"`
	// Text is what to watch for, the goal, or the task's ask; How is how to
	// check a watch, or when a goal is met.
	Text string `json:"text"`
	How  string `json:"how,omitempty"`
	// Every is how long after one run ends the next starts: zero for a
	// goal, whose next turn starts at once.
	Every time.Duration `json:"every,omitempty"`
	// Until is when the task expires; Limit the most runs it makes.
	Until time.Time `json:"until"`
	Limit int       `json:"limit"`
	// Next is when the next run is due.
	Next time.Time `json:"next"`
	// Runs counts the runs started; Failures the runs in a row that failed.
	Runs     int `json:"runs"`
	Failures int `json:"failures,omitempty"`
	// Running is the run in flight, "" when none is; Last the run before.
	Running string `json:"running,omitempty"`
	Last    string `json:"last,omitempty"`
	// Thread is the conversation the runs are in.
	Thread string `json:"thread,omitempty"`
	State  State  `json:"state"`
	// Result is what the last run said, and, once ended, what the person
	// was told.
	Result    string    `json:"result,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	CreatedBy string    `json:"created_by,omitempty"`
	EndedAt   time.Time `json:"ended_at,omitzero"`
}

// Spec is what a caller asks for when it starts a task.
type Spec struct {
	Kind                           Kind
	Owner, Agent, Session, Project string
	Text, How                      string
	// Every and For take the kind's defaults when zero. Turns bounds a
	// goal's turns, DefaultTurns when zero.
	Every, For time.Duration
	Turns      int
	// CreatedBy is the run that started the task. A goal counts it as its
	// first turn: its next turn starts once that run ends.
	CreatedBy string
	// Thread is where a goal's turns run: its session. A watch's and a
	// task's are made for them on their first run.
	Thread string
}

const (
	namespace = "background"
	active    = "active/"
	ended     = "ended/"
	running   = "run/"
)

// Store keeps background tasks.
type Store struct {
	st      state.Store
	tenants *state.Tenants
}

// Open returns the tasks kept in st, which may be shared with other
// processes. A nil st keeps them in this process's memory.
func Open(st state.Store) (*Store, error) {
	if st == nil {
		st = state.NewMemory()
	}
	tenants, err := state.OpenTenants(st, namespace)
	if err != nil {
		return nil, fmt.Errorf("background: tenants: %w", err)
	}
	return &Store{st: st, tenants: tenants}, nil
}

func ctx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), state.Timeout)
}

func clean(s string, limit int) (string, error) {
	s = strings.TrimSpace(s)
	if strings.ContainsRune(s, 0) || !utf8.ValidString(s) {
		return "", fmt.Errorf("%w: text must be valid UTF-8", ErrInvalid)
	}
	if utf8.RuneCountInString(s) > limit {
		return "", fmt.Errorf("%w: text is longer than %d characters", ErrInvalid, limit)
	}
	return s, nil
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "bg_" + hex.EncodeToString(b)
}

// shape checks a spec's timing for its kind, and returns its interval, how
// long it runs and its most runs.
func shape(sp Spec) (every, span time.Duration, limit int, err error) {
	every, span = sp.Every, sp.For
	switch sp.Kind {
	case Watch:
		if every == 0 {
			every = DefaultEvery
		}
		if span == 0 {
			span = DefaultWatch
		}
		if every < MinEvery || every > MaxEvery {
			return 0, 0, 0, fmt.Errorf("%w: check every %s to %s", ErrInvalid, MinEvery, MaxEvery)
		}
		if span < every || span > MaxWatchFor {
			return 0, 0, 0, fmt.Errorf("%w: watch for at least one interval and at most %s", ErrInvalid, MaxWatchFor)
		}
		return every, span, MaxChecks, nil
	case Goal:
		if every != 0 {
			return 0, 0, 0, fmt.Errorf("%w: a goal's next turn starts when the last ends", ErrInvalid)
		}
		if span == 0 {
			span = DefaultGoal
		}
		if span < 0 || span > MaxGoalFor {
			return 0, 0, 0, fmt.Errorf("%w: work toward a goal for at most %s", ErrInvalid, MaxGoalFor)
		}
		limit = sp.Turns
		if limit == 0 {
			limit = DefaultTurns
		}
		if limit < 1 || limit > MaxTurns {
			return 0, 0, 0, fmt.Errorf("%w: a goal takes 1 to %d turns", ErrInvalid, MaxTurns)
		}
		return 0, span, limit, nil
	case Task:
		if every != 0 || span != 0 {
			return 0, 0, 0, fmt.Errorf("%w: a task runs once", ErrInvalid)
		}
		return 0, TaskFor, 1, nil
	}
	return 0, 0, 0, fmt.Errorf("%w: no kind %q", ErrInvalid, sp.Kind)
}

// Create starts a task at now. A watch's or a task's first run is due at
// once; a goal's once the run that set it ends.
func (s *Store) Create(tenant string, sp Spec, now time.Time) (Item, error) {
	if tenant == "" || sp.Owner == "" || sp.Agent == "" || sp.Session == "" {
		return Item{}, fmt.Errorf("%w: a task needs a tenant, a person, an agent and a session", ErrInvalid)
	}
	every, span, limit, err := shape(sp)
	if err != nil {
		return Item{}, err
	}
	text, err := clean(sp.Text, MaxText)
	if err != nil {
		return Item{}, err
	}
	if text == "" {
		return Item{}, fmt.Errorf("%w: say what to do", ErrInvalid)
	}
	how, err := clean(sp.How, MaxHow)
	if err != nil {
		return Item{}, err
	}
	if sp.Kind == Goal && (sp.CreatedBy == "" || sp.Thread == "") {
		return Item{}, fmt.Errorf("%w: a goal is set by a run in a session", ErrInvalid)
	}
	all, err := s.Active(tenant)
	if err != nil {
		return Item{}, err
	}
	mine := 0
	for _, it := range all {
		if it.Owner == sp.Owner {
			mine++
		}
	}
	if len(all) >= MaxActive || mine >= MaxPerPerson {
		return Item{}, fmt.Errorf("%w: stop a background task before starting another", ErrLimit)
	}
	it := Item{
		ID: newID(), Kind: sp.Kind, Owner: sp.Owner, Agent: sp.Agent, Session: sp.Session, Project: sp.Project,
		Text: text, How: how, Every: every, Until: now.Add(span), Limit: limit, Next: now,
		State: Active, CreatedAt: now, CreatedBy: sp.CreatedBy,
	}
	ops := []state.Op{}
	if sp.Kind == Goal {
		// The turn that set the goal is its first: the next starts when
		// it ends, in the same session.
		it.Thread, it.Running, it.Runs = sp.Thread, sp.CreatedBy, 1
		ops = append(ops, state.Put(tenant, running+it.Running, []byte(it.ID)))
	}
	if err := s.tenants.Mark(tenant); err != nil {
		return Item{}, err
	}
	b, err := json.Marshal(it)
	if err != nil {
		return Item{}, err
	}
	c, cancel := ctx()
	defer cancel()
	if err := s.st.Write(c, namespace, append(ops, state.Create(tenant, active+it.ID, b))...); err != nil {
		return Item{}, err
	}
	return it, nil
}

// Get returns one task, still running or ended.
func (s *Store) Get(tenant, id string) (Item, error) {
	it, _, err := s.lookup(tenant, active+id)
	if errors.Is(err, ErrNotFound) {
		it, _, err = s.lookup(tenant, ended+id)
	}
	return it, err
}

// ByRun returns the task whose run in flight is run.
func (s *Store) ByRun(tenant, run string) (Item, error) {
	c, cancel := ctx()
	defer cancel()
	id, err := s.st.Get(c, namespace, tenant, running+run)
	if errors.Is(err, state.ErrNotFound) {
		return Item{}, ErrNotFound
	}
	if err != nil {
		return Item{}, err
	}
	return s.Get(tenant, string(id))
}

func (s *Store) lookup(tenant, key string) (Item, int64, error) {
	c, cancel := ctx()
	defer cancel()
	rec, err := s.st.Lookup(c, namespace, tenant, key)
	if errors.Is(err, state.ErrNotFound) {
		return Item{}, 0, ErrNotFound
	}
	if err != nil {
		return Item{}, 0, err
	}
	var it Item
	if err := json.Unmarshal(rec.Value, &it); err != nil {
		return Item{}, 0, err
	}
	return it, rec.Version, nil
}

// Active returns a tenant's tasks still running: one page, as a tenant holds
// at most MaxActive.
func (s *Store) Active(tenant string) ([]Item, error) {
	return s.page(tenant, active, MaxActive+1)
}

// Ended returns up to one page of a tenant's ended tasks, in no order.
func (s *Store) Ended(tenant string) ([]Item, error) {
	return s.page(tenant, ended, state.MaxList)
}

func (s *Store) page(tenant, prefix string, limit int) ([]Item, error) {
	c, cancel := ctx()
	defer cancel()
	recs, _, err := s.st.List(c, namespace, tenant, state.Query{Prefix: prefix, Limit: limit})
	if err != nil {
		return nil, err
	}
	out := make([]Item, 0, len(recs))
	for _, r := range recs {
		var it Item
		if err := json.Unmarshal(r.Value, &it); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, nil
}

// Tenants calls fn for every tenant that has held a task.
func (s *Store) Tenants(fn func(tenant string) error) error { return s.tenants.Each(fn) }

// update changes a task still running at the version it was read, and writes
// it back, or moves it to ended when change ends it. The index of its run in
// flight follows Running.
func (s *Store) update(tenant, id string, change func(*Item) error) (Item, error) {
	it, version, err := s.lookup(tenant, active+id)
	if err != nil {
		return Item{}, err
	}
	was := it.Running
	if err := change(&it); err != nil {
		return it, err
	}
	b, err := json.Marshal(it)
	if err != nil {
		return Item{}, err
	}
	ops := []state.Op{state.PutAt(tenant, active+id, b, version)}
	if it.State != Active {
		ops = []state.Op{state.DeleteAt(tenant, active+id, version), state.Put(tenant, ended+id, b)}
	}
	if was != it.Running {
		if was != "" {
			ops = append(ops, state.Delete(tenant, running+was))
		}
		if it.Running != "" {
			ops = append(ops, state.Put(tenant, running+it.Running, []byte(id)))
		}
	}
	c, cancel := ctx()
	defer cancel()
	if err := s.st.Write(c, namespace, ops...); err != nil {
		if errors.Is(err, state.ErrConflict) {
			return Item{}, ErrBusy
		}
		return Item{}, err
	}
	return it, nil
}

// Due reports whether a task's next run should start at now.
func (it Item) Due(now time.Time) bool {
	return it.State == Active && it.Running == "" && !now.Before(it.Next)
}

// Over reports whether a task has no runs left at now.
func (it Item) Over(now time.Time) bool {
	return !now.Before(it.Until) || it.Runs >= it.Limit
}

// Begin claims a task's next run as run. Of two processes beginning the same
// run, one gets ErrBusy.
func (s *Store) Begin(tenant, id, run string, now time.Time) (Item, error) {
	return s.update(tenant, id, func(it *Item) error {
		if !it.Due(now) || it.Over(now) {
			return ErrBusy
		}
		it.Running = run
		it.Runs++
		return nil
	})
}

// Abandon gives up a run that could not start, as a failed one.
func (s *Store) Abandon(tenant, id, run string, now time.Time) (Item, error) {
	return s.Settle(tenant, id, run, Outcome{Failed: true}, now)
}

// SetThread records the conversation a task's runs are in.
func (s *Store) SetThread(tenant, id, thread string) error {
	_, err := s.update(tenant, id, func(it *Item) error {
		it.Thread = thread
		return nil
	})
	return err
}

// Outcome is how one run ended.
type Outcome struct {
	// Done reports that the condition held, or the work is finished.
	Done bool
	// Blocked reports a goal the agent said it cannot reach.
	Blocked bool
	// Failed reports a run that did not finish.
	Failed bool
	// Result is what the run said.
	Result string
}

// Settle records how the run in flight ended at now and returns the task as
// it now is. A task ends when it is done or blocked, when its runs failed
// MaxFailures times in a row (a task's once), or when it has no runs left;
// otherwise its next run is due Every from now. A run that is not the task's
// run in flight changes nothing and is ErrBusy.
func (s *Store) Settle(tenant, id, run string, o Outcome, now time.Time) (Item, error) {
	return s.update(tenant, id, func(it *Item) error {
		if it.Running == "" || it.Running != run {
			return ErrBusy
		}
		it.Running, it.Last = "", run
		if result, err := clean(clip(o.Result, MaxResult), MaxResult); err == nil && result != "" {
			it.Result = result
		}
		switch {
		case o.Done:
			it.end(Done, now)
		case o.Blocked:
			it.end(Failed, now)
		case o.Failed:
			it.Failures++
			if it.Failures >= MaxFailures || it.Kind == Task {
				it.end(Failed, now)
				return nil
			}
			fallthrough
		default:
			if !o.Failed {
				it.Failures = 0
			}
			it.Next = now.Add(it.Every)
			if it.Over(it.Next) {
				it.end(Expired, now)
			}
		}
		return nil
	})
}

// Expire ends a task that has no runs left and none in flight.
func (s *Store) Expire(tenant, id string, now time.Time) (Item, error) {
	return s.update(tenant, id, func(it *Item) error {
		if it.Running != "" || !it.Over(now) {
			return ErrBusy
		}
		it.end(Expired, now)
		return nil
	})
}

// Stop ends a task at a person's or an agent's word. A run in flight
// finishes, and what it finds is not sent.
func (s *Store) Stop(tenant, id string, now time.Time) (Item, error) {
	return s.update(tenant, id, func(it *Item) error {
		it.end(Stopped, now)
		return nil
	})
}

func (it *Item) end(st State, now time.Time) {
	it.State, it.EndedAt, it.Running = st, now, ""
}

// Prune removes up to one page of a tenant's ended tasks that ended before
// cut, and reports how many it removed.
func (s *Store) Prune(tenant string, cut time.Time) (int, error) {
	c, cancel := ctx()
	defer cancel()
	recs, _, err := s.st.List(c, namespace, tenant, state.Query{Prefix: ended, Limit: state.MaxList})
	if err != nil {
		return 0, err
	}
	var ops []state.Op
	for _, r := range recs {
		var it Item
		if json.Unmarshal(r.Value, &it) == nil && it.EndedAt.Before(cut) {
			ops = append(ops, state.DeleteAt(tenant, r.Key, r.Version))
		}
	}
	if len(ops) == 0 {
		return 0, nil
	}
	if err := s.st.Write(c, namespace, ops...); err != nil {
		return 0, err
	}
	return len(ops), nil
}

// clip cuts s to at most n runes.
func clip(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}
