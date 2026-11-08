package platform

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/threads"
	"github.com/urmzd/orchagent/flow/session"
)

// A thread on a chat surface, such as a Slack thread, has one root session:
// the place's, keyed by its address (slack://<team>/<channel>/<thread ts>)
// and owned by the entry agent, like a console chat's. The chat service
// posts each person's message with the surface it came from and the person
// it acts for; the platform finds the place's root or starts it, files it
// where the Slack channel is bound (filedAt), and answers as it answers in
// the console: a mention starts or continues that agent's child session, a
// message naming nobody is picked for by the entry agent.

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
func (p *Platform) StartSurfaceSession(ctx context.Context, who auth.Principal, in SessionInput) (view SessionView, started bool, err error) {
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
	post := MessageInput{Text: in.Text, ActAs: in.ActAs, Surface: &ref}
	if th, ok, err := p.Threads.BySurface(who.Tenant, sf); err != nil {
		return SessionView{}, false, err
	} else if ok {
		if th.Session == nil {
			return SessionView{}, false, ErrSurfaceThreadBeforeSessions
		}
		view, err := p.PostSession(ctx, who, th.ID, post)
		return view, false, err
	}
	person, err := p.poster(who, in.ActAs)
	if err != nil {
		return SessionView{}, false, err
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
	th := threads.Thread{ID: rec.ID, Project: at.project, Channel: at.channel, Visibility: at.vis, Title: firstWords(in.Text, threads.MaxTitle), Default: owner, Agents: []string{owner}, CreatedBy: humanSubject(person), Surface: &sf, Session: &threads.SessionIdentity{Agent: owner, Parent: rec.Key.Parent, Label: rec.Key.Label}}
	_, err = p.Threads.Create(who.Tenant, th, p.now())
	started = err == nil
	if err != nil && !errors.Is(err, threads.ErrExists) {
		return SessionView{}, false, err
	}
	view, err = p.PostSession(ctx, who, rec.ID, post)
	return view, started, err
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
