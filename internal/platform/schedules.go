package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"strings"
	"sync"
	"time"

	"github.com/urmzd/humagen/fiber/pkg/connectors/cron"

	"github.com/urmzd/humagen/internal/legacy"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/state"
)

// Schedule errors.
var (
	ErrScheduleExists   = errors.New("platform: schedule already exists")
	ErrScheduleNotFound = errors.New("platform: schedule not found")
	// ErrScheduleManaged reports a change through the API to a schedule the
	// platform file declares. It can be paused, resumed and run, not
	// changed or deleted.
	ErrScheduleManaged = errors.New("platform: the platform file manages this schedule")
)

// ManagedFile marks a schedule the platform file declares.
const ManagedFile = "file"

// MaxScheduleDescription bounds what a schedule says about itself.
const MaxScheduleDescription = 500

// ErrScheduleInvalid reports a schedule the platform will not keep, for a
// reason of its own rather than the cron connector's.
var ErrScheduleInvalid = fmt.Errorf("%w: invalid", cron.ErrSchedule)

// About is what the platform keeps about a schedule beside what the cron
// connector runs. None of it reaches the agent.
type About struct {
	Description string
	UpdatedBy   string
	UpdatedAt   time.Time
	// WrittenBy is who wrote what an edited schedule runs, its text, the
	// agent it asks and the project it is filed in, whose instructions and
	// notes go ahead of the text, or "" for nobody: an edit that changes
	// none of them keeps it, and one that changes the agent or the project
	// but not the text keeps it only when made by that person. Set by
	// Replace. A schedule nobody has edited is its creator's, and one edited
	// before this was kept is nobody's.
	WrittenBy string
	// CreatedAt is when the schedule was created. A schedule kept before it
	// was recorded reads as zero.
	CreatedAt time.Time
	// Managed is ManagedFile while the platform file declares the schedule.
	Managed string `json:",omitempty"`
}

// authorOf is who wrote what a schedule runs (About.WrittenBy).
func authorOf(sc cron.Schedule, about About) string {
	if about.UpdatedBy == "" {
		return sc.Context["created_by"]
	}
	return about.WrittenBy
}

func (a About) validate() error {
	if len(a.Description) > MaxScheduleDescription {
		return fmt.Errorf("%w: a description is at most %d characters", ErrScheduleInvalid, MaxScheduleDescription)
	}
	return nil
}

// Schedules is the store the cron connector reads. It reads and writes the
// database per request (pkg/state, per request): it holds no schedule in
// memory, so every process on one database sees the same schedules, and it
// does not need pkg/state's single-writer lock.
//
// Its records, in the namespace "schedules":
//
//	<tenant>/<name>          a schedule (storedSchedule)
//	<tenant>/#count          how many schedules the tenant holds
//	""/name:<name>           which tenant holds a name (nameClaim)
//	""/tenant:<tenant>       the directory of tenants (state.Tenants)
//	""/converted:v1          that names held before claims were kept are claimed
//
// A schedule's name is unique across tenants, because it is part of the id
// of every run it raises. A tenant claims a name before it writes the
// schedule, and gives the claim up after removing it.
type Schedules struct {
	rec     recordSet
	tenants *state.Tenants

	// read is when the connector's last read began, or when the store was
	// opened. A schedule edited since is left out of the next read once, so
	// the connector forgets where it was in the old expression and starts
	// the new one from now, as it does for a resumed schedule. Otherwise an
	// edit from "daily" to "every minute" would catch up on every minute
	// since the last daily run. It is the connector's own memory: dropped
	// with the process, as the connector's place in each schedule is.
	mu   sync.Mutex
	read time.Time
}

// MaxSchedules is the most schedules a tenant may hold.
const MaxSchedules = 1000

// claimGrace is how long a claim on a name with no schedule under it is
// taken to be a create in flight. Past it, the claim was left by a create
// that failed, and another create may take it.
const claimGrace = time.Minute

const (
	schedulesNamespace = "schedules"
	scheduleCount      = "#count"
	schedulesConverted = "converted:v1"
)

func claimKey(name string) string { return "name:" + name }

// nameClaim is which tenant holds a schedule's name, and since when.
type nameClaim struct {
	Tenant string    `json:"tenant"`
	At     time.Time `json:"at"`
}

// storedSchedule is a schedule as it is written. Seq is the place a release
// before this one kept it in; it is read and kept, and orders nothing.
type storedSchedule struct {
	Seq      int64         `json:"seq"`
	Schedule cron.Schedule `json:"schedule"`
	Paused   bool          `json:"paused,omitempty"`
	About    About         `json:"about"`
	// EditedAt is when what the schedule runs, or when, last changed.
	EditedAt time.Time `json:"edited_at,omitzero"`
}

