package platform

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/inbox"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/threads"
)

// A thread on a client's surface, such as a Slack thread, is one thread here.
// The client's machine posts each person's message there with the surface it
// came from, and the platform finds the thread by that surface or starts it.
// Where it is filed is the platform's to decide, never the request's: in the
// channel the Slack channel is bound to, shared; or, outside a bound channel,
// in the client's home, private for a direct message or the assistant pane
// and shared otherwise. The message is Slack's own text, which the platform
// reads in the one grammar (slackText).
//
// A Slack thread under an announcement the client posted is that
// announcement's while the Slack channel stays bound to the announcement's
// project: the thread it starts is seeded with it (announcementAt), so its
// turns start with the announcement, from whoever posted it, and then the
// reply, as its poster's own message.

// SurfaceRef is where a message was posted on a client's surface.
type SurfaceRef struct {
	// Files asks the platform to ingest files from this verified Slack message.
	Files bool `json:"files,omitempty"`
	// Client is the client whose surface it is.
	Client string `json:"client"`
	// Channel is the Slack channel, or for a direct message the conversation.
	Channel string `json:"channel"`
	// TS is the thread's: the ts of its first message.
	TS string `json:"ts"`
	// Message is the message's own ts, which makes posting it again change
	// nothing. Empty is TS: the message that starts the thread.
	Message string `json:"message,omitempty"`
	// AppUser is the app's own bot user id, whose mention is how a channel
	// message reaches the app and is not a tag.
	AppUser string `json:"app_user,omitempty"`
}

func (r SurfaceRef) surface(client string) threads.Surface {
	return threads.Surface{Client: client, Channel: r.Channel, TS: r.TS}
}

func (r SurfaceRef) message() string { return cmp.Or(r.Message, r.TS) }

// ErrSurfacePlace is a surface's message that names where to file its thread.
var ErrSurfacePlace = fmt.Errorf("%w: a surface's thread is filed where its channel is bound, never where the request says", threads.ErrInvalid)

// ErrAnnouncementGone reports a reply to an announcement's post on a
// client's surface that starts nothing: the announcement was withdrawn, or
// it is a draft, which never reaches a channel.
var ErrAnnouncementGone = errors.New("platform: a reply to this announcement starts nothing")

// directMessage reports a Slack conversation with one person: a direct
// message or the assistant pane, whose ids start with D.
func directMessage(channel string) bool { return strings.HasPrefix(channel, "D") }

// surfaceHost returns the client a surface's message came through, which
// must name the caller as its machine. To anyone else it is not found.
func (p *Platform) surfaceHost(who auth.Principal, ref SurfaceRef, as *ActAs) (clients.Client, error) {
	c, err := p.Clients.Get(who.Tenant, ref.Client)
	if err != nil || !machineOf(c, who) {
		return clients.Client{}, fmt.Errorf("%w: %q", clients.ErrNotFound, ref.Client)
	}
	if as == nil || as.Client != c.ID {
		return clients.Client{}, fmt.Errorf("%w: a surface's message is posted for a person of its client (act_as)", threads.ErrInvalid)
	}
	return c, nil
}

// filing is where a thread a client starts on its surface is filed, and
// whether the Slack channel's binding filed it there.
type filing struct {
	project, channel string
	vis              threads.Visibility
	bound            bool
}

// filedAt is where a thread a client starts on its surface is filed.
func (p *Platform) filedAt(tenant string, c clients.Client, channel string) (filing, error) {
	if ch, bound := p.Projects.BoundTo(tenant, c.ID, channel); bound {
		return filing{project: ch.Project, channel: ch.Name, vis: threads.Shared, bound: true}, nil
	}
	if resource, ok := slackResource(channel); ok {
		if project, target, linked := p.linkedClientResource(c, resource); linked {
			return filing{project: project, channel: target, vis: threads.Shared, bound: true}, nil
		}
	}
	h := c.HomeOf()
	vis := threads.Shared
	if directMessage(channel) {
		vis = threads.Private
	}
	if _, err := p.Projects.Channel(tenant, h.Project, h.Channel); err != nil {
		where := h.Project
		if where == projects.Root {
			where = "the workspace"
		}
		return filing{}, fmt.Errorf("%w: %s's home, #%s in %s, is gone; give it another", projects.ErrNotFound, c.ID, h.Channel, where)
	}
	return filing{project: h.Project, channel: h.Channel, vis: vis}, nil
}

// surfaceText reads a Slack message in the one grammar, and says which of
// its mentions tagged nobody.
func (p *Platform) surfaceText(tenant string, c clients.Client, ref SurfaceRef, text string) (string, []threads.Mention) {
	if c.Surface != clients.Slack {
		return text, nil
	}
	cx := slackContext{AppUser: ref.AppUser, Bots: c.Bots, HandleOf: func(subject string) (string, bool) {
		if p.People == nil {
			return "", false
		}
		who, err := p.People.Get(tenant, subject)
		return who.Handle, err == nil && who.Handle != ""
	}}
	if c.Slack != nil {
		cx.People = c.Slack.People
	}
	out, refused := slackText(text, cx)
	var tags []threads.Mention
	for _, r := range refused {
		tags = append(tags, threads.Mention{Kind: threads.Person, Name: r.User, Refused: r.Reason})
	}
	return out, tags
}

