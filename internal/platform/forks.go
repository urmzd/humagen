package platform

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/urmzd/humagen/pkg/audit"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
)

// ForkInput chooses a saved branch point and routing for future messages.
type ForkInput struct {
	Sources *[]string `json:"sources,omitempty"`
	Through string    `json:"through"`
	Title   string    `json:"title,omitempty"`
	Agents  []string  `json:"agents,omitempty"`
	Mode    string    `json:"mode,omitempty"`
}

// ForkThread saves passive history. It does not invoke any agent or copy actions.
func (p *Platform) ForkThread(ctx context.Context, who auth.Principal, id string, in ForkInput) (threads.Thread, error) {
	mu := p.moves.of(who.Tenant, id)
	mu.RLock()
	defer mu.RUnlock()
	source, err := p.ThreadRecord(who.Tenant, id)
	if err != nil || !p.SeesThread(who, id) {
		return threads.Thread{}, threads.ErrNotFound
	}
	if _, _, _, _, err := p.author(who, nil); err != nil {
		return threads.Thread{}, err
	}
	person, err := p.poster(who, nil)
	if err != nil {
		return threads.Thread{}, err
	}
	if err = p.MayPost(person, Place{Project: source.Project, Thread: id}); err != nil {
		return threads.Thread{}, err
	}
	agents, mode, err := p.checkRouting(ctx, who.Tenant, source.Project, in.Agents, in.Mode, p.Seer(who).Agent)
	if err != nil {
		return threads.Thread{}, err
	}
	if in.Sources == nil && source.Fork != nil && source.Fork.Sources != nil {
		names := slices.Clone(*source.Fork.Sources)
		in.Sources = &names
	}
	if in.Sources != nil {
		if len(*in.Sources) > 16 || len(p.Seer(who).Sources(*in.Sources)) != len(*in.Sources) {
			return threads.Thread{}, fmt.Errorf("%w: selected sources are not available", threads.ErrInvalid)
		}
		known := map[string]bool{}
		for _, source := range p.Knowledge.List(who.Tenant) {
			known[source.Name] = true
		}
		for _, name := range *in.Sources {
			if !known[name] || !p.SourceVisible(who, name) {
				return threads.Thread{}, threads.ErrNotFound
			}
		}
	}
	history, err := p.forkHistory(ctx, who, source, in.Through, source.Project)
	if err != nil {
		return threads.Thread{}, err
	}
	now := p.now()
	attempt, err := p.BeginAudit(who, audit.Change{Action: "conversation.fork", Resource: "conversation", Scope: "personal", Subject: who.Subject, ID: id, Project: source.Project, Thread: id, After: map[string]string{"through": in.Through}})
	if err != nil {
		return threads.Thread{}, err
	}
	result, err := p.Threads.Create(who.Tenant, threads.Thread{Project: source.Project, Channel: source.Channel, Title: in.Title, Visibility: threads.Private, CreatedBy: person.Subject, Agents: agents, Mode: mode, Fork: &threads.Fork{Sources: in.Sources, SourceThread: id, Through: in.Through, By: person.Subject, At: now, Context: history}}, now)
	_ = attempt.Finish(err)
	return result, err
}

// BringBackInput selects an outcome. Text, if present, is the caller's edit.
type BringBackInput struct {
	Run  string `json:"run"`
	Text string `json:"text,omitempty"`
}

