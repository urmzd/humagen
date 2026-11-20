package platform

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/urmzd/saige/flow/event"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/approve"
	"github.com/urmzd/humagen/pkg/audit"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/inbox"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
)

// The approval agent (BuiltinApprover) decides tool calls in a session set
// to Run (threads.ApprovalAuto). It is a built-in agent, in the store like
// the others, with its own versioned base and model chain, and a workspace
// may change it like any agent. It is asked only about a call the policy
// leaves to a person (autoApprovable).
//
// Each decision is a run of it, metered like any run: autoApprove starts it
// as housekeeping (startHook) with the call put as approve.Describe writes
// it, and when that run ends fileApproval reads its answer and answers the
// question through the path every answer takes, as the person who chose
// Run, so it never allows more than they could. The session shows "Allowed
// by the approval agent: <reason>" or "Denied by the approval agent:
// <reason>", which a denied agent reads. A run that fails (none of its
// chain's providers is enabled, say), an answer that cannot be read, and
// "ask" leave the question for a person. Every decision is audited, and
// kept on the question's card (judge) with the fields a workspace's schema
// adds.
//
// In a session set to Run and Answer (threads.ApprovalDecide) it is also
// shown a question an agent asks a person (approval_questions.go), and
// answers it the same way, as the person who chose the mode, only when the
// session settles the answer.
//
// The approval agent answers with structured output: its spec's output
// schema (approve.DefaultSchema as it ships; a workspace may add fields of
// its own, keeping decision and reason) is what its model is asked for, and
// fileApproval reads the run's Output, the answer checked against that
// schema. An answer that is missing or does not hold to it leaves the
// question for a person.

const (
	hookApproval = "approval"
	// metaApprovalRun, metaApprovalQuestion and metaApprovalTool name the
	// question an approval run decides.
	metaApprovalRun      = "_humagen.approval.run"
	metaApprovalQuestion = "_humagen.approval.question"
	metaApprovalTool     = "_humagen.approval.tool"
	// metaApprovalKind is the kind of question an approval run decides:
	// empty, or approval, for a call, else choice or reply.
	metaApprovalKind = "_humagen.approval.kind"
)

// sessionTurns is how many of the session's latest messages the approval
// agent reads.
const sessionTurns = 40

// approvalCall is what the approval agent reads about a call: the call, the
// agent making it, why the policy asks, and the session so far.
func (p *Platform) approvalCall(run runs.Run, q event.Question, spec wire.AgentSpec) approve.Call {
	c := approve.Call{
		Agent:         run.Agent,
		Description:   spec.Description,
		Tool:          q.Tool.Name,
		Arguments:     q.Tool.Arguments,
		ActsForPerson: q.Tool.ActsForPerson,
	}
	where := "the workspace"
	if run.Project != "" {
		where = "project " + run.Project
	}
	c.Policy = []string{
		fmt.Sprintf("In %s, calls of this kind (%s) need approval before they run.", where, strings.ReplaceAll(string(p.categoryOf(q.Tool.Name)), "_", " ")),
		"The session's owner set it to Run, so you decide in their place, and only within what they could approve themselves.",
	}
	if q.Tool.ActsForPerson {
		c.Policy = append(c.Policy, "This tool acts with a person's own account: allow only what the session clearly asks of it.")
	}
	c.Session = p.sessionSoFar(run)
	return c
}

// sessionSoFar is the latest messages of run's session, oldest first, as
// the approval agent reads them. A run in no session, or one whose session
// cannot be read, has none.
func (p *Platform) sessionSoFar(run runs.Run) []approve.Turn {
	if run.Thread == "" {
		return nil
	}
	last, err := p.Threads.Last(run.Tenant, run.Thread)
	if err != nil {
		return nil
	}
	path, err := p.Threads.Path(run.Tenant, run.Thread, last.ID)
	if err != nil {
		return nil
	}
	if len(path) > sessionTurns {
		path = path[len(path)-sessionTurns:]
	}
	out := make([]approve.Turn, 0, len(path))
	for _, m := range path {
		who := m.Name
		if who == "" {
			who = "someone"
		}
		out = append(out, approve.Turn{Who: who, Text: m.Text})
	}
	return out
}

