package platform

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"

	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/join"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
)

// Follow-ups: a message posted while agents work in its thread.
//
// A message that would start agent work, posted while a top-level run its
// poster may stop is working in the thread, is not carried out at once. It
// is classified (join.Classify) as adding to the ask (queue), replacing it
// (replace) or stopping it (stop), and the decision is kept on the message
// (threads.Followup) with the runs it waits on. A replace or a stop asks
// those runs to stop, with its reason as the cause, which keeps what they
// have said; a stop starts nothing. A queue or a replace is released, and
// what it calls for started, once the runs it waits on have ended: a
// completion step of each run (ended), and a try as it is posted.

// maxFollowupContext bounds how much of the working runs' asks the
// classifier is sent.
const maxFollowupContext = 8000

// maxPendingFollowups bounds how many of a thread's pending follow-ups one
// release reads. Those past it are read by the next.
const maxPendingFollowups = 50

// followupAsker is what classifies a follow-up for a workspace: the model
// that asks agents whether to join (asker), when it can. Nil queues every
// follow-up.
func (p *Platform) followupAsker(tenant string) join.FollowupAsker {
	a, _ := p.asker(tenant).(join.FollowupAsker)
	return a
}

// startsWork reports whether a message would start agent work where it is
// posted, decided as carryOut decides it (threads.Decide): it tags an agent
// that can answer, or names a skill, or tags nobody where the thread's
// selected agents or its default answer untagged messages. A message that
// tags only people, or one posted in the console that tags no agent where
// nobody is selected, starts nothing, so it is no follow-up. via is the
// client that posted it for its poster, "" for a person as themselves.
// TagsOnly mirrors carryOut's; keep the two together.
func (p *Platform) startsWork(tenant string, th threads.Thread, mentions []threads.Mention, via string, sees func(string) bool) bool {
	def, unavailable := p.defaultOf(tenant, th, sees)
	routed := slices.DeleteFunc(slices.Clone(th.Agents), func(a string) bool { return sees != nil && !sees(a) })
	d := threads.Decide(threads.Input{Mentions: mentions, TagsOnly: via == "" && th.Session == nil, Routed: routed, Mode: th.Mode, Default: def, Unavailable: unavailable})
	return len(d.Agents) > 0
}

// forcesStop reports whether a message's first word, after any leading
// mentions, is stop: it stops the work going on even when it would start
// none itself.
func forcesStop(text string) bool {
	f, ok := join.Forced(text)
	return ok && f.Outcome == join.FollowupStop
}

