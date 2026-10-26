package inbox

import (
	"slices"
	"strconv"
	"strings"
	"time"
)

// tally is what Count reads, kept as changes apply and so rebuilt whenever
// they are replayed: the messages open for everyone, in buckets of messages
// that every reader is shown alike, with how many of each bucket each reader
// has set aside. A count adds up buckets, never reading a message that is
// settled, and asks who may see a bucket once, of a message that stands for
// all of it.
//
// What opens again by itself, with time and no change, is kept apart: the
// messages snoozed for everyone, in groups alike as buckets are, and each
// reader's own snoozes of broadcasts. A count reads those one by one, and
// asks who may see a group once.
type tally struct {
	buckets map[string]*bucket
	// in is the bucket each message open for everyone counts in, by id.
	in map[string]*bucket
	// snoozed are the messages snoozed for everyone, by id, and asleep the
	// same messages in groups alike in what decides who reads them, by
	// signature. A broadcast is never one: each reader snoozes their own
	// copy.
	snoozed map[string]*Message
	asleep  map[string]*group
	// snoozedBy are, for each reader, the open broadcasts they snoozed for
	// themselves, until when: subject -> message id -> until.
	snoozedBy map[string]map[string]time.Time
}

func newTally() tally {
	return tally{buckets: map[string]*bucket{}, in: map[string]*bucket{}, snoozed: map[string]*Message{}, asleep: map[string]*group{}, snoozedBy: map[string]map[string]time.Time{}}
}

// group is the messages snoozed for everyone alike in what decides who reads
// them, as a bucket's are.
type group struct {
	rep  Message
	msgs map[string]*Message
}

// bucket is the open messages alike in everything that decides who reads
// them: their kind, whether they are a broadcast, whom they are addressed
// to, and what they are about (Source's run, project, thread and agent).
type bucket struct {
	sig string
	// rep holds those fields, and only those, for the predicates a count
	// asks.
	rep Message
	// open is how many messages are in the bucket.
	open int
	// out is, for each reader, how many of them the reader has set aside:
	// read, or for a broadcast read or hidden for themselves.
	out map[string]int
}

// signature names the bucket a message counts in. Each field is written
// with its length, so no two lists of fields read alike.
func signature(m *Message) string {
	var b strings.Builder
	field := func(s string) {
		b.WriteString(strconv.Itoa(len(s)))
		b.WriteByte(':')
		b.WriteString(s)
	}
	field(string(m.Kind))
	field(strconv.FormatBool(m.Broadcast))
	field(m.To)
	for _, list := range [][]string{m.Audience, m.Excluded} {
		b.WriteString(strconv.Itoa(len(list)))
		b.WriteByte('|')
		for _, s := range list {
			field(s)
		}
	}
	field(m.Source.Run)
	field(m.Source.Project)
	field(m.Source.Thread)
	field(m.Source.Agent)
	for _, part := range scopeParts(m) {
		if part.Memory != nil {
			field("memory")
			field(part.Memory.Owner)
		}
		if part.Budget != nil {
			field("budget")
			field(part.Budget.ScopeKind)
			field(part.Budget.ScopeName)
		}
		if part.Question != nil {
			field("question")
			field(part.Question.Run)
		}
		if part.Draft != nil {
			field("draft")
			field(part.Draft.Project)
		}
	}
	return b.String()
}

// representative is a message carrying only what a bucket's messages share.
func representative(m *Message) Message {
	return Message{
		Kind: m.Kind, Broadcast: m.Broadcast, To: m.To, Audience: slices.Clone(m.Audience), Excluded: slices.Clone(m.Excluded),
		Source: Source{Run: m.Source.Run, Project: m.Source.Project, Thread: m.Source.Thread, Agent: m.Source.Agent},
		Parts:  scopeParts(m),
	}
}

// isOut reports whether a reader has set a message aside: read it, or hid
// or snoozed their own copy of a broadcast.
func (t *tenant) isOut(m *Message, subject string) bool {
	if t.read[m.ID][subject] {
		return true
	}
	_, hid := t.hidden[m.ID][subject]
	return m.Broadcast && hid
}

// eachMarker calls fn once for every reader who has marked a message: read
// it, unread it, or hid or snoozed their own copy.
func (t *tenant) eachMarker(m *Message, fn func(subject string)) {
	for s := range t.read[m.ID] {
		fn(s)
	}
	for s := range t.hidden[m.ID] {
		if _, also := t.read[m.ID][s]; !also {
			fn(s)
		}
	}
}

