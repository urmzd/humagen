package platform

import (
	"context"
	"fmt"
	"slices"

	"github.com/urmzd/orchagent/flow/runtime"

	"github.com/urmzd/humagen/pkg/audit"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/corpora"
	"github.com/urmzd/humagen/pkg/knowledge"
	"github.com/urmzd/humagen/pkg/threads"
)

// Who a corpus tool's answer reaches decides what it may show. In the
// person's own private conversation it shows what that person may read, at
// any label, and the run is marked private: what it posts elsewhere is a
// pointer back, and nothing it read goes into shared memory. In a shared
// conversation it shows only public and internal corpora that every person
// in it may read, and only members every one of them may see; in a Slack or
// GitHub thread, whose audience cannot be listed, only corpora the whole
// workspace reads. Confidential and restricted content never leaves its
// reader's private conversation.

// corpusAudience is who a run's answer reaches.
type corpusAudience struct {
	// person is whom the run acts for.
	person string
	// private is set in that person's own private conversation.
	private bool
	// readers are every person in the conversation, as principals.
	readers []auth.Principal
	// surfaced is set for a conversation posted to Slack or GitHub.
	surfaced bool
}

func (p *Platform) audienceOf(req runtime.Request) (corpusAudience, error) {
	if _, person, err := p.ownPrivateConversation(req); err == nil {
		return corpusAudience{person: person, private: true, readers: []auth.Principal{p.invitationReader(req.Tenant, person)}}, nil
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
	a := corpusAudience{person: person, surfaced: th.Surface != nil}
	seen := map[string]bool{}
	for _, who := range append([]string{person}, participantsOf(th)...) {
		if !seen[who] {
			seen[who] = true
			a.readers = append(a.readers, p.invitationReader(req.Tenant, who))
		}
	}
	return a, nil
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

// mayShow reports whether an audience may be shown a corpus.
func (p *Platform) mayShow(a corpusAudience, c corpora.Corpus) bool {
	if a.private {
		return p.corpusVisible(a.readers[0], c)
	}
	if c.Label.Sensitive() || (a.surfaced && !slices.Contains(c.Readers, corpora.Reader{Kind: corpora.Workspace})) {
		return false
	}
	for _, r := range a.readers {
		if !p.corpusVisible(r, c) {
			return false
		}
	}
	return true
}

// memberShown reports whether every reader may see a member.
func (p *Platform) memberShown(a corpusAudience, m corpora.Member) bool {
	for _, r := range a.readers {
		if !p.memberVisible(r, m) {
			return false
		}
	}
	return true
}

// shownCorpora are the corpora granted to a run's agent that its audience
// may be shown, by name.
func (p *Platform) shownCorpora(req runtime.Request, a corpusAudience) ([]corpora.Corpus, error) {
	all, err := p.Corpora.List(req.Tenant)
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(all, func(c corpora.Corpus) bool { return !c.Grants(req.Agent()) || !p.mayShow(a, c) }), nil
}

// readCorpora records what a tool read: the run is marked private when its
// answer was for one person, and every read of a confidential or
// restricted corpus is audited with the agent, the conversation and the
// corpus's purpose, so people can see who read what.
func (p *Platform) readCorpora(ctx context.Context, req runtime.Request, a corpusAudience, read []corpora.Corpus) error {
	if a.private {
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
			After: map[string]string{"label": string(c.Label), "purpose": c.Purpose, "run": req.RunID}})
		if err != nil {
			return err
		}
		if err := attempt.Finish(ctx.Err()); err != nil {
			return err
		}
	}
	return nil
}
