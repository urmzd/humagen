package platform_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
)

func TestSaveAsArtifactKeepsAnswersMessagesAndScratchpadsAsVersions(t *testing.T) {
	p := start(t, call.Limits{})
	ctx := t.Context()
	r, err := p.Converse(ctx, tenant, platform.Turn{ID: "save-answer", Agent: "lead", Text: "Draft the plan", Actor: platform.ActorOf(operator)})
	if err != nil {
		t.Fatal(err)
	}
	run := await(t, p, r.ID, runs.Completed)
	if strings.TrimSpace(run.Answer) == "" {
		t.Skip("the test model answered nothing")
	}
	saved, err := p.SaveArtifact(ctx, operator, run.Thread, platform.ArtifactSave{Source: "run", ID: run.ID})
	if err != nil {
		t.Fatal(err)
	}
	if saved.Run != run.ID || saved.Agent != "" || saved.CreatedBy != operator.Subject || !strings.HasSuffix(saved.Title, ".md") || saved.Version != 1 {
		t.Fatalf("saved answer = %+v", saved)
	}
	again, err := p.SaveArtifact(ctx, operator, run.Thread, platform.ArtifactSave{Source: "run", ID: run.ID})
	if err != nil || again.ID != saved.ID || again.Version != 1 || again.Title != saved.Title {
		t.Fatalf("saved again = %+v %v", again, err)
	}
	pad, err := p.CreateScratchpad(ctx, operator, run.Thread, platform.ScratchpadInput{Name: "Plan", Content: "# Plan"})
	if err != nil {
		t.Fatal(err)
	}
	fromPad, err := p.SaveArtifact(ctx, operator, run.Thread, platform.ArtifactSave{Source: "scratchpad", ID: pad.ID, Title: "Launch plan"})
	if err != nil || fromPad.Title != "Launch plan.md" || fromPad.Preview != "# Plan" {
		t.Fatalf("saved scratchpad = %+v %v", fromPad, err)
	}
	repeated, err := p.SaveArtifact(ctx, operator, run.Thread, platform.ArtifactSave{Source: "scratchpad", ID: pad.ID, Title: "Launch plan"})
	if err != nil || repeated.ID != fromPad.ID {
		t.Fatalf("unchanged scratchpad = %+v %v", repeated, err)
	}
	if _, err := p.UpdateScratchpad(ctx, operator, run.Thread, pad.ID, platform.ScratchpadInput{Name: pad.Name, Content: "# Revised plan", Version: pad.Version}); err != nil {
		t.Fatal(err)
	}
	changed, err := p.SaveArtifact(ctx, operator, run.Thread, platform.ArtifactSave{Source: "scratchpad", ID: pad.ID, Title: "Launch plan"})
	if err != nil || changed.ID == fromPad.ID || changed.Version != 2 || changed.Preview != "# Revised plan" {
		t.Fatalf("changed scratchpad = %+v %v", changed, err)
	}
	// Another conversation's run, an unknown source and a reader without
	// artifacts:write are refused.
	other := startThread(t, p, operator, platform.ThreadInput{Title: "Other", Text: "Other", PostOnly: true, Visibility: threads.Private}).Thread
	if _, err := p.SaveArtifact(ctx, operator, other.ID, platform.ArtifactSave{Source: "run", ID: run.ID}); !errors.Is(err, threads.ErrNotFound) {
		t.Fatalf("another conversation's run = %v", err)
	}
	if _, err := p.SaveArtifact(ctx, operator, run.Thread, platform.ArtifactSave{Source: "file", ID: "x"}); !errors.Is(err, platform.ErrSandbox) {
		t.Fatalf("unknown source = %v", err)
	}
	reader := auth.Principal{Subject: operator.Subject, Tenant: tenant, Kind: auth.User, Scopes: []auth.Permission{auth.RunsRead}}
	if _, err := p.SaveArtifact(ctx, reader, run.Thread, platform.ArtifactSave{Source: "run", ID: run.ID}); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("reader = %v", err)
	}
}
