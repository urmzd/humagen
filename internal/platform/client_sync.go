package platform

import (
	"context"
	"sync"
	"time"

	"github.com/urmzd/humagen/pkg/clients"
)

// Clients on every replica.
//
// Every client in pkg/clients is a webhook client: Slack's Events API over
// HTTP and GitHub's webhooks. Any replica may be sent a request, so every
// replica seats every enabled client. Delivery is already one replica per
// tenant at a time, through egress's consumer group.
//
// A change to a client is made on one replica, which seats it at once and
// counts the change (clients.Generations). Every replica reads the count
// every clientSyncEvery and, when it moved, reads the clients again and
// seats what changed: a new client, a new version of one, or none where one
// was removed or turned off. A webhook no seat claims, and an event no seat
// carries, read the count at once (at most every clientRefreshGap), so a
// request sent to another replica right after a change is not refused.

// clientSyncEvery is how often a replica reads the clients' change count.
const clientSyncEvery = 2 * time.Second

// clientRefreshGap is the least time between two reads of the count that a
// webhook or an event no seat matched asks for, so unclaimed requests cannot
// read the database at their own rate.
const clientRefreshGap = 250 * time.Millisecond

// touchClients counts a change to a client, so other replicas seat it. A
// failure is logged: the other replicas then see the change at the next one.
func (p *Platform) touchClients() {
	g, ok := p.Clients.(clients.Generations)
	if !ok {
		return
	}
	n, err := g.Touch()
	if err != nil {
		p.log.Warn("platform: a client change could not be counted; other replicas see it at the next change", "error", err)
		return
	}
	// This replica's seats are current when the count it made follows the
	// one it last read: a sync of its own change is then skipped. Otherwise
	// another replica changed a client too, and the next sync seats it.
	p.clientSync.mu.Lock()
	if p.clientSync.seen == n-1 {
		p.clientSync.seen = n
	}
	p.clientSync.mu.Unlock()
}

// clientSyncState is what a replica last read of the change count.
type clientSyncState struct {
	mu    sync.Mutex
	seen  int64
	asked time.Time
}

// syncClientsLoop seats what other replicas changed, until ctx ends.
func (p *Platform) syncClientsLoop(ctx context.Context) error {
	if _, ok := p.Clients.(clients.Generations); !ok {
		return nil
	}
	tick := time.NewTicker(clientSyncEvery)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
			p.refreshClients(false)
		}
	}
}

// refreshClients reads the change count and, when another replica moved
// it, seats the clients again. asked is a request that no seat matched; it
// reads at most every clientRefreshGap. It reports whether it seated again.
func (p *Platform) refreshClients(asked bool) bool {
	g, ok := p.Clients.(clients.Generations)
	if !ok {
		return false
	}
	p.clientSync.mu.Lock()
	if asked && time.Since(p.clientSync.asked) < clientRefreshGap {
		p.clientSync.mu.Unlock()
		return false
	}
	if asked {
		p.clientSync.asked = time.Now()
	}
	seen := p.clientSync.seen
	p.clientSync.mu.Unlock()

	n, err := g.Generation()
	if err != nil {
		p.log.Warn("platform: the clients' change count could not be read", "error", err)
		return false
	}
	if n == seen {
		return false
	}
	p.syncClients()
	p.clientSync.mu.Lock()
	p.clientSync.seen = max(p.clientSync.seen, n)
	p.clientSync.mu.Unlock()
	return true
}

// primeClientSync records the count the clients restore read at start.
func (p *Platform) primeClientSync() {
	g, ok := p.Clients.(clients.Generations)
	if !ok {
		return
	}
	if n, err := g.Generation(); err == nil {
		p.clientSync.mu.Lock()
		p.clientSync.seen = n
		p.clientSync.mu.Unlock()
	}
}

// syncClients makes this replica's seats what the store holds: a client
// not seated, or seated at another version, is built and seated; a seat
// whose client is gone or off is removed. Connectors the embedding program
// built (static) are left alone.
func (p *Platform) syncClients() {
	p.clientsMu.Lock()
	defer p.clientsMu.Unlock()
	for _, surface := range clients.Surfaces() {
		s := p.surfaces[string(surface)]
		if s == nil {
			continue
		}
		on, err := p.Clients.On(surface)
		if err != nil {
			p.log.Warn("platform: clients could not be read to seat what changed", "surface", surface, "error", err)
			continue
		}
		want := make(map[[2]string]clients.Client, len(on))
		for _, c := range on {
			want[[2]string{c.Tenant, c.ID}] = c
		}
		for _, st := range s.all() {
			if st.client.Managed == clients.ManagedStatic {
				continue
			}
			if _, ok := want[[2]string{st.client.Tenant, st.client.ID}]; !ok {
				s.remove(st.client.Tenant, st.client.ID)
				p.log.Info("platform: client unseated after a change on another replica", "tenant", st.client.Tenant, "client", st.client.ID)
			}
		}
		for _, c := range on {
			if st := s.seat(c.Tenant, c.ID); st != nil && st.client.Version == c.Version {
				continue
			}
			_, secrets, err := p.credentials(c.Tenant, c.ID, c.Surface, c.Credentials, ClientInput{})
			var st *seat
			if err == nil {
				st, err = p.dial(c, secrets)
			}
			if err != nil {
				p.log.Error("platform: client changed on another replica could not be started", "tenant", c.Tenant, "client", c.ID, "surface", c.Surface, "error", err)
				continue
			}
			p.seat(c, st)
			p.log.Info("platform: client seated after a change on another replica", "tenant", c.Tenant, "client", c.ID, "version", c.Version)
		}
	}
}
