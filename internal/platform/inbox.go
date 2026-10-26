package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/urmzd/fiber/pkg/registry"

	"github.com/urmzd/humagen/pkg/alerts"
	"github.com/urmzd/humagen/pkg/artifacts"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/inbox"
	"github.com/urmzd/humagen/pkg/knowledge"
	"github.com/urmzd/humagen/pkg/memory"
	"github.com/urmzd/humagen/pkg/policy"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
)

// The inbox is one state seen from two sides. An alert and the message about
// it are the same thing: acknowledging either settles both. So every change to
// a source that has a message goes through a method here, which keeps them in
// step, and nothing writes to the inbox store from anywhere else.
//
// Resolving a part is doing the thing it stands for. The right that is checked
// is the right to do that thing, not the right to read an inbox: approving
// memories needs agents:write, raising a limit needs billing:write.

// Inbox errors a caller can match.
var (
	// ErrAction reports an action a part does not take.
	ErrAction = errors.New("platform: no such action")
)

// Actions a part can take.
const (
	ActAcknowledge = "acknowledge"
	ActRetry       = "retry"
	ActKeep        = "keep"
	ActDiscard     = "discard"
	ActRaiseLimit  = "raise_limit"
	// ActDone sets a mention aside: the person has seen to it.
	ActDone = "done"
	// ActPublish posts an agent's draft as the announcement it wrote.
	ActPublish = "publish"
)

// Resolution is what a person decided about a part.
type Resolution struct {
	Action string
	// IDs are the proposed memories to keep.
	IDs  []string
	Note string
	// Limits are the new numbers when raising a limit.
	Limits inbox.Limits
	// Arguments, Choice and Text answer a question: edited arguments for an
	// approval, the option chosen, the words of a reply.
	Arguments json.RawMessage
	Choice    string
	Text      string
	// ActAs is the person a surface answers a question for.
	ActAs *ActAs
}

func alertKey(id string) string    { return "alert:" + id }
func runKey(id string) string      { return "run:" + id }
func artifactKey(id string) string { return "artifact:" + id }

func severityOf(s alerts.Severity) inbox.Severity {
	switch s {
	case alerts.Critical:
		return inbox.Critical
	case alerts.Warning:
		return inbox.Warning
	}
	return inbox.Info
}

// reads is the right needed to see a kind of message.
func reads(k inbox.Kind) auth.Permission {
	switch k {
	case inbox.KindSource:
		return auth.KnowledgeRead
	case inbox.KindAnnouncement:
		return auth.ProjectsRead
	case inbox.KindMemory:
		return auth.AgentsRead
	case inbox.KindFailure, inbox.KindApproval, inbox.KindClarification, inbox.KindResult, inbox.KindMention, inbox.KindOperation:
		return auth.RunsRead
	case inbox.KindArtifact:
		return auth.ArtifactsRead
	}
	return auth.AlertsRead
}

// writes is the right needed to set a kind of message aside. Hiding an
// announcement hides the reader's own copy and changes nothing others see,
// so reading it is enough.
func writes(k inbox.Kind) auth.Permission {
	switch k {
	case inbox.KindSource:
		return auth.KnowledgeWrite
	case inbox.KindAnnouncement:
		return auth.ProjectsRead
	case inbox.KindMemory:
		return auth.AgentsWrite
	case inbox.KindFailure, inbox.KindApproval, inbox.KindClarification, inbox.KindResult, inbox.KindMention, inbox.KindOperation:
		return auth.RunsWrite
	case inbox.KindArtifact:
		return auth.ArtifactsWrite
	}
	return auth.AlertsWrite
}

// needs is the right an action on a part takes.
func needs(t inbox.PartType, action string) (auth.Permission, bool) {
	switch {
	case t == inbox.PartSource && action == ActRetry:
		return auth.KnowledgeWrite, true
	case t == inbox.PartOperation && action == ActAcknowledge:
		return auth.RunsWrite, true
	case t == inbox.PartAlert && action == ActAcknowledge:
		return auth.AlertsWrite, true
	case t == inbox.PartFailure && (action == ActRetry || action == ActAcknowledge):
		return auth.RunsWrite, true
	case t == inbox.PartMemory && (action == ActKeep || action == ActDiscard):
		return auth.AgentsWrite, true
	case t == inbox.PartBudget && action == ActRaiseLimit:
		return auth.BillingWrite, true
	case t == inbox.PartBudget && action == ActAcknowledge:
		return auth.AlertsWrite, true
	case t == inbox.PartDraft && (action == ActPublish || action == ActDiscard):
		// Deciding an agent's post is the project's owners' to do: the
		// role is checked when the action is taken.
		return auth.ProjectsWrite, true
	case t == inbox.PartMention && (action == ActReply || action == ActDone):
		// Replying posts a message in the thread as the reader.
		return auth.RunsWrite, true
	case t == inbox.PartApproval && (action == ActApprove || action == ActDeny),
		t == inbox.PartChoice && action == ActChoose,
		t == inbox.PartReply && action == ActReply:
		// Answering resumes a run. Who may answer at all is the run's
		// spec's to say, and is checked by the runtime besides.
		return auth.RunsWrite, true
	}
	return "", false
}

