package threads

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/urmzd/humagen/pkg/state"
)

// kept is a thread an earlier release wrote, read back once to convert it.
type kept struct {
	h       head
	version int64
	msgs    []Message
}

// convert gives the records an earlier release kept, which hold a thread's
// record, its messages and its moves, what this layout reads them by: each
// record its head, the activity index, the surfaces, the surface ids and
// words of its messages, and each tenant its counts. It reads the namespace
// once, the first time any process opens it, and refuses records that do not
// make threads (ErrInvalid), as a release that loaded them did.
//
// Every write it makes is one another process converting at the same time
// makes too: the index entries are the same whoever writes them, a record is
// written at the version read, and one already converted is left, and the
// counts and the mark are created only once.
func (s *Store) convert(st state.Store) error {
	ctx, cancel := context.WithTimeout(context.Background(), state.Timeout)
	defer cancel()
	if _, err := st.Lookup(ctx, namespace, "", converted); err == nil {
		return nil
	} else if !errors.Is(err, state.ErrNotFound) {
		return err
	}
	recs, err := st.Load(ctx, namespace)
	if err != nil {
		return err
	}
	tenants, err := check(recs)
	if err != nil {
		return err
	}
	for tenantName, ths := range tenants {
		c := counts{Projects: map[string]int{}}
		for _, k := range ths {
			c.Total++
			c.Projects[k.h.Project]++
			if k.h.Layout >= layout {
				continue
			}
			if err := s.convertThread(tenantName, k); err != nil {
				return err
			}
		}
		w := s.write()
		w.at(tenantName, keyCounts, c, 0)
		if err := w.commit(s); err != nil && !errors.Is(err, state.ErrConflict) {
			return err
		}
	}
	err = st.Write(ctx, namespace, state.Create("", converted, []byte("{}")))
	if err != nil && !errors.Is(err, state.ErrConflict) {
		return err
	}
	return nil
}

// convertThread writes one thread's indexes, a page at a time, then its
// record with its head at the version read.
func (s *Store) convertThread(tenantName string, k *kept) error {
	h := k.h
	h.Layout, h.Head = layout, len(k.msgs)
	if len(k.msgs) > 0 {
		h.First = clipText(k.msgs[0].Text, MaxTitle)
	}
	w := s.write()
	flush := func(force bool) error {
		if len(w.ops) < state.MaxList && !force {
			return nil
		}
		if err := w.commit(s); err != nil {
			return err
		}
		w = s.write()
		return nil
	}
	w.moved(tenantName, nil, h)
	if h.Surface != nil {
		w.put(tenantName, surfaceKey(*h.Surface), h.ID)
	}
	for i, m := range k.msgs {
		if m.SurfaceID != "" {
			w.put(tenantName, postedKey(h.ID, m.SurfaceID), i+1)
		}
		for _, word := range words(m.Text) {
			w.put(tenantName, wordKey(word, h.ID), struct{}{})
			if err := flush(false); err != nil {
				return err
			}
		}
	}
	if err := flush(true); err != nil {
		return err
	}
	w.at(tenantName, threadKey(h.ID), h, k.version)
	err := w.commit(s)
	if !errors.Is(err, state.ErrConflict) {
		return err
	}
	// Another process converted it first, and may have written to it since.
	now, _, err := s.lookup(tenantName, h.ID)
	if err != nil {
		return err
	}
	if now.Layout < layout {
		return fmt.Errorf("%w: the thread %q changed while it was converted", ErrInvalid, h.ID)
	}
	return nil
}

