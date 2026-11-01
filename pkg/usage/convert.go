package usage

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/urmzd/humagen/pkg/state"
)

// convert moves the entries an earlier release kept, every tenant's in the
// tenant "" under its place alone (state.Seq), to the layout this one reads
// (keys.go), once: layoutMarker records that it is done.
//
// Each entry moves in one Write that deletes the old record at the version
// read, so processes converting at once move each entry once. Without
// shared counters an entry moves by being recorded again, which moves its
// counts with it. With them, the counters already hold the entries' counts,
// and the entries are copied with what each added, from a ledger in memory
// they are recorded into first; the first time the counters are opened, they
// are filled from that ledger (Counters.seed). No entry moves before the
// counters are filled, so the first process to fill them has read every
// entry.
func (l *Ledger) convert() error {
	ctx, cancel := context.WithTimeout(context.Background(), state.Timeout)
	defer cancel()
	if _, err := l.st.Lookup(ctx, namespace, "", layoutMarker); err == nil {
		return nil
	} else if !errors.Is(err, state.ErrNotFound) {
		return err
	}
	old := state.NewTable[Entry](l.st, namespace)
	if l.shared == nil {
		err := eachOld(old, func(it state.Item[Entry]) error {
			_, _, err := recordLocal(l.st, it.Value, func() ([]state.Op, bool, error) {
				cur, err := old.Lookup("", it.Key)
				if errors.Is(err, state.ErrNotFound) {
					return nil, false, nil
				}
				if err != nil {
					return nil, false, err
				}
				return []state.Op{state.DeleteAt("", it.Key, cur.Version)}, true, nil
			})
			return err
		})
		if err != nil {
			return err
		}
		return l.st.Write(ctx, namespace, state.Put("", layoutMarker, []byte(`{}`)))
	}

	mem := NewLedger()
	added := map[string]*Spend{}
	var last int64
	err := eachOld(old, func(it state.Item[Entry]) error {
		n, err := strconv.ParseInt(it.Key, 10, 64)
		if err != nil {
			return fmt.Errorf("an entry is kept under %q", it.Key)
		}
		last = max(last, n)
		a, ok, err := mem.Record(it.Value)
		if err != nil {
			return err
		}
		if ok && (it.Value.Kind == Spent || it.Value.Kind == Settled) {
			added[it.Key] = &a
		}
		return nil
	})
	if err != nil {
		return err
	}
	if err := l.shared.seed(ctx, mem.st, last); err != nil {
		return fmt.Errorf("counters: %w", err)
	}
	var page []state.Item[Entry]
	move := func(items []state.Item[Entry]) error {
		var ops []state.Op
		for _, it := range items {
			n, _ := strconv.ParseInt(it.Key, 10, 64)
			kept, err := entryOps(it.Value, added[it.Key], n)
			if err != nil {
				return err
			}
			ops = append(ops, kept...)
			ops = append(ops, state.DeleteAt("", it.Key, it.Version))
		}
		wctx, cancel := context.WithTimeout(context.Background(), state.Timeout)
		defer cancel()
		return l.st.Write(wctx, namespace, ops...)
	}
	flush := func() error {
		if len(page) == 0 {
			return nil
		}
		err := move(page)
		if errors.Is(err, state.ErrConflict) {
			// Another process moved some of them: move the rest one by one.
			for _, it := range page {
				if err := move([]state.Item[Entry]{it}); err != nil && !errors.Is(err, state.ErrConflict) {
					return err
				}
			}
			err = nil
		}
		page = page[:0]
		return err
	}
	err = eachOld(old, func(it state.Item[Entry]) error {
		page = append(page, it)
		if len(page) < 200 {
			return nil
		}
		return flush()
	})
	if err == nil {
		err = flush()
	}
	if err != nil {
		return err
	}
	return l.st.Write(ctx, namespace, state.Put("", layoutMarker, []byte(`{}`)))
}

// eachOld calls fn for every entry an earlier release kept, oldest first, a
// page at a time.
func eachOld(old *state.Table[Entry], fn func(state.Item[Entry]) error) error {
	after := ""
	for {
		// Seq keys are 20 digits, the first a 0 for any place a ledger gave.
		items, next, err := old.List("", state.Query{Prefix: "0", After: after, Limit: state.MaxList})
		if err != nil {
			return err
		}
		for _, it := range items {
			if err := fn(it); err != nil {
				return err
			}
		}
		if next == "" {
			return nil
		}
		after = next
	}
}