// UnmarshalJSON reads a stored schedule, one written before the event union
// among them: its context was then kept as Meta (legacy.ScheduleContext).
func (r *storedSchedule) UnmarshalJSON(b []byte) error {
	type plain storedSchedule
	var w struct {
		plain
		Schedule json.RawMessage `json:"schedule"`
	}
	if err := json.Unmarshal(b, &w); err != nil {
		return err
	}
	*r = storedSchedule(w.plain)
	if len(w.Schedule) == 0 {
		return nil
	}
	if err := json.Unmarshal(w.Schedule, &r.Schedule); err != nil {
		return err
	}
	if r.Schedule.Context == nil {
		old, err := legacy.ScheduleContext(w.Schedule)
		if err != nil {
			return err
		}
		r.Schedule.Context = old
	}
	return nil
}

// ScheduleEntry is a schedule with what the platform keeps beside it.
type ScheduleEntry struct {
	cron.Schedule
	Paused bool
	About  About
}

// OpenSchedules returns schedules kept in st, which may be shared with other
// processes. A nil st keeps them in this process's memory.
//
// Names held before claims were kept are claimed the first time any process
// opens the store; a process that opens it at the same time claims the same.
func OpenSchedules(st state.Store) (*Schedules, error) {
	if st == nil {
		st = state.NewMemory()
	}
	s := &Schedules{rec: recordSet{st: st, namespace: schedulesNamespace}, read: time.Now()}
	if err := s.convert(); err != nil {
		return nil, fmt.Errorf("platform: claim schedule names: %w", err)
	}
	tenants, err := state.OpenTenants(st, schedulesNamespace)
	if err != nil {
		return nil, fmt.Errorf("platform: schedule tenants: %w", err)
	}
	s.tenants = tenants
	return s, nil
}

// convert claims every name a schedule was kept under before claims were.
func (s *Schedules) convert() error {
	var done bool
	if _, err := s.rec.lookup("", schedulesConverted, &done); err == nil {
		return nil
	} else if !errors.Is(err, state.ErrNotFound) {
		return err
	}
	c, cancel := storeCtx()
	recs, err := s.rec.st.Load(c, schedulesNamespace)
	cancel()
	if err != nil {
		return err
	}
	for _, r := range recs {
		if r.Tenant == "" || strings.HasPrefix(r.Key, "#") {
			continue
		}
		op, err := recordOp("", claimKey(r.Key), nameClaim{Tenant: r.Tenant}, state.Missing)
		if err != nil {
			return err
		}
		if err := s.rec.write(op); err != nil && !errors.Is(err, state.ErrConflict) {
			return err
		}
	}
	op, err := recordOp("", schedulesConverted, true, state.Missing)
	if err != nil {
		return err
	}
	if err := s.rec.write(op); err != nil && !errors.Is(err, state.ErrConflict) {
		return err
	}
	return nil
}

// lookup reads one schedule and its version.
func (s *Schedules) lookup(tenant, name string) (storedSchedule, int64, error) {
	var r storedSchedule
	if tenant == "" || name == "" || strings.HasPrefix(name, "#") {
		return r, 0, fmt.Errorf("%w: %q", ErrScheduleNotFound, name)
	}
	v, err := s.rec.lookup(tenant, name, &r)
	if errors.Is(err, state.ErrNotFound) {
		return r, 0, fmt.Errorf("%w: %q", ErrScheduleNotFound, name)
	}
	if err != nil {
		return r, 0, fmt.Errorf("platform: read schedule: %w", err)
	}
	return r, v, nil
}

// all reads every schedule of a tenant, by name.
func (s *Schedules) all(tenant string) ([]storedSchedule, error) {
	var out []storedSchedule
	after := ""
	for {
		page, next, err := s.rec.list(tenant, state.Query{After: after, Limit: state.MaxList})
		if err != nil {
			return nil, fmt.Errorf("platform: read schedules: %w", err)
		}
		for _, r := range page {
			if strings.HasPrefix(r.Key, "#") {
				continue
			}
			var sc storedSchedule
			if err := json.Unmarshal(r.Value, &sc); err != nil {
				return nil, fmt.Errorf("platform: schedule %q/%q: %w", tenant, r.Key, err)
			}
			out = append(out, sc)
		}
		if len(out) > MaxSchedules {
			return nil, fmt.Errorf("%w: %q holds more than %d schedules", state.ErrTooMany, tenant, MaxSchedules)
		}
		if next == "" {
			return out, nil
		}
		after = next
	}
}

