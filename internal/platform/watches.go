package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	saigetypes "github.com/urmzd/saige/agent/types"
	"github.com/urmzd/saige/flow/event"
	"github.com/urmzd/saige/flow/runtime"
	"github.com/urmzd/saige/flow/session"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/inbox"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
	"github.com/urmzd/humagen/pkg/watches"
)

// Watch and notify: "tell me when the deploy is green".
//
// A person asks an agent, in a conversation, to tell them when something
// happens. The agent calls watch_start with the condition, how to check it,
// how often and for how long (pkg/watches keeps it). The leader checks each
// watch that is due (checkWatches): it starts one run of the same agent,
// acting for the same person, in a private conversation kept for the watch's
// checks, so each check sees what the ones before it found and none of them
// crowd the conversation that asked. A check answers NONE while the
// condition does not hold. Any other answer is what happened: the watch ends
// and the answer goes to the person's inbox, which their notification
// settings deliver (console, email). A watch that runs out of time or keeps
// failing tells the person so.
//
// notify is the other half: an agent tells the person something now, as it
// finishes work they asked to hear about.

// The tools.
const (
	ToolNotify     = "notify"
	ToolWatchStart = "watch_start"
	ToolWatchList  = "watch_list"
	ToolWatchStop  = "watch_stop"
)

// WatchTools are the tools for telling a person something, now or when it
// happens.
func WatchTools() []string {
	return []string{ToolNotify, ToolWatchStart, ToolWatchList, ToolWatchStop}
}

// metaWatch names the watch a check run checks. Only the platform writes it:
// a check is believed only as the run its watch began (watches.Store.Settle).
const metaWatch = "humagen.watch"

// watchEvery is how often the leader looks for watches that are due.
const watchEvery = 30 * time.Second

// watchStale is how long a check run may sit unsettled after it ended before
// the sweep settles it from the run's record.
const watchStale = time.Minute

// MaxNotifySubject and MaxNotifyText bound what notify sends.
const (
	MaxNotifySubject = 200
	MaxNotifyText    = 4000
)

// watchCaller is who a watch tool acts for: the person the run acts for, in
// the conversation the run is in. An evaluation, a run that acts for nobody,
// and a watch's own check may not start watches or send notices.
func (p *Platform) watchCaller(req runtime.Request) (auth.Principal, runs.Run, threads.Thread, error) {
	run, err := p.runRecordOf(req.Tenant, req.RunID)
	if err != nil {
		return auth.Principal{}, runs.Run{}, threads.Thread{}, err
	}
	if IsEvaluation(run) || run.Origin == runs.OriginSystem {
		return auth.Principal{}, run, threads.Thread{}, fmt.Errorf("%w: an evaluation or housekeeping run tells nobody anything", auth.ErrForbidden)
	}
	who := p.byPolicy(req.Tenant, personOf(run.Actor))
	if who.Subject == "" {
		return who, run, threads.Thread{}, fmt.Errorf("%w: this run acts for nobody, so there is nobody to tell", auth.ErrForbidden)
	}
	th, err := p.ThreadRecord(req.Tenant, run.Thread)
	if err != nil {
		th = threads.Thread{ID: run.Thread, Project: run.Project}
	}
	return who, run, th, nil
}

