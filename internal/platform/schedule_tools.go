package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	saigetypes "github.com/urmzd/saige/agent/types"
	"github.com/urmzd/saige/flow/runtime"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
)

// Schedules from a conversation: an agent sets up recurring work for the
// person it works for, the way that person could in the console.
//
// A person asks, in a conversation (a Slack thread among them), "check Plaud
// every 15 minutes and post in this channel when something is new". The
// agent proposes a schedule with create_schedule; the call waits on an
// approval card that says what it checks, how often, which agent, and where
// it posts; once approved, the tool answers with the schedule's next run
// times, which the agent confirms. run_schedule then runs it once now, so
// the person can see it work, itself an approved call.
//
// Each tool is the API's own operation (createSchedule, updateSchedule,
// pauseSchedule, resumeSchedule, runSchedule), served in-process through its
// route as the person the run acts for, with their access, so every check
// the API applies holds: the agent can do nothing the person could not. A
// schedule a run makes is filed in the conversation's project and owned by
// the person (its created_by), and only the schedules filed there can be
// changed, paused, resumed or run from it.
//
// Every call is asked about, whatever the action policy allows: of the
// person the run acts for alone, who may answer it themselves, or of the
// approval agent in a conversation they set to Auto where the policy asks.

// The tools. Each shares its name with the API operation an MCP client is
// offered, under a registry key of its own (runToolKey).
const (
	ToolCreateSchedule = "create_schedule"
	ToolUpdateSchedule = "update_schedule"
	ToolPauseSchedule  = "pause_schedule"
	ToolResumeSchedule = "resume_schedule"
	ToolRunSchedule    = "run_schedule"
)

// ScheduleTools are the tools for making and running schedules from a
// conversation.
func ScheduleTools() []string {
	return []string{ToolCreateSchedule, ToolUpdateSchedule, ToolPauseSchedule, ToolResumeSchedule, ToolRunSchedule}
}

// Where a schedule made from a conversation posts, as the tools take it:
// DeliverHere is the Slack channel the conversation is in. The others are
// the API's deliver values.
const DeliverHere = "here"

const (
	// scheduleApprovalWait is how long a schedule call waits for its person,
	// as a call of a person's own tools does.
	scheduleApprovalWait = 24 * time.Hour
	// scheduleNextShown is how many next run times a tool answers with.
	scheduleNextShown = 3
	// maxScheduleWords bounds a value an approval question repeats.
	maxScheduleWords = 80
)

// schedulePerson is who a schedule tool acts for: the person the run acts
// for, in the conversation the run is in. An evaluation, a housekeeping run,
// a run that acts for nobody, background work and a scheduled run make and
// run no schedules. Each is a fact of the run, so a call refused here is
// refused whenever it is made.
func (p *Platform) schedulePerson(req runtime.Request) (auth.Principal, runs.Run, threads.Thread, error) {
	run, err := p.runRecordOf(req.Tenant, req.RunID)
	if err != nil {
		return auth.Principal{}, runs.Run{}, threads.Thread{}, err
	}
	meta := wire.Meta(run.Tag())
	switch {
	case IsEvaluation(run) || run.Origin == runs.OriginSystem:
		return auth.Principal{}, run, threads.Thread{}, fmt.Errorf("%w: an evaluation or housekeeping run makes no schedules", auth.ErrForbidden)
	case meta[metaBackground] != "" || meta[wire.MetaCronSchedule] != "":
		return auth.Principal{}, run, threads.Thread{}, fmt.Errorf("%w: background work and scheduled runs make and run no schedules", auth.ErrForbidden)
	}
	who := p.byPolicy(req.Tenant, personOf(run.Actor))
	if who.Subject == "" {
		return who, run, threads.Thread{}, fmt.Errorf("%w: this run acts for nobody, so a schedule would belong to nobody", auth.ErrForbidden)
	}
	th, err := p.ThreadRecord(req.Tenant, run.Thread)
	if err != nil {
		th = threads.Thread{ID: run.Thread, Project: run.Project}
	}
	return who, run, th, nil
}

