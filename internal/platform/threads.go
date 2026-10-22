package platform

import (
	"cmp"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"hash/fnv"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/urmzd/fiber/pkg/envelope"

	"github.com/urmzd/warp/pkg/auth"
	"github.com/urmzd/warp/pkg/inbox"
	"github.com/urmzd/warp/pkg/pick"
	"github.com/urmzd/warp/pkg/projects"
	"github.com/urmzd/warp/pkg/runs"
	"github.com/urmzd/warp/pkg/threads"
)

// Shared threads (docs/projects.md, "Channels and threads"): the
// conversations people and agents have together in a project's channel.
//
// A person posts a message; it starts no run of its own. What it starts is
// decided by the one grammar (threads.Parse) and the one rule order
// (threads.Decide): the agents it tags answer, side by side; a message that
// tags only people notifies them; an untagged answer, reply or message
// posted only starts nothing; otherwise the default the thread inherits
// answers.
// Every run passes through Invoke, so admission, limits and availability
// decide as they do for every other run.
//
// A person posts only as themselves: the caller, or the person a surface's
// credential acts for through its client (act_as). No route names an
// author, and nothing here writes a turn for an agent: an agent's turn is a
// run.

// ErrThreadAuthor reports a post with no person to post it: a credential
// that is not acting for one.
var ErrThreadAuthor = errors.New("platform: a message is posted by a person")

// ErrAnswered reports a message posted as the answer to a question that no
// longer waits for one: it was answered, or its run stopped or ended. Nothing
// is posted, since the message would answer nothing.
var ErrAnswered = errors.New("platform: no longer waiting for an answer")

// ThreadInput starts a thread with its first message.
type ThreadInput struct {
	// Project is where it is filed: "" or "~" is the root.
	Project string `json:"project"`
	// Channel is the project's channel. "" is general.
	Channel string `json:"channel"`
	// Visibility is shared, read by everyone who sees the project, or
	// private, read by its participants.
	Visibility threads.Visibility `json:"visibility"`
	Title      string             `json:"title,omitempty"`
	// Default answers untagged messages in the thread: an agent, or auto.
	Default string `json:"default,omitempty"`
	// Agents are selected to answer the thread's untagged messages, and Mode
	// how (threads.Routing).
	Agents   []string `json:"agents,omitempty"`
	Mode     string   `json:"mode,omitempty"`
	Text     string   `json:"text"`
	PostOnly bool     `json:"post_only,omitempty"`
	// ActAs is the person a surface's credential posts for.
	ActAs *ActAs `json:"act_as,omitempty"`
	// Surface is where on a client's surface the message was posted, sent
	// only by the client's machine. The thread is that surface's, found or
	// started, and filed from the binding: Project, Channel, Visibility and
	// Default are then left out.
	Surface *SurfaceRef `json:"surface,omitempty"`
}

// MessageInput posts a message into a thread.
type MessageInput struct {
	Text string `json:"text"`
	// After is the turn it follows: a message of the thread, a run answering
	// one, or a run of a conversation from before threads. "" follows the
	// latest.
	After    string `json:"after,omitempty"`
	PostOnly bool   `json:"post_only,omitempty"`
	// Answers records the message as a question's answer, which resumes the
	// run parked on it.
	Answers *threads.AnswerRef `json:"answers,omitempty"`
	ActAs   *ActAs             `json:"act_as,omitempty"`
	// Surface is where on the thread's surface the message was posted, sent
	// only by the client's machine. Its message ts makes a second post of it
	// change nothing.
	Surface *SurfaceRef `json:"surface,omitempty"`
	// reply is a reply to a mention, from the inbox.
	reply bool
}

// ThreadUpdate changes a thread's title, visibility, default and routing,
// as a whole.
type ThreadUpdate struct {
	// Version is the one read. A conversation with no record reads 0.
	Version    int                `json:"version"`
	Title      string             `json:"title"`
	Visibility threads.Visibility `json:"visibility"`
	Default    string             `json:"default"`
	// Agents and Mode are the thread's selected agents and how they answer
	// (threads.Routing). A selected agent the caller is not shown is kept.
	Agents []string `json:"agents,omitempty"`
	Mode   string   `json:"mode,omitempty"`
	// Project and Channel move a private conversation while no run has been
	// filed in it: "" keeps the thread's own project, and its own channel
	// there, general in another.
	Project string `json:"project,omitempty"`
	Channel string `json:"channel,omitempty"`
}

// ParticipantsUpdate adds and removes people and agents.
type ParticipantsUpdate struct {
	Version int `json:"version"`
	// Add names people by handle and agents by name, as participants.
	Add []threads.Participant `json:"add,omitempty"`
	// Remove names them by handle or name, which no person and agent
	// share.
	Remove []string `json:"remove,omitempty"`
}

// Posted is what a message did.
type Posted struct {
	Thread  threads.Thread  `json:"thread"`
	Message threads.Message `json:"message"`
	// Runs are the runs it started, at most threads.MaxRuns.
	Runs []runs.Run `json:"runs"`
	// Notified is who it was sent to: people and experts by subject, the
	// experts a role names as the role. ShowPosted names them by handle.
	Notified []string `json:"notified"`
	// Refused are its tags that started or notified nothing, each saying
	// why.
	Refused []threads.Mention `json:"refused"`
	// Rule is the rule that decided what it starts, and Reason why no agent
	// answers when none does.
	Rule   threads.Rule `json:"rule"`
	Reason string       `json:"reason,omitempty"`
	// Repeated is set when the message was posted before, by its surface's
	// id: this is what it did then, and nothing was started again.
	Repeated bool `json:"repeated,omitempty"`
}

// ThreadTurn is one turn of a thread: a person's message, an agent's run,
// or the announcement the thread was started from, which is its first.
type ThreadTurn struct {
	// Kind is message, run or announcement.
	Kind         string                `json:"kind"`
	Message      *threads.Message      `json:"message,omitempty"`
	Run          *runs.Run             `json:"run,omitempty"`
	Announcement *threads.Announcement `json:"announcement,omitempty"`
	// Ask is the question a parked run waits on, as the reader stands to
	// it (AsksIn).
	Ask *Ask `json:"ask,omitempty"`
}

// Turn kinds.
const (
	TurnMessage      = "message"
	TurnRun          = "run"
	TurnAnnouncement = "announcement"
)

// ThreadSummary is one conversation at a glance: what its runs say, with
// what its record adds.
type ThreadSummary struct {
	runs.ThreadSummary
	Visibility threads.Visibility `json:"visibility"`
	// Messages is how many messages people have posted in it.
	Messages int `json:"messages"`
}

// author is who a message is posted as: the caller, or the person a
// surface's credential acts for. A credential that is not a person posts
// only for one (ErrThreadAuthor), and the person it acts for must hold
// runs:write themselves. slack is that client's map of Slack people, for
// Slack's own mentions.
func (p *Platform) author(who auth.Principal, as *ActAs) (subject, name, via string, slack map[string]string, err error) {
	if as == nil {
		if who.Kind == auth.Machine {
			return "", "", "", nil, fmt.Errorf("%w: a credential posts only for a person it acts for (act_as)", ErrThreadAuthor)
		}
		return who.Subject, p.nameOf(who.Tenant, who.Subject), threads.ViaConsole, nil, nil
	}
	reader, _, err := p.answerer(who, as)
	if err != nil {
		return "", "", "", nil, err
	}
	if !mayResume(who, reader) {
		return "", "", "", nil, fmt.Errorf("%w: that person cannot post in threads", ErrForbidden)
	}
	c, err := p.Clients.Get(who.Tenant, as.Client)
	if err != nil {
		return "", "", "", nil, err
	}
	if c.Slack != nil {
		slack = c.Slack.People
	}
	return reader.Subject, p.nameOf(who.Tenant, reader.Subject), c.ID, slack, nil
}

// takesPart reports whether a person takes part in a thread.
func takesPart(th threads.Thread, subject string) bool {
	return subject != "" && slices.ContainsFunc(th.Participants, func(pt threads.Participant) bool {
		return pt.Kind == threads.Person && pt.Name == subject
	})
}

// MayPost holds the person a message or a turn is posted by to what posting
// in a place takes: being a member of its project (MayFileIn), and in a
// private thread taking part in it. A project they cannot see is
// projects.ErrNotFound, and so is a private thread they cannot read; one they
// read with projects:govern without taking part is ErrForbidden, as reading a
// thread is not posting in it.
func (p *Platform) MayPost(person auth.Principal, at Place) error {
	_, err := p.mayPost(person, at, projects.Member)
	return err
}

// mayPost is MayPost with the role the project takes. Starting a thread, or
// posting a message that may start a run, takes a member, as starting a
// conversation does. A message that only answers what its poster was asked, a
// question a run there put to them or a message that tagged them, and tags
// no one, starts nothing and notifies no one, and takes only seeing the
// project (postNeed): an expert a project names need not be a member to
// answer, and the answer's own check holds it to whom it was asked of. It
// also says whether what the runs it starts teach the project may be kept
// unasked: only when the poster could keep it themselves.
func (p *Platform) mayPost(person auth.Principal, at Place, need projects.Role) (keeps bool, err error) {
	project := projects.NameOf(at.Project)
	switch {
	case project == projects.Root && need == projects.Member:
	case !p.HasProject(person.Tenant, project):
		// A deleted project's conversations are read, not continued: one who
		// reads them may try, and admission refuses the turn as deleted.
		if !p.Seer(person).Project(project) {
			return false, fmt.Errorf("%w: %q", projects.ErrNotFound, project)
		}
	default:
		if _, _, err := p.ProjectRole(person, project, need); err != nil {
			return false, err
		}
	}
	keeps = project != projects.Root && p.HasProject(person.Tenant, project) && p.MayChangeNotes(person, project) == nil
	if at.Thread == "" {
		return keeps, nil
	}
	th, err := p.Threads.Get(person.Tenant, at.Thread)
	if err != nil || th.Visibility != threads.Private || takesPart(th, person.Subject) {
		return keeps, nil
	}
	if p.Seer(person).Place(at) {
		return false, fmt.Errorf("%w: only its participants post in a private thread", ErrForbidden)
	}
	return false, fmt.Errorf("%w: thread %q", threads.ErrNotFound, at.Thread)
}

