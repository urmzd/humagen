package platform

import (
	"cmp"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/urmzd/fiber/pkg/envelope"

	"github.com/urmzd/warp/pkg/auth"
	"github.com/urmzd/warp/pkg/inbox"
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
// tags only people notifies them; an answer, a reply or a message posted
// only starts nothing; otherwise the default the thread inherits answers.
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
	Default  string `json:"default,omitempty"`
	Text     string `json:"text"`
	PostOnly bool   `json:"post_only,omitempty"`
	// ActAs is the person a surface's credential posts for.
	ActAs *ActAs `json:"act_as,omitempty"`
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
	// reply is a reply to a mention, from the inbox.
	reply bool
}

// ThreadUpdate changes a thread's title, visibility or default, as a whole.
type ThreadUpdate struct {
	// Version is the one read. A conversation with no record reads 0.
	Version    int                `json:"version"`
	Title      string             `json:"title"`
	Visibility threads.Visibility `json:"visibility"`
	Default    string             `json:"default"`
}

// ParticipantsUpdate adds and removes people and agents.
type ParticipantsUpdate struct {
	Version int `json:"version"`
	// Add names people by subject and agents by name, as participants.
	Add []threads.Participant `json:"add,omitempty"`
	// Remove names them by subject or name.
	Remove []string `json:"remove,omitempty"`
}

// Posted is what a message did.
type Posted struct {
	Thread  threads.Thread  `json:"thread"`
	Message threads.Message `json:"message"`
	// Runs are the runs it started, at most threads.MaxRuns.
	Runs []runs.Run `json:"runs"`
	// Notified is who it was sent to: people by subject, experts by subject
	// or role.
	Notified []string `json:"notified"`
	// Refused are its tags that started or notified nothing, each saying
	// why.
	Refused []threads.Mention `json:"refused"`
	// Rule is the rule that decided what it starts, and Reason why no agent
	// answers when none does.
	Rule   threads.Rule `json:"rule"`
	Reason string       `json:"reason,omitempty"`
}

// ThreadTurn is one turn of a thread: a person's message or an agent's run.
type ThreadTurn struct {
	// Kind is message or run.
	Kind    string           `json:"kind"`
	Message *threads.Message `json:"message,omitempty"`
	Run     *runs.Run        `json:"run,omitempty"`
}

// Turn kinds.
const (
	TurnMessage = "message"
	TurnRun     = "run"
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
		return who.Subject, cmp.Or(who.Name, who.Subject), threads.ViaConsole, nil, nil
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

// nameOf is what a person is called: their name in the directory, or their
// subject.
func (p *Platform) nameOf(tenant, subject string) string {
	if p.People != nil {
		if who, err := p.People.Get(tenant, subject); err == nil && who.Name != "" {
			return who.Name
		}
	}
	return subject
}

// tagLookup resolves the names a message tags, within one tenant.
type tagLookup struct {
	ctx    context.Context
	p      *Platform
	tenant string
	slack  map[string]string
}

func (l tagLookup) Agent(name string) bool {
	_, err := l.p.Agents.Get(l.ctx, l.tenant, name, 0)
	return err == nil
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
	ch, _ := p.Projects.Channel(tenant, th.Project, th.Channel)
	at := Place{Project: th.Project}
	for _, d := range threads.Nearer(th, "", ch) {
		at.Nearer = append(at.Nearer, Nearer{Agent: d.Agent, From: d.From})
	}
	return at
}

// defaultOf is the default a message in a thread inherits, and why it
// cannot answer when it has stopped being available there.
func (p *Platform) defaultOf(tenant string, th threads.Thread) (threads.Default, string) {
	n := p.DefaultAt(tenant, p.placeOfThread(tenant, th))
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
	d, unavailable = p.defaultOf(tenant, th)
	return d, unavailable, nil
}

// ThreadRecord returns a thread's record. A conversation from before
// threads has none, and reads as shared, in general of its project, with
// the people who asked in it and the agents who answered as its
// participants, at version 0. Its record is written the first time anything
// about it changes.
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
		CreatedBy: first.Actor.ID, CreatedAt: first.CreatedAt, UpdatedAt: first.UpdatedAt,
	}
	add := func(kind, name string) {
		if name == "" || len(th.Participants) >= threads.MaxParticipants {
			return
		}
		if slices.ContainsFunc(th.Participants, func(x threads.Participant) bool { return x.Kind == kind && x.Name == name }) {
			return
		}
		th.Participants = append(th.Participants, threads.Participant{Kind: kind, Name: name, Owner: kind == threads.Person && name == first.Actor.ID})
	}
	for _, r := range turns {
		if !r.Actor.Bot {
			add(threads.Person, r.Actor.ID)
		}
		add(threads.Agent, r.Agent)
		if r.UpdatedAt.After(th.UpdatedAt) {
			th.UpdatedAt = r.UpdatedAt
		}
	}
	return th, nil
}

