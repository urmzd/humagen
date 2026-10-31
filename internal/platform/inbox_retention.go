package platform

import (
	"cmp"
	"context"
	"time"

	"github.com/urmzd/humagen/pkg/inbox"
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
// again at the next pass. A pass stops between workspaces once ctx is done.
func (p *Platform) retainInbox(ctx context.Context) {
	now := p.now()
	if err := p.Notifications.EachTenant(func(tenant string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := p.Notifications.Retain(tenant, now.Add(-p.inboxRetention(tenant))); err != nil {
			p.log.Error("platform: notification retention failed", "error", err)
		}
		return nil
	}); err != nil && ctx.Err() == nil {
		p.log.Error("platform: notification retention could not list workspaces", "error", err)
	}
	if ctx.Err() != nil {
		return
	}
	if err := p.EmailNotifications.EachTenant(func(tenant string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := p.EmailNotifications.Retain(tenant, now.Add(-p.inboxRetention(tenant))); err != nil {
			p.log.Error("platform: email notification retention failed", "error", err)
		}
		return nil
	}); err != nil && ctx.Err() == nil {
		p.log.Error("platform: email notification retention could not list workspaces", "error", err)
	}
	if ctx.Err() != nil {
		return
	}
	for _, tenant := range p.Audit.Tenants() {
		if ctx.Err() != nil {
			return
		}
		if err := p.Audit.Retain(tenant, now.Add(-p.inboxRetention(tenant))); err != nil {
			p.log.Error("platform: audit retention failed", "error", err)
		}
	}
	for _, tenant := range p.ReadAudit.Tenants() {
		if ctx.Err() != nil {
			return
		}
		if err := p.ReadAudit.Retain(tenant, now.Add(-p.inboxRetention(tenant))); err != nil {
			p.log.Error("platform: read audit retention failed", "error", err)
		}
	}
	tenants, err := p.Inbox.Tenants()
	if err != nil {
		p.log.Warn("platform: inbox retention could not list workspaces; the next pass tries again", "error", err)
	}
	for _, tenant := range tenants {
		if ctx.Err() != nil {
			return
		}
		n, err := p.Inbox.Retain(tenant, now.Add(-p.inboxRetention(tenant)))
		if n > 0 {
			p.log.Info("platform: inbox retention dropped what was settled", "tenant", tenant, "messages", n)
		}
		if err != nil {
			p.log.Warn("platform: inbox retention could not be written; the next pass tries again", "tenant", tenant, "error", err)
		}
	}
}