// changesThread holds a caller to changing a thread's record at all: one they
// read (else threads.ErrNotFound, as a thread that is not there), and not a
// machine without projects:govern, which posts only for a person.
func (p *Platform) changesThread(who auth.Principal, th threads.Thread) error {
	if !p.Seer(who).Place(Place{Project: th.Project, Thread: th.ID}) {
		return fmt.Errorf("%w: thread %q", threads.ErrNotFound, th.ID)
	}
	if who.Kind == auth.Machine && !who.Can(auth.ProjectsGovern) {
		return fmt.Errorf("%w: a credential changes a thread only with projects:govern", ErrForbidden)
	}
	return nil
}

// MayFollow holds the person a turn is posted by to what adding it after a
// run takes: posting where the run lives (MayPost).
func (p *Platform) MayFollow(person auth.Principal, prev runs.Run) error {
	return p.MayPost(person, placeOfRun(prev))
}

// mayChangeThread holds a caller to what changing a thread's visibility,
// title or default, or removing someone else from it, takes: the thread's
// owner, the person who started it, or an owner of its project
// (projects:govern owns every project). Its owner changes it only while they
// are a member of the project, as they were to start it: its default answers
// everyone's untagged messages there, and a viewer starts nothing. A
// conversation from before threads that no person asked in has no owner yet:
// whoever changes it first becomes one, and that takes a member of the
// project.
func (p *Platform) mayChangeThread(who auth.Principal, th threads.Thread) error {
	if err := p.changesThread(who, th); err != nil {
		return err
	}
	switch {
	case th.CreatedBy == "":
		return p.MayFileIn(who, th.Project)
	case who.Subject != "" && (who.Subject == th.CreatedBy || slices.ContainsFunc(th.Participants, func(pt threads.Participant) bool {
		return pt.Owner && pt.Kind == threads.Person && pt.Name == who.Subject
	})):
		return p.MayFileIn(who, th.Project)
	}
	_, _, err := p.ProjectRole(who, th.Project, projects.Owner)
	if errors.Is(err, projects.ErrForbidden) {
		return fmt.Errorf("%w: only whoever started this thread, or an owner of its project, changes it", projects.ErrForbidden)
	}
	return err
}

// nameOf is what a person is called when they post: their name in the
// directory, which is never an address, else their handle, else their
// subject when it holds no address (see shower.name).
func (p *Platform) nameOf(tenant, subject string) string {
	return p.shower(tenant).name(subject)
}

// poster is the person a message is posted as, as a principal whose own
// role on a project is the one that counts: the caller, or the person a
// surface's credential acts for, with the roles the workspace's policy grants
// them, never the credential's.
func (p *Platform) poster(who auth.Principal, as *ActAs) (auth.Principal, error) {
	if as == nil {
		return who, nil
	}
	return p.Acting(who, *as)
}

// postNeed is the role a message in a thread needs of its poster. An answer
// or a reply is the poster's response to what they were asked, which the
// default does not answer, so a viewer may post one. But the agents it tags
// still answer it and the people and experts it tags are still told, as for
// any message (threads.Decide), so one that tags anyone takes a member, as
// tagging does. Whether it tags is read from the text alone, before any
// name is looked up: a tag that names nobody counts too.
func postNeed(in MessageInput) projects.Role {
	if in.Answers == nil && !in.reply {
		return projects.Member
	}
	if tags, err := threads.Parse(in.Text, nil); err != nil || len(tags) > 0 {
		return projects.Member
	}
	return projects.Viewer
}

// tagLookup resolves the names a message tags, within one tenant, as its
// poster reads them: an agent they may not read tags no agent, as one that
// does not exist.
type tagLookup struct {
	ctx    context.Context
	p      *Platform
	tenant string
	slack  map[string]string
	sees   func(agent string) bool
	// tree and project are where the message is posted, for the skills its
	// chain offers.
	tree    *projects.Tree
	project string
}

// Skill reports whether a skill goes by name on the chain of the project
// the message is posted in.
func (l tagLookup) Skill(name string) bool {
	if l.tree == nil {
		return false
	}
	_, _, ok := l.tree.Skill(l.project, name)
	return ok
}

// MentionKind is what a mention kind the workspace declares resolves to.
func (l tagLookup) MentionKind(kind string) (string, bool) { return l.p.mentionKind(l.tenant, kind) }

func (l tagLookup) Agent(name string) bool {
	_, err := l.p.Agents.Get(l.ctx, l.tenant, name, 0)
	return err == nil && (l.sees == nil || l.sees(name))
}

func (l tagLookup) Person(handle string) (string, bool) {
	if l.p.People == nil {
		return "", false
	}
	who, err := l.p.People.ByHandle(l.tenant, handle)
	return who.Subject, err == nil
}

func (l tagLookup) Slack(user string) (string, bool) {
	who, ok := l.slack[user]
	return who, ok
}

// tagRefusal says why an agent cannot answer in a project, or "" when it
// can. It is the check admission makes (Available), made sooner so the
// message says why.
func (p *Platform) tagRefusal(tenant, project, agent string) string {
	if agent == Auto {
		return ""
	}
	if err := p.Available(tenant, project, agent); err != nil {
		return refusalOf(err)
	}
	return ""
}

// checkTags refuses the tags that cannot be carried out where a message is
// posted: an agent not available in the thread's project, and experts of a
// topic nobody on the chain answers for.
func (p *Platform) checkTags(tenant string, tree *projects.Tree, project string, ms []threads.Mention) []threads.Mention {
	out := slices.Clone(ms)
	for i := range out {
		m := &out[i]
		if m.Refused != "" {
			continue
		}
		switch m.Kind {
		case threads.Agent:
			m.Refused = p.tagRefusal(tenant, project, m.Name)
		case threads.SME:
			if who, _ := tree.ExpertsFor(project, m.Name); len(who) == 0 && m.Name == "" {
				m.Refused = "nobody here is listed as an expert"
			} else if len(who) == 0 {
				m.Refused = fmt.Sprintf("nobody here is listed as an expert on %s", m.Name)
			}
		}
	}
	return out
}

// placeOfThread is where a message in a thread is posted, for deciding who
// answers it: the thread's project, with the thread's and its channel's
// defaults nearer than the project's.
func (p *Platform) placeOfThread(tenant string, th threads.Thread) Place {
	return p.placeIn(tenant, th, "")
}

// placeIn is placeOfThread for a message a client sends, with the client's
// default between the thread's and the channel's.
func (p *Platform) placeIn(tenant string, th threads.Thread, clientDefault string) Place {
	ch, _ := p.Projects.Channel(tenant, th.Project, cmp.Or(th.Channel, projects.General))
	at := Place{Project: th.Project}
	for _, d := range threads.Nearer(th, cmp.Or(clientDefault, p.clientDefault(tenant, th)), ch) {
		at.Nearer = append(at.Nearer, Nearer{Agent: d.Agent, From: d.From})
	}
	return at
}

// clientDefault is the default of the client whose surface a thread is on,
// for a thread it filed outside a bound channel: a direct message, the
// assistant pane, an unbound channel. In a bound channel the channel's
// chain decides.
func (p *Platform) clientDefault(tenant string, th threads.Thread) string {
	if th.Surface == nil {
		return ""
	}
	if ch, bound := p.Projects.BoundTo(tenant, th.Surface.Client, th.Surface.Channel); bound && ch.Project == th.Project && ch.Name == th.Channel {
		return ""
	}
	c, err := p.Clients.Get(tenant, th.Surface.Client)
	if err != nil {
		return ""
	}
	return c.DefaultAgent
}

// defaultOf is the default a message in a thread inherits, and why it
// cannot answer when it has stopped being available there.
//
// sees, when set, is the agents the person it is for may read: a default
// nearer than the project naming one they may not read is passed over for
// them, as if unset (placeSeen).
func (p *Platform) defaultOf(tenant string, th threads.Thread, sees func(string) bool) (threads.Default, string) {
	n := p.DefaultAt(tenant, placeSeen(p.placeOfThread(tenant, th), sees))
	d := threads.Default{Agent: n.Agent, From: n.From}
	if d.Agent == "" || d.Agent == Auto {
		return d, ""
	}
	if p.tagRefusal(tenant, th.Project, d.Agent) != "" {
		return d, "it is not available here"
	}
	return d, ""
}

// DefaultFor is who answers an untagged message in a thread, or in a
// channel before any thread is started there (id ""), and where that
// default is set: what the console says under its composer. Unavailable
// says why it cannot answer, when it has stopped being available.
func (p *Platform) DefaultFor(tenant, id, project, channel string) (d threads.Default, unavailable string, err error) {
	th := threads.Thread{Project: projects.NameOf(project), Channel: cmp.Or(channel, projects.General)}
	if id != "" {
		if th, err = p.ThreadRecord(tenant, id); err != nil {
			return threads.Default{}, "", err
		}
	}
	d, unavailable = p.defaultOf(tenant, th, nil)
	return d, unavailable, nil
}

// placeSeen is a place as a person who may read only the agents sees reads
// it: each default nearer than the project (the thread's, the client's, the
// channel's) that names an agent they may not read is passed over, as if
// unset, so who answers them is the next default along. Such an agent is
// never available where they read, so it could not answer them; and a
// default they are shown in its place reads exactly as it would were the
// hidden one not set. A project's default is never passed over: it is
// available where it is set, so it lives on that project's chain, which
// whoever reads the place reads. A nil sees passes over nothing.
func placeSeen(at Place, sees func(string) bool) Place {
	if sees == nil {
		return at
	}
	out := at
	out.Nearer = slices.Clone(at.Nearer)
	for i, n := range out.Nearer {
		if n.Agent != "" && n.Agent != Auto && !sees(n.Agent) {
			out.Nearer[i].Agent = ""
		}
	}
	return out
}