var offered = map[inbox.PartType][]string{
	inbox.PartAlert:     {ActAcknowledge},
	inbox.PartSource:    {ActRetry},
	inbox.PartOperation: {ActAcknowledge},
	inbox.PartFailure:   {ActRetry, ActAcknowledge},
	inbox.PartMemory:    {ActKeep, ActDiscard},
	inbox.PartBudget:    {ActRaiseLimit, ActAcknowledge},
	inbox.PartApproval:  {ActApprove, ActDeny},
	inbox.PartChoice:    {ActChoose},
	inbox.PartReply:     {ActReply},
	inbox.PartMention:   {ActReply, ActDone},
	inbox.PartDraft:     {ActPublish, ActDiscard},
}

// allows reports whether who may take an action on a part, and if not, why.
// An action needs what needs says, except a proposal for notes, which is
// decided as the notes' own memory routes decide it: a project's by
// MayChangeNotes (projects:write, ownership, and projects:govern for the
// workspace's and a company project's), and an agent's that lives in a
// project by ownership of its home as well. A retry starts a turn, so it
// takes what starting one there takes: being a member of the project the
// failed turn is filed in. So the inbox is no way around any of them.
// An agent's draft is its project's owners' to decide.
func (p *Platform) allows(tree *projects.Tree, who auth.Principal, part inbox.Part, action string) error {
	perm, known := needs(part.Type, action)
	if !known {
		return fmt.Errorf("%w: a %s part does not take %q", ErrAction, part.Type, action)
	}
	if project, ok := memoryProject(part); ok {
		return p.mayChangeNotes(tree, who, project)
	}
	if part.Type == inbox.PartDraft && part.Draft != nil {
		if !decidesDrafts(tree, who, part.Draft.Project) {
			pr, _ := tree.Get(part.Draft.Project)
			return fmt.Errorf("%w: %s needs projects:write, and %s", ErrForbidden, action, roleNeeded(projects.Owner, titleOf(pr, p.Settings(who.Tenant).WorkspaceName)))
		}
		return nil
	}
	if !who.Can(perm) {
		return fmt.Errorf("%w: %s needs %s", ErrForbidden, action, perm)
	}
	if part.Type == inbox.PartMemory && part.Memory != nil {
		return p.ownsHome(tree, who, part.Memory.Owner)
	}
	if part.Type == inbox.PartFailure && action == ActRetry && part.Failure != nil {
		return p.mayRetryIn(tree, who, part.Failure.Run)
	}
	return nil
}

// mayRetryIn holds a retry of a failed turn to MayFileIn: the person retrying
// must be a member of the project the turn is filed in, whoever asked it
// first. A run the index does not hold is not retried. A project the tree no
// longer holds is Converse's to refuse, saying it was deleted. Whose turn it
// is to retry at all is mayRetry's to say: this only adds the project's role.
func (p *Platform) mayRetryIn(tree *projects.Tree, who auth.Principal, runID string) error {
	run, err := p.Runs.Get(who.Tenant, runID)
	if err != nil {
		return err
	}
	if p.RunPlace(who.Tenant, run).Project != run.Project {
		return fmt.Errorf("%w: historical runs cannot replay after publication; post a new message in this conversation", threads.ErrInvalid)
	}
	project := projects.NameOf(run.Project)
	if project == projects.Root || !tree.Has(project) {
		return nil
	}
	_, _, err = p.projectRole(tree, who, project, projects.Member)
	return err
}

// seenBy fills in what a reader may do with each part, what the people it
// names by subject are called (named), and what the thread it is about is
// called, when they may read that thread.
func (p *Platform) seenBy(tree *projects.Tree, names Names, m inbox.Message, who auth.Principal) inbox.Message {
	m = named(names, m)
	m.Source.ThreadTitle = ""
	id := m.Source.Thread
	if id == "" && m.Source.Run != "" {
		// A message about a run, such as a recap's announcement, is about
		// the conversation the run is in.
		if r, err := p.Runs.Get(who.Tenant, m.Source.Run); err == nil {
			id = r.Thread
		}
	}
	if id != "" {
		if th, err := p.ThreadRecord(who.Tenant, id); err == nil && p.Seer(who).Place(Place{Project: th.Project, Thread: id}) {
			m.Source.ThreadTitle = p.threadTitle(who.Tenant, th)
			m.Source.Project = th.Project
			for i := range m.Parts {
				if m.Parts[i].Mention != nil {
					mention := *m.Parts[i].Mention
					mention.Project, mention.Channel = th.Project, th.Channel
					m.Parts[i].Mention = &mention
				}
			}
		}
	}
	for i := range m.Parts {
		part := &m.Parts[i]
		part.Actions = []string{}
		if part.State != inbox.PartOpen || m.State == inbox.Resolved || m.State == inbox.Dismissed {
			continue
		}
		for _, a := range offered[part.Type] {
			if a == ActRetry && (part.Failure == nil || !part.Failure.Retryable || p.mayRetry(who, part.Failure.Run) != nil) {
				continue
			}
			if p.allows(tree, who, *part, a) == nil {
				part.Actions = append(part.Actions, a)
			}
		}
	}
	return m
}

// threadTitle is what a thread is called: its title, else the start of its
// first message, else of what its first run was asked, as its room heads it.
func (p *Platform) threadTitle(tenant string, th threads.Thread) string {
	if th.Title != "" {
		return th.Title
	}
	if th.Version > 0 {
		if msgs, _, err := p.Threads.Messages(tenant, th.ID, "", 1); err == nil && len(msgs) > 0 {
			return headline(msgs[0].Text, threads.MaxTitle)
		}
	}
	if turns, err := p.Runs.Conversation(tenant, th.ID); err == nil && len(turns) > 0 {
		return headline(turns[0].Text, threads.MaxTitle)
	}
	return ""
}

