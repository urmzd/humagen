package platform

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/urmzd/warp/pkg/auth"
	"github.com/urmzd/warp/pkg/inbox"
	"github.com/urmzd/warp/pkg/projects"
	"github.com/urmzd/warp/pkg/runs"
)

// Scheduled agents and where their answers go.
//
// A schedule runs an agent with nobody present, so nobody reads the answer
// unless the schedule says where it goes. The schedule's deliver setting is
// written into its meta when the schedule is written, so every run it raises,
// by the clock or by a person's "run now", carries the value it was raised
// with, and changing the schedule later does not change where an earlier
// run's answer goes.
//
// A recap is nothing more than this: a schedule in a project whose answer is
// posted to the project.

// MetaDeliver is the schedule meta key that says where the answers of a
// schedule's runs go. Only the server writes it, and only the cron trigger
// and a person's run now carry it into a run.
const MetaDeliver = "warp.deliver"

// metaCreatedBy is the schedule meta key naming who created it.
const metaCreatedBy = "created_by"

// Where a scheduled run's answer goes.
const (
	// DeliverNone keeps the answer on the run, as before: nobody is told.
	DeliverNone = ""
	// DeliverInbox sends it to the schedule's creator, as a result.
	DeliverInbox = "inbox"
	// DeliverProject posts it to the schedule's project as an announcement
	// from the agent.
	DeliverProject = "project"
)

// ValidDeliver reports whether a value names a place an answer can go.
func ValidDeliver(v string) bool { return v == DeliverNone || v == DeliverInbox || v == DeliverProject }

// Bounds on what a scheduled agent's answer becomes.
const (
	// MaxAnnouncementSubject is the most of an answer's first line kept as
	// its subject.
	MaxAnnouncementSubject = 150
	// MaxAnnouncementText is the most of the rest kept as its text.
	MaxAnnouncementText = 4000
)

// scheduledAnswer reports where a run's answer goes, or "" when it goes
// nowhere: only a completed root run a schedule raised is delivered, as the
// schedule said when the run was raised.
func scheduledAnswer(run runs.Run) string {
	if run.Status != runs.Completed || run.Parent != "" || run.Schedule == "" ||
		run.Origin != "cron" && run.Origin != runs.OriginManual {
		return DeliverNone
	}
	return run.Tag().Meta[MetaDeliver]
}

// deliverAnswer sends a scheduled run's answer where its schedule said. It is
// called once, when the run ends.
func (p *Platform) deliverAnswer(_ context.Context, run runs.Run) {
	switch scheduledAnswer(run) {
	case DeliverInbox:
		p.deliverToInbox(run)
	case DeliverProject:
		p.deliverToProject(run)
	}
}

// MaxOpenDrafts is how many of its agents' drafts a project holds for its
// owners at once.
const MaxOpenDrafts = 20

// deliverToProject posts a scheduled run's answer in the run's project, as an
// announcement from the agent that wrote it, which reaches the members of the
// project and of every project below it. A project whose agent posts are
// reviewed holds it as a draft for its owners instead. An answer that cannot
// be posted, because the project's announcements for the day are used up or
// its owners already hold as many drafts as they may, goes to the schedule's
// creator, saying why, so nobody's answer is lost.
func (p *Platform) deliverToProject(run runs.Run) {
	subject, text, post := announcementOf(run.Answer)
	if !post {
		return
	}
	if text == "" {
		text = subject
	}
	tree := p.Projects.Tree(run.Tenant)
	if !tree.Has(run.Project) {
		p.log.Warn("platform: a scheduled answer's project is gone", "tenant", run.Tenant, "run", run.ID, "project", run.Project)
		p.heldBack(run, subject, text, "its project is gone")
		return
	}
	where := p.whereOf(run.Tenant, tree, run.Project)
	if projects.EffectiveAgentPosts(tree, run.Project) == projects.AgentPostsPublish {
		_, err := p.postFromAgent(run.Tenant, inbox.DraftPart{
			Project: run.Project, Agent: run.Agent, Run: run.ID, Subject: subject, Text: text,
		}, run.Thread, "")
		switch {
		case errors.Is(err, inbox.ErrLimit):
			p.heldBack(run, subject, text, where+" has posted all the announcements it may today")
		case err != nil:
			p.log.Error("platform: a scheduled answer could not be posted", "tenant", run.Tenant, "run", run.ID, "error", err)
		}
		return
	}
	if p.openDrafts(run.Tenant, run.Project) >= MaxOpenDrafts {
		p.heldBack(run, subject, text, fmt.Sprintf("the owners of %s already hold %d drafts", where, MaxOpenDrafts))
		return
	}
	p.raiseInbox(run.Tenant, draftKey(run.ID), inbox.Message{
		Kind: inbox.KindAnnouncement, Subject: subject, Severity: inbox.Info,
		From: inbox.From{Kind: "agent", Name: run.Agent},
		// Only the project's owners decide, whoever they are when it is
		// read.
		Audience: []string{inbox.OwnersAudience(run.Project)},
		Source:   inbox.Source{Run: run.ID, Thread: run.Thread, Agent: run.Agent, Project: run.Project},
		Parts: []inbox.Part{{Type: inbox.PartDraft, Draft: &inbox.DraftPart{
			Project: run.Project, Agent: run.Agent, Run: run.ID, Subject: subject, Text: text,
		}}},
	}, p.now())
}

func draftKey(run string) string { return "draft:" + run }

