package platform

import (
	"fmt"
	"strings"

	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/clients"
)

// SlackAppMachine is the subject of the credential the server makes for the
// Slack app it starts in a team under tailscale sign-in. It is the same at
// every start, so a client can name it as its slack.machine.
func SlackAppMachine(team string) string { return slackAppMachine + team }

const slackAppMachine = "slack-app:"

// SlackMachine reports whether who is a Slack app's credential: a machine a
// Slack client of its workspace names as its slack.machine, enabled or not,
// or one the server made for the Slack app it starts. Clients that cannot be
// read count as naming it, so a store that is away refuses rather than lets
// the credential act as itself.
func (p *Platform) SlackMachine(who auth.Principal) bool {
	if who.Kind != auth.Machine || who.Subject == "" {
		return false
	}
	if strings.HasPrefix(who.Subject, slackAppMachine) {
		return true
	}
	all, err := p.Clients.List(who.Tenant)
	if err != nil {
		p.log.Error("platform: clients could not be read, so a machine is held to asking for a person", "tenant", who.Tenant, "subject", who.Subject, "error", err)
		return true
	}
	for _, c := range all {
		if c.Surface == clients.Slack && c.Slack != nil && c.Slack.Machine == who.Subject {
			return true
		}
	}
	return false
}

// MayAskForNobody holds a Slack app's credential to starting work only at a
// person's asking. Anyone who can message a Slack app (a new member, a guest,
// someone from another company in a shared channel) reaches it, so a run its
// credential starts as itself would answer people the workspace never let
// in. It starts one for a person its client maps (act_as), or for nobody at
// such a person's asking (asked_by), and nothing else. Every other caller
// may: an API key or a person is someone the workspace let in.
func (p *Platform) MayAskForNobody(who auth.Principal) error {
	if p.SlackMachine(who) {
		return fmt.Errorf("%w: a Slack app starts work only for a person its client maps (act_as or asked_by)", ErrForbidden)
	}
	return nil
}

// AskedBy is the person a surface's credential starts a run for nobody at
// the asking of: someone its client maps, who may start runs. A Slack app
// asks for nobody when a person asks about someone else's message, or asks
// again a turn that acted for nobody, so nobody's credentials run words that
// were not theirs. Anyone the client does not map is refused.
func (p *Platform) AskedBy(who auth.Principal, as ActAs) (auth.Principal, error) {
	person, err := p.Acting(who, as)
	if err != nil {
		return auth.Principal{}, err
	}
	if !person.Can(auth.RunsWrite) {
		return auth.Principal{}, fmt.Errorf("%w: %s may not start runs (asking needs %s)", ErrForbidden, p.NameOf(who.Tenant, person.Subject), auth.RunsWrite)
	}
	return person, nil
}