// named fills in what the people a message names by subject are called: a
// person who sent it, whoever decided each part, and the person whose own
// credentials a question's answer may reach. The subjects stay, as the keys
// they are. A mention keeps no subject: it is from its poster as
// the thread calls them now (Names.mention).
func named(names Names, m inbox.Message) inbox.Message {
	switch {
	case m.Kind == inbox.KindMention:
		m = names.mention(m)
	case m.From.Kind == "person":
		m.From.Display = names.Of(m.From.Name)
	case IsProduct(m.From):
		// The product, whatever name the record keeps: warp, before the rename.
		m.From.Display = SystemDisplay
	}
	for i := range m.Parts {
		if o := m.Parts[i].Outcome; o != nil {
			o.ByName = names.Of(o.By)
		}
		if q := m.Parts[i].Question; q != nil {
			q.SteeredForName = names.Of(q.SteeredFor)
		}
	}
	return m
}

// readerOf is who a person is to the inbox: their subject and their roles,
// which decide what is addressed to them.
func readerOf(who auth.Principal) inbox.Reader {
	roles := make([]string, 0, len(who.Roles))
	for _, r := range who.Roles {
		roles = append(roles, string(r))
	}
	return inbox.Reader{Subject: who.Subject, Roles: roles}
}

// inboxReader is who a person is to their own inbox: readerOf, with the
// projects whose announcements reach them (where they are a member, and every
// ancestor of those, less the projects they muted) and the projects they own,
// read from the tree as it is now. Revoking a membership hides its
// announcements at the next read.
func (p *Platform) inboxReader(tree *projects.Tree, who auth.Principal) inbox.Reader {
	r := readerOf(who)
	r.ProjectOf = p.inboxProject(who.Tenant)
	reader := ReaderOf(who)
	muted := p.Prefs.Get(who.Tenant, who.Subject).Muted
	for _, name := range tree.Memberships(reader) {
		if name == projects.Root || !slices.Contains(muted, name) {
			r.Projects = append(r.Projects, name)
		}
	}
	r.Owns = tree.Owned(reader)
	return r
}

func (p *Platform) inboxQuery(tree *projects.Tree, who auth.Principal, q inbox.Query) inbox.Query {
	r := p.inboxReader(tree, who)
	q.Reader, q.Roles, q.ReaderProjects, q.ReaderOwns, q.Now = r.Subject, r.Roles, r.Projects, r.Owns, p.now()
	q.ProjectOf = r.ProjectOf
	q.Visible = func(k inbox.Kind) bool { return who.Can(reads(k)) }
	q.Sees = p.Seer(who).Message
	return q
}

// InboxSees reports whether a caller may see messages of a kind.
func InboxSees(who auth.Principal, k inbox.Kind) bool { return who.Can(reads(k)) }

// InboxPage returns one page of a person's inbox. q.Projects narrows it to
// what is filed in those projects; it never reaches further than their own
// inbox does.
func (p *Platform) InboxPage(who auth.Principal, q inbox.Query) ([]inbox.Message, string, error) {
	tree := p.Projects.Tree(who.Tenant)
	q.Shared = false
	msgs, next, err := p.Inbox.Page(who.Tenant, p.inboxQuery(tree, who, q))
	names := p.Names(who.Tenant)
	for i := range msgs {
		msgs[i] = p.ownDeliveries(who, p.seenBy(tree, names, msgs[i], who))
	}
	return msgs, next, err
}

// InboxCount returns how many messages are open for a person, how many of
// those they have not read, and how many announcements they have not read.
// Announcements count only in Updates, so they never grow the badge.
func (p *Platform) InboxCount(who auth.Principal) inbox.Counts {
	return p.Inbox.Count(who.Tenant, p.inboxQuery(p.Projects.Tree(who.Tenant), who, inbox.Query{}))
}

// InboxGet returns one message as a person sees it: one in their own inbox,
// or an announcement in the inbox of a project they can see, which a viewer
// reads there, and so does a member who muted the project. Reading it,
// hiding it and snoozing it go through here, so whatever a project's inbox
// shows a reader they may also act on.
func (p *Platform) InboxGet(who auth.Principal, id string) (inbox.Message, error) {
	return p.inboxGetAs(who, p.sharedReader(p.Projects.Tree(who.Tenant), who), id)
}

// sharedReader is inboxReader with every project the person can see, so an
// announcement in any of those projects' inboxes reaches them, member or
// not, muted or not. Only announcements are addressed to a project, so it
// reaches nothing else further. It reads one message; their own inbox and
// its notices keep inboxReader, so they are sent nothing more.
func (p *Platform) sharedReader(tree *projects.Tree, who auth.Principal) inbox.Reader {
	r := p.inboxReader(tree, who)
	for name, a := range tree.Roles(ReaderOf(who)) {
		if a.Role != projects.None && !slices.Contains(r.Projects, name) {
			r.Projects = append(r.Projects, name)
		}
	}
	return r
}

