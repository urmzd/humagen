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
			if !known[name] {
				return threads.Thread{}, threads.ErrNotFound
			}
		}
	}
	// The lineage chain itself is bounded even when its saved text is short.
	parent := source
	for depth := 1; parent.Fork != nil; depth++ {
		if depth >= threads.MaxForkDepth {
			return threads.Thread{}, fmt.Errorf("%w: fork lineage is at most %d deep", threads.ErrInvalid, threads.MaxForkDepth)
		}
		parent, err = p.ThreadRecord(who.Tenant, parent.Fork.SourceThread)
		if err != nil {
			return threads.Thread{}, threads.ErrNotFound
		}
	}
	history := []threads.ForkTurn{}
	if source.Fork != nil {
		history = append(history, source.Fork.Context...)
	}
	found := false
	for _, turn := range p.turnsOf(who.Tenant, source) {
		switch {
		case turn.Message != nil:
			m := turn.Message
			history = append(history, threads.ForkTurn{Kind: threads.Person, By: m.By, Name: m.Name, Text: m.Text, At: m.At})
			found = m.ID == in.Through
		case turn.Run != nil:
			r := turn.Run
			if !r.Status.Terminal() {
				if r.ID == in.Through {
					return threads.Thread{}, fmt.Errorf("%w: fork from a completed turn", threads.ErrInvalid)
				}
				continue
			}
			if !p.Seer(who).Place(placeOfRun(*r)) {
				continue
			}
			if r.Message == "" {
				by, kind := runs.AskedBy(*r), threads.Person
				if by == "" {
					by, kind = "system", threads.System
				}
				history = append(history, threads.ForkTurn{Kind: kind, By: by, Text: r.Text, At: r.CreatedAt})
			}
			history = append(history, threads.ForkTurn{Kind: threads.Agent, By: r.Agent, Text: r.Answer, At: r.CreatedAt})
			found = r.ID == in.Through
		case turn.Announcement != nil:
			a := turn.Announcement
			history = append(history, threads.ForkTurn{Kind: a.Kind, By: a.By, Name: a.Name, Text: a.Text, At: a.At})
		}
		if len(history) > threads.MaxForkTurns {
			return threads.Thread{}, fmt.Errorf("%w: fork history exceeds %d turns", threads.ErrInvalid, threads.MaxForkTurns)
		}
		if found {
			break
		}
	}
	if !found {
		return threads.Thread{}, threads.ErrNotFound
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
func (p *Platform) BringBackResult(_ context.Context, who auth.Principal, id string, in BringBackInput) (threads.Message, error) {
	fork, err := p.ThreadRecord(who.Tenant, id)
	if err != nil || !p.SeesThread(who, id) || fork.Fork == nil {
		return threads.Message{}, threads.ErrNotFound
	}
	run, err := p.Runs.Get(who.Tenant, in.Run)
	if err != nil || run.Thread != id || run.Status != runs.Completed || !p.Seer(who).Place(placeOfRun(run)) {
		return threads.Message{}, threads.ErrNotFound
	}
	mu := p.moves.of(who.Tenant, fork.Fork.SourceThread)
	mu.RLock()
	defer mu.RUnlock()
	parent, err := p.ThreadRecord(who.Tenant, fork.Fork.SourceThread)
	if err != nil || !p.SeesThread(who, parent.ID) {
		return threads.Message{}, threads.ErrNotFound
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
