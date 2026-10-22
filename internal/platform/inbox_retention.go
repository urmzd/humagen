package platform

import (
	"cmp"
	"time"

	"github.com/urmzd/warp/pkg/inbox"
)

// retainEvery is how often each workspace's inbox drops what was settled
// longer ago than its retention.
const retainEvery = time.Hour

// inboxRetention is how long a workspace's inbox keeps what was settled:
// what its Platform document sets (Config.InboxRetention), else
// inbox.DefaultRetention, and never less than inbox.MinRetention.
func (p *Platform) inboxRetention(tenant string) time.Duration {
	return max(cmp.Or(p.cfg.InboxRetention[tenant], inbox.DefaultRetention), inbox.MinRetention)
}

// retainInbox drops from every workspace's inbox what was settled for
// everyone, resolved, dismissed or withdrawn, longer ago than its
// retention (inbox.Store.Retain). What a step could not write is tried
// again at the next pass.
func (p *Platform) retainInbox() {
	now := p.now()
	for _, tenant := range p.Inbox.Tenants() {
		n, err := p.Inbox.Retain(tenant, now.Add(-p.inboxRetention(tenant)))
		if n > 0 {
			p.log.Info("platform: inbox retention dropped what was settled", "tenant", tenant, "messages", n)
		}
		if err != nil {
			p.log.Warn("platform: inbox retention could not be written; the next pass tries again", "tenant", tenant, "error", err)
		}
	}
}
