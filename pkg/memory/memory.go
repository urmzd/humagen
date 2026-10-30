// Package memory is what an agent carries from one conversation to the next.
//
// Each agent has its own short list of notes. They are sent to the agent with
// every conversation turn, the way a person brings what they know to a meeting.
// A note is written by a person, or proposed by the workspace's memory agent
// after a turn. Either way it is plain text that anyone with access can read,
// correct and remove: nothing an agent remembers is hidden from the people it
// works for.
//
// # A person decides what is kept
//
// What the memory agent writes is a proposal. It is held as pending, it is not
// sent to the agent, and it becomes a memory only when a person approves it.
// That is saige's own default for memory writes: its Policy asks a human before
// every write unless AutoApprove is set, and a workspace can choose the same.
//
// What may be stored at all is also saige's rule. Every note passes through
// saige's memory Policy, which refuses text that carries personal data.
//
// The store is in memory. Built with Open, it writes every change through a
// state.Store and reads them back when it is built again.
package memory

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	saigememory "github.com/urmzd/saige/agent/memory"

	"github.com/urmzd/humagen/pkg/blob"
	"github.com/urmzd/humagen/pkg/drive"
	"github.com/urmzd/humagen/pkg/state"
)

// Errors.
var (
	ErrInvalid  = errors.New("memory: invalid")
	ErrNotFound = errors.New("memory: not found")
	// ErrSensitive reports a note that carries personal data.
	ErrSensitive = errors.New("memory: the note carries personal data")
)

// Bounds. Memory rides along on every turn, so it is kept small.
const (
	MaxNotes = 50
	MaxChars = 300
	// MaxPending bounds proposals waiting for a person. When it is full the
	// oldest proposal is dropped, so an unattended queue cannot grow.
	MaxPending = 20
)

// Status says whether a note is in use.
type Status string

const (
	// Approved notes are sent to the agent.
	Approved Status = "approved"
	// Pending notes are proposals. They are not sent to the agent.
	Pending Status = "pending"
)

// Source says who wrote a note.
type Source string

const (
	// Person is a note someone typed.
	Person Source = "person"
	// Consolidated is a note the memory agent wrote after a turn.
	Consolidated Source = "consolidated"
	Artifact     Source = "artifact"
)

type Kind string

const (
	Fact       Kind = "fact"
	Risk       Kind = "risk"
	Decision   Kind = "decision"
	Preference Kind = "preference"
	PersonKind Kind = "person"
)

// Details are reviewable metadata. Empty kind means fact for old notes.
type Details struct {
	Kind      Kind      `json:"kind,omitempty"`
	ExpiresAt time.Time `json:"expires_at,omitzero"`
}

// Note is one thing an agent remembers.
type Note struct {
	EditedBy   string    `json:"edited_by,omitempty"`
	EditedAt   time.Time `json:"edited_at,omitzero"`
	Uses       uint64    `json:"uses,omitempty"`
	LastUsedAt time.Time `json:"last_used_at,omitzero"`
	Details
	ReviewedBy string    `json:"reviewed_by,omitempty"`
	ReviewedAt time.Time `json:"reviewed_at,omitzero"`
	ID         string    `json:"id"`
	Text       string    `json:"text"`
	Source     Source    `json:"source"`
	Status     Status    `json:"status"`
	// By is the person who wrote it, or the run it was drawn from.
	By string    `json:"by,omitempty"`
	At time.Time `json:"at"`
	// seq orders an owner's notes. It is kept in the note's file.
	seq int
}

// Store holds every agent's notes. It is a view of the notes' files in a
// drive.Store, kept in memory and written through on every change.
type Store struct {
	mu      sync.RWMutex
	by      map[string][]Note
	origins map[string]map[string]string
	usage   map[string]map[string]usage
	// props is, by owner, the drive proposal each pending note is.
	props map[string]map[string]string
	next  int
	drive *drive.Store
	// kept is what is kept beside the files: the id counter, use counts and
	// reviewed origins. Nil keeps them only in memory.
	kept *state.Table[stored]
}