// join counts a message as it now stands: in its bucket when it is open for
// everyone, among the snoozed when it is snoozed, and nowhere once settled.
func (t *tenant) join(m *Message) {
	switch m.State {
	case Open:
		sig := signature(m)
		b := t.tally.buckets[sig]
		if b == nil {
			b = &bucket{sig: sig, rep: representative(m), out: map[string]int{}}
			t.tally.buckets[sig] = b
		}
		b.open++
		t.tally.in[m.ID] = b
		t.eachMarker(m, func(s string) {
			if t.isOut(m, s) {
				b.out[s]++
			}
		})
		for s, h := range t.hidden[m.ID] {
			if m.Broadcast && h.state == Snoozed {
				t.snoozeFor(s, m.ID, h.until)
			}
		}
	case Snoozed:
		t.tally.snoozed[m.ID] = m
		sig := signature(m)
		g := t.tally.asleep[sig]
		if g == nil {
			g = &group{rep: representative(m), msgs: map[string]*Message{}}
			t.tally.asleep[sig] = g
		}
		g.msgs[m.ID] = m
	}
}

// leave takes a message out of every count, before its state changes.
func (t *tenant) leave(m *Message) {
	if _, ok := t.tally.snoozed[m.ID]; ok {
		delete(t.tally.snoozed, m.ID)
		sig := signature(m)
		if g := t.tally.asleep[sig]; g != nil {
			if delete(g.msgs, m.ID); len(g.msgs) == 0 {
				delete(t.tally.asleep, sig)
			}
		}
	}
	b := t.tally.in[m.ID]
	if b == nil {
		return
	}
	delete(t.tally.in, m.ID)
	t.eachMarker(m, func(s string) {
		if t.isOut(m, s) {
			b.setAside(s, -1)
		}
	})
	if b.open--; b.open == 0 {
		delete(t.tally.buckets, b.sig)
	}
	for s := range t.hidden[m.ID] {
		t.snoozeFor(s, m.ID, time.Time{})
	}
}

// remark brings the count up to date after one reader's own mark on a
// message changed: wasOut is whether the message was set aside for them
// before.
func (t *tenant) remark(m *Message, subject string, wasOut bool) {
	b := t.tally.in[m.ID]
	if b == nil {
		return
	}
	switch now := t.isOut(m, subject); {
	case now && !wasOut:
		b.setAside(subject, 1)
	case !now && wasOut:
		b.setAside(subject, -1)
	}
	until := time.Time{}
	if h, ok := t.hidden[m.ID][subject]; ok && m.Broadcast && h.state == Snoozed {
		until = h.until
	}
	t.snoozeFor(subject, m.ID, until)
}

func (b *bucket) setAside(subject string, by int) {
	if b.out[subject] += by; b.out[subject] == 0 {
		delete(b.out, subject)
	}
}

// snoozeFor records a reader's own snooze of an open broadcast, or forgets it
// with a zero until.
func (t *tenant) snoozeFor(subject, id string, until time.Time) {
	if until.IsZero() {
		if mine := t.tally.snoozedBy[subject]; mine != nil {
			delete(mine, id)
			if len(mine) == 0 {
				delete(t.tally.snoozedBy, subject)
			}
		}
		return
	}
	if t.tally.snoozedBy[subject] == nil {
		t.tally.snoozedBy[subject] = map[string]time.Time{}
	}
	t.tally.snoozedBy[subject][id] = until
}

// count adds up what is open for a reader: each bucket they are shown, less
// what they set aside in it, and what a snooze has let out since.
func (t *tenant) count(f filter) Counts {
	var c Counts
	for _, b := range t.tally.buckets {
		if !f.admits(&b.rep) {
			continue
		}
		unread := b.open - b.out[f.Reader]
		if b.rep.Broadcast {
			c.Updates += unread
			continue
		}
		c.Open += b.open
		c.Unread += unread
	}
	if f.Now.IsZero() {
		return c
	}
	for _, g := range t.tally.asleep {
		asked, admitted := false, false
		for _, m := range g.msgs {
			if f.Now.Before(m.SnoozedUntil) {
				continue
			}
			if !asked {
				asked, admitted = true, f.admits(&g.rep)
			}
			if !admitted {
				break
			}
			c.Open++
			if !t.read[m.ID][f.Reader] {
				c.Unread++
			}
		}
	}
	for id, until := range t.tally.snoozedBy[f.Reader] {
		if f.Now.Before(until) || t.read[id][f.Reader] {
			continue
		}
		if b := t.tally.in[id]; b != nil && f.admits(&b.rep) {
			c.Updates++
		}
	}
	return c
}

// scopeParts retains only the independent configuration scopes needed by read
// confinement. Their contents, outcomes and action data never enter counters.
func scopeParts(m *Message) []Part {
	var out []Part
	for _, p := range m.Parts {
		switch {
		case p.Question != nil:
			out = append(out, Part{Question: &QuestionPart{Run: p.Question.Run}})
		case p.Memory != nil:
			out = append(out, Part{Memory: &MemoryPart{Owner: p.Memory.Owner}})
		case p.Budget != nil:
			out = append(out, Part{Budget: &BudgetPart{ScopeKind: p.Budget.ScopeKind, ScopeName: p.Budget.ScopeName}})
		case p.Draft != nil:
			out = append(out, Part{Draft: &DraftPart{Project: p.Draft.Project}})
		}
	}
	return out
}
