package threads

import (
	"errors"
	"fmt"
	"time"

	"github.com/urmzd/humagen/pkg/state"
)

// ErrNotAuthor is an edit of a message by someone other than who posted it.
var ErrNotAuthor = errors.New("threads: only who posted a message may correct it")

// Correct replaces the words of a spoken message, for its author: a word the
// transcription heard wrong is put right for every later turn. Only a spoken
// message can be corrected, its tags stay as posted, and the runs it
// already started are not changed.
func (s *Store) Correct(tenantName, id, mid, by, text string, at time.Time) (Message, error) {
	if err := CheckText(text); err != nil {
		return Message{}, err
	}
	var out Message
	changed := false
	err := state.Retry(func() error {
		cur, _, err := s.writable(tenantName, id)
		if err != nil {
			return err
		}
		n := seqOf(mid, cur.Head)
		if n == 0 {
			return fmt.Errorf("%w: message %s of %q", ErrNotFound, mid, id)
		}
		was, v, err := s.message(tenantName, id, n)
		if err != nil {
			return err
		}
		switch {
		case was.By != by:
			return ErrNotAuthor
		case !was.Spoken:
			return fmt.Errorf("%w: only a spoken message can be corrected", ErrInvalid)
		case was.Text == text:
			out, changed = was, false
			return nil
		}
		m := was.clone()
		m.Text = text
		edited := at.UTC()
		m.EditedAt = &edited
		if err := m.validate(); err != nil {
			return err
		}
		w := s.write()
		w.at(tenantName, messageKey(id, n), m, v)
		if err := w.commit(s); err != nil {
			return err
		}
		out, changed = m, true
		return nil
	})
	if err != nil {
		return Message{}, err
	}
	if changed {
		s.tell(tenantName, Notice{ID: id, Message: mid, At: at})
	}
	return settled(out), nil
}
