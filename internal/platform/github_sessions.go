package platform

import (
	"cmp"
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
	"github.com/urmzd/saige/flow/address"
	"github.com/urmzd/saige/flow/event"
	"github.com/urmzd/saige/flow/session"
	"github.com/urmzd/saige/flow/thread"
)

// A GitHub issue or pull request has one session, as a Slack thread has
// one: keyed by its address, github://<owner>/<repo>/pull/<n> or
// github://<owner>/<repo>/issues/<n>, with every parameter (the head
// commit, a review thread) left out, and owned by the agent that first
// answered there. That key is the one the runtime already keeps the agent's
// conversation on the issue or pull request under (thread.KeyOf), so the
// session the console lists is the conversation the agent continues, and a
// run from before sessions were filed is bound to the same record.
//
// The session is private to the people who asked in it: each person the
// client maps a GitHub login to (GitHubOptions.People) is added when they
// ask. GitHub lets them read the repository, which Humagen cannot check
// for anyone else, so nobody else reads it, and an unmapped login adds
// nobody. It is filed where the run is filed: the project the repository
// is linked to, or else the client's home, as Slack files a thread in a
// channel bound to no project. The session's surface is the client's, so
// what an answer may use is held to what a GitHub thread may show
// (corpusAudienceOf), never the asker's own private conversation.

// githubPlace is the issue or pull request a GitHub run was asked on, from
// the address the connector raised it at, and its surface on the client.
// A run raised anywhere else, such as an automation's or an automatic
// review's, has none.
func githubPlace(tag event.Envelope, client string) (string, threads.Surface, bool) {
	at, err := address.Parse(tag.Source)
	if err != nil || at.Connector != runs.OriginGitHub || len(at.Path) != 3 {
		return "", threads.Surface{}, false
	}
	repo, kind, number := at.Path[0], at.Path[1], at.Path[2]
	if kind != "pull" && kind != "issues" {
		return "", threads.Surface{}, false
	}
	if n, err := strconv.Atoi(number); err != nil || n <= 0 || strconv.Itoa(n) != number {
		return "", threads.Surface{}, false
	}
	place := thread.Surface(at)
	if place != (address.Address{Connector: at.Connector, Workspace: at.Workspace, Path: at.Path}).String() {
		return "", threads.Surface{}, false
	}
	sf := threads.Surface{Client: client, Channel: at.Workspace + "/" + repo, TS: kind + "/" + number}
	if client == "" || sf.Validate() != nil {
		return "", threads.Surface{}, false
	}
	return place, sf, true
}

// githubTitle is a GitHub session's title: the issue's or pull request's
// own, or its reference when the event did not carry it.
func githubTitle(tag event.Envelope, sf threads.Surface) string {
	if title := firstWords(wire.GitHubTitleOf(tag), threads.MaxTitle); title != "" {
		return title
	}
	_, number, _ := strings.Cut(sf.TS, "/")
	return firstWords(sf.Channel+"#"+number, threads.MaxTitle)
}

// fileInGitHubSession files a GitHub run in its issue's or pull request's
// session, opening the session on the first run there, and adds the person
// it acts for to the session's people. Another agent asked there later
// answers in the same session with a conversation of its own, as an agent
// a Slack message mentions does. A session that cannot be filed leaves the
// run its own conversation, as every GitHub run was before, rather than
// lose it; what is delivered to GitHub is the same either way.
//
// A thread named in the tag is never believed: the platform names it here.
func (p *Platform) fileInGitHubSession(ctx context.Context, tag event.Envelope, project, client string) event.Envelope {
	wire.DeleteMeta(&tag, runs.MetaThread)
	place, sf, ok := githubPlace(tag, client)
	if !ok {
		return tag
	}
	agent := wire.AgentOf(tag)
	person := personOf(wire.ActorOf(tag))
	th, found, err := p.Threads.BySurface(tag.Tenant, sf)
	if err == nil && !found {
		th, err = p.openGitHubSession(ctx, tag, project, place, sf, agent, person)
	}
	if err != nil {
		p.log.Warn("platform: a GitHub run stays its own conversation", "place", place, "client", client, "error", err)
		return tag
	}
	if person != "" && !sessionOf(th, person) {
		pt := threads.Participant{Kind: threads.Person, Name: person, At: p.now()}
		if _, err := p.Threads.Participants(tag.Tenant, th.ID, th.Version, []threads.Participant{pt}, nil, func(threads.Participant) bool { return true }, "github", p.now()); err != nil {
			// The run is still filed: they asked, and may be added on
			// their next mention.
			p.log.Warn("platform: a GitHub asker was not added to the session", "place", place, "session", th.ID, "error", err)
		}
	}
	wire.SetMeta(&tag, runs.MetaThread, th.ID)
	return tag
}