// usage is how often a note was sent, kept beside its file.
type usage struct {
	Uses       uint64    `json:"uses,omitempty"`
	LastUsedAt time.Time `json:"last_used_at,omitzero"`
}

// stored is what an owner keeps beside its files. Next is the store's
// counter when it was written, so the order of notes survives a restart.
//
// Before notes were files, Notes held them. Open moves them to files once and
// marks the record Exported.
type stored struct {
	Notes    []Note            `json:"notes,omitempty"`
	Next     int               `json:"next"`
	Origins  map[string]string `json:"reviewed_origins,omitempty"`
	Usage    map[string]usage  `json:"usage,omitempty"`
	Exported bool              `json:"exported,omitempty"`
}

// NewStore returns an empty store in memory.
func NewStore() *Store {
	s, err := Open(nil, nil)
	if err != nil {
		panic(err)
	}
	return s
}

// Open returns a store whose notes are files in d, keeping what is not
// content in st. A nil st keeps that in memory; a nil d is a drive on st,
// content included.
//
// Notes kept in st before notes were files are exported to d once: approved
// ones are committed, pending ones proposed. The export is safe to repeat: a
// note already in the tree is not written again, and an owner is marked
// exported only after its notes are in place.
func Open(st state.Store, d *drive.Store) (*Store, error) {
	if d == nil {
		var err error
		ds := st
		if ds == nil {
			ds = state.NewMemory()
		}
		if d, err = drive.Open(ds, blob.NewState(ds), drive.WithScreen(Screen)); err != nil {
			return nil, err
		}
	}
	s := &Store{by: map[string][]Note{}, origins: map[string]map[string]string{}, usage: map[string]map[string]usage{}, props: map[string]map[string]string{}, drive: d}
	s.kept = state.NewTable[stored](st, "memory")
	legacy := map[string]stored{}
	err := s.kept.Load(func(tenant, owner string, r stored) error {
		s.next = max(s.next, r.Next)
		k := key(tenant, owner)
		s.origins[k] = r.Origins
		s.usage[k] = r.Usage
		if !r.Exported {
			legacy[k] = r
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("memory: load: %w", err)
	}
	ctx, cancel := ctxTimeout()
	defer cancel()
	for _, tenant := range d.Tenants() {
		for _, tree := range d.Trees(tenant) {
			if owner, ok := OwnerOf(tree); ok {
				if err := s.load(ctx, tenant, owner); err != nil {
					return nil, fmt.Errorf("memory: load %s: %w", tree.Key(), err)
				}
			}
		}
	}
	keys := make([]string, 0, len(legacy))
	for k := range legacy {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		tenant, owner, _ := strings.Cut(k, "\x00")
		if err := s.export(tenant, owner, legacy[k]); err != nil {
			return nil, fmt.Errorf("memory: export %q: %w", owner, err)
		}
	}
	d.Watch(s.watch)
	return s, nil
}

// export moves an owner's notes from before notes were files into its tree.
func (s *Store) export(tenant, owner string, r stored) error {
	notes := slices.Clone(r.Notes)
	for i := range notes {
		// The old list's order is the notes' order.
		notes[i].seq = i + 1
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next = max(s.next, len(notes))
	return s.commit(tenant, owner, notes, max(s.next, r.Next), r.Origins, r)
}

func key(tenant, agent string) string { return tenant + "\x00" + agent }

// set replaces an owner's notes, writing them first. It is called with the
// lock held, and on a failed write leaves everything as it was.
func (s *Store) set(tenant, owner string, notes []Note, next int) error {
	return s.setOrigins(tenant, owner, notes, next, s.origins[key(tenant, owner)])
}

func (s *Store) setOrigins(tenant, owner string, notes []Note, next int, origins map[string]string) error {
	k := key(tenant, owner)
	return s.commit(tenant, owner, notes, next, origins, stored{Next: s.next, Origins: s.origins[k], Usage: s.usage[k], Exported: true})
}

// commit writes what is kept beside the files first, then the files. What is
// beside them names notes by id and means nothing without them, so a failed
// file write leaves it harmless; it is put back as prev all the same.
func (s *Store) commit(tenant, owner string, notes []Note, next int, origins map[string]string, prev stored) error {
	k := key(tenant, owner)
	retained := map[string]string{}
	used := map[string]usage{}
	for _, n := range notes {
		if origin := origins[n.ID]; origin != "" {
			retained[n.ID] = origin
		}
		if n.Uses > 0 || !n.LastUsedAt.IsZero() {
			used[n.ID] = usage{Uses: n.Uses, LastUsedAt: n.LastUsedAt}
		}
	}
	if err := s.kept.Put(tenant, owner, stored{Next: next, Origins: retained, Usage: used, Exported: true}); err != nil {
		return fmt.Errorf("memory: write: %w", err)
	}
	if err := s.write(tenant, owner, notes); err != nil {
		_ = s.kept.Put(tenant, owner, prev)
		return fmt.Errorf("memory: write: %w", err)
	}
	s.next = next
	s.origins[k] = retained
	s.usage[k] = used
	if len(notes) == 0 {
		delete(s.by, k)
		delete(s.origins, k)
		delete(s.usage, k)
		return nil
	}
	s.by[k] = notes
	return nil
}

// clean makes text fit for a note: one line, trimmed, bounded.
func clean(text string) (string, error) {
	text = strings.Join(strings.Fields(text), " ")
	text = strings.TrimLeft(text, "-*• ")
	switch {
	case text == "":
		return "", fmt.Errorf("%w: a note needs text", ErrInvalid)
	case len(text) > MaxChars:
		return "", fmt.Errorf("%w: a note is at most %d characters", ErrInvalid, MaxChars)
	}
	// saige's memory policy decides what may be stored. With no redactor
	// configured it refuses text its detector finds personal data in.
	if _, err := (saigememory.Policy{}).CheckContent(context.Background(), saigememory.Scope{}, text); err != nil {
		if errors.Is(err, saigememory.ErrSensitive) {
			return "", fmt.Errorf("%w: %v", ErrSensitive, err)
		}
		return "", err
	}
	return text, nil
}

// Add stores a note for an agent, approved or pending. A note the agent already
// holds, in either state, is not stored twice.
//
// When the approved list is full, the oldest consolidated note makes room, and
// a list full of notes people wrote refuses the new one: a person's note is
// never pushed out by an automatic one. When the pending list is full, the
// oldest proposal is dropped.
func (s *Store) Add(tenant, agent, text string, source Source, status Status, by string, at time.Time) (Note, error) {
	return s.AddDetailed(tenant, agent, text, source, status, by, at, Details{})
}

func (s *Store) AddDetailed(tenant, agent, text string, source Source, status Status, by string, at time.Time, details Details) (Note, error) {
	if details.Kind == "" {
		details.Kind = Fact
	}
	if details.Kind != Risk && details.Kind != Fact && details.Kind != Decision && details.Kind != Preference && details.Kind != PersonKind {
		return Note{}, ErrInvalid
	}
	if !details.ExpiresAt.IsZero() && !details.ExpiresAt.After(at) {
		return Note{}, ErrInvalid
	}
	text, err := clean(text)
	if err != nil {
		return Note{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key(tenant, agent)
	notes := s.by[k]
	for _, n := range notes {
		if strings.EqualFold(n.Text, text) {
			return n, nil
		}
	}
	notes, err = makeRoom(slices.Clone(notes), status)
	if err != nil {
		return Note{}, err
	}
	next := s.next + 1
	note := Note{Details: details, ID: "m_" + rand.Text(), Text: text, Source: source, Status: status, By: by, At: at, seq: next}
	if status == Approved {
		note.ReviewedBy = by
		note.ReviewedAt = at
	}
	if err := s.set(tenant, agent, append(notes, note), next); err != nil {
		return Note{}, err
	}
	return note, nil
}

func count(notes []Note, status Status) int {
	n := 0
	for _, x := range notes {
		if x.Status == status {
			n++
		}
	}
	return n
}

// makeRoom frees a place for one more note of a status, or refuses.
func makeRoom(notes []Note, status Status) ([]Note, error) {
	if status == Pending {
		if count(notes, Pending) >= MaxPending {
			i := slices.IndexFunc(notes, func(n Note) bool { return n.Status == Pending })
			notes = slices.Delete(notes, i, i+1)
		}
		return notes, nil
	}
	if count(notes, Approved) < MaxNotes {
		return notes, nil
	}
	i := slices.IndexFunc(notes, func(n Note) bool { return n.Status == Approved && n.Source == Consolidated })
	if i < 0 {
		return nil, fmt.Errorf("%w: an agent holds at most %d notes", ErrInvalid, MaxNotes)
	}
	return slices.Delete(notes, i, i+1), nil
}

// Approve turns the named proposals into memories. Ids that are not pending
// proposals of this agent are ignored. It returns how many were approved.
func (s *Store) Approve(tenant, agent string, ids []string, reviews ...Review) (int, error) {
	if len(reviews) > 1 || len(ids) > MaxNotes+MaxPending {
		return 0, ErrInvalid
	}
	if len(reviews) == 1 && (reviews[0].By == "" || reviews[0].At.IsZero()) {
		return 0, ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	notes := s.by[key(tenant, agent)]
	next := s.next
	approved := 0
	var refused error
	for _, id := range ids {
		i := slices.IndexFunc(notes, func(n Note) bool { return n.ID == id && n.Status == Pending })
		if i < 0 {
			continue
		}
		note := notes[i]
		rest, err := makeRoom(slices.Delete(slices.Clone(notes), i, i+1), Approved)
		if err != nil {
			refused = err
			break
		}
		note.Status = Approved
		// An approved note goes to the end of the list, as it always has.
		next++
		note.seq = next
		if len(reviews) == 1 {
			note.ReviewedBy = reviews[0].By
			note.ReviewedAt = reviews[0].At
		}
		notes = append(rest, note)
		approved++
	}
	if approved > 0 {
		if err := s.set(tenant, agent, notes, next); err != nil {
			return 0, err
		}
	}
	return approved, refused
}

// Proposal is a pending note and the agent it is for.
type Proposal struct {
	Agent string `json:"agent"`
	Note  Note   `json:"note"`
}

// Pending returns every proposal waiting in a tenant, oldest first.
func (s *Store) Pending(tenant string) []Proposal {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []Proposal
	prefix := tenant + "\x00"
	for k, notes := range s.by {
		agent, ok := strings.CutPrefix(k, prefix)
		if !ok {
			continue
		}
		for _, n := range notes {
			if n.Status == Pending {
				out = append(out, Proposal{Agent: agent, Note: n})
			}
		}
	}
	slices.SortStableFunc(out, func(a, b Proposal) int { return a.Note.At.Compare(b.Note.At) })
	return out
}

// List returns an agent's notes, oldest first.
func (s *Store) List(tenant, agent string) []Note {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return slices.Clone(s.by[key(tenant, agent)])
}

// Drop forgets everything an owner holds, for when the owner is deleted: its
// tree, history included.
func (s *Store) Drop(tenant, owner string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key(tenant, owner)
	if _, ok := s.by[k]; !ok {
		return nil
	}
	tree, err := TreeOf(owner)
	if err != nil {
		return err
	}
	ctx, cancel := ctxTimeout()
	defer cancel()
	if err := s.drive.Drop(ctx, tenant, tree, originMemory); err != nil {
		return fmt.Errorf("memory: write: %w", err)
	}
	delete(s.by, k)
	delete(s.origins, k)
	delete(s.usage, k)
	delete(s.props, k)
	if err := s.kept.Put(tenant, owner, stored{Next: s.next, Exported: true}); err != nil {
		return fmt.Errorf("memory: write: %w", err)
	}
	return nil
}

// Remove deletes one note.
func (s *Store) Remove(tenant, agent, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key(tenant, agent)
	i := slices.IndexFunc(s.by[k], func(n Note) bool { return n.ID == id })
	if i < 0 {
		return fmt.Errorf("%w: note %q", ErrNotFound, id)
	}
	return s.set(tenant, agent, slices.Delete(slices.Clone(s.by[k]), i, i+1), s.next)
}

// Prompt renders an agent's notes for the start of a turn, or "" for none.
func (s *Store) Prompt(tenant, agent string) string {
	return s.PromptAs(tenant, agent, "What you remember from earlier conversations:")
}

// projectPrefix marks a memory owner that is a project rather than an agent.
// An agent's name cannot contain a colon, so the two never collide.
const projectPrefix = "project:"

// ProjectOwner is the owner under which a project's shared notes are kept.
// Every agent answering in that project, or below it, is sent them. The
// root's, the workspace's own, is "project:" with no name after it: it holds
// the colon too, so no agent's name can be it.
func ProjectOwner(project string) string { return projectPrefix + project }

// OwnerProject reports the project an owner names, if it names one: "project:"
// names the root, "".
func OwnerProject(owner string) (string, bool) { return strings.CutPrefix(owner, projectPrefix) }

// PromptAs renders an owner's approved notes under a heading, or "" for none.
func (s *Store) PromptAs(tenant, owner, heading string) string {
	return s.PromptAt(tenant, owner, heading, time.Now())
}

// PromptAt excludes expired notes at the turn clock, including after replay.
func (s *Store) PromptAt(tenant, owner, heading string, at time.Time) string {
	var b strings.Builder
	for _, n := range s.List(tenant, owner) {
		// A proposal nobody has approved is not a memory yet.
		if n.Status != Approved || !n.ExpiresAt.IsZero() && !n.ExpiresAt.After(at) {
			continue
		}
		if b.Len() == 0 {
			b.WriteString(heading)
		}
		b.WriteString("\n- ")
		b.WriteString(n.Text)
	}
	return b.String()
}

// PersonalOwner is private to one human subject, never an agent or project name.
func PersonalOwner(subject string) string     { return "person:" + subject }
func OwnerPerson(owner string) (string, bool) { return strings.CutPrefix(owner, "person:") }

// UsedRendered records only approved, unexpired notes actually present in the
// composed turn. A preview never calls this method.
func (s *Store) UsedRendered(tenant, owner, text string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	notes := slices.Clone(s.by[key(tenant, owner)])
	changed := false
	for i, n := range notes {
		if n.Status == Approved && (n.ExpiresAt.IsZero() || n.ExpiresAt.After(at)) && strings.Contains(text, "\n- "+n.Text) {
			notes[i].Uses++
			notes[i].LastUsedAt = at
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return s.set(tenant, owner, notes, s.next)
}

// Review identifies a human decision without replacing original provenance.
type Review struct {
	By string
	At time.Time
}

// Edit corrects text without replacing original provenance or admitting an
// unreviewed proposal. A human correction of an approved note is reviewed.
func (s *Store) Edit(tenant, owner, id, text, by string, at time.Time, details *Details) (Note, error) {
	text, err := clean(text)
	if err != nil {
		return Note{}, err
	}
	if by == "" || at.IsZero() {
		return Note{}, ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	notes := slices.Clone(s.by[key(tenant, owner)])
	index := slices.IndexFunc(notes, func(note Note) bool { return note.ID == id })
	if index < 0 {
		return Note{}, ErrNotFound
	}
	note := notes[index]
	if details != nil {
		if details.Kind == "" {
			details.Kind = note.Kind
		}
		if details.Kind == "" {
			details.Kind = Fact
		}
		if details.Kind != Fact && details.Kind != Decision && details.Kind != Preference && details.Kind != PersonKind && details.Kind != Risk {
			return Note{}, ErrInvalid
		}
		if !details.ExpiresAt.IsZero() && !details.ExpiresAt.After(at) && !details.ExpiresAt.Equal(note.ExpiresAt) {
			return Note{}, ErrInvalid
		}
		note.Details = *details
	}
	for _, other := range notes {
		if other.ID != id && strings.EqualFold(other.Text, text) {
			return Note{}, ErrInvalid
		}
	}
	note.Text = text
	note.EditedBy = by
	note.EditedAt = at
	if note.Status == Approved {
		note.ReviewedBy = by
		note.ReviewedAt = at
	}
	notes[index] = note
	if err := s.set(tenant, owner, notes, s.next); err != nil {
		return Note{}, err
	}
	return note, nil
}