// inboxGetAs returns one message as it is shown to reader, with the actions
// the caller may take. reader is the caller, or the person a surface answers
// for.
func (p *Platform) inboxGetAs(who auth.Principal, reader inbox.Reader, id string) (inbox.Message, error) {
	m, err := p.Inbox.GetAs(who.Tenant, id, reader)
	if err != nil {
		return inbox.Message{}, err
	}
	// What a caller may not see does not exist for them: a kind they may
	// not read, or what lives where they may not read. A surface reading for
	// a person it maps reads where that person may.
	if !who.Can(reads(m.Kind)) || !p.readerSeer(who, reader).Message(m) {
		return inbox.Message{}, fmt.Errorf("%w: message %q", inbox.ErrNotFound, id)
	}
	if m.State == inbox.Snoozed && !p.now().Before(m.SnoozedUntil) {
		m.State, m.Hidden = inbox.Open, false
	}
	return p.ownDeliveries(who, p.seenBy(p.Projects.Tree(who.Tenant), p.Names(who.Tenant), m, who)), nil
}

// readerSeer is what reader may read: the caller, or the person a surface
// reads or answers for, who reads where that person may, never where the
// surface's credential may.
func (p *Platform) readerSeer(who auth.Principal, reader inbox.Reader) *Seer {
	if reader.Subject != "" && reader.Subject != who.Subject {
		return p.Seer(principalOf(who, reader))
	}
	return p.Seer(who)
}

// InboxRead marks a message read or unread for the caller alone.
func (p *Platform) InboxRead(who auth.Principal, id string, read bool) (inbox.Message, error) {
	if _, err := p.InboxGet(who, id); err != nil {
		return inbox.Message{}, err
	}
	if _, err := p.Inbox.Read(who.Tenant, id, who.Subject, read, p.now()); err != nil {
		return inbox.Message{}, err
	}
	return p.InboxGet(who, id)
}

// InboxSnooze hides a message from everyone until a time.
func (p *Platform) InboxSnooze(who auth.Principal, id string, until time.Time) (inbox.Message, error) {
	m, err := p.InboxGet(who, id)
	if err != nil {
		return inbox.Message{}, err
	}
	if !who.Can(writes(m.Kind)) {
		return inbox.Message{}, fmt.Errorf("%w: snoozing this needs %s", ErrForbidden, writes(m.Kind))
	}
	if _, _, err := p.Inbox.Snooze(who.Tenant, id, who.Subject, until, p.now()); err != nil {
		return inbox.Message{}, err
	}
	p.audit(who, m, "", "snooze", until.Format(time.RFC3339))
	return p.InboxGet(who, id)
}

// InboxDismiss sets a message aside. A message about an alert cannot be set
// aside while the alert stays open, so dismissing it acknowledges the alert.
func (p *Platform) InboxDismiss(who auth.Principal, id string) (inbox.Message, error) {
	m, err := p.InboxGet(who, id)
	if err != nil {
		return inbox.Message{}, err
	}
	if m.State == inbox.Resolved || m.State == inbox.Dismissed {
		return m, nil
	}
	if !who.Can(writes(m.Kind)) {
		return inbox.Message{}, fmt.Errorf("%w: dismissing this needs %s", ErrForbidden, writes(m.Kind))
	}
	// A run is waiting on the answer. Setting the question aside would not
	// stop it waiting, so it is answered, or the run is stopped.
	if slices.ContainsFunc(m.Parts, func(x inbox.Part) bool { return x.Question != nil && x.State == inbox.PartOpen }) {
		return inbox.Message{}, fmt.Errorf("%w: a run is waiting on this question; answer it, or stop the run", ErrAction)
	}
	if m.Source.Alert != "" {
		if _, err := p.AckAlert(who.Tenant, m.Source.Alert, who.Subject); err != nil {
			return inbox.Message{}, err
		}
	} else if _, _, err := p.Inbox.Dismiss(who.Tenant, id, who.Subject, p.now()); err != nil {
		return inbox.Message{}, err
	}
	p.audit(who, m, "", "dismiss", "")
	return p.InboxGet(who, id)
}

// InboxUnhide brings back an announcement the caller hid or snoozed for
// themselves, as everyone else has it. Only their own copy changes, so it
// takes what hiding it took. One they never set aside, or one withdrawn for
// everyone, is answered as it is. Any other message is everyone's to set
// aside, and is not brought back this way.
func (p *Platform) InboxUnhide(who auth.Principal, id string) (inbox.Message, error) {
	m, err := p.InboxGet(who, id)
	if err != nil {
		return inbox.Message{}, err
	}
	if !m.Broadcast {
		return inbox.Message{}, fmt.Errorf("%w: only an announcement you hid for yourself is brought back", ErrAction)
	}
	if !who.Can(writes(m.Kind)) {
		return inbox.Message{}, fmt.Errorf("%w: bringing this back needs %s", ErrForbidden, writes(m.Kind))
	}
	if !m.Hidden {
		return m, nil
	}
	if _, _, err := p.Inbox.Unhide(who.Tenant, id, who.Subject, p.now()); err != nil {
		return inbox.Message{}, err
	}
	p.audit(who, m, "", "unhide", "")
	return p.InboxGet(who, id)
}

