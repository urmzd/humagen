package platform

import (
	"context"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/urmzd/warp/pkg/inbox"
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
		// Schedules refuse deliver: project until project inboxes are
		// built, so no run carries it yet.
		p.log.Warn("platform: a run asks to be posted to its project, which is not built", "tenant", run.Tenant, "run", run.ID)
	}
}

// deliverToInbox sends the answer to the person who created the schedule, as
// a result only they are shown. A schedule with no creator on record has
// nobody to send it to, so nothing is sent.
func (p *Platform) deliverToInbox(run runs.Run) {
	to := run.Tag().Meta[metaCreatedBy]
	subject, text, post := announcementOf(run.Answer)
	if !post {
		return
	}
	if to == "" {
		p.log.Warn("platform: a scheduled answer has nobody to go to", "tenant", run.Tenant, "run", run.ID, "schedule", run.Schedule)
		return
	}
	// A one-line answer is its own subject, and still the message's body,
	// as every result has one.
	if text == "" {
		text = subject
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