// ThreadRecord returns a thread's record. A conversation from before
// threads has none, and reads as shared, in general of its project, with
// the people who asked in it and the agents who answered as its
// participants, at version 0. The first person who asked in it started it.
// A person is whom a run acted for (runs.AskedBy), so a connector's run is
// the person its user is mapped to, never that user, and a credential that
// is no person (a machine's call to POST /api/runs, or a surface's run for
// someone it maps to nobody) is never a participant, and never the owner.
// Its record is written the first time anything about it changes.
func (p *Platform) ThreadRecord(tenant, id string) (threads.Thread, error) {
	th, err := p.Threads.Get(tenant, id)
	if err == nil || !errors.Is(err, threads.ErrNotFound) {
		return th, err
	}
	turns, rerr := p.Runs.Conversation(tenant, id)
	if rerr != nil || !threads.ValidID(id) {
		return threads.Thread{}, fmt.Errorf("%w: thread %q", threads.ErrNotFound, id)
	}
	first := turns[0]
	th = threads.Thread{
		ID: id, Project: first.Project, Channel: cmp.Or(first.Channel, projects.General),
		Visibility: threads.Shared, Participants: []threads.Participant{},
		CreatedAt: first.CreatedAt, UpdatedAt: first.UpdatedAt,
	}
	for _, r := range turns {
		if who := runs.AskedBy(r); who != "" {
			th.CreatedBy = who
			break
		}
	}
	add := func(kind, name string) {
		if name == "" || len(th.Participants) >= threads.MaxHeld {
			return
		}
		if slices.ContainsFunc(th.Participants, func(x threads.Participant) bool { return x.Kind == kind && x.Name == name }) {
			return
		}
		th.Participants = append(th.Participants, threads.Participant{Kind: kind, Name: name, Owner: kind == threads.Person && name == th.CreatedBy})
	}
	for _, r := range turns {
		add(threads.Person, runs.AskedBy(r))
		add(threads.Agent, r.Agent)
		if r.UpdatedAt.After(th.UpdatedAt) {
			th.UpdatedAt = r.UpdatedAt
		}
	}
	return th, nil
}

// recordOf returns a thread's stored record, writing the one a
// conversation from before threads reads as the first time it is needed.
// by is who becomes its owner when no person asked in the conversation, as
// claimant answers for the person making the change: "" leaves it with none,
// for the first member who changes it to become one.
func (p *Platform) recordOf(tenant, id, by string) (threads.Thread, error) {
	th, err := p.ThreadRecord(tenant, id)
	if err != nil {
		return th, err
	}
	if th.Version > 0 {
		if th.CreatedBy == "" && by != "" {
			return p.Threads.Claim(tenant, id, by, p.now())
		}
		return th, nil
	}
	th.CreatedBy = cmp.Or(th.CreatedBy, by)
	for i := range th.Participants {
		th.Participants[i].Owner = false
	}
	stored, err := p.Threads.Create(tenant, th, p.now())
	if errors.Is(err, threads.ErrExists) {
		// Another post wrote it first.
		return p.Threads.Get(tenant, id)
	}
	return stored, err
}

// claimant is who becomes the owner of a conversation from before threads
// that no person asked in, when a change they make writes its record: the
// person making it, while they are a member of its project, as changing the
// thread takes of them then (mayChangeThread). Anyone else, a viewer who
// answers what they were asked or removes themselves, leaves it with no
// owner, for a member to become one.
func (p *Platform) claimant(who auth.Principal, project string) string {
	if p.MayFileIn(who, project) != nil {
		return ""
	}
	return who.Subject
}

// placeFor checks where a person starts a new thread: a project of the
// tenant they are a member of (MayFileIn), which one they cannot see is as
// one that is not there, and one of its channels that is not archived.
func (p *Platform) placeFor(person auth.Principal, project, channel string) (string, projects.Channel, error) {
	tenant := person.Tenant
	project = projects.NameOf(project)
	if !p.HasProject(tenant, project) {
		return "", projects.Channel{}, fmt.Errorf("%w: %q", projects.ErrNotFound, project)
	}
	if err := p.MayFileIn(person, project); err != nil {
		return "", projects.Channel{}, err
	}
	ch, err := p.Projects.Channel(tenant, project, cmp.Or(channel, projects.General))
	if err != nil {
		return "", projects.Channel{}, err
	}
	if ch.Archived {
		return "", projects.Channel{}, fmt.Errorf("%w: #%s is archived, so it takes no new threads", projects.ErrInvalid, ch.Name)
	}
	return project, ch, nil
}

// checkDefault refuses a default that cannot answer where it is written: one
// that is neither auto nor an agent available in the project. sees is the
// agents its writer may read, nil for every agent: any other is refused as
// one that does not exist.
func (p *Platform) checkDefault(ctx context.Context, tenant, project, agent string, sees func(string) bool) error {
	if agent == "" || agent == threads.AutoName {
		return nil
	}
	if !projects.ValidDefault(agent) {
		return fmt.Errorf("%w: a default is an agent's name or auto", projects.ErrInvalid)
	}
	if _, err := p.Agents.Get(ctx, tenant, agent, 0); err != nil || (sees != nil && !sees(agent)) {
		return fmt.Errorf("%w: there is no agent called %s", projects.ErrInvalid, agent)
	}
	if reason := p.tagRefusal(tenant, project, agent); reason != "" {
		return fmt.Errorf("%w: %s, so it cannot answer there", projects.ErrInvalid, reason)
	}
	return nil
}

// checkRouting normalizes a thread's selected agents (threads.Routing) and
// refuses one that cannot answer in the project: an agent that does not
// exist, or one its writer may not read (sees, nil for every agent), as the
// same "there is no agent", and one not available there.
func (p *Platform) checkRouting(ctx context.Context, tenant, project string, agents []string, mode string, sees func(string) bool) ([]string, string, error) {
	agents, mode, err := threads.Routing(agents, mode)
	if err != nil {
		return nil, "", fmt.Errorf("%w: %s", projects.ErrInvalid, detailOf(err, threads.ErrInvalid))
	}
	for _, a := range agents {
		if _, err := p.Agents.Get(ctx, tenant, a, 0); err != nil || (sees != nil && !sees(a)) {
			return nil, "", fmt.Errorf("%w: there is no agent called %s", projects.ErrInvalid, a)
		}
		if reason := p.tagRefusal(tenant, project, a); reason != "" {
			return nil, "", fmt.Errorf("%w: %s, so it cannot answer there", projects.ErrInvalid, reason)
		}
	}
	return agents, mode, nil
}

// detailOf is an error's words after its sentinel's.
func detailOf(err, sentinel error) string {
	return strings.TrimPrefix(err.Error(), sentinel.Error()+": ")
}

// StartThread starts a shared thread in a project's channel with its first
// message, and starts what the message calls for.
func (p *Platform) StartThread(ctx context.Context, who auth.Principal, in ThreadInput) (Posted, error) {
	if in.Surface != nil {
		return p.startFromSurface(ctx, who, in)
	}
	return p.startThread(ctx, who, in, nil)
}

// startThread starts a thread, on a client's surface when on says which.
func (p *Platform) startThread(ctx context.Context, who auth.Principal, in ThreadInput, on *surfaced) (Posted, error) {
	by, name, via, slack, err := p.author(who, in.ActAs)
	if err != nil {
		return Posted{}, err
	}
	// Refused before its tags are looked up.
	if err := threads.CheckText(in.Text); err != nil {
		return Posted{}, err
	}
	if in.Visibility == "" && on == nil && in.ActAs == nil {
		// A conversation a person starts as themselves, as in the console,
		// starts private, its starter's alone, until they share it
		// (ShareThread). A client names its visibility.
		in.Visibility = threads.Private
	}
	switch in.Visibility {
	case threads.Shared, threads.Private:
	default:
		return Posted{}, fmt.Errorf("%w: a thread is shared or private", threads.ErrInvalid)
	}
	person, err := p.poster(who, in.ActAs)
	if err != nil {
		return Posted{}, err
	}
	project, ch, err := p.placeFor(person, in.Project, in.Channel)
	if err != nil {
		return Posted{}, err
	}
	keeps, err := p.mayPost(person, Place{Project: project}, projects.Member)
	if err != nil {
		return Posted{}, err
	}
	seer := p.Seer(person)
	if err := p.checkDefault(ctx, who.Tenant, project, in.Default, seer.Agent); err != nil {
		return Posted{}, err
	}
	routed, mode, err := p.checkRouting(ctx, who.Tenant, project, in.Agents, in.Mode, seer.Agent)
	if err != nil {
		return Posted{}, err
	}
	if err := p.posts.take(who.Tenant, by, project, p.now()); err != nil {
		return Posted{}, err
	}
	tree := p.Projects.Tree(who.Tenant)
	text, unmapped := in.Text, []threads.Mention(nil)
	rec := threads.Thread{
		Project: project, Channel: ch.Name, Title: strings.TrimSpace(in.Title), Visibility: in.Visibility, Default: in.Default, CreatedBy: by,
		Agents: routed, Mode: mode,
	}
	first := threads.Message{By: by, Name: name, Via: via}
	if on != nil {
		text, unmapped = p.surfaceText(who.Tenant, on.client, on.ref, in.Text)
		sf := on.ref.surface(on.client.ID)
		rec.Surface, rec.Announcement, first.SurfaceID, slack = &sf, on.announcement, on.ref.message(), nil
	}
	mentions, err := threads.Parse(text, tagLookup{ctx: ctx, p: p, tenant: who.Tenant, slack: slack, sees: seer.Agent, tree: tree, project: project})
	if err != nil {
		return Posted{}, err
	}
	if mentions, err = skillTags(mentions, on != nil, on == nil && in.ActAs == nil && !in.PostOnly, len(routed) > 0); err != nil {
		return Posted{}, err
	}
	if on != nil && on.ref.Files {
		first.Attachments, err = p.surfaceFiles(ctx, who, on.ref, in.ActAs)
		if err != nil {
			return Posted{}, err
		}
	}
	first.Text, first.Mentions = text, p.checkTags(who.Tenant, tree, project, mentions)
	th, m, err := p.Threads.Start(who.Tenant, rec, first, p.now())
	if err != nil {
		return Posted{}, err
	}
	out, err := p.carryOut(ctx, who.Tenant, tree, th, m, clientOf(in.ActAs), in.PostOnly, false, keeps, seer.Agent)
	out.Refused = append(out.Refused, unmapped...)
	return out, err
}

// clientOf is the client whose credential posts for a person, "" for a
// person who posts as themselves.
func clientOf(as *ActAs) string {
	if as == nil {
		return ""
	}
	return as.Client
}

