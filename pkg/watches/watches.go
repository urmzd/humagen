// Package watches keeps what a person asked an agent to watch for: "tell me
// when the deploy is green". A watch names a condition in words, how to check
// it, how often, and until when. The platform checks it in the background,
// one agent run per check, and tells the person once it holds, or once the
// watch runs out.
//
// The package keeps the records and decides what a check's outcome does to
// a watch. It starts no run and sends nothing: internal/platform does both.
//
// It reads and writes the database per request (pkg/state, per request): a
// change reads the watch and writes it at the version it read, so of two
// processes starting the same check, one wins. Its records, in the namespace
// "watches":
//
//	<tenant>/active/<id>   a watch still watching
//	<tenant>/ended/<id>    a watch that ended, kept for Retention
//	""/tenant:<tenant>     the directory of tenants (state.Tenants)
//
// A tenant holds at most MaxActive watches still watching, and a person at
// most MaxPerPerson, so a sweep reads one page per tenant.
package watches

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
	ErrInvalid  = errors.New("watches: invalid")
	ErrNotFound = errors.New("watches: not found")
	ErrLimit    = errors.New("watches: limit reached")
	// ErrBusy reports a check that another process started first, or a
	// watch that changed while it was read.
	ErrBusy = errors.New("watches: busy")
)

// Bounds on a watch.
const (
	// MaxActive is the most watches a tenant holds still watching.
	MaxActive = 500
	// MaxPerPerson is the most watches one person holds still watching.
	MaxPerPerson = 20
	// MinEvery and MaxEvery bound how often a watch is checked.
	MinEvery = 5 * time.Minute
	MaxEvery = 24 * time.Hour
	// MaxFor is the longest a watch keeps watching.
	MaxFor = 7 * 24 * time.Hour
	// DefaultEvery and DefaultFor are used when a watch names neither.
	DefaultEvery = 15 * time.Minute
	DefaultFor   = 24 * time.Hour
	// MaxChecks bounds the checks of one watch, whatever its interval.
	MaxChecks = 200
	// MaxFailures is how many checks in a row may fail before the watch
	// ends as failed.
	MaxFailures = 3
	// MaxCondition, MaxHow and MaxResult bound the text a watch keeps.
	MaxCondition = 500
	MaxHow       = 2000
	MaxResult    = 4000
	// Retention is how long an ended watch is kept.
	Retention = 30 * 24 * time.Hour
)

// State is where a watch is.
type State string

const (
	// Watching is a watch still being checked.
	Watching State = "watching"
	// Met is a watch whose condition held: the person was told.
	Met State = "met"
	// Expired is a watch that ran out of time or checks first.
	Expired State = "expired"
	// Stopped is a watch a person or an agent stopped.
	Stopped State = "stopped"
	// Failed is a watch whose checks kept failing.
	Failed State = "failed"
)

