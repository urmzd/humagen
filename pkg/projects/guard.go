package projects

import "fmt"

// GuardSnapshot serializes one external placement commit with every project
// and channel mutation. The callback must not call a locking Store method.
// Tree reads are immutable snapshots and are safe inside the callback.
func (s *Store) GuardSnapshot(tenantName string, snapshot *Tree, channel Channel, commit func() error) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if snapshot == nil || snapshot != s.Tree(tenantName) {
		return fmt.Errorf("%w: the project audience changed; review the publication again", ErrVersion)
	}
	tenant := s.tenants[tenantName]
	if tenant == nil {
		tenant = newTenant()
	}
	current, ok := tenant.channel(channel.Project, channel.Name)
	if !ok || !sameChannel(current, channel) {
		return fmt.Errorf("%w: the destination channel changed; review the publication again", ErrVersion)
	}
	return commit()
}