// PostMessage posts a person's message into a thread, and starts what it
// calls for. A message that answers a question records the answer first,
// which resumes the run, and posts nothing if the answer is refused. A
// second post with a surface's id already posted is the first, and starts
// nothing again.
func (p *Platform) PostMessage(ctx context.Context, who auth.Principal, id string, in MessageInput) (Posted, error) {
	// Another tenant's thread is not there, whoever asks.
	th, err := p.ThreadRecord(who.Tenant, id)
	if err != nil {
		return Posted{}, err
	}
	// Refused before its tags are looked up.
	if err := threads.CheckText(in.Text); err != nil {
		return Posted{}, err
	}
	by, name, via, slack, err := p.author(who, in.ActAs)
	if err != nil {
		return Posted{}, err
	}
	person, err := p.poster(who, in.ActAs)
	if err != nil {
		return Posted{}, err
	}
	need := postNeed(in)
	keeps, err := p.mayPost(person, Place{Project: th.Project, Thread: th.ID}, need)
	if errors.Is(err, projects.ErrForbidden) && need == projects.Member && (in.Answers != nil || in.reply) {
		return Posted{}, fmt.Errorf("%w to tag anyone, even in an answer or a reply", err)
	}
	if err != nil {
		return Posted{}, err
	}
	on, err := p.surfaceOfPost(who, th, in)
	if err != nil {
		return Posted{}, err
	}
	prev, err := p.prevOf(who.Tenant, th, in.After)
	if err != nil {
		return Posted{}, err
	}
	if err := p.posts.take(who.Tenant, by, th.Project, p.now()); err != nil {
		return Posted{}, err
	}
	tree := p.Projects.Tree(who.Tenant)
	text, unmapped, surfaceID := in.Text, []threads.Mention(nil), ""
	if on != nil {
		text, unmapped = p.surfaceText(who.Tenant, on.client, on.ref, in.Text)
		surfaceID, slack = on.ref.message(), nil
	}
	sees := p.Seer(person).Agent
	mentions, err := threads.Parse(text, tagLookup{ctx: ctx, p: p, tenant: who.Tenant, slack: slack, sees: sees, tree: tree, project: th.Project})
	if err != nil {
		return Posted{}, err
	}
	// An answer or a reply is the poster's response, which starts no run
	// for a skill it names.
	console := on == nil && in.ActAs == nil && !in.PostOnly && in.Answers == nil && !in.reply
	if mentions, err = skillTags(mentions, on != nil, console, slices.ContainsFunc(th.Agents, sees)); err != nil {
		return Posted{}, err
	}
	mentions = p.checkTags(who.Tenant, tree, th.Project, mentions)
	msg := threads.Message{By: by, Name: name, Text: text, Mentions: mentions, Via: via, Prev: prev, SurfaceID: surfaceID}
	if on != nil && on.ref.Files {
		msg.Attachments, err = p.surfaceFiles(ctx, who, on.ref, in.ActAs)
		if err != nil {
			return Posted{}, err
		}
	}
	if in.Answers != nil {
		if err := p.answerFromThread(ctx, who, in.ActAs, th, *in.Answers, in.Text); err != nil {
			return Posted{}, err
		}
		a := *in.Answers
		msg.Answers = &a
	}
	if th, err = p.recordOf(who.Tenant, id, p.claimant(person, th.Project)); err != nil {
		return Posted{}, err
	}
	m, fresh, err := p.Threads.Post(who.Tenant, th.ID, msg, p.now())
	if err != nil {
		return Posted{}, err
	}
	if th, err = p.Threads.Get(who.Tenant, th.ID); err != nil {
		return Posted{}, err
	}
	if !fresh {
		again := p.posted(who.Tenant, th, m)
		again.Repeated = true
		return again, nil
	}
	p.repliedTo(who.Tenant, th.ID, m)
	out, err := p.carryOut(ctx, who.Tenant, tree, th, m, clientOf(in.ActAs), in.PostOnly, in.Answers != nil || in.reply, keeps, sees)
	out.Refused = append(out.Refused, unmapped...)
	return out, err
}

// repliedTo settles the mention of a message's poster in the message it
// follows: a person who replies in the thread, from wherever they read it,
// has answered being mentioned.
func (p *Platform) repliedTo(tenant, thread string, m threads.Message) {
	if !strings.HasPrefix(m.Prev, "m-") {
		return
	}
	mention, ok := p.Inbox.ByKey(tenant, inbox.MentionKey(thread, m.Prev, m.By))
	if !ok || mention.State == inbox.Resolved || mention.State == inbox.Dismissed {
		return
	}
	for _, part := range mention.Parts {
		if part.Type == inbox.PartMention && part.State == inbox.PartOpen {
			_, _, _ = p.Inbox.Resolve(tenant, mention.ID, part.ID, inbox.Outcome{Action: ActReply, By: m.By, At: p.now(), Note: "Replied in the thread"})
		}
	}
}

// prevOf is the turn a message follows, from what the poster named: a
// message of the thread, the message a run of it answers, or, in a
// conversation from before threads, one of its runs. "" follows the latest
// message, or for a conversation with no message yet, its latest turn.
func (p *Platform) prevOf(tenant string, th threads.Thread, after string) (string, error) {
	switch {
	case after == "":
		if th.Version > 0 {
			if _, err := p.Threads.Message(tenant, th.ID, threads.MessageID(1)); err == nil {
				return "", nil
			}
		}
		turns, err := p.Runs.Conversation(tenant, th.ID)
		if err != nil {
			return "", nil
		}
		return turns[len(turns)-1].ID, nil
	case strings.HasPrefix(after, "m-"):
		if _, err := p.Threads.Message(tenant, th.ID, after); err != nil {
			return "", fmt.Errorf("%w: this thread has no message %s", threads.ErrInvalid, after)
		}
		return after, nil
	}
	r, err := p.Runs.Get(tenant, after)
	if err != nil || r.Thread != th.ID || r.Parent != "" {
		return "", fmt.Errorf("%w: %q is not a turn of this thread", threads.ErrInvalid, after)
	}
	if r.Message != "" {
		return r.Message, nil
	}
	return r.ID, nil
}

// answerFromThread records a message as a question's answer: the reply's
// words, or the option it names. The question must be one a run of this
// thread is parked on, and who may answer it is checked as on every other
// path. as is the person a surface's credential posts for, so the answer is
// checked against and recorded as the person the message is posted as, with
// the credential as the way it came. The message is the answer only if it is
// the one the run was given: a question already answered, or whose run
// stopped or ended, or that someone else answered first, is ErrAnswered, and
// nothing is posted. So a viewer, who posts only answers, posts one for each
// question put to them.
func (p *Platform) answerFromThread(ctx context.Context, who auth.Principal, as *ActAs, th threads.Thread, ref threads.AnswerRef, text string) error {
	run, err := p.Runs.Get(who.Tenant, ref.Run)
	if err != nil || run.Thread != th.ID {
		return fmt.Errorf("%w: no run of this thread asked that", threads.ErrInvalid)
	}
	m, ok := p.Inbox.ByKey(who.Tenant, questionKey(ref.Run, ref.Question))
	if !ok || len(m.Parts) == 0 {
		return fmt.Errorf("%w: no run of this thread asked that", threads.ErrInvalid)
	}
	a := QuestionAnswer{Text: strings.TrimSpace(text), ActAs: as}
	switch m.Parts[0].Type {
	case inbox.PartReply:
		a.Action = ActReply
	case inbox.PartChoice:
		a.Action, a.Choice = ActChoose, a.Text
	default:
		return fmt.Errorf("%w: an approval is decided with Approve or Deny, not a message", threads.ErrInvalid)
	}
	_, recorded, err := p.answerFirst(ctx, who, m, a)
	if err == nil && !recorded {
		err = fmt.Errorf("%w: that question was answered, or its run stopped", ErrAnswered)
	}
	return err
}

// carryOut starts what a posted message calls for, records what each of
// its tags came to, and notifies the people and experts it tags. via is the
// client whose credential posted it for its poster, "" for a person who
// posted as themselves. keeps says the poster may change the project's notes
// (mayPost). sees is the agents the poster may read: a default naming one
// they may not read is passed over for them, as if unset (placeSeen), so
// what the message did never names it.
func (p *Platform) carryOut(ctx context.Context, tenant string, tree *projects.Tree, th threads.Thread, m threads.Message, via string, postOnly, reply, keeps bool, sees func(string) bool) (Posted, error) {
	// No share moves the thread while its runs are being started, and they
	// are filed where it is now.
	mu := p.moves.of(tenant, th.ID)
	mu.RLock()
	defer mu.RUnlock()
	if cur, err := p.Threads.Get(tenant, th.ID); err == nil {
		if cur.Project != th.Project {
			// Shared into another project since it was posted: whether the
			// poster may keep notes was asked of the project it left.
			keeps = false
		}
		th = cur
	}
	def, unavailable := p.defaultOf(tenant, th, sees)
	// A person posting in the console, as themselves, runs only the agents
	// they tag: no default answers them. A client's surface keeps its
	// defaults, where mentioning the app is what asks.
	tagsOnly := m.Via == threads.ViaConsole
	// The thread's selected agents answer an untagged message, those the
	// poster may read: one they may not is passed over for them.
	routed, mode := slices.DeleteFunc(slices.Clone(th.Agents), func(a string) bool { return sees != nil && !sees(a) }), th.Mode
	if len(routed) < 2 {
		mode = ""
	}
	d := threads.Decide(threads.Input{Mentions: m.Mentions, Reply: reply, PostOnly: postOnly, TagsOnly: tagsOnly, Routed: routed, Mode: mode, Default: def, Unavailable: unavailable})
	out := Posted{Runs: []runs.Run{}, Notified: []string{}, Refused: []threads.Mention{}, Rule: d.Rule, Reason: d.Reason}
	mentions := d.Mentions
	// The runs a message starts act for the person who posted it, and for
	// nobody else in the thread: the tools that act as a person use the
	// poster's own credentials (credentials.go), though each run is sent
	// what others said in the room, fenced off ahead of the poster's words
	// (threads.Ask).
	actor := envelope.Actor{ID: m.By, Display: m.Name, User: m.By}
	var answered []threads.Participant
	start := func(agent string) (runs.Run, error) {
		// A run the selection starts says how: the best fit among the
		// selected agents, or one of all of them (a lone selected agent
		// answers as all of one).
		routedBy := ""
		switch {
		case d.Rule == threads.RuleRouted && d.Among != nil:
			routedBy = threads.ModeBestFit
		case d.Rule == threads.RuleRouted:
			routedBy = threads.ModeAll
		}
		run, err := p.answerMessage(ctx, tenant, th, m, via, agent, d.Among, routedBy, actor, answerTagID(th.ID, m.ID, agent), keeps, sees)
		if err == nil {
			out.Runs = append(out.Runs, run)
			answered = append(answered, threads.Participant{Kind: threads.Agent, Name: run.Agent, AddedBy: m.By})
		}
		return run, err
	}
	switch {
	case d.Rule == threads.RuleTagged:
		for i := range mentions {
			mn := &mentions[i]
			if mn.Kind != threads.Agent || mn.Refused != "" || !slices.Contains(d.Agents, mn.Name) {
				continue
			}
			run, err := start(mn.Name)
			if err != nil {
				mn.Refused = refusalOf(err)
				continue
			}
			mn.Run = run.ID
		}
	case d.Rule == threads.RuleRouted:
		var refused []string
		for _, agent := range d.Agents {
			if _, err := start(agent); err != nil {
				refused = append(refused, refusalOf(err))
			}
		}
		switch {
		case len(refused) > 0 && len(out.Runs) == 0:
			out.Reason = "No selected agent could answer: " + strings.Join(refused, "; ") + "."
		case len(refused) > 0:
			out.Reason = "Not every selected agent could answer: " + strings.Join(refused, "; ") + "."
		}
	case d.Default && len(d.Agents) == 1:
		if _, err := start(d.Agents[0]); err != nil {
			out.Reason = fmt.Sprintf("@%s answers untagged messages here, but %s.", d.Agents[0], refusalOf(err))
		}
	}
	out.Notified = p.notifyTagged(tenant, tree, th, m, mentions)
	if !slices.Equal(mentions, m.Mentions) {
		settled, err := p.Threads.Settle(tenant, th.ID, m.ID, mentions)
		if err != nil {
			p.log.Error("platform: what a message's tags came to could not be kept", "tenant", tenant, "thread", th.ID, "message", m.ID, "error", err)
		} else {
			m = settled
		}
	}
	if len(answered) > 0 {
		if joined, err := p.Threads.Join(tenant, th.ID, answered, p.now()); err == nil {
			th = joined
		}
	}
	for _, mn := range m.Mentions {
		if mn.Refused != "" {
			out.Refused = append(out.Refused, mn)
		}
	}
	out.Thread, out.Message = th, m
	return out, nil
}

