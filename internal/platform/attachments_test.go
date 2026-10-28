package platform_test

import (
	"errors"
	"testing"

	transportslack "github.com/urmzd/humagen/fiber/pkg/connectors/slack"
	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/runs"
)

func TestSlackAttachmentsComeFromTheActualPostersMessage(t *testing.T) {
	f := &fakes{}
	p := startWith(t, f.config())
	in := slackClient("app", "A01APP", "T01ABCDEF", "lead")
	in.Agents = []string{clients.AnyAgent}
	in.Slack.Machine, in.Slack.People = "svc-slack", map[string]string{"U0ANA": "ana"}
	create(t, p, tenant, in)
	machine := auth.Principal{Tenant: tenant, Subject: "svc-slack", Kind: auth.Machine, Scopes: []auth.Permission{auth.RunsRead, auth.RunsWrite}}.WithPolicy(operators(t, "ana"))
	// Over-size input deliberately never needs network access. The resolver must
	// tell the agent a file was attached and unread, rather than silently omit it.
	fake := f.slackFor("token-app")
	ts := fake.Say("T01ABCDEF", "D01DM", "", "U0ANA", "@lead review", transportslack.File{ID: "F01FILE", Name: "large.png", Size: 100 << 20})
	post := fromSlack("D01DM", ts, "", "@lead review")
	post.Surface.Files = true
	got, err := p.StartThread(t.Context(), machine, post)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Message.Attachments) != 1 || got.Message.Attachments[0].Unread == "" {
		t.Fatalf("files lost: %+v", got.Message)
	}
	if len(got.Runs) != 1 || len(wire.AttachmentsOf(got.Runs[0].Tag())) != 1 {
		t.Fatalf("run lost files: %+v", got.Runs)
	}
	await(t, p, got.Runs[0].ID, runs.Completed)
	other := fake.Say("T01ABCDEF", "D02DM", "", "U0OTHER", "private", transportslack.File{ID: "F02FILE", Name: "private.png", Size: 100 << 20})
	post = fromSlack("D02DM", other, "", "@lead review")
	post.Surface.Files = true
	if _, err := p.StartThread(t.Context(), machine, post); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("another person's message supplied files: %v", err)
	}
}
