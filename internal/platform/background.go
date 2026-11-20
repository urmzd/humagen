package platform

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	saigetypes "github.com/urmzd/saige/agent/types"
	"github.com/urmzd/saige/flow/event"
	"github.com/urmzd/saige/flow/runtime"
	"github.com/urmzd/saige/flow/session"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/background"
	"github.com/urmzd/humagen/pkg/inbox"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
)

// Background work: what an agent does for a person while nobody watches.
//
// A person asks an agent, in a conversation, to keep at something: tell me
// when the deploy is green (a watch), keep going until every test passes (a
// goal), or run this long search while I do something else (a task). The
// agent starts it with a tool, and pkg/background keeps it. Each is a loop of
// runs of the same agent, acting for the same person:
//
//   - A watch's checks run every interval in a private session kept for the
//     watch, so each sees what the ones before found and none crowd the
//     conversation that asked. A check answers NONE until the condition
//     holds; any other answer is what happened.
//   - A goal's turns run in the session that set it, the next as soon as the
//     last ends, until a turn's answer ends with GOAL MET, or GOAL BLOCKED and
//     why, or its turns or time run out.
//   - A task is one run in a private session of its own: a running thread
//     the person can open, whose answer is sent to them when it ends.
//
// A run's end settles it (a completion step, settleBackgroundRun); the
// leader's sweep (checkBackground) starts what is due, ends what ran out, and
// settles a run that ended unobserved. When one ends on its own, the person
// is told in their inbox, which their notification settings deliver
// (console, email), and a Slack app that maps them (background_report.go).
// One that a person or an agent stopped tells nobody.
//
// notify is the plain case: an agent tells the person something now, as it
// finishes work they asked to hear about.

// The tools.
const (
	ToolNotify          = "notify"
	ToolWatchStart      = "watch_start"
	ToolGoalStart       = "goal_start"
	ToolTaskStart       = "task_start"
	ToolBackgroundList  = "background_list"
	ToolBackgroundStop  = "background_stop"
	goalMet             = "GOAL MET"
	goalBlocked         = "GOAL BLOCKED"
	backgroundEvery     = 30 * time.Second
	backgroundStale     = time.Minute
	maxBackgroundListed = 100
)

// BackgroundTools are the tools for telling a person something and for work
// that goes on without them.
func BackgroundTools() []string {
	return []string{ToolNotify, ToolWatchStart, ToolGoalStart, ToolTaskStart, ToolBackgroundList, ToolBackgroundStop}
}

// metaBackground names the background task a run does. The platform writes
// it on the runs it starts; a run is settled only as the one its task began
// (background.Store.ByRun), so a tag that brings the key settles nothing.
const metaBackground = "humagen.background"

// MaxNotifySubject and MaxNotifyText bound what notify sends.
const (
	MaxNotifySubject = 200
	MaxNotifyText    = 4000
)

// withBackgroundTools offers a turn the background tools, on the request's
// copy of the revision: the pinned definition itself is never changed. They
// are part of what every agent works with, as asking a person is, so no
// definition has to name them: any agent a person talks to can be told "tell
// me when" or "keep going until". Only a turn that can use them is offered
// them: one an agent takes for a person, at the top of its tree. An
// evaluation, a housekeeping run, a run for nobody, a run another agent
// called and a run that is itself background work are offered nothing here,
// and the tools refuse them besides (backgroundCaller, startBackground). A
// workspace still takes them away with its run settings' tool ceiling, which
// is applied after this (boundRunContext).
func (p *Platform) withBackgroundTools(req runtime.Request) runtime.Request {
	run, err := p.runRecordOf(req.Tenant, req.RunID)
	if err != nil || IsEvaluation(run) || run.Origin == runs.OriginSystem || run.Parent != "" || personOf(run.Actor) == "" {
		return req
	}
	if wire.Meta(run.Tag())[metaBackground] != "" {
		return req
	}
	tools := slices.Clone(req.Definition.Spec.Tools)
	for _, name := range BackgroundTools() {
		if !slices.Contains(tools, name) {
			tools = append(tools, name)
		}
	}
	req.Definition.Spec.Tools = tools
	return req
}

