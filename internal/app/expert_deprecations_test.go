package app

import (
	"github.com/urmzd/humagen/pkg/spec"
	"strings"
	"testing"
)

func TestExpertConfigurationProducesActionableRetirementDiagnostics(t *testing.T) {
	cfg := spec.Config{Platforms: []spec.Platform{{Metadata: spec.Metadata{Name: "acme"}, Spec: spec.PlatformSpec{Workspace: spec.Workspace{Experts: []spec.Expert{}, Projects: []spec.Project{{Name: "Field", Experts: []spec.Expert{}}}, MentionKinds: []spec.MentionKind{{Kind: "expert", Resolves: "experts"}}}}}}}
	warnings := expertDeprecations(cfg)
	if len(warnings) != 3 {
		t.Fatalf("diagnostics: %v", warnings)
	}
	for _, warning := range warnings {
		if !strings.Contains(warning, "retired") || !strings.Contains(warning, "Find Help") || !strings.Contains(warning, "remove") {
			t.Fatalf("not actionable: %s", warning)
		}
	}
}