// scheduleCaller is schedulePerson, held to a conversation in a project the
// person may still post in: the project the schedules it reaches are filed
// in.
func (p *Platform) scheduleCaller(req runtime.Request) (auth.Principal, threads.Thread, error) {
	who, _, th, err := p.schedulePerson(req)
	if err != nil {
		return who, th, err
	}
	if th.Project == "" {
		return who, th, fmt.Errorf("%w: this conversation is in no project, so a schedule made here would have nowhere to be filed", auth.ErrForbidden)
	}
	if err := p.MayPost(who, Place{Project: th.Project, Thread: th.ID}); err != nil {
		return who, th, err
	}
	return who, th, nil
}

// scheduleHere is a schedule of the conversation's project. One filed
// anywhere else is not found from here, whatever the person may reach.
func (p *Platform) scheduleHere(tenant string, th threads.Thread, name string) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("name the schedule")
	}
	sc, _, err := p.Schedules.Get(tenant, name)
	if err != nil {
		return err
	}
	if sc.Context[runs.MetaProject] != th.Project {
		return ErrScheduleNotFound
	}
	return nil
}

// scheduleOperation calls the API operation op as who, through its route.
// What the API refused comes back as the error the agent reads.
func (p *Platform) scheduleOperation(ctx context.Context, who auth.Principal, op string, args map[string]any) (string, error) {
	t, ok := p.SystemTool(op)
	if !ok || t.Call == nil {
		return "", fmt.Errorf("platform: no API operation %s", op)
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return "", err
	}
	res, err := t.Call(ctx, SystemCall{Who: who}, raw)
	if err != nil {
		return "", err
	}
	if res.IsError {
		return "", errors.New(res.Text)
	}
	return res.Text, nil
}

// scheduleBody is what create_schedule and update_schedule send the API,
// from what the agent asked: the conversation's project, the calling agent
// unless another is named, and where it posts.
func (p *Platform) scheduleBody(req runtime.Request, th threads.Thread, args map[string]any) (map[string]any, error) {
	agent := strings.TrimSpace(arg(args, "agent"))
	if agent == "" {
		agent = req.Agent()
	}
	body := map[string]any{
		"name":        strings.TrimSpace(arg(args, "name")),
		"expression":  strings.Join(strings.Fields(arg(args, "cron")), " "),
		"agent":       agent,
		"text":        strings.TrimSpace(arg(args, "check")),
		"project":     th.Project,
		"description": strings.TrimSpace(arg(args, "description")),
	}
	deliver, client, to, err := scheduleDelivery(th, args)
	if err != nil {
		return nil, err
	}
	if deliver != DeliverNone {
		body["deliver"] = deliver
	}
	if client != "" {
		body["deliver_client"], body["deliver_to"] = client, to
	}
	return body, nil
}

// scheduleDelivery is where a schedule's answers go, as the API takes it, from the
// tool's post, connection and to: "here" is the Slack channel the
// conversation is in, through the connection it came in by.
func scheduleDelivery(th threads.Thread, args map[string]any) (deliver, client, to string, err error) {
	post := strings.ToLower(strings.TrimSpace(arg(args, "post")))
	client, to = strings.TrimSpace(arg(args, "connection")), strings.TrimSpace(arg(args, "to"))
	switch post {
	case "", "session":
		return DeliverNone, "", "", nil
	case DeliverHere:
		if th.Surface == nil || th.Surface.Client == "" || th.Surface.Channel == "" {
			return "", "", "", errors.New("this conversation is not in a Slack channel: post slack with a connection and a channel id, or leave post out")
		}
		return DeliverSlack, th.Surface.Client, th.Surface.Channel, nil
	case DeliverSlack, DeliverGitHub:
		if client == "" || to == "" {
			return "", "", "", fmt.Errorf("post %s names a connection and to", post)
		}
		return post, client, to, nil
	case DeliverInbox:
		return DeliverInbox, "", "", nil
	}
	return "", "", "", fmt.Errorf("post is here, slack, github, inbox or session, not %q", post)
}