// notifyTool tells the person the run acts for something now.
func (p *Platform) notifyTool(req runtime.Request) saigetypes.Tool {
	params := schema([]string{"subject"}, map[string]string{
		"subject": fmt.Sprintf("One line: what happened, at most %d characters", MaxNotifySubject),
		"text":    fmt.Sprintf("Details, at most %d characters", MaxNotifyText),
	})
	return &saigetypes.ToolFunc{Def: saigetypes.ToolDef{
		Name:        ToolNotify,
		Description: "Notify the person you work for now: it lands in their inbox and reaches them wherever their notification settings say (console, email). Use it when they asked to be told when something finishes or happens. Not for progress updates.",
		Parameters:  params,
	}, Fn: func(_ context.Context, args map[string]any) (string, error) {
		who, run, th, err := p.watchCaller(req)
		if err != nil {
			return "", err
		}
		subject := strings.Join(strings.Fields(arg(args, "subject")), " ")
		text := strings.TrimSpace(arg(args, "text"))
		if subject == "" {
			return "", errors.New("say what happened in subject")
		}
		subject, text = clipRunes(subject, MaxNotifySubject), clipRunes(text, MaxNotifyText)
		if text == "" {
			text = subject
		}
		sum := sha256.Sum256([]byte(subject + "\x00" + text))
		key := "notify:" + run.ID + ":" + hex.EncodeToString(sum[:8])
		err = p.completionInbox(req.Tenant, key, inbox.Message{
			Kind: inbox.KindResult, To: who.Subject, Subject: subject,
			From: inbox.From{Kind: "agent", Name: run.Agent}, Severity: inbox.Info,
			Source: inbox.Source{Run: run.ID, Thread: th.ID, Agent: run.Agent, Project: th.Project},
			Parts:  []inbox.Part{{Type: inbox.PartText, State: inbox.PartInfo, Text: text}},
		}, p.now())
		if err != nil {
			return "", err
		}
		return "Sent to " + p.Names(req.Tenant).Of(who.Subject) + ".", nil
	}}
}

// parseSpan reads a span such as "15m", "2h" or "3d". Empty is zero.
func parseSpan(s string) (time.Duration, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return 0, nil
	}
	if days, ok := strings.CutSuffix(s, "d"); ok {
		n, err := strconv.Atoi(days)
		if err != nil || n < 1 {
			return 0, fmt.Errorf("%w: %q is not a number of days", watches.ErrInvalid, s)
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("%w: %q is not a span such as 15m, 2h or 3d", watches.ErrInvalid, s)
	}
	return d, nil
}

// watchView is what the tools say about a watch.
type watchView struct {
	ID        string    `json:"id"`
	Condition string    `json:"condition"`
	How       string    `json:"how,omitempty"`
	State     string    `json:"state"`
	Every     string    `json:"every"`
	Until     time.Time `json:"until"`
	Next      time.Time `json:"next_check,omitzero"`
	Checks    int       `json:"checks"`
	Session   string    `json:"session"`
	Thread    string    `json:"checks_session,omitempty"`
	Result    string    `json:"result,omitempty"`
}

func viewOf(w watches.Watch) watchView {
	v := watchView{ID: w.ID, Condition: w.Condition, How: w.How, State: string(w.State), Every: w.Every.String(), Until: w.Until, Checks: w.Checks, Session: w.Session, Thread: w.Thread, Result: w.Result}
	if w.State == watches.Watching && w.Running == "" {
		v.Next = w.Next
	}
	return v
}

func marshal(v any) (string, error) {
	b, err := json.Marshal(v)
	return string(b), err
}

// watchStartTool starts a watch for the person the run acts for.
func (p *Platform) watchStartTool(req runtime.Request) saigetypes.Tool {
	params := schema([]string{"condition"}, map[string]string{
		"condition": "What to watch for, as the person would say it: \"the release workflow on main passes\"",
		"how":       "How to check it with your tools, so a later check needs no guessing: which tool, which repository, which URL",
		"every":     fmt.Sprintf("How often to check: %s to %s, such as 15m or 1h. Defaults to %s", watches.MinEvery, watches.MaxEvery, watches.DefaultEvery),
		"for":       fmt.Sprintf("How long to keep watching: up to 7d, such as 6h or 2d. Defaults to %s", watches.DefaultFor),
	})
	return &saigetypes.ToolFunc{Def: saigetypes.ToolDef{
		Name:        ToolWatchStart,
		Description: "Watch for something in the background and notify the person when it happens, when they ask \"tell me when ...\". You check it again every interval, in a conversation of its own, until it holds or the time runs out. Say in your answer what you are watching and for how long.",
		Parameters:  params,
	}, Fn: func(_ context.Context, args map[string]any) (string, error) {
		who, run, th, err := p.watchCaller(req)
		if err != nil {
			return "", err
		}
		if wire.Meta(run.Tag())[metaWatch] != "" {
			return "", fmt.Errorf("%w: a check does not start another watch", auth.ErrForbidden)
		}
		if th.Project == "" {
			return "", fmt.Errorf("%w: this conversation is in no project, so its checks have nowhere to run", watches.ErrInvalid)
		}
		if err := p.MayPost(who, Place{Project: th.Project, Thread: th.ID}); err != nil {
			return "", err
		}
		every, err := parseSpan(arg(args, "every"))
		if err != nil {
			return "", err
		}
		span, err := parseSpan(arg(args, "for"))
		if err != nil {
			return "", err
		}
		w, err := p.Watches.Create(req.Tenant, watches.Spec{
			Owner: who.Subject, Agent: req.Agent(), Session: th.ID, Project: th.Project,
			Condition: arg(args, "condition"), How: arg(args, "how"), Every: every, For: span, CreatedBy: run.ID,
		}, p.now())
		if err != nil {
			return "", err
		}
		return marshal(viewOf(w))
	}}
}

