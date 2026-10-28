package platform_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/saige/agent/agenttest"
	saigetypes "github.com/urmzd/saige/agent/types"

	"github.com/urmzd/humagen/fiber/pkg/event"
	"github.com/urmzd/humagen/fiber/pkg/turn/native"
	"github.com/urmzd/humagen/internal/wire"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/credentials"
	"github.com/urmzd/humagen/pkg/inbox"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
)

// recapping starts a platform with an agent, "recapper", whose every answer
// is answer, and a project, Field, that ada created and so owns.
func recapping(t *testing.T, answer string) *platform.Platform {
	t.Helper()
	p := startWith(t, platform.Config{Ext: platform.Extensions{Providers: map[string]native.ProviderFunc{
		"says": func(native.Spec) (saigetypes.Provider, error) {
			out := make([][]saigetypes.Delta, 8)
			for i := range out {
				out[i] = agenttest.TextResponse(answer)
			}
			return &agenttest.ScriptedProvider{Responses: out}, nil
		},
	}}})
	if _, err := p.Agents.Create(t.Context(), tenant, wire.AgentSpec{Name: "recapper", Provider: "says", Model: "m", SystemPrompt: "Recap."}, "test"); err != nil {
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
	run, err := p.Invoke(t.Context(), wire.NewEvent(wire.Raise{ID: id, Tenant: tenant, Agent: "recapper", Origin: runs.OriginManual, Text: "What changed?", Actor: event.Actor{ID: "bob"}, Context: map[string]string{
		"cron_schedule": "field-recap", "cron_expression": "41 8 * * 1", "created_by": "ada",
		runs.MetaProject: project, platform.MetaDeliver: platform.DeliverProject,
	}}))
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
	// What it is about is the run's conversation, named by what it asked.
	if seen, err := p.InboxGet(adaSigned, m.ID); err != nil || seen.Source.ThreadTitle != "What changed?" {
		t.Fatalf("the recap's conversation is called %q (%v)", seen.Source.ThreadTitle, err)
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

// A run now acts for the person who pressed it when they wrote what it runs,
// so its tools that act as a person reach their own credentials. What those
// reached is theirs alone: the answer is neither posted in the project nor
// held for its owners, nor sent to a creator who is someone else, but sent to
// that person, saying why. The same schedule run for nobody posts as before.
func TestAScheduledAnswerThatMayHoldAPersonsCredentialIsTheirsAlone(t *testing.T) {
	w := newWorld(t, platform.Config{})
	w.model.mu.Lock()
	w.model.recall = true
	w.model.mu.Unlock()
	ctx := t.Context()
	if _, err := w.p.CreateProject(ctx, tenant, ada, platform.ProjectInput{Name: "Launch"}); err != nil {
		t.Fatal(err)
	}
	w.connect(t, ana, credentials.Input{Ref: "env:HUMAGEN_PERSON_ANA_ISSUES"})
	scheduled := func(id, deliver string, actor event.Actor) runs.Run {
		t.Helper()
		run, err := w.p.Invoke(ctx, wire.NewEvent(wire.Raise{ID: id, Tenant: tenant, Agent: "worker", Origin: runs.OriginManual, Text: "Any open bugs?", Actor: actor, Context: map[string]string{
			"cron_schedule": "launch-recap", "cron_expression": "41 8 * * 1", "created_by": "ada",
			runs.MetaProject: "Launch", platform.MetaDeliver: deliver,
		}}))
		if err != nil {
			t.Fatal(err)
		}
		return await(t, w.p, run.ID, runs.Completed)
	}
	results := func(run string) []inbox.Message {
		var out []inbox.Message
		for _, m := range snapshot(w.p) {
			if m.Source.Run == run && (m.Kind == inbox.KindResult || m.Kind == inbox.KindAnnouncement) {
				out = append(out, m)
			}
		}
		return out
	}
	for _, deliver := range []string{platform.DeliverProject, platform.DeliverInbox} {
		run := scheduled("manual-ana-"+deliver, deliver, platform.ActorOf(ana))
		if run.Answer != found {
			t.Fatalf("ana's run now answered %q", run.Answer)
		}
		var got []inbox.Message
		eventually(t, "ana was sent her answer", func() bool { got = results(run.ID); return len(got) > 0 })
		if len(got) != 1 || got[0].Kind != inbox.KindResult || got[0].To != "ana" || !strings.Contains(got[0].Parts[0].Text, "PRIV-7") {
			t.Fatalf("deliver %s: %+v", deliver, got)
		}
	}
	// Run for nobody, the schedule posts in the project as it says.
	run := scheduled("manual-nobody", platform.DeliverProject, event.Actor{ID: "cron", Bot: true})
	eventually(t, "the answer was posted", func() bool {
		got := results(run.ID)
		return len(got) == 1 && got[0].Kind == inbox.KindAnnouncement && got[0].Source.Project == "Launch"
	})
}
