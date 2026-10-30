package memory

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	saigememory "github.com/urmzd/saige/agent/memory"

	"github.com/urmzd/humagen/pkg/drive"
	"github.com/urmzd/humagen/pkg/state"
)

// Notes are files.
//
// Each owner's notes are a tree in pkg/drive: an approved note is the file
// notes/<id>.md at the tree's head, and a pending one is a proposal that
// writes it. A note's text is the file's body, and its provenance is a short
// header above it. Which tree an owner is:
//
//	an agent's name    drive.Agent("", agent)  /project/memory/<agent>
//	project:<name>     drive.Project(name)     /project/memory/@project
//	person:<subject>   drive.Person(subject)   /me/memory
//
// An agent's notes are the workspace's, as they always were: the root
// project's tree for that agent.
//
// How often a note was used, and the run a reviewed note came from, are not
// content, so they are kept beside the files and not in them: counting a use
// does not write a version.

// originMemory marks the changes this package writes, so it does not read
// back its own writes when the drive reports them.
const originMemory = "memory"

// notesDir holds an owner's notes in its tree.
const notesDir = "notes/"

// TreeOf is the tree an owner's notes are kept in.
func TreeOf(owner string) (drive.Tree, error) {
	var t drive.Tree
	switch {
	case strings.HasPrefix(owner, "person:"):
		t = drive.Person(strings.TrimPrefix(owner, "person:"))
	case strings.HasPrefix(owner, projectPrefix):
		t = drive.Project(strings.TrimPrefix(owner, projectPrefix))
	default:
		t = drive.Agent("", owner)
	}
	if err := t.Valid(); err != nil {
		return drive.Tree{}, fmt.Errorf("%w: owner %q", ErrInvalid, owner)
	}
	return t, nil
}

// OwnerOf is the owner whose notes a tree holds, if it holds an owner's
// notes.
func OwnerOf(t drive.Tree) (string, bool) {
	switch t.Kind {
	case drive.PersonKind:
		return PersonalOwner(t.Person), true
	case drive.ProjectKind:
		return ProjectOwner(t.Project), true
	case drive.AgentKind:
		if t.Project == "" {
			return t.Agent, true
		}
	}
	return "", false
}

// NotePath is where a note is kept in its owner's tree.
func NotePath(id string) string { return notesDir + id + ".md" }

