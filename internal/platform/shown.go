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
// that subject. Their name is the one they chose for themselves, as the
// Directory and the console's own header show it, else the one they signed
// in with.
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
	if s.p.Prefs != nil {
		if own := s.p.Prefs.Get(s.tenant, subject).DisplayName; own != "" {
			who.Name = own
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

// Names calls the people of one tenant as the console shows them. Every
// route that names a person by subject, the key the platform keeps them by,
// sends what Names calls them beside it, so a reader sees a name and never a
// subject that holds an address. It looks each subject up in the directory
// once, and is made for one answer.
type Names struct{ s *shower }

// Names is how one answer in a tenant calls people.
func (p *Platform) Names(tenant string) Names { return Names{s: p.shower(tenant)} }

// Of is what a subject is called: the person's name, else their handle,
// else the subject itself when it holds no address (a machine's, or a word
// such as system), else someone. No subject is called nothing.
func (n Names) Of(subject string) string {
	if subject == "" {
		return ""
	}
	return n.s.name(subject)
}

// Key is how a person is named outside the platform: their handle, else
// their subject when it holds no address, else "".
func (n Names) Key(subject string) string { return n.s.key(subject) }

// NameOf is what a person is called, for one name (see Names).
func (p *Platform) NameOf(tenant, subject string) string { return p.Names(tenant).Of(subject) }

// mention is a mention as it is handed out: from its poster as their message
// is shown now (message), by handle and by name, its part naming them by the
// same handle, and its subject by the same name. What a mention keeps of its
// poster is the name they went by when they posted, which is no subject, so
// it is never looked up as one: it is what a mention whose message cannot be
// read is shown from, as it was kept.
func (n Names) mention(m inbox.Message) inbox.Message {
	kept := m.From.Name
	m.From.Display = kept
	for i := range m.Parts {
		part := m.Parts[i].Mention
		if part == nil || n.s.p.Threads == nil {
			continue
		}
		posted, err := n.s.p.Threads.Message(n.s.tenant, part.Thread, part.Message)
		if err != nil {
			continue
		}
		shown := n.s.message(posted)
		if shown.By != "" {
			m.From.Name, part.By = shown.By, shown.By
		}
		m.From.Display = shown.Name
		if rest, ok := strings.CutPrefix(m.Subject, kept+" "); ok && kept != "" {
			m.Subject = shown.Name + " " + rest
		}
		break
	}
	return m
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
	// A message is called what its poster is called now, as everywhere else
	// shows them: one who had no name yet when they posted (only a handle,
	// or a subject recorded from a grant) and has given one since reads by
	// it. A poster the directory does not know keeps the name they posted
	// under, unless it was kept before names were checked and is an address.
	who := s.person(m.By)
	kept := m.Name
	if strings.Contains(kept, "@") {
		kept = ""
	}
	m.Name = cmp.Or(who.Name, kept, who.Handle, who.ShownSubject(), someone)
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
