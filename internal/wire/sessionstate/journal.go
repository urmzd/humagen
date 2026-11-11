package sessionstate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/saige/flow/attachment"
	"github.com/urmzd/saige/flow/thread"
)

// Journal is the append-only agent transcript. The sequence and appended batch
// commit together, so retries never expose half an exchange or rewrite a pin.
type Journal struct{ state state.Store }

func NewJournal(st state.Store) *Journal {
	if st == nil {
		st = state.NewMemory()
	}
	return &Journal{state: st}
}

var _ thread.Store = (*Journal)(nil)

func journalKey(k thread.Key) string { b, _ := json.Marshal(k); return hash(string(b)) }
func (j *Journal) head(ctx context.Context, k thread.Key) (uint64, int64, error) {
	if err := k.Validate(); err != nil {
		return 0, 0, err
	}
	r, err := j.state.Lookup(ctx, namespace, k.Tenant, "h:"+journalKey(k))
	if errors.Is(err, state.ErrNotFound) {
		return 0, state.Missing, nil
	}
	if err != nil {
		return 0, 0, err
	}
	var n uint64
	err = json.Unmarshal(r.Value, &n)
	return n, r.Version, err
}
func (j *Journal) Head(ctx context.Context, k thread.Key) (uint64, error) {
	n, _, err := j.head(ctx, k)
	return n, err
}
func (j *Journal) Append(ctx context.Context, k thread.Key, entries ...thread.Entry) ([]thread.Entry, error) {
	if err := k.Validate(); err != nil {
		return nil, err
	}
	for _, e := range entries {
		if !e.Role.Valid() || (strings.TrimSpace(e.Text) == "" && len(e.Attachments) == 0) || (e.RunID == "" && e.SurfaceID == "") {
			return nil, thread.ErrInvalid
		}
		if err := attachment.ValidateAll(e.Attachments); err != nil {
			return nil, fmt.Errorf("%w: %v", thread.ErrInvalid, err)
		}
	}
	var result []thread.Entry
	err := state.Retry(func() error {
		n, v, err := j.head(ctx, k)
		if err != nil {
			return err
		}
		base := journalKey(k)
		result = make([]thread.Entry, 0, len(entries))
		ops := []state.Op{}
		held := map[string]thread.Entry{}
		for _, e := range entries {
			if len(e.Attachments) == 0 {
				e.Attachments = nil
			}
			keys := []string{}
			if e.RunID != "" {
				keys = append(keys, "i:"+base+":"+hash("run:"+e.RunID+"\x00"+string(e.Role)))
			}
			if e.SurfaceID != "" {
				keys = append(keys, "i:"+base+":"+hash("surface:"+e.SurfaceID))
			}
			var old thread.Entry
			found := false
			for _, ik := range keys {
				if existing, ok := held[ik]; ok {
					old = existing
					found = true
					break
				}
				row, err := j.state.Lookup(ctx, namespace, k.Tenant, ik)
				if err == nil {
					if err = json.Unmarshal(row.Value, &old); err != nil {
						return err
					}
					found = true
					break
				}
				if !errors.Is(err, state.ErrNotFound) {
					return err
				}
			}
			if found {
				result = append(result, old)
				continue
			}

			if n >= 10000 {
				return fmt.Errorf("%w: session transcript limit reached", thread.ErrInvalid)
			}
			n++
			e.Seq = n
			if e.At.IsZero() {
				e.At = time.Now()
			}
			e.At = e.At.UTC().Truncate(time.Microsecond)
			for _, ik := range keys {
				held[ik] = e
				ops = append(ops, put(k.Tenant, ik, e, state.Missing))
			}
			result = append(result, e)
			ops = append(ops, put(k.Tenant, "e:"+base+":"+state.Seq(int64(n)), e, state.Missing))
		}
		if len(ops) == 0 {
			return nil
		}
		ops = append(ops, put(k.Tenant, "h:"+base, n, v))
		return j.state.Write(ctx, namespace, ops...)
	})
	return result, err
}
func (j *Journal) History(ctx context.Context, k thread.Key, upTo uint64) ([]thread.Entry, error) {
	head, err := j.Head(ctx, k)
	if err != nil {
		return nil, err
	}
	if upTo > head {
		return nil, thread.ErrGap
	}
	out := []thread.Entry{}
	prefix := "e:" + journalKey(k) + ":"
	after := ""
	for uint64(len(out)) < upTo {
		rows, next, err := j.state.List(ctx, namespace, k.Tenant, state.Query{Prefix: prefix, After: after, Limit: int(min(200, upTo-uint64(len(out))))})
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			var e thread.Entry
			if err = json.Unmarshal(row.Value, &e); err != nil {
				return nil, err
			}
			if e.Seq != uint64(len(out))+1 {
				return nil, thread.ErrGap
			}
			out = append(out, e)
		}
		if next == "" {
			break
		}
		after = next
	}
	if uint64(len(out)) != upTo {
		return nil, thread.ErrGap
	}
	return out, nil
}