// BringBackResult posts one explicit human message, with no tag interpretation,
// tool execution, pending approvals or automatic agent invocation.
func (p *Platform) BringBackResult(ctx context.Context, who auth.Principal, id string, in BringBackInput) (threads.Message, error) {
	fork, err := p.ThreadRecord(who.Tenant, id)
	if err != nil || !p.SeesThread(who, id) || fork.Fork == nil {
		return threads.Message{}, threads.ErrNotFound
	}
	run, err := p.Runs.Get(who.Tenant, in.Run)
	if err != nil || run.Thread != id || run.Status != runs.Completed || !p.Seer(who).Run(run) {
		return threads.Message{}, threads.ErrNotFound
	}
	mu := p.moves.of(who.Tenant, fork.Fork.SourceThread)
	mu.RLock()
	defer mu.RUnlock()
	parent, err := p.ThreadRecord(who.Tenant, fork.Fork.SourceThread)
	if err != nil || !p.SeesThread(who, parent.ID) {
		return threads.Message{}, threads.ErrNotFound
	}
	if p.RunRequiresReviewedSummary(ctx, run, parent.Project) {
		return threads.Message{}, fmt.Errorf("%w: private-context results require a reviewed summary", ErrForbidden)
	}
	if _, _, _, _, err := p.author(who, nil); err != nil {
		return threads.Message{}, err
	}
	person, err := p.poster(who, nil)
	if err != nil {
		return threads.Message{}, err
	}
	if err = p.MayPost(person, Place{Project: parent.Project, Thread: parent.ID}); err != nil {
		return threads.Message{}, err
	}
	text := in.Text
	if text == "" {
		text = run.Answer
	}
	text = "Result brought back from " + id + " (" + run.Agent + ", " + run.ID + "):\n\n" + text
	if err = threads.CheckText(text); err != nil {
		return threads.Message{}, err
	}
	if err = p.posts.take(who.Tenant, person.Subject, parent.Project, p.now()); err != nil {
		return threads.Message{}, err
	}
	if _, err = p.recordOf(who.Tenant, parent.ID, p.claimant(person, parent.Project)); err != nil {
		return threads.Message{}, err
	}
	prev, err := p.prevOf(who.Tenant, parent, "")
	if err != nil {
		return threads.Message{}, err
	}
	attempt, err := p.BeginAudit(who, audit.Change{Action: "conversation.bring_back", Resource: "conversation", ID: parent.ID, Project: parent.Project, Thread: parent.ID, After: map[string]string{"fork": id, "run": run.ID}})
	if err != nil {
		return threads.Message{}, err
	}
	m, _, err := p.Threads.Post(who.Tenant, parent.ID, threads.Message{By: person.Subject, Name: p.NameOf(who.Tenant, person.Subject), Text: text, Prev: prev, Via: threads.ViaConsole}, p.now())
	_ = attempt.Finish(err)
	return m, err
}

// forkSourcesVisible checks every source without recursive Place calls. The
// callback grants ordinary access or records each governed read as usual.
func (s *Seer) forkSourcesVisible(at Place, direct func(Place) bool) bool {
	seen := map[string]bool{}
	for depth := 0; at.Thread != ""; depth++ {
		if depth > threads.MaxForkDepth || seen[at.Thread] {
			return false
		}
		seen[at.Thread] = true
		th, err := s.p.Threads.Get(s.who.Tenant, at.Thread)
		if err != nil {
			// A lifecycle notice can precede the run index. Only recorded
			// threads can carry fork history; retain the ordinary read rule
			// for unrecorded conversations and their early events.
			return true
		}
		if th.Fork == nil {
			return true
		}
		parent, err := s.p.ThreadRecord(s.who.Tenant, th.Fork.SourceThread)
		if err != nil {
			return false
		}
		at = Place{Project: parent.Project, Thread: parent.ID}
		if !direct(at) {
			return false
		}
	}
	return true
}

// forkSeed preserves immutable history inside the same fenced, untrusted
// transcript used for live turns. Snapshot text is never interpreted as tags.
func forkSeed(th threads.Thread) []threads.Turn {
	if th.Fork == nil {
		return nil
	}
	out := make([]threads.Turn, 0, len(th.Fork.Context))
	for i, c := range th.Fork.Context {
		out = append(out, threads.AnnouncementTurn(threads.Announcement{ID: fmt.Sprintf("fork-%d", i), Kind: c.Kind, By: c.By, Name: c.Name, Subject: "Read-only fork context", Text: strings.TrimSpace(c.Text), At: c.At}))
	}
	return out
}

// forkSourceSelection returns a fresh optional allowlist for an admitted run.
func (p *Platform) forkSourceSelection(tenant, thread string) *[]string {
	th, err := p.ThreadRecord(tenant, thread)
	if err != nil || th.Fork == nil || th.Fork.Sources == nil {
		return nil
	}
	names := slices.Clone(*th.Fork.Sources)
	return &names
}

