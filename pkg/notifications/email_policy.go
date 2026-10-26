package notifications

import (
	"github.com/urmzd/humagen/pkg/prefs"
	"github.com/urmzd/humagen/pkg/state"
	"time"
)

// RecheckEmailPolicy revisits prepared but unacknowledged relay messages.
// Native-console delivery remains visible after presentation; email preparation
// must instead obey policy changes until the relay actually accepts the message.
func (s *Store) RecheckEmailPolicy(tenant, subject string, p prefs.Personal, now time.Time) error {
	if s.namespace != emailNamespace || tenant == "" || subject == "" || now.IsZero() {
		return ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	owner := [2]string{tenant, subject}
	before := s.rows[owner]
	rows := make(map[string]Entry, len(before))
	ops := []state.Op{}
	for id, e := range before {
		original := e
		if !e.Acknowledged && !e.Suppressed {
			configured := p.Notifications[e.Kind]
			if configured == "" {
				configured = prefs.Off
			}
			previous := e.PolicyMode
			if previous == "" {
				previous = e.Mode
			}
			quiet := QuietUntil(p, now)
			if configured != previous {
				e.PolicyMode = configured
				e.Mode = p.NotificationDelivery(e.Kind, now)
				e.Due = due(p, e.Kind, now)
				e.DeliveredAt = time.Time{}
				e.Delivery = ""
				e.Suppressed = configured == prefs.Off
			} else if !quiet.IsZero() {
				e.Mode = prefs.Digest
				e.PolicyMode = configured
				if e.Due.Before(quiet) {
					e.Due = quiet
				}
				e.DeliveredAt = time.Time{}
				e.Delivery = ""
			}
		}
		rows[id] = e
		if original != e {
			ops = append(ops, entryOp(tenant, e))
		}
	}
	if err := s.write(ops); err != nil {
		return err
	}
	s.rows[owner] = rows
	return nil
}
