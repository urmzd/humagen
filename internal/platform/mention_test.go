package platform_test

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/inbox"
	"github.com/urmzd/humagen/pkg/projects"
)

// A mention is for the person tagged alone. Reading it takes runs:read, and
// setting it aside runs:write, as for every other thing a run's conversation
// puts in the inbox.
func TestAMentionIsForThePersonTaggedAndDoneTakesRunsWrite(t *testing.T) {
	p, err := platform.New(platform.Config{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	reader := auth.Principal{Subject: "vic", Tenant: tenant, Kind: auth.User, Roles: []auth.Role{auth.Viewer}}
	// A mention is read where its thread lives: Globex, which everyone sees.
	if _, err := p.CreateProject(t.Context(), tenant, projects.Writer{By: "ada"}, platform.ProjectInput{Name: "Globex"}); err != nil {
		t.Fatal(err)
	}
	part := inbox.MentionPart{Thread: "th-0123456789abcdef", Message: "m-2", Project: "Globex", Channel: "renewals", By: "ada", Excerpt: inbox.Excerpt("@opal can you check the renewal?")}
	raise := func(to string) inbox.Message {
		t.Helper()
		m, _, err := p.Inbox.Raise(tenant, inbox.MentionKey(part.Thread, part.Message, to), inbox.Message{
			Kind: inbox.KindMention, To: to, Subject: "Ada mentioned you in #renewals", From: inbox.From{Kind: "person", Name: "ada"},
			Severity: inbox.Info, Source: inbox.Source{Thread: part.Thread, Project: part.Project},
			Parts: []inbox.Part{{Type: inbox.PartMention, Mention: &part}},
		}, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	mine := raise(operator.Subject)
	theirs := raise(reader.Subject)

	if _, err := p.InboxGet(admin, mine.ID); !errors.Is(err, inbox.ErrNotFound) {
		t.Fatalf("someone not tagged read a mention: %v", err)
	}
	got, err := p.InboxGet(operator, mine.ID)
	if err != nil || got.Parts[0].Mention == nil || got.Parts[0].Mention.Excerpt != part.Excerpt || !slices.Equal(got.Parts[0].Actions, []string{platform.ActReply, platform.ActDone}) {
		t.Fatalf("the person tagged reads %+v, %v", got, err)
	}
	seen, err := p.InboxGet(reader, theirs.ID)
	if err != nil || len(seen.Parts[0].Actions) != 0 {
		t.Fatalf("a viewer reads %+v, %v; want it with no actions", seen, err)
	}
	if _, err := p.InboxResolve(t.Context(), reader, theirs.ID, "p1", platform.Resolution{Action: platform.ActDone}); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("a viewer set a mention aside: %v", err)
	}
	done, err := p.InboxResolve(t.Context(), operator, mine.ID, "p1", platform.Resolution{Action: platform.ActDone})
	if err != nil || done.State != inbox.Resolved || done.Parts[0].Outcome.Action != platform.ActDone {
		t.Fatalf("done = %+v, %v", done, err)
	}
	// The same tag again is raised again only once the first is dealt with.
	if again := raise(operator.Subject); again.ID == mine.ID {
		t.Fatal("a settled mention was handed back for a new one")
	}
}
