package platform

import (
	"slices"
	"strings"

	"github.com/urmzd/humagen/pkg/alerts"
)

// Alert gives native tools and HTTP readers the same safe person identifiers.
// Stored subjects can contain addresses and never belong in model context.
func (n Names) Alert(a alerts.Alert) alerts.Alert {
	a.AckedBy, a.ResolvedBy = n.Key(a.AckedBy), n.Key(a.ResolvedBy)
	a.Who, a.Notify = slices.Clone(a.Who), slices.Clone(a.Notify)
	for i, who := range a.Who {
		if !strings.HasPrefix(who, alerts.RolePrefix) {
			a.Who[i] = n.Key(who)
		}
	}
	for i, who := range a.Notify {
		if !strings.HasPrefix(who, alerts.RolePrefix) {
			a.Notify[i] = n.Key(who)
		}
	}
	return a
}