// Watch is one thing a person asked to be told about.
type Watch struct {
	ID string `json:"id"`
	// Owner is the person told, and the one each check acts for.
	Owner string `json:"owner"`
	// Agent checks it, in the session that asked (Session), filed in
	// Project.
	Agent   string `json:"agent"`
	Session string `json:"session"`
	Project string `json:"project,omitempty"`
	// Condition is what to watch for; How is how to check it, in words.
	Condition string `json:"condition"`
	How       string `json:"how,omitempty"`
	// Every is how long after one check ends the next starts.
	Every time.Duration `json:"every"`
	// Until is when the watch expires unmet.
	Until time.Time `json:"until"`
	// Next is when the next check is due.
	Next time.Time `json:"next"`
	// Checks counts the checks started; Failures the checks in a row that
	// failed.
	Checks   int `json:"checks"`
	Failures int `json:"failures,omitempty"`
	// Running is the run of the check in flight, "" when none is.
	Running string `json:"running,omitempty"`
	// Thread is the conversation the checks are filed in, once made.
	Thread string `json:"thread,omitempty"`
	State  State  `json:"state"`
	// Result is what the person was told when the watch ended.
	Result    string    `json:"result,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	CreatedBy string    `json:"created_by,omitempty"`
	EndedAt   time.Time `json:"ended_at,omitzero"`
}

// Spec is what a caller asks for when it starts a watch.
type Spec struct {
	Owner, Agent, Session, Project string
	Condition, How                 string
	// Every and For default to DefaultEvery and DefaultFor when zero.
	Every, For time.Duration
	// CreatedBy is the run that started the watch.
	CreatedBy string
}

const (
	namespace = "watches"
	active    = "active/"
	ended     = "ended/"
)

// Store keeps watches.
type Store struct {
	st      state.Store
	tenants *state.Tenants
}

// Open returns the watches kept in st, which may be shared with other
// processes. A nil st keeps them in this process's memory.
func Open(st state.Store) (*Store, error) {
	if st == nil {
		st = state.NewMemory()
	}
	tenants, err := state.OpenTenants(st, namespace)
	if err != nil {
		return nil, fmt.Errorf("watches: tenants: %w", err)
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
	return "w_" + hex.EncodeToString(b)
}

// Create starts a watch at now. Its first check is due at once.
func (s *Store) Create(tenant string, sp Spec, now time.Time) (Watch, error) {
	if tenant == "" || sp.Owner == "" || sp.Agent == "" || sp.Session == "" {
		return Watch{}, fmt.Errorf("%w: a watch needs a tenant, a person, an agent and a session", ErrInvalid)
	}
	condition, err := clean(sp.Condition, MaxCondition)
	if err != nil {
		return Watch{}, err
	}
	if condition == "" {
		return Watch{}, fmt.Errorf("%w: say what to watch for", ErrInvalid)
	}
	how, err := clean(sp.How, MaxHow)
	if err != nil {
		return Watch{}, err
	}
	every, until := sp.Every, sp.For
	if every == 0 {
		every = DefaultEvery
	}
	if until == 0 {
		until = DefaultFor
	}
	if every < MinEvery || every > MaxEvery {
		return Watch{}, fmt.Errorf("%w: check every %s to %s", ErrInvalid, MinEvery, MaxEvery)
	}
	if until < every || until > MaxFor {
		return Watch{}, fmt.Errorf("%w: watch for at least one interval and at most %s", ErrInvalid, MaxFor)
	}
	all, err := s.Active(tenant)
	if err != nil {
		return Watch{}, err
	}
	mine := 0
	for _, w := range all {
		if w.Owner == sp.Owner {
			mine++
		}
	}
	if len(all) >= MaxActive || mine >= MaxPerPerson {
		return Watch{}, fmt.Errorf("%w: stop a watch before starting another", ErrLimit)
	}
	w := Watch{
		ID: newID(), Owner: sp.Owner, Agent: sp.Agent, Session: sp.Session, Project: sp.Project,
		Condition: condition, How: how, Every: every, Until: now.Add(until), Next: now,
		State: Watching, CreatedAt: now, CreatedBy: sp.CreatedBy,
	}
	if err := s.tenants.Mark(tenant); err != nil {
		return Watch{}, err
	}
	b, err := json.Marshal(w)
	if err != nil {
		return Watch{}, err
	}
	c, cancel := ctx()
	defer cancel()
	if err := s.st.Write(c, namespace, state.Create(tenant, active+w.ID, b)); err != nil {
		return Watch{}, err
	}
	return w, nil
}

// Get returns one watch, still watching or ended.
func (s *Store) Get(tenant, id string) (Watch, error) {
	w, _, err := s.lookup(tenant, active+id)
	if errors.Is(err, ErrNotFound) {
		w, _, err = s.lookup(tenant, ended+id)
	}
	return w, err
}

func (s *Store) lookup(tenant, key string) (Watch, int64, error) {
	c, cancel := ctx()
	defer cancel()
	rec, err := s.st.Lookup(c, namespace, tenant, key)
	if errors.Is(err, state.ErrNotFound) {
		return Watch{}, 0, ErrNotFound
	}
	if err != nil {
		return Watch{}, 0, err
	}
	var w Watch
	if err := json.Unmarshal(rec.Value, &w); err != nil {
		return Watch{}, 0, err
	}
	return w, rec.Version, nil
}

// Active returns a tenant's watches still watching: one page, as a tenant
// holds at most MaxActive.
func (s *Store) Active(tenant string) ([]Watch, error) {
	c, cancel := ctx()
	defer cancel()
	recs, _, err := s.st.List(c, namespace, tenant, state.Query{Prefix: active, Limit: MaxActive + 1})
	if err != nil {
		return nil, err
	}
	out := make([]Watch, 0, len(recs))
	for _, r := range recs {
		var w Watch
		if err := json.Unmarshal(r.Value, &w); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, nil
}

// Tenants calls fn for every tenant that has held a watch.
func (s *Store) Tenants(fn func(tenant string) error) error { return s.tenants.Each(fn) }

// update changes a watch still watching at the version it was read, and
// writes it back, or moves it to ended when change ends it.
func (s *Store) update(tenant, id string, change func(*Watch) error) (Watch, error) {
	w, version, err := s.lookup(tenant, active+id)
	if err != nil {
		return Watch{}, err
	}
	if err := change(&w); err != nil {
		return w, err
	}
	b, err := json.Marshal(w)
	if err != nil {
		return Watch{}, err
	}
	ops := []state.Op{state.PutAt(tenant, active+id, b, version)}
	if w.State != Watching {
		ops = []state.Op{state.DeleteAt(tenant, active+id, version), state.Put(tenant, ended+id, b)}
	}
	c, cancel := ctx()
	defer cancel()
	if err := s.st.Write(c, namespace, ops...); err != nil {
		if errors.Is(err, state.ErrConflict) {
			return Watch{}, ErrBusy
		}
		return Watch{}, err
	}
	return w, nil
}

// Due reports whether a watch's next check should start at now.
func (w Watch) Due(now time.Time) bool {
	return w.State == Watching && w.Running == "" && !now.Before(w.Next)
}

// Over reports whether a watch has no checks left at now.
func (w Watch) Over(now time.Time) bool {
	return !now.Before(w.Until) || w.Checks >= MaxChecks
}

// Begin claims a watch's next check for run. Of two processes beginning the
// same check, one gets ErrBusy.
func (s *Store) Begin(tenant, id, run string, now time.Time) (Watch, error) {
	return s.update(tenant, id, func(w *Watch) error {
		if !w.Due(now) || w.Over(now) {
			return ErrBusy
		}
		w.Running = run
		w.Checks++
		return nil
	})
}

// Abandon gives up a check that could not start, as a failed one.
func (s *Store) Abandon(tenant, id, run string, now time.Time) (Watch, error) {
	return s.Settle(tenant, id, run, Outcome{Failed: true}, now)
}

// SetThread records the conversation a watch's checks are filed in.
func (s *Store) SetThread(tenant, id, thread string) error {
	_, err := s.update(tenant, id, func(w *Watch) error {
		w.Thread = thread
		return nil
	})
	return err
}

// Outcome is how one check ended.
type Outcome struct {
	// Met reports that the condition held; Result is what to tell the
	// person.
	Met    bool
	Result string
	// Failed reports a check that did not finish.
	Failed bool
}

// Settle records how the check run ended at now and returns the watch as it
// now is. A watch ends when the condition held, when MaxFailures checks in a
// row failed, or when it has no checks left; otherwise its next check is due
// Every from now. A run that is not the watch's check in flight changes
// nothing and is ErrBusy.
func (s *Store) Settle(tenant, id, run string, o Outcome, now time.Time) (Watch, error) {
	return s.update(tenant, id, func(w *Watch) error {
		if w.Running == "" || w.Running != run {
			return ErrBusy
		}
		w.Running = ""
		switch {
		case o.Met:
			result, _ := clean(clip(o.Result, MaxResult), MaxResult)
			w.end(Met, result, now)
		case o.Failed:
			w.Failures++
			if w.Failures >= MaxFailures {
				w.end(Failed, "", now)
				return nil
			}
			fallthrough
		default:
			if !o.Failed {
				w.Failures = 0
			}
			w.Next = now.Add(w.Every)
			if w.Over(w.Next) {
				w.end(Expired, "", now)
			}
		}
		return nil
	})
}

// Expire ends a watch that has no checks left and none in flight.
func (s *Store) Expire(tenant, id string, now time.Time) (Watch, error) {
	return s.update(tenant, id, func(w *Watch) error {
		if w.Running != "" || !w.Over(now) {
			return ErrBusy
		}
		w.end(Expired, "", now)
		return nil
	})
}

// Stop ends a watch at a person's or an agent's word. A check in flight
// finishes, and what it finds is not sent.
func (s *Store) Stop(tenant, id string, now time.Time) (Watch, error) {
	return s.update(tenant, id, func(w *Watch) error {
		w.Running = ""
		w.end(Stopped, "", now)
		return nil
	})
}

func (w *Watch) end(st State, result string, now time.Time) {
	w.State, w.Result, w.EndedAt = st, result, now
}

// Prune removes up to one page of a tenant's ended watches that ended before
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
		var w Watch
		if json.Unmarshal(r.Value, &w) == nil && w.EndedAt.Before(cut) {
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
