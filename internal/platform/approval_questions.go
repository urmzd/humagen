package platform

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/urmzd/saige/flow/event"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/approve"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/inbox"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
)

// In Auto (threads.ApprovalAuto) the approval agent decides gated calls and
// also answers a question an agent asks a person (ask_person: a choice or a
// reply), as the person who chose the mode, when the session already
// settles the answer: an option they already stated, or a confirmation of
// what they asked for. It hands the question to a person otherwise, and the
// platform hands it over without asking the agent at all (answerable) when
// the question is for someone else: it names another person or the experts,
// someone else asked for the work, or the person could not answer it
// themselves. The answer goes through the path every answer takes
// (answerFirst), so the runtime's checks and the question's audience still
// hold. The card shows "Answered by the approval agent" and its reason, and
// every decision and hand-off is audited as approval.judged.

// isPersonQuestion reports whether q is a question an agent asks a person,
// which Auto may answer: a choice or a reply, not an
// approval, a budget request or any other hold.
func isPersonQuestion(q event.Question) bool {
	return (q.Kind == event.QuestionChoice || q.Kind == event.QuestionReply) && q.Hold == nil && !isBudget(q)
}

// autoAnswer hands a question run parked on to the approval agent, when the
// session is set to Auto and the question is one the person who chose it
// could answer themselves. Anything else stays for a person, and the
// hand-off is audited.
func (p *Platform) autoAnswer(ctx context.Context, run runs.Run, q event.Question, m inbox.Message, a threads.Approval) {
	if !a.Auto() {
		return
	}
	by := p.approvingFor(run.Tenant, a)
	about := aboutOf(q)
	if ok, why := p.answerable(ctx, run, q, m, by); !ok {
		p.log.Info("platform: a question was left for a person", "tenant", run.Tenant, "run", run.ID, "question", q.ID, "why", why)
		p.auditJudgement(by, run, about, "", approve.Decision{Verdict: approve.Ask, Reason: why})
		return
	}
	rev, err := p.Catalog().Get(ctx, run.Tenant, run.Agent, run.Rev)
	if err != nil {
		// The agent's rules cannot be read: a person answers.
		p.log.Warn("platform: a question's agent could not be read; a person answers", "tenant", run.Tenant, "run", run.ID, "question", q.ID, "error", err)
		return
	}
	p.askApprover(ctx, run, about, m, approve.DescribeQuestion(p.approvalQuestion(run, q, rev.Spec, by)), by)
}

// answerable reports whether the approval agent may be asked to answer q
// for by, and if not, why, in words the person reads. It answers only what
// by could answer themselves, and only what is theirs: a question for no
// one in particular or for them, in work they asked for.
func (p *Platform) answerable(ctx context.Context, run runs.Run, q event.Question, m inbox.Message, by auth.Principal) (bool, string) {
	name := p.NameOf(run.Tenant, by.Subject)
	switch {
	case !isPersonQuestion(q):
		return false, "It is not a question for a person."
	case q.Kind == event.QuestionChoice && len(q.Options) == 0:
		return false, "It offers no options to choose from, so a person answers it."
	case !by.Can(auth.RunsWrite):
		return false, fmt.Sprintf("%s may not answer questions now, so a person does.", name)
	}
	if who := strings.TrimSpace(q.AskedOf); who != "" && who != by.Subject {
		// A named person, a role, or the experts: never answered for them.
		return false, fmt.Sprintf("It asks %s, not %s, so they answer it.", oneLine(who, 80), name)
	}
	if !slices.Contains(p.selfApprovers(run, false), by.Subject) {
		return false, fmt.Sprintf("Someone other than %s asked for this work, so a person answers what it asks.", name)
	}
	i := slices.IndexFunc(m.Parts, func(x inbox.Part) bool { return x.Question != nil && x.Question.ID == q.ID })
	if i < 0 {
		return false, "Its question could not be read, so a person answers it."
	}
	// Whether by could answer it themselves, as answering will check again.
	plain := QuestionAnswer{Action: ActReply, Text: "?"}
	if q.Kind == event.QuestionChoice {
		plain = QuestionAnswer{Action: ActChoose, Choice: q.Options[0]}
	}
	if err := p.checkAnswer(ctx, by, readerOf(by), m, *m.Parts[i].Question, plain, false); err != nil {
		return false, fmt.Sprintf("It is not for %s to answer, so a person does.", name)
	}
	return true, ""
}

