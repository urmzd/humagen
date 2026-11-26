package platform

import (
	"slices"
	"testing"

	"github.com/urmzd/humagen/pkg/agentlooks"
)

// Every template is complete, grants only what the builder may grant, is
// drawn as a shape a look takes, and fits the look's bounds on starters.
func TestAgentTemplatesAreWellFormed(t *testing.T) {
	if agentTemplates[0].ID != BlankTemplate {
		t.Fatalf("first template = %q, want %q", agentTemplates[0].ID, BlankTemplate)
	}
	seen := map[string]bool{}
	for _, tpl := range agentTemplates {
		if seen[tpl.ID] {
			t.Errorf("template %q is listed twice", tpl.ID)
		}
		seen[tpl.ID] = true
		if tpl.ID == "" || tpl.Title == "" || tpl.Outcome == "" || tpl.Description == "" || tpl.Greeting == "" || tpl.Prompt == "" {
			t.Errorf("template %q is incomplete: %+v", tpl.ID, tpl)
		}
		if len(tpl.Greeting) > 280 || len(tpl.Description) > 400 {
			t.Errorf("template %q has a greeting or description over the look's bounds", tpl.ID)
		}
		if !slices.Contains(agentlooks.Shapes, tpl.Shape) {
			t.Errorf("template %q is drawn as %q, which no look takes", tpl.ID, tpl.Shape)
		}
		for _, tool := range tpl.Tools {
			if !builderGrantable(tool) {
				t.Errorf("template %q grants %q, which the builder may not grant", tpl.ID, tool)
			}
		}
		if len(tpl.Starters) > 3 {
			t.Errorf("template %q offers %d starters, over three", tpl.ID, len(tpl.Starters))
		}
		for _, st := range tpl.Starters {
			if st.Title == "" || st.Prompt == "" || len(st.Title) > 60 || len(st.Prompt) > 500 {
				t.Errorf("template %q has a starter out of bounds: %+v", tpl.ID, st)
			}
		}
	}
}
