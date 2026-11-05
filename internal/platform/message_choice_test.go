package platform_test

import (
	"errors"
	"testing"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
)

// A message's model and effort pick reaches the runs it starts, pinned to
// their tags, and a pick that names no model is refused before anything is
// posted.
func TestAMessagePicksItsRunsModelAndEffort(t *testing.T) {
	p := start(t, call.Limits{})
	who := auth.Principal{Kind: auth.User, Tenant: tenant, Subject: "ada", Roles: []auth.Role{auth.Admin}}
	lead, err := p.Agents.Get(t.Context(), tenant, "lead", 0)
	if err != nil {
		t.Fatal(err)
	}
	spec := lead.Spec
	spec.Choices = &wire.Choices{Models: []string{"offline/other"}}
	if _, err := p.Agents.Revise(t.Context(), tenant, spec, lead.Rev, "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.StartThread(t.Context(), who, platform.ThreadInput{Text: "@lead hi", Model: "other"}); !errors.Is(err, threads.ErrInvalid) {
		t.Fatalf("a model with no provider = %v, want ErrInvalid", err)
	}
	posted, err := p.StartThread(t.Context(), who, platform.ThreadInput{Text: "@lead hi", Model: "offline/other"})
	if err != nil || len(posted.Runs) != 1 {
		t.Fatal(posted, err)
	}
	run := await(t, p, posted.Runs[0].ID, runs.Completed)
	if got := wire.Meta(run.Tag())["_humagen.choice.model"]; got != "offline/other" {
		t.Fatalf("the run was pinned with model %q", got)
	}
	next, err := p.PostMessage(t.Context(), who, posted.Thread.ID, platform.MessageInput{Text: "@lead again"})
	if err != nil || len(next.Runs) != 1 {
		t.Fatal(next, err)
	}
	if got, ok := wire.Meta(await(t, p, next.Runs[0].ID, runs.Completed).Tag())["_humagen.choice.model"]; ok {
		t.Fatalf("a message with no pick carried %q to its run", got)
	}
}
