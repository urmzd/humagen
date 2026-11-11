package platform

import (
	"slices"
	"time"

	"github.com/urmzd/humagen/pkg/alerts"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/inbox"
)

// CreateSessionAlert records attention and its compatibility transport. The
// notification worker repairs an interrupted projection from the durable alert.
func (p *Platform) CreateSessionAlert(tenant string, a alerts.Alert) (alerts.Alert, error) {
	a, err := p.Alerts.CreateSession(tenant, a)
	if err != nil {
		return a, err
	}
	return a, p.inboxAlertCompletion(tenant, a)
}

func sessionAlertFor(a alerts.Alert, who auth.Principal) bool {
	if len(a.Who) == 0 {
		return true
	}
	for _, target := range a.Who {
		if target == who.Subject {
			return true
		}
		for _, role := range who.Roles {
			if target == alerts.RolePrefix+string(role) {
				return true
			}
		}
	}
	return false
}

// sessionAlertDeliveries maps transport rows to canonical attention. Only
// visible recipients may repair a missing transport; neither this read nor a
// transport's unread flag changes an alert's state.
func (p *Platform) sessionAlertDeliveries(who auth.Principal, at time.Time) (map[string]alerts.Alert, error) {
	all, err := p.Alerts.Alerts(who.Tenant)
	if err != nil {
		return nil, err
	}
	out := map[string]alerts.Alert{}
	seer := p.Seer(who)
	for _, a := range all {
		if a.Session == "" {
			continue
		}
		stored := a
		a = a.View(at)
		if a.Projection != "" {
			out[a.Projection] = stored
			continue
		}
		m, ok := p.Inbox.ByKey(who.Tenant, alertKey(a.ID))
		if !ok && a.State == alerts.OpenState && who.Can(auth.AlertsRead) && sessionAlertFor(a, who) && seer.Alert(a) {
			if err := p.inboxAlertCompletion(who.Tenant, a); err != nil {
				return nil, err
			}
			m, ok = p.Inbox.ByKey(who.Tenant, alertKey(a.ID))
		}
		if ok {
			out[m.ID] = stored
		}
	}
	return out, nil
}

// notificationPolicy rechecks canonical state and access immediately before
// dispatch. Transport reads never resolve or suppress canonical attention.
func (p *Platform) notificationPolicy(who auth.Principal, m inbox.Message, at time.Time, attention map[string]alerts.Alert) (string, bool) {
	a, canonical := attention[m.ID]
	if !canonical && m.Source.Alert != "" {
		var err error
		a, err = p.Alerts.Alert(who.Tenant, m.Source.Alert)
		canonical = err == nil && a.Session != ""
	}
	if canonical {
		current, err := p.Alerts.Alert(who.Tenant, a.ID)
		if err != nil {
			return "", false
		}
		a = current.View(at)
		return string(a.Priority), a.State == alerts.OpenState && who.Can(auth.AlertsRead) && sessionAlertFor(a, who) && p.Seer(who).Alert(a)
	}
	return string(m.Kind), who.Can(reads(m.Kind)) && p.Seer(who).Message(m) && m.Unread && !m.Hidden && m.State == inbox.Open
}

// prioritizeSessions preserves recency within each priority. Snoozed,
// resolved, inaccessible and another person's alerts never raise a row.
func (p *Platform) prioritizeSessions(who auth.Principal, rows []SessionSummary) error {
	all, err := p.Alerts.Alerts(who.Tenant)
	if err != nil {
		return err
	}
	ranks := map[string]int{}
	seer := p.Seer(who)
	for _, a := range all {
		a = a.View(p.now())
		if a.State != alerts.OpenState || !who.Can(auth.AlertsRead) || !sessionAlertFor(a, who) || !seer.Alert(a) {
			continue
		}
		rank := 1
		switch a.Priority {
		case alerts.Urgent:
			rank = 3
		case alerts.Normal:
			rank = 2
		}
		if rank > ranks[a.Session] {
			ranks[a.Session] = rank
		}
	}
	for i := range rows {
		rows[i].rank = ranks[rows[i].ID]
	}
	slices.SortStableFunc(rows, func(a, b SessionSummary) int { return b.rank - a.rank })
	return nil
}