// followupOf decides what a message that may start agent work does to the
// work going on in its thread. It is nil when no top-level run its poster
// may stop is working there: the message is then carried out at once, as
// any is. A run its poster may not stop is neither waited on nor
// interrupted.
func (p *Platform) followupOf(ctx context.Context, person auth.Principal, th threads.Thread, text string) (*threads.Followup, error) {
	turns, err := p.Runs.Conversation(person.Tenant, th.ID)
	if errors.Is(err, runs.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var waiting []string
	var current strings.Builder
	for _, r := range turns {
		if r.Parent != "" || r.Status.Terminal() || len(waiting) == threads.MaxFollowupWaiting || p.MayStop(person, r) != nil {
			continue
		}
		waiting = append(waiting, r.ID)
		if current.Len() < maxFollowupContext {
			current.WriteString(r.Text)
			current.WriteString("\n")
		}
	}
	if len(waiting) == 0 {
		return nil, nil
	}
	d := join.Classify(ctx, p.followupAsker(person.Tenant), current.String(), text)
	return &threads.Followup{
		Outcome: d.Outcome, Reason: d.Reason, Waiting: waiting,
		// A stop is carried out as it is posted: there is nothing to release.
		Handled: d.Outcome == join.FollowupStop,
	}, nil
}

// followThrough applies a follow-up just posted. A replace or a stop asks
// the runs it waits on to stop, with its reason as the cause; a stop tells
// the people it tags, as nothing else will; and whatever is ready is
// released at once, for work that ended while the message was posted. A
// post repeated by its surface's id applies it again, which changes nothing
// already done.
func (p *Platform) followThrough(ctx context.Context, person auth.Principal, tree *projects.Tree, th threads.Thread, m threads.Message, repeated bool) (Posted, error) {
	tenant, f := person.Tenant, m.Followup
	if f.Outcome != threads.FollowupQueue {
		cause := f.Outcome
		if f.Reason != "" {
			cause += ": " + f.Reason
		}
		for _, id := range f.Waiting {
			r, err := p.Runs.Get(tenant, id)
			if errors.Is(err, runs.ErrNotFound) {
				continue
			}
			if err != nil {
				return Posted{}, err
			}
			if r.Status.Terminal() || p.MayStop(person, r) != nil {
				continue
			}
			if err := p.requestStop(ctx, tenant, id, cause, person.Subject); err != nil {
				return Posted{}, err
			}
		}
	}
	var notified []string
	if f.Outcome == threads.FollowupStop && !repeated {
		notified = p.notifyTagged(tenant, tree, th, m, slices.Clone(m.Mentions))
	}
	if err := p.releaseFollowups(ctx, tenant, th.ID); err != nil {
		return Posted{}, err
	}
	if now, err := p.Threads.Message(tenant, th.ID, m.ID); err == nil {
		m = now
	}
	if now, err := p.Threads.Get(tenant, th.ID); err == nil {
		th = now
	}
	out := p.posted(tenant, th, m)
	out.Rule, out.Repeated = threads.RuleFollowup, repeated
	if notified != nil {
		out.Notified = notified
	}
	return out, nil
}

// releaseFollowups starts what a thread's pending follow-ups call for,
// oldest first, each once it is ready: every run it waits on has ended, and
// no run answering an earlier message is working, so queued messages run one
// after another in the order they were posted. A later one waits for an
// earlier one that is not ready.
//
// It is a durable completion step of every top-level run (ended), retried
// until it succeeds, and it is tried as a follow-up is posted. Repeating it
// starts nothing twice: a released message is marked handled, and the runs
// it starts are named by the message (answerTagID), so a release retried
// after a worker restarts, or racing another process, joins the runs
// already started.
func (p *Platform) releaseFollowups(ctx context.Context, tenant, thread string) error {
	if thread == "" {
		return nil
	}
	pending, err := p.Threads.Pending(tenant, thread, maxPendingFollowups)
	if errors.Is(err, threads.ErrNotFound) || (err == nil && len(pending) == 0) {
		return nil
	}
	if err != nil {
		return err
	}
	th, err := p.Threads.Get(tenant, thread)
	if err != nil {
		return err
	}
	turns, err := p.Runs.Conversation(tenant, thread)
	if err != nil && !errors.Is(err, runs.ErrNotFound) {
		return err
	}
	// working is each top-level run still working, and the message it
	// answers.
	working := map[string]string{}
	for _, r := range turns {
		if r.Parent == "" && !r.Status.Terminal() {
			working[r.ID] = r.Message
		}
	}
	for _, m := range pending {
		if m.Followup.Outcome != threads.FollowupStop && !followupReady(m, working) {
			return nil
		}
		started, err := p.releaseFollowup(ctx, tenant, th, m)
		if err != nil {
			return err
		}
		for _, r := range started {
			if !r.Status.Terminal() {
				working[r.ID] = m.ID
			}
		}
	}
	return nil
}

// followupReady reports whether a follow-up may start: none of the runs it
// waits on, and no run answering a message posted before it, is working.
func followupReady(m threads.Message, working map[string]string) bool {
	for _, id := range m.Followup.Waiting {
		if _, ok := working[id]; ok {
			return false
		}
	}
	seq := messageSeq(m.ID)
	for _, answers := range working {
		if n := messageSeq(answers); n > 0 && n < seq {
			return false
		}
	}
	return true
}

// messageSeq is a message id's place in its thread, 0 for anything else.
func messageSeq(id string) int {
	n, err := strconv.Atoi(strings.TrimPrefix(id, "m-"))
	if err != nil || !strings.HasPrefix(id, "m-") || n < 1 {
		return 0
	}
	return n
}

// releaseFollowup carries out one ready follow-up and marks it handled. A
// stop starts nothing.
func (p *Platform) releaseFollowup(ctx context.Context, tenant string, th threads.Thread, m threads.Message) ([]runs.Run, error) {
	var started []runs.Run
	if m.Followup.Outcome != threads.FollowupStop {
		var err error
		if started, err = p.startFollowup(ctx, tenant, th, m); err != nil {
			return nil, err
		}
	}
	if _, err := p.Threads.HandleFollowup(tenant, th.ID, m.ID); err != nil {
		return nil, err
	}
	return started, nil
}

// startFollowup carries out a queued or replacing message for its poster as
// they stand now, never as they stood when they posted it: their roles are
// the directory's and the policy's (invitationReader), and they must still
// start runs and post where the thread is. One who may no longer starts
// nothing. A verb the poster wrote first is left out of what runs.
func (p *Platform) startFollowup(ctx context.Context, tenant string, th threads.Thread, m threads.Message) ([]runs.Run, error) {
	who := p.invitationReader(tenant, m.By)
	refused := func(why error) ([]runs.Run, error) {
		p.log.Info("platform: a follow-up started nothing, as its poster may no longer post there", "tenant", tenant, "thread", th.ID, "message", m.ID, "why", why.Error())
		return nil, nil
	}
	if !who.Can(auth.RunsWrite) {
		return refused(ErrForbidden)
	}
	keeps, err := p.mayPost(who, Place{Project: th.Project, Thread: th.ID}, projects.Member)
	if deniedPost(err) {
		return refused(err)
	}
	if err != nil {
		return nil, err
	}
	ask := m
	if f, ok := join.Forced(m.Text); ok && f.Outcome == m.Followup.Outcome {
		ask.Text = f.Text
	}
	via := m.Via
	if via == threads.ViaConsole {
		via = ""
	}
	out, err := p.carryOut(ctx, who, tenant, p.Projects.Tree(tenant), th, ask, via, false, false, keeps, p.Seer(who).Agent)
	if deniedPost(err) {
		return refused(err)
	}
	if err != nil {
		return nil, err
	}
	return out.Runs, nil
}

// deniedPost reports an error that says a person may not post where a
// message is, as against one that may pass.
func deniedPost(err error) bool {
	return errors.Is(err, ErrForbidden) || errors.Is(err, projects.ErrForbidden) || errors.Is(err, projects.ErrNotFound) || errors.Is(err, threads.ErrNotFound)
}
