package platform

import (
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/inbox"
)

// ProjectUnread counts unread messages and announcements in one project in
// the caller's own inbox. The inbox's reader counters keep reads independent,
// and its read rule omits private threads and hidden sources.
func (p *Platform) ProjectUnread(who auth.Principal, name string) int {
	tree := p.ProjectTree(who.Tenant)
	if !p.Seer(who).Project(name) {
		return 0
	}
	c := p.Inbox.CountProject(who.Tenant, name, p.inboxQuery(tree, who, inbox.Query{}))
	return c.Unread + c.Updates
}
