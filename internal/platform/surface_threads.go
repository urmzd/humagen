package platform

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/urmzd/warp/pkg/auth"
	"github.com/urmzd/warp/pkg/clients"
	"github.com/urmzd/warp/pkg/projects"
	"github.com/urmzd/warp/pkg/threads"
)

// A thread on a client's surface, such as a Slack thread, is one thread here.
// The client's machine posts each person's message there with the surface it
// came from, and the platform finds the thread by that surface or starts it.
// Where it is filed is the platform's to decide, never the request's: in the
// channel the Slack channel is bound to, shared; or, outside a bound channel,
// in the client's home, private for a direct message or the assistant pane
// and shared otherwise. The message is Slack's own text, which the platform
// reads in the one grammar (slackText).

// SurfaceRef is where a message was posted on a client's surface.
type SurfaceRef struct {
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

// filedAt is where a thread a client starts on its surface is filed.
func (p *Platform) filedAt(tenant string, c clients.Client, channel string) (string, string, threads.Visibility, error) {
	if ch, bound := p.Projects.BoundTo(tenant, c.ID, channel); bound {
		return ch.Project, ch.Name, threads.Shared, nil
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
		return "", "", "", fmt.Errorf("%w: %s's home, #%s in %s, is gone; give it another", projects.ErrNotFound, c.ID, h.Channel, where)
	}
	return h.Project, h.Channel, vis, nil
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
	project, channel, vis, err := p.filedAt(who.Tenant, c, ref.Channel)
	if err != nil {
		return Posted{}, err
	}
	in.Project, in.Channel, in.Visibility = project, channel, vis
	got, err := p.startThread(ctx, who, in, &surfaced{client: c, ref: ref})
	if errors.Is(err, threads.ErrExists) {
		// Another post started it first: this one joins it.
		if th, ok := p.Threads.BySurface(who.Tenant, sf); ok {
			return p.PostMessage(ctx, who, th.ID, post)
		}
	}
	return got, err
}

// surfaced is a message's surface, as the platform has checked it.
type surfaced struct {
	client clients.Client
	ref    SurfaceRef
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
