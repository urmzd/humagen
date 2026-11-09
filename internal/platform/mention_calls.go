package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/orchagent/flow/event"
	"github.com/urmzd/orchagent/flow/runtime"
	"github.com/urmzd/orchagent/flow/turn"
)

// A message that mentions several agents gets one answer, the entry agent's.
// Before that run's first model step the platform makes the agent calls the
// mentions asked for itself: each is an agent call of the run like any other
// (a child run through pkg/call, keyed by the call's id and carrying it as
// runs.Run.CallID), recorded on the run as a call to the agent tool and its
// result, so activity shows it where the entry agent's own calls show. The
// model is never shown a tool call it did not make: what each agent said
// reaches it as content of the person's turn, with the answer it is to
// write: each agent's result as given, under its own heading, then a short
// synthesis if one helps.
//
// The run's final text is the model's own, as for every run: the platform
// does not rewrite it, so that each result is repeated as given rests on
// what the model is told here.

// metaMentioned names, on the entry agent's run that answers a message, the
// agents the message mentions, in the order written. It is private: it is
// kept with the run and never shown to a model.
const metaMentioned = wire.PrivateMetaPrefix + "humagen.mentioned"

// maxMentionedTold bounds how much of each mentioned agent's answer the entry
// agent is sent, in bytes. One cut short says so.
const maxMentionedTold = 16000

// mentionedCall is one agent call the platform made for a mention, and how
// it ended.
type mentionedCall struct {
	agent  string
	id     string // the call's id on the run's events
	child  runs.Run
	answer string
	err    error
}

// mentionCallID is the id the i-th mentioned agent's call has on its run's
// events. Like a provider's call id it never leaves the platform: the API
// names the call htu_<run>_<n>.
func mentionCallID(i int) string { return fmt.Sprintf("mention_%d", i+1) }

// mentionedIn are the agents a run's metadata says its message mentioned.
func mentionedIn(meta map[string]string) []string {
	var out []string
	for _, a := range strings.Split(meta[metaMentioned], ",") {
		if a = strings.TrimSpace(a); a != "" {
			out = append(out, a)
		}
	}
	return out
}

// callMentioned makes the agent calls a run's message asked for by mention,
// before the run's first model step, and waits for them. told is what each
// agent said, as the entry agent is sent it ahead of the person's message,
// with how to answer. A call that is refused or fails is said so in its
// place and never fails the run. stopped is set when the run was asked to
// stop while it waited: the calls still running are stopped with it.
//
// A turn that is retried makes the same calls under the same keys, so it
// joins the runs it started.
func (p *Platform) callMentioned(ctx context.Context, req runtime.Request, emit func(context.Context, event.Body) error) (told string, stopped *turn.Stop, err error) {
	agents := mentionedIn(req.Meta)
	if len(agents) == 0 || req.RunID == "" {
		return "", nil, nil
	}
	if emit == nil {
		emit = func(context.Context, event.Body) error { return nil }
	}
	run, err := p.runRecordOf(req.Tenant, req.RunID)
	if err != nil {
		return "", nil, err
	}
	calls := make([]mentionedCall, len(agents))
	for i, agent := range agents {
		calls[i] = mentionedCall{agent: agent, id: mentionCallID(i)}
		args, _ := json.Marshal(AgentCall{Agent: agent, Title: mentionTitle(run.Text), Brief: mentionBrief(run, agent, agents)})
		if err := emit(ctx, event.ToolCall{ID: calls[i].id, Name: ToolAgent, Phase: event.PhaseExecuting, Arguments: args, ArgumentsBytes: len(args)}); err != nil {
			return "", nil, err
		}
	}
	// One place among the runs that wait on a run they called is this run's,
	// for all of its mentions: they are waited on one after another.
	if int(p.waiting.Add(1)) >= p.slots() {
		p.waiting.Add(-1)
		for i := range calls {
			calls[i].err = fmt.Errorf("%w, so %s cannot be asked right now", ErrBusy, calls[i].agent)
		}
	} else {
		defer p.waiting.Add(-1)
		// Every call starts before any is waited on, so the agents work at
		// the same time.
		for i := range calls {
			c := &calls[i]
			id, err := p.toolUseIDFor(ctx, req.Tenant, req.RunID, c.id)
			if err != nil {
				c.err = err
				continue
			}
			c.child, c.err = p.call(ctx, req.Tenant, req.RunID, call.Request{Key: id, Agent: c.agent, Text: mentionBrief(run, c.agent, agents), Mode: call.Subagent, CallID: id}, true, nil)
			if c.err == nil {
				data, _ := json.Marshal(calledRun{Run: c.child.ID, Agent: c.child.Agent, Lifecycle: c.child.Lifecycle, ToolCallID: c.id})
				_ = emit(ctx, event.Action{Name: ToolAgent, Summary: "Helper started", Address: "run:" + c.child.ID, Data: data})
			}
		}
		for i := range calls {
			c := &calls[i]
			if c.err != nil {
				continue
			}
			if stopped == nil {
				c.answer, stopped, c.err = p.awaitMentioned(ctx, run, c.child)
			}
			if stopped != nil {
				if err := p.requestStop(context.WithoutCancel(ctx), req.Tenant, c.child.ID, attachedCause("stopped", run), cmpOr(stopped.By, "system")); err != nil {
					p.log.Warn("platform: a mentioned agent's run could not be asked to stop", "tenant", req.Tenant, "run", c.child.ID, "error", err)
				}
				c.err = cmpErr(c.err, fmt.Errorf("%s was stopped", c.agent))
				continue
			}
			if err := ctx.Err(); err != nil {
				return "", nil, err
			}
			// What it spent is the caller's spending too, however it ended.
			if done, err := p.Runs.Get(req.Tenant, c.child.ID); err == nil && done.Usage != nil {
				_ = emit(ctx, event.UsageChild{AccountingID: event.ChildRunAccountingID(done.ID), RunID: done.ID, Agent: done.Agent, Depth: 1, Usage: done.Usage.Total})
			}
		}
	}
	var sent strings.Builder
	for _, c := range calls {
		done := event.ToolDone{ID: c.id, Name: ToolAgent, Result: c.answer}
		if c.err != nil {
			done.Result, done.Error = "", c.err.Error()
		}
		if err := emit(ctx, done); err != nil {
			return "", nil, err
		}
		text := strings.TrimSpace(c.answer)
		switch {
		case c.err != nil:
			fmt.Fprintf(&sent, "<answer agent=%q failed=\"true\">\n@%s did not answer: %s.\n</answer>\n", c.agent, c.agent, strings.TrimSuffix(refusalOf(c.err), "."))
		case len(text) > maxMentionedTold:
			fmt.Fprintf(&sent, "<answer agent=%q cut=\"true\">\n%s\n</answer>\n", c.agent, clipText(text, maxMentionedTold))
		default:
			fmt.Fprintf(&sent, "<answer agent=%q>\n%s\n</answer>\n", c.agent, text)
		}
	}
	if stopped != nil {
		return "", stopped, nil
	}
	return "<mentioned_agents>\nThe person mentioned these agents, so each was asked this message before you. Their answers:\n\n" +
		sent.String() +
		"</mentioned_agents>\n\n" + mentionedAnswer, nil, nil
}