// InboxResolve carries out what a person decided about one part of a message,
// and records it. Resolving a part that is already resolved does nothing and
// returns the message with the first outcome.
func (p *Platform) InboxResolve(ctx context.Context, who auth.Principal, id, partID string, r Resolution) (inbox.Message, error) {
	tree := p.Projects.Tree(who.Tenant)
	reader := p.inboxReader(tree, who)
	if r.ActAs != nil {
		var err error
		if reader, _, err = p.answerer(who, r.ActAs); err != nil {
			return inbox.Message{}, err
		}
	}
	m, err := p.inboxGetAs(who, reader, id)
	if err != nil {
		return inbox.Message{}, err
	}
	i := slices.IndexFunc(m.Parts, func(x inbox.Part) bool { return x.ID == partID })
	if i < 0 {
		return inbox.Message{}, fmt.Errorf("%w: part %q", inbox.ErrNotFound, partID)
	}
	part := m.Parts[i]
	if part.Question != nil {
		if _, known := needs(part.Type, r.Action); !known {
			return inbox.Message{}, fmt.Errorf("%w: a %s part does not take %q", ErrAction, part.Type, r.Action)
		}
		return p.answer(ctx, who, m, QuestionAnswer{
			Action: r.Action, Arguments: r.Arguments, Choice: r.Choice, Text: r.Text, Note: r.Note, ActAs: r.ActAs,
		})
	}
	// A surface answers a question for the person it maps, and settles a
	// mention of them: reply posts in the thread as them, through the client.
	if r.ActAs != nil && part.Type != inbox.PartMention {
		return inbox.Message{}, fmt.Errorf("%w: only a question or a mention is answered for someone else", ErrAction)
	}
	by := who.Subject
	if r.ActAs != nil {
		if !mayResume(who, reader) {
			return inbox.Message{}, fmt.Errorf("%w: that person cannot settle it", ErrForbidden)
		}
		by = reader.Subject
	}
	// The right is checked before saying whether there is anything left to
	// do, so a caller without it learns nothing from trying.
	if err := p.allows(tree, who, part, r.Action); err != nil {
		return inbox.Message{}, err
	}
	if part.Type == inbox.PartFailure && r.Action == ActRetry && part.Failure != nil {
		if err := p.mayRetry(who, part.Failure.Run); err != nil {
			return inbox.Message{}, err
		}
	}
	if part.Type == inbox.PartDraft {
		if err := p.ownsDraft(who, *part.Draft); err != nil {
			return inbox.Message{}, err
		}
	}
	if part.State != inbox.PartOpen || m.State == inbox.Resolved || m.State == inbox.Dismissed {
		return m, nil
	}

	note := r.Note
	switch {
	case r.Action == ActAcknowledge:
		// Settled below, with everything else the message holds.
	case part.Type == inbox.PartSource:
		if part.Source == nil {
			return inbox.Message{}, knowledge.ErrInvalid
		}
		src, err := p.SyncSource(ctx, who, part.Source.Name)
		if err != nil {
			return inbox.Message{}, err
		}
		if src.Lifecycle == nil || src.Lifecycle.Status == knowledge.NeedsAttention {
			return p.inboxGetAs(who, reader, id)
		}
		note = "Source refreshed"
	case part.Type == inbox.PartFailure:
		run, err := p.retry(ctx, who, part.Failure.Run)
		if err != nil {
			return inbox.Message{}, err
		}
		note = "Started again as " + run.ID
	case part.Type == inbox.PartMemory:
		kept, err := p.decideMemory(who.Tenant, *part.Memory, r)
		if err != nil {
			return inbox.Message{}, err
		}
		note = fmt.Sprintf("Kept %d of %d", kept, len(part.Memory.Notes))
	case part.Type == inbox.PartDraft && r.Action == ActPublish:
		posted, err := p.postFromAgent(who.Tenant, *part.Draft, who.Subject)
		if err != nil {
			return inbox.Message{}, err
		}
		note = "Published as " + posted.ID
	case part.Type == inbox.PartDraft:
		note = "Discarded"
	case part.Type == inbox.PartMention && r.Action == ActReply:
		m, err := p.replyToMention(ctx, who, *part.Mention, r.Text, r.ActAs)
		if err != nil {
			return inbox.Message{}, err
		}
		note = "Replied as " + m.ID
	case part.Type == inbox.PartBudget:
		saved, err := p.raiseLimit(who, *part.Budget, r.Limits)
		if err != nil {
			return inbox.Message{}, err
		}
		note = fmt.Sprintf("Now version %d", saved.Version)
	}

	if _, _, err := p.Inbox.Resolve(who.Tenant, id, partID, inbox.Outcome{Action: r.Action, By: by, At: p.now(), Note: note}); err != nil {
		return inbox.Message{}, err
	}
	p.audit(who, m, partID, r.Action, note)
	// Dealing with any part of a message about an alert deals with the alert.
	if m.Source.Alert != "" {
		if _, err := p.AckAlert(who.Tenant, m.Source.Alert, who.Subject); err != nil {
			return inbox.Message{}, err
		}
	} else {
		p.settle(who.Tenant, id, reader, by)
	}
	return p.inboxGetAs(who, reader, id)
}

// audit writes one line for every decision made in the inbox. The inbox's own
// record holds the same facts; this puts them where an operator reads.
func (p *Platform) audit(who auth.Principal, m inbox.Message, part, action, note string) {
	p.log.Info("platform: inbox", "tenant", who.Tenant, "by", who.Subject, "message", m.ID, "kind", m.Kind,
		"part", part, "action", action, "note", note, "alert", m.Source.Alert, "run", m.Source.Run)
}

