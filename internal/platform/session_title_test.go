package platform_test

import (
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
)

func titleSession(t *testing.T, p *platform.Platform, text string) platform.SessionView {
	t.Helper()
	v, err := p.StartSession(t.Context(), admin, platform.SessionInput{Project: "Field", Agent: "lead", Text: text})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range v.Messages {
		if m.Run != nil {
			await(t, p, m.Run.ID, runs.Completed)
		}
	}
	return v
}

func postTitle(t *testing.T, p *platform.Platform, id, text string) string {
	t.Helper()
	v, err := p.PostSession(t.Context(), admin, id, platform.MessageInput{Text: text})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range v.Messages {
		if m.Run != nil {
			await(t, p, m.Run.ID, runs.Completed)
		}
	}
	got, err := p.ThreadRecord(admin.Tenant, id)
	if err != nil {
		t.Fatal(err)
	}
	return got.Title
}

func TestSessionTitleDropsLeadingMention(t *testing.T) {
	p := start(t, call.Limits{})
	field(t, p)
	v := titleSession(t, p, "@lead How did the launch go?")
	if v.Thread.Title != "How did the launch go?" {
		t.Fatalf("title %q", v.Thread.Title)
	}
}

func TestSessionTitleSubstantiveFirstMessageKeepsItsTitle(t *testing.T) {
	p := start(t, call.Limits{})
	field(t, p)
	v := titleSession(t, p, "check the build")
	if got := postTitle(t, p, v.ID, "now deploy it"); got != "Check the build" {
		t.Fatalf("title %q", got)
	}
}

func TestSessionTitleGreetingIsRetitledByFirstRequest(t *testing.T) {
	p := start(t, call.Limits{})
	field(t, p)
	v := titleSession(t, p, "howdy")
	if v.Thread.Title != "Howdy" {
		t.Fatalf("title %q", v.Thread.Title)
	}
	if got := postTitle(t, p, v.ID, "hey again"); got != "Howdy" {
		t.Fatalf("a second greeting retitled to %q", got)
	}
	if got := postTitle(t, p, v.ID, "@lead summarize the open incidents"); got != "Summarize the open incidents" {
		t.Fatalf("title %q", got)
	}
	if got := postTitle(t, p, v.ID, "and the closed ones"); got != "Summarize the open incidents" {
		t.Fatalf("retitled twice: %q", got)
	}
}

func TestSessionTitleRenamedSessionIsNeverRetitled(t *testing.T) {
	p := start(t, call.Limits{})
	field(t, p)
	v := titleSession(t, p, "howdy")
	th, err := p.ThreadRecord(admin.Tenant, v.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.Threads.Update(admin.Tenant, v.ID, th.Version, time.Now(), func(x *threads.Thread) error {
		x.Title = "My chat"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if got := postTitle(t, p, v.ID, "summarize the open incidents"); got != "My chat" {
		t.Fatalf("title %q", got)
	}
}