// errGitHubSessionElsewhere reports an issue's or pull request's session
// that another client on the same repository opened. Each client's runs
// keep to their own surface, so the run stays its own conversation, as it
// was before sessions.
var errGitHubSessionElsewhere = errors.New("another client on this repository holds its session")

// openGitHubSession opens the session of an issue or pull request for its
// first run, or finds the one another process opened at the same time.
func (p *Platform) openGitHubSession(ctx context.Context, tag event.Envelope, project, place string, sf threads.Surface, agent, person string) (threads.Thread, error) {
	channel := cmp.Or(wire.Meta(tag)[runs.MetaChannel], runs.DefaultChannel)
	ch, err := p.Projects.Channel(tag.Tenant, project, channel)
	if err != nil {
		return threads.Thread{}, err
	}
	if ch.Archived {
		return threads.Thread{}, errors.New("its channel is archived")
	}
	// The record the runtime opens for this conversation: the same key and
	// the same channel, so either may open it first.
	rec, err := p.Sessions.Open(ctx, tag.Tenant, session.Open{Parent: place, Agent: agent, Channel: tag.Channel})
	if err != nil {
		return threads.Thread{}, err
	}
	th := threads.Thread{
		ID: rec.ID, Project: project, Channel: ch.Name, Title: githubTitle(tag, sf), Visibility: threads.Private,
		CreatedBy: person, Default: agent, Agents: []string{agent}, Surface: &sf,
		Session: &threads.SessionIdentity{Agent: agent, Parent: rec.Key.Parent, Label: rec.Key.Label},
	}
	created, err := p.Threads.Create(tag.Tenant, th, p.now())
	if !errors.Is(err, threads.ErrExists) {
		return created, err
	}
	// Another process filed the first run on this surface: it is the
	// session. Otherwise the record's thread is another client's.
	if got, found, err := p.Threads.BySurface(tag.Tenant, sf); err != nil || found {
		return got, err
	}
	return threads.Thread{}, errGitHubSessionElsewhere
}

// githubSessionOf is the GitHub session a thread belongs to: the thread
// itself, or the root of the session it is a child of.
func (p *Platform) githubSessionOf(tenant, id string) (threads.Thread, bool) {
	for range 64 {
		th, err := p.Threads.Get(tenant, id)
		if err != nil || th.Session == nil {
			return threads.Thread{}, false
		}
		if strings.HasPrefix(th.Session.Parent, "s_") {
			id = th.Session.Parent
			continue
		}
		return th, th.Surface != nil && strings.HasPrefix(th.Session.Parent, "github://")
	}
	return threads.Thread{}, false
}

// onItsGitHubSession reports that a run belongs to a tree a GitHub client
// raised and is in that client's session of the issue or pull request, or
// a child session of it: the root run, or an agent it called. Its
// conversation is the one on GitHub, so what it posts or does there is
// held to the rules it was held to before GitHub runs were filed, not to a
// private conversation's: the session is private only so that Humagen
// shows it to nobody who did not ask there. A person's reply posted in
// the console is not such a run.
func (p *Platform) onItsGitHubSession(run runs.Run) bool {
	root := run
	if run.Parent != "" {
		top, err := p.Runs.Get(run.Tenant, run.Root)
		if err != nil {
			return false
		}
		root = top
	}
	if root.Origin != runs.OriginGitHub || root.Client == "" || run.Client != root.Client {
		return false
	}
	th, ok := p.githubSessionOf(run.Tenant, run.Thread)
	return ok && th.ID == root.Thread && th.Surface.Client == root.Client
}

// publishesPrivately reports that a run is in a conversation only its
// participants read, and not one on GitHub that it answers on GitHub.
func (p *Platform) publishesPrivately(run runs.Run) bool {
	return p.privateThread(run.Tenant, run.Thread) && !p.onItsGitHubSession(run)
}
