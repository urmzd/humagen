package app

import (
	"time"

	"github.com/urmzd/warp/pkg/spec"
)

// inboxRetention is how long each workspace's inbox keeps what was settled,
// as its Platform document sets workspace.inbox_retention. A workspace that
// leaves it out keeps the default.
func inboxRetention(cfg spec.Config) map[string]time.Duration {
	out := map[string]time.Duration{}
	for _, p := range cfg.Platforms {
		if p.Spec.Workspace.InboxRetention != "" {
			out[p.Metadata.Name] = p.Spec.Workspace.Retention()
		}
	}
	return out
}