// ScheduleToolView is what a schedule tool answers about a schedule: when it
// runs next, in UTC, written so the agent can say it to the person exactly.
type ScheduleToolView struct {
	Name       string `json:"name"`
	Expression string `json:"cron"`
	// Every is the cron expression in words.
	Every    string `json:"every"`
	Timezone string `json:"timezone"`
	Agent    string `json:"agent"`
	Project  string `json:"project,omitempty"`
	// PostsIn is where each answer that is not NONE goes.
	PostsIn string `json:"posts_in"`
	Paused  bool   `json:"paused"`
	// NextRun is the next time it runs, RFC 3339 in UTC; Next the next few.
	// Both are empty while it is paused.
	NextRun string   `json:"next_run,omitempty"`
	Next    []string `json:"next_runs"`
	// Say is a line to tell the person.
	Say string `json:"say"`
}

// scheduleViewOf reads the API's answer about a schedule as the tool's.
func scheduleViewOf(out string, th threads.Thread) (ScheduleToolView, error) {
	var v struct {
		Name          string      `json:"name"`
		Expression    string      `json:"expression"`
		Agent         string      `json:"agent"`
		Project       string      `json:"project"`
		Deliver       string      `json:"deliver"`
		DeliverClient string      `json:"deliver_client"`
		DeliverTo     string      `json:"deliver_to"`
		Paused        bool        `json:"paused"`
		Next          []time.Time `json:"next"`
	}
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		return ScheduleToolView{}, fmt.Errorf("platform: the schedule could not be read: %w", err)
	}
	view := ScheduleToolView{
		Name: v.Name, Expression: v.Expression, Every: CronInWords(v.Expression), Timezone: "UTC",
		Agent: v.Agent, Project: v.Project, PostsIn: postsIn(th, v.Deliver, v.DeliverTo), Paused: v.Paused, Next: []string{},
	}
	for i, t := range v.Next {
		if i == scheduleNextShown {
			break
		}
		view.Next = append(view.Next, t.UTC().Format(time.RFC3339))
	}
	switch {
	case v.Paused:
		view.Say = fmt.Sprintf("%s is paused: it runs again once resumed.", v.Name)
	case len(view.Next) > 0:
		view.NextRun = view.Next[0]
		view.Say = fmt.Sprintf("%s runs %s. Next run: %s (%s).", v.Name, view.Every, v.Next[0].UTC().Format("Mon 2 Jan 2006 15:04 MST"), view.NextRun)
	default:
		view.Say = fmt.Sprintf("%s has no run coming: its cron %q never fires again.", v.Name, v.Expression)
	}
	return view, nil
}

// postsIn says what becomes of a schedule's answers, as a person reads it:
// "posts in Slack channel C0123ABC only when something fires".
func postsIn(th threads.Thread, deliver, to string) string {
	const fires = " only when something fires"
	switch deliver {
	case DeliverSlack:
		if th.Surface != nil && th.Surface.Channel == to {
			return "posts in this Slack channel (" + to + ")" + fires
		}
		return "posts in Slack channel " + to + fires
	case DeliverGitHub:
		return "comments on " + to + fires
	case DeliverInbox:
		return "sends to your inbox" + fires
	case DeliverProject:
		return "posts in the project" + fires
	}
	return "keeps each answer in its own session"
}