// settle closes whatever is still open in a message once it has been dealt
// with, reading it as the reader who dealt with it.
func (p *Platform) settle(tenant, id string, reader inbox.Reader, by string) {
	m, err := p.Inbox.GetAs(tenant, id, reader)
	if err != nil {
		return
	}
	p.settleParts(tenant, m, by)
}

// settleParts closes whatever is still open in a message, whoever it is
// addressed to.
func (p *Platform) settleParts(tenant string, m inbox.Message, by string) {
	id := m.ID
	for _, part := range m.Parts {
		if part.State == inbox.PartOpen {
			_, _, _ = p.Inbox.Resolve(tenant, id, part.ID, inbox.Outcome{Action: ActAcknowledge, By: by, At: p.now()})
		}
	}
}

// AckAlert acknowledges an alert and settles the message about it. It is the
// only way an alert is acknowledged, from the inbox or from anywhere else.
func (p *Platform) AckAlert(tenant, id, by string) (alerts.Alert, error) {
	a, err := p.Alerts.Ack(tenant, id, by, p.now())
	if err != nil {
		return alerts.Alert{}, err
	}
	if m, ok := p.Inbox.ByKey(tenant, alertKey(id)); ok {
		// The first to acknowledge is who the alert says, whoever asked now.
		// They may not be among the people the message is addressed to, so
		// it is settled as it stands, not as they would see it.
		p.settleParts(tenant, m, a.AckedBy)
	}
	return a, nil
}

// inboxAlert raises the message about an alert. extra parts follow the alert's.
func (p *Platform) inboxAlert(tenant string, a alerts.Alert, extra ...inbox.Part) {
	kind, from := inbox.KindAlert, inbox.From{Kind: "rule", Name: a.Rule}
	if a.Kind == alerts.Budget {
		kind, from = inbox.KindBudget, inbox.From{Kind: "system", Name: "Limits"}
	}
	m := inbox.Message{
		Kind: kind, Subject: a.Title, From: from, Severity: severityOf(a.Severity),
		// A rule that names whom to notify addresses its message to them.
		// Who may read alerts at all is still checked for every reader.
		Audience: slices.Clone(a.Notify),
		Source:   inbox.Source{Alert: a.ID, Rule: a.Rule, Run: a.RunID, Agent: a.Agent, Project: a.Project},
		Parts: []inbox.Part{{Type: inbox.PartAlert, Alert: &inbox.AlertPart{
			ID: a.ID, Rule: a.Rule, Kind: string(a.Kind), Severity: string(a.Severity), Title: a.Title, Body: a.Body,
		}}},
	}
	if a.RunID != "" {
		if run, err := p.Runs.Get(tenant, a.RunID); err == nil {
			m.Source.Thread, m.Source.Project = run.Thread, run.Project
			if run.Status == runs.Failed {
				m.Parts = append(m.Parts, failurePart(run))
			}
		}
	}
	m.Parts = append(m.Parts, extra...)
	p.raiseInbox(tenant, alertKey(a.ID), m, a.At)
}

func retryable(run runs.Run) bool { return run.Origin == runs.OriginAPI && run.Parent == "" }

func failurePart(run runs.Run) inbox.Part {
	return inbox.Part{Type: inbox.PartFailure, Failure: &inbox.FailurePart{
		Run: run.ID, Agent: run.Agent, Error: run.Error, Retryable: retryable(run),
	}}
}

// inboxFailure raises the message about a failed run that no alert rule spoke
// for. A run a rule did catch is already in the inbox as that alert.
func (p *Platform) inboxFailure(run runs.Run) {
	if run.Status != runs.Failed || run.Origin == runs.OriginSystem || run.Origin == OriginEval {
		return
	}
	p.raiseInbox(run.Tenant, runKey(run.ID), inbox.Message{
		Kind: inbox.KindFailure, Subject: run.Agent + " failed", From: inbox.From{Kind: "agent", Name: run.Agent},
		Severity: inbox.Warning,
		Source:   inbox.Source{Run: run.ID, Thread: run.Thread, Agent: run.Agent, Project: run.Project},
		Parts:    []inbox.Part{failurePart(run)},
	}, run.UpdatedAt)
}

// inboxMemory raises the message asking which proposed memories to keep.
func (p *Platform) inboxMemory(run runs.Run, owner string, notes []memory.Note) {
	if len(notes) == 0 {
		return
	}
	part := inbox.MemoryPart{Owner: owner}
	about := owner
	var audience []string
	if project, ok := memory.OwnerProject(owner); ok {
		part.Project, about = project, "the project "+project
		if project == "" {
			about = "the workspace"
		}
		// A project's notes are its owners' to decide.
		audience = []string{inbox.OwnersAudience(project)}
	} else {
		part.Agent = owner
	}
	for _, n := range notes {
		part.Notes = append(part.Notes, inbox.NoteRef{ID: n.ID, Text: n.Text})
	}
	noun := "memories"
	if len(notes) == 1 {
		noun = "memory"
	}
	p.raiseInbox(run.Tenant, "memory:"+run.ID, inbox.Message{
		Kind: inbox.KindMemory, Subject: fmt.Sprintf("%d proposed %s for %s", len(notes), noun, about),
		From: inbox.From{Kind: "agent", Name: run.Agent}, Severity: inbox.Info, Audience: audience,
		Source: inbox.Source{Run: run.ID, Agent: part.Agent, Project: part.Project},
		Parts:  []inbox.Part{{Type: inbox.PartMemory, Memory: &part}},
	}, p.now())
}

