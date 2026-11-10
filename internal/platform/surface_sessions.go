package platform

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/threads"
	"github.com/urmzd/orchagent/flow/session"
)

// A thread on a chat surface, such as a Slack thread, has one root session:
// the place's, keyed by its address (slack://<team>/<channel>/<thread ts>)
// and owned by the entry agent, like a console chat's. The chat service
// posts each person's message with the surface it came from and the person
// it acts for; the platform finds the place's root or starts it, files it
// where the Slack channel is bound (filedAt), and answers as it answers in
// the console: the one agent a message mentions answers it there, and a
// message naming nobody is the entry agent's.

// ErrSurfaceThreadBeforeSessions reports a surface thread that started as a
// conversation before root sessions, which stays one.
var ErrSurfaceThreadBeforeSessions = fmt.Errorf("%w: this thread started before sessions; post to it as a conversation, or start a new thread", threads.ErrExists)

// surfacePlace is the address of a surface thread's root session.
func surfacePlace(team, channel, ts string) string {
	return "slack://" + team + "/" + channel + "/" + ts
}

// StartSurfaceSession posts a person's message from a chat surface to that
// place's root session, starting the root when it is the thread's first
// message. started reports whether it did. Only the client's own machine may
// post, for a person of the client (act_as), and the request may not say
// where to file the session or who owns it.
func (p *Platform) StartSurfaceSession(ctx context.Context, who auth.Principal, in SessionInput) (out SessionView, started bool, err error) {
	if in.Surface == nil {
		return SessionView{}, false, fmt.Errorf("%w: a surface session needs its surface", threads.ErrInvalid)
	}
	ref := *in.Surface
	if in.Project != "" || in.Parent != "" || in.Agent != "" || in.Label != "" || in.Fresh || in.SourceAlert != "" {
		return SessionView{}, false, ErrSurfacePlace
	}
	if kind := strings.TrimSpace(in.SurfaceKind); kind != "" && kind != "slack" {
		return SessionView{}, false, fmt.Errorf("%w: surface kind %q is not one this server takes", threads.ErrInvalid, kind)
	}
	if err := threads.CheckText(in.Text); err != nil {
		return SessionView{}, false, err
	}
	c, err := p.surfaceHost(who, ref, in.ActAs)
	if err != nil {
		return SessionView{}, false, err
	}
	sf := ref.surface(c.ID)
	if err := sf.Validate(); err != nil {
		return SessionView{}, false, err
	}
	post := MessageInput{Text: in.Text, ActAs: in.ActAs, Surface: &ref, PostOnly: in.PostOnly}
	person, err := p.poster(who, in.ActAs)
	if err != nil {
		return SessionView{}, false, err
	}
	vis, people, aud, err := p.surfaceAudience(who, c, ref, person)
	if err != nil {
		return SessionView{}, false, err
	}
	if th, ok, err := p.Threads.BySurface(who.Tenant, sf); err != nil {
		return SessionView{}, false, err
	} else if ok {
		if th.Session == nil {
			return SessionView{}, false, ErrSurfaceThreadBeforeSessions
		}
		if vis == threads.Private && ref.Audience != nil && len(ref.Audience.Members) > 0 {
			if err := p.keepAudience(who.Tenant, th, people); err != nil {
				return SessionView{}, false, err
			}
		}
		if err := p.audiences.Put(who.Tenant, th.ID, aud); err != nil {
			return SessionView{}, false, err
		}
		out, err := p.surfacePost(ctx, who, th, post)
		return out, false, err
	}
	at, err := p.filedAt(who.Tenant, c, ref.Channel)
	if err != nil {
		return SessionView{}, false, err
	}
	owner := p.entryFor(ctx, who.Tenant, at.project, p.Seer(person).Agent)
	if owner == "" {
		// Without built-in agents the client's default owns the root, as it
		// answers the client's untagged messages.
		if owner = c.DefaultAgent; owner == "" || owner == Open {
			return SessionView{}, false, fmt.Errorf("%w: nobody answers this chat here: the server has no entry agent and %s names no default agent", ErrNoDefault, c.ID)
		}
	}
	rec, err := p.Sessions.Open(ctx, who.Tenant, session.Open{Parent: surfacePlace(c.Identity.Team, ref.Channel, ref.TS), Agent: owner, Channel: wire.Channel(who.Tenant)})
	if err != nil {
		return SessionView{}, false, err
	}
	th := threads.Thread{ID: rec.ID, Project: at.project, Channel: at.channel, Visibility: vis, Participants: people, Title: firstWords(in.Text, threads.MaxTitle), Default: owner, Agents: []string{owner}, CreatedBy: humanSubject(person), Surface: &sf, Session: &threads.SessionIdentity{Agent: owner, Parent: rec.Key.Parent, Label: rec.Key.Label}}
	_, err = p.Threads.Create(who.Tenant, th, p.now())
	started = err == nil
	if err != nil && !errors.Is(err, threads.ErrExists) {
		return SessionView{}, false, err
	}
	if err := p.audiences.Put(who.Tenant, rec.ID, aud); err != nil {
		return SessionView{}, false, err
	}
	if th, err = p.Threads.Get(who.Tenant, rec.ID); err != nil {
		return SessionView{}, false, err
	}
	out, err = p.surfacePost(ctx, who, th, post)
	return out, started, err
}