// approvalQuestion is what the approval agent reads about a question: the
// question, its options, whom it asked, whom it would answer as, and the
// session so far.
func (p *Platform) approvalQuestion(run runs.Run, q event.Question, spec wire.AgentSpec, by auth.Principal) approve.Question {
	name := p.NameOf(run.Tenant, by.Subject)
	return approve.Question{
		Agent:       run.Agent,
		Description: spec.Description,
		Kind:        q.Kind,
		Prompt:      q.Prompt,
		Options:     slices.Clone(q.Options),
		AskedOf:     q.AskedOf,
		For:         name,
		Policy: []string{
			fmt.Sprintf("%s set the session to Auto, so you may answer in their place, and only what they have already settled in the session.", name),
			"Ask a person about a preference they have not stated, money or spending, a credential or secret, anything that cannot be undone, or anything asked of someone else.",
			"Your answer is shown to them as yours, and they can correct it.",
		},
		Session: p.sessionSoFar(run),
	}
}

// answerVia is the prefix of an answer's note, and of what the card shows,
// when the approval agent answered.
const answerVia = "Answered by the approval agent: "

// answerFrom is the answer d gives to q, or d turned to Ask with why when
// it gives none that fits: no answer, or a choice that is not one of the
// options.
func answerFrom(q *inbox.QuestionPart, d approve.Decision) (QuestionAnswer, approve.Decision) {
	ask := func(why string) (QuestionAnswer, approve.Decision) {
		// What it would have answered is not given, so it is not kept.
		d.Verdict, d.Reason, d.Answer = approve.Ask, why, ""
		return QuestionAnswer{}, d
	}
	text := strings.TrimSpace(d.Answer)
	switch {
	case q == nil:
		return ask("The question could not be read, so a person will answer it.")
	case text == "":
		return ask("The approval agent gave no answer, so a person will.")
	case q.Kind == event.QuestionChoice:
		pick := ""
		if slices.Contains(q.Options, text) {
			pick = text
		} else {
			for _, o := range q.Options {
				if strings.EqualFold(strings.TrimSpace(o), text) {
					if pick != "" {
						pick = ""
						break
					}
					pick = o
				}
			}
		}
		if pick == "" {
			return ask("The approval agent's answer is not one of the options, so a person will choose.")
		}
		d.Answer = pick
		return QuestionAnswer{Action: ActChoose, Choice: pick, Note: answerVia + d.Reason}, d
	case q.Kind == event.QuestionReply:
		d.Answer = text
		return QuestionAnswer{Action: ActReply, Text: text, Note: answerVia + d.Reason}, d
	}
	return ask("It is not a question the approval agent answers.")
}

// fileAnswer answers the question an approval run decided in Auto, as by, unless the run could not decide, its answer cannot be read
// or does not fit, or it handed the question to a person. A denial of a
// question hands it to a person.
func (p *Platform) fileAnswer(ctx context.Context, decided, parked runs.Run, m inbox.Message, about judgedAbout, by auth.Principal) error {
	var d approve.Decision
	switch {
	case decided.Status != runs.Completed:
		why := "its run did not finish"
		if decided.Cause != "" {
			why = oneLine(decided.Cause, 200)
		}
		d = approve.Decision{Verdict: approve.Ask, Reason: "The approval agent could not decide (" + why + "), so a person will."}
	default:
		var err error
		if d, err = approve.Parse(decided.Output); err != nil {
			d = approve.Decision{Verdict: approve.Ask, Reason: "The approval agent's answer could not be read, so a person will."}
		}
	}
	var answer QuestionAnswer
	switch d.Verdict {
	case approve.Allow:
		var q *inbox.QuestionPart
		if i := slices.IndexFunc(m.Parts, func(x inbox.Part) bool { return x.Question != nil && x.Question.ID == about.Question }); i >= 0 {
			q = m.Parts[i].Question
		}
		answer, d = answerFrom(q, d)
	case approve.Deny:
		// A question is not denied: it is a person's to answer.
		d.Verdict, d.Answer = approve.Ask, ""
	default:
		d.Answer = ""
	}
	p.judge(decided.Tenant, m, about.Question, decided.ID, d)
	if d.Verdict != approve.Allow {
		p.log.Info("platform: the approval agent handed a question to a person", "tenant", parked.Tenant, "run", parked.ID, "question", about.Question, "reason", d.Reason)
		p.auditJudgement(by, parked, about, decided.ID, d)
		return nil
	}
	actx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	if _, first, err := p.answerFirst(actx, by, m, answer); err != nil || !first {
		p.log.Info("platform: a question was left for a person", "tenant", parked.Tenant, "run", parked.ID, "question", about.Question, "error", err)
		if err != nil {
			// The card says what happened, not an answer that was not given.
			left := approve.Decision{Verdict: approve.Ask, Reason: "The approval agent's answer could not be given, so a person will answer.", References: d.References, Fields: d.Fields}
			p.judge(decided.Tenant, m, about.Question, decided.ID, left)
			p.auditJudgement(by, parked, about, decided.ID, left)
		}
		return nil
	}
	p.auditJudgement(by, parked, about, decided.ID, d)
	return nil
}
