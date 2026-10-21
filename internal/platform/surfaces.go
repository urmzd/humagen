package platform

import (
	"fmt"

	"github.com/urmzd/warp/pkg/auth"
	"github.com/urmzd/warp/pkg/clients"
	"github.com/urmzd/warp/pkg/inbox"
)

// What a client's own surface host, such as the Slack app, records through
// the API about what it posted. Only the credential a client names as its
// machine speaks for it: to anyone else the client is not found, so trying
// confirms nothing.

// machineOf reports whether a caller is the credential an enabled Slack
// client names as the machine that acts for it.
func machineOf(c clients.Client, who auth.Principal) bool {
	return who.Kind == auth.Machine && who.Subject != "" && c.Surface == clients.Slack && c.Enabled &&
		c.Slack != nil && c.Slack.Machine == who.Subject
}

// RecordDelivery records where a client posted an inbox message, such as the
// channel and ts of an announcement it posted to Slack, so it updates that
// post later instead of posting again. The message must be one the machine
// may read itself: of a kind it may read, and addressed to it.
func (p *Platform) RecordDelivery(who auth.Principal, id, client string, d inbox.Delivery) error {
	c, err := p.Clients.Get(who.Tenant, client)
	if err != nil || !machineOf(c, who) {
		return fmt.Errorf("%w: %q", clients.ErrNotFound, client)
	}
	d.At = p.now()
	reader, seer := readerOf(who), p.Seer(who)
	return p.Inbox.Deliver(who.Tenant, id, c.ID, d, func(m inbox.Message) bool { return who.Can(reads(m.Kind)) && m.For(reader) && seer.Message(m) })
}

// ownDeliveries keeps, of a message's deliveries, only the receipt of the
// client whose machine is reading: where another client posted something is
// that client's business.
func (p *Platform) ownDeliveries(who auth.Principal, m inbox.Message) inbox.Message {
	if len(m.Deliveries) == 0 {
		return m
	}
	var kept map[string]inbox.Delivery
	for id, d := range m.Deliveries {
		if c, err := p.Clients.Get(who.Tenant, id); err == nil && machineOf(c, who) {
			if kept == nil {
				kept = map[string]inbox.Delivery{}
			}
			kept[id] = d
		}
	}
	m.Deliveries = kept
	return m
}

// checkHome checks that a client's home is somewhere the workspace has. A
// project that is gone later leaves the client's threads unfiled rather than
// filed somewhere else: the home is never swapped for another place.
func (p *Platform) checkHome(tenant string, c clients.Client) error {
	if c.Home == nil {
		return nil
	}
	h := c.HomeOf()
	if h.Project != "" && !p.HasProject(tenant, h.Project) {
		return fmt.Errorf("%w: the home %q is not a project in this workspace", clients.ErrInvalid, c.Home.Project)
	}
	if h.Channel != clients.HomeChannel {
		return fmt.Errorf("%w: a client's home is the %s channel until channels are built", clients.ErrInvalid, clients.HomeChannel)
	}
	return nil
}
