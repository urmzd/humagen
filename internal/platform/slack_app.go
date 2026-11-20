package platform

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"

	"github.com/urmzd/humagen/pkg/clients"
)

// SlackApp is the Slack app a Platform document has the server start
// (apps/slack), as far as its client record goes: the team it serves, the
// subject its credential signs in as, and the Slack people it acts for.
type SlackApp struct {
	Team    string
	Machine string
	People  map[string]string
}

// SlackAppClient is the id of the client record of the app a server starts
// in a Slack team. It is made from the team, so the apps of two teams are two
// records, and it is the same at every start.
func SlackAppClient(team string) string { return "slack-app-" + strings.ToLower(team) }

// ApplySlackApp makes the client record of the Slack app a server starts
// what its Platform document says, and returns it. With none, it is made:
// enabled, routing to any agent with no default of its own, so a message that
// names no agent is answered as it is with no client, by the default where it
// lands, and a person may still name any agent they may use.
//
// On a record an earlier start kept, only what the file owns is written: the
// team, the machine and the people, added and removed. Its default agent,
// its agents, its home, whether it is enabled and every other option are an
// administrator's, and stay as they are. An unchanged record keeps its
// version. A client of that id that is not such an app is left as it is and
// answers ErrExists.
func (p *Platform) ApplySlackApp(_ context.Context, tenant, by string, app SlackApp) (clients.Client, error) {
	p.clientsMu.Lock()
	defer p.clientsMu.Unlock()

	id := SlackAppClient(app.Team)
	owned := func(c clients.Client) clients.Client {
		opts := clients.SlackOptions{}
		if c.Slack != nil {
			opts = *c.Slack
		}
		opts.Machine, opts.People = app.Machine, maps.Clone(app.People)
		c.Identity, c.Slack = clients.Identity{Team: app.Team}, &opts
		return c
	}
	old, err := p.Clients.Get(tenant, id)
	if errors.Is(err, clients.ErrNotFound) {
		return p.create(tenant, by, ClientInput{slackApp: true, Client: owned(clients.Client{
			ID: id, Name: "Slack app", Surface: clients.Slack, Managed: clients.ManagedApp,
			Enabled: true, Agents: []string{clients.AnyAgent},
		})})
	}
	if err != nil {
		return clients.Client{}, err
	}
	if old.Managed != clients.ManagedApp {
		return clients.Client{}, fmt.Errorf("%w: %q is not the Slack app the server starts", clients.ErrExists, id)
	}
	next := owned(old)
	if sameRecord(next, old) {
		return old, nil
	}
	return p.replace(old, next, by, ClientInput{Client: next})
}

// ClearSlackApps takes the machine and the people from every record of a
// Slack app the server starts in a tenant but keep, the one its Platform
// document still maps people for. A file that stops mapping people, or drops
// the surface, so leaves nobody for a credential to act for; the record and
// what an administrator chose on it stay.
func (p *Platform) ClearSlackApps(_ context.Context, tenant, by, keep string) error {
	p.clientsMu.Lock()
	defer p.clientsMu.Unlock()

	all, err := p.Clients.List(tenant)
	if err != nil {
		return err
	}
	for _, c := range all {
		if c.Managed != clients.ManagedApp || c.ID == keep || c.Slack == nil || (c.Slack.Machine == "" && len(c.Slack.People) == 0) {
			continue
		}
		opts := *c.Slack
		opts.Machine, opts.People = "", nil
		next := c
		next.Slack = &opts
		if _, err := p.replace(c, next, by, ClientInput{Client: next}); err != nil {
			return err
		}
	}
	return nil
}

// slackAppKeeps is a change to the record of a Slack app the server starts,
// with what the file owns put back: its identity, its machine and its people
// are changed in the Platform document, not through the API.
func slackAppKeeps(old, c clients.Client) clients.Client {
	opts := clients.SlackOptions{}
	if c.Slack != nil {
		opts = *c.Slack
	}
	opts.Machine, opts.People = "", nil
	if old.Slack != nil {
		opts.Machine, opts.People = old.Slack.Machine, old.Slack.People
	}
	c.Identity, c.Surface, c.Managed, c.Slack = old.Identity, old.Surface, old.Managed, &opts
	return c
}