// SessionPosted is what one posted message did: the message, the run it
// started, the people it told, the
// mentions it refused, and why it started what it did or nothing. Repeated
// is a message already posted, which starts nothing again.
type SessionPosted struct {
	Message  SessionPostedMessage `json:"message"`
	Runs     []runs.Run           `json:"runs"`
	Notified []string             `json:"notified"`
	Refused  []threads.Mention    `json:"refused"`
	Rule     threads.Rule         `json:"rule"`
	Reason   string               `json:"reason,omitempty"`
	Repeated bool                 `json:"repeated,omitempty"`
}

// SessionPostedMessage is a posted message's id and its text as read.
type SessionPostedMessage struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

func sessionPosted(p Posted) SessionPosted {
	return SessionPosted{Message: SessionPostedMessage{ID: p.Message.ID, Text: p.Message.Text}, Runs: nonNil(p.Runs), Notified: nonNil(p.Notified), Refused: nonNil(p.Refused), Rule: p.Rule, Reason: p.Reason, Repeated: p.Repeated}
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// surfacePost posts a message to its root session th, where it gets one
// answer: its owner's, the one other agent's it mentions, or for several
// mentions the entry agent's, with what each of them said (carryOut).
func (p *Platform) surfacePost(ctx context.Context, who auth.Principal, th threads.Thread, in MessageInput) (SessionView, error) {
	text, reader, err := p.sessionReading(who, in)
	if err != nil {
		return SessionView{}, err
	}
	if _, _, err = p.sessionAgent(ctx, reader, th.Project, th.Session.Agent, text); err != nil {
		return SessionView{}, err
	}
	posted, err := p.PostMessage(ctx, who, th.ID, in)
	if err != nil {
		return SessionView{}, err
	}
	view, err := p.ReadSession(ctx, who, th.ID, "", "", false)
	sp := sessionPosted(posted)
	view.Posted = &sp
	return view, err
}

// postSurfaceChild posts a message a surface asked a child session for by
// option: into the root, starting nothing there, then into the named agent's
// child session, where it answers.
func (p *Platform) postSurfaceChild(ctx context.Context, who auth.Principal, root threads.Thread, in MessageInput, text, agent string, option SessionMessageOptions) (Posted, error) {
	only := in
	only.PostOnly = true
	inRoot, err := p.PostMessage(ctx, who, root.ID, only)
	if err != nil || inRoot.Repeated {
		return inRoot, err
	}
	posted, _, err := p.postSurfaceChildOf(ctx, who, root, in, text, agent, option)
	if err != nil {
		return Posted{}, err
	}
	inRoot.Notified = append(inRoot.Notified, posted.Notified...)
	inRoot.Rule, inRoot.Reason, inRoot.Runs = posted.Rule, posted.Reason, posted.Runs
	return inRoot, nil
}

// postSurfaceChildOf posts text into agent's child session under root
// (continued, or fresh or labelled as asked), as the person it is posted
// for, where the agent answers it, and returns the child's id.
func (p *Platform) postSurfaceChildOf(ctx context.Context, who auth.Principal, root threads.Thread, in MessageInput, text, agent string, option SessionMessageOptions) (Posted, string, error) {
	rec, err := p.Sessions.Open(ctx, who.Tenant, session.Open{Parent: root.ID, Agent: agent, Label: option.Label, Fresh: option.Fresh, Channel: wire.Channel(who.Tenant)})
	if err != nil {
		return Posted{}, "", err
	}
	child := threads.Thread{ID: rec.ID, Project: root.Project, Channel: root.Channel, Visibility: root.Visibility, CreatedBy: root.CreatedBy, Participants: slices.Clone(root.Participants), Default: agent, Agents: []string{agent}, Title: firstWords(text, threads.MaxTitle), Session: &threads.SessionIdentity{Agent: agent, Parent: root.ID, Label: rec.Key.Label, SourceAlert: root.Session.SourceAlert}}
	if _, err := p.Threads.Create(who.Tenant, child, p.now()); err != nil && !errors.Is(err, threads.ErrExists) {
		return Posted{}, "", err
	}
	posted, err := p.PostMessage(ctx, who, rec.ID, MessageInput{Text: text, ActAs: in.ActAs})
	return posted, rec.ID, err
}

// Slack conversation kinds (SurfaceAudience.Type).
const (
	ChannelPublic  = "public_channel"
	ChannelPrivate = "private_channel"
	ChannelGroupDM = "mpim"
	ChannelDM      = "im"
)

// maxSurfaceMembers bounds the members one message may name.
const maxSurfaceMembers = 500

// SessionAudience is who a surface session was asked in front of, as the
// place said with its latest message: Kind public, private, group or
// direct; Members the people the client maps those members to; Unmapped the
// members it maps to nobody. Knowledge is gated on it at answer time.
type SessionAudience struct {
	Kind     string    `json:"kind"`
	Members  []string  `json:"members,omitempty"`
	Unmapped int       `json:"unmapped,omitempty"`
	At       time.Time `json:"at"`
}

// audienceStore keeps the latest audience of each surface root session.
func audienceStore(st state.Store) *state.Table[SessionAudience] {
	if st == nil {
		st = state.NewMemory()
	}
	return state.NewTable[SessionAudience](st, "session-audiences")
}

// SessionAudience is the latest audience of session's root, the place it
// belongs to. It is false for a session no surface reported one for.
func (p *Platform) SessionAudience(tenant, session string) (SessionAudience, bool) {
	id := session
	for range 64 {
		th, err := p.Threads.Get(tenant, id)
		if err != nil || th.Session == nil {
			return SessionAudience{}, false
		}
		if !strings.HasPrefix(th.Session.Parent, "s_") {
			break
		}
		id = th.Session.Parent
	}
	it, err := p.audiences.Lookup(tenant, id)
	if err != nil {
		return SessionAudience{}, false
	}
	return it.Value, true
}

// surfaceAudience is who a surface thread's root session is for, by the
// place it was asked in: a public channel's is
// shared where it is filed; a private channel's or a group direct message's
// is private to the conversation's members now, those the client maps to a
// person; a direct message's is private to its person. The entry agent and
// the children it hands work to inherit it, and a child is never read by
// anyone who cannot read its root (SeesSession). aud is what is kept for the
// knowledge gate.
func (p *Platform) surfaceAudience(_ auth.Principal, c clients.Client, ref SurfaceRef, person auth.Principal) (threads.Visibility, []threads.Participant, SessionAudience, error) {
	return p.audienceOf(c, ref, person)
}

// audienceOf is surfaceAudience for a conversation on client c, mapping its
// members to people through the client.
func (p *Platform) audienceOf(c clients.Client, ref SurfaceRef, person auth.Principal) (vis threads.Visibility, people []threads.Participant, aud SessionAudience, err error) {
	kind, members := "", []string(nil)
	if ref.Audience != nil {
		kind, members = ref.Audience.Type, ref.Audience.Members
	}
	if kind == "" {
		// Nothing reported: a direct message by its id, and anything else
		// private to the person asking. A channel id does not say whether
		// the channel is public, so it is never assumed to be.
		kind = ChannelPrivate
		if directMessage(ref.Channel) {
			kind = ChannelDM
		}
	}
	if len(members) > maxSurfaceMembers {
		return "", nil, SessionAudience{}, fmt.Errorf("%w: an audience names at most %d members", threads.ErrInvalid, maxSurfaceMembers)
	}
	at := p.now()
	self := humanSubject(person)
	owner := threads.Participant{Kind: threads.Person, Name: self, Owner: true, At: at}
	aud = SessionAudience{At: at}
	switch kind {
	case ChannelPublic:
		aud.Kind = "public"
		return threads.Shared, nil, aud, nil
	case ChannelDM:
		aud.Kind, aud.Members = "direct", []string{self}
		return threads.Private, []threads.Participant{owner}, aud, nil
	case ChannelPrivate, ChannelGroupDM:
		aud.Kind = "private"
		if kind == ChannelGroupDM {
			aud.Kind = "group"
		}
		people = []threads.Participant{owner}
		aud.Members = []string{self}
		machine := ""
		if c.Slack != nil {
			machine = c.Slack.Machine
		}
		for _, u := range members {
			s, ok := c.ActsFor(machine, u)
			if !ok || s == "" {
				aud.Unmapped++ // reads nothing here, and the knowledge gate fails closed on it
				continue
			}
			if !slices.Contains(aud.Members, s) {
				aud.Members = append(aud.Members, s)
				if len(people) < threads.MaxParticipants {
					people = append(people, threads.Participant{Kind: threads.Person, Name: s, AddedBy: self, At: at})
				}
			}
		}
		return threads.Private, people, aud, nil
	}
	return "", nil, SessionAudience{}, fmt.Errorf("%w: audience.type is public_channel, private_channel, mpim or im", threads.ErrInvalid)
}

// keepAudience makes a private root's people the conversation's members
// now: members who joined are added, people who left are removed. Its
// agents, and the person who started it, stay.
func (p *Platform) keepAudience(tenant string, th threads.Thread, people []threads.Participant) error {
	want := map[string]bool{}
	for _, pt := range people {
		want[pt.Name] = true
	}
	var add []threads.Participant
	for _, pt := range people {
		if !slices.ContainsFunc(th.Participants, func(have threads.Participant) bool { return have.Kind == threads.Person && have.Name == pt.Name }) {
			add = append(add, pt)
		}
	}
	var remove []string
	for _, have := range th.Participants {
		if have.Kind == threads.Person && !have.Owner && !want[have.Name] {
			remove = append(remove, have.Name)
		}
	}
	if len(add) == 0 && len(remove) == 0 {
		return nil
	}
	_, err := p.Threads.Participants(tenant, th.ID, th.Version, add, remove, func(threads.Participant) bool { return true }, "surface", p.now())
	return err
}

// sessionReading is the text of a session message as mentions are read
// from it, and whom they are read as: a surface's message in that surface's
// grammar, as the person it is posted for; any other as posted, as who.
func (p *Platform) sessionReading(who auth.Principal, in MessageInput) (string, auth.Principal, error) {
	reader := who
	if in.ActAs != nil {
		person, err := p.poster(who, in.ActAs)
		if err != nil {
			return "", auth.Principal{}, err
		}
		reader = person
	}
	if in.Surface == nil {
		return in.Text, reader, nil
	}
	c, err := p.surfaceHost(who, *in.Surface, in.ActAs)
	if err != nil {
		return "", auth.Principal{}, err
	}
	text, _ := p.surfaceText(who.Tenant, c, *in.Surface, in.Text)
	return text, reader, nil
}