// refusalOf is why a run was not started, as a person reads it: the error's
// words, without the name of the error they wrap.
func refusalOf(err error) string {
	msg := err.Error()
	for _, known := range []error{ErrNotAvailable, ErrNoDefault} {
		msg = strings.TrimPrefix(msg, known.Error()+": ")
	}
	for _, prefix := range []string{"platform: ", "pick: "} {
		msg = strings.TrimPrefix(msg, prefix)
	}
	return msg
}

// posted is what a message already posted did, read back: the runs that
// answer it and what its tags came to.
func (p *Platform) posted(tenant string, th threads.Thread, m threads.Message) Posted {
	out := Posted{Thread: th, Message: m, Runs: []runs.Run{}, Notified: []string{}, Refused: []threads.Mention{}}
	if turns, err := p.Runs.Conversation(tenant, th.ID); err == nil {
		for _, r := range turns {
			if r.Message == m.ID {
				out.Runs = append(out.Runs, r)
			}
		}
	}
	for _, mn := range m.Mentions {
		if mn.Refused != "" {
			out.Refused = append(out.Refused, mn)
		}
	}
	return out
}

// answerTagID names the run of one agent answering one message, so a post
// that is retried joins the run it started.
func answerTagID(thread, message, agent string) string {
	return "thread-" + thread + "-" + message + "-" + agent
}

// answerMessage starts an agent answering a person's message, as the run
// tagID names: @auto picks the agent first, among the agents among names
// alone when it names any (a thread's selected agents, best fit). The agent is sent its context and
// the room's transcript up to the message, then the message. via is the
// client whose credential posted the message for its poster, if one did.
// keeps pins that the person it acts for may change the project's notes, as
// a turn does (Turn.KeepsNotes). sees is the agents that person may read:
// @auto never falls back to one they may not read.
func (p *Platform) answerMessage(ctx context.Context, tenant string, th threads.Thread, m threads.Message, via, agent string, among []string, routed string, actor envelope.Actor, tagID string, keeps bool, sees func(string) bool) (runs.Run, error) {
	if agent == Auto && len(among) > 0 {
		// The best fit is picked among the selected agents the clients it
		// reaches the thread through may route to, and it is the agent
		// picked that they are held to, not @auto.
		among = slices.DeleteFunc(slices.Clone(among), func(a string) bool { return p.mayRoute(tenant, th, via, a) != nil })
		if len(among) == 0 {
			return runs.Run{}, fmt.Errorf("%w: no selected agent may answer here", ErrNotAvailable)
		}
	} else if err := p.mayRoute(tenant, th, via, agent); err != nil {
		return runs.Run{}, err
	}
	meta := map[string]string{
		runs.MetaAsk: m.Text, runs.MetaThread: th.ID, runs.MetaPrev: m.ID, runs.MetaMessage: m.ID,
		runs.MetaChannel: th.Channel, runs.MetaProject: th.Project,
	}
	if keeps && th.Project != "" {
		meta[metaKeepsNotes] = th.Project
	}
	if routed != "" {
		meta[runs.MetaRouted] = routed
	}
	skills := p.skillsOfMessage(tenant, th, m, via)
	pinSkills(meta, skills)
	if agent == Auto && len(among) > 0 {
		who, err := p.bestFit(ctx, tenant, th.Project, pickText(m.Text, skills), among, sees)
		if err != nil {
			return runs.Run{}, err
		}
		agent = who.Agent
		meta[runs.MetaPicked] = who.Reason
	} else if agent == Auto {
		who, err := p.resolveAgent(ctx, tenant, placeSeen(p.placeOfThread(tenant, th), sees), Auto, pickText(m.Text, skills), sees)
		if err != nil {
			return runs.Run{}, err
		}
		agent = who.Agent
		meta[runs.MetaPicked] = who.Picked
	}
	cfg := p.Settings(tenant)
	body := m.Text
	if cfg.History == HistoryRecent {
		body = threads.Ask(p.roomBefore(tenant, th, m), cfg.HistoryLimit, p.shower(tenant), threads.Asker{Person: m.By, Name: m.Name}, m.Text)
	}
	return p.Invoke(ctx, envelope.Tag{
		ID: tagID, Tenant: tenant, Agent: agent, Origin: runs.OriginAPI,
		Text: p.contextIn(tenant, th.Project, th.Channel, agent, body), Attachments: m.Attachments, Actor: actor, Meta: meta,
	})
}

// bestFit is the one of a thread's selected agents that fits a message best,
// as @auto picks (pkg/pick), among them alone and those the poster may read;
// when none fits, the first of them available in the project.
func (p *Platform) bestFit(ctx context.Context, tenant, project, text string, among []string, sees func(string) bool) (pick.Choice, error) {
	tree := p.Projects.Tree(tenant)
	keep := func(a string) bool { return slices.Contains(among, a) && (sees == nil || sees(a)) }
	fallback := ""
	for _, a := range among {
		if keep(a) && tree.IsAvailable(project, a) {
			fallback = a
			break
		}
	}
	return p.auto(ctx, tenant, tree, project, text, fallback, keep)
}

// mayRoute holds an agent answering a message to the allow-lists of the
// clients it reaches the thread through: the client whose surface the
// thread is on, and the client whose credential posted the message for a
// person, wherever the thread is. An agent either may not route to is
// refused, never swapped, and a client that lists its agents treats @auto
// as a name outside the list. A client that posted the message and is gone
// now routes to nothing.
func (p *Platform) mayRoute(tenant string, th threads.Thread, via, agent string) error {
	if th.Surface != nil {
		if c, err := p.Clients.Get(tenant, th.Surface.Client); err == nil && !c.Routes(agent) {
			return fmt.Errorf("%w: %s may not route to @%s", ErrNotAvailable, c.ID, agent)
		}
	}
	if via == "" || th.Surface != nil && th.Surface.Client == via {
		return nil
	}
	c, err := p.Clients.Get(tenant, via)
	if err != nil {
		return fmt.Errorf("%w: the client this was posted through is gone", ErrNotAvailable)
	}
	if !c.Routes(agent) {
		return fmt.Errorf("%w: %s may not route to @%s", ErrNotAvailable, c.ID, agent)
	}
	return nil
}

// maxRoomDepth bounds how many times a transcript follows a branch back from
// the runs that answer a message to the room before it.
const maxRoomDepth = 8

// room is a conversation's runs, read once for one transcript: the runs that
// answer each message, and the runs that follow a run, as the API's
// follow-ups do.
type room struct {
	answers map[string][]runs.Run
	follows map[string][]runs.Run
}

func (p *Platform) roomOf(tenant, id string) room {
	rm := room{answers: map[string][]runs.Run{}, follows: map[string][]runs.Run{}}
	if turns, err := p.Runs.Conversation(tenant, id); err == nil {
		for _, r := range turns {
			switch {
			case r.Message != "":
				rm.answers[r.Message] = append(rm.answers[r.Message], r)
			case r.Prev != "":
				rm.follows[r.Prev] = append(rm.follows[r.Prev], r)
			}
		}
	}
	return rm
}

// after is the runs that follow a run, and the runs that follow them, made
// before a moment: what the room showed after it then.
func (rm room) after(id string, before time.Time) []threads.Turn {
	var out []threads.Turn
	next := []string{id}
	for len(next) > 0 {
		at := next[0]
		next = next[1:]
		for _, r := range rm.follows[at] {
			if r.CreatedAt.Before(before) {
				out = append(out, threads.RunTurn(r))
				next = append(next, r.ID)
			}
		}
	}
	return out
}

// roomBefore is the thread as it stands before a message, on the message's
// branch: the messages leading to it, each followed by the runs that answer
// it and the follow-ups to those made before the message, and ahead of them
// the turns that the first message follows.
func (p *Platform) roomBefore(tenant string, th threads.Thread, m threads.Message) []threads.Turn {
	return append(p.seedTurns(tenant, th), p.messagesBefore(tenant, th, p.roomOf(tenant, th.ID), m, maxRoomDepth)...)
}

