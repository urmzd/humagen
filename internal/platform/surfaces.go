package platform

import (
	"fmt"

	"github.com/urmzd/warp/pkg/auth"
	"github.com/urmzd/warp/pkg/clients"
	"github.com/urmzd/warp/pkg/inbox"
	"github.com/urmzd/warp/pkg/projects"
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
// may read itself, as it reads it: of a kind it may read, addressed to it or
// to a project it has a role in (an announcement's audience), and about
// what it may read.
func (p *Platform) RecordDelivery(who auth.Principal, id, client string, d inbox.Delivery) error {
	c, err := p.Clients.Get(who.Tenant, client)
	if err != nil || !machineOf(c, who) {
		return fmt.Errorf("%w: %q", clients.ErrNotFound, client)
	}
	d.At = p.now()
	reader, seer := p.sharedReader(p.Projects.Tree(who.Tenant), who), p.Seer(who)
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

// InboxHistory is what happened to one message as who is shown it: their own
// marks and what changed it for everyone, and a client's receipts only where
// who is that client's machine, as ownDeliveries keeps them.
func (p *Platform) InboxHistory(who auth.Principal, id string) []inbox.Change {
	return p.Inbox.HistoryFor(who.Tenant, id, who.Subject, func(client string) bool {
		c, err := p.Clients.Get(who.Tenant, client)
		return err == nil && machineOf(c, who)
	})
}

// checkHome checks that a client's home is somewhere the workspace has: a
// project, and a channel of it that is not archived. A project or channel
// that is gone later leaves the client's threads unfiled rather than filed
// somewhere else: the home is never swapped for another place.
func (p *Platform) checkHome(tenant string, c clients.Client) error {
	if c.Home == nil {
		return nil
	}
	h := c.HomeOf()
	if h.Project != "" && !p.HasProject(tenant, h.Project) {
		return fmt.Errorf("%w: the home %q is not a project in this workspace", clients.ErrInvalid, c.Home.Project)
	}
	ch, err := p.Projects.Channel(tenant, h.Project, h.Channel)
	if err != nil {
		return fmt.Errorf("%w: the home channel #%s is not a channel of %s", clients.ErrInvalid, h.Channel, c.Home.Project)
	}
	if ch.Archived {
		return fmt.Errorf("%w: the home channel #%s is archived", clients.ErrInvalid, h.Channel)
	}
	return nil
}

// SeesHome holds a client's home to who sets it: a home in a project the
// caller cannot see answers exactly as one in a project that is not there.
func (p *Platform) SeesHome(who auth.Principal, c clients.Client) error {
	if c.Home == nil {
		return nil
	}
	h := c.HomeOf()
	if h.Project != projects.Root && p.HasProject(who.Tenant, h.Project) && !p.Seer(who).Project(h.Project) {
		return fmt.Errorf("%w: the home %q is not a project in this workspace", clients.ErrInvalid, c.Home.Project)
	}
	return nil
}

// ActingFor is the person a client's machine reads for: the Slack person
// as, mapped through the client's people to their subject here, with the
// roles the workspace's policy grants it. Only the client's machine reads
// for its people; to anyone else the client is not found.
func (p *Platform) ActingFor(who auth.Principal, as ActAs) (auth.Principal, error) {
	c, err := p.Clients.Get(who.Tenant, as.Client)
	if err != nil || !machineOf(c, who) {
		return auth.Principal{}, fmt.Errorf("%w: %q", clients.ErrNotFound, as.Client)
	}
	return p.Acting(who, as)
}

// principalOf is the principal a person a surface reads or acts for is: their
// subject, the roles the workspace's policy grants it, under the policy the
// surface's credential is decided by.
func principalOf(who auth.Principal, person inbox.Reader) auth.Principal {
	roles := make([]auth.Role, 0, len(person.Roles))
	for _, r := range person.Roles {
		roles = append(roles, auth.Role(r))
	}
	return auth.Principal{Subject: person.Subject, Name: person.Subject, Tenant: who.Tenant, Kind: auth.User, Roles: roles}.WithPolicy(who.Policy())
}