// inboxArtifact says in the inbox that a file was published. A new version
// replaces the message about the one before it, so a file revised ten times
// is one message about its latest version, not ten.
func (p *Platform) inboxArtifact(tenant string, a artifacts.Artifact) {
	part := inbox.ArtifactPart{
		ID: a.ID, Title: a.Title, Kind: string(a.Kind), Mime: a.Mime, Size: a.Size, Version: a.Version, Description: a.Description,
	}
	from := inbox.From{Kind: "system", Name: "Artifacts"}
	who := "A file"
	if a.Agent != "" {
		from, who = inbox.From{Kind: "agent", Name: a.Agent}, a.Agent
	}
	subject := fmt.Sprintf("%s published %s", who, a.Title)
	if a.Agent == "" {
		subject = fmt.Sprintf("%s was published", a.Title)
	}
	if a.Version > 1 {
		subject = fmt.Sprintf("%s updated %s to version %d", who, a.Title, a.Version)
		if a.Agent == "" {
			subject = fmt.Sprintf("%s is at version %d", a.Title, a.Version)
		}
		if versions, err := p.Artifacts.Versions(tenant, a.ID); err == nil {
			for _, v := range versions {
				if v.ID == a.ID {
					continue
				}
				if part.Previous == "" {
					part.Previous = v.ID
				}
				if m, ok := p.Inbox.ByKey(tenant, artifactKey(v.ID)); ok && m.State != inbox.Resolved && m.State != inbox.Dismissed {
					_, _, _ = p.Inbox.Withdraw(tenant, m.ID, "system", fmt.Sprintf("Replaced by version %d", a.Version), p.now())
				}
			}
		}
	}
	p.raiseInbox(tenant, artifactKey(a.ID), inbox.Message{
		Kind: inbox.KindArtifact, Subject: subject, From: from, Severity: inbox.Info,
		Source: inbox.Source{Run: a.Run, Thread: a.Thread, Agent: a.Agent, Project: a.Project},
		Parts:  []inbox.Part{{Type: inbox.PartArtifact, State: inbox.PartInfo, Artifact: &part}},
	}, a.CreatedAt)
}

// raiseInbox raises a message for the platform itself, which has nobody to
// hand an error to: one that could not be kept is logged and not raised.
func (p *Platform) raiseInbox(tenant, key string, m inbox.Message, at time.Time) (inbox.Message, bool) {
	out, raised, err := p.Inbox.Raise(tenant, key, m, at)
	if err != nil {
		p.log.Error("platform: inbox message could not be kept", "tenant", tenant, "key", key, "error", err)
		return inbox.Message{}, false
	}
	return out, raised
}

// inboxArtifactGone withdraws the message about a version that was deleted.
func (p *Platform) inboxArtifactGone(tenant string, a artifacts.Artifact) {
	if m, ok := p.Inbox.ByKey(tenant, artifactKey(a.ID)); ok && m.State != inbox.Resolved && m.State != inbox.Dismissed {
		_, _, _ = p.Inbox.Withdraw(tenant, m.ID, "system", "The file was deleted", p.now())
	}
}

// A retry asks the same turn again for the same person.
//
// A failed turn that acted for a person reached their tools with their own
// credential, and its retry does too, so it is theirs alone to press, signed
// in as themselves. Anyone else pressing it would either run that person's
// words with their own credential, or run them with that person's
// credential on a choice the person never made, so they are refused, whatever
// else they may do. A turn that acted for nobody is retried for nobody, by
// anyone who may start runs: the words were nobody's, and no one's
// credential is put behind them.
//
// The retry's id names whoever pressed it, so two people's presses never
// join one run, and one person pressing twice joins their first.

// mayRetry reports whether who may ask a failed turn again, and if not, why.
func (p *Platform) mayRetry(who auth.Principal, runID string) error {
	run, err := p.Runs.Get(who.Tenant, runID)
	if err != nil {
		return err
	}
	if person := personOf(run.Actor); person != "" && (who.Kind == auth.Machine || who.Subject != person) {
		name := p.NameOf(who.Tenant, person)
		return fmt.Errorf("%w: this turn acted for %s, so only %s may ask it again", ErrForbidden, name, name)
	}
	return nil
}

// retry asks a failed turn again, on the same branch it was first asked on,
// for whoever the turn acted for (see mayRetry).
func (p *Platform) retry(ctx context.Context, who auth.Principal, runID string) (runs.Run, error) {
	run, err := p.Runs.Get(who.Tenant, runID)
	if err != nil {
		return runs.Run{}, err
	}
	if !retryable(run) {
		return runs.Run{}, fmt.Errorf("%w: only a turn a person asked can be started again", ErrAction)
	}
	if err := p.mayRetry(who, run.ID); err != nil {
		return runs.Run{}, err
	}
	// Whoever pressed it asked for it, so they do not approve its calls.
	// It acts for the person the turn acted for, who is them, or for
	// nobody.
	actor := ActorOf(who)
	if personOf(run.Actor) == "" {
		actor.User = ""
	}
	t := Turn{
		ID:    "api-retry-" + strconv.Itoa(len(who.Subject)) + "-" + who.Subject + "-" + run.ID,
		Agent: run.Agent, Text: run.Text, After: run.Prev, Actor: actor,
		// An agent moved since into a project they may not read is one that
		// is not there.
		Sees: p.Seer(who).Agent,
	}
	// The retry is the person retrying's turn, so what it teaches the
	// project is kept unasked only if they could keep it themselves.
	t.KeepsNotes = run.Project != "" && p.MayChangeNotes(who, run.Project) == nil
	if run.Message != "" {
		return p.retryAnswer(ctx, who.Tenant, run, t)
	}
	if run.Prev == "" {
		t.Project = run.Project
	}
	return p.Converse(ctx, who.Tenant, t)
}

