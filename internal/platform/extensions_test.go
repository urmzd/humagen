package platform_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/urmzd/saige/agent/agenttest"
	saigetypes "github.com/urmzd/saige/agent/types"

	"github.com/urmzd/humagen/fiber/pkg/turn/native"
	"github.com/urmzd/humagen/internal/wire"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/runs"
)

func TestAnExtensionProviderRunsAnAgent(t *testing.T) {
	model := &agenttest.ScriptedProvider{Responses: [][]saigetypes.Delta{agenttest.TextResponse("from the house model")}}
	p := startWith(t, platform.Config{Ext: platform.Extensions{Providers: map[string]native.ProviderFunc{
		"house": func(native.Spec) (saigetypes.Provider, error) { return model, nil },
	}}})
	ctx := t.Context()
	spec := wire.AgentSpec{Name: "housed", Provider: "house", Model: "h1", SystemPrompt: "Answer."}
	if _, err := p.Agents.Create(ctx, tenant, spec, "test"); err != nil {
		t.Fatalf("a spec naming an extension provider was refused: %v", err)
	}
	r, err := p.Invoke(ctx, tag("ext-1", "housed"))
	if err != nil {
		t.Fatal(err)
	}
	if got := await(t, p, r.ID, runs.Completed); !strings.Contains(got.Answer, "house model") {
		t.Fatalf("answer = %q", got.Answer)
	}
}

func TestAProviderNameIsDeclaredOnce(t *testing.T) {
	fn := func(native.Spec) (saigetypes.Provider, error) { return nil, errors.New("unused") }
	a := platform.Extensions{Providers: map[string]native.ProviderFunc{"house": fn}}
	if _, err := a.Merge(a); err == nil {
		t.Fatal("the same provider from a file and an option merged")
	}
}
