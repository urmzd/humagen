package platform

import (
	"time"

	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/inbox"
	"github.com/urmzd/humagen/pkg/notifications"
	"github.com/urmzd/humagen/pkg/prefs"
)

type NotificationItem struct {
	Inbox   string     `json:"inbox"`
	Kind    inbox.Kind `json:"kind"`
	Subject string     `json:"subject"`
	Project string     `json:"project,omitempty"`
	Thread  string     `json:"thread,omitempty"`
	At      time.Time  `json:"at"`
}
type Notification struct {
	ID    string             `json:"id"`
	Mode  prefs.Delivery     `json:"mode"`
	At    time.Time          `json:"at"`
	Items []NotificationItem `json:"items"`
}

// NativeNotifications is the authenticated native-console delivery worker.
// Each poll discovers a bounded inbox page and reconciles pending delivery with
// the current identity. It is safe after restart because only opaque references
// and durable delivery IDs are retained. It changes no authoritative inbox row.
func (p *Platform) NativeNotifications(who auth.Principal) ([]Notification, error) {
	if who.Kind != auth.User || who.Subject == "" {
		return nil, ErrForbidden
	}
	tree := p.Projects.Tree(who.Tenant)
	reader := p.inboxReader(tree, who)
	seer := p.Seer(who)
	now := p.now()
	cut := now.Add(-p.inboxRetention(who.Tenant))
	cache := map[string]inbox.Message{}
	lookup := func(id string) (notifications.Candidate, bool) {
		m, ok := cache[id]
		if !ok {
			var err error
			m, err = p.Inbox.GetAs(who.Tenant, id, reader)
			if err != nil {
				return notifications.Candidate{}, false
			}
			cache[id] = m
		}
		if m.State == inbox.Snoozed && !now.Before(m.SnoozedUntil) {
			m.State, m.Hidden = inbox.Open, false
			cache[id] = m
		}
		if !who.Can(reads(m.Kind)) || !seer.Message(m) || !m.Unread || m.Hidden || m.State != inbox.Open {
			return notifications.Candidate{}, false
		}
		stamp := m.UpdatedAt
		if stamp.IsZero() {
			stamp = m.At
		}
		if stamp.Before(cut) {
			return notifications.Candidate{}, false
		}
		return notifications.Candidate{Message: id, Kind: string(m.Kind), Stamp: stamp}, true
	}
	candidates := []notifications.Candidate{}
	seen := map[string]bool{}
	cursor := p.Notifications.Cursor(who.Tenant, who.Subject)
	nextCursor := cursor
	// The newest page is checked every poll. Four backfill pages advance a
	// durable cursor, so a busy inbox cannot permanently starve older updates.
	before := ""
	for page := 0; page < 5; page++ {
		if page == 1 {
			if cursor == "" {
				before = nextCursor
			} else {
				before = cursor
			}
		}
		if page > 0 && before == "" {
			break
		}
		query := p.inboxQuery(tree, who, inbox.Query{Unread: true, Limit: 100, Before: before})
		rows, next, err := p.Inbox.Page(who.Tenant, query)
		if err != nil {
			return nil, err
		}
		for _, m := range rows {
			cache[m.ID] = m
			if seen[m.ID] {
				continue
			}
			seen[m.ID] = true
			if candidate, visible := lookup(m.ID); visible {
				candidates = append(candidates, candidate)
			}
		}
		nextCursor = next
		before = next
		if next == "" {
			break
		}
	}
	delivered, err := p.Notifications.Reconcile(who.Tenant, who.Subject, nextCursor, candidates, p.Prefs.Get(who.Tenant, who.Subject).Personal, now, cut, lookup)
	if err != nil {
		return nil, err
	}
	out := []Notification{}
	for _, delivery := range delivered {
		n := Notification{ID: delivery.ID, Mode: delivery.Mode, At: delivery.At, Items: []NotificationItem{}}
		for _, id := range delivery.Messages {
			if _, visible := lookup(id); !visible {
				continue
			}
			m := cache[id]
			place := p.CurrentPlace(who.Tenant, Place{Project: m.Source.Project, Thread: m.Source.Thread})
			if m.Source.Run != "" {
				if run, err := p.Runs.Get(who.Tenant, m.Source.Run); err == nil {
					place = p.RunPlace(who.Tenant, run)
				}
			}
			n.Items = append(n.Items, NotificationItem{Inbox: id, Kind: m.Kind, Subject: m.Subject, Project: place.Project, Thread: place.Thread, At: m.At})
		}
		if len(n.Items) > 0 {
			out = append(out, n)
		}
	}
	return out, nil
}
func (p *Platform) AcknowledgeNotification(who auth.Principal, id string) error {
	if who.Kind != auth.User || who.Subject == "" {
		return notifications.ErrNotFound
	}
	reader := p.inboxReader(p.Projects.Tree(who.Tenant), who)
	seer := p.Seer(who)
	return p.Notifications.Ack(who.Tenant, who.Subject, id, func(message string) bool {
		m, err := p.Inbox.GetAs(who.Tenant, message, reader)
		return err == nil && who.Can(reads(m.Kind)) && seer.Message(m)
	})
}
