package platform_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/artifacts"
	"github.com/urmzd/humagen/pkg/runs"
	saigetypes "github.com/urmzd/saige/agent/types"
)

func TestSaveReviewV2ArtifactPreservesStructuredAnswer(t *testing.T) {
	answer := `{"format":"humagen.review.v2","title":"Retry review","summary":"No confirmed findings.","verdict":"comment","scope":{"commit":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"findings":[]}`
	p := stage(t, &cast{says: map[string][][]saigetypes.Delta{"reviewer": {say(answer)}}}, platform.Config{})
	tag := wire.NewEvent(wire.Raise{ID: "save-review-v2", Tenant: tenant, Agent: "reviewer", Origin: runs.OriginAPI, Text: "Review this change", Actor: platform.ActorOf(operator)})
	wire.SetGitHubHeadSHA(&tag, strings.Repeat("a", 40))
	r, err := p.Invoke(t.Context(), tag)
	if err != nil {
		t.Fatal(err)
	}
	run := await(t, p, r.ID, runs.Completed)
	saved, err := p.SaveArtifact(t.Context(), operator, run.Thread, platform.ArtifactSave{Source: "run", ID: run.ID})
	if err != nil {
		t.Fatal(err)
	}
	var report struct {
		Format string `json:"format"`
		Title  string `json:"title"`
	}
	if !strings.HasSuffix(saved.Title, ".review.json") || saved.Kind != artifacts.Data || json.Unmarshal([]byte(saved.Preview), &report) != nil || report.Format != "humagen.review.v2" || report.Title != "Retry review" || saved.Run != run.ID {
		t.Fatalf("structured review was not preserved: %+v", saved)
	}
}