// branchBefore is the conversation as a follow-up through the API finds it:
// the runs of its branch and, when the first of them answers a message, the
// room up to and with that message ahead of them.
func (p *Platform) branchBefore(tenant string, th threads.Thread, path []runs.Run) []threads.Turn {
	return append(p.seedTurns(tenant, th), p.runsBefore(tenant, th, p.roomOf(tenant, th.ID), path, maxRoomDepth)...)
}

func (p *Platform) messagesBefore(tenant string, th threads.Thread, rm room, m threads.Message, depth int) []threads.Turn {
	path, err := p.Threads.Path(tenant, th.ID, m.ID)
	if err != nil {
		return nil
	}
	var out []threads.Turn
	if first := path[0]; first.Prev != "" && !strings.HasPrefix(first.Prev, "m-") {
		if branch, err := p.Runs.Path(tenant, first.Prev); err == nil {
			out = p.runsBefore(tenant, th, rm, branch, depth)
		}
	}
	for _, msg := range path[:len(path)-1] {
		out = append(out, threads.MessageTurn(msg))
		for _, r := range rm.answers[msg.ID] {
			out = append(out, threads.RunTurn(r))
			out = append(out, rm.after(r.ID, m.At)...)
		}
	}
	return out
}

func (p *Platform) runsBefore(tenant string, th threads.Thread, rm room, path []runs.Run, depth int) []threads.Turn {
	var out []threads.Turn
	if head := path[0]; depth > 0 && strings.HasPrefix(head.Prev, "m-") {
		if m, err := p.Threads.Message(tenant, th.ID, head.Prev); err == nil {
			out = append(p.messagesBefore(tenant, th, rm, m, depth-1), threads.MessageTurn(m))
		}
	}
	for _, r := range path {
		out = append(out, threads.RunTurn(r))
	}
	return out
}

// MaxNotified is how many people, and roles, one message sends a mention to.
// The experts of a topic can be many, so a tag of them stops there: a tag
// none of whose people is reached past it is refused, saying so.
const MaxNotified = 20

// notifyTagged sends the people and experts a message tags a mention in
// their inbox, once each and at most MaxNotified in all, and returns who was
// sent one. The poster is not sent their own. A tag that reaches nobody for
// the cap is marked refused in mentions.
func (p *Platform) notifyTagged(tenant string, tree *projects.Tree, th threads.Thread, m threads.Message, mentions []threads.Mention) []string {
	sent := []string{}
	// invited are the people this message invited into a private thread by
	// tagging them, who take part from now on.
	var invited []string
	for i := range mentions {
		mn := &mentions[i]
		if mn.Refused != "" {
			continue
		}
		var to []string
		switch mn.Kind {
		case threads.Person:
			to = []string{mn.Name}
		case threads.SME:
			to, _ = tree.ExpertsFor(th.Project, mn.Name)
		case threads.Role:
			// Its holders are not invited, so in a private thread they
			// could not read what they were told of.
			if th.Visibility == threads.Private {
				mn.Refused = "a role is not told in a private thread; tag its people, or share the thread"
				continue
			}
			to = []string{projects.RolePrefix + mn.Name}
		}
		reached, cut, blocked, outside := false, false, false, false
		for _, who := range to {
			if who == m.By || slices.Contains(sent, who) {
				reached = true
				continue
			}
			// Experts are found, not chosen by the poster, so in a private
			// thread they are told only when they take part: none is
			// invited, and a role, which takes no part, is told nothing.
			if mn.Kind == threads.SME && th.Visibility == threads.Private && !slices.Contains(invited, who) && !takesPart(th, who) {
				outside = true
				continue
			}
			// Only someone who can read the conversation is told of it: a
			// person at least a viewer of its project (whom a private
			// thread then invites), and a role's holders only in a shared
			// thread, which a role cannot be invited to.
			if !p.mayBeTold(tenant, tree, th, who) {
				blocked = true
				continue
			}
			if len(sent) >= MaxNotified {
				cut = true
				break
			}
			// In a private thread, tagging a person invites them: they
			// take part from now on, so they can read what they were
			// tagged in.
			if th.Visibility == threads.Private && mn.Kind == threads.Person && !takesPart(th, who) {
				if _, err := p.Threads.Join(tenant, th.ID, []threads.Participant{{Kind: threads.Person, Name: who, AddedBy: m.By}}, p.now()); err != nil {
					p.log.Warn("platform: a person tagged in a private thread could not be invited", "tenant", tenant, "thread", th.ID, "error", err)
					continue
				}
				invited = append(invited, who)
			}
			p.raiseMention(tenant, th, m, who, *mn)
			sent = append(sent, who)
			reached = true
		}
		switch {
		case outside && !reached:
			mn.Refused = "no expert takes part in this private conversation, so nobody was told; share it first, or tag people"
		case outside:
			mn.Refused = "only the experts taking part were told; the others cannot read this private conversation"
		case cut && !reached:
			mn.Refused = fmt.Sprintf("a message notifies at most %d people", MaxNotified)
		case blocked && !reached && mn.Kind == threads.Person:
			mn.Refused = "they cannot read this project, so they were not told"
		case blocked && !reached && th.Visibility == threads.Private:
			mn.Refused = "a role is told only in a shared conversation; share it first, or tag people"
		case blocked && !reached:
			mn.Refused = "nobody it names can read this project, so nobody was told"
		}
	}
	return sent
}

// mayBeTold reports whether someone a message tags may be told of it: a
// person who is at least a viewer of the thread's project, as their roles in
// the directory and the workspace's policy make them (the directory's are
// those they held when last seen, so a group they lost since still counts
// until they sign in again; the inbox's own read check holds them then), and a role
// ("role:<name>") only in a shared thread, since a private one is read by
// its participants and a role is none.
func (p *Platform) mayBeTold(tenant string, tree *projects.Tree, th threads.Thread, who string) bool {
	if strings.HasPrefix(who, projects.RolePrefix) {
		return th.Visibility != threads.Private
	}
	person := p.byPolicy(tenant, who)
	if p.People != nil {
		if known, err := p.People.Get(tenant, who); err == nil {
			for _, r := range known.Roles {
				if !slices.Contains(person.Roles, auth.Role(r)) {
					person.Roles = append(person.Roles, auth.Role(r))
				}
			}
		}
	}
	reader := ReaderOf(person)
	return reader.Govern || tree.Resolve(th.Project, reader).Role.AtLeast(projects.Viewer)
}

// raiseMention puts a mention in the inbox of the person or experts tagged.
// It names the poster as the thread shows them, never by subject: what it
// keeps is the name they go by now, and a read names them as their message
// is called then (Names.mention).
func (p *Platform) raiseMention(tenant string, th threads.Thread, m threads.Message, to string, mn threads.Mention) {
	s := p.shower(tenant)
	shown := s.message(m)
	where := "#" + th.Channel
	subject := fmt.Sprintf("%s mentioned you in %s", shown.Name, where)
	switch mn.Kind {
	case threads.SME:
		subject = fmt.Sprintf("%s asked the experts in %s", shown.Name, where)
		if mn.Name != "" {
			subject = fmt.Sprintf("%s asked the %s experts in %s", shown.Name, mn.Name, where)
		}
	case threads.Role:
		subject = fmt.Sprintf("%s mentioned everyone holding %s in %s", shown.Name, mn.Name, where)
	}
	msg := inbox.Message{
		Kind: inbox.KindMention, Subject: subject, From: inbox.From{Kind: "person", Name: shown.Name}, Severity: inbox.Info,
		Source: inbox.Source{Thread: th.ID, Project: th.Project},
		Parts:  []inbox.Part{{Type: inbox.PartMention, Mention: s.mentionOf(th, m)}},
	}
	// A person is sent their own; experts named by a role share one.
	if strings.HasPrefix(to, projects.RolePrefix) {
		msg.Audience = []string{to}
	} else {
		msg.To = to
	}
	p.raiseInbox(tenant, inbox.MentionKey(th.ID, m.ID, to), msg, m.At)
}

// replyToMention posts the reader's reply to a message that mentioned them,
// as a human message the default does not answer. Only the agents it tags
// answer it, and tagging takes a member (postNeed).
func (p *Platform) replyToMention(ctx context.Context, who auth.Principal, part inbox.MentionPart, text string, as *ActAs) (threads.Message, error) {
	if strings.TrimSpace(text) == "" {
		return threads.Message{}, fmt.Errorf("%w: a reply says something", threads.ErrInvalid)
	}
	if err := threads.CheckText(text); err != nil {
		return threads.Message{}, err
	}
	posted, err := p.PostMessage(ctx, who, part.Thread, MessageInput{Text: text, After: part.Message, reply: true, ActAs: as})
	return posted.Message, err
}