// backgroundCaller is who a background tool acts for: the person the run
// acts for, in the conversation the run is in. An evaluation, a housekeeping
// run and a run that acts for nobody tell nobody anything.
func (p *Platform) backgroundCaller(req runtime.Request) (auth.Principal, runs.Run, threads.Thread, error) {
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
		Description: "Notify the person you work for now: it lands in their inbox and reaches them wherever their notification settings say (console, email), and in Slack when they asked from there: keep subject to one line that reads well on its own. Use it when they asked to be told when something finishes or happens. Not for progress updates.",
		Parameters:  params,
	}, Fn: func(_ context.Context, args map[string]any) (string, error) {
		who, run, th, err := p.backgroundCaller(req)
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
			return 0, fmt.Errorf("%w: %q is not a number of days", background.ErrInvalid, s)
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("%w: %q is not a span such as 15m, 2h or 3d", background.ErrInvalid, s)
	}
	return d, nil
}

// BackgroundView is what the tools and the API say about a background task.
type BackgroundView struct {
	ID      string    `json:"id"`
	Kind    string    `json:"kind"`
	Text    string    `json:"text"`
	How     string    `json:"how,omitempty"`
	State   string    `json:"state"`
	Agent   string    `json:"agent"`
	Project string    `json:"project,omitempty"`
	Every   string    `json:"every,omitempty"`
	Until   time.Time `json:"until"`
	// Next is when the next run starts, while one is waited for.
	Next    time.Time `json:"next,omitzero"`
	Working bool      `json:"working"`
	Runs    int       `json:"runs"`
	Limit   int       `json:"limit"`
	// Session is the conversation that asked; Thread the one its runs are in.
	Session   string    `json:"session"`
	Thread    string    `json:"thread,omitempty"`
	Result    string    `json:"result,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	EndedAt   time.Time `json:"ended_at,omitzero"`
}

func viewOf(it background.Item) BackgroundView {
	v := BackgroundView{
		ID: it.ID, Kind: string(it.Kind), Text: it.Text, How: it.How, State: string(it.State), Agent: it.Agent, Project: it.Project,
		Until: it.Until, Working: it.Running != "", Runs: it.Runs, Limit: it.Limit, Session: it.Session, Thread: it.Thread,
		Result: it.Result, CreatedAt: it.CreatedAt, EndedAt: it.EndedAt,
	}
	if it.Every > 0 {
		v.Every = it.Every.String()
	}
	if it.State == background.Active && it.Running == "" {
		v.Next = it.Next
	}
	return v
}

func marshal(v any) (string, error) {
	b, err := json.Marshal(v)
	return string(b), err
}

// startBackground starts a task for the person the run acts for, in the
// run's conversation. A run that is itself background work starts none.
func (p *Platform) startBackground(req runtime.Request, sp background.Spec) (string, error) {
	who, run, th, err := p.backgroundCaller(req)
	if err != nil {
		return "", err
	}
	if wire.Meta(run.Tag())[metaBackground] != "" {
		return "", fmt.Errorf("%w: background work does not start more background work", auth.ErrForbidden)
	}
	if th.Project == "" {
		return "", fmt.Errorf("%w: this conversation is in no project, so its runs have nowhere to go", background.ErrInvalid)
	}
	if err := p.MayPost(who, Place{Project: th.Project, Thread: th.ID}); err != nil {
		return "", err
	}
	sp.Owner, sp.Agent, sp.Session, sp.Project = who.Subject, req.Agent(), th.ID, th.Project
	if sp.Kind == background.Goal {
		sp.CreatedBy, sp.Thread = run.ID, th.ID
	} else {
		sp.CreatedBy = run.ID
	}
	it, err := p.Background.Create(req.Tenant, sp, p.now())
	if err != nil {
		return "", err
	}
	return marshal(viewOf(it))
}

// watchStartTool starts a watch.
func (p *Platform) watchStartTool(req runtime.Request) saigetypes.Tool {
	params := schema([]string{"condition"}, map[string]string{
		"condition": "What to watch for, as the person would say it: \"the release workflow on main passes\"",
		"how":       "How to check it with your tools, so a later check needs no guessing: which tool, which repository, which URL",
		"every":     fmt.Sprintf("How often to check: %s to %s, such as 15m or 1h. Defaults to %s", background.MinEvery, background.MaxEvery, background.DefaultEvery),
		"for":       fmt.Sprintf("How long to keep watching: up to 7d, such as 6h or 2d. Defaults to %s", background.DefaultWatch),
	})
	return &saigetypes.ToolFunc{Def: saigetypes.ToolDef{
		Name:        ToolWatchStart,
		Description: "Watch for something in the background and notify the person when it happens, when they ask \"tell me when ...\". You check it again every interval, in a session of its own, until it holds or the time runs out. Say in your answer what you are watching and for how long.",
		Parameters:  params,
	}, Fn: func(_ context.Context, args map[string]any) (string, error) {
		every, err := parseSpan(arg(args, "every"))
		if err != nil {
			return "", err
		}
		span, err := parseSpan(arg(args, "for"))
		if err != nil {
			return "", err
		}
		return p.startBackground(req, background.Spec{Kind: background.Watch, Text: arg(args, "condition"), How: arg(args, "how"), Every: every, For: span})
	}}
}

// goalStartTool sets a goal on the run's conversation.
func (p *Platform) goalStartTool(req runtime.Request) saigetypes.Tool {
	params := schema([]string{"goal"}, map[string]string{
		"goal":      "What to achieve, as the person would say it: \"every test in ./pkg passes\"",
		"done_when": "How to tell it is achieved, so each turn can check: a command that passes, a page that shows something",
		"turns":     fmt.Sprintf("The most turns to take, this one included: 1 to %d. Defaults to %d", background.MaxTurns, background.DefaultTurns),
		"for":       fmt.Sprintf("How long to keep going: up to %s, such as 2h. Defaults to %s", background.MaxGoalFor, background.DefaultGoal),
	})
	return &saigetypes.ToolFunc{Def: saigetypes.ToolDef{
		Name: ToolGoalStart,
		Description: "Set a goal on this conversation and keep working toward it turn after turn, without the person asking again, when they want something finished rather than one step taken. " +
			"This turn is the first. End every turn's answer, this one included, with a last line " + goalMet + " once the goal is fully achieved, or " + goalBlocked + ": and why when you cannot go on. The person is notified when it ends.",
		Parameters: params,
	}, Fn: func(_ context.Context, args map[string]any) (string, error) {
		span, err := parseSpan(arg(args, "for"))
		if err != nil {
			return "", err
		}
		turns := 0
		if s := strings.TrimSpace(arg(args, "turns")); s != "" {
			if turns, err = strconv.Atoi(s); err != nil || turns < 1 {
				return "", fmt.Errorf("%w: turns is a whole number", background.ErrInvalid)
			}
		}
		return p.startBackground(req, background.Spec{Kind: background.Goal, Text: arg(args, "goal"), How: arg(args, "done_when"), For: span, Turns: turns})
	}}
}

// taskStartTool starts one run in a session of its own.
func (p *Platform) taskStartTool(req runtime.Request) saigetypes.Tool {
	params := schema([]string{"ask"}, map[string]string{
		"ask": "The work to do, complete enough to do without this conversation: what to run or search, where, and what to report",
	})
	return &saigetypes.ToolFunc{Def: saigetypes.ToolDef{
		Name:        ToolTaskStart,
		Description: "Run work in the background, in a session of its own, so this conversation stays free: a long command, a wide search, a slow build. The person is notified with its answer when it finishes and can open its session at any time.",
		Parameters:  params,
	}, Fn: func(_ context.Context, args map[string]any) (string, error) {
		return p.startBackground(req, background.Spec{Kind: background.Task, Text: arg(args, "ask")})
	}}
}

// backgroundListTool lists the person's background work.
func (p *Platform) backgroundListTool(req runtime.Request) saigetypes.Tool {
	return &saigetypes.ToolFunc{Def: saigetypes.ToolDef{
		Name:        ToolBackgroundList,
		Description: "List the background work you keep for the person you work for: watches, goals and tasks, what each is for, whether it is working now, and how ones that ended came out.",
		Parameters:  schema(nil, nil),
	}, Fn: func(context.Context, map[string]any) (string, error) {
		who, _, _, err := p.backgroundCaller(req)
		if err != nil {
			return "", err
		}
		out, err := p.ListBackground(who)
		if err != nil {
			return "", err
		}
		return marshal(out)
	}}
}

// backgroundStopTool stops one of the person's background tasks.
func (p *Platform) backgroundStopTool(req runtime.Request) saigetypes.Tool {
	return &saigetypes.ToolFunc{Def: saigetypes.ToolDef{
		Name:        ToolBackgroundStop,
		Description: "Stop one of the person's watches, goals or tasks, when they no longer want it.",
		Parameters:  schema([]string{"id"}, map[string]string{"id": "Its id, from background_list"}),
	}, Fn: func(_ context.Context, args map[string]any) (string, error) {
		who, _, _, err := p.backgroundCaller(req)
		if err != nil {
			return "", err
		}
		v, err := p.StopBackground(who, strings.TrimSpace(arg(args, "id")))
		if err != nil {
			return "", err
		}
		return marshal(v)
	}}
}

// ListBackground returns a person's own background work: what is still
// running, newest first, then what ended, most recent first, at most
// maxBackgroundListed in all.
func (p *Platform) ListBackground(who auth.Principal) ([]BackgroundView, error) {
	act, err := p.Background.Active(who.Tenant)
	if err != nil {
		return nil, err
	}
	ended, err := p.Background.Ended(who.Tenant)
	if err != nil {
		return nil, err
	}
	mine := func(all []background.Item, at func(background.Item) time.Time) []BackgroundView {
		all = slices.DeleteFunc(all, func(it background.Item) bool { return it.Owner != who.Subject })
		slices.SortFunc(all, func(a, b background.Item) int { return cmp.Or(at(b).Compare(at(a)), strings.Compare(a.ID, b.ID)) })
		out := make([]BackgroundView, 0, len(all))
		for _, it := range all {
			out = append(out, viewOf(it))
		}
		return out
	}
	out := append(mine(act, func(it background.Item) time.Time { return it.CreatedAt }), mine(ended, func(it background.Item) time.Time { return it.EndedAt })...)
	if len(out) > maxBackgroundListed {
		out = out[:maxBackgroundListed]
	}
	return out, nil
}

// StopBackground stops one of a person's own background tasks. Another
// person's, or one in another tenant, is not found. One already ended is
// returned as it is.
func (p *Platform) StopBackground(who auth.Principal, id string) (BackgroundView, error) {
	it, err := p.Background.Get(who.Tenant, id)
	if err != nil || it.Owner != who.Subject {
		return BackgroundView{}, fmt.Errorf("%w: %q", background.ErrNotFound, id)
	}
	if it.State != background.Active {
		return viewOf(it), nil
	}
	it, err = p.Background.Stop(who.Tenant, id, p.now())
	return viewOf(it), err
}

// runBackground is the leader's loop over background work.
func (p *Platform) runBackground(ctx context.Context) error {
	tick := time.NewTicker(backgroundEvery)
	defer tick.Stop()
	pruned := time.Time{}
	for {
		p.checkBackground(ctx)
		if now := p.now(); now.Sub(pruned) > time.Hour {
			pruned = now
			p.pruneBackground()
		}
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		}
	}
}

// checkBackground starts every run that is due, ends tasks with no runs
// left, and settles runs that ended unobserved.
func (p *Platform) checkBackground(ctx context.Context) {
	err := p.Background.Tenants(func(tenant string) error {
		all, err := p.Background.Active(tenant)
		if err != nil {
			p.log.Warn("platform: background work could not be read; the next pass tries again", "tenant", tenant, "error", err)
			return nil
		}
		for _, it := range all {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			p.checkItem(ctx, tenant, it)
		}
		return nil
	})
	if err != nil && ctx.Err() == nil {
		p.log.Warn("platform: background tenants could not be read; the next pass tries again", "error", err)
	}
}

func (p *Platform) checkItem(ctx context.Context, tenant string, it background.Item) {
	now := p.now()
	switch {
	case it.Running != "":
		// The run's end settles it. One that ended while no process
		// observed it is settled here.
		run, err := p.Runs.Get(tenant, it.Running)
		if errors.Is(err, runs.ErrNotFound) {
			ended, err := p.Background.Abandon(tenant, it.ID, it.Running, now)
			_ = p.backgroundEnded(tenant, ended, err)
		} else if err == nil && run.Status.Terminal() && now.Sub(run.UpdatedAt) > backgroundStale {
			_ = p.settleBackgroundRun(ctx, run)
		}
	case it.Over(now):
		ended, err := p.Background.Expire(tenant, it.ID, now)
		_ = p.backgroundEnded(tenant, ended, err)
	case it.Due(now):
		p.startBackgroundRun(ctx, tenant, it, now)
	}
}

// backgroundTag is the id of a task's nth run: retried, it joins the run
// already started.
func backgroundTag(id string, n int) string { return id + "-" + strconv.Itoa(n) }

// startBackgroundRun starts a task's next run: of its agent, for its person,
// in the conversation its runs are in.
func (p *Platform) startBackgroundRun(ctx context.Context, tenant string, it background.Item, now time.Time) {
	n := it.Runs + 1
	runID := runtime.RunID(tenant, backgroundTag(it.ID, n))
	it, err := p.Background.Begin(tenant, it.ID, runID, now)
	if err != nil {
		return
	}
	fail := func(why string, err error) {
		p.log.Warn("platform: a background run could not start", "tenant", tenant, "task", it.ID, "kind", it.Kind, "why", why, "error", err)
		ended, err := p.Background.Abandon(tenant, it.ID, runID, p.now())
		_ = p.backgroundEnded(tenant, ended, err)
	}
	who := p.byPolicy(tenant, it.Owner)
	if err := p.MayPost(who, Place{Project: it.Project}); err != nil {
		fail("the person may no longer work in the project", err)
		return
	}
	if it.Thread == "" {
		thread, err := p.backgroundThread(ctx, tenant, it)
		if err != nil {
			fail("its session could not be made", err)
			return
		}
		it.Thread = thread
		if err := p.Background.SetThread(tenant, it.ID, thread); err != nil {
			p.log.Warn("platform: a background task's session was not recorded; the next run finds it again", "task", it.ID, "error", err)
		}
	}
	ask := backgroundAsk(p.Names(tenant).Of(it.Owner), it, n)
	meta := map[string]string{runs.MetaThread: it.Thread, runs.MetaProject: it.Project, runs.MetaAsk: ask, metaBackground: it.ID}
	if it.Kind == background.Goal && it.Last != "" {
		meta[runs.MetaPrev] = it.Last
	}
	_, err = p.Invoke(ctx, wire.NewEvent(wire.Raise{
		ID: backgroundTag(it.ID, n), Tenant: tenant, Agent: it.Agent, Origin: runs.OriginAPI,
		Actor: p.namedActor(tenant, event.Actor{User: it.Owner}), Context: meta,
		Text: p.turnContext(tenant, it.Project, "", it.Agent, it.Owner, ask, meta),
	}))
	if err != nil {
		fail("the run was refused", err)
	}
}

// backgroundAsk is what a task's nth run asks its agent.
func backgroundAsk(person string, it background.Item, n int) string {
	var b strings.Builder
	switch it.Kind {
	case background.Watch:
		fmt.Fprintf(&b, "You are watching for something %s asked to be told about, in conversation %s.\n\n", person, it.Session)
		fmt.Fprintf(&b, "Watch for: %s\n", it.Text)
		if it.How != "" {
			fmt.Fprintf(&b, "How to check: %s\n", it.How)
		}
		fmt.Fprintf(&b, "This is check %d. Checks run every %s until %s.\n\n", n, it.Every, it.Until.UTC().Format(time.RFC3339))
		b.WriteString("Check now with your tools. Ask nobody anything.\n")
		b.WriteString("If it has not happened yet, answer exactly NONE.\n")
		b.WriteString("If it has, answer with one line saying what happened, then the details that matter. That answer is sent to them and the watch ends.")
	case background.Goal:
		fmt.Fprintf(&b, "Keep working toward the goal %s set in this conversation.\n\n", person)
		fmt.Fprintf(&b, "Goal: %s\n", it.Text)
		if it.How != "" {
			fmt.Fprintf(&b, "Done when: %s\n", it.How)
		}
		fmt.Fprintf(&b, "This is turn %d of at most %d; work stops at %s.\n", n, it.Limit, it.Until.UTC().Format(time.RFC3339))
		if it.Result != "" {
			fmt.Fprintf(&b, "Your last turn ended with:\n%s\n", clipRunes(it.Result, 2000))
		}
		b.WriteString("\nDo the next piece of work now with your tools. Ask nobody anything.\n")
		fmt.Fprintf(&b, "End your answer with a last line %s once the goal is fully achieved, or %s: and why when you cannot go on.", goalMet, goalBlocked)
	case background.Task:
		fmt.Fprintf(&b, "%s asked for this to be done in the background, from conversation %s, while they do other things:\n\n%s\n\n", person, it.Session, it.Text)
		b.WriteString("Do it now with your tools. Ask nobody anything. Your answer is sent to them when you finish: lead with the result.")
	}
	return b.String()
}

// backgroundThread opens the private session a watch's checks or a task's
// run go in: one per task, keyed (<kind>://<id>, agent), in its project, read
// by its person alone.
func (p *Platform) backgroundThread(ctx context.Context, tenant string, it background.Item) (string, error) {
	ch, err := p.Projects.Channel(tenant, it.Project, projects.General)
	if err != nil {
		return "", err
	}
	if ch.Archived {
		return "", fmt.Errorf("platform: the channel %q is archived", ch.Name)
	}
	rec, err := p.Sessions.Open(ctx, tenant, session.Open{Parent: string(it.Kind) + "://" + it.ID, Agent: it.Agent, Label: "runs", Channel: wire.Channel(tenant)})
	if err != nil {
		return "", err
	}
	title := it.Text
	if it.Kind == background.Watch {
		title = "Watching: " + it.Text
	}
	at := p.now()
	th := threads.Thread{
		ID: rec.ID, Project: it.Project, Channel: ch.Name, Title: firstWords(title, threads.MaxTitle),
		Visibility: threads.Private, CreatedBy: it.Owner, Default: it.Agent, Agents: []string{it.Agent},
		Participants: []threads.Participant{{Kind: threads.Person, Name: it.Owner, Owner: true, At: at}},
		Session:      &threads.SessionIdentity{Agent: it.Agent, Parent: rec.Key.Parent, Label: rec.Key.Label},
	}
	if _, err := p.Threads.Create(tenant, th, at); err != nil && !errors.Is(err, threads.ErrExists) {
		return "", err
	}
	return rec.ID, nil
}

// outcomeOf reads how a run of a task came out from its record.
func backgroundOutcome(kind background.Kind, run runs.Run) background.Outcome {
	if run.Status != runs.Completed {
		return background.Outcome{Failed: true, Result: run.Answer}
	}
	answer := strings.TrimSpace(run.Answer)
	switch kind {
	case background.Watch:
		subject, text, post := announcementOf(answer)
		return background.Outcome{Done: post, Result: strings.TrimSpace(subject + "\n" + text)}
	case background.Goal:
		body, last := "", answer
		if i := strings.LastIndex(answer, "\n"); i >= 0 {
			body, last = strings.TrimSpace(answer[:i]), answer[i+1:]
		}
		mark := strings.ToUpper(strings.Trim(strings.TrimSpace(last), "*_`#. "))
		switch {
		case mark == goalMet:
			return background.Outcome{Done: true, Result: body}
		case strings.HasPrefix(mark, goalBlocked):
			why := strings.TrimSpace(strings.TrimLeft(strings.Trim(strings.TrimSpace(last), "*_`#. ")[len(goalBlocked):], ": "))
			return background.Outcome{Blocked: true, Result: strings.TrimSpace(why + "\n\n" + body)}
		}
		return background.Outcome{Result: answer}
	}
	return background.Outcome{Done: true, Result: answer}
}

