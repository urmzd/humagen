package platform

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/urmzd/fiber/pkg/connectors/cron"

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
		return sc.Meta["created_by"]
	}
	return about.WrittenBy
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
// whoever last changed its text, or its creator (meta created_by) while
// nobody has, and nobody ("") once someone else chose its agent or its
// project (see About.WrittenBy). Both are read under one lock, so the author is the
// text's.
func (s *Schedules) Authored(tenant, name string) (cron.Schedule, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sc := range s.all {
		if sc.Name == name && sc.Tenant == tenant {
			return sc, authorOf(sc, s.about[name]), nil
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
// and meta, by about.UpdatedBy. Whether it is paused is kept. The new
// schedule is checked in full before the old one is touched, so an edit that
// is refused changes nothing. Who wrote what it runs (About.WrittenBy) is
// decided here, from the schedule as it was, under the lock: the editor
// when they change its text, and when they change only its agent or its
// project, whose instructions and notes go ahead of the text, they if they
// wrote its text and else nobody, since anyone else's choice of agent or
// project for those words is no one's whole say-so. Saving it as it was
// writes nothing.
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
	if s.about[sc.Name].Managed != "" {
		return fmt.Errorf("%w: change %q in the file", ErrScheduleManaged, sc.Name)
	}
	old, author := s.all[i], authorOf(s.all[i], s.about[sc.Name])
	switch {
	case sc.Text != old.Text:
		author = about.UpdatedBy
	case (sc.Agent != old.Agent || sc.Meta[runs.MetaProject] != old.Meta[runs.MetaProject]) && author != about.UpdatedBy:
		author = ""
	}
	about.WrittenBy = author
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
			if s.about[name].Managed != "" {
				return fmt.Errorf("%w: remove %q from the file first", ErrScheduleManaged, name)
			}
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
	s.mu.Lock()
	defer s.mu.Unlock()
	i := slices.IndexFunc(s.all, func(have cron.Schedule) bool { return have.Name == sc.Name })
	if i < 0 {
		about.CreatedAt = at
		if err := s.write(sc, false, about); err != nil {
			return false, err
		}
		s.all = append(s.all, sc)
		s.setAbout(sc.Name, about)
		return true, nil
	}
	have, was := s.all[i], s.about[sc.Name]
	if have.Tenant != sc.Tenant {
		return false, fmt.Errorf("%w: %q is another workspace's", ErrScheduleExists, sc.Name)
	}
	about.CreatedAt, about.UpdatedBy, about.UpdatedAt = was.CreatedAt, was.UpdatedBy, was.UpdatedAt
	sc.Routes = have.Routes
	if was == about && sameSchedule(have, sc) {
		return false, nil
	}
	about.UpdatedBy, about.UpdatedAt = ManagedFile, at
	if err := s.write(sc, s.paused[sc.Name], about); err != nil {
		return false, err
	}
	s.all[i] = sc
	s.setAbout(sc.Name, about)
	if s.edited == nil {
		s.edited = map[string]bool{}
	}
	s.edited[sc.Name] = true
	return true, nil
}

// Release hands a schedule that left the platform file to the console. It
// stays as it is, and the API may change it from now on.
func (s *Schedules) Release(tenant, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, have := range s.all {
		if have.Name == name && have.Tenant == tenant {
			about := s.about[name]
			if about.Managed == "" {
				return nil
			}
			about.Managed = ""
			if err := s.write(have, s.paused[name], about); err != nil {
				return err
			}
			s.setAbout(name, about)
			return nil
		}
	}
	return fmt.Errorf("%w: %q", ErrScheduleNotFound, name)
}

func sameSchedule(a, b cron.Schedule) bool {
	return a.Name == b.Name && a.Expression == b.Expression && a.Agent == b.Agent && a.Text == b.Text &&
		a.Tenant == b.Tenant && maps.Equal(a.Meta, b.Meta)
}
