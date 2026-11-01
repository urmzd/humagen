package platform

import (
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/memory"
	"github.com/urmzd/humagen/pkg/operations"
)

func TestAutomaticNotesRespectReadContextPolicy(t *testing.T) {
	p, err := New(Config{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Memory.Add("t", "a", "Remember the quarterly review", memory.Person, memory.Approved, "ada", time.Now()); err != nil {
		t.Fatal(err)
	}
	for _, decision := range []operations.Level{operations.Allow, operations.Ask, operations.Never} {
		policy, _ := p.Operations.Policy("t", operations.Scope{Kind: "workspace"})
		if _, err := p.Operations.SetPolicy("t", operations.Policy{Scope: policy.Scope, Rules: map[operations.Category]operations.Level{operations.ReadContext: decision}}, policy.Version, "ada", time.Now()); err != nil {
			t.Fatal(err)
		}
		got := p.turnContext("t", "", "", "a", "ada", "Hello", map[string]string{})
		if strings.Contains(got, "quarterly review") != (decision == operations.Allow) {
			t.Fatalf("%s: %s", decision, got)
		}
	}
	policy, _ := p.Operations.Policy("t", operations.Scope{Kind: "workspace"})
	if _, err := p.Operations.SetPolicy("t", operations.Policy{Scope: policy.Scope, Rules: map[operations.Category]operations.Level{operations.ReadContext: operations.Allow}}, policy.Version, "ada", time.Now()); err != nil {
		t.Fatal(err)
	}
	if got := p.turnContext("t", "", "", "a", "ada", "Hello", map[string]string{operations.MetaForceAsk: "true"}); strings.Contains(got, "quarterly review") {
		t.Fatal("dry run consumed notes before approval")
	}
}