// watchListTool lists the person's watches still watching.
func (p *Platform) watchListTool(req runtime.Request) saigetypes.Tool {
	return &saigetypes.ToolFunc{Def: saigetypes.ToolDef{
		Name:        ToolWatchList,
		Description: "List the watches you keep for the person you work for: what each watches for, when it next checks and when it stops.",
		Parameters:  schema(nil, nil),
	}, Fn: func(context.Context, map[string]any) (string, error) {
		who, _, _, err := p.watchCaller(req)
		if err != nil {
			return "", err
		}
		all, err := p.Watches.Active(req.Tenant)
		if err != nil {
			return "", err
		}
		out := []watchView{}
		for _, w := range all {
			if w.Owner == who.Subject {
				out = append(out, viewOf(w))
			}
		}
		return marshal(out)
	}}
}

// watchStopTool stops one of the person's watches.
func (p *Platform) watchStopTool(req runtime.Request) saigetypes.Tool {
	return &saigetypes.ToolFunc{Def: saigetypes.ToolDef{
		Name:        ToolWatchStop,
		Description: "Stop one of the person's watches, when they no longer want to be told.",
		Parameters:  schema([]string{"id"}, map[string]string{"id": "The watch's id, from watch_list"}),
	}, Fn: func(_ context.Context, args map[string]any) (string, error) {
		who, _, _, err := p.watchCaller(req)
		if err != nil {
			return "", err
		}
		w, err := p.StopWatch(who, strings.TrimSpace(arg(args, "id")))
		if err != nil {
			return "", err
		}
		return marshal(viewOf(w))
	}}
}

// StopWatch stops one of a person's own watches. Another person's, or one in
// another tenant, is not found.
func (p *Platform) StopWatch(who auth.Principal, id string) (watches.Watch, error) {
	w, err := p.Watches.Get(who.Tenant, id)
	if err != nil || w.Owner != who.Subject {
		return watches.Watch{}, fmt.Errorf("%w: watch %q", watches.ErrNotFound, id)
	}
	if w.State != watches.Watching {
		return w, nil
	}
	return p.Watches.Stop(who.Tenant, id, p.now())
}

// runWatches is the leader's loop over watches.
func (p *Platform) runWatches(ctx context.Context) error {
	tick := time.NewTicker(watchEvery)
	defer tick.Stop()
	pruned := time.Time{}
	for {
		p.checkWatches(ctx)
		if now := p.now(); now.Sub(pruned) > time.Hour {
			pruned = now
			p.pruneWatches()
		}
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		}
	}
}

// checkWatches starts every check that is due, ends watches with no checks
// left, and settles checks whose run ended unobserved.
func (p *Platform) checkWatches(ctx context.Context) {
	err := p.Watches.Tenants(func(tenant string) error {
		all, err := p.Watches.Active(tenant)
		if err != nil {
			p.log.Warn("platform: watches could not be read; the next pass tries again", "tenant", tenant, "error", err)
			return nil
		}
		for _, w := range all {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			p.checkWatch(ctx, tenant, w)
		}
		return nil
	})
	if err != nil && ctx.Err() == nil {
		p.log.Warn("platform: watch tenants could not be read; the next pass tries again", "error", err)
	}
}

