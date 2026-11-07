package alerts

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/urmzd/humagen/pkg/state"
)

// Priority describes how soon a person needs to act, independently of read state.
type Priority string

const (
	Urgent Priority = "urgent"
	Normal Priority = "normal"
	Low    Priority = "low"
)

// AlertState is the durable decision on an alert. Reading never resolves it.
type AlertState string

const (
	OpenState AlertState = "open"
	Snoozed   AlertState = "snoozed"
	Resolved  AlertState = "resolved"
)

// View supplies defaults for older records and wakes elapsed snoozes without
// relying on a process-local timer. It does not mutate the stored record.
func (a Alert) View(at time.Time) Alert {
	if a.Priority == "" {
		a.Priority = Normal
		if a.Severity == Critical {
			a.Priority = Urgent
		}
	}
	if a.State == "" {
		a.State = OpenState
		if a.AckedBy != "" {
			a.State = Resolved
		}
	}
	if a.State == Snoozed && !at.Before(a.SnoozedUntil) {
		a.State = OpenState
		a.SnoozedUntil = time.Time{}
	}
	if a.ResolvedBy == "" {
		a.ResolvedBy, a.ResolvedAt = a.AckedBy, a.AckedAt
	}
	if len(a.Who) == 0 {
		a.Who = slices.Clone(a.Notify)
	}
	return a
}

// CreateSession raises a person or agent's alert in the same persistent engine
// as rule alerts. The caller must authorize the session and recipients first.
func (e *Engine) CreateSession(tenant string, a Alert) (Alert, error) {
	a.Title = strings.TrimSpace(a.Title)
	if tenant == "" || a.Session == "" || a.Title == "" || len(a.Title) > 500 || len(a.Body) > 10000 || len(a.ResolveWhen) > 500 {
		return Alert{}, fmt.Errorf("%w: a session and a title of at most 500 characters are required", ErrInvalid)
	}
	if a.Priority != Urgent && a.Priority != Normal && a.Priority != Low {
		return Alert{}, fmt.Errorf("%w: priority must be urgent, normal or low", ErrInvalid)
	}
	switch a.Action {
	case "", "answer", "approve", "retry", "reconnect":
	default:
		return Alert{}, fmt.Errorf("%w: unknown alert action", ErrInvalid)
	}
	if err := validNotify(a.Who); err != nil {
		return Alert{}, err
	}
	if a.At.IsZero() {
		return Alert{}, fmt.Errorf("%w: creation time is required", ErrInvalid)
	}
	a.State, a.Severity = OpenState, Warning
	if a.Priority == Urgent {
		a.Severity = Critical
	}
	a.Notify = slices.Clone(a.Who)
	a.Kind = Kind("session")
	var out Alert
	err := state.Retry(func() error {
		w, err := e.raising(tenant)
		if err != nil {
			return err
		}
		if err = w.add(a, ""); err != nil {
			return err
		}
		if err = w.commit(); err != nil {
			return err
		}
		out = w.raised[0]
		return nil
	})
	return out, wrap(err)
}

// ChangeState resolves, snoozes or reopens an alert atomically across processes.
// Reopening is the Undo operation. A rule alert cannot reclaim an open key
// already occupied by a newer alert for the same condition.
func (e *Engine) ChangeState(tenant, id, by string, next AlertState, until, at time.Time) (Alert, error) {
	if by == "" || at.IsZero() {
		return Alert{}, fmt.Errorf("%w: actor and time are required", ErrInvalid)
	}
	switch next {
	case OpenState, Resolved:
		if !until.IsZero() {
			return Alert{}, fmt.Errorf("%w: only snoozed alerts take snoozed_until", ErrInvalid)
		}
	case Snoozed:
		if !until.After(at) || until.After(at.Add(30*24*time.Hour)) {
			return Alert{}, fmt.Errorf("%w: snooze must end within the next 30 days", ErrInvalid)
		}
	default:
		return Alert{}, fmt.Errorf("%w: state must be open, snoozed or resolved", ErrInvalid)
	}
	var out Alert
	err := state.Retry(func() error {
		cur, err := e.kept.Lookup(tenant, alertKey(id))
		if errors.Is(err, state.ErrNotFound) || err == nil && (cur.Value.Alert == nil || cur.Value.Alert.ID != id) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		a := *cur.Value.Alert
		if next == Resolved && !a.Open() {
			out = a.alert().View(at)
			return nil
		}
		wasOpen := a.Open()
		a.State, a.SnoozedUntil = next, until
		a.AckedBy, a.ResolvedBy = "", ""
		a.AckedAt, a.ResolvedAt = time.Time{}, time.Time{}
		if next == Resolved {
			a.AckedBy, a.ResolvedBy = by, by
			a.AckedAt, a.ResolvedAt = at, at
		}
		b := e.kept.Batch()
		b.PutAt(tenant, alertKey(id), stored{Alert: &a}, cur.Version)
		if next == Resolved {
			if err = e.release(b, tenant, a); err != nil {
				return err
			}
		} else if !wasOpen && a.Scope != "" {
			held, err := e.kept.Lookup(tenant, a.Scope)
			if err == nil && held.Value.Open != id {
				return fmt.Errorf("%w: a newer alert already watches this condition", ErrInvalid)
			}
			if errors.Is(err, state.ErrNotFound) {
				b.Create(tenant, a.Scope, stored{Open: id})
			} else if err != nil {
				return err
			}
		}
		if err = b.Commit(); err != nil {
			return err
		}
		out = a.alert().View(at)
		return nil
	})
	return out, wrap(err)
}

// ProjectSession records a lifecycle alert once. Replaying the same event after
// resolution returns the original alert rather than raising attention again.
func (e *Engine) ProjectSession(tenant, key string, a Alert) (Alert, error) {
	if tenant == "" || key == "" || a.Session == "" {
		return Alert{}, fmt.Errorf("%w: lifecycle alert needs a session and event key", ErrInvalid)
	}
	record := "session-event/" + hash(key)
	var out Alert
	err := state.Retry(func() error {
		cur, err := e.kept.Lookup(tenant, record)
		if err == nil && len(cur.Value.Completion) > 0 {
			out = cur.Value.Completion[0]
			if current, e := e.Alert(tenant, out.ID); e == nil {
				out = current
			}
			return nil
		}
		if err != nil && !errors.Is(err, state.ErrNotFound) {
			return err
		}
		w, err := e.raising(tenant)
		if err != nil {
			return err
		}
		a = a.View(a.At)
		if err = w.add(a, ""); err != nil {
			return err
		}
		w.b.Create(tenant, record, stored{Completion: w.raised})
		if err = w.commit(); err != nil {
			return err
		}
		out = w.raised[0]
		return nil
	})
	return out, wrap(err)
}

// AttachSession anchors a legacy rule alert when its owning session is known.
func (e *Engine) AttachSession(tenant, id, session, action string) (Alert, error) {
	if session == "" {
		return Alert{}, fmt.Errorf("%w: session is required", ErrInvalid)
	}
	var out Alert
	_, err := e.kept.Update(tenant, alertKey(id), func(s stored, exists bool) (stored, error) {
		if !exists || s.Alert == nil || s.Alert.ID != id {
			return s, ErrNotFound
		}
		a := *s.Alert
		if a.Session != "" && a.Session != session {
			return s, fmt.Errorf("%w: alert already belongs to another session", ErrInvalid)
		}
		a.Session = session
		if action != "" {
			a.Action = action
		}
		s.Alert = &a
		out = a.alert()
		return s, nil
	})
	return out, wrap(err)
}