// scheduleParams are what create_schedule and update_schedule take.
func scheduleParams(required []string) saigetypes.ParameterSchema {
	return schema(required, map[string]string{
		"name":        "The schedule's name: short, lowercase, with dashes, such as plaud-new-recordings. It cannot change later",
		"cron":        "When it runs: a cron expression of five fields (minute hour day month weekday), read in UTC, such as */15 * * * * for every 15 minutes or 0 9 * * 1-5 for weekdays at 09:00 UTC",
		"check":       "What each run does, complete enough to do with nobody present: what to check, with which tools, and what counts as something to report. Each run must answer NONE when there is nothing to report: only other answers are posted",
		"agent":       "The agent that runs it. Defaults to you",
		"description": "What the schedule is for, for the people who read the list of schedules",
		"post":        "Where each answer that is not NONE goes: here (the Slack channel this conversation is in), slack or github (with connection and to), inbox (the person's inbox), or session (nowhere: each answer stays in its own session). Defaults to session",
		"connection":  "With post slack or github: the connection's id",
		"to":          "With post slack: a Slack channel id such as C0123ABC. With post github: owner/repo#12",
	})
}

// createScheduleTool proposes a schedule for the person the run acts for.
func (p *Platform) createScheduleTool(req runtime.Request) saigetypes.Tool {
	return &saigetypes.ToolFunc{Def: saigetypes.ToolDef{
		Name: ToolCreateSchedule,
		Description: "Set up recurring work for the person you work for: an agent runs on a cron and posts what it finds. " +
			"The person approves every schedule before it exists, on a card that says what it checks, how often, which agent and where it posts. " +
			"Once approved, tell them when it runs next, exactly as next_run says (UTC), and offer run_schedule to try it once now.",
		Parameters: scheduleParams([]string{"name", "cron", "check"}),
	}, Fn: func(ctx context.Context, args map[string]any) (string, error) {
		who, th, err := p.scheduleCaller(req)
		if err != nil {
			return "", err
		}
		body, err := p.scheduleBody(req, th, args)
		if err != nil {
			return "", err
		}
		out, err := p.scheduleOperation(ctx, who, ToolCreateSchedule, map[string]any{"body": body})
		if err != nil {
			return "", err
		}
		return scheduleAnswer(out, th)
	}}
}

// updateScheduleTool changes a schedule of the conversation's project.
func (p *Platform) updateScheduleTool(req runtime.Request) saigetypes.Tool {
	return &saigetypes.ToolFunc{Def: saigetypes.ToolDef{
		Name: ToolUpdateSchedule,
		Description: "Change a schedule of this conversation's project: when it runs, what it checks, its agent, or where it posts. Give every field as it should now be, not only what changes. " +
			"The person approves the change first. Then tell them when it runs next, exactly as next_run says (UTC).",
		Parameters: scheduleParams([]string{"name", "cron", "check"}),
	}, Fn: func(ctx context.Context, args map[string]any) (string, error) {
		who, th, err := p.scheduleCaller(req)
		if err != nil {
			return "", err
		}
		name := strings.TrimSpace(arg(args, "name"))
		if err := p.scheduleHere(req.Tenant, th, name); err != nil {
			return "", err
		}
		body, err := p.scheduleBody(req, th, args)
		if err != nil {
			return "", err
		}
		out, err := p.scheduleOperation(ctx, who, ToolUpdateSchedule, map[string]any{"name": name, "body": body})
		if err != nil {
			return "", err
		}
		return scheduleAnswer(out, th)
	}}
}

// pauseScheduleTool pauses or resumes a schedule of the conversation's
// project.
func (p *Platform) pauseScheduleTool(req runtime.Request, pause bool) saigetypes.Tool {
	name, op, does := ToolResumeSchedule, ToolResumeSchedule, "Resume a paused schedule of this conversation's project. Then tell the person when it runs next, exactly as next_run says (UTC)."
	if pause {
		name, op, does = ToolPauseSchedule, ToolPauseSchedule, "Pause a schedule of this conversation's project: it does not run until resumed."
	}
	return &saigetypes.ToolFunc{Def: saigetypes.ToolDef{
		Name:        name,
		Description: does + " The person approves it first.",
		Parameters:  schema([]string{"name"}, map[string]string{"name": "The schedule's name"}),
	}, Fn: func(ctx context.Context, args map[string]any) (string, error) {
		who, th, err := p.scheduleCaller(req)
		if err != nil {
			return "", err
		}
		sched := strings.TrimSpace(arg(args, "name"))
		if err := p.scheduleHere(req.Tenant, th, sched); err != nil {
			return "", err
		}
		out, err := p.scheduleOperation(ctx, who, op, map[string]any{"name": sched})
		if err != nil {
			return "", err
		}
		return scheduleAnswer(out, th)
	}}
}