// mentionedAnswer tells the entry agent how to answer a message whose
// mentioned agents have answered.
const mentionedAnswer = "Write the one answer the person reads. For each agent above, in that order, write a heading with its name (\"## @name\") and under it that agent's answer exactly as it is given: do not shorten, reword, reorder or correct it. Under the heading of an agent that did not answer, say that it did not and why; where an answer was cut, say it was cut for length. After the last, add a short synthesis only if it helps: where they agree or differ, and what to do next. Do not call those agents again for the same thing."

// awaitMentioned waits for a mentioned agent's run to end and returns its
// answer. stopped is set, with no answer, when the caller's turn was asked to
// stop first.
func (p *Platform) awaitMentioned(ctx context.Context, caller, child runs.Run) (answer string, stopped *turn.Stop, err error) {
	stop := runtime.StopSignal(ctx)
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		r, err := p.Runs.Get(caller.Tenant, child.ID)
		if err != nil {
			return "", nil, err
		}
		switch r.Status {
		case runs.Completed:
			return r.Answer, nil, nil
		case runs.Failed:
			return "", nil, fmt.Errorf("%s failed: %s", r.Agent, oneLine(r.Error, 300))
		case runs.Stopped:
			return "", nil, fmt.Errorf("%s was stopped: %s", r.Agent, r.Cause)
		}
		select {
		case s := <-stop:
			return "", &s, nil
		case <-ctx.Done():
			// A turn stopped past its grace period is cancelled with why;
			// one whose worker went away is not, and its next attempt joins
			// the same run.
			if cause := context.Cause(ctx); errors.Is(cause, runtime.ErrStopped) {
				s := turn.Stop{Reason: "stopped"}
				var asked *runtime.StoppedError
				if errors.As(cause, &asked) {
					s = turn.Stop{Reason: asked.Reason, By: asked.By}
				}
				return "", &s, nil
			}
			return "", nil, ctx.Err()
		case <-tick.C:
		}
	}
}

// mentionTitle is the title of a mentioned agent's call: the start of the
// message, as its card shows it.
func mentionTitle(text string) string {
	return cmpOr(firstWords(text, MaxAgentCallTitle), "Answer a mention")
}

// mentionBrief is what a mentioned agent is asked: the person's message, and
// that it was asked by mention beside the others.
func mentionBrief(run runs.Run, agent string, all []string) string {
	var others []string
	for _, a := range all {
		if a != agent {
			others = append(others, "@"+a)
		}
	}
	who := cmpOr(run.Actor.Display, "A person")
	return fmt.Sprintf("%s mentioned you in the message below, together with %s. Answer your part of it. Your answer is shown to them as you give it, under your name.\n\n<message>\n%s\n</message>", who, strings.Join(others, ", "), run.Text)
}

func cmpErr(a, b error) error {
	if a != nil {
		return a
	}
	return b
}
