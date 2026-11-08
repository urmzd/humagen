package platform

import (
	"testing"

	"github.com/urmzd/humagen/api"
)

// The registry's operations come from the API contract, and an operation the
// contract lacks is refused rather than offered.
func TestTheContractToolsComeFromTheContract(t *testing.T) {
	tools, err := loadContractTools(api.OpenAPI, SystemOperations)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != len(SystemOperations) {
		t.Fatalf("%d tools for %d operations", len(tools), len(SystemOperations))
	}
	for _, tl := range tools {
		if tl.def.Name == "" || tl.def.Description == "" || tl.def.Input["type"] != "object" || tl.method == "" || tl.path == "" {
			t.Errorf("tool %+v is incomplete", tl.def)
		}
	}
	if got := snake("listAgentRevisions"); got != "list_agent_revisions" {
		t.Errorf("snake = %q", got)
	}
	if _, err := loadContractTools(api.OpenAPI, []string{"noSuchOperation"}); err == nil {
		t.Fatal("an operation the contract lacks was offered")
	}
}
