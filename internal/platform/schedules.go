package platform

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/urmzd/fiber/pkg/connectors/cron"

	"github.com/urmzd/warp/pkg/state"
)

// Schedule errors.
var (
	ErrScheduleExists   = errors.New("platform: schedule already exists")
	ErrScheduleNotFound = errors.New("platform: schedule not found")
)

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
}

func (a About) validate() error {
	if len(a.Description) > MaxScheduleDescription {
		return fmt.Errorf("%w: a description is at most %d characters", ErrScheduleInvalid, MaxScheduleDescription)
	}
	return nil
}

// Schedules is the store the cron connector reads. It is in memory. Built
// with OpenSchedules, it writes every change through a state.Store and reads
// the schedules back when it is built again, paused or not.
type Schedules struct {
	mu     sync.Mutex
	all    []cron.Schedule
	paused map[string]bool
	about  map[string]About
	// edited are schedules changed since the connector last read the store.
	// The next read leaves them out once, so the connector forgets where it
	// was in the old expression and starts the new one from now, as it does
	// for a resumed schedule. Otherwise an edit from "daily" to "every
	// minute" would catch up on every minute since the last daily run.
	edited map[string]bool

	// kept is where schedules are written, and seqs each one's place in all
	// as it was written. Nil keeps them only in memory.
	kept *state.Table[storedSchedule]
	seq  int64
	seqs map[string]int64
}

// storedSchedule is a schedule as it is written.
type storedSchedule struct {
	Seq      int64         `json:"seq"`
	Schedule cron.Schedule `json:"schedule"`
	Paused   bool          `json:"paused,omitempty"`
	About    About         `json:"about"`
}

// OpenSchedules returns schedules kept in st, holding what st already has. A
// nil st keeps them in memory.
func OpenSchedules(st state.Store) (*Schedules, error) {
	s := &Schedules{kept: state.NewTable[storedSchedule](st, "schedules"), seqs: map[string]int64{}}
	var loaded []storedSchedule
	if err := s.kept.Load(func(_, _ string, r storedSchedule) error {
		loaded = append(loaded, r)
		return nil
	}); err != nil {
		return nil, fmt.Errorf("platform: load schedules: %w", err)
	}
	slices.SortFunc(loaded, func(a, b storedSchedule) int { return cmp.Compare(a.Seq, b.Seq) })
	for _, r := range loaded {
		s.all = append(s.all, r.Schedule)
		s.setAbout(r.Schedule.Name, r.About)
		if r.Paused {
			if s.paused == nil {
				s.paused = map[string]bool{}
			}
			s.paused[r.Schedule.Name] = true
		}
		s.seqs[r.Schedule.Name] = r.Seq
		s.seq = max(s.seq, r.Seq)
	}
	return s, nil
}

// write puts a schedule's record. The caller holds the lock.
func (s *Schedules) write(sc cron.Schedule, paused bool, about About) error {
	if s.kept == nil {
		return nil
	}
	seq, ok := s.seqs[sc.Name]
	if !ok {
		seq = s.seq + 1
	}
	if err := s.kept.Put(sc.Tenant, sc.Name, storedSchedule{Seq: seq, Schedule: sc, Paused: paused, About: about}); err != nil {
		return fmt.Errorf("platform: write schedule: %w", err)
	}
	if !ok {
		s.seq, s.seqs[sc.Name] = seq, seq
	}
	return nil
}

var _ cron.Store = (*Schedules)(nil)

// Schedules implements cron.Store. It returns every tenant's schedules, because
// the connector is the deployment's and each schedule carries its own tenant.
//
// A paused schedule is left out, which is all pausing is: the connector forgets
// a schedule it is no longer shown, and starts it fresh when it reappears, so a
// resumed schedule does not fire for the occurrences it slept through.
//
// An edited schedule is left out of one read for the same reason.
func (s *Schedules) Schedules(context.Context) ([]cron.Schedule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]cron.Schedule, 0, len(s.all))
	for _, sc := range s.all {
		if s.edited[sc.Name] {
			delete(s.edited, sc.Name)
			continue
		}
		if !s.paused[sc.Name] {
			out = append(out, sc)
		}
	}
	return out, nil
}

