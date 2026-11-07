package platform_test

import (
	"strings"
	"testing"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/orchagent/flow/turn/native"
	"github.com/urmzd/saige/agent/agenttest"
	types "github.com/urmzd/saige/agent/types"
)

func TestNativeRetrievalKeepsExactContextForReplay(t *testing.T) {
	model := &agenttest.ScriptedProvider{Responses: [][]types.Delta{agenttest.TextResponse("Zora signs renewals.")}}
	p := startWith(t, platform.Config{AgentOptions: []native.Option{native.WithProvider("offline", func(native.Spec) (types.Provider, error) { return model, nil })}})
	shelf(t, p, map[string]string{"manual": "Zora"})
	if _, err := p.Agents.Create(t.Context(), tenant, wire.AgentSpec{Name: "source-worker", Provider: "offline", Model: "offline", SystemPrompt: "Use the source.", Sources: []wire.Source{{Name: "manual", URI: "knowledge://manual"}}}, "test"); err != nil {
		t.Fatal(err)
	}
	run := askIn(t, p, "retrieval-capture", "source-worker", "")
	snapshot, err := p.Evals.LastRetrieval(tenant, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Validate() != nil || len(snapshot.Blocks) != 1 || !strings.Contains(snapshot.Prompt, "Zora") || len(snapshot.Sources) != 1 || snapshot.Sources[0] != "manual" {
		t.Fatalf("missing complete retrieval: %+v", snapshot)
	}
	var sent string
	for _, msg := range model.Requests()[0].Messages {
		sent += types.TextOf(msg)
	}
	if !strings.Contains(sent, snapshot.Prompt+"\n\n"+snapshot.Input) {
		t.Fatalf("stored context differs from actual model input: %s", sent)
	}
}
