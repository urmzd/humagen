package platform

import (
	"fmt"
	"slices"
	"strings"

	"github.com/urmzd/humagen/pkg/alerts"
	"github.com/urmzd/humagen/pkg/auth"
)

// SessionAlertAudience resolves public handles to stored subjects. Naming a
// person never grants access to the session or any of its ancestors. Role
// audiences remain subject to each reader's current session visibility.
func (p *Platform) SessionAlertAudience(who auth.Principal, session string, requested []string) ([]string, error) {
	if len(requested) == 0 {
		return []string{who.Subject}, nil
	}
	if len(requested) > 20 {
		return nil, fmt.Errorf("%w: too many alert recipients", alerts.ErrInvalid)
	}
	out := make([]string, 0, len(requested))
	for _, value := range requested {
		if strings.HasPrefix(value, alerts.RolePrefix) {
			if !slices.Contains(out, value) {
				out = append(out, value)
			}
			continue
		}
		subject := who.Subject
		reader := who
		if value != who.Subject && value != p.Names(who.Tenant).Key(who.Subject) {
			var err error
			subject, err = p.operationPersonSubject(who.Tenant, value)
			if err != nil || subject == "" {
				return nil, fmt.Errorf("%w: alert recipient is unavailable", alerts.ErrInvalid)
			}
			reader = p.byPolicy(who.Tenant, subject)
		}
		if subject == "" || !p.SeesSession(reader, session) {
			return nil, fmt.Errorf("%w: alert recipient cannot read this session", alerts.ErrInvalid)
		}
		if !slices.Contains(out, subject) {
			out = append(out, subject)
		}
	}
	return out, nil
}