// retryAnswer asks a failed run that answered a person's message in a
// shared thread to answer that message again: the same agent, sent the room
// up to the message, filed under it beside the failed run. The message is
// the poster's, and the retry acts as t says, for the person the failed run
// acted for, who posted it.
func (p *Platform) retryAnswer(ctx context.Context, tenant string, run runs.Run, t Turn) (runs.Run, error) {
	th, err := p.ThreadRecord(tenant, run.Thread)
	if err != nil {
		return runs.Run{}, err
	}
	m, err := p.Threads.Message(tenant, th.ID, run.Message)
	if err != nil {
		return runs.Run{}, err
	}
	if t.Agent != Auto && !t.Sees(t.Agent) {
		return runs.Run{}, fmt.Errorf("%w: %q", registry.ErrNotFound, t.Agent)
	}
	return p.answerMessage(ctx, tenant, th, m, "", t.Agent, nil, run.Routed, t.Actor, t.ID, t.KeepsNotes, t.Sees)
}

// decideMemory keeps the chosen proposals and discards the rest.
func (p *Platform) decideMemory(tenant string, part inbox.MemoryPart, r Resolution) (int, error) {
	keep := map[string]bool{}
	if r.Action == ActKeep {
		for _, id := range r.IDs {
			if !slices.ContainsFunc(part.Notes, func(n inbox.NoteRef) bool { return n.ID == id }) {
				return 0, fmt.Errorf("%w: %q is not one of this message's proposals", ErrAction, id)
			}
			keep[id] = true
		}
		if len(keep) == 0 {
			return 0, fmt.Errorf("%w: choose at least one to keep, or discard them", ErrAction)
		}
	}
	kept, err := p.Memory.Approve(tenant, part.Owner, r.IDs)
	if r.Action != ActKeep {
		kept, err = 0, nil
	}
	if err != nil {
		return 0, err
	}
	for _, n := range part.Notes {
		if !keep[n.ID] {
			// One that is already gone was dealt with somewhere else.
			_ = p.Memory.Remove(tenant, part.Owner, n.ID)
		}
	}
	return kept, nil
}

// raiseLimit saves new numbers for the policy a budget part is about.
func (p *Platform) raiseLimit(who auth.Principal, part inbox.BudgetPart, to inbox.Limits) (policy.Policy, error) {
	scope := policy.Scope{Kind: policy.Kind(part.ScopeKind), Name: part.ScopeName}
	all := p.Policies.List(who.Tenant)
	i := slices.IndexFunc(all, func(x policy.Policy) bool { return x.Scope == scope })
	if i < 0 {
		return policy.Policy{}, fmt.Errorf("%w: %s has no limits any more", ErrAction, scope)
	}
	pol := all[i]
	if to == (inbox.Limits{}) {
		return policy.Policy{}, fmt.Errorf("%w: say what the limit should be", ErrAction)
	}
	if to.MaxRuns > 0 {
		pol.MaxRuns = to.MaxRuns
	}
	if to.MaxConcurrent > 0 {
		pol.MaxConcurrent = to.MaxConcurrent
	}
	if to.SpendLimit > 0 {
		pol.SpendLimit = to.SpendLimit
	}
	if to.MaxTokens > 0 {
		pol.MaxTokens = to.MaxTokens
	}
	return p.Policies.Put(who.Tenant, pol, who.Subject, p.now())
}

// ApproveMemory keeps proposals, from anywhere but the inbox, and brings the
// inbox into step.
func (p *Platform) ApproveMemory(tenant, owner string, ids []string) (int, error) {
	n, err := p.Memory.Approve(tenant, owner, ids)
	p.syncMemory(tenant, owner)
	return n, err
}

// RemoveMemory deletes a note or a proposal, and brings the inbox into step.
func (p *Platform) RemoveMemory(tenant, owner, id string) error {
	err := p.Memory.Remove(tenant, owner, id)
	p.syncMemory(tenant, owner)
	return err
}

// syncMemory closes messages whose proposals have all been dealt with elsewhere.
func (p *Platform) syncMemory(tenant, owner string) {
	var pending []string
	for _, n := range p.Memory.List(tenant, owner) {
		if n.Status == memory.Pending {
			pending = append(pending, n.ID)
		}
	}
	for _, m := range p.Inbox.OpenOf(tenant, inbox.KindMemory) {
		for _, part := range m.Parts {
			if part.Memory == nil || part.Memory.Owner != owner || part.State != inbox.PartOpen {
				continue
			}
			if !slices.ContainsFunc(part.Memory.Notes, func(n inbox.NoteRef) bool { return slices.Contains(pending, n.ID) }) {
				_, _, _ = p.Inbox.Withdraw(tenant, m.ID, "system", "Dealt with on the "+strings.TrimPrefix(owner, "project:")+" memory page", p.now())
			}
		}
	}
}
