package platform_test

import (
	"strings"
	"testing"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/artifacts"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/saige/agent/agenttest"
	saigetypes "github.com/urmzd/saige/agent/types"
)

func TestWriterProducesAnEditableDeliverableAndRecordsItsActualOutcome(t *testing.T) {
	p := stage(t, &cast{
		says: map[string][][]saigetypes.Delta{"writer": {
			agenttest.ToolCallResponse("native", platform.ToolFileWrite, map[string]any{"path": "launch.pptx", "format": "native", "content": `{"slides":[{"title":"Launch","body":["Tuesday"],"notes":"Confirm the owner","sources":["https://example.test/launch"]}]}`}),
			agenttest.ToolCallResponse("publish", platform.ToolPublish, map[string]any{"path": "launch.pptx"}),
			agenttest.ToolCallResponse("outcome", platform.ToolOutcome, map[string]any{"kind": "update", "summary": "Published launch.pptx.", "decisions": "[\"Keep the Tuesday launch\"]", "next_action": "Confirm the launch owner"}),
			say("Published launch.pptx."),
		}}, tools: map[string][]string{"writer": platform.FileTools()},
	}, platform.Config{})
	first, err := p.Converse(t.Context(), tenant, platform.Turn{ID: "native-outcome", Agent: "writer", Text: "Make the launch deck"})
	if err != nil {
		t.Fatal(err)
	}
	done := await(t, p, first.ID, runs.Completed)
	within(t, "the authored outcome reached the run", func() bool { done, _ = p.Runs.Get(tenant, first.ID); return done.Outcome != nil })
	if done.Outcome.Kind != "update" || done.Outcome.NextAction != "Confirm the launch owner" {
		t.Fatalf("outcome = %+v", done.Outcome)
	}
	files, _, err := p.Artifacts.Page(tenant, artifacts.Query{Thread: first.Thread})
	if err != nil || len(files) != 1 || files[0].Kind != artifacts.Presentation || !strings.Contains(files[0].Mime, "presentationml") {
		t.Fatalf("real published files: %+v,%v", files, err)
	}
}
