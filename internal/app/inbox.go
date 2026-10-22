package app

import (
	"time"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/spec"
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

// mentionKinds is the mention kinds each workspace's Platform document
// declares, as workspace.mention_kinds sets them. A workspace that declares
// none has the built-in ones only.
func mentionKinds(cfg spec.Config) map[string][]platform.MentionKind {
	out := map[string][]platform.MentionKind{}
	for _, p := range cfg.Platforms {
		for _, k := range p.Spec.Workspace.MentionKinds {
			out[p.Metadata.Name] = append(out[p.Metadata.Name], platform.MentionKind{Kind: k.Kind, Title: k.Title, Resolves: k.Resolves})
		}
	}
	return out
}