// Get returns one of a tenant's schedules and whether it is paused.
func (s *Schedules) Get(tenant, name string) (cron.Schedule, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sc := range s.all {
		if sc.Name == name && sc.Tenant == tenant {
			return sc, s.paused[name], nil
		}
	}
	return cron.Schedule{}, false, fmt.Errorf("%w: %q", ErrScheduleNotFound, name)
}

// Authored returns one of a tenant's schedules and who wrote what it runs:
// its last editor, or its creator (meta created_by) while nobody has edited
// it. Both are read under one lock, so the author is the text's.
func (s *Schedules) Authored(tenant, name string) (cron.Schedule, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sc := range s.all {
		if sc.Name == name && sc.Tenant == tenant {
			return sc, cmp.Or(s.about[name].UpdatedBy, sc.Meta["created_by"]), nil
		}
	}
	return cron.Schedule{}, "", fmt.Errorf("%w: %q", ErrScheduleNotFound, name)
}

// Paused reports whether a schedule is paused.
func (s *Schedules) Paused(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.paused[name]
}

// About returns what the platform keeps about a schedule.
func (s *Schedules) About(name string) About {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.about[name]
}

// SetPaused pauses or resumes a tenant's schedule.
func (s *Schedules) SetPaused(tenant, name string, paused bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sc := range s.all {
		if sc.Name == name && sc.Tenant == tenant {
			if err := s.write(sc, paused, s.about[name]); err != nil {
				return err
			}
			if s.paused == nil {
				s.paused = map[string]bool{}
			}
			if paused {
				s.paused[name] = true
			} else {
				delete(s.paused, name)
			}
			return nil
		}
	}
	return fmt.Errorf("%w: %q", ErrScheduleNotFound, name)
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
func (s *Schedules) List(tenant string) []cron.Schedule {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []cron.Schedule
	for _, sc := range s.all {
		if sc.Tenant == tenant {
			out = append(out, sc)
		}
	}
	slices.SortFunc(out, func(a, b cron.Schedule) int { return strings.Compare(a.Name, b.Name) })
	return out
}

// Add stores a schedule. Names are unique across tenants, because a schedule's
// name is part of the id of every run it raises.
func (s *Schedules) Add(sc cron.Schedule, about About) error {
	if err := sc.Validate(); err != nil {
		return err
	}
	if err := about.validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, have := range s.all {
		if have.Name == sc.Name {
			return fmt.Errorf("%w: %q", ErrScheduleExists, sc.Name)
		}
	}
	if err := s.write(sc, false, about); err != nil {
		return err
	}
	s.all = append(s.all, sc)
	s.setAbout(sc.Name, about)
	return nil
}

// Replace changes a tenant's schedule in place: its expression, agent, text
// and meta. Whether it is paused is kept. The new schedule is checked in full
// before the old one is touched, so an edit that is refused changes nothing.
func (s *Schedules) Replace(sc cron.Schedule, about About) error {
	if err := sc.Validate(); err != nil {
		return err
	}
	if err := about.validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	i := slices.IndexFunc(s.all, func(have cron.Schedule) bool { return have.Name == sc.Name && have.Tenant == sc.Tenant })
	if i < 0 {
		return fmt.Errorf("%w: %q", ErrScheduleNotFound, sc.Name)
	}
	if err := s.write(sc, s.paused[sc.Name], about); err != nil {
		return err
	}
	s.all[i] = sc
	s.setAbout(sc.Name, about)
	if s.edited == nil {
		s.edited = map[string]bool{}
	}
	s.edited[sc.Name] = true
	return nil
}

func (s *Schedules) setAbout(name string, about About) {
	if about == (About{}) {
		delete(s.about, name)
		return
	}
	if s.about == nil {
		s.about = map[string]About{}
	}
	s.about[name] = about
}

// Remove deletes a tenant's schedule.
func (s *Schedules) Remove(tenant, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, have := range s.all {
		if have.Name == name && have.Tenant == tenant {
			if s.kept != nil {
				if err := s.kept.Delete(tenant, name); err != nil {
					return fmt.Errorf("platform: write schedule: %w", err)
				}
				delete(s.seqs, name)
			}
			s.all = slices.Delete(s.all, i, i+1)
			delete(s.paused, name)
			delete(s.about, name)
			delete(s.edited, name)
			return nil
		}
	}
	return fmt.Errorf("%w: %q", ErrScheduleNotFound, name)
}