// count reads how many schedules a tenant holds, and the version of the
// count, 0 when none is kept yet and it was counted.
func (s *Schedules) count(tenant string) (int, int64, error) {
	var n int
	v, err := s.rec.lookup(tenant, scheduleCount, &n)
	if err == nil {
		return n, v, nil
	}
	if !errors.Is(err, state.ErrNotFound) {
		return 0, 0, err
	}
	all, err := s.all(tenant)
	return len(all), 0, err
}

// update changes one schedule by compare-and-set. fn returning
// state.ErrUnchanged writes nothing.
func (s *Schedules) update(tenant, name string, fn func(r *storedSchedule) error) error {
	return state.Retry(func() error {
		r, v, err := s.lookup(tenant, name)
		if err != nil {
			return err
		}
		if err := fn(&r); errors.Is(err, state.ErrUnchanged) {
			return nil
		} else if err != nil {
			return err
		}
		op, err := recordOp(tenant, name, r, v)
		if err != nil {
			return err
		}
		if err := s.rec.write(op); err != nil {
			if errors.Is(err, state.ErrConflict) {
				return err
			}
			return fmt.Errorf("platform: write schedule: %w", err)
		}
		return nil
	})
}

var _ cron.Store = (*Schedules)(nil)

// Schedules implements cron.Store. It returns every tenant's schedules, because
// the connector is the deployment's and each schedule carries its own tenant.
// It pages the directory of tenants, and reads each tenant's schedules.
//
// A paused schedule is left out, which is all pausing is: the connector forgets
// a schedule it is no longer shown, and starts it fresh when it reappears, so a
// resumed schedule does not fire for the occurrences it slept through.
//
// An edited schedule is left out of one read for the same reason.
func (s *Schedules) Schedules(context.Context) ([]cron.Schedule, error) {
	now := time.Now()
	s.mu.Lock()
	since := s.read
	s.mu.Unlock()
	var out []cron.Schedule
	err := s.tenants.Each(func(tenant string) error {
		all, err := s.all(tenant)
		if err != nil {
			return err
		}
		for _, r := range all {
			if !r.Paused && r.EditedAt.Before(since) {
				out = append(out, r.Schedule)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.read = now
	s.mu.Unlock()
	return out, nil
}

// Get returns one of a tenant's schedules and whether it is paused.
func (s *Schedules) Get(tenant, name string) (cron.Schedule, bool, error) {
	r, _, err := s.lookup(tenant, name)
	if err != nil {
		return cron.Schedule{}, false, err
	}
	return r.Schedule, r.Paused, nil
}

// Entry returns one of a tenant's schedules with what is kept beside it.
func (s *Schedules) Entry(tenant, name string) (ScheduleEntry, error) {
	r, _, err := s.lookup(tenant, name)
	if err != nil {
		return ScheduleEntry{}, err
	}
	return ScheduleEntry{Schedule: r.Schedule, Paused: r.Paused, About: r.About}, nil
}

// Authored returns one of a tenant's schedules and who wrote what it runs:
// whoever last changed its text, or its creator (meta created_by) while
// nobody has, and nobody ("") once someone else chose its agent or its
// project (see About.WrittenBy). Both are read in one record, so the author
// is the text's.
func (s *Schedules) Authored(tenant, name string) (cron.Schedule, string, error) {
	r, _, err := s.lookup(tenant, name)
	if err != nil {
		return cron.Schedule{}, "", err
	}
	return r.Schedule, authorOf(r.Schedule, r.About), nil
}

// About returns what the platform keeps about one of a tenant's schedules.
func (s *Schedules) About(tenant, name string) (About, error) {
	r, _, err := s.lookup(tenant, name)
	return r.About, err
}

// SetPaused pauses or resumes a tenant's schedule.
func (s *Schedules) SetPaused(tenant, name string, paused bool) error {
	return s.update(tenant, name, func(r *storedSchedule) error {
		r.Paused = paused
		return nil
	})
}

// Upcoming returns the next n times an expression fires after a time, in UTC.
func Upcoming(expression string, after time.Time, n int) []time.Time {
	spec, err := cron.Parse(expression)
	if err != nil {
		return nil
	}
	out := make([]time.Time, 0, n)
	t := after.UTC()
	for range n {
		next, ok := spec.Next(t)
		if !ok {
			break
		}
		out = append(out, next)
		t = next
	}
	return out
}

// List returns one tenant's schedules, by name.
func (s *Schedules) List(tenant string) ([]ScheduleEntry, error) {
	all, err := s.all(tenant)
	if err != nil {
		return nil, err
	}
	out := make([]ScheduleEntry, 0, len(all))
	for _, r := range all {
		out = append(out, ScheduleEntry{Schedule: r.Schedule, Paused: r.Paused, About: r.About})
	}
	return out, nil
}

// Add stores a schedule. Names are unique across tenants, because a schedule's
// name is part of the id of every run it raises.
func (s *Schedules) Add(sc cron.Schedule, about About) error { return s.add(sc, about, false) }

// AddPaused keeps a standing assignment inert until its contract is durable.
func (s *Schedules) AddPaused(sc cron.Schedule, about About) error { return s.add(sc, about, true) }

func (s *Schedules) add(sc cron.Schedule, about About, paused bool) error {
	if err := sc.Validate(); err != nil {
		return err
	}
	if err := about.validate(); err != nil {
		return err
	}
	return s.create(sc, about, paused)
}

// create claims a schedule's name for its tenant and writes it, with the
// tenant's count, in one write. A create that fails gives the claim up.
func (s *Schedules) create(sc cron.Schedule, about About, paused bool) error {
	if err := s.claim(sc.Tenant, sc.Name); err != nil {
		return err
	}
	if err := s.tenants.Mark(sc.Tenant); err != nil {
		s.unclaim(sc.Tenant, sc.Name)
		return fmt.Errorf("platform: write schedule: %w", err)
	}
	err := state.Retry(func() error {
		if _, _, err := s.lookup(sc.Tenant, sc.Name); err == nil {
			return fmt.Errorf("%w: %q", ErrScheduleExists, sc.Name)
		} else if !errors.Is(err, ErrScheduleNotFound) {
			return err
		}
		n, v, err := s.count(sc.Tenant)
		if err != nil {
			return err
		}
		if n >= MaxSchedules {
			return fmt.Errorf("%w: a workspace holds at most %d schedules", ErrScheduleInvalid, MaxSchedules)
		}
		rec, err := recordOp(sc.Tenant, sc.Name, storedSchedule{Schedule: sc, Paused: paused, About: about}, state.Missing)
		if err != nil {
			return err
		}
		cnt, err := recordOp(sc.Tenant, scheduleCount, n+1, versionOf(v))
		if err != nil {
			return err
		}
		if err := s.rec.write(rec, cnt); err != nil {
			if errors.Is(err, state.ErrConflict) {
				return err
			}
			return fmt.Errorf("platform: write schedule: %w", err)
		}
		return nil
	})
	if err != nil {
		s.unclaim(sc.Tenant, sc.Name)
	}
	return err
}

// claim takes a name for a tenant, or reports ErrScheduleExists when a
// schedule holds it, or another create claimed it within claimGrace.
func (s *Schedules) claim(tenant, name string) error {
	return state.Retry(func() error {
		var c nameClaim
		v, err := s.rec.lookup("", claimKey(name), &c)
		if err != nil && !errors.Is(err, state.ErrNotFound) {
			return fmt.Errorf("platform: read schedule names: %w", err)
		}
		if err == nil {
			if _, _, err := s.lookup(c.Tenant, name); err == nil {
				if c.Tenant != tenant {
					return fmt.Errorf("%w: %q is another workspace's", ErrScheduleExists, name)
				}
				return fmt.Errorf("%w: %q", ErrScheduleExists, name)
			} else if !errors.Is(err, ErrScheduleNotFound) {
				return err
			}
			if time.Since(c.At) < claimGrace {
				return fmt.Errorf("%w: %q is being created", ErrScheduleExists, name)
			}
		}
		op, err := recordOp("", claimKey(name), nameClaim{Tenant: tenant, At: time.Now().UTC()}, versionOf(v))
		if err != nil {
			return err
		}
		if err := s.rec.write(op); err != nil {
			if errors.Is(err, state.ErrConflict) {
				return err
			}
			return fmt.Errorf("platform: write schedule names: %w", err)
		}
		return nil
	})
}

// unclaim gives a tenant's claim on a name up, when no schedule holds it. A
// claim it cannot give up is taken over by the next create after claimGrace.
func (s *Schedules) unclaim(tenant, name string) {
	var c nameClaim
	v, err := s.rec.lookup("", claimKey(name), &c)
	if err != nil || c.Tenant != tenant {
		return
	}
	if _, _, err := s.lookup(tenant, name); !errors.Is(err, ErrScheduleNotFound) {
		return
	}
	_ = s.rec.write(state.DeleteAt("", claimKey(name), v))
}

// Replace changes a tenant's schedule in place: its expression, agent, text
// and meta, by about.UpdatedBy. Whether it is paused is kept. The new
// schedule is checked in full before the old one is touched, so an edit that
// is refused changes nothing. Who wrote what it runs (About.WrittenBy) is
// decided here, from the schedule as it was read for the write: the editor
// when they change its text, and when they change only its agent or its
// project, whose instructions and notes go ahead of the text, they if they
// wrote its text and else nobody, since anyone else's choice of agent or
// project for those words is no one's whole say-so.
func (s *Schedules) Replace(sc cron.Schedule, about About) error {
	if err := sc.Validate(); err != nil {
		return err
	}
	if err := about.validate(); err != nil {
		return err
	}
	return s.update(sc.Tenant, sc.Name, func(r *storedSchedule) error {
		if r.About.Managed != "" {
			return fmt.Errorf("%w: change %q in the file", ErrScheduleManaged, sc.Name)
		}
		old, author := r.Schedule, authorOf(r.Schedule, r.About)
		switch {
		case sc.Text != old.Text:
			author = about.UpdatedBy
		case (sc.Agent != old.Agent || sc.Context[runs.MetaProject] != old.Context[runs.MetaProject]) && author != about.UpdatedBy:
			author = ""
		}
		next := about
		next.WrittenBy = author
		r.Schedule, r.About = sc, next
		r.EditedAt = time.Now().UTC()
		return nil
	})
}

// Remove deletes a tenant's schedule.
func (s *Schedules) Remove(tenant, name string) error {
	err := state.Retry(func() error {
		r, v, err := s.lookup(tenant, name)
		if err != nil {
			return err
		}
		if r.About.Managed != "" {
			return fmt.Errorf("%w: remove %q from the file first", ErrScheduleManaged, name)
		}
		ops := []state.Op{state.DeleteAt(tenant, name, v)}
		var n int
		if cv, err := s.rec.lookup(tenant, scheduleCount, &n); err == nil {
			op, err := recordOp(tenant, scheduleCount, max(n-1, 0), cv)
			if err != nil {
				return err
			}
			ops = append(ops, op)
		} else if !errors.Is(err, state.ErrNotFound) {
			return err
		}
		if err := s.rec.write(ops...); err != nil {
			if errors.Is(err, state.ErrConflict) {
				return err
			}
			return fmt.Errorf("platform: write schedule: %w", err)
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.unclaim(tenant, name)
	return nil
}

// Declare puts a schedule the platform file declares: created, or taken over
// from the console, or changed to what the file now says, and marked as the
// file's. Whether it is paused is kept, and so is when it was created. A
// schedule the file declares unchanged is not written again. A name another
// workspace holds is ErrScheduleExists: names are unique across them. Its
// words are the file's, so Run Now acts for nobody (About.WrittenBy).
func (s *Schedules) Declare(sc cron.Schedule, description string, at time.Time) (changed bool, err error) {
	if err := sc.Validate(); err != nil {
		return false, err
	}
	about := About{Description: description, Managed: ManagedFile}
	if err := about.validate(); err != nil {
		return false, err
	}
	err = s.update(sc.Tenant, sc.Name, func(r *storedSchedule) error {
		next := about
		was := r.About
		next.CreatedAt, next.UpdatedBy, next.UpdatedAt = was.CreatedAt, was.UpdatedBy, was.UpdatedAt
		if was == next && sameSchedule(r.Schedule, sc) {
			changed = false
			return state.ErrUnchanged
		}
		next.UpdatedBy, next.UpdatedAt = ManagedFile, at
		r.Schedule, r.About = sc, next
		r.EditedAt = time.Now().UTC()
		changed = true
		return nil
	})
	if !errors.Is(err, ErrScheduleNotFound) {
		return changed, err
	}
	about.CreatedAt = at
	if err := s.create(sc, about, false); err != nil {
		return false, err
	}
	return true, nil
}

// Release hands a schedule that left the platform file to the console. It
// stays as it is, and the API may change it from now on.
func (s *Schedules) Release(tenant, name string) error {
	return s.update(tenant, name, func(r *storedSchedule) error {
		if r.About.Managed == "" {
			return state.ErrUnchanged
		}
		r.About.Managed = ""
		return nil
	})
}

func sameSchedule(a, b cron.Schedule) bool {
	return a.Name == b.Name && a.Expression == b.Expression && a.Agent == b.Agent && a.Text == b.Text &&
		a.Tenant == b.Tenant && maps.Equal(a.Context, b.Context)
}
