package platform

import (
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/inbox"
	"github.com/urmzd/humagen/pkg/projects"
)

// Every project has an inbox: the inbox messages whose Source.Project is the
// project, read by everyone who can see it. An announcement is a message in
// it, posted by a person, by an agent scheduled in the project, or by humagen
// in the workspace's own inbox for a release. It reaches the members of the
// project and of every project below it, decided when it is read: one
// message, never a copy per person. Each reader keeps their own copy hidden
// or snoozed, and a withdrawal is everyone's.

// Announcement is what is posted: a subject line, the text, and how much it
// matters. Run is the run that wrote it, for an agent's.
type Announcement struct {
	Subject  string
	Text     string
	Severity inbox.Severity
	Run      string
}

// Announce posts an announcement in a project's inbox, "~" for the
// workspace's, from a person or an agent. It is the platform's one way in, for
// people (PostAnnouncement), for agents scheduled in a project and for humagen's
// own release notes (PostReleaseNotes). A project may post 20 a day and a
// workspace 200 (inbox.ErrLimit), and the text is plain: surfaces escape any
// mention syntax in it before rendering.
func (p *Platform) Announce(tenant, project string, from inbox.From, a Announcement, at time.Time) (inbox.Message, error) {
	m, _, err := p.announce(tenant, project, "", from, a, at)
	return m, err
}

// announce is Announce, posted once under key when key is set: an
// announcement ever posted under it is returned with false instead.
func (p *Platform) announce(tenant, project, key string, from inbox.From, a Announcement, at time.Time) (inbox.Message, bool, error) {
	project = projects.NameOf(project)
	if !p.Projects.Tree(tenant).Has(project) {
		return inbox.Message{}, false, fmt.Errorf("%w: %q", projects.ErrNotFound, project)
	}
	m := inbox.Message{
		Subject: a.Subject, From: from, Severity: a.Severity,
		Source: inbox.Source{Project: project, Run: a.Run},
		Parts:  []inbox.Part{{Type: inbox.PartText, Text: a.Text}},
	}
	if from.Kind == "agent" {
		m.Source.Agent = from.Name
	}
	if key != "" {
		return p.Inbox.PostOnce(tenant, key, m, at, inbox.DefaultPostCaps())
	}
	m, err := p.Inbox.Post(tenant, m, at, inbox.DefaultPostCaps())
	return m, err == nil, err
}

// PostReleaseNotes posts what a release of humagen brings in the workspace's
// own inbox, from humagen, so it reaches everyone who reads the workspace as any
// announcement there does. It is keyed release:<version> and posted once: a
// restart, a later process on the same state or a withdrawal never posts it
// again, and posted reports whether this call posted it.
func (p *Platform) PostReleaseNotes(tenant, version string, a Announcement) (m inbox.Message, posted bool, err error) {
	if version == "" {
		return inbox.Message{}, false, fmt.Errorf("%w: release notes name their version", inbox.ErrInvalid)
	}
	a.Severity, a.Run = inbox.Info, ""
	return p.announce(tenant, projects.Root, "release:"+version, inbox.From{Kind: "system", Name: "humagen"}, a, p.now())
}

// Announcer is the lowest role that may post in a project: its Announce, or
// owner when it names none.
func Announcer(pr projects.Project) projects.Role {
	if pr.Announce == projects.None {
		return projects.Owner
	}
	return pr.Announce
}

// PostAnnouncement posts an announcement as the caller, who needs at least
// the role the project lets announce (owner unless it says otherwise).
func (p *Platform) PostAnnouncement(who auth.Principal, project string, a Announcement) (inbox.Message, error) {
	tree := p.Projects.Tree(who.Tenant)
	pr, _, err := p.projectRole(tree, who, projects.NameOf(project), projects.Viewer)
	if err != nil {
		return inbox.Message{}, err
	}
	if _, _, err := p.projectRole(tree, who, pr.Name, Announcer(pr)); err != nil {
		return inbox.Message{}, err
	}
	m, err := p.Announce(who.Tenant, pr.Name, inbox.From{Kind: "person", Name: who.Subject}, a, p.now())
	if err != nil {
		return inbox.Message{}, err
	}
	p.audit(who, m, "", "announce", "")
	return p.seenBy(tree, p.Names(who.Tenant), m, who), nil
}

// WithdrawAnnouncement withdraws an announcement for everyone. Its author or
// an owner of the project withdraws it; an id that is not an announcement in
// this project is not found.
func (p *Platform) WithdrawAnnouncement(who auth.Principal, project, id string) error {
	tree := p.Projects.Tree(who.Tenant)
	pr, access, err := p.projectRole(tree, who, projects.NameOf(project), projects.Viewer)
	if err != nil {
		return err
	}
	m, err := p.Inbox.GetAs(who.Tenant, id, inbox.Reader{Subject: who.Subject, Projects: []string{pr.Name}})
	if err != nil || m.Kind != inbox.KindAnnouncement || m.Source.Project != pr.Name {
		return fmt.Errorf("%w: no announcement %q in %s", inbox.ErrNotFound, id, display(pr))
	}
	author := m.From.Kind == "person" && m.From.Name == who.Subject
	if !author && access.Role != projects.Owner {
		return fmt.Errorf("%w: its author withdraws it, or %s", projects.ErrForbidden, roleNeeded(projects.Owner, titleOf(pr, p.Settings(who.Tenant).WorkspaceName)))
	}
	// m is the caller's own view, where an announcement they hid for
	// themselves reads as closed. Whether it is closed for everyone is the
	// store's to say, so withdrawing one already withdrawn changes nothing.
	_, changed, err := p.Inbox.Withdraw(who.Tenant, id, who.Subject, "Withdrawn by "+p.NameOf(who.Tenant, who.Subject), p.now())
	if err != nil {
		return err
	}
	if changed {
		p.audit(who, m, "", "withdraw", "")
	}
	return nil
}