// UpdateThread changes a thread's title, visibility, default and selected
// agents, and moves a private conversation. Title, visibility and default
// take the thread's owner, the person who started it, or an owner of its
// project (mayChangeThread), and so does moving a private conversation to
// another project or channel, as sharing it does, which also takes being a
// member of the project it goes to, whose channel is not archived
// (placeFor). Selecting agents takes anyone who may post in it (MayPost). A move keeps it private (Share
// makes it shared), and is refused once a run has been filed in it
// (ErrStaysInProject, ErrStaysInChannel), serialized with its runs as a
// share is. A default, and each selected agent, must be able to answer where
// the thread is after the change.
func (p *Platform) UpdateThread(ctx context.Context, who auth.Principal, id string, in ThreadUpdate) (threads.Thread, error) {
	mu := p.moves.of(who.Tenant, id)
	mu.Lock()
	defer mu.Unlock()
	th, err := p.ThreadRecord(who.Tenant, id)
	if err != nil {
		return threads.Thread{}, err
	}
	seer := p.Seer(who)
	// A default naming an agent the caller may not read was not shown to
	// them, so naming none keeps it; and so is a selected agent.
	shown := seer.threadAgents(th)
	shownDefault := th.Default
	if shownDefault != "" && !shown(shownDefault) {
		shownDefault = ""
	}
	keepsDefault := in.Default == shownDefault
	project, channel := th.Project, cmp.Or(in.Channel, th.Channel)
	if in.Project != "" && projects.NameOf(in.Project) != th.Project {
		project, channel = projects.NameOf(in.Project), cmp.Or(in.Channel, projects.General)
	}
	moves := project != th.Project || channel != th.Channel
	if !moves && strings.TrimSpace(in.Title) == th.Title && cmp.Or(in.Visibility, th.Visibility) == th.Visibility && keepsDefault {
		// Only which agents answer changes: anyone who may post here does
		// that, as they may tag. Moving it takes what sharing it takes.
		if err := p.changesThread(who, th); err != nil {
			return threads.Thread{}, err
		}
		if err := p.MayPost(who, Place{Project: th.Project, Thread: th.ID}); err != nil {
			return threads.Thread{}, err
		}
	} else if err := p.mayChangeThread(who, th); err != nil {
		return threads.Thread{}, err
	}
	if in.Version != th.Version {
		return threads.Thread{}, fmt.Errorf("%w: %q is at version %d, not %d", threads.ErrVersion, id, th.Version, in.Version)
	}
	if moves {
		if th.Visibility != threads.Private || cmp.Or(in.Visibility, th.Visibility) != threads.Private || th.Surface != nil {
			return threads.Thread{}, fmt.Errorf("%w: only a private conversation moves here; Share moves one as it shares it", threads.ErrInvalid)
		}
		got, ch, err := p.placeFor(who, project, channel)
		if err != nil {
			return threads.Thread{}, err
		}
		project, channel = got, ch.Name
		if _, err := p.Runs.Conversation(who.Tenant, id); err == nil {
			if project != th.Project {
				return threads.Thread{}, ErrStaysInProject
			}
			return threads.Thread{}, ErrStaysInChannel
		}
	}
	if err := p.checkDefault(ctx, who.Tenant, project, in.Default, seer.Agent); err != nil {
		return threads.Thread{}, err
	}
	// Every name the caller sends is checked as they read it, so one they
	// may not read is refused as unknown whether or not it is selected.
	routed, mode, err := p.checkRouting(ctx, who.Tenant, project, in.Agents, in.Mode, seer.Agent)
	if err != nil {
		return threads.Thread{}, err
	}
	// A selected agent the caller may not read is kept, while it still fits
	// beside theirs and can answer where the thread is; otherwise their
	// selection wins and it is dropped. No refusal counts or names it.
	hidden := slices.DeleteFunc(slices.Clone(th.Agents), func(a string) bool { return shown(a) || p.tagRefusal(who.Tenant, project, a) != "" })
	if len(hidden) > 0 {
		if all, m, err := threads.Routing(append(slices.Clone(routed), hidden...), cmp.Or(in.Mode, mode, th.Mode)); err == nil {
			routed, mode = all, m
		}
	}
	if th, err = p.recordOf(who.Tenant, id, p.claimant(who, th.Project)); err != nil {
		return threads.Thread{}, err
	}
	return p.Threads.UpdateMoving(who.Tenant, id, th.Version, p.now(), func(t *threads.Thread) error {
		t.Title, t.Visibility = strings.TrimSpace(in.Title), cmp.Or(in.Visibility, t.Visibility)
		if !keepsDefault {
			t.Default = in.Default
		}
		if t.Project != project && t.Default != "" && t.Default != Auto && p.tagRefusal(who.Tenant, project, t.Default) != "" {
			// A default kept, hidden from the caller, that cannot answer
			// where it goes is cleared, naming nothing.
			t.Default = ""
		}
		t.Project, t.Channel = project, channel
		t.Agents, t.Mode = routed, mode
		return nil
	})
}

// SetParticipants adds and removes a thread's people and agents. Adding
// takes posting there (MayPost): a member of the project, and in a private
// thread a participant. Removing anyone but oneself takes the thread's owner
// or an owner of its project (mayChangeThread). An agent added must be
// available in the thread's project, and a person must be in the workspace's
// directory. People are named by handle, as a thread shows them, and kept by
// subject.
func (p *Platform) SetParticipants(ctx context.Context, who auth.Principal, id string, in ParticipantsUpdate) (threads.Thread, error) {
	th, err := p.ThreadRecord(who.Tenant, id)
	if err != nil {
		return threads.Thread{}, err
	}
	if err := p.changesThread(who, th); err != nil {
		return threads.Thread{}, err
	}
	if len(in.Add) > 0 {
		if err := p.MayPost(who, Place{Project: th.Project, Thread: th.ID}); err != nil {
			return threads.Thread{}, err
		}
	}
	if in.Version != th.Version {
		return threads.Thread{}, fmt.Errorf("%w: %q is at version %d, not %d", threads.ErrVersion, id, th.Version, in.Version)
	}
	if len(in.Add)+len(in.Remove) > threads.MaxParticipants {
		return threads.Thread{}, fmt.Errorf("%w: at most %d people and agents are added or removed at once", threads.ErrInvalid, threads.MaxParticipants)
	}
	add := make([]threads.Participant, len(in.Add))
	for i, a := range in.Add {
		switch a.Kind {
		case threads.Agent:
			// One the caller may not read is one that does not exist.
			if _, err := p.Agents.Get(ctx, who.Tenant, a.Name, 0); err != nil || !p.Seer(who).Agent(a.Name) {
				return threads.Thread{}, fmt.Errorf("%w: there is no agent called %s", threads.ErrInvalid, a.Name)
			}
			if reason := p.tagRefusal(who.Tenant, th.Project, a.Name); reason != "" {
				return threads.Thread{}, fmt.Errorf("%w: %s", threads.ErrInvalid, reason)
			}
		case threads.Person:
			subject, found := p.byHandle(who.Tenant, a.Name)
			if !found {
				return threads.Thread{}, fmt.Errorf("%w: no one in this workspace goes by @%s", threads.ErrInvalid, a.Name)
			}
			a.Name = subject
		}
		add[i] = a
	}
	remove := make([]string, len(in.Remove))
	for i, name := range in.Remove {
		if subject, found := p.byHandle(who.Tenant, name); found {
			name = subject
		}
		remove[i] = name
	}
	if slices.ContainsFunc(remove, func(name string) bool { return name != who.Subject }) {
		if err := p.mayChangeThread(who, th); err != nil {
			return threads.Thread{}, err
		}
	}
	// An agent the caller may not read that never answered here is not
	// among the participants they are shown, so removing it is removing one
	// that is not there, and it counts toward no limit they are told of.
	shown := p.Seer(who).participantShownNow(th)
	remove = slices.DeleteFunc(remove, func(name string) bool {
		return !shown(name) && slices.ContainsFunc(th.Participants, func(pt threads.Participant) bool {
			return pt.Kind == threads.Agent && pt.Name == name
		})
	})
	if th, err = p.recordOf(who.Tenant, id, p.claimant(who, th.Project)); err != nil {
		return threads.Thread{}, err
	}
	counted := func(pt threads.Participant) bool { return pt.Kind != threads.Agent || shown(pt.Name) }
	return p.Threads.Participants(who.Tenant, id, th.Version, add, remove, counted, who.Subject, p.now())
}

// ErrStaysInProject reports a share that would move a conversation that has
// runs to another project, and ErrStaysInChannel one that would move it to
// another channel: its runs were filed where it is, and they stay read there.
var (
	ErrStaysInProject = errors.New("platform: a conversation that has runs stays in its project")
	ErrStaysInChannel = errors.New("platform: a conversation that has runs stays in its channel")
)

// ThreadShare shares a conversation: the project and channel it is shared
// in, and people to add to it by handle.
type ThreadShare struct {
	// Project is where it is shared: "" keeps the thread's own, "~" is the
	// workspace.
	Project string `json:"project,omitempty"`
	// Channel is the project's channel. "" keeps the thread's own in its own
	// project, and is general in another.
	Channel string `json:"channel,omitempty"`
	// Add names people by handle, as participants.
	Add []string `json:"add,omitempty"`
}

// threadLocks serialize starting a thread's runs (carryOut) with moving the
// thread (ShareThread), so a run is never filed in the project a thread has
// just left: a share reads whether the thread has runs, and moves it, while
// no run of it is being started. A stripe holds many threads.
type threadLocks [64]sync.RWMutex

func (l *threadLocks) of(tenant, id string) *sync.RWMutex {
	h := fnv.New32a()
	_, _ = h.Write([]byte(tenant + "\x00" + id))
	return &l[h.Sum32()%uint32(len(l))]
}

// ShareThread makes a conversation shared in a project's channel, read by
// everyone who sees that project, and adds the people it names. Only a
// participant shares. Making a private thread shared, or moving a thread,
// also takes what changing it takes (mayChangeThread: whoever started it,
// while a member, or an owner of its project), so someone tagged into a
// private conversation does not publish it; and it goes only into a project
// the sharer is a member of, to a channel that is not archived (placeFor).
// Sharing a thread that is shared already where it is only adds people, as
// SetParticipants does. It moves to another project or channel only while no
// run has been filed in it (ErrStaysInProject, ErrStaysInChannel). A default
// the thread names that cannot answer where it moves is cleared. Shared stays
// shared: nothing here makes a thread private again.
func (p *Platform) ShareThread(ctx context.Context, who auth.Principal, id string, in ThreadShare) (threads.Thread, error) {
	// Held from the first read to the write, so no run of the thread starts
	// meanwhile and a run starting is not a change the share trips over.
	mu := p.moves.of(who.Tenant, id)
	mu.Lock()
	defer mu.Unlock()
	th, err := p.ThreadRecord(who.Tenant, id)
	if err != nil {
		return threads.Thread{}, err
	}
	if err := p.changesThread(who, th); err != nil {
		return threads.Thread{}, err
	}
	if !takesPart(th, who.Subject) {
		return threads.Thread{}, fmt.Errorf("%w: only its participants share a conversation", projects.ErrForbidden)
	}
	if len(in.Add) > threads.MaxParticipants {
		return threads.Thread{}, fmt.Errorf("%w: at most %d people are added at once", threads.ErrInvalid, threads.MaxParticipants)
	}
	project, channel := th.Project, cmp.Or(in.Channel, th.Channel)
	if in.Project != "" && projects.NameOf(in.Project) != th.Project {
		project, channel = projects.NameOf(in.Project), cmp.Or(in.Channel, projects.General)
	}
	moves := project != th.Project || channel != th.Channel
	if moves || th.Visibility == threads.Private {
		if err := p.mayChangeThread(who, th); err != nil {
			return threads.Thread{}, err
		}
		got, ch, err := p.placeFor(who, project, channel)
		if err != nil {
			return threads.Thread{}, err
		}
		project, channel = got, ch.Name
		moves = project != th.Project || channel != th.Channel
	} else if err := p.MayPost(who, Place{Project: th.Project, Thread: th.ID}); err != nil {
		return threads.Thread{}, err
	}
	add := make([]threads.Participant, 0, len(in.Add))
	for _, handle := range in.Add {
		subject, found := p.byHandle(who.Tenant, handle)
		if !found {
			return threads.Thread{}, fmt.Errorf("%w: no one in this workspace goes by @%s", threads.ErrInvalid, handle)
		}
		add = append(add, threads.Participant{Kind: threads.Person, Name: subject})
	}
	if th, err = p.recordOf(who.Tenant, id, p.claimant(who, th.Project)); err != nil {
		return threads.Thread{}, err
	}
	def := th.Default
	if project != th.Project && def != "" && def != Auto && p.tagRefusal(who.Tenant, project, def) != "" {
		// It could not answer there. Clearing it says nothing of an agent
		// the sharer may not read.
		def = ""
	}
	shown := p.Seer(who).participantShownNow(th)
	counted := func(pt threads.Participant) bool { return pt.Kind != threads.Agent || shown(pt.Name) }
	if moves {
		if _, err := p.Runs.Conversation(who.Tenant, id); err == nil {
			if project != th.Project {
				return threads.Thread{}, ErrStaysInProject
			}
			return threads.Thread{}, ErrStaysInChannel
		}
	}
	return p.Threads.Share(who.Tenant, id, th.Version, project, channel, def, add, counted, who.Subject, p.now())
}

