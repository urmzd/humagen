package platform_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/saige/agent/agenttest"
	saigetypes "github.com/urmzd/saige/agent/types"

	"github.com/urmzd/fiber/pkg/agentrt"
	"github.com/urmzd/fiber/pkg/agentspec"
	"github.com/urmzd/fiber/pkg/envelope"

	"github.com/urmzd/warp/internal/platform"
	"github.com/urmzd/warp/pkg/auth"
	"github.com/urmzd/warp/pkg/inbox"
	"github.com/urmzd/warp/pkg/projects"
	"github.com/urmzd/warp/pkg/runs"
)

// recapping starts a platform with an agent, "recapper", whose every answer
// is answer, and a project, Field, that ada created and so owns.
func recapping(t *testing.T, answer string) *platform.Platform {
	t.Helper()
	p := startWith(t, platform.Config{Ext: platform.Extensions{Providers: map[string]agentrt.ProviderFunc{
		"says": func(agentrt.Spec) (saigetypes.Provider, error) {
			out := make([][]saigetypes.Delta, 8)
			for i := range out {
				out[i] = agenttest.TextResponse(answer)
			}
			return &agenttest.ScriptedProvider{Responses: out}, nil
		},
	}}})
	if _, err := p.Agents.Create(t.Context(), tenant, agentspec.AgentSpec{Name: "recapper", Provider: "says", Model: "m", SystemPrompt: "Recap."}, "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.CreateProject(t.Context(), tenant, projects.Writer{By: "ada"}, platform.ProjectInput{Name: "Field"}); err != nil {
		t.Fatal(err)
	}
	return p
}

