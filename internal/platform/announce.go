package platform

import (
	"fmt"
	"time"

	"github.com/urmzd/warp/pkg/auth"
	"github.com/urmzd/warp/pkg/inbox"
	"github.com/urmzd/warp/pkg/projects"
)

// Every project has an inbox: the inbox messages whose Source.Project is the
// project, read by everyone who can see it. An announcement is a message in
// it, posted by a person or by an agent scheduled in the project. It reaches
// the members of the project and of every project below it, decided when it
// is read: one message, never a copy per person. Each reader keeps their own
// copy hidden or snoozed, and a withdrawal is everyone's.

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
// people (PostAnnouncement) and for agents scheduled in a project. A project
// may post 20 a day and a workspace 200 (inbox.ErrLimit), and the text is
// plain: surfaces escape any mention syntax in it before rendering.
func (p *Platform) Announce(tenant, project string, from inbox.From, a Announcement, at time.Time) (inbox.Message, error) {
	project = projects.NameOf(project)
	if !p.Projects.Tree(tenant).Has(project) {
		return inbox.Message{}, fmt.Errorf("%w: %q", projects.ErrNotFound, project)
	}
	m := inbox.Message{
		Subject: a.Subject, From: from, Severity: a.Severity,
		Source: inbox.Source{Project: project, Run: a.Run},
		Parts:  []inbox.Part{{Type: inbox.PartText, Text: a.Text}},
	}
	if from.Kind == "agent" {
		m.Source.Agent = from.Name
	}
	return p.Inbox.Post(tenant, m, at, inbox.DefaultPostCaps())
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
	return p.seenBy(tree, m, who), nil
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
	if m.State == inbox.Resolved || m.State == inbox.Dismissed {
		return nil
	}
	if _, _, err := p.Inbox.Withdraw(who.Tenant, id, who.Subject, "Withdrawn by "+who.Subject, p.now()); err != nil {
		return err
	}
	p.audit(who, m, "", "withdraw", "")
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
// reaches only them.
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
	for i := range msgs {
		msgs[i] = p.seenBy(tree, msgs[i], who)
	}
	return msgs, next, err
}

// InboxNoticeFor reports whether a notice about an inbox message may reach a
// caller's event stream: they may see its kind, and it is addressed to them
// as their own inbox reads it, so an id from a project they cannot see, or an
// announcement not for them, never reaches it.
func (p *Platform) InboxNoticeFor(who auth.Principal, n inbox.Notice) bool {
	if !InboxSees(who, n.Kind) {
		return false
	}
	m, err := p.Inbox.GetAs(who.Tenant, n.ID, p.inboxReader(p.Projects.Tree(who.Tenant), who))
	return err == nil && p.Seer(who).Message(m)
}