// runScheduleTool runs a schedule of the conversation's project once, now,
// as the person's own press of Run Now would.
func (p *Platform) runScheduleTool(req runtime.Request) saigetypes.Tool {
	return &saigetypes.ToolFunc{Def: saigetypes.ToolDef{
		Name: ToolRunSchedule,
		Description: "Run a schedule of this conversation's project once now, so the person can see it work before its next run. " +
			"The person approves it first. Its answer goes where the schedule posts, only when something fires.",
		Parameters: schema([]string{"name"}, map[string]string{"name": "The schedule's name"}),
	}, Fn: func(ctx context.Context, args map[string]any) (string, error) {
		who, th, err := p.scheduleCaller(req)
		if err != nil {
			return "", err
		}
		name := strings.TrimSpace(arg(args, "name"))
		if err := p.scheduleHere(req.Tenant, th, name); err != nil {
			return "", err
		}
		out, err := p.scheduleOperation(ctx, who, ToolRunSchedule, map[string]any{"name": name})
		if err != nil {
			return "", err
		}
		var run struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal([]byte(out), &run)
		sc, _, err := p.Schedules.Get(req.Tenant, name)
		if err != nil {
			return "", err
		}
		return marshal(map[string]any{
			"schedule":   name,
			"run":        run.ID,
			"started_at": p.now().UTC().Format(time.RFC3339),
			"posts_in":   postsIn(th, sc.Context[MetaDeliver], sc.Context[MetaDeliverTo]),
			"say":        fmt.Sprintf("%s is running once now. It %s.", name, postsIn(th, sc.Context[MetaDeliver], sc.Context[MetaDeliverTo])),
		})
	}}
}

// scheduleAnswer is the tool's answer from the API's.
func scheduleAnswer(out string, th threads.Thread) (string, error) {
	v, err := scheduleViewOf(out, th)
	if err != nil {
		return "", err
	}
	return marshal(v)
}

// scheduleApproval is how a schedule tool's calls are approved: every call
// is asked of the person the run acts for, who may answer it themselves,
// whatever the action policy allows. A call from a run that may make no
// schedule is refused by the tool itself, so it is not asked about.
func (p *Platform) scheduleApproval(prompt func(ApprovalCall) string) ToolApproval {
	each := ToolApproval{Mode: ApprovalAlways, SelfApproval: true, Expires: scheduleApprovalWait, Prompt: prompt}
	a := each
	a.PerCall = func(q ApprovalCall) (ToolApproval, bool) {
		who, _, _, err := p.schedulePerson(q.Request)
		if err != nil {
			return ToolApproval{}, false
		}
		asked := each
		asked.Approvers = []string{who.Subject}
		return asked, true
	}
	return a
}

// scheduleArgs are a call's arguments as a tool reads them.
func scheduleArgs(q ApprovalCall) map[string]any {
	if q.Call.Arguments == nil {
		return map[string]any{}
	}
	return q.Call.Arguments
}

// words is a value an approval question repeats: one line, bounded.
func words(s string) string {
	return clipRunes(strings.Join(strings.Fields(s), " "), maxScheduleWords)
}

// scheduleThread is the conversation a call is made in, for its question.
func (p *Platform) scheduleThread(req runtime.Request) threads.Thread {
	_, _, th, _ := p.schedulePerson(req)
	return th
}

// createSchedulePrompt asks about a new schedule: "Create a schedule:
// plaud-new, every 15 minutes, posts in this Slack channel (C0123ABC) only
// when something fires".
func (p *Platform) createSchedulePrompt(q ApprovalCall) string {
	return p.scheduleChangePrompt(q, "Create a schedule: %s")
}

