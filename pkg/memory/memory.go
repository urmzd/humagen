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
// The store reads and writes per request (pkg/state, per request): an owner's
// notes are read from its tree in pkg/drive on every call, through the
// drive's cache of what never changes, and every change is written to the
// tree at the head it read. It holds nothing in memory, so any number of
// processes on one database agree, and it does not need pkg/state's
// single-writer lock.
package memory

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"slices"
	"strings"
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
// drive.Store, read per request: it holds no note in memory.
type Store struct {
	drive *drive.Store
	// kept is what is kept beside the files: use counts and reviewed
	// origins, one record per owner.
	kept *state.Table[stored]
}

// usage is how often a note was sent, kept beside its file.
type usage struct {
	Uses       uint64    `json:"uses,omitempty"`
	LastUsedAt time.Time `json:"last_used_at,omitzero"`
}

// stored is what an owner keeps beside its files. Next was a counter that
// ordered notes before an owner's notes ordered themselves; it is read by
// nothing and kept only as it was written.
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

// namespace is where what is kept beside the files is: the tenant, then the
// owner as the key.
const namespace = "memory"

// exported marks, in the tenant "", that every owner's notes kept before
// notes were files have been moved into files.
const exported = "files:v1"

// NewStore returns an empty store in memory.
func NewStore() *Store {
	s, err := Open(nil, nil)
	if err != nil {
		panic(err)
	}
	return s
}

// Open returns a store whose notes are files in d, keeping what is not
// content in st. A nil st is a state.Store in memory; a nil d is a drive on
// st, content included.
//
// Notes kept in st before notes were files are exported to d once, the first
// time any process opens the store: approved ones are committed, pending ones
// proposed. The export is safe to repeat and to run in two processes at once:
// a note already in the tree is not written again, an owner is marked
// exported only after its notes are in place, and the store is marked only
// after every owner is.
func Open(st state.Store, d *drive.Store) (*Store, error) {
	if st == nil {
		st = state.NewMemory()
	}
	if d == nil {
		var err error
		if d, err = drive.Open(st, blob.NewState(st), drive.WithScreen(Screen)); err != nil {
			return nil, err
		}
	}
	s := &Store{drive: d, kept: state.NewTable[stored](st, namespace)}
	if err := s.exportAll(); err != nil {
		return nil, err
	}
	return s, nil
}

