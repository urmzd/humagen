package platform

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/urmzd/orchagent/flow/runtime"

	"github.com/urmzd/humagen/pkg/audit"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/corpora"
	"github.com/urmzd/humagen/pkg/knowledge"
	"github.com/urmzd/humagen/pkg/threads"
)

// An answer stays where it was asked, so what a corpus tool may use is
// limited to what everyone that place reaches may read, checked when the
// tool runs (people join channels):
//
//   - a direct conversation (the person's own private conversation in the
//     console, or their DM with the app): everything that person may read,
//     at any label, with live references and their own warehouse logins;
//   - a group (a shared conversation in the console, or a Slack private
//     channel or group DM): a corpus every current member reads, at any
//     label, and only members every one of them may see; a member the
//     workspace cannot map to a person fails it;
//   - a public channel, or a Slack thread whose audience is not known:
//     public and internal corpora the whole workspace reads;
//   - a GitHub thread, which may be public beyond the workspace: public
//     corpora the whole workspace reads.
//
// When a corpus is held back, the tool says so, so the agent can suggest a
// direct message, or a private channel whose members may all read it.

// Audience kinds.
const (
	AudienceDirect = "direct"
	AudienceGroup  = "group"
	AudiencePublic = "public"
	AudienceGitHub = "github"
)

// ReportedAudience is who a Slack conversation reaches, as its app last
// reported it with a message: its kind (public, private, group or
// direct), its members as people, and how many members the workspace could
// not map to a person.
type ReportedAudience struct {
	Kind     string
	Members  []string
	Unmapped int
	At       time.Time
}

// reportedAudience reads a surfaced session's latest reported audience
// through audienceSource, which the session API sets. Not reported reads as
// a public channel.
func (p *Platform) reportedAudience(tenant, session string) (ReportedAudience, bool) {
	if p.audienceSource == nil {
		return ReportedAudience{}, false
	}
	return p.audienceSource(tenant, session)
}

// corpusAudience is who a run's answer reaches.
type corpusAudience struct {
	kind string
	// person is whom the run acts for.
	person string
	// readers are the people the answer reaches: the person alone when
	// direct, every member of a group.
	readers []auth.Principal
	// unmapped counts a group's members the workspace cannot map to a
	// person.
	unmapped int
}

func (a corpusAudience) direct() bool { return a.kind == AudienceDirect }

func (p *Platform) audienceOf(req runtime.Request) (corpusAudience, error) {
	if _, person, err := p.ownPrivateConversation(req); err == nil {
		return p.directAudience(req.Tenant, person), nil
	}
	run, err := p.Runs.Get(req.Tenant, req.RunID)
	if err != nil {
		return corpusAudience{}, knowledge.ErrNotFound
	}
	person := personOf(run.Actor)
	if person == "" {
		return corpusAudience{}, fmt.Errorf("%w: a corpus is read for a person", corpora.ErrInvalid)
	}
	th, err := p.Threads.Get(req.Tenant, run.Thread)
	if err != nil {
		return corpusAudience{}, knowledge.ErrNotFound
	}
	public := corpusAudience{kind: AudiencePublic, person: person, readers: []auth.Principal{p.invitationReader(req.Tenant, person)}}
	if th.Surface == nil {
		return p.groupAudience(req.Tenant, person, participantsOf(th), 0), nil
	}
	if c, err := p.Clients.Get(req.Tenant, th.Surface.Client); err != nil {
		return public, nil
	} else if c.Surface == clients.GitHub {
		public.kind = AudienceGitHub
		return public, nil
	}
	if directMessage(th.Surface.Channel) {
		return p.directAudience(req.Tenant, person), nil
	}
	au, ok := p.reportedAudience(req.Tenant, th.ID)
	if !ok {
		return public, nil
	}
	switch au.Kind {
	case AudienceDirect:
		return p.directAudience(req.Tenant, person), nil
	case "private", AudienceGroup:
		return p.groupAudience(req.Tenant, person, au.Members, au.Unmapped), nil
	}
	return public, nil
}

func (p *Platform) directAudience(tenant, person string) corpusAudience {
	return corpusAudience{kind: AudienceDirect, person: person, readers: []auth.Principal{p.invitationReader(tenant, person)}}
}

// groupAudience is a group of people, the person asking first.
func (p *Platform) groupAudience(tenant, person string, members []string, unmapped int) corpusAudience {
	a := corpusAudience{kind: AudienceGroup, person: person, unmapped: unmapped}
	seen := map[string]bool{}
	for _, who := range append([]string{person}, members...) {
		if who != "" && !seen[who] {
			seen[who] = true
			a.readers = append(a.readers, p.invitationReader(tenant, who))
		}
	}
	return a
}

