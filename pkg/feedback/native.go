package feedback

import (
	"errors"
	"github.com/urmzd/humagen/pkg/state"
	"strings"
	"time"
)

// SaveNative atomically records a native event's identity and its rating. A
// retry returns the originally saved value without overwriting a newer rating.
func (s *Store) SaveNative(tenant, id string, r Record) (Record, error) {
	if id == "" || len(id) > 512 || tenant == "" || r.Run == "" || r.Subject == "" || (r.Rating != "up" && r.Rating != "down") || len(r.Comment) > 4000 || len(r.Reason) > 80 || len(r.FindingID) > 200 {
		return Record{}, errors.New("feedback: invalid native record")
	}
	r.Comment, r.Reason = strings.TrimSpace(r.Comment), strings.TrimSpace(r.Reason)
	var out Record
	err := state.Retry(func() error { var err error; out, err = s.save(tenant, r, id); return err })
	return out, err
}

// RemoveNative records a reaction removal once, and only removes the matching
// rating. Removing a stale thumbs-up never clears a later thumbs-down.
func (s *Store) RemoveNative(tenant, id string, r Record) (Record, error) {
	out, _, err := s.removeNative(tenant, id, "", r)
	return out, err
}

// RemoveNativeSource withdraws only the exact assessment created by source.
// Its ownership marker distinguishes a later same-rating or console update.
func (s *Store) RemoveNativeSource(tenant, id, source string, r Record) (Record, bool, error) {
	if source == "" || len(source) > 512 {
		return Record{}, false, errors.New("feedback: invalid native source")
	}
	return s.removeNative(tenant, id, source, r)
}

func (s *Store) removeNative(tenant, id, source string, r Record) (Record, bool, error) {
	if id == "" || len(id) > 512 || tenant == "" || r.Run == "" || r.Subject == "" || (r.Rating != "up" && r.Rating != "down") {
		return Record{}, false, errors.New("feedback: invalid native removal")
	}
	var out Record
	var applied bool
	err := state.Retry(func() error {
		applied = false
		var previous Record
		_, err := s.lookup(tenant, "native:"+hash(id), &previous)
		if err == nil {
			out = previous
			_, err = s.lookup(tenant, "native-removal:"+hash(id), &applied)
			if errors.Is(err, state.ErrNotFound) {
				return nil
			}
			return err
		}
		if !errors.Is(err, state.ErrNotFound) {
			return err
		}
		out = r
		out.UpdatedAt = time.Now().UTC()
		w := &write{}
		w.put(tenant, "native:"+hash(id), out, state.Missing)
		var n int64
		v, err := s.lookup(tenant, subjectKey(r.Run, r.Subject, r.FindingID), &n)
		if errors.Is(err, state.ErrNotFound) {
			w.put(tenant, "native-removal:"+hash(id), false, state.Missing)
			return w.commit(s.st)
		}
		if err != nil {
			return err
		}
		var old Record
		rv, err := s.lookup(tenant, recordKey(r.Run, n), &old)
		if err != nil {
			return err
		}
		matches := old.Rating == r.Rating
		if matches && source != "" {
			var owner string
			_, err := s.lookup(tenant, nativeOwnerKey(r), &owner)
			if err != nil && !errors.Is(err, state.ErrNotFound) {
				return err
			}
			matches = err == nil && owner == source
		}
		if matches {
			applied = true
			if err := s.touchChange(w, tenant, r.Run, r.Subject, out.UpdatedAt); err != nil {
				return err
			}
			w.ops = append(w.ops, state.DeleteAt(tenant, subjectKey(r.Run, r.Subject, r.FindingID), v), state.DeleteAt(tenant, recordKey(r.Run, n), rv), state.Delete(tenant, ownKey(old)))
			w.ops = append(w.ops, state.Delete(tenant, nativeOwnerKey(r)))
		}
		w.put(tenant, "native-removal:"+hash(id), applied, state.Missing)
		return w.commit(s.st)
	})
	return out, applied, err
}

func nativeOwnerKey(r Record) string {
	return "native-owner:" + subjectKey(r.Run, r.Subject, r.FindingID)
}
