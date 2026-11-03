package inbox

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/urmzd/humagen/pkg/state"
)

// What Count reads is kept as changes apply, in the same write as the change:
// the messages open for everyone, in buckets of messages that every reader
// is shown alike ("b:"), with how many of each bucket each reader has set
// aside ("o:": read, hidden or deleted) and how many they hid or deleted
// ("s:"). A count adds up buckets, never reading a message that is
// settled, and asks who may see a bucket once, of a message that stands for
// all of it.
//
// What opens again by itself, with time and no change, is kept apart: the
// messages snoozed for everyone ("z:"), each with what decides who reads
// it, and each reader's own snoozes of broadcasts ("y:"). A count reads
// those one by one, and asks who may see a group alike once.

func unmarshal(it item, v any) error {
	if err := json.Unmarshal(it.raw, v); err != nil {
		return fmt.Errorf("inbox: %q: %w", it.key, err)
	}
	return nil
}

// signature names the bucket a message counts in: its kind, whether it is a
// broadcast, whom it is addressed to, and what it is about (Source's run,
// project, thread and agent, and the scopes of its parts). Each field is
// written with its length, so no two lists of fields read alike.
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
	field(m.Source.Knowledge)
	field(m.Source.Run)
	field(m.Source.Project)
	field(m.Source.Thread)
	field(m.Source.Agent)
	for _, part := range scopeParts(m) {
		if part.Operation != nil {
			field("operation")
			field(part.Operation.Kind)
			field(part.Operation.ID)
		}
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
		Source: Source{Knowledge: m.Source.Knowledge, Run: m.Source.Run, Project: m.Source.Project, Thread: m.Source.Thread, Agent: m.Source.Agent},
		Parts:  scopeParts(m),
	}
}

func keptRep(m *Message) keptMessage {
	r := representative(m)
	return keep(&r)
}

// isOut reports whether a reader has set a message aside, so it is not
// unread for them: read it, hid it (or snoozed their own copy of a
// broadcast) or deleted it. Only a broadcast was hidden before a reader
// could hide any message, so the count of older records stands.
func isOut(m *Message, k mark) bool {
	return k.Read || k.Hide != nil || k.Deleted
}

// isAside reports whether a message has left a reader's Inbox by their own
// hand: hidden, snoozed for themselves, or deleted.
func isAside(k mark) bool { return k.Hide != nil || k.Deleted }

// out moves one reader's count of what they set aside in a bucket.
func (x *tx) out(subject, sig string, by int) error {
	return x.tally(outKey(subject, sig), by)
}

// aside moves one reader's count of what they hid or deleted in a bucket.
func (x *tx) aside(subject, sig string, by int) error {
	return x.tally(asideKey(subject, sig), by)
}

// tally moves one reader's counter. It never goes below zero: a mark kept
// before its counter was (a broadcast hidden before "s:" counted it) leaves
// nothing to take away.
func (x *tx) tally(key string, by int) error {
	var o outRec
	if _, err := x.get(key, &o); err != nil {
		return err
	}
	if o.N += by; o.N <= 0 {
		x.del(key)
		return nil
	}
	x.put(key, o)
	return nil
}

// ownSnooze records a reader's own snooze of an open broadcast, or forgets
// it with a zero until.
func (x *tx) ownSnooze(subject, id, sig string, until time.Time) {
	if until.IsZero() {
		x.del(ownSnoozeKey(subject, id))
		return
	}
	x.put(ownSnoozeKey(subject, id), ownSnooze{Until: until, Sig: sig})
}

// join counts a message as it now stands: in its bucket when it is open for
// everyone, among the snoozed when it is snoozed, and nowhere once settled.
func (x *tx) join(m *Message) error {
	switch m.State {
	case Open:
		sig := signature(m)
		var b bucketRec
		if ok, err := x.get(bucketKey(sig), &b); err != nil {
			return err
		} else if !ok {
			b = bucketRec{Sig: sig, Rep: keptRep(m)}
		}
		b.Open++
		x.put(bucketKey(sig), b)
		marks, err := x.marks(m.ID)
		if err != nil {
			return err
		}
		for _, k := range marks {
			if isOut(m, k) {
				if err := x.out(k.Subject, sig, 1); err != nil {
					return err
				}
			}
			if isAside(k) {
				if err := x.aside(k.Subject, sig, 1); err != nil {
					return err
				}
			}
			if m.Broadcast && k.Hide != nil && k.Hide.State == Snoozed {
				x.ownSnooze(k.Subject, m.ID, sig, k.Hide.Until)
			}
		}
	case Snoozed:
		x.put(sleepKey(m.ID), sleepRec{Until: m.SnoozedUntil, Sig: signature(m), Rep: keptRep(m)})
	}
	return nil
}

// leave takes a message out of every count, before its state changes.
func (x *tx) leave(m *Message) error {
	switch m.State {
	case Snoozed:
		x.del(sleepKey(m.ID))
	case Open:
		sig := signature(m)
		var b bucketRec
		if ok, err := x.get(bucketKey(sig), &b); err != nil {
			return err
		} else if ok {
			if b.Open--; b.Open <= 0 {
				x.del(bucketKey(sig))
			} else {
				x.put(bucketKey(sig), b)
			}
		}
		marks, err := x.marks(m.ID)
		if err != nil {
			return err
		}
		for _, k := range marks {
			if isOut(m, k) {
				if err := x.out(k.Subject, sig, -1); err != nil {
					return err
				}
			}
			if isAside(k) {
				if err := x.aside(k.Subject, sig, -1); err != nil {
					return err
				}
			}
			if k.Hide != nil {
				x.ownSnooze(k.Subject, m.ID, sig, time.Time{})
			}
		}
	}
	return nil
}