// updateSchedulePrompt asks about a change to a schedule.
func (p *Platform) updateSchedulePrompt(q ApprovalCall) string {
	return p.scheduleChangePrompt(q, "Change the schedule %s")
}

func (p *Platform) scheduleChangePrompt(q ApprovalCall, lead string) string {
	args := scheduleArgs(q)
	th := p.scheduleThread(q.Request)
	parts := []string{fmt.Sprintf(lead, words(arg(args, "name"))), CronInWords(words(arg(args, "cron")))}
	if agent := words(arg(args, "agent")); agent != "" && agent != q.Request.Agent() {
		parts = append(parts, "run by "+agent)
	}
	deliver, _, to, err := scheduleDelivery(th, args)
	if err != nil {
		parts = append(parts, "posts nowhere yet: "+err.Error())
	} else {
		parts = append(parts, postsIn(th, deliver, words(to)))
	}
	return strings.Join(parts, ", ")
}

// pauseSchedulePrompt, resumeSchedulePrompt and runSchedulePrompt ask about
// a schedule by its name.
func pauseSchedulePrompt(q ApprovalCall) string {
	return "Pause the schedule " + words(arg(scheduleArgs(q), "name"))
}

func resumeSchedulePrompt(q ApprovalCall) string {
	return "Resume the schedule " + words(arg(scheduleArgs(q), "name"))
}

func runSchedulePrompt(q ApprovalCall) string {
	return "Run " + words(arg(scheduleArgs(q), "name")) + " once now"
}

// CronInWords is a schedule's cron expression, read in UTC, in words for a
// person: "every 15 minutes", "weekdays at 09:00 UTC". A shape it does not
// know is the expression itself, said to be cron in UTC.
func CronInWords(expression string) string {
	f := strings.Fields(expression)
	fallback := fmt.Sprintf("on the cron schedule %q (UTC)", strings.Join(f, " "))
	if len(f) != 5 || f[2] != "*" || f[3] != "*" {
		return fallback
	}
	minute, hour, weekday := f[0], f[1], f[4]
	num := func(s string, max int) (int, bool) {
		n, err := strconv.Atoi(s)
		return n, err == nil && n >= 0 && n <= max
	}
	step := func(s string, max int) (int, bool) {
		rest, ok := strings.CutPrefix(s, "*/")
		if !ok {
			return 0, false
		}
		n, err := strconv.Atoi(rest)
		return n, err == nil && n >= 1 && n <= max
	}
	plural := func(n int, one string) string {
		if n == 1 {
			return "every " + one
		}
		return fmt.Sprintf("every %d %ss", n, one)
	}
	if weekday == "*" && hour == "*" {
		if minute == "*" {
			return "every minute"
		}
		if n, ok := step(minute, 59); ok {
			return plural(n, "minute")
		}
		if m, ok := num(minute, 59); ok {
			if m == 0 {
				return "every hour"
			}
			return fmt.Sprintf("every hour at minute %d", m)
		}
		return fallback
	}
	m, ok := num(minute, 59)
	if !ok {
		return fallback
	}
	if n, ok := step(hour, 23); ok && weekday == "*" {
		if m == 0 {
			return plural(n, "hour")
		}
		return fmt.Sprintf("%s at minute %d", plural(n, "hour"), m)
	}
	h, ok := num(hour, 23)
	if !ok {
		return fallback
	}
	at := fmt.Sprintf("%02d:%02d UTC", h, m)
	days := []string{"Sundays", "Mondays", "Tuesdays", "Wednesdays", "Thursdays", "Fridays", "Saturdays"}
	switch weekday {
	case "*":
		return "daily at " + at
	case "1-5":
		return "weekdays at " + at
	case "0,6", "6,0":
		return "weekends at " + at
	}
	if d, ok := num(weekday, 7); ok {
		return days[d%7] + " at " + at
	}
	return fallback
}