func noteID(path string) (string, bool) {
	rest, ok := strings.CutPrefix(path, notesDir)
	if !ok {
		return "", false
	}
	id, ok := strings.CutSuffix(rest, ".md")
	if !ok || id == "" || strings.ContainsAny(id, "/ ") || len(id) > 64 {
		return "", false
	}
	return id, true
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// render writes a note as a file: a header of its provenance, then its text.
// Only what a person reviews is in it: use counts are kept beside it.
func render(n Note) []byte {
	var b strings.Builder
	b.WriteString("---\n")
	field := func(k, v string) {
		if v = oneLine(v); v != "" {
			b.WriteString(k + ": " + v + "\n")
		}
	}
	at := func(k string, t time.Time) {
		if !t.IsZero() {
			field(k, t.UTC().Format(time.RFC3339Nano))
		}
	}
	if n.seq > 0 {
		field("seq", strconv.Itoa(n.seq))
	}
	field("kind", string(n.Kind))
	field("source", string(n.Source))
	field("by", n.By)
	at("at", n.At)
	at("expires_at", n.ExpiresAt)
	field("reviewed_by", n.ReviewedBy)
	at("reviewed_at", n.ReviewedAt)
	field("edited_by", n.EditedBy)
	at("edited_at", n.EditedAt)
	b.WriteString("---\n")
	b.WriteString(n.Text)
	b.WriteString("\n")
	return []byte(b.String())
}

// split separates a file's header from its body. A file without a header is
// all body.
func split(data string) (map[string]string, string) {
	head := map[string]string{}
	rest, ok := strings.CutPrefix(data, "---\n")
	if !ok {
		return head, data
	}
	header, body, ok := strings.Cut(rest, "\n---\n")
	if !ok {
		if header, ok = strings.CutSuffix(rest, "\n---"); !ok {
			return map[string]string{}, data
		}
		body = ""
	}
	for _, line := range strings.Split(header, "\n") {
		if k, v, ok := strings.Cut(line, ":"); ok {
			head[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return head, body
}

// parse reads a note file. A file someone wrote without a header is a note
// by whoever wrote the version it is in. A file whose text is not fit for a
// note is not one.
func parse(id string, data []byte, v drive.Version) (Note, bool) {
	head, body := split(string(data))
	text, err := clean(body)
	if err != nil {
		return Note{}, false
	}
	when := func(k string) time.Time {
		t, _ := time.Parse(time.RFC3339Nano, head[k])
		return t
	}
	n := Note{ID: id, Text: text, Source: Source(head["source"]), By: head["by"], At: when("at"),
		ReviewedBy: head["reviewed_by"], ReviewedAt: when("reviewed_at"), EditedBy: head["edited_by"], EditedAt: when("edited_at")}
	n.Kind = Kind(head["kind"])
	n.ExpiresAt = when("expires_at")
	n.seq, _ = strconv.Atoi(head["seq"])
	if n.Kind == "" {
		n.Kind = Fact
	}
	if n.Source == "" {
		n.Source = Person
	}
	if n.By == "" {
		n.By = v.By
	}
	if n.At.IsZero() {
		n.At = v.At
	}
	return n, true
}

// Screen is saige's memory policy over a file in a tree that is not a
// person's own: a note's text, or any other file's content. It is the
// drive's screen.
func Screen(_ drive.Tree, path string, data []byte) error {
	text := string(data)
	if _, ok := noteID(path); ok {
		_, text = split(text)
	}
	if _, err := (saigememory.Policy{}).CheckContent(context.Background(), saigememory.Scope{}, text); err != nil {
		if errors.Is(err, saigememory.ErrSensitive) {
			return fmt.Errorf("%w: %v", ErrSensitive, err)
		}
		return err
	}
	return nil
}

func ctxTimeout() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), state.Timeout)
}

// load reads an owner's notes from its tree: the head's notes are approved,
// the open proposals' are pending. The caller holds the lock.
func (s *Store) load(ctx context.Context, tenant, owner string) error {
	tree, err := TreeOf(owner)
	if err != nil {
		return err
	}
	k := key(tenant, owner)
	head, err := s.drive.Head(ctx, tenant, drive.System, tree)
	if err != nil {
		return err
	}
	var notes []Note
	for _, e := range head.Entries {
		id, ok := noteID(e.Path)
		if !ok {
			continue
		}
		b, err := s.drive.Content(ctx, tenant, e.Hash)
		if err != nil {
			return err
		}
		if n, ok := parse(id, b, head); ok {
			n.Status = Approved
			notes = append(notes, n)
		}
	}
	proposals, err := s.drive.Proposals(tenant, drive.System, tree)
	if err != nil {
		return err
	}
	props := map[string]string{}
	for _, p := range proposals {
		for _, c := range p.Changes {
			id, ok := noteID(c.Path)
			if !ok || c.Delete || slices.ContainsFunc(notes, func(n Note) bool { return n.ID == id && n.Status == Pending }) {
				continue
			}
			b, err := s.drive.Content(ctx, tenant, c.Hash)
			if err != nil {
				return err
			}
			n, ok := parse(id, b, drive.Version{By: p.By, At: p.At})
			if !ok {
				continue
			}
			n.Status = Pending
			// A proposal to change an approved note shows as the pending
			// note; the approved one stays in use until it is decided.
			if i := slices.IndexFunc(notes, func(x Note) bool { return x.ID == id }); i >= 0 {
				continue
			}
			props[id] = p.ID
			notes = append(notes, n)
		}
	}
	slices.SortStableFunc(notes, func(a, b Note) int {
		switch {
		case a.seq != b.seq && a.seq > 0 && b.seq > 0:
			return a.seq - b.seq
		case a.seq > 0 && b.seq == 0:
			return -1
		case a.seq == 0 && b.seq > 0:
			return 1
		}
		if c := a.At.Compare(b.At); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
	for i, n := range notes {
		if u, ok := s.usage[k][n.ID]; ok {
			notes[i].Uses, notes[i].LastUsedAt = u.Uses, u.LastUsedAt
		}
		s.next = max(s.next, n.seq)
	}
	s.props[k] = props
	if len(notes) == 0 {
		delete(s.by, k)
		return nil
	}
	s.by[k] = notes
	return nil
}

// watch reads an owner's notes again when something other than this package
// changed its tree: a run's end, a restore, an approval through the files
// API.
func (s *Store) watch(e drive.Event) {
	if e.Origin == originMemory {
		return
	}
	owner, ok := OwnerOf(e.Tree)
	if !ok {
		return
	}
	ctx, cancel := ctxTimeout()
	defer cancel()
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.load(ctx, e.Tenant, owner)
}

// write makes an owner's tree hold notes: approved notes at the head,
// pending ones as proposals. The caller holds the lock. What fails part way
// is read back from the tree, so the notes in memory are always the tree's.
func (s *Store) write(tenant, owner string, notes []Note) error {
	tree, err := TreeOf(owner)
	if err != nil {
		return err
	}
	ctx, cancel := ctxTimeout()
	defer cancel()
	k := key(tenant, owner)
	old := map[string]Note{}
	for _, n := range s.by[k] {
		old[n.ID] = n
	}
	want := map[string]Note{}
	for _, n := range notes {
		want[n.ID] = n
	}
	props := s.props[k]
	var changes []drive.Change
	var resolves []string
	type proposal struct {
		id   string
		note Note
	}
	var propose []proposal
	var discard []string
	for _, n := range notes {
		o, had := old[n.ID]
		switch n.Status {
		case Approved:
			if had && o.Status == Approved && string(render(o)) == string(render(n)) {
				continue
			}
			changes = append(changes, drive.Change{Path: NotePath(n.ID), Data: render(n)})
			if had && o.Status == Pending && props[n.ID] != "" {
				resolves = append(resolves, props[n.ID])
			}
		case Pending:
			if had && o.Status == Pending && string(render(o)) == string(render(n)) {
				continue
			}
			id := props[n.ID]
			if id == "" {
				id = n.ID
			}
			propose = append(propose, proposal{id: id, note: n})
		}
	}
	for _, o := range s.by[k] {
		n, kept := want[o.ID]
		switch {
		case o.Status == Approved && (!kept || n.Status != Approved):
			changes = append(changes, drive.Change{Path: NotePath(o.ID), Delete: true})
		case o.Status == Pending && !kept && props[o.ID] != "":
			discard = append(discard, props[o.ID])
		}
	}
	failed := func(err error) error {
		_ = s.load(ctx, tenant, owner)
		return err
	}
	wrote := false
	if len(changes) > 0 || len(resolves) > 0 {
		if len(changes) == 0 {
			// A proposal whose note is already what it proposes is closed.
			// It is discarded as this package's own change, so the drive does
			// not report it back while the lock is held.
			for _, id := range resolves {
				if err := s.drive.Discard(ctx, tenant, drive.System, tree, id, originMemory); err != nil {
					return failed(err)
				}
			}
		} else if _, err := s.drive.Commit(ctx, tenant, drive.System, tree, drive.CommitInput{Base: drive.Head, Changes: changes, Resolves: resolves, By: originMemory, Origin: originMemory}); err != nil {
			if wrote {
				return failed(err)
			}
			return err
		}
		wrote = true
	}
	for _, p := range propose {
		if _, err := s.drive.Propose(ctx, tenant, drive.System, tree, drive.ProposeInput{ID: p.id, Base: drive.Head, Changes: []drive.Change{{Path: NotePath(p.note.ID), Data: render(p.note)}}, By: p.note.By, At: p.note.At, Origin: originMemory, Message: "memory note"}); err != nil {
			if wrote {
				return failed(err)
			}
			return err
		}
		wrote = true
	}
	for _, id := range discard {
		if err := s.drive.Discard(ctx, tenant, drive.System, tree, id, originMemory); err != nil && !errors.Is(err, drive.ErrNotFound) {
			if wrote {
				return failed(err)
			}
			return err
		}
		wrote = true
	}
	next := map[string]string{}
	for _, n := range notes {
		if n.Status != Pending {
			continue
		}
		if id := props[n.ID]; id != "" {
			next[n.ID] = id
		} else {
			next[n.ID] = n.ID
		}
	}
	s.props[k] = next
	return nil
}
