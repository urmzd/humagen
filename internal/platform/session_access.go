package platform

import (
	"slices"
	"strings"

	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/threads"
)

// invitationReader includes the person's last recorded directory roles, as
// mention delivery does. Every later read still checks the signed-in principal.
func (p *Platform) invitationReader(tenant, subject string) auth.Principal {
	person := p.byPolicy(tenant, subject)
	if p.People != nil {
		if known, err := p.People.Get(tenant, subject); err == nil {
			for _, role := range known.Roles {
				if !slices.Contains(person.Roles, auth.Role(role)) {
					person.Roles = append(person.Roles, auth.Role(role))
				}
			}
		}
	}
	return person
}

// Inviting someone to a child cannot also grant its inherited parent context.
func (p *Platform) sessionInviteeSeesAncestors(tenant string, th threads.Thread, subject string) bool {
	if th.Session == nil || !strings.HasPrefix(th.Session.Parent, "s_") {
		return true
	}
	return p.SeesSession(p.invitationReader(tenant, subject), th.Session.Parent)
}