// byHandle is the subject of the person who goes by a handle.
func (p *Platform) byHandle(tenant, handle string) (string, bool) {
	if p.People == nil || handle == "" {
		return "", false
	}
	who, err := p.People.ByHandle(tenant, handle)
	return who.Subject, err == nil
}

// MaxTurnPage is how many turns one read of a thread returns.
const MaxTurnPage = 200

// ErrTurnCursor reports a cursor into a thread's turns that this server did
// not issue.
var ErrTurnCursor = errors.New("platform: invalid cursor")

func turnCursor(n int) string {
	return base64.RawURLEncoding.EncodeToString([]byte("turns:" + strconv.Itoa(n)))
}

func turnPosition(c string) (int, error) {
	raw, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil {
		return 0, ErrTurnCursor
	}
	n, err := strconv.Atoi(strings.TrimPrefix(string(raw), "turns:"))
	if err != nil || !strings.HasPrefix(string(raw), "turns:") || n < 0 {
		return 0, ErrTurnCursor
	}
	return n, nil
}

// ThreadTurns returns a thread's record and one page of its turns, oldest
// first: its people's messages and its agents' runs, in the order they were
// made. The first page is the newest; next is the cursor for the turns
// before it, "" when there are none. A turn is added only after every turn
// before it, so a cursor stays where it was.
func (p *Platform) ThreadTurns(tenant, id, before string, limit int) (threads.Thread, []ThreadTurn, string, error) {
	if limit <= 0 || limit > MaxTurnPage {
		limit = MaxTurnPage
	}
	th, err := p.ThreadRecord(tenant, id)
	if err != nil {
		return threads.Thread{}, nil, "", err
	}
	all := p.turnsOf(tenant, th)
	end := len(all)
	if before != "" {
		n, err := turnPosition(before)
		if err != nil || n > len(all) {
			return threads.Thread{}, nil, "", ErrTurnCursor
		}
		end = n
	}
	start := max(0, end-limit)
	next := ""
	if start > 0 {
		next = turnCursor(start)
	}
	return th, all[start:end], next, nil
}

// turnsOf is every turn of a thread, in the order they were made: each
// message by when it was posted, each run by when it was accepted, a
// message ahead of the runs that answer it. A thread started from an
// announcement starts with it, as it stands now.
func (p *Platform) turnsOf(tenant string, th threads.Thread) []ThreadTurn {
	var out []ThreadTurn
	if turns, err := p.Runs.Conversation(tenant, th.ID); err == nil {
		for _, r := range turns {
			out = append(out, ThreadTurn{Kind: TurnRun, Run: &r})
		}
	}
	if th.Version > 0 {
		for after := ""; ; {
			msgs, next, err := p.Threads.Messages(tenant, th.ID, after, threads.MaxPage)
			if err != nil {
				break
			}
			for _, m := range msgs {
				out = append(out, ThreadTurn{Kind: TurnMessage, Message: &m})
			}
			if next == "" {
				break
			}
			after = next
		}
	}
	slices.SortStableFunc(out, func(a, b ThreadTurn) int {
		at, bt := a.at(), b.at()
		if c := at.Compare(bt); c != 0 {
			return c
		}
		// A message comes ahead of a run made at the same moment, which
		// may answer it.
		return cmp.Compare(rank(a), rank(b))
	})
	if a, ok := p.seedOf(tenant, th); ok {
		out = append([]ThreadTurn{{Kind: TurnAnnouncement, Announcement: &a}}, out...)
	}
	if out == nil {
		out = []ThreadTurn{}
	}
	return out
}

// rank puts a message ahead of a run.
func rank(t ThreadTurn) int {
	if t.Kind == TurnMessage {
		return 0
	}
	return 1
}

func (t ThreadTurn) at() time.Time {
	switch {
	case t.Message != nil:
		return t.Message.At
	case t.Announcement != nil:
		return t.Announcement.At
	}
	return t.Run.CreatedAt
}

// ThreadPage returns one page of conversations, most recently active first,
// as the run index's query selects them: the run index's conversations, with
// what each one's record adds, and the threads people have started that no
// agent has answered yet. A conversation's channel and project are its
// record's when it has one.
func (p *Platform) ThreadPage(tenant string, q runs.ThreadQuery) ([]ThreadSummary, string, error) {
	return p.threadPage(tenant, q, nil)
}

// ThreadPageAs is ThreadPage holding only the conversations the reader may
// read: in a project they see, and in a private thread taking part. A page is
// full of those, so a hidden conversation never shortens one.
func (p *Platform) ThreadPageAs(who auth.Principal, q runs.ThreadQuery) ([]ThreadSummary, string, error) {
	seer := p.Seer(who)
	return p.threadPage(who.Tenant, q, func(s ThreadSummary) bool { return seer.Conversation(s.ID, s.Project) })
}

func (p *Platform) threadPage(tenant string, q runs.ThreadQuery, keep func(ThreadSummary) bool) ([]ThreadSummary, string, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = runs.DefaultThreadPage
	}
	limit = min(limit, runs.MaxThreadPage)
	var afterAt int64
	var afterID string
	if q.Before != "" {
		var err error
		if afterAt, afterID, err = decodeThreadCursor(q.Before); err != nil {
			return nil, "", err
		}
	}
	// A record says where its conversation is, so only the records the
	// query keeps are copied, and a conversation whose record is elsewhere
	// is left out.
	var in map[string]bool
	if q.Projects != nil {
		in = make(map[string]bool, len(q.Projects))
		for _, name := range q.Projects {
			in[name] = true
		}
	}
	recs, elsewhere := p.Threads.Summaries(tenant, func(th threads.Thread) bool {
		return (in == nil || in[th.Project]) && (q.Channel == "" || th.Channel == q.Channel)
	})
	byID := map[string]*ThreadSummary{}
	var order []*ThreadSummary
	for _, s := range p.Runs.Threads(tenant, 0) {
		if elsewhere[s.ID] {
			continue
		}
		sum := &ThreadSummary{ThreadSummary: s, Visibility: threads.Shared}
		byID[s.ID] = sum
		order = append(order, sum)
	}
	for _, rec := range recs {
		sum, ok := byID[rec.ID]
		if !ok {
			sum = &ThreadSummary{ThreadSummary: runs.ThreadSummary{
				ID: rec.ID, Title: rec.First, Origin: runs.OriginAPI, Agents: []string{}, People: []string{}, CreatedAt: rec.CreatedAt, UpdatedAt: rec.UpdatedAt,
			}}
			byID[rec.ID] = sum
			order = append(order, sum)
		}
		sum.Project, sum.Channel, sum.Visibility, sum.Messages = rec.Project, rec.Channel, rec.Visibility, rec.Messages
		if rec.Title != "" {
			sum.Title, sum.Named = rec.Title, true
		}
		if rec.UpdatedAt.After(sum.UpdatedAt) {
			sum.UpdatedAt = rec.UpdatedAt
		}
		for _, who := range rec.Participants {
			if who.Kind == threads.Person && !slices.Contains(sum.People, who.Name) {
				sum.People = append(sum.People, who.Name)
			}
		}
	}
	slices.SortFunc(order, func(a, b *ThreadSummary) int {
		return cmp.Or(b.UpdatedAt.Compare(a.UpdatedAt), strings.Compare(a.ID, b.ID))
	})
	page := []ThreadSummary{}
	for _, s := range order {
		switch {
		case in != nil && !in[s.Project]:
			continue
		case q.Origin != "" && s.Origin != q.Origin:
			continue
		case q.Agent != "" && !slices.Contains(s.Agents, q.Agent):
			continue
		case q.Standing == runs.StandingRunning && s.Active == 0:
			continue
		case q.Standing == runs.StandingFailed && s.Failed == 0:
			continue
		case q.Channel != "" && s.Channel != q.Channel:
			continue
		case keep != nil && !keep(*s):
			continue
		}
		if q.Before != "" {
			at := s.UpdatedAt.UnixNano()
			if at > afterAt || (at == afterAt && s.ID <= afterID) {
				continue
			}
		}
		if len(page) == limit {
			return page, encodeThreadCursor(page[len(page)-1]), nil
		}
		page = append(page, *s)
	}
	return page, "", nil
}

// A cursor into a list of conversations is the run index's: when the last
// one on a page was last active, and its id.
func encodeThreadCursor(s ThreadSummary) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(s.UpdatedAt.UnixNano(), 10) + ":" + s.ID))
}

func decodeThreadCursor(c string) (int64, string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil {
		return 0, "", runs.ErrCursor
	}
	stamp, id, ok := strings.Cut(string(raw), ":")
	at, err := strconv.ParseInt(stamp, 10, 64)
	if !ok || id == "" || err != nil {
		return 0, "", runs.ErrCursor
	}
	return at, id, nil
}

// WatchThreads reports what happens in a tenant's threads until cancel is
// called. The event stream tells each reader only of the threads they may
// read (Seer).
func (p *Platform) WatchThreads(tenant string) (<-chan threads.Notice, func()) {
	return p.Threads.Watch(tenant)
}
