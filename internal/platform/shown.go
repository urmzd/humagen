package platform

import (
	"cmp"
	"slices"
	"strings"

	"github.com/urmzd/warp/pkg/inbox"
	"github.com/urmzd/warp/pkg/people"
	"github.com/urmzd/warp/pkg/projects"
	"github.com/urmzd/warp/pkg/threads"
)

// How a thread shows the people in it.
//
// A thread keeps a person by subject, the key they sign in with, and some
// subjects hold an address: a Tailscale login, or a subject a Slack people
// map names. No address is shown (people.ShownSubject), so what a thread
// hands out names a person by their handle, the name they are tagged by,
// and calls them by their name, or their handle without one. The subject
// stays the key inside the platform.

// someone is what a thread calls a person it cannot name.
const someone = "someone"

// shower names the people of one tenant's threads, looking each subject up
// in the directory once. It is made for one answer, so what it holds is
// bounded by what that answer names.
type shower struct {
	p      *Platform
	tenant string
	known  map[string]people.Person
}

func (p *Platform) shower(tenant string) *shower {
	return &shower{p: p, tenant: tenant, known: map[string]people.Person{}}
}

// person is who a subject is in the directory, or a person known only by
// that subject.
func (s *shower) person(subject string) people.Person {
	if who, ok := s.known[subject]; ok {
		return who
	}
	who := people.Person{Subject: subject}
	if s.p.People != nil {
		if got, err := s.p.People.Get(s.tenant, subject); err == nil {
			who = got
		}
	}
	s.known[subject] = who
	return who
}

// key is how a person is named outside the platform: their handle, or their
// subject when they have none and it holds no address. "" when neither.
func (s *shower) key(subject string) string {
	if subject == "" {
		return ""
	}
	who := s.person(subject)
	return cmp.Or(who.Handle, who.ShownSubject())
}

// Person is who a subject is, for a transcript (threads.Names): their name
// and their handle.
func (s *shower) Person(subject string) (name, handle string) {
	who := s.person(subject)
	return who.Name, who.Handle
}

// name is what a person is called: their name, their handle, their subject
// when it holds no address, or someone.
func (s *shower) name(subject string) string {
	who := s.person(subject)
	return cmp.Or(who.Name, who.Handle, who.ShownSubject(), someone)
}

// who names whoever a message was sent to: a person or an expert by key,
// and the experts a role names as the role.
func (s *shower) who(to string) string {
	if strings.HasPrefix(to, projects.RolePrefix) {
		return to
	}
	return s.key(to)
}

// list names each person once, in order, leaving out anyone it cannot name.
func (s *shower) list(subjects []string, name func(string) string) []string {
	out := make([]string, 0, len(subjects))
	for _, who := range subjects {
		if k := name(who); k != "" && !slices.Contains(out, k) {
			out = append(out, k)
		}
	}
	return out
}

func (s *shower) thread(th threads.Thread) threads.Thread {
	ps := make([]threads.Participant, len(th.Participants))
	for i, who := range th.Participants {
		if who.Kind == threads.Person {
			who.Name = s.key(who.Name)
		}
		who.AddedBy = s.key(who.AddedBy)
		ps[i] = who
	}
	th.Participants = ps
	th.CreatedBy = s.key(th.CreatedBy)
	return th
}

func (s *shower) mentions(ms []threads.Mention) []threads.Mention {
	out := make([]threads.Mention, len(ms))
	for i, mn := range ms {
		// A refused tag of a person nobody goes by keeps the handle as
		// written, which is no subject.
		if mn.Kind == threads.Person && mn.Refused == "" {
			mn.Name = s.key(mn.Name)
		}
		out[i] = mn
	}
	return out
}

func (s *shower) message(m threads.Message) threads.Message {
	// A name kept before names were checked could be the subject.
	if m.Name == "" || strings.Contains(m.Name, "@") {
		m.Name = s.name(m.By)
	}
	m.By = s.key(m.By)
	m.Mentions = s.mentions(m.Mentions)
	return m
}

// ShowThread is a thread's record as it is handed out: people by handle.
func (p *Platform) ShowThread(tenant string, th threads.Thread) threads.Thread {
	return p.shower(tenant).thread(th)
}

// ShowPosted is what a message did, as it is handed out: people by handle,
// the experts a role names by the role.
func (p *Platform) ShowPosted(tenant string, posted Posted) Posted {
	s := p.shower(tenant)
	posted.Thread = s.thread(posted.Thread)
	posted.Message = s.message(posted.Message)
	posted.Notified = s.list(posted.Notified, s.who)
	posted.Refused = s.mentions(posted.Refused)
	return posted
}

// ShowTurns is a thread and a page of its turns as they are handed out:
// the people in its record and its messages by handle. A run is shown as
// every run route shows it.
func (p *Platform) ShowTurns(tenant string, th threads.Thread, turns []ThreadTurn) (threads.Thread, []ThreadTurn) {
	s := p.shower(tenant)
	out := make([]ThreadTurn, len(turns))
	for i, t := range turns {
		if t.Message != nil {
			m := s.message(*t.Message)
			t.Message = &m
		}
		if t.Announcement != nil && t.Announcement.Kind == threads.Person {
			a := *t.Announcement
			a.Name, a.By = s.name(a.By), s.key(a.By)
			t.Announcement = &a
		}
		out[i] = t
	}
	return s.thread(th), out
}

// ShowSummaries is a page of conversations as it is handed out: the people
// in each by handle.
func (p *Platform) ShowSummaries(tenant string, page []ThreadSummary) []ThreadSummary {
	s := p.shower(tenant)
	out := make([]ThreadSummary, len(page))
	for i, sum := range page {
		sum.People = s.list(sum.People, s.key)
		out[i] = sum
	}
	return out
}

// mentionOf is the inbox part a message raises for whoever it tags: the
// poster by handle, never by subject.
func (s *shower) mentionOf(th threads.Thread, m threads.Message) *inbox.MentionPart {
	return &inbox.MentionPart{
		Thread: th.ID, Message: m.ID, Project: th.Project, Channel: th.Channel, By: s.key(m.By), Excerpt: inbox.Excerpt(m.Text),
	}
}