// check reads the records of every tenant but "", and refuses those that do
// not make threads: a message of a thread that is not there, a gap in a
// thread's messages, a broken history of moves, or a record kept under
// another key.
func check(recs []state.Record) (map[string]map[string]*kept, error) {
	type move struct {
		tenant string
		change PlacementChange
	}
	tenants := map[string]map[string]*kept{}
	surfaces := map[string]string{}
	var moves []move
	of := func(tenantName string) map[string]*kept {
		if tenants[tenantName] == nil {
			tenants[tenantName] = map[string]*kept{}
		}
		return tenants[tenantName]
	}
	for _, r := range recs {
		if r.Tenant == "" {
			continue
		}
		key := r.Key
		switch {
		case strings.HasPrefix(key, keyThread):
			h, err := decodeHead(r.Value)
			if err != nil {
				return nil, fmt.Errorf("thread %q: %w", key, err)
			}
			if threadKey(h.ID) != key {
				return nil, fmt.Errorf("%w: the thread %q is kept under %q", ErrInvalid, h.ID, key)
			}
			if err := h.validate(); err != nil {
				return nil, fmt.Errorf("thread %q: %w", key, err)
			}
			if h.Surface != nil {
				sk := r.Tenant + "\x00" + h.Surface.key()
				if surfaces[sk] != "" {
					return nil, fmt.Errorf("%w: %q and %q are one surface's thread", ErrInvalid, surfaces[sk], h.ID)
				}
				surfaces[sk] = h.ID
			}
			k := of(r.Tenant)[h.ID]
			if k == nil {
				k = &kept{}
				of(r.Tenant)[h.ID] = k
			}
			k.h, k.version = h, r.Version
		case strings.HasPrefix(key, keyPlacement):
			var change PlacementChange
			if err := json.Unmarshal(r.Value, &change); err != nil {
				return nil, err
			}
			if key != placementKey(change.Thread, change.Version) || !ValidID(change.Thread) || change.Version < 1 || change.Version > MaxMoves {
				return nil, fmt.Errorf("%w: invalid placement history", ErrInvalid)
			}
			moves = append(moves, move{r.Tenant, change})
		case strings.HasPrefix(key, keyMessage):
			var m Message
			if err := json.Unmarshal(r.Value, &m); err != nil {
				return nil, fmt.Errorf("message %q: %w", key, err)
			}
			id, seq, ok := strings.Cut(strings.TrimPrefix(key, keyMessage), ":")
			n, err := strconv.Atoi(seq)
			if !ok || err != nil || n < 1 || messageKey(id, n) != key || m.Thread != id || m.ID != MessageID(n) {
				return nil, fmt.Errorf("%w: the message %q of %q is kept under %q", ErrInvalid, m.ID, m.Thread, key)
			}
			if err := m.validate(); err != nil {
				return nil, fmt.Errorf("message %q: %w", key, err)
			}
			// Messages ("m:") sort before their thread's record ("t:"), so
			// a thread is checked once every record is read.
			k := of(r.Tenant)[id]
			if k == nil {
				k = &kept{}
				of(r.Tenant)[id] = k
			}
			k.msgs = append(k.msgs, m)
		case strings.HasPrefix(key, keyActivity), strings.HasPrefix(key, keySurface), strings.HasPrefix(key, keyPosted),
			strings.HasPrefix(key, keyWord), key == keyCounts:
			// What a conversion that stopped part way wrote.
		default:
			return nil, fmt.Errorf("%w: nothing is kept under %q", ErrInvalid, key)
		}
	}
	for _, mv := range moves {
		k := tenants[mv.tenant][mv.change.Thread]
		if k == nil || k.h.ID == "" || mv.change.Version > k.h.Moves {
			return nil, fmt.Errorf("%w: orphan placement history", ErrInvalid)
		}
	}
	histories := map[string]map[int]PlacementChange{}
	for _, mv := range moves {
		hk := mv.tenant + "\x00" + mv.change.Thread
		if histories[hk] == nil {
			histories[hk] = map[int]PlacementChange{}
		}
		histories[hk][mv.change.Version] = mv.change
	}
	for tenantName, ths := range tenants {
		for id, k := range ths {
			// A thread's messages come in the order of their keys; a gap is
			// a lost message, and is refused.
			if k.h.ID == "" && len(k.msgs) > 0 {
				return nil, fmt.Errorf("%w: the message %s belongs to %q, which is missing", ErrInvalid, k.msgs[0].ID, id)
			}
			for i, m := range k.msgs {
				switch n := i + 1; {
				case m.ID != MessageID(n):
					return nil, fmt.Errorf("%w: %q has no message %d before %s", ErrInvalid, id, n, m.ID)
				case strings.HasPrefix(m.Prev, "m-") && seqOf(m.Prev, i) == 0:
					return nil, fmt.Errorf("%w: %s of %q follows %s, which is not before it", ErrInvalid, m.ID, id, m.Prev)
				}
			}
			if k.h.Layout >= layout && k.h.Head != len(k.msgs) && (k.h.Archive == nil || k.h.Archive.Deleted == nil) {
				return nil, fmt.Errorf("%w: %q holds %d messages, not %d", ErrInvalid, id, len(k.msgs), k.h.Head)
			}
			history := histories[tenantName+"\x00"+id]
			if k.h.Moves < 0 || k.h.Moves > MaxMoves || len(history) != k.h.Moves {
				return nil, fmt.Errorf("%w: incomplete placement history", ErrInvalid)
			}
			var last PlacementChange
			for n := 1; n <= k.h.Moves; n++ {
				next, ok := history[n]
				if !ok || n > 1 && (next.FromProject != last.Project || next.FromChannel != last.Channel) {
					return nil, fmt.Errorf("%w: broken placement chain", ErrInvalid)
				}
				last = next
			}
			if k.h.Moves > 0 && (last.Project != k.h.Project || last.Channel != k.h.Channel) {
				return nil, fmt.Errorf("%w: placement does not match history", ErrInvalid)
			}
		}
	}
	return tenants, nil
}