func (p *Platform) checkWatch(ctx context.Context, tenant string, w watches.Watch) {
	now := p.now()
	switch {
	case w.Running != "":
		// The run's end settles it (settleWatchRun). One that ended while
		// no process observed it is settled here.
		run, err := p.Runs.Get(tenant, w.Running)
		if errors.Is(err, runs.ErrNotFound) {
			ended, err := p.Watches.Abandon(tenant, w.ID, w.Running, now)
			_ = p.watchEnded(tenant, ended, err)
		} else if err == nil && run.Status.Terminal() && now.Sub(run.UpdatedAt) > watchStale {
			_ = p.settleWatchRun(run)
		}
	case w.Over(now):
		ended, err := p.Watches.Expire(tenant, w.ID, now)
		_ = p.watchEnded(tenant, ended, err)
	case w.Due(now):
		p.startWatchCheck(ctx, tenant, w, now)
	}
}

// watchCheckTag is the id of a watch's nth check: retried, it joins the run
// already started.
func watchCheckTag(id string, n int) string { return "watch-" + id + "-" + strconv.Itoa(n) }

// startWatchCheck starts one check of a watch: a run of its agent, for its
// person, in the conversation kept for its checks.
func (p *Platform) startWatchCheck(ctx context.Context, tenant string, w watches.Watch, now time.Time) {
	n := w.Checks + 1
	runID := runtime.RunID(tenant, watchCheckTag(w.ID, n))
	w, err := p.Watches.Begin(tenant, w.ID, runID, now)
	if err != nil {
		return
	}
	fail := func(why string, err error) {
		p.log.Warn("platform: a watch check could not start", "tenant", tenant, "watch", w.ID, "why", why, "error", err)
		ended, err := p.Watches.Abandon(tenant, w.ID, runID, p.now())
		_ = p.watchEnded(tenant, ended, err)
	}
	who := p.byPolicy(tenant, w.Owner)
	if err := p.MayPost(who, Place{Project: w.Project}); err != nil {
		fail("the person may no longer work in the project", err)
		return
	}
	thread := w.Thread
	if thread == "" {
		if thread, err = p.watchThread(ctx, tenant, w); err != nil {
			fail("its conversation could not be made", err)
			return
		}
		if err := p.Watches.SetThread(tenant, w.ID, thread); err != nil {
			p.log.Warn("platform: a watch's conversation was not recorded; the next check finds it again", "watch", w.ID, "error", err)
		}
	}
	ask := watchAsk(p.Names(tenant).Of(w.Owner), w, n)
	meta := map[string]string{runs.MetaThread: thread, runs.MetaProject: w.Project, runs.MetaAsk: ask, metaWatch: w.ID}
	_, err = p.Invoke(ctx, wire.NewEvent(wire.Raise{
		ID: watchCheckTag(w.ID, n), Tenant: tenant, Agent: w.Agent, Origin: runs.OriginAPI,
		Actor: p.namedActor(tenant, event.Actor{User: w.Owner}), Context: meta,
		Text: p.turnContext(tenant, w.Project, "", w.Agent, w.Owner, ask, meta),
	}))
	if err != nil {
		fail("the run was refused", err)
	}
}

// watchAsk is what a check asks its agent.
func watchAsk(person string, w watches.Watch, n int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "You are watching for something %s asked to be told about, in conversation %s.\n\n", person, w.Session)
	fmt.Fprintf(&b, "Watch for: %s\n", w.Condition)
	if w.How != "" {
		fmt.Fprintf(&b, "How to check: %s\n", w.How)
	}
	fmt.Fprintf(&b, "This is check %d. Checks run every %s until %s.\n\n", n, w.Every, w.Until.UTC().Format(time.RFC3339))
	b.WriteString("Check now with your tools. Ask nobody anything.\n")
	b.WriteString("If it has not happened yet, answer exactly NONE.\n")
	b.WriteString("If it has, answer with one line saying what happened, then the details that matter. That answer is sent to them and the watch ends.")
	return b.String()
}

