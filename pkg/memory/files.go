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

// originMemory marks the changes this package writes.
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

// notesOf reads an owner's notes from its tree: the head's notes are
// approved, the open proposals' are pending. It returns, too, the drive
// proposal each pending note is.
func (s *Store) notesOf(ctx context.Context, tenant string, head drive.Version, open []drive.Proposal) ([]Note, map[string]string, error) {
	var notes []Note
	for _, e := range head.Entries {
		id, ok := noteID(e.Path)
		if !ok {
			continue
		}
		b, err := s.drive.Content(ctx, tenant, e.Hash)
		if err != nil {
			return nil, nil, err
		}
		if n, ok := parse(id, b, head); ok {
			n.Status = Approved
			notes = append(notes, n)
		}
	}
	open = slices.Clone(open)
	slices.SortFunc(open, func(a, b drive.Proposal) int {
		if c := a.At.Compare(b.At); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
	props := map[string]string{}
	for _, p := range open {
		for _, c := range p.Changes {
			id, ok := noteID(c.Path)
			if !ok || c.Delete || props[id] != "" {
				continue
			}
			// A proposal to change an approved note shows as the approved
			// note, which stays in use until it is decided.
			if slices.ContainsFunc(notes, func(x Note) bool { return x.ID == id }) {
				continue
			}
			b, err := s.drive.Content(ctx, tenant, c.Hash)
			if err != nil {
				return nil, nil, err
			}
			n, ok := parse(id, b, drive.Version{By: p.By, At: p.At})
			if !ok {
				continue
			}
			n.Status = Pending
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
	return notes, props, nil
}

// plan is the change that makes an owner's tree hold want, from old as it
// holds now: approved notes committed to the head, pending ones proposed,
// and what is gone deleted or discarded. It returns, too, the ids of the
// notes want no longer holds.
func plan(old []Note, props map[string]string, want []Note) (drive.EditInput, []string) {
	before := map[string]Note{}
	for _, n := range old {
		before[n.ID] = n
	}
	after := map[string]Note{}
	for _, n := range want {
		after[n.ID] = n
	}
	in := drive.EditInput{By: originMemory, Origin: originMemory}
	for _, n := range want {
		o, had := before[n.ID]
		switch n.Status {
		case Approved:
			if had && o.Status == Approved && string(render(o)) == string(render(n)) {
				continue
			}
			in.Changes = append(in.Changes, drive.Change{Path: NotePath(n.ID), Data: render(n)})
			if had && o.Status == Pending && props[n.ID] != "" {
				in.Resolves = append(in.Resolves, props[n.ID])
			}
		case Pending:
			if had && o.Status == Pending && string(render(o)) == string(render(n)) {
				continue
			}
			id := props[n.ID]
			if id == "" {
				id = n.ID
			}
			in.Propose = append(in.Propose, drive.ProposeInput{ID: id, Base: drive.Head, Changes: []drive.Change{{Path: NotePath(n.ID), Data: render(n)}}, By: n.By, At: n.At, Origin: originMemory, Message: "memory note"})
		}
	}
	var removed []string
	for _, o := range old {
		n, kept := after[o.ID]
		if !kept {
			removed = append(removed, o.ID)
		}
		switch {
		case o.Status == Approved && (!kept || n.Status != Approved):
			in.Changes = append(in.Changes, drive.Change{Path: NotePath(o.ID), Delete: true})
		case o.Status == Pending && !kept && props[o.ID] != "":
			in.Discard = append(in.Discard, props[o.ID])
		}
	}
	return in, removed
}