// display is a project's name in an error: "~" for the workspace.
func display(pr projects.Project) string {
	if pr.IsRoot() {
		return projects.RootAddress
	}
	return pr.Name
}

// ProjectInbox returns one page of a project's inbox, which everyone who can
// see the project reads: what is filed in it, and with children what is filed
// in the projects below it too. There an announcement reaches every reader,
// member or not, and a message addressed to someone in particular still
// reaches only them. As everywhere a message is read, a client's machine is
// shown its own client's receipts and nobody else any: a Slack app catches
// up on a bound project's announcements from this page.
func (p *Platform) ProjectInbox(who auth.Principal, project string, children bool, q inbox.Query) ([]inbox.Message, string, error) {
	tree := p.Projects.Tree(who.Tenant)
	pr, _, err := p.projectRole(tree, who, projects.NameOf(project), projects.Viewer)
	if err != nil {
		return nil, "", err
	}
	q.Projects, q.Shared = []string{pr.Name}, true
	if children {
		q.Projects = tree.Subtree(pr.Name)
	}
	msgs, next, err := p.Inbox.Page(who.Tenant, p.inboxQuery(tree, who, q))
	names := p.Names(who.Tenant)
	for i := range msgs {
		msgs[i] = p.ownDeliveries(who, p.seenBy(tree, names, msgs[i], who))
	}
	return msgs, next, err
}

// SurfaceNoticesFor reports, for a client's machine, a notice of a message
// addressed to people its clients map, and who each of them is there: a
// mention of them, or a question they may answer, named by subject or sent
// to a role they hold, as experts named by a role are. The machine reads it
// for each of them (act_as) and tells them in a direct message. Nothing
// reaches the machine about anyone it does not map, and one notice names at
// most MaxNotified people, as one message notifies at most that many.
func (p *Platform) SurfaceNoticesFor(who auth.Principal, n inbox.Notice) []ActAs {
	switch n.Kind {
	case inbox.KindMention, inbox.KindClarification, inbox.KindApproval:
	default:
		return nil
	}
	if who.Kind != auth.Machine {
		return nil
	}
	var tree *projects.Tree
	var out []ActAs
	for _, c := range p.Clients.List(who.Tenant) {
		if !machineOf(c, who) || c.Slack == nil {
			continue
		}
		for _, user := range slices.Sorted(maps.Keys(c.Slack.People)) {
			// Who the message names is read first, by subject and roles,
			// so nobody else's projects are walked.
			named := p.mapped(who, c.Slack.People[user])
			m, err := p.Inbox.GetAs(who.Tenant, n.ID, named)
			if err != nil || !addressedTo(m, named) {
				continue
			}
			person := principalOf(who, named)
			if !InboxSees(person, n.Kind) {
				continue
			}
			if tree == nil {
				tree = p.Projects.Tree(who.Tenant)
			}
			if m, err = p.Inbox.GetAs(who.Tenant, n.ID, p.inboxReader(tree, person)); err != nil || !p.Seer(person).Message(m) {
				continue
			}
			out = append(out, ActAs{Client: c.ID, User: user})
			if len(out) == MaxNotified {
				return out
			}
		}
	}
	return out
}

// addressedTo reports whether a message names a reader in particular: by
// subject, or by a role they hold. A message to a project's members or its
// owners does not; it is read in their inbox, not sent to them.
func addressedTo(m inbox.Message, r inbox.Reader) bool {
	if m.To != "" {
		return m.To == r.Subject
	}
	if r.Subject != "" && slices.Contains(m.Audience, r.Subject) {
		return true
	}
	for _, role := range r.Roles {
		if role != "" && slices.Contains(m.Audience, projects.RolePrefix+role) {
			return true
		}
	}
	return false
}

// InboxNoticeFor reports whether a notice about an inbox message may reach a
// caller's event stream: they may see its kind, and it is addressed to them
// as their own inbox reads it (noticeReader), so an id from a project they
// cannot see, or an announcement not for them, never reaches it.
func (p *Platform) InboxNoticeFor(who auth.Principal, n inbox.Notice) bool {
	if !InboxSees(who, n.Kind) {
		return false
	}
	m, err := p.Inbox.GetAs(who.Tenant, n.ID, p.noticeReader(p.Projects.Tree(who.Tenant), who))
	return err == nil && p.Seer(who).Message(m)
}

// noticeReader is who a caller is to the notices on their stream: their own
// inbox's reader, and for a client's machine every project whose general
// channel one of its clients is bound to and which it may see, a viewer's
// included, as the app posts that project's announcements there whatever
// its role. A members-only project, which a machine has no role in, is
// never among them.
func (p *Platform) noticeReader(tree *projects.Tree, who auth.Principal) inbox.Reader {
	r := p.inboxReader(tree, who)
	if who.Kind != auth.Machine {
		return r
	}
	var seer *Seer
	for _, c := range p.Clients.List(who.Tenant) {
		if !machineOf(c, who) {
			continue
		}
		for _, b := range p.Projects.Bindings(who.Tenant, c.ID) {
			if b.Channel != projects.General || slices.Contains(r.Projects, b.Project) {
				continue
			}
			if seer == nil {
				seer = p.Seer(who)
			}
			if seer.Project(b.Project) {
				r.Projects = append(r.Projects, b.Project)
			}
		}
	}
	return r
}