// startFromSurface posts a person's message from a client's surface: into
// the thread that surface already is, or into a new one filed from the
// binding.
func (p *Platform) startFromSurface(ctx context.Context, who auth.Principal, in ThreadInput) (Posted, error) {
	ref := *in.Surface
	c, err := p.surfaceHost(who, ref, in.ActAs)
	if err != nil {
		return Posted{}, err
	}
	if in.Project != "" || in.Channel != "" || in.Visibility != "" || in.Default != "" {
		return Posted{}, ErrSurfacePlace
	}
	sf := ref.surface(c.ID)
	if err := sf.Validate(); err != nil {
		return Posted{}, err
	}
	post := MessageInput{Text: in.Text, PostOnly: in.PostOnly, ActAs: in.ActAs, Surface: &ref}
	if th, ok := p.Threads.BySurface(who.Tenant, sf); ok {
		return p.PostMessage(ctx, who, th.ID, post)
	}
	if resource, ok := slackResource(ref.Channel); ok {
		if err := p.observeClientResource(c, resource); err != nil {
			return Posted{}, err
		}
	}
	at, err := p.filedAt(who.Tenant, c, ref.Channel)
	if err != nil {
		return Posted{}, err
	}
	seed, err := p.announcementAt(who.Tenant, c, ref, at)
	if err != nil {
		return Posted{}, err
	}
	in.Project, in.Channel, in.Visibility = at.project, at.channel, at.vis
	got, err := p.startThread(ctx, who, in, &surfaced{client: c, ref: ref, announcement: seed})
	if errors.Is(err, threads.ErrExists) {
		// Another post started it first: this one joins it.
		if th, ok := p.Threads.BySurface(who.Tenant, sf); ok {
			return p.PostMessage(ctx, who, th.ID, post)
		}
	}
	return got, err
}

// surfaced is a message's surface, as the platform has checked it, and the
// announcement a thread it starts is seeded with, if any.
type surfaced struct {
	client       clients.Client
	ref          SurfaceRef
	announcement string
}

// announcementAt is the announcement a new thread on a client's surface is
// seeded with: the one whose post there the client's machine recorded
// (RecordDelivery) as the Slack thread's own first message, since a reply to
// it is what starts the thread. It seeds only a thread the Slack channel's
// binding files in the announcement's own project, whose readers see that
// project. A thread filed anywhere else, after the Slack channel was bound
// to another project or unbound, starts as any other there, whatever became
// of the announcement. Where it would seed, one that was withdrawn, or a
// draft, starts nothing (ErrAnnouncementGone).
func (p *Platform) announcementAt(tenant string, c clients.Client, ref SurfaceRef, at filing) (string, error) {
	m, ok := p.Inbox.ByDelivery(tenant, c.ID, inbox.Delivery{Channel: ref.Channel, TS: ref.TS})
	if !ok || m.Kind != inbox.KindAnnouncement || !at.bound || m.Source.Project != at.project {
		return "", nil
	}
	if slices.ContainsFunc(m.Parts, func(pt inbox.Part) bool { return pt.Type == inbox.PartDraft }) {
		return "", fmt.Errorf("%w: it is a draft, which never reaches a channel", ErrAnnouncementGone)
	}
	if closedForAll(m) {
		return "", fmt.Errorf("%w: it was withdrawn", ErrAnnouncementGone)
	}
	return m.ID, nil
}

// closedForAll reports a message closed for everyone: an announcement
// withdrawn, or a draft published or discarded. A reader who hid a
// broadcast for themselves closed only their own copy.
func closedForAll(m inbox.Message) bool {
	return m.State == inbox.Dismissed || m.State == inbox.Resolved
}

// seedOf is the announcement a thread was started from, as it stands when
// the thread is read: who posted it, a person, an agent or humagen itself for
// a release's notes, and what it says, or only that it was withdrawn. It is
// read under the thread's own read rule, which is the announcement's
// project's, as announcementAt filed it.
func (p *Platform) seedOf(tenant string, th threads.Thread) (threads.Announcement, bool) {
	if th.Announcement == "" {
		return threads.Announcement{}, false
	}
	m, ok := p.Inbox.ByID(tenant, th.Announcement)
	if !ok || m.Kind != inbox.KindAnnouncement || m.Source.Project != th.Project {
		return threads.Announcement{}, false
	}
	a := threads.Announcement{ID: m.ID, By: m.From.Name, At: m.At}
	switch m.From.Kind {
	case "person":
		a.Kind = threads.Person
	case "agent":
		a.Kind = threads.Agent
	case "system":
		a.Kind = threads.System
	default:
		return threads.Announcement{}, false
	}
	if closedForAll(m) {
		a.Withdrawn = true
		return a, true
	}
	var texts []string
	for _, part := range m.Parts {
		if part.Type == inbox.PartText && part.Text != "" {
			texts = append(texts, part.Text)
		}
	}
	a.Subject, a.Text = m.Subject, strings.Join(texts, "\n\n")
	return a, true
}

// seedTurns is the turn a thread's room starts with: its announcement, when
// it was started from one.
func (p *Platform) seedTurns(tenant string, th threads.Thread) []threads.Turn {
	if a, ok := p.seedOf(tenant, th); ok {
		return []threads.Turn{threads.AnnouncementTurn(a)}
	}
	return nil
}

// surfaceOfPost checks a message posted with its surface into a thread: the
// thread must be that surface's.
func (p *Platform) surfaceOfPost(who auth.Principal, th threads.Thread, in MessageInput) (*surfaced, error) {
	if in.Surface == nil {
		return nil, nil
	}
	c, err := p.surfaceHost(who, *in.Surface, in.ActAs)
	if err != nil {
		return nil, err
	}
	if th.Surface == nil || *th.Surface != in.Surface.surface(c.ID) {
		return nil, fmt.Errorf("%w: this thread is not that surface's", threads.ErrInvalid)
	}
	return &surfaced{client: c, ref: *in.Surface}, nil
}