// recordOf returns a thread's stored record, writing the one a
// conversation from before threads reads as the first time it is needed.
// by is who the change is made by, the record's owner when the
// conversation's first turn names nobody.
func (p *Platform) recordOf(tenant, id, by string) (threads.Thread, error) {
	th, err := p.ThreadRecord(tenant, id)
	if err != nil || th.Version > 0 {
		return th, err
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

// placeFor checks where a new thread is started: a project of the tenant and
// one of its channels that is not archived.
func (p *Platform) placeFor(tenant, project, channel string) (string, projects.Channel, error) {
	project = projects.NameOf(project)
	if !p.HasProject(tenant, project) {
		return "", projects.Channel{}, fmt.Errorf("%w: %q", projects.ErrNotFound, project)
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
// that is neither auto nor an agent available in the project.
func (p *Platform) checkDefault(ctx context.Context, tenant, project, agent string) error {
	if agent == "" || agent == threads.AutoName {
		return nil
	}
	if !projects.ValidDefault(agent) {
		return fmt.Errorf("%w: a default is an agent's name or auto", projects.ErrInvalid)
	}
	if _, err := p.Agents.Get(ctx, tenant, agent, 0); err != nil {
		return fmt.Errorf("%w: there is no agent called %s", projects.ErrInvalid, agent)
	}
	if reason := p.tagRefusal(tenant, project, agent); reason != "" {
		return fmt.Errorf("%w: %s, so it cannot answer there", projects.ErrInvalid, reason)
	}
	return nil
}

// StartThread starts a shared thread in a project's channel with its first
// message, and starts what the message calls for.
func (p *Platform) StartThread(ctx context.Context, who auth.Principal, in ThreadInput) (Posted, error) {
	by, name, via, slack, err := p.author(who, in.ActAs)
	if err != nil {
		return Posted{}, err
	}
	switch in.Visibility {
	case threads.Shared, threads.Private:
	default:
		return Posted{}, fmt.Errorf("%w: a thread is shared or private", threads.ErrInvalid)
	}
	project, ch, err := p.placeFor(who.Tenant, in.Project, in.Channel)
	if err != nil {
		return Posted{}, err
	}
	if err := p.checkDefault(ctx, who.Tenant, project, in.Default); err != nil {
		return Posted{}, err
	}
	tree := p.Projects.Tree(who.Tenant)
	mentions, err := threads.Parse(in.Text, tagLookup{ctx: ctx, p: p, tenant: who.Tenant, slack: slack})
	if err != nil {
		return Posted{}, err
	}
	mentions = p.checkTags(who.Tenant, tree, project, mentions)
	th, m, err := p.Threads.Start(who.Tenant, threads.Thread{
		Project: project, Channel: ch.Name, Title: strings.TrimSpace(in.Title), Visibility: in.Visibility, Default: in.Default, CreatedBy: by,
	}, threads.Message{By: by, Name: name, Text: in.Text, Mentions: mentions, Via: via}, p.now())
	if err != nil {
		return Posted{}, err
	}
	return p.carryOut(ctx, who.Tenant, tree, th, m, in.PostOnly, false)
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
	by, name, via, slack, err := p.author(who, in.ActAs)
	if err != nil {
		return Posted{}, err
	}
	prev, err := p.prevOf(who.Tenant, th, in.After)
	if err != nil {
		return Posted{}, err
	}
	tree := p.Projects.Tree(who.Tenant)
	mentions, err := threads.Parse(in.Text, tagLookup{ctx: ctx, p: p, tenant: who.Tenant, slack: slack})
	if err != nil {
		return Posted{}, err
	}
	mentions = p.checkTags(who.Tenant, tree, th.Project, mentions)
	msg := threads.Message{By: by, Name: name, Text: in.Text, Mentions: mentions, Via: via, Prev: prev}
	if in.Answers != nil {
		if err := p.answerFromThread(ctx, who, th, *in.Answers, in.Text); err != nil {
			return Posted{}, err
		}
		a := *in.Answers
		msg.Answers = &a
	}
	if th, err = p.recordOf(who.Tenant, id, by); err != nil {
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
		return p.posted(who.Tenant, th, m), nil
	}
	return p.carryOut(ctx, who.Tenant, tree, th, m, in.PostOnly, in.Answers != nil || in.reply)
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
// path.
func (p *Platform) answerFromThread(ctx context.Context, who auth.Principal, th threads.Thread, ref threads.AnswerRef, text string) error {
	run, err := p.Runs.Get(who.Tenant, ref.Run)
	if err != nil || run.Thread != th.ID {
		return fmt.Errorf("%w: no run of this thread asked that", threads.ErrInvalid)
	}
	m, ok := p.Inbox.ByKey(who.Tenant, questionKey(ref.Run, ref.Question))
	if !ok || len(m.Parts) == 0 {
		return fmt.Errorf("%w: no run of this thread asked that", threads.ErrInvalid)
	}
	a := QuestionAnswer{Text: strings.TrimSpace(text)}
	switch m.Parts[0].Type {
	case inbox.PartReply:
		a.Action = ActReply
	case inbox.PartChoice:
		a.Action, a.Choice = ActChoose, a.Text
	default:
		return fmt.Errorf("%w: an approval is decided with Approve or Deny, not a message", threads.ErrInvalid)
	}
	_, err = p.AnswerQuestion(ctx, who, ref.Run, ref.Question, a)
	return err
}

// carryOut starts what a posted message calls for, records what each of
// its tags came to, and notifies the people and experts it tags.
func (p *Platform) carryOut(ctx context.Context, tenant string, tree *projects.Tree, th threads.Thread, m threads.Message, postOnly, reply bool) (Posted, error) {
	def, unavailable := p.defaultOf(tenant, th)
	d := threads.Decide(threads.Input{Mentions: m.Mentions, Reply: reply, PostOnly: postOnly, Default: def, Unavailable: unavailable})
	out := Posted{Runs: []runs.Run{}, Notified: []string{}, Refused: []threads.Mention{}, Rule: d.Rule, Reason: d.Reason}
	mentions := d.Mentions
	actor := envelope.Actor{ID: m.By, Display: m.Name}
	var answered []threads.Participant
	start := func(agent string) (runs.Run, error) {
		run, err := p.answerMessage(ctx, tenant, th, m, agent, actor)
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

// answerMessage starts an agent answering a person's message: @auto picks
// the agent first. The agent is sent its context and the room's transcript
// up to the message, then the message.
func (p *Platform) answerMessage(ctx context.Context, tenant string, th threads.Thread, m threads.Message, agent string, actor envelope.Actor) (runs.Run, error) {
	meta := map[string]string{
		runs.MetaAsk: m.Text, runs.MetaThread: th.ID, runs.MetaPrev: m.ID, runs.MetaMessage: m.ID,
		runs.MetaChannel: th.Channel, runs.MetaProject: th.Project,
	}
	tagID := answerTagID(th.ID, m.ID, agent)
	if agent == Auto {
		who, err := p.resolveAgent(ctx, tenant, p.placeOfThread(tenant, th), Auto, m.Text)
		if err != nil {
			return runs.Run{}, err
		}
		agent = who.Agent
		meta[runs.MetaPicked] = who.Picked
	}
	cfg := p.Settings(tenant)
	body := m.Text
	if cfg.History == HistoryRecent {
		if before := p.roomBefore(tenant, th, m); len(before) > 0 {
			body = threads.Transcript(before, cfg.HistoryLimit) + "\n\n" + cmp.Or(m.Name, m.By) + ": " + m.Text
		}
	}
	return p.Invoke(ctx, envelope.Tag{
		ID: tagID, Tenant: tenant, Agent: agent, Origin: runs.OriginAPI,
		Text: p.contextIn(tenant, th.Project, th.Channel, agent, body), Actor: actor, Meta: meta,
	})
}

// roomBefore is the thread as it stands before a message, on the message's
// branch: the messages leading to it, each followed by the runs that answer
// it, and ahead of them the turns of the conversation from before threads
// that the first message follows.
func (p *Platform) roomBefore(tenant string, th threads.Thread, m threads.Message) []threads.Turn {
	path, err := p.Threads.Path(tenant, th.ID, m.ID)
	if err != nil {
		return nil
	}
	answers := map[string][]runs.Run{}
	if turns, err := p.Runs.Conversation(tenant, th.ID); err == nil {
		for _, r := range turns {
			if r.Message != "" {
				answers[r.Message] = append(answers[r.Message], r)
			}
		}
	}
	var out []threads.Turn
	if first := path[0]; first.Prev != "" && !strings.HasPrefix(first.Prev, "m-") {
		if legacy, err := p.Runs.Path(tenant, first.Prev); err == nil {
			for _, r := range legacy {
				out = append(out, threads.RunTurn(r))
			}
		}
	}
	for _, msg := range path[:len(path)-1] {
		out = append(out, threads.MessageTurn(msg))
		for _, r := range answers[msg.ID] {
			out = append(out, threads.RunTurn(r))
		}
	}
	return out
}

// notifyTagged sends the people and experts a message tags a mention in
// their inbox, once each, and returns who was sent one. The poster is not
// sent their own.
func (p *Platform) notifyTagged(tenant string, tree *projects.Tree, th threads.Thread, m threads.Message, mentions []threads.Mention) []string {
	sent := []string{}
	for _, mn := range mentions {
		if mn.Refused != "" {
			continue
		}
		var to []string
		switch mn.Kind {
		case threads.Person:
			to = []string{mn.Name}
		case threads.SME:
			to, _ = tree.ExpertsFor(th.Project, mn.Name)
		}
		for _, who := range to {
			if who == m.By || slices.Contains(sent, who) {
				continue
			}
			// In a private thread, tagging a person invites them: they
			// take part from now on, so they can read what they were
			// tagged in.
			if th.Visibility == threads.Private && !strings.HasPrefix(who, projects.RolePrefix) {
				if _, err := p.Threads.Join(tenant, th.ID, []threads.Participant{{Kind: threads.Person, Name: who, AddedBy: m.By}}, p.now()); err != nil {
					p.log.Warn("platform: a person tagged in a private thread could not be invited", "tenant", tenant, "thread", th.ID, "error", err)
					continue
				}
			}
			p.raiseMention(tenant, th, m, who, mn)
			sent = append(sent, who)
		}
	}
	return sent
}

// raiseMention puts a mention in the inbox of the person or experts tagged.
func (p *Platform) raiseMention(tenant string, th threads.Thread, m threads.Message, to string, mn threads.Mention) {
	where := "#" + th.Channel
	subject := fmt.Sprintf("%s mentioned you in %s", cmp.Or(m.Name, m.By), where)
	if mn.Kind == threads.SME {
		subject = fmt.Sprintf("%s asked the experts in %s", cmp.Or(m.Name, m.By), where)
		if mn.Name != "" {
			subject = fmt.Sprintf("%s asked the %s experts in %s", cmp.Or(m.Name, m.By), mn.Name, where)
		}
	}
	msg := inbox.Message{
		Kind: inbox.KindMention, Subject: subject, From: inbox.From{Kind: "person", Name: cmp.Or(m.Name, m.By)}, Severity: inbox.Info,
		Source: inbox.Source{Thread: th.ID, Project: th.Project},
		Parts: []inbox.Part{{Type: inbox.PartMention, Mention: &inbox.MentionPart{
			Thread: th.ID, Message: m.ID, Project: th.Project, Channel: th.Channel, By: m.By, Excerpt: inbox.Excerpt(m.Text),
		}}},
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
// as a human message that starts no run.
func (p *Platform) replyToMention(ctx context.Context, who auth.Principal, part inbox.MentionPart, text string) (threads.Message, error) {
	if strings.TrimSpace(text) == "" {
		return threads.Message{}, fmt.Errorf("%w: a reply says something", threads.ErrInvalid)
	}
	posted, err := p.PostMessage(ctx, who, part.Thread, MessageInput{Text: text, After: part.Message, reply: true})
	return posted.Message, err
}

// UpdateThread changes a thread's title, visibility or default. A private
// thread is refused until reads are confined, and a default must be able to
// answer in the thread's project.
func (p *Platform) UpdateThread(ctx context.Context, who auth.Principal, id string, in ThreadUpdate) (threads.Thread, error) {
	th, err := p.ThreadRecord(who.Tenant, id)
	if err != nil {
		return threads.Thread{}, err
	}
	if in.Version != th.Version {
		return threads.Thread{}, fmt.Errorf("%w: %q is at version %d, not %d", threads.ErrVersion, id, th.Version, in.Version)
	}
	if err := p.checkDefault(ctx, who.Tenant, th.Project, in.Default); err != nil {
		return threads.Thread{}, err
	}
	if th, err = p.recordOf(who.Tenant, id, who.Subject); err != nil {
		return threads.Thread{}, err
	}
	return p.Threads.Update(who.Tenant, id, th.Version, p.now(), func(t *threads.Thread) error {
		t.Title, t.Visibility, t.Default = strings.TrimSpace(in.Title), cmp.Or(in.Visibility, t.Visibility), in.Default
		return nil
	})
}

// SetParticipants adds and removes a thread's people and agents. An agent
// added must be available in the thread's project, and a person must be in
// the workspace's directory.
func (p *Platform) SetParticipants(ctx context.Context, who auth.Principal, id string, in ParticipantsUpdate) (threads.Thread, error) {
	th, err := p.ThreadRecord(who.Tenant, id)
	if err != nil {
		return threads.Thread{}, err
	}
	if in.Version != th.Version {
		return threads.Thread{}, fmt.Errorf("%w: %q is at version %d, not %d", threads.ErrVersion, id, th.Version, in.Version)
	}
	if len(in.Add)+len(in.Remove) > threads.MaxParticipants {
		return threads.Thread{}, fmt.Errorf("%w: at most %d people and agents are added or removed at once", threads.ErrInvalid, threads.MaxParticipants)
	}
	for _, a := range in.Add {
		switch a.Kind {
		case threads.Agent:
			if _, err := p.Agents.Get(ctx, who.Tenant, a.Name, 0); err != nil {
				return threads.Thread{}, fmt.Errorf("%w: there is no agent called %s", threads.ErrInvalid, a.Name)
			}
			if reason := p.tagRefusal(who.Tenant, th.Project, a.Name); reason != "" {
				return threads.Thread{}, fmt.Errorf("%w: %s", threads.ErrInvalid, reason)
			}
		case threads.Person:
			if p.People == nil {
				return threads.Thread{}, fmt.Errorf("%w: no one in this workspace is %s", threads.ErrInvalid, a.Name)
			}
			if _, err := p.People.Get(who.Tenant, a.Name); err != nil {
				return threads.Thread{}, fmt.Errorf("%w: no one in this workspace is %s", threads.ErrInvalid, a.Name)
			}
		}
	}
	if th, err = p.recordOf(who.Tenant, id, who.Subject); err != nil {
		return threads.Thread{}, err
	}
	return p.Threads.Participants(who.Tenant, id, th.Version, in.Add, in.Remove, who.Subject, p.now())
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
// message ahead of the runs that answer it.
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
	if t.Message != nil {
		return t.Message.At
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
	return p.threadPage(who.Tenant, q, func(s ThreadSummary) bool { return seer.Place(Place{Project: s.Project, Thread: s.ID}) })
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
	byID := map[string]*ThreadSummary{}
	var order []*ThreadSummary
	for _, s := range p.Runs.Threads(tenant, 0) {
		sum := &ThreadSummary{ThreadSummary: s, Visibility: threads.Shared}
		byID[s.ID] = sum
		order = append(order, sum)
	}
	for _, rec := range p.Threads.Summaries(tenant) {
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
		case q.Projects != nil && !slices.Contains(q.Projects, s.Project):
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
// called. Every thread is shared until reads are confined, so whoever may
// read conversations may be told of each.
func (p *Platform) WatchThreads(tenant string) (<-chan threads.Notice, func()) {
	return p.Threads.Watch(tenant)
}