func participantsOf(th threads.Thread) []string {
	var out []string
	for _, pt := range th.Participants {
		if pt.Kind == threads.Person {
			out = append(out, pt.Name)
		}
	}
	return out
}

func workspaceReads(c corpora.Corpus) bool {
	return slices.Contains(c.Readers, corpora.Reader{Kind: corpora.Workspace})
}

// mayShow reports whether an audience may be shown a corpus.
func (p *Platform) mayShow(a corpusAudience, c corpora.Corpus) bool {
	switch a.kind {
	case AudienceDirect:
		return p.corpusVisible(a.readers[0], c)
	case AudienceGroup:
		if a.unmapped > 0 {
			return false
		}
		for _, r := range a.readers {
			if !p.corpusVisible(r, c) {
				return false
			}
		}
		return true
	case AudiencePublic:
		return workspaceReads(c) && !c.Label.Sensitive() && p.corpusVisible(a.readers[0], c)
	case AudienceGitHub:
		return workspaceReads(c) && c.Label == corpora.Public && p.corpusVisible(a.readers[0], c)
	}
	return false
}

// memberShown reports whether everyone an answer reaches may see a member:
// the person when direct, every member of a group, and, in a public place,
// anyone in the workspace (a source no project confines, a warehouse
// published to the workspace).
func (p *Platform) memberShown(a corpusAudience, m corpora.Member) bool {
	for _, r := range a.readers {
		if !p.memberVisible(r, m) {
			return false
		}
	}
	if a.kind != AudiencePublic && a.kind != AudienceGitHub {
		return true
	}
	switch m.Kind {
	case corpora.Source:
		src, err := p.Knowledge.Get(a.readers[0].Tenant, m.Name)
		return err == nil && (src.Lifecycle == nil || src.Lifecycle.Project == "")
	case corpora.Catalog:
		c, err := p.CatalogStore.Connector(a.readers[0].Tenant, m.Name)
		return err == nil && c.Published != nil && c.Project == ""
	}
	return false
}

// shownCorpora are the corpora granted to a run's agent that its audience
// may be shown, by name, and how many others the person asking could read
// but this place may not be shown.
func (p *Platform) shownCorpora(req runtime.Request, a corpusAudience) ([]corpora.Corpus, int, error) {
	all, err := p.Corpora.List(req.Tenant)
	if err != nil {
		return nil, 0, err
	}
	var shown []corpora.Corpus
	held := 0
	for _, c := range all {
		if !c.Grants(req.Agent()) {
			continue
		}
		switch {
		case p.mayShow(a, c):
			shown = append(shown, c)
		case p.corpusVisible(a.readers[0], c):
			held++
		}
	}
	return shown, held, nil
}

// corporaHeldBack is what a tool tells the agent about corpora it could not use
// here.
func corporaHeldBack(n int) string {
	if n == 0 {
		return ""
	}
	return fmt.Sprintf("%d corpora granted here could not be used in this conversation: their content is confidential or restricted, or not everyone it reaches may read them. Say so plainly, and suggest asking in a direct message, or in a private channel whose members may all read them.", n)
}

// readCorpora records what a tool read: the run is marked private when its
// answer reached one person, and every read of a confidential or
// restricted corpus is audited with the agent, the conversation, the
// audience and the corpus's purpose, so people can see who read what.
func (p *Platform) readCorpora(ctx context.Context, req runtime.Request, a corpusAudience, read []corpora.Corpus) error {
	if a.direct() {
		if err := p.MarkPrivateContext(req.Tenant, req.RunID); err != nil {
			return err
		}
	}
	thread := ""
	if run, err := p.Runs.Get(req.Tenant, req.RunID); err == nil {
		thread = run.Thread
	}
	for _, c := range read {
		if !c.Label.Sensitive() {
			continue
		}
		attempt, err := p.BeginAudit(a.readers[0], audit.Change{Action: "corpus.read", Resource: "corpus", ID: c.Name, Scope: "personal", Thread: thread, Subject: a.person, Agent: req.Agent(),
			After: map[string]string{"label": string(c.Label), "purpose": c.Purpose, "run": req.RunID, "audience": a.kind}})
		if err != nil {
			return err
		}
		if err := attempt.Finish(ctx.Err()); err != nil {
			return err
		}
	}
	return nil
}