// exportAll moves every owner's notes from before notes were files into its
// tree, once.
func (s *Store) exportAll() error {
	if _, err := s.kept.Lookup("", exported); err == nil {
		return nil
	} else if !errors.Is(err, state.ErrNotFound) {
		return fmt.Errorf("memory: load: %w", err)
	}
	type owner struct {
		tenant, owner string
		r             stored
	}
	var legacy []owner
	err := s.kept.Load(func(tenant, key string, r stored) error {
		if tenant != "" && !r.Exported {
			legacy = append(legacy, owner{tenant, key, r})
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("memory: load: %w", err)
	}
	for _, o := range legacy {
		if err := s.export(o.tenant, o.owner, o.r); err != nil {
			return fmt.Errorf("memory: export %q: %w", o.owner, err)
		}
	}
	if err := s.kept.Put("", exported, stored{Exported: true}); err != nil {
		return fmt.Errorf("memory: export: %w", err)
	}
	return nil
}

// export moves an owner's notes from before notes were files into its tree:
// what is kept beside them first, then the notes the tree does not hold yet,
// then the mark.
func (s *Store) export(tenant, owner string, r stored) error {
	notes := slices.Clone(r.Notes)
	for i := range notes {
		// The old list's order is the notes' order.
		notes[i].seq = i + 1
	}
	_, err := s.kept.Update(tenant, owner, func(cur stored, _ bool) (stored, error) {
		for _, n := range notes {
			if origin := r.Origins[n.ID]; origin != "" {
				cur.Origins = setIn(cur.Origins, n.ID, origin)
			}
			if n.Uses > 0 || !n.LastUsedAt.IsZero() {
				cur.Usage = setIn(cur.Usage, n.ID, usage{Uses: n.Uses, LastUsedAt: n.LastUsedAt})
			}
		}
		return cur, nil
	})
	if err != nil {
		return err
	}
	if _, err := s.change(tenant, owner, func(cur []Note) ([]Note, error) {
		for _, n := range notes {
			if !slices.ContainsFunc(cur, func(x Note) bool { return x.ID == n.ID }) {
				cur = append(cur, n)
			}
		}
		return cur, nil
	}); err != nil {
		return err
	}
	_, err = s.kept.Update(tenant, owner, func(cur stored, _ bool) (stored, error) {
		if cur.Exported && cur.Notes == nil {
			return cur, state.ErrUnchanged
		}
		cur.Notes, cur.Exported = nil, true
		return cur, nil
	})
	return err
}

func setIn[V any](m map[string]V, k string, v V) map[string]V {
	if m == nil {
		m = map[string]V{}
	}
	m[k] = v
	return m
}

func key(tenant, agent string) string { return tenant + "\x00" + agent }

// read returns an owner's notes as its tree holds them, with how often each
// was used.
func (s *Store) read(tenant, owner string) ([]Note, error) {
	tree, err := TreeOf(owner)
	if err != nil {
		return nil, err
	}
	ctx, cancel := ctxTimeout()
	defer cancel()
	head, err := s.drive.Head(ctx, tenant, drive.System, tree)
	if err != nil {
		return nil, fmt.Errorf("memory: read: %w", err)
	}
	open, err := s.drive.Proposals(tenant, drive.System, tree)
	if err != nil {
		return nil, fmt.Errorf("memory: read: %w", err)
	}
	notes, _, err := s.notesOf(ctx, tenant, head, open)
	if err != nil {
		return nil, fmt.Errorf("memory: read: %w", err)
	}
	if len(notes) == 0 {
		return nil, nil
	}
	return s.withUsage(tenant, owner, notes)
}

// withUsage fills in how often each note was used.
func (s *Store) withUsage(tenant, owner string, notes []Note) ([]Note, error) {
	k, err := s.kept.Lookup(tenant, owner)
	if errors.Is(err, state.ErrNotFound) {
		return notes, nil
	}
	if err != nil {
		return nil, fmt.Errorf("memory: read: %w", err)
	}
	for i, n := range notes {
		if u, ok := k.Value.Usage[n.ID]; ok {
			notes[i].Uses, notes[i].LastUsedAt = u.Uses, u.LastUsedAt
		}
	}
	return notes, nil
}

// change rewrites an owner's notes: fn is given them as the tree holds them
// and returns what they become, and the difference is written to the tree in
// one change at the head it read. When another process changed the tree in
// between, fn is called again on what it holds now. It returns the notes as
// fn left them, and forgets the use counts and origins of the notes it
// removed.
func (s *Store) change(tenant, owner string, fn func(notes []Note) ([]Note, error)) ([]Note, error) {
	tree, err := TreeOf(owner)
	if err != nil {
		return nil, err
	}
	ctx, cancel := ctxTimeout()
	defer cancel()
	var removed []string
	var out []Note
	_, err = s.drive.Edit(ctx, tenant, drive.System, tree, func(head drive.Version, open []drive.Proposal) (drive.EditInput, error) {
		notes, props, err := s.notesOf(ctx, tenant, head, open)
		if err != nil {
			return drive.EditInput{}, err
		}
		want, err := fn(slices.Clone(notes))
		if err != nil {
			return drive.EditInput{}, err
		}
		var in drive.EditInput
		in, removed = plan(notes, props, want)
		out = want
		return in, nil
	})
	if err != nil {
		if errors.Is(err, ErrInvalid) || errors.Is(err, ErrNotFound) || errors.Is(err, ErrReviewConflict) || errors.Is(err, ErrSensitive) {
			return nil, err
		}
		return nil, fmt.Errorf("memory: write: %w", err)
	}
	if len(removed) > 0 {
		// Best effort: a count or an origin of a note that is gone names
		// nothing.
		_, _ = s.kept.Update(tenant, owner, func(cur stored, ok bool) (stored, error) {
			if !ok {
				return cur, state.ErrUnchanged
			}
			changed := false
			for _, id := range removed {
				if _, had := cur.Origins[id]; had {
					delete(cur.Origins, id)
					changed = true
				}
				if _, had := cur.Usage[id]; had {
					delete(cur.Usage, id)
					changed = true
				}
			}
			if !changed {
				return cur, state.ErrUnchanged
			}
			return cur, nil
		})
	}
	return out, nil
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
	id := "m_" + rand.Text()
	var note Note
	_, err = s.change(tenant, agent, func(notes []Note) ([]Note, error) {
		for _, n := range notes {
			if strings.EqualFold(n.Text, text) {
				note = n
				return notes, nil
			}
		}
		next := nextSeq(notes)
		notes, err := makeRoom(notes, status)
		if err != nil {
			return nil, err
		}
		note = Note{Details: details, ID: id, Text: text, Source: source, Status: status, By: by, At: at, seq: next}
		if status == Approved {
			note.ReviewedBy = by
			note.ReviewedAt = at
		}
		return append(notes, note), nil
	})
	if err != nil {
		return Note{}, err
	}
	if note.ID != id {
		// The note was already held: report it with its use count.
		if got, err := s.withUsage(tenant, agent, []Note{note}); err == nil {
			note = got[0]
		}
	}
	return note, nil
}

// nextSeq is the place after every note an owner holds.
func nextSeq(notes []Note) int {
	n := 0
	for _, x := range notes {
		n = max(n, x.seq)
	}
	return n + 1
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
	approved := 0
	var refused error
	_, err := s.change(tenant, agent, func(notes []Note) ([]Note, error) {
		approved, refused = 0, nil
		next := nextSeq(notes)
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
			note.seq = next
			next++
			if len(reviews) == 1 {
				note.ReviewedBy = reviews[0].By
				note.ReviewedAt = reviews[0].At
			}
			notes = append(rest, note)
			approved++
		}
		return notes, nil
	})
	if err != nil {
		return 0, err
	}
	return approved, refused
}

// Proposal is a pending note and the agent it is for.
type Proposal struct {
	Agent string `json:"agent"`
	Note  Note   `json:"note"`
}

// Pending returns every proposal waiting in a tenant, oldest first. It reads
// the notes of the first drive.MaxTrees trees the tenant holds.
func (s *Store) Pending(tenant string) ([]Proposal, error) {
	ctx, cancel := ctxTimeout()
	defer cancel()
	trees, _, err := s.drive.Trees(ctx, tenant, "", drive.MaxTrees)
	if err != nil {
		return nil, fmt.Errorf("memory: read: %w", err)
	}
	var out []Proposal
	for _, tree := range trees {
		owner, ok := OwnerOf(tree)
		if !ok {
			continue
		}
		notes, err := s.read(tenant, owner)
		if err != nil {
			return nil, err
		}
		for _, n := range notes {
			if n.Status == Pending {
				out = append(out, Proposal{Agent: owner, Note: n})
			}
		}
	}
	slices.SortStableFunc(out, func(a, b Proposal) int { return a.Note.At.Compare(b.Note.At) })
	return out, nil
}

// List returns an agent's notes, oldest first.
func (s *Store) List(tenant, agent string) ([]Note, error) {
	return s.read(tenant, agent)
}

// Drop forgets everything an owner holds, for when the owner is deleted: its
// tree, history included.
func (s *Store) Drop(tenant, owner string) error {
	tree, err := TreeOf(owner)
	if err != nil {
		return err
	}
	ctx, cancel := ctxTimeout()
	defer cancel()
	if err := s.drive.Drop(ctx, tenant, tree, originMemory); err != nil {
		return fmt.Errorf("memory: write: %w", err)
	}
	if _, err := s.kept.Update(tenant, owner, func(cur stored, ok bool) (stored, error) {
		if !ok || cur.Origins == nil && cur.Usage == nil && cur.Notes == nil {
			return cur, state.ErrUnchanged
		}
		return stored{Exported: true}, nil
	}); err != nil {
		return fmt.Errorf("memory: write: %w", err)
	}
	return nil
}

// Remove deletes one note.
func (s *Store) Remove(tenant, agent, id string) error {
	_, err := s.change(tenant, agent, func(notes []Note) ([]Note, error) {
		i := slices.IndexFunc(notes, func(n Note) bool { return n.ID == id })
		if i < 0 {
			return nil, fmt.Errorf("%w: note %q", ErrNotFound, id)
		}
		return slices.Delete(notes, i, i+1), nil
	})
	return err
}

// Prompt renders an agent's notes for the start of a turn, or "" for none.
func (s *Store) Prompt(tenant, agent string) (string, error) {
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
func (s *Store) PromptAs(tenant, owner, heading string) (string, error) {
	return s.PromptAt(tenant, owner, heading, time.Now())
}

// PromptAt excludes expired notes at the turn clock.
func (s *Store) PromptAt(tenant, owner, heading string, at time.Time) (string, error) {
	notes, err := s.List(tenant, owner)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, n := range notes {
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
	return b.String(), nil
}

// PersonalOwner is private to one human subject, never an agent or project name.
func PersonalOwner(subject string) string     { return "person:" + subject }
func OwnerPerson(owner string) (string, bool) { return strings.CutPrefix(owner, "person:") }

// UsedRendered records only approved, unexpired notes actually present in the
// composed turn. A preview never calls this method.
func (s *Store) UsedRendered(tenant, owner, text string, at time.Time) error {
	notes, err := s.List(tenant, owner)
	if err != nil {
		return err
	}
	var used []string
	for _, n := range notes {
		if n.Status == Approved && (n.ExpiresAt.IsZero() || n.ExpiresAt.After(at)) && strings.Contains(text, "\n- "+n.Text) {
			used = append(used, n.ID)
		}
	}
	if len(used) == 0 {
		return nil
	}
	// A count is added to what is kept, at the version read, so counts
	// from two processes at once are both kept.
	if _, err := s.kept.Update(tenant, owner, func(cur stored, _ bool) (stored, error) {
		for _, id := range used {
			u := cur.Usage[id]
			u.Uses++
			u.LastUsedAt = at
			cur.Usage = setIn(cur.Usage, id, u)
		}
		cur.Exported = true
		return cur, nil
	}); err != nil {
		return fmt.Errorf("memory: write: %w", err)
	}
	return nil
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
	var note Note
	_, err = s.change(tenant, owner, func(notes []Note) ([]Note, error) {
		index := slices.IndexFunc(notes, func(note Note) bool { return note.ID == id })
		if index < 0 {
			return nil, ErrNotFound
		}
		note = notes[index]
		if details != nil {
			d := *details
			if d.Kind == "" {
				d.Kind = note.Kind
			}
			if d.Kind == "" {
				d.Kind = Fact
			}
			if d.Kind != Fact && d.Kind != Decision && d.Kind != Preference && d.Kind != PersonKind && d.Kind != Risk {
				return nil, ErrInvalid
			}
			if !d.ExpiresAt.IsZero() && !d.ExpiresAt.After(at) && !d.ExpiresAt.Equal(note.ExpiresAt) {
				return nil, ErrInvalid
			}
			note.Details = d
		}
		for _, other := range notes {
			if other.ID != id && strings.EqualFold(other.Text, text) {
				return nil, ErrInvalid
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
		return notes, nil
	})
	if err != nil {
		return Note{}, err
	}
	if got, err := s.withUsage(tenant, owner, []Note{note}); err == nil {
		note = got[0]
	}
	return note, nil
}