// recap runs a schedule's occurrence now, as run now does, filed in project
// and delivered there, and waits for it to end.
func recap(t *testing.T, p *platform.Platform, id, project string) runs.Run {
	t.Helper()
	run, err := p.Invoke(t.Context(), envelope.Tag{
		ID: id, Tenant: tenant, Agent: "recapper", Origin: runs.OriginManual, Text: "What changed?",
		Actor: envelope.Actor{ID: "bob"},
		Meta: map[string]string{
			"cron_schedule": "field-recap", "cron_expression": "41 8 * * 1", "created_by": "ada",
			runs.MetaProject: project, platform.MetaDeliver: platform.DeliverProject,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return await(t, p, run.ID, runs.Completed)
}

// about returns the announcements, drafts included, that a run raised.
func about(p *platform.Platform, run string) []inbox.Message {
	var out []inbox.Message
	for _, m := range snapshot(p) {
		if m.Kind == inbox.KindAnnouncement && m.Source.Run == run {
			out = append(out, m)
		}
	}
	return out
}

var (
	adaSigned = auth.Principal{Subject: "ada", Tenant: tenant, Kind: auth.User, Roles: []auth.Role{auth.Operator}}
	bobSigned = auth.Principal{Subject: "bob", Tenant: tenant, Kind: auth.User, Roles: []auth.Role{auth.Operator}}
)

// A team project publishes what its scheduled agent writes: an announcement
// from the agent, its first line the subject and the rest the text, linked to
// the run, reaching the project's members.
func TestARecapIsPostedInItsProject(t *testing.T) {
	p := recapping(t, "Field shipped two fixes\nThe export is faster.\nThe login loop is gone.")
	run := recap(t, p, "manual-1", "Field")
	var got []inbox.Message
	eventually(t, "no announcement was posted", func() bool { got = about(p, run.ID); return len(got) > 0 })
	m := got[0]
	switch {
	case len(got) != 1:
		t.Fatalf("posted %d times", len(got))
	case m.Subject != "Field shipped two fixes" || len(m.Parts) != 1 || m.Parts[0].Text != "The export is faster.\nThe login loop is gone.":
		t.Fatalf("subject %q, parts %+v", m.Subject, m.Parts)
	case m.From != inbox.From{Kind: "agent", Name: "recapper"} || m.Source.Project != "Field" || m.Source.Agent != "recapper":
		t.Fatalf("from %+v, source %+v", m.From, m.Source)
	case !m.For(inbox.Reader{Subject: "cy", Projects: []string{"Field"}}) || m.For(inbox.Reader{Subject: "cy"}):
		t.Fatal("the announcement does not reach exactly the project's members")
	}
}

// An agent that has nothing to say says NONE, and nothing is posted.
func TestARecapOfNothingPostsNothing(t *testing.T) {
	p := recapping(t, "None.")
	run := recap(t, p, "manual-1", "Field")
	time.Sleep(50 * time.Millisecond)
	if got := about(p, run.ID); len(got) != 0 {
		t.Fatalf("NONE posted %+v", got)
	}
}

// The workspace reviews what its agents post, since everyone reads it: the
// answer is held as a draft that only an owner is shown and may publish, and
// publishing posts it from the agent, saying who approved it.
func TestAReviewedPostWaitsForAnOwner(t *testing.T) {
	p := recapping(t, "Quarter closed\nRevenue is up.")
	if _, err := p.Projects.Update(tenant, "Field", 1, projects.Writer{By: "ada"}, time.Now(), func(pr *projects.Project) error {
		pr.AgentPosts = projects.AgentPostsReview
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	run := recap(t, p, "manual-1", "Field")
	var drafts []inbox.Message
	eventually(t, "no draft was raised", func() bool { drafts = about(p, run.ID); return len(drafts) > 0 })
	d := drafts[0]
	if len(drafts) != 1 || d.Broadcast || len(d.Parts) != 1 || d.Parts[0].Type != inbox.PartDraft || d.Parts[0].Draft.Subject != "Quarter closed" {
		t.Fatalf("draft = %+v", drafts)
	}

	// Bob is a member of Field, not an owner: he is not shown it and cannot
	// publish it.
	if _, err := p.InboxGet(bobSigned, d.ID); !errors.Is(err, inbox.ErrNotFound) {
		t.Fatalf("a member read the draft: %v", err)
	}
	if _, err := p.InboxResolve(t.Context(), bobSigned, d.ID, d.Parts[0].ID, platform.Resolution{Action: platform.ActPublish}); err == nil {
		t.Fatal("a member published a draft")
	}
	seen, err := p.InboxGet(adaSigned, d.ID)
	if err != nil || !strings.Contains(strings.Join(seen.Parts[0].Actions, ","), platform.ActPublish) {
		t.Fatalf("the owner sees %+v, %v", seen, err)
	}
	if _, err := p.InboxResolve(t.Context(), adaSigned, d.ID, d.Parts[0].ID, platform.Resolution{Action: platform.ActPublish}); err != nil {
		t.Fatal(err)
	}
	var posted []inbox.Message
	for _, m := range about(p, run.ID) {
		if m.Broadcast {
			posted = append(posted, m)
		}
	}
	if len(posted) != 1 || posted[0].From != (inbox.From{Kind: "agent", Name: "recapper", ApprovedBy: "ada"}) || posted[0].Parts[0].Text != "Revenue is up." {
		t.Fatalf("published = %+v", posted)
	}
	if after, _ := p.InboxGet(adaSigned, d.ID); after.State != inbox.Resolved {
		t.Fatalf("the draft is still %s", after.State)
	}
	// Deciding again changes nothing.
	if _, err := p.InboxResolve(t.Context(), adaSigned, d.ID, d.Parts[0].ID, platform.Resolution{Action: platform.ActPublish}); err != nil {
		t.Fatal(err)
	}
	if n := len(about(p, run.ID)); n != 2 {
		t.Fatalf("publishing twice posted again: %d messages", n)
	}
}

func TestADiscardedDraftPostsNothing(t *testing.T) {
	p := recapping(t, "Quarter closed")
	run := recap(t, p, "manual-1", "")
	var drafts []inbox.Message
	eventually(t, "the workspace did not hold its agent's post", func() bool { drafts = about(p, run.ID); return len(drafts) > 0 })
	govern := auth.Principal{Subject: "root", Tenant: tenant, Kind: auth.User, Roles: []auth.Role{auth.Admin}}
	if _, err := p.InboxResolve(t.Context(), adaSigned, drafts[0].ID, drafts[0].Parts[0].ID, platform.Resolution{Action: platform.ActDiscard}); err == nil {
		t.Fatal("someone who does not own the workspace discarded its draft")
	}
	if _, err := p.InboxResolve(t.Context(), govern, drafts[0].ID, drafts[0].Parts[0].ID, platform.Resolution{Action: platform.ActDiscard}); err != nil {
		t.Fatal(err)
	}
	if got := about(p, run.ID); len(got) != 1 || got[0].State != inbox.Resolved {
		t.Fatalf("after discarding = %+v", got)
	}
}

// A project posts at most 20 announcements a day. An answer past that is not
// lost: it goes to the schedule's creator, saying why.
func TestARecapPastTheDailyLimitReachesItsCreator(t *testing.T) {
	p := recapping(t, "Again\nMore news.")
	for range inbox.DefaultPostCaps().PerProject {
		if _, err := p.Inbox.Post(tenant, inbox.Message{
			Subject: "Earlier", From: inbox.From{Kind: "person", Name: "ada"}, Source: inbox.Source{Project: "Field"},
			Parts: []inbox.Part{{Type: inbox.PartText, Text: "x"}},
		}, time.Now(), inbox.PostCaps{}); err != nil {
			t.Fatal(err)
		}
	}
	run := recap(t, p, "manual-1", "Field")
	var held inbox.Message
	eventually(t, "the creator was not sent the answer", func() bool {
		for _, m := range snapshot(p) {
			if m.Kind == inbox.KindResult && m.Source.Run == run.ID {
				held = m
				return true
			}
		}
		return false
	})
	if held.To != "ada" || !strings.Contains(held.Parts[0].Text, "Not posted") || !strings.Contains(held.Parts[0].Text, "More news.") {
		t.Fatalf("held back = %+v", held)
	}
	if got := about(p, run.ID); len(got) != 0 {
		t.Fatalf("posted past the limit: %+v", got)
	}
}

func snapshot(p *platform.Platform) []inbox.Message {
	msgs, _ := p.Inbox.Snapshot(tenant)
	return msgs
}