// startApproval asks the approval agent about q, which run parked on and m
// holds, in a run of its own. With no approval agent in the workspace the
// question stays for a person.
func (p *Platform) startApproval(ctx context.Context, run runs.Run, q event.Question, m inbox.Message, spec wire.AgentSpec, by auth.Principal) {
	p.askApprover(ctx, run, aboutOf(q), m, approve.Describe(p.approvalCall(run, q, spec)), by)
}

// judgedAbout is what an approval run decides: one question run asked, a
// call (Tool, kind approval) or a question for a person (kind choice or
// reply).
type judgedAbout struct {
	Question, Tool, Kind string
	// Mode is the session's approval mode it was decided in.
	Mode string
}

func aboutOf(q event.Question) judgedAbout {
	a := judgedAbout{Question: q.ID, Kind: q.Kind, Mode: threads.ApprovalAuto}
	if q.Tool != nil {
		a.Tool = q.Tool.Name
	}
	if isPersonQuestion(q) {
		a.Mode = threads.ApprovalDecide
	}
	return a
}

// askApprover starts the approval agent's run about one question, put to
// it as prompt.
func (p *Platform) askApprover(ctx context.Context, run runs.Run, about judgedAbout, m inbox.Message, prompt string, by auth.Principal) {
	if err := p.EnsureBuiltins(ctx, run.Tenant); err != nil {
		p.log.Warn("platform: the approval agent could not be made ready; a person decides", "tenant", run.Tenant, "run", run.ID, "error", err)
		return
	}
	if _, err := p.Catalog().Get(ctx, run.Tenant, BuiltinApprover, 0); err != nil {
		p.auditJudgement(by, run, about, "", approve.Decision{Verdict: approve.Ask, Reason: "This workspace has no approval agent, so a person decides."})
		return
	}
	// One run per question: a run that parks twice is asked about twice.
	turn := run
	turn.ID = run.ID + "-" + about.Question
	extra := map[string]string{metaApprovalRun: run.ID, metaApprovalQuestion: about.Question, metaApprovalTool: about.Tool, metaApprovalKind: about.Kind}
	if err := p.startHook(ctx, turn, BuiltinApprover, hookApproval, m.ID, prompt, extra); err != nil {
		p.log.Warn("platform: the approval agent could not be asked; a person decides", "tenant", run.Tenant, "run", run.ID, "question", about.Question, "error", err)
	}
}

