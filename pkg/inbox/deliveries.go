package inbox

import (
	"fmt"
	"maps"
	"strings"
	"time"
	"unicode"
)

// Delivered records that a client posted a message somewhere outside the
// inbox, such as an announcement posted to a Slack channel. It is the client's
// receipt: after a restart, an edit or a withdrawal it updates that post
// instead of posting again. Like reading, it changes nothing anyone else
// sees, so it is kept and replayed but never announced.
const Delivered What = "delivered"

// Delivery is where one client posted a message.
type Delivery struct {
	// Channel is where it was posted, such as a Slack channel id.
	Channel string `json:"channel"`
	// TS identifies the post there, such as a Slack message ts.
	TS string `json:"ts"`
	// At is when the client last said the post shows the message as it
	// stands: when it was posted, or when the client recorded it again
	// after bringing it up to date. A message updated after At has changed
	// since its post last showed it.
	At time.Time `json:"at"`
}

// MaxDeliveries bounds the clients one message records: a workspace has at
// most this many.
const MaxDeliveries = 100

// maxDeliveryField bounds a client id, a channel and a ts.
const maxDeliveryField = 64

func deliveryField(s string) bool {
	return s != "" && len(s) <= maxDeliveryField && !strings.ContainsFunc(s, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r)
	})
}

// cloneDeliveries returns a copy of a message's deliveries.
func cloneDeliveries(d map[string]Delivery) map[string]Delivery {
	if d == nil {
		return nil
	}
	return maps.Clone(d)
}

// postKey is where a client's post is found: the client, the channel and the
// post there.
func postKey(client string, d Delivery) string {
	return client + "\x00" + d.Channel + "\x00" + d.TS
}

// delivered applies a Delivered change, and reports whether it changed
// anything. The same post recorded again moves the receipt's time on only
// when the message changed since it was recorded: the client brought the
// post up to date. Otherwise it changes nothing, so a client that reports
// one post however often records it once a change.
func (m *Message) delivered(c Change) bool {
	if c.Delivery == nil || c.By == "" {
		return false
	}
	have, ok := m.Deliveries[c.By]
	if ok && have.Channel == c.Delivery.Channel && have.TS == c.Delivery.TS &&
		(!m.UpdatedAt.After(have.At) || !c.Delivery.At.After(have.At)) {
		return false
	}
	if !ok && len(m.Deliveries) >= MaxDeliveries {
		return false
	}
	if m.Deliveries == nil {
		m.Deliveries = map[string]Delivery{}
	}
	m.Deliveries[c.By] = *c.Delivery
	return true
}

// Deliver records where a client posted a message. allow is asked about the
// message first, under the store's lock: a message it refuses is not found,
// as one that does not exist.
func (s *Store) Deliver(tenantName, id, client string, d Delivery, allow func(Message) bool) error {
	if !deliveryField(client) || !deliveryField(d.Channel) || !deliveryField(d.TS) {
		return fmt.Errorf("%w: a delivery names a client, a channel and a post, each 1 to %d characters with no spaces", ErrInvalid, maxDeliveryField)
	}
	s.mu.Lock()
	m := s.of(tenantName).byID[id]
	visible := m != nil && (allow == nil || allow(m.clone()))
	s.mu.Unlock()
	if !visible {
		return fmt.Errorf("%w: message %q", ErrNotFound, id)
	}
	d.At = d.At.UTC()
	_, _, err := s.do(Change{Tenant: tenantName, Message: id, What: Delivered, By: client, At: d.At, Delivery: &d})
	return err
}

// ByDelivery returns the message a client last recorded posting at a place,
// such as the announcement a Slack app posted at a channel and ts, for the
// platform's own use: who may read it is the caller's to decide. A message
// whose receipt moved to another post is no longer found at the old one.
func (s *Store) ByDelivery(tenantName, client string, at Delivery) (Message, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.of(tenantName).byPost[postKey(client, at)]
	if m == nil {
		return Message{}, false
	}
	return m.clone(), true
}