// watchThread opens the private conversation a watch's checks are filed in:
// one session per watch, keyed (watch://<id>, agent), so every check
// continues it, in the watch's project, read by its person alone.
func (p *Platform) watchThread(ctx context.Context, tenant string, w watches.Watch) (string, error) {
	ch, err := p.Projects.Channel(tenant, w.Project, projects.General)
	if err != nil {
		return "", err
	}
	if ch.Archived {
		return "", fmt.Errorf("platform: the channel %q is archived", ch.Name)
	}
	rec, err := p.Sessions.Open(ctx, tenant, session.Open{Parent: "watch://" + w.ID, Agent: w.Agent, Label: "checks", Channel: wire.Channel(tenant)})
	if err != nil {
		return "", err
	}
	at := p.now()
	th := threads.Thread{
		ID: rec.ID, Project: w.Project, Channel: ch.Name, Title: firstWords("Watching: "+w.Condition, threads.MaxTitle),
		Visibility: threads.Private, CreatedBy: w.Owner, Default: w.Agent, Agents: []string{w.Agent},
		Participants: []threads.Participant{{Kind: threads.Person, Name: w.Owner, Owner: true, At: at}},
		Session:      &threads.SessionIdentity{Agent: w.Agent, Parent: rec.Key.Parent, Label: rec.Key.Label},
	}
	if _, err := p.Threads.Create(tenant, th, at); err != nil && !errors.Is(err, threads.ErrExists) {
		return "", err
	}
	return rec.ID, nil
}

// settleWatchRun records how a watch's check ended, when run is one: a
// completion step, and the sweep's for a run that ended unobserved.
func (p *Platform) settleWatchRun(run runs.Run) error {
	return p.settleWatch(run, wire.Meta(run.Tag())[metaWatch])
}

// settleWatch settles watch id with how its check run ended.
func (p *Platform) settleWatch(run runs.Run, id string) error {
	if id == "" || run.Origin != runs.OriginAPI || !run.Status.Terminal() {
		return nil
	}
	o := watches.Outcome{Failed: run.Status != runs.Completed}
	if !o.Failed {
		subject, text, post := announcementOf(run.Answer)
		o.Met = post
		o.Result = strings.TrimSpace(subject + "\n" + text)
	}
	w, err := p.Watches.Settle(run.Tenant, id, run.ID, o, p.now())
	if errors.Is(err, watches.ErrBusy) || errors.Is(err, watches.ErrNotFound) {
		// Settled already, or stopped: nothing to tell.
		return nil
	}
	return p.watchEnded(run.Tenant, w, err)
}

// watchEnded tells a watch's person how it ended, when it did on its own.
// A watch a person or an agent stopped tells nobody.
func (p *Platform) watchEnded(tenant string, w watches.Watch, err error) error {
	if err != nil || w.State == watches.Watching || w.State == watches.Stopped {
		if errors.Is(err, watches.ErrBusy) {
			return nil
		}
		return err
	}
	subject, text := "", ""
	switch w.State {
	case watches.Met:
		subject, text, _ = strings.Cut(w.Result, "\n")
		if text = strings.TrimSpace(text); text == "" {
			text = subject
		}
	case watches.Expired:
		subject = clipRunes("Stopped watching: "+w.Condition, MaxNotifySubject)
		text = fmt.Sprintf("It did not happen in %d checks, up to %s.", w.Checks, w.Until.UTC().Format(time.RFC1123))
	case watches.Failed:
		subject = clipRunes("Stopped watching: "+w.Condition, MaxNotifySubject)
		text = fmt.Sprintf("The last %d checks could not finish. Ask again to keep watching.", watches.MaxFailures)
	}
	return p.completionInbox(tenant, "watch:"+w.ID, inbox.Message{
		Kind: inbox.KindResult, To: w.Owner, Subject: subject,
		From: inbox.From{Kind: "agent", Name: w.Agent}, Severity: inbox.Info,
		Source: inbox.Source{Run: w.CreatedBy, Thread: w.Session, Agent: w.Agent, Project: w.Project},
		Parts:  []inbox.Part{{Type: inbox.PartText, State: inbox.PartInfo, Text: text}},
	}, w.EndedAt)
}

// pruneWatches drops ended watches past their retention.
func (p *Platform) pruneWatches() {
	cut := p.now().Add(-watches.Retention)
	_ = p.Watches.Tenants(func(tenant string) error {
		if _, err := p.Watches.Prune(tenant, cut); err != nil {
			p.log.Warn("platform: ended watches could not be pruned", "tenant", tenant, "error", err)
		}
		return nil
	})
}
