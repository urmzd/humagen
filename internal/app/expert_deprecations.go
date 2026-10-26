package app

import (
	"fmt"
	"github.com/urmzd/humagen/pkg/spec"
)

// Stored expert metadata is retained for old questions. It no longer injects
// prompts or routes new mention tags; operators get an actionable migration.
func expertDeprecations(cfg spec.Config) []string {
	var warnings []string
	for _, doc := range cfg.Platforms {
		prefix := fmt.Sprintf("platform %q: workspace", doc.Metadata.Name)
		if doc.Spec.Workspace.Experts != nil {
			warnings = append(warnings, prefix+".experts is retired; remove it and use Find Help to select a project and an agent")
		}
		for i, pr := range doc.Spec.Workspace.Projects {
			if pr.Experts != nil {
				warnings = append(warnings, fmt.Sprintf("%s.projects[%d].experts is retired; remove it and use Find Help", prefix, i))
			}
		}
		for i, kind := range doc.Spec.Workspace.MentionKinds {
			if kind.Resolves == "experts" {
				warnings = append(warnings, fmt.Sprintf("%s.mention_kinds[%d] resolves to retired expert routing; remove it and use Find Help", prefix, i))
			}
		}
	}
	return warnings
}
