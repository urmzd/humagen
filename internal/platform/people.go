package platform

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/people"
)

// The directory of people: who is in the workspace, by name and handle. It is
// recorded from three places, and nothing here decides who anyone is or what
// they may do: a person signing in, a client mapping a Slack person to a
// subject, and the policy granting a subject a role.

// ErrNameTaken reports an agent named like a person's handle or a reserved
// word, which tagging could not tell apart.
var ErrNameTaken = errors.New("platform: name taken")

// agentTaken reports whether a handle is an agent's name in a workspace, so
// no person is given it.
func (p *Platform) agentTaken(tenant, handle string) bool {
	if IsBuiltin(handle) {
		return true
	}
	_, err := p.Catalog().Get(context.Background(), tenant, handle, 0)
	return err == nil
}

// SeePerson records a person who signed in: their name and their roles now.
// A sighting that changes nothing is not written for an hour, so it is cheap
// on every request. A machine is not a person and is not recorded.
func (p *Platform) SeePerson(who auth.Principal) {
	if who.Kind != auth.User || who.Subject == "" || who.Tenant == "" {
		return
	}
	roles := make([]string, len(who.Roles))
	for i, r := range who.Roles {
		roles[i] = string(r)
	}
	if _, err := p.People.Seen(who.Tenant, people.Sighting{Subject: who.Subject, Name: who.Name, Roles: roles, Source: people.SignIn}, p.now()); err != nil && !errors.Is(err, people.ErrLimit) {
		p.log.Warn("platform: a person could not be recorded", "tenant", who.Tenant, "subject", who.Subject, "error", err)
	}
}

// seeMapped records the people a Slack client maps, by subject.
func (p *Platform) seeMapped(c clients.Client) {
	if c.Slack == nil {
		return
	}
	subjects := make([]string, 0, len(c.Slack.People))
	for _, who := range c.Slack.People {
		subjects = append(subjects, who)
	}
	slices.Sort(subjects)
	for _, who := range slices.Compact(subjects) {
		if _, err := p.People.Ensure(c.Tenant, who, people.Slack, p.now()); err != nil && !errors.Is(err, people.ErrLimit) {
			p.log.Warn("platform: a mapped person could not be recorded", "tenant", c.Tenant, "client", c.ID, "error", err)
		}
	}
}

// Grant is a role the policy gives one subject.
type Grant struct {
	Subject string
	Roles   []string
}

// SeeGranted records the subjects the policy grants roles to by name, in a
// workspace. Their roles are kept only for someone who has never signed in:
// a sign-in's roles are the whole set.
func (p *Platform) SeeGranted(tenant string, grants []Grant) error {
	var errs []error
	for _, g := range grants {
		_, err := p.People.Seen(tenant, people.Sighting{Subject: g.Subject, Roles: g.Roles, Source: people.Policy}, p.now())
		if err != nil && !errors.Is(err, people.ErrLimit) {
			errs = append(errs, fmt.Errorf("platform: record %q: %w", g.Subject, err))
		}
	}
	return errors.Join(errs...)
}

// CheckAgentName refuses a name a new agent cannot take: the built-in router,
// the experts' tag, and any handle a person in the workspace goes by, since a
// tag must name one of them.
func (p *Platform) CheckAgentName(tenant, name string) error {
	name = wire.NormalizeName(strings.TrimSpace(name))
	if people.IsReserved(name) {
		return fmt.Errorf("%w: @%s is reserved: it is never an agent's name", ErrNameTaken, name)
	}
	if person, err := p.People.ByHandle(tenant, name); err == nil {
		return fmt.Errorf("%w: a person goes by @%s", ErrNameTaken, person.Handle)
	}
	return nil
}
