package projects

import "fmt"

// GuardSnapshot runs one external placement commit against the tree a caller
// read (from Tree or Read) and a channel as it read it: it refuses
// (ErrVersion) when the tenant has changed since, and otherwise runs commit.
// In this process no project or channel write runs while commit does. Once
// commit returns, it moves the tenant's head, so a write in another process
// that read the tenant before commit landed cannot land and checks again
// against what commit did. The callback must not call a locking Store
// method. Tree reads are immutable snapshots and are safe inside it.
func (s *Store) GuardSnapshot(tenantName string, read *Tree, channel Channel, commit func() error) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	snap, err := s.read(tenantName)
	if err != nil {
		return err
	}
	if read == nil || read.version == 0 || read.version != snap.version {
		return fmt.Errorf("%w: the project audience changed; review the publication again", ErrVersion)
	}
	current, ok := snap.t.channel(channel.Project, channel.Name)
	if !ok || !sameChannel(current, channel) {
		return fmt.Errorf("%w: the destination channel changed; review the publication again", ErrVersion)
	}
	if err := commit(); err != nil {
		return err
	}
	return s.fence(tenantName, snap.version, func(*snapshot) error { return nil })
}