// settleBackgroundRun records how a run ended, when it is a background
// task's run in flight: a completion step, and the sweep's for a run that
// ended unobserved. A goal whose next turn is due starts it at once.
func (p *Platform) settleBackgroundRun(ctx context.Context, run runs.Run) error {
	if !run.Status.Terminal() {
		return nil
	}
	it, err := p.Background.ByRun(run.Tenant, run.ID)
	if errors.Is(err, background.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	it, err = p.Background.Settle(run.Tenant, it.ID, run.ID, backgroundOutcome(it.Kind, run), p.now())
	if errors.Is(err, background.ErrBusy) || errors.Is(err, background.ErrNotFound) {
		// Settled already, or stopped: nothing to tell.
		return nil
	}
	if err != nil {
		return err
	}
	if it.State == background.Active {
		if it.Kind == background.Goal && it.Due(p.now()) {
			p.startBackgroundRun(ctx, run.Tenant, it, p.now())
		}
		return nil
	}
	return p.backgroundEnded(run.Tenant, it, nil)
}

// backgroundEnded tells a task's person how it ended, when it did on its own.
// One a person or an agent stopped tells nobody.
func (p *Platform) backgroundEnded(tenant string, it background.Item, err error) error {
	if errors.Is(err, background.ErrBusy) {
		return nil
	}
	if err != nil || it.State == background.Active || it.State == background.Stopped {
		return err
	}
	subject, text := backgroundNotice(it)
	where := it.Session
	if it.Kind == background.Task && it.Thread != "" {
		where = it.Thread
	}
	return p.completionInbox(tenant, "background:"+it.ID, inbox.Message{
		Kind: inbox.KindResult, To: it.Owner, Subject: subject,
		From: inbox.From{Kind: "agent", Name: it.Agent}, Severity: inbox.Info,
		Source: inbox.Source{Run: cmp.Or(it.Last, it.CreatedBy), Thread: where, Agent: it.Agent, Project: it.Project},
		Parts:  []inbox.Part{{Type: inbox.PartText, State: inbox.PartInfo, Text: text}},
	}, it.EndedAt)
}

// backgroundNotice is what a task's person is told when it ends.
func backgroundNotice(it background.Item) (subject, text string) {
	about := firstWords(it.Text, 80)
	text = it.Result
	switch {
	case it.Kind == background.Watch && it.State == background.Done:
		subject, text, _ = strings.Cut(it.Result, "\n")
		text = strings.TrimSpace(text)
	case it.Kind == background.Watch && it.State == background.Expired:
		subject = "Stopped watching: " + about
		text = fmt.Sprintf("It did not happen in %d checks, up to %s.", it.Runs, it.Until.UTC().Format(time.RFC1123))
	case it.Kind == background.Goal && it.State == background.Done:
		subject = "Goal met: " + about
	case it.Kind == background.Goal && it.State == background.Expired:
		subject = "Goal not met: " + about
		text = fmt.Sprintf("Stopped after %d turns.\n\n%s", it.Runs, it.Result)
	case it.Kind == background.Goal:
		subject = "Goal blocked: " + about
	case it.Kind == background.Task && it.State == background.Done:
		subject = "Done: " + about
	case it.State == background.Failed:
		subject = "Could not finish: " + about
		text = fmt.Sprintf("The last runs could not finish. Ask again to try once more.\n\n%s", it.Result)
	default:
		subject = "Stopped: " + about
	}
	subject = clipRunes(cmp.Or(strings.TrimSpace(subject), about), MaxNotifySubject)
	return subject, clipRunes(cmp.Or(strings.TrimSpace(text), subject), MaxNotifyText)
}

// pruneBackground drops ended tasks past their retention.
func (p *Platform) pruneBackground() {
	cut := p.now().Add(-background.Retention)
	_ = p.Background.Tenants(func(tenant string) error {
		if _, err := p.Background.Prune(tenant, cut); err != nil {
			p.log.Warn("platform: ended background work could not be pruned", "tenant", tenant, "error", err)
		}
		return nil
	})
}