// forkHistory is the passive snapshot used by interactive forks and read-only
// project archives. The caller holds the source placement lock.
func (p *Platform) forkHistory(ctx context.Context, who auth.Principal, source threads.Thread, through, destination string) ([]threads.ForkTurn, error) {
	if err := p.checkForkDepth(who.Tenant, source); err != nil {
		return nil, err
	}
	var turns []threads.Turn
	if strings.HasPrefix(through, "m-") {
		message, err := p.Threads.Message(who.Tenant, source.ID, through)
		if err != nil {
			return nil, threads.ErrNotFound
		}
		turns = append(p.roomBefore(who.Tenant, source, message), threads.MessageTurn(message))
	} else {
		run, err := p.Runs.Get(who.Tenant, through)
		if err != nil || run.Thread != source.ID || !p.Seer(who).Run(run) {
			return nil, threads.ErrNotFound
		}
		if !run.Status.Terminal() {
			return nil, fmt.Errorf("%w: fork from a completed turn", threads.ErrInvalid)
		}
		path, err := p.Runs.Path(who.Tenant, run.ID)
		if err != nil {
			return nil, threads.ErrNotFound
		}
		turns = p.branchBefore(who.Tenant, source, path)
	}
	history := []threads.ForkTurn{}
	seer := p.Seer(who)
	for _, turn := range turns {
		switch {
		case turn.Message != nil:
			m := turn.Message
			if m.Thread != source.ID {
				return nil, threads.ErrNotFound
			}
			history = append(history, threads.ForkTurn{Kind: threads.Person, By: m.By, Name: m.Name, Text: m.Text, At: m.At})
		case turn.Run != nil:
			r := turn.Run
			if r.Thread != source.ID {
				return nil, threads.ErrNotFound
			}
			if !seer.Run(*r) {
				continue
			}
			if p.RunRequiresReviewedSummary(ctx, *r, destination) {
				return nil, fmt.Errorf("%w: private-context results require a reviewed summary", threads.ErrInvalid)
			}
			if !r.Status.Terminal() {
				continue
			}
			if r.Message == "" {
				by, kind := runs.AskedBy(*r), threads.Person
				if by == "" {
					by, kind = "system", threads.System
				}
				history = append(history, threads.ForkTurn{Kind: kind, By: by, Text: r.Text, At: r.CreatedAt})
			}
			history = append(history, threads.ForkTurn{Kind: threads.Agent, By: r.Agent, Text: threads.PassiveReply(*r), At: r.CreatedAt})
		case turn.Announcement != nil:
			ann := turn.Announcement
			history = append(history, threads.ForkTurn{Kind: ann.Kind, By: ann.By, Name: ann.Name, Text: ann.Text, At: ann.At})
		}
		if len(history) > threads.MaxForkTurns {
			return nil, fmt.Errorf("%w: fork history exceeds %d turns", threads.ErrInvalid, threads.MaxForkTurns)
		}
	}
	return history, nil
}

func (p *Platform) checkForkDepth(tenant string, source threads.Thread) error {
	parent := source
	for depth := 1; parent.Fork != nil; depth++ {
		if depth >= threads.MaxForkDepth {
			return fmt.Errorf("%w: fork lineage is at most %d deep", threads.ErrInvalid, threads.MaxForkDepth)
		}
		next, err := p.ThreadRecord(tenant, parent.Fork.SourceThread)
		if err != nil {
			return threads.ErrNotFound
		}
		parent = next
	}
	return nil
}

// Passive inherited context cannot be published as an independent raw history.
// A reviewed summary deliberately copies only the person's selected text.
type forkPublicationGuard struct{ p *Platform }

func (g forkPublicationGuard) ConversationOperationsSettled(tenant, thread string) bool {
	th, err := g.p.Threads.Get(tenant, thread)
	return err == nil && !th.ReadOnly && (th.Fork == nil || len(th.Fork.Context) == 0)
}