// remark brings the count up to date after one reader's own marks on a
// message changed from was to k.
func (x *tx) remark(m *Message, was, k mark) error {
	if m.State != Open {
		return nil
	}
	sig := signature(m)
	if by := shift(isOut(m, was), isOut(m, k)); by != 0 {
		if err := x.out(k.Subject, sig, by); err != nil {
			return err
		}
	}
	if by := shift(isAside(was), isAside(k)); by != 0 {
		if err := x.aside(k.Subject, sig, by); err != nil {
			return err
		}
	}
	until := time.Time{}
	if m.Broadcast && k.Hide != nil && k.Hide.State == Snoozed {
		until = k.Hide.Until
	}
	x.ownSnooze(k.Subject, m.ID, sig, until)
	return nil
}

// shift is how a counter moves when a mark goes from was to now.
func shift(was, now bool) int {
	switch {
	case now && !was:
		return 1
	case was && !now:
		return -1
	}
	return 0
}

// count adds up what is open for a reader: each bucket they are shown, less
// what they set aside in it, and what a snooze has let out since. It reads
// the buckets, the reader's own counts, and with a time the messages
// snoozed for everyone and the reader's own snoozes.
func (x *tx) count(f filter) (Counts, error) {
	var c Counts
	items, err := x.list("b:", maxBuckets)
	if err != nil {
		return c, err
	}
	buckets := make(map[string]*bucketRec, len(items))
	admitted := make(map[string]bool, len(items))
	for _, it := range items {
		var b bucketRec
		if err := unmarshal(it, &b); err != nil {
			return c, err
		}
		buckets[it.key[len("b:"):]] = &b
	}
	outs, err := x.list(outPrefix(f.Reader), maxBuckets)
	if err != nil {
		return c, err
	}
	out := make(map[string]int, len(outs))
	for _, it := range outs {
		var o outRec
		if err := unmarshal(it, &o); err != nil {
			return c, err
		}
		out[it.key[len(outPrefix(f.Reader)):]] = o.N
	}
	asides, err := x.list(asidePrefix(f.Reader), maxBuckets)
	if err != nil {
		return c, err
	}
	aside := make(map[string]int, len(asides))
	for _, it := range asides {
		var o outRec
		if err := unmarshal(it, &o); err != nil {
			return c, err
		}
		aside[it.key[len(asidePrefix(f.Reader)):]] = o.N
	}
	for h, b := range buckets {
		rep := b.Rep.message("")
		if admitted[h] = f.admits(rep); !admitted[h] {
			continue
		}
		unread := max(b.Open-out[h], 0)
		in := max(b.Open-aside[h], 0)
		c.Inbox += in
		if rep.Broadcast {
			c.Updates += unread
			continue
		}
		c.Open += in
		c.Unread += unread
	}
	if c.Hidden, err = x.countHidden(f); err != nil {
		return c, err
	}
	if f.Now.IsZero() {
		return c, nil
	}
	sleeping, err := x.list("z:", maxSnoozed)
	if err != nil {
		return c, err
	}
	asked := map[string]bool{}
	for _, it := range sleeping {
		var z sleepRec
		if err := unmarshal(it, &z); err != nil {
			return c, err
		}
		if f.Now.Before(z.Until) {
			continue
		}
		h := hash(z.Sig)
		ok, seen := asked[h]
		if !seen {
			ok = f.admits(z.Rep.message(""))
			asked[h] = ok
		}
		if !ok {
			continue
		}
		id := idOfKey(it.key)
		k, err := x.markOf(id, f.Reader)
		if err != nil {
			return c, err
		}
		if isAside(k) {
			continue
		}
		c.Open++
		c.Inbox++
		if !k.Read {
			c.Unread++
		}
	}
	mine, err := x.list(ownSnoozePrefix(f.Reader), maxBuckets)
	if err != nil {
		return c, err
	}
	for _, it := range mine {
		var y ownSnooze
		if err := unmarshal(it, &y); err != nil {
			return c, err
		}
		if f.Now.Before(y.Until) {
			continue
		}
		h := hash(y.Sig)
		if buckets[h] == nil || !admitted[h] {
			continue
		}
		k, err := x.markOf(idOfKey(it.key), f.Reader)
		if err != nil {
			return c, err
		}
		if k.Deleted {
			continue
		}
		c.Inbox++
		if !k.Read {
			c.Updates++
		}
	}
	return c, nil
}

// countHidden adds up a reader's Hidden view from their "h:" records, at
// most MaxHiddenCount, asking who may read each set alike once. A snooze
// that has run out is in their Inbox again, not here.
func (x *tx) countHidden(f filter) (int, error) {
	n := 0
	asked := map[string]bool{}
	_, err := scan(x.st, x.tenant, hiddenPrefix(f.Reader), "", false, MaxHiddenCount, func(r state.Record) (bool, error) {
		var h hiddenRec
		if err := json.Unmarshal(r.Value, &h); err != nil {
			return false, fmt.Errorf("inbox: %q: %w", r.Key, err)
		}
		if !h.Until.IsZero() && !f.Now.IsZero() && !f.Now.Before(h.Until) {
			return true, nil
		}
		sig := hash(h.Sig)
		ok, seen := asked[sig]
		if !seen {
			ok = f.admits(h.Rep.message(""))
			asked[sig] = ok
		}
		if ok {
			n++
		}
		return true, nil
	})
	return n, err
}

// scopeParts retains only the independent configuration scopes needed by read
// confinement. Their contents, outcomes and action data never enter counters.
func scopeParts(m *Message) []Part {
	var out []Part
	for _, p := range m.Parts {
		switch {
		case p.Operation != nil:
			out = append(out, Part{Operation: &OperationPart{Kind: p.Operation.Kind, ID: p.Operation.ID}})
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