// postFromAgent posts an announcement an agent wrote, in d's project, linked
// to the run that wrote it. approvedBy is who published it from a draft, or
// "" for a post the project publishes as it comes.
func (p *Platform) postFromAgent(tenant string, d inbox.DraftPart, thread, approvedBy string) (inbox.Message, error) {
	return p.Inbox.Post(tenant, inbox.Message{
		Subject:  d.Subject,
		From:     inbox.From{Kind: "agent", Name: d.Agent, ApprovedBy: approvedBy},
		Severity: inbox.Info,
		Source:   inbox.Source{Run: d.Run, Thread: thread, Agent: d.Agent, Project: d.Project},
		Parts:    []inbox.Part{{Type: inbox.PartText, Text: d.Text}},
	}, p.now(), inbox.PostCaps{})
}

// openDrafts counts the drafts a project's owners have still to decide.
func (p *Platform) openDrafts(tenant, project string) int {
	n := 0
	for _, m := range p.Inbox.OpenOf(tenant, inbox.KindAnnouncement) {
		for _, part := range m.Parts {
			if part.Type == inbox.PartDraft && part.Draft != nil && part.Draft.Project == project && part.State == inbox.PartOpen {
				n++
			}
		}
	}
	return n
}

// heldBack sends a scheduled answer that could not be posted to the
// schedule's creator, saying why.
func (p *Platform) heldBack(run runs.Run, subject, text, why string) {
	p.sendToCreator(run, subject, "Not posted: "+why+".\n\n"+text)
}

// deliverToInbox sends the answer to the person who created the schedule, as
// a result only they are shown. A schedule with no creator on record has
// nobody to send it to, so nothing is sent.
func (p *Platform) deliverToInbox(run runs.Run) {
	subject, text, post := announcementOf(run.Answer)
	if !post {
		return
	}
	// A one-line answer is its own subject, and still the message's body,
	// as every result has one.
	if text == "" {
		text = subject
	}
	p.sendToCreator(run, subject, text)
}

// sendToCreator sends a result about a scheduled run to whoever created its
// schedule, the only one shown it.
func (p *Platform) sendToCreator(run runs.Run, subject, text string) {
	to := run.Tag().Meta[metaCreatedBy]
	if to == "" {
		p.log.Warn("platform: a scheduled answer has nobody to go to", "tenant", run.Tenant, "run", run.ID, "schedule", run.Schedule)
		return
	}
	p.raiseInbox(run.Tenant, "result:"+run.ID, inbox.Message{
		Kind: inbox.KindResult, To: to, Subject: subject,
		From: inbox.From{Kind: "agent", Name: run.Agent}, Severity: inbox.Info,
		Source: inbox.Source{Run: run.ID, Thread: run.Thread, Agent: run.Agent, Project: run.Project},
		Parts:  []inbox.Part{{Type: inbox.PartText, State: inbox.PartInfo, Text: text}},
	}, run.UpdatedAt)
}

// announcementOf turns an agent's answer into what is posted: its first line
// is the subject and the rest is the text, each bounded. An answer that is
// nothing but NONE, whatever its case, punctuation or spacing, says there is
// nothing to post, and post is false. The answer is a model's text, so it is
// kept plain: control characters other than line breaks and tabs are dropped,
// and the subject is one line.
func announcementOf(answer string) (subject, text string, post bool) {
	answer = strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\t':
			return r
		case r == utf8.RuneError, unicode.IsControl(r), unicode.Is(unicode.Cf, r):
			return -1
		}
		return r
	}, strings.ReplaceAll(answer, "\r\n", "\n"))
	answer = strings.TrimSpace(answer)
	if answer == "" || saysNone(answer) {
		return "", "", false
	}
	first, rest, _ := strings.Cut(answer, "\n")
	subject = strings.Join(strings.Fields(strings.Trim(strings.TrimSpace(first), "#*_ ")), " ")
	if subject == "" {
		subject = strings.Join(strings.Fields(first), " ")
	}
	return clipRunes(subject, MaxAnnouncementSubject), clipRunes(strings.TrimSpace(rest), MaxAnnouncementText), true
}

// saysNone reports an answer that is only the word NONE.
func saysNone(s string) bool {
	word := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, s)
	return word == "none"
}

// clipRunes shortens text to at most limit characters.
func clipRunes(s string, limit int) string {
	if utf8.RuneCountInString(s) <= limit {
		return s
	}
	n := 0
	for i := range s {
		if n == limit {
			return strings.TrimSpace(s[:i])
		}
		n++
	}
	return s
}

// ownsDraft refuses anyone but an owner of the draft's project: publishing
// puts the agent's words in front of everyone it reaches.
func (p *Platform) ownsDraft(who auth.Principal, d inbox.DraftPart) error {
	tree := p.Projects.Tree(who.Tenant)
	if tree.Resolve(d.Project, projectReader(who)).Role == projects.Owner {
		return nil
	}
	return fmt.Errorf("%w: deciding a draft needs projects:write, and you need to own %s", ErrForbidden, p.whereOf(who.Tenant, tree, d.Project))
}

// projectReader is who a caller is to the project tree.
func projectReader(who auth.Principal) projects.Reader {
	roles := make([]string, 0, len(who.Roles))
	for _, r := range who.Roles {
		roles = append(roles, string(r))
	}
	return projects.Reader{Subject: who.Subject, Roles: roles, Govern: who.Can(auth.ProjectsGovern), Machine: who.Kind == auth.Machine}
}

// withOwns adds to a reader the projects the caller owns, which an
// "owners:<name>" audience entry reaches: an agent's drafts are addressed so.
func (p *Platform) withOwns(who auth.Principal, r inbox.Reader) inbox.Reader {
	r.Owns = p.Projects.Tree(who.Tenant).Owned(projectReader(who))
	return r
}