// fileApproval answers the question an approval run decided, once that run
// has ended, unless the session has left Run since or someone answered
// first.
func (p *Platform) fileApproval(ctx context.Context, decided runs.Run) error {
	meta := wire.Meta(decided.Tag())
	parked, err := p.Runs.Get(decided.Tenant, meta[metaApprovalRun])
	if err != nil {
		return nil
	}
	m, ok := p.Inbox.ByID(decided.Tenant, meta[metaFor])
	if !ok {
		return nil
	}
	th, err := p.Threads.Get(decided.Tenant, parked.Thread)
	if err != nil || th.Approval == nil || !th.Approval.Auto() {
		return nil
	}
	by := p.approvingFor(decided.Tenant, *th.Approval)
	question, tool := meta[metaApprovalQuestion], meta[metaApprovalTool]
	if kind := meta[metaApprovalKind]; kind == event.QuestionChoice || kind == event.QuestionReply {
		if !th.Approval.Decides() {
			// The session left Run and Answer since: a person answers.
			return nil
		}
		return p.fileAnswer(ctx, decided, parked, m, judgedAbout{Question: question, Kind: kind, Mode: threads.ApprovalDecide}, by)
	}
	about := judgedAbout{Question: question, Tool: tool, Kind: event.QuestionApproval, Mode: th.Approval.ModeOf()}
	var d approve.Decision
	switch {
	case decided.Status != runs.Completed:
		why := "its run did not finish"
		if decided.Cause != "" {
			why = oneLine(decided.Cause, 200)
		}
		d = approve.Decision{Verdict: approve.Ask, Reason: "The approval agent could not decide (" + why + "), so a person will."}
	default:
		// The answer is read only as structured data: what the run kept
		// once its answer held to the approval agent's output schema.
		if d, err = approve.Parse(decided.Output); err != nil {
			d = approve.Decision{Verdict: approve.Ask, Reason: "The approval agent's answer could not be read, so a person will."}
		}
	}
	p.judge(decided.Tenant, m, question, decided.ID, d)
	var answer QuestionAnswer
	switch d.Verdict {
	case approve.Allow:
		answer = QuestionAnswer{Action: ActApprove, Note: "Allowed by the approval agent: " + d.Reason}
	case approve.Deny:
		answer = QuestionAnswer{Action: ActDeny, Note: "Denied by the approval agent: " + d.Reason}
	default:
		p.log.Info("platform: the approval agent handed a call to a person", "tenant", parked.Tenant, "run", parked.ID, "question", question, "reason", d.Reason)
		p.auditJudgement(by, parked, about, decided.ID, d)
		return nil
	}
	actx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	if _, first, err := p.answerFirst(actx, by, m, answer); err != nil || !first {
		p.log.Info("platform: an approval was left for a person", "tenant", parked.Tenant, "run", parked.ID, "question", question, "error", err)
		return nil
	}
	p.auditJudgement(by, parked, about, decided.ID, d)
	return nil
}

// judge keeps the approval agent's judgement on the question's card in m,
// with the fields the workspace's schema adds, before the question is
// answered, so the card shows it beside the outcome or, when a person
// decides, beside the call.
func (p *Platform) judge(tenant string, m inbox.Message, question, decidedBy string, d approve.Decision) {
	i := slices.IndexFunc(m.Parts, func(x inbox.Part) bool { return x.Question != nil && x.Question.ID == question })
	if i < 0 {
		return
	}
	j := inbox.Judgement{Decision: string(d.Verdict), Reason: d.Reason, References: d.References, Run: decidedBy}
	for _, f := range d.Fields {
		j.Fields = append(j.Fields, inbox.JudgementField{Name: f.Name, Label: f.Label, Value: f.Value})
	}
	if _, _, err := p.Inbox.Judge(tenant, m.ID, m.Parts[i].ID, j, p.now()); err != nil {
		p.log.Warn("platform: the approval agent's judgement could not be kept on its card", "tenant", tenant, "message", m.ID, "question", question, "error", err)
	}
}

// auditJudgement records what the approval agent decided about a question
// run asked, for the person who chose Run. decidedBy is the approval run.
// The fields a workspace's schema adds are recorded with it, each under
// "field." and its name, and an answer to a question under "answer".
func (p *Platform) auditJudgement(by auth.Principal, run runs.Run, about judgedAbout, decidedBy string, d approve.Decision) {
	mode := cmp.Or(about.Mode, threads.ApprovalAuto)
	after := map[string]string{"question": about.Question, "mode": mode, "verdict": string(d.Verdict), "reason": d.Reason, "agent": BuiltinApprover}
	if about.Tool != "" {
		after["tool"] = about.Tool
	}
	if about.Kind != "" {
		after["kind"] = about.Kind
	}
	if d.Answer != "" {
		after["answer"] = d.Answer
	}
	if decidedBy != "" {
		after["approval_run"] = decidedBy
	}
	if len(d.References) > 0 {
		after["references"] = strings.Join(d.References, "; ")
	}
	for _, f := range d.Fields {
		after["field."+f.Name] = f.Value
	}
	if err := p.RecordAudit(by, audit.Change{
		Action: "approval.judged", Resource: "run", ID: run.ID, Project: run.Project, Thread: run.Thread, Agent: run.Agent,
		After: after,
	}); err != nil {
		p.log.Error("platform: an approval decision could not be audited", "tenant", run.Tenant, "run", run.ID, "error", err)
	}
}
