package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/urmzd/fiber/pkg/agentrt/payload"
	"github.com/urmzd/fiber/pkg/envelope"
	"github.com/urmzd/fiber/pkg/runtime"

	"github.com/urmzd/warp/pkg/auth"
	"github.com/urmzd/warp/pkg/authz"
	"github.com/urmzd/warp/pkg/clients"
	"github.com/urmzd/warp/pkg/inbox"
	"github.com/urmzd/warp/pkg/projects"
	"github.com/urmzd/warp/pkg/runs"
	"github.com/urmzd/warp/pkg/threads"
)

// People in the loop.
//
// A run parks on a person when its agent calls a tool its spec lists under
// approvals, or asks with ask_person. fiber owns the waiting: the question
// is recorded on the run in the ledger, the run holds no worker while it
// waits, and the first answer recorded is the answer. The platform owns who
// is asked and where: each question becomes one inbox message, addressed to
// the people who may answer it and to nobody else, and answering it there,
// on the run, or in Slack, is one call to the runtime.
//
// Who may answer is the runtime's rule, decided from the run's pinned
// revision: approvers or answerers when the spec names them, else anyone in
// the workspace, and never the person whose request raised a call they are
// asked to approve unless the spec allows self-approval. The platform
// addresses the message by the same rule, so a question lands only where it
// can be answered, and checks it again before it asks the runtime. A role in
// those lists is one of this workspace's roles, from its authorization policy.

// Actions on a question part. Each needs runs:write: answering resumes a run.
const (
	ActApprove = "approve"
	ActDeny    = "deny"
	ActChoose  = "choose"
	ActReply   = "reply"
)

// Outcomes a question part can settle with besides a person's answer.
const (
	// OutcomeExpired is a question nobody answered in time. The run carried
	// on with its default.
	OutcomeExpired = "expired"
	// OutcomeStopped is a question whose run was stopped while it waited.
	OutcomeStopped = "stopped"
	// OutcomeClosed is a question whose run ended without an answer to it.
	OutcomeClosed = "closed"
)

// ActAs names the person a surface authenticated itself and answers for: a
// Slack user of a Slack client. Only the credential the client names may use
// it, and only for the people the client maps. Anyone else is nobody.
type ActAs struct {
	Client string `json:"client"`
	User   string `json:"user"`
}

// QuestionAnswer is what a person said to a question.
type QuestionAnswer struct {
	// Action is approve, deny, choose or reply.
	Action string
	// Arguments replace the model's on an approval. They must satisfy the
	// tool's schema.
	Arguments json.RawMessage
	Choice    string
	Text      string
	Note      string
	ActAs     *ActAs
}

func questionKey(run, id string) string { return "question:" + run + ":" + id }

// questionParts maps a question's kind to its part and message kinds.
func questionParts(kind string) (inbox.PartType, inbox.Kind, bool) {
	switch kind {
	case payload.QuestionApproval:
		return inbox.PartApproval, inbox.KindApproval, true
	case payload.QuestionChoice:
		return inbox.PartChoice, inbox.KindClarification, true
	case payload.QuestionReply:
		return inbox.PartReply, inbox.KindClarification, true
	}
	return "", "", false
}

// answering carries the person an answer is made by, and the roles they
// hold, to the runtime's role check for that answer.
type answeringKey struct{}

type answering struct {
	subject string
	roles   []string
}

// rolesOf is the runtime's role resolver (runtime.WithRoles). The roles of
// the person answering are the ones they were authenticated with: a
// signed-in person's, or for someone a surface answers for, what the
// workspace's policy grants their subject. Anyone else holds no role, so a
// role admits nobody it was not asked about.
func (p *Platform) rolesOf(ctx context.Context, _ string, principal string) ([]string, error) {
	if a, ok := ctx.Value(answeringKey{}).(answering); ok && a.subject == principal {
		return slices.Clone(a.roles), nil
	}
	return nil, nil
}

// requesterNames is every name the person whose request raised a run goes
// by: the actor's id and its id qualified by the trigger, as the runtime
// compares an approver against them, and every principal a Slack client maps
// that person to. A Slack connector raises a run as the Slack user ("U0ADA"),
// who is a principal ("ada") only through some client's people map, and that
// map is often not on the client that received the message (a people map
// needs a machine, so an app built on the API holds it). Without it, ada
// could approve her own call. So the actor is looked up in every Slack client
// of the tenant serving the run's Slack team, enabled or not: the team is the
// one the run was accepted from (its source address, written by the
// connector whose client verified the request), never anything an answerer
// says. The names are pinned when the run is accepted, and joined with the
// current ones, so removing a mapping later reopens nothing.
//
// The runtime knows nothing of these maps (fiber's AnswerRequest has no
// requester alias), so the platform holds this rule itself, before it asks
// the runtime. A GitHub connector's actor is a GitHub login, which no client
// maps to a principal, so a person approving a call their own GitHub event
// raised cannot be detected.
func (p *Platform) requesterNames(run runs.Run) []string {
	out := slices.Clone(run.Requesters())
	for _, n := range p.resolveRequester(run.Tenant, run.Tag(), run.Client) {
		if !slices.Contains(out, n) {
			out = append(out, n)
		}
	}
	return out
}

// resolveRequester is every name a tag's actor goes by now.
func (p *Platform) resolveRequester(tenant string, tag envelope.Tag, client string) []string {
	id := strings.TrimSpace(tag.Actor.ID)
	if id == "" {
		return nil
	}
	out := []string{id}
	if tag.Origin != "" {
		out = append(out, tag.Origin+":"+id)
	}
	// The person the run acts for asked for it, by whatever name.
	if u := strings.TrimSpace(tag.Actor.User); u != "" && !slices.Contains(out, u) {
		out = append(out, u)
	}
	if tag.Origin != string(clients.Slack) {
		return out
	}
	team := ""
	if tag.Source.Connector == string(clients.Slack) {
		team = tag.Source.Workspace
	}
	if team == "" && client != "" {
		if c, err := p.Clients.Get(tenant, client); err == nil {
			team = c.Identity.Team
		}
	}
	if team == "" {
		return out
	}
	for _, c := range p.Clients.List(tenant) {
		if c.Surface != clients.Slack || c.Identity.Team != team || c.Slack == nil {
			continue
		}
		if who := c.Slack.People[id]; who != "" && !slices.Contains(out, who) {
			out = append(out, who)
		}
	}
	return out
}

// selfApprovers are the people who may not approve a call in a run: whoever
// asked for the run, and whoever started the tree it is in, by every name
// they go by. Empty when the run's spec allows self-approval.
func (p *Platform) selfApprovers(run runs.Run, selfApproval bool) []string {
	if selfApproval {
		return nil
	}
	out := p.requesterNames(run)
	if run.Root != "" && run.Root != run.ID {
		if root, err := p.Runs.Get(run.Tenant, run.Root); err == nil {
			out = append(out, p.requesterNames(root)...)
		}
	}
	return out
}

// nobody is an audience no reader is in. A question whose rules cannot be
// read is addressed to it, so it lands nowhere rather than everywhere.
const nobody = "\x00nobody"

// Whom a question is for, as a surface may say it to someone it is not
// for (Ask.For).
const (
	// ForAnyone is anyone in the workspace who may resume runs.
	ForAnyone = "anyone"
	// ForPerson is one person: the person the run acts for, whose own
	// credential an answer may reach.
	ForPerson = "person"
	// ForExperts is the experts on a topic of the project the run is filed
	// in, or every expert there for no topic.
	ForExperts = "experts"
	// ForAnswerers is the people the agent's rules name, or whom it asked.
	ForAnswerers = "answerers"
	// ForOwners is the owners of the nearest project on the run's chain, a
	// question for the experts where there is no expert (nearestOwners).
	ForOwners = "owners"
	// ForNobody is nobody: the rules of the run, or of a run that called
	// it, cannot be read.
	ForNobody = "nobody"
)

// addressee is whom a question is for, as addressOf decided it: a kind
// (ForAnyone and the rest), the experts' topic, or the person by subject.
type addressee struct {
	kind   string
	topic  string
	person string
	// project is the project whose owners a question for ForOwners is
	// for: "" is the workspace.
	project string
}

// addressOf decides who a question is for: the people who may answer it,
// narrowed to whom the agent asked when that is one of them. to says whom
// that is, in a way a surface can say it.
func (p *Platform) addressOf(ctx context.Context, run runs.Run, q payload.Question) (audience, excluded []string, to addressee) {
	rev, err := p.Agents.Get(ctx, run.Tenant, run.Agent, run.Rev)
	if err != nil {
		p.log.Warn("platform: a question's rules could not be read, so it is addressed to nobody", "run", run.ID, "question", q.ID, "error", err)
		return []string{nobody}, nil, addressee{kind: ForNobody}
	}
	if q.Kind != payload.QuestionApproval {
		// A reply or a choice steers the run, and its callers, so one whose
		// answer may reach a person's own credential asks them alone
		// (maySteer), even when it asks the experts: an expert's words
		// would reach the person's tools with the person's credential.
		switch person := p.steeredFor(ctx, run); person {
		case "":
		case nobody:
			return []string{person}, nil, addressee{kind: ForNobody}
		default:
			return []string{person}, nil, addressee{kind: ForPerson, person: person}
		}
		if experts, topic, asked := p.expertsAsked(run, q); asked {
			if len(experts) == 0 {
				// With no expert on the chain, the question goes to the
				// project's owners, narrowed to the answerers as experts
				// are.
				if owners, from := p.nearestOwners(run); len(owners) > 0 {
					audience = expertAudience(rev.Spec.Answerers, owners)
					if slices.ContainsFunc(audience, func(who string) bool { return slices.Contains(owners, who) }) {
						return audience, nil, addressee{kind: ForOwners, project: from}
					}
					return audience, nil, addresseeOf(audience)
				}
			}
			audience = expertAudience(rev.Spec.Answerers, experts)
			if slices.ContainsFunc(audience, func(who string) bool { return slices.Contains(experts, who) }) {
				return audience, nil, addressee{kind: ForExperts, topic: topic}
			}
			return audience, nil, addresseeOf(audience)
		}
		audience = audienceOf(rev.Spec.Answerers, q)
		return audience, nil, addresseeOf(audience)
	}
	// A call held by the callee's policy names its own approvers, which
	// replace the caller spec's for this one question, as the runtime
	// decides who may answer it.
	allowed, self := rev.Spec.Approvers, rev.Spec.SelfApproval
	if len(q.Approvers) > 0 {
		allowed, self = q.Approvers, q.SelfApproval
	}
	// Whoever asked for the call does not approve it, and neither does
	// whoever asked for the work it is part of.
	audience = audienceOf(allowed, q)
	return audience, p.selfApprovers(run, self), addresseeOf(audience)
}

// addresseeOf is whom an audience is, when it is not the experts or one
// person: anyone when it names nobody, else the people the rules name.
func addresseeOf(audience []string) addressee {
	if len(audience) == 0 {
		return addressee{kind: ForAnyone}
	}
	return addressee{kind: ForAnswerers}
}

// expertsAsked is who a question that asks the project's experts is for:
// asked_of "sme:<topic>" names the experts for the topic, and "sme" any
// expert, of the nearest project on the run's chain that lists one. asked is
// false for a question that asks someone else.
func (p *Platform) expertsAsked(run runs.Run, q payload.Question) (experts []string, topic string, asked bool) {
	who := strings.TrimSpace(q.AskedOf)
	topic, ok := strings.CutPrefix(who, threads.SMEName+":")
	switch {
	case who == threads.SMEName:
		topic = ""
	case !ok || !projects.ValidTopic(topic):
		return nil, "", false
	}
	experts, _ = p.Projects.Tree(run.Tenant).ExpertsFor(run.Project, topic)
	return experts, topic, true
}

// nearestOwners is whom a question for the experts goes to when the run's
// chain lists no expert: the owners of the nearest project on the chain, the
// run's own first and the workspace last, that names any by its own entries
// who own the run's project now. An entry for everyone names no owner, and
// one that grants nothing, as under a members-only project that does not let
// its person in, is passed over. A run in a private thread has none: its
// owners do not take part in it, so the question stays with whoever may
// answer there. from is the project that names them. Nil when no project on
// the chain names one.
func (p *Platform) nearestOwners(run runs.Run) (owners []string, from string) {
	at := placeOfRun(run)
	if p.privateThread(run.Tenant, at.Thread) {
		return nil, ""
	}
	tree := p.Projects.Tree(run.Tenant)
	owns := func(who string) bool {
		// A person with the roles they held when last seen, as the
		// directory knows them.
		r := projects.Reader{Subject: who}
		if role, ok := strings.CutPrefix(who, projects.RolePrefix); ok {
			// The holders of a role, as one who holds that role alone: one
			// let in above by another role they also hold is passed over,
			// which errs toward the owners above.
			r = projects.Reader{Subject: who, Roles: []string{role}}
		} else if p.People != nil {
			if known, err := p.People.Get(run.Tenant, who); err == nil {
				r.Roles = known.Roles
			}
		}
		return tree.Resolve(at.Project, r).Role == projects.Owner
	}
	for _, pr := range tree.Chain(at.Project) {
		var out []string
		for _, m := range pr.Members {
			if m.Role == projects.Owner && m.Who != projects.Everyone && !slices.Contains(out, m.Who) && owns(m.Who) {
				out = append(out, m.Who)
			}
		}
		if len(out) > 0 {
			return out, pr.Name
		}
	}
	return nil, ""
}

// expertAudience is who a question for experts is addressed to: the experts,
// narrowed to allowed when allowed names anyone. Addressing routes and does
// not admit, so when none of the experts may answer, or there are none, it
// is allowed, as audienceOf does. Who may answer stays the runtime's to
// check.
func expertAudience(allowed, experts []string) []string {
	if len(allowed) == 0 {
		return slices.Clone(experts)
	}
	var out []string
	for _, e := range experts {
		if slices.Contains(allowed, e) {
			out = append(out, e)
		}
	}
	if len(out) == 0 {
		return slices.Clone(allowed)
	}
	return out
}

// audienceOf is who a question is addressed to: allowed, narrowed to whom
// the agent asked when that is one of them, or whom it asked when anyone may.
func audienceOf(allowed []string, q payload.Question) (audience []string) {
	var hint []string
	if who := strings.TrimSpace(q.AskedOf); who != "" {
		hint = []string{who}
		if !strings.HasPrefix(who, "role:") {
			hint = append(hint, "role:"+who)
		}
	}
	switch {
	case len(allowed) == 0:
		audience = hint
	case len(hint) > 0:
		for _, h := range hint {
			if slices.Contains(allowed, h) {
				audience = append(audience, h)
			}
		}
		if len(audience) == 0 {
			audience = slices.Clone(allowed)
		}
	default:
		audience = slices.Clone(allowed)
	}
	return audience
}

// raiseQuestion puts a question a run is parked on in the inbox, once. via is
// the run whose stream showed it, when that is not the run that asked.
func (p *Platform) raiseQuestion(ctx context.Context, run runs.Run, q payload.Question, via string) (inbox.Message, bool) {
	if _, _, ok := questionParts(q.Kind); !ok || q.ID == "" {
		return inbox.Message{}, false
	}
	key := questionKey(run.ID, q.ID)
	if m, ok := p.Inbox.ByKey(run.Tenant, key); ok {
		return m, true
	}
	msg, _ := p.questionMessage(ctx, run, q, via)
	at := q.At
	if at.IsZero() {
		at = p.now()
	}
	m, raised := p.raiseInbox(run.Tenant, key, msg, at)
	if raised {
		p.log.Info("platform: question raised", "tenant", run.Tenant, "run", run.ID, "question", q.ID, "kind", q.Kind,
			"audience", strings.Join(msg.Audience, ","), "message", m.ID)
		p.wakeAtExpiry(run.Tenant, run.ID, q)
	}
	return m, true
}

// questionMessage is the inbox message a question is raised as, addressed
// to the people it is for, and whom that is.
func (p *Platform) questionMessage(ctx context.Context, run runs.Run, q payload.Question, via string) (inbox.Message, addressee) {
	partType, kind, _ := questionParts(q.Kind)
	part := &inbox.QuestionPart{
		Run: run.ID, ID: q.ID, Kind: q.Kind, Agent: run.Agent, Prompt: q.Prompt, Options: slices.Clone(q.Options),
		AskedOf: q.AskedOf, ExpiresAt: q.ExpiresAt, AskedAt: q.At, Via: via,
		Approvers: slices.Clone(q.Approvers), SelfApproval: q.SelfApproval,
	}
	if q.Agent != "" {
		part.Agent = q.Agent
	}
	// Who alone may say anything to it, so a surface offers nobody else a
	// note or an edit (maySteer). A run whose caller cannot be read takes
	// words from nobody, which is not said.
	if person := p.steeredFor(ctx, run); person != nobody {
		part.SteeredFor = person
	}
	if q.Tool != nil {
		part.Tool = &inbox.QuestionTool{
			Name: q.Tool.Name, CallID: q.Tool.CallID, Arguments: q.Tool.Arguments, Schema: q.Tool.Schema,
			ActsForPerson: q.Tool.ActsForPerson,
		}
	}
	switch {
	case q.Kind == payload.QuestionApproval:
		part.Default = payload.DecisionDeny
	case q.Default != nil && q.Default.Choice != "":
		part.Default = q.Default.Choice
	case q.Default != nil:
		part.Default = q.Default.Text
	}
	audience, excluded, to := p.addressOf(ctx, run, q)
	// The subject says whom the agent asked when that is not the reader
	// alone, so an expert knows why it reached them.
	asks := " asks: "
	if to.kind == ForOwners {
		asks = " asks the project's owners: "
	}
	if to.kind == ForExperts {
		asks = " asks the experts: "
		if to.topic != "" {
			asks = " asks the " + to.topic + " experts: "
		}
	}
	subject := part.Agent + asks + headline(q.Prompt, maxQuestionSubject)
	severity := inbox.Info
	if q.Kind == payload.QuestionApproval {
		severity = inbox.Warning
		if q.Tool != nil {
			subject = part.Agent + " wants to run " + q.Tool.Name
		}
	}
	return inbox.Message{
		Kind: kind, Subject: subject, From: inbox.From{Kind: "agent", Name: part.Agent}, Severity: severity,
		Audience: audience, Excluded: excluded,
		Source: inbox.Source{Run: run.ID, Thread: run.Thread, Agent: run.Agent, Project: run.Project, Target: questionTarget(q)},
		Parts:  []inbox.Part{{Type: partType, Question: part}},
	}, to
}

// maxQuestionSubject bounds how much of a question its message's subject
// holds, in bytes. The whole question is the message's part.
const maxQuestionSubject = 80

// headline is text as one line, at most limit bytes: its lines are joined,
// so a question asked below a line of context is still in it, and a longer
// one is cut at a word and ends with an ellipsis, so a subject never stops
// part of the way through without saying so.
func headline(text string, limit int) string {
	line := strings.Trim(strings.Join(strings.Fields(text), " "), "\"'`.")
	if len(line) <= limit {
		return line
	}
	const more = "…"
	cut := clip(line, limit-len(more))
	// A cut that ends a word is kept; one inside a word goes back to the
	// space before it.
	if line[len(cut)] != ' ' {
		if i := strings.LastIndexByte(cut, ' '); i > len(cut)/2 {
			cut = cut[:i]
		}
	}
	return strings.TrimRight(cut, " ,;:-") + more
}

// recordWait bounds how long an answer waits for the runtime to record a
// question it has already announced.
const recordWait = 2 * time.Second

// expiryGrace is how long after a question's deadline its run is woken to
// take the default. The worker's own poll wakes it too, later.
const expiryGrace = 50 * time.Millisecond

// wakeAtExpiry wakes a parked run once its question has expired, so the
// default applies when it is due rather than at the worker's next poll. The
// run is woken only if it is still waiting on that question. Waking a run
// early would hold its place in the queue past the answer that should wake
// it, so it is never enqueued ahead of time.
func (p *Platform) wakeAtExpiry(tenant, runID string, q payload.Question) {
	if q.ExpiresAt.IsZero() || p.queue == nil {
		return
	}
	p.wakers.Lock()
	defer p.wakers.Unlock()
	if p.wakersDone {
		return
	}
	if p.wakerSet == nil {
		p.wakerSet = map[*time.Timer]struct{}{}
	}
	var t *time.Timer
	t = time.AfterFunc(max(q.ExpiresAt.Sub(p.now()), 0)+expiryGrace, func() {
		p.wakers.Lock()
		_, live := p.wakerSet[t]
		delete(p.wakerSet, t)
		p.wakers.Unlock()
		if !live {
			return
		}
		run, err := p.Runs.Get(tenant, runID)
		if err != nil || run.Status != runs.Parked || run.Question == nil || run.Question.ID != q.ID {
			return
		}
		_ = p.queue.Enqueue(context.Background(), runID, 0)
	})
	p.wakerSet[t] = struct{}{}
}

// stopWakers cancels every wake still to come. A platform that has stopped
// wakes nothing: the worker's poll wakes the run on the next start.
func (p *Platform) stopWakers() {
	p.wakers.Lock()
	defer p.wakers.Unlock()
	p.wakersDone = true
	for t := range p.wakerSet {
		t.Stop()
	}
	p.wakerSet = nil
}

// observeQuestion folds the lifecycle events that concern questions into the
// inbox: a question raised, a question answered, and a run that ended while
// one was open.
func (p *Platform) observeQuestion(ctx context.Context, e envelope.Event) {
	switch e.Kind {
	case envelope.KindParked:
		body, err := payload.Decode[payload.Lifecycle](e)
		if err != nil || body.Question == nil {
			return
		}
		if run, err := p.Runs.Get(e.Tenant, e.RunID); err == nil {
			p.raiseQuestion(ctx, run, *body.Question, "")
		}
	case envelope.KindChildParked:
		// A run waiting on a run it called shows that run's question. The
		// question is the child's, and so is its message.
		cq, err := payload.Decode[payload.ChildQuestion](e)
		if err != nil || cq.RunID == "" {
			return
		}
		if child, err := p.Runs.Get(e.Tenant, cq.RunID); err == nil {
			p.raiseQuestion(ctx, child, cq.Question, e.RunID)
		}
	case envelope.KindResumed:
		p.answers.Lock()
		defer p.answers.Unlock()
		body, err := payload.Decode[payload.Lifecycle](e)
		if err != nil || body.Answer == nil {
			return
		}
		p.settleQuestion(e.Tenant, e.RunID, body.Answer.QuestionID, outcomeOf(*body.Answer, p.now()), "")
	}
}

// outcomeOf is how an answer settles its part.
func outcomeOf(a payload.Answer, now time.Time) inbox.Outcome {
	at := a.At
	if at.IsZero() {
		at = now
	}
	o := inbox.Outcome{By: a.By, At: at, Note: a.Note}
	switch {
	case a.Expired:
		o.Action, o.By = OutcomeExpired, "system"
		switch {
		case a.Decision == payload.DecisionDeny:
			o.Note = "Nobody answered in time, so it was denied"
		case a.Choice != "":
			o.Note = "Nobody answered in time, so it went with " + a.Choice
		case a.Text != "":
			o.Note = "Nobody answered in time, so it went with " + oneLine(a.Text, 200)
		default:
			o.Note = "Nobody answered in time"
		}
	case a.Decision == payload.DecisionApprove:
		o.Action = ActApprove
		if len(bytes.TrimSpace(a.Arguments)) > 0 {
			o.Note = strings.TrimSpace("With edited arguments. " + a.Note)
		}
	case a.Decision == payload.DecisionDeny:
		o.Action = ActDeny
	case a.Choice != "":
		o.Action, o.Note = ActChoose, a.Choice
	default:
		o.Action, o.Note = ActReply, a.Text
	}
	return o
}

// settleQuestion records how a question was resolved on its message. A part
// already settled keeps its first outcome.
func (p *Platform) settleQuestion(tenant, runID, questionID string, o inbox.Outcome, via string) (inbox.Message, bool) {
	m, ok := p.Inbox.ByKey(tenant, questionKey(runID, questionID))
	if !ok {
		return inbox.Message{}, false
	}
	if via != "" {
		o.Note = strings.TrimSpace(o.Note + " (" + via + ")")
	}
	for _, part := range m.Parts {
		if part.Question != nil && part.State == inbox.PartOpen {
			if _, changed, _ := p.Inbox.Resolve(tenant, m.ID, part.ID, o); changed {
				p.log.Info("platform: question settled", "tenant", tenant, "run", runID, "question", questionID,
					"outcome", o.Action, "by", o.By, "message", m.ID)
			}
		}
	}
	return m, true
}

// settleQuestionsOf settles every question still open on a run that ended:
// stopped while it waited, or ended without an answer to it.
func (p *Platform) settleQuestionsOf(run runs.Run) {
	p.answers.Lock()
	defer p.answers.Unlock()
	for _, kind := range []inbox.Kind{inbox.KindApproval, inbox.KindClarification} {
		for _, m := range p.Inbox.OpenOf(run.Tenant, kind) {
			if m.Source.Run != run.ID {
				continue
			}
			for _, part := range m.Parts {
				if part.Question == nil || part.State != inbox.PartOpen {
					continue
				}
				o := inbox.Outcome{Action: OutcomeClosed, By: "system", At: p.now(), Note: "The run ended without an answer"}
				if run.Status == runs.Stopped {
					o.Action, o.Note = OutcomeStopped, run.Cause
				}
				_, _, _ = p.Inbox.Resolve(run.Tenant, m.ID, part.ID, o)
			}
		}
	}
}

// questionOf is the question a part holds, as the runtime checks an answer
// against it.
func questionOf(q inbox.QuestionPart) payload.Question {
	out := payload.Question{ID: q.ID, Kind: q.Kind, Prompt: q.Prompt, Options: q.Options}
	if q.Tool != nil {
		out.Tool = &payload.QuestionTool{Name: q.Tool.Name, Arguments: q.Tool.Arguments, Schema: q.Tool.Schema, ActsForPerson: q.Tool.ActsForPerson}
	}
	return out
}

// answerOf turns what a person said into the runtime's answer, and checks it
// fits the question before anything is recorded.
func answerOf(q inbox.QuestionPart, a QuestionAnswer) (payload.Answer, error) {
	var out payload.Answer
	if args := bytes.TrimSpace(a.Arguments); len(args) == 0 || string(args) == "null" {
		a.Arguments = nil
	}
	switch {
	case q.Kind == payload.QuestionApproval && (a.Action == ActApprove || a.Action == ActDeny):
		out = payload.Answer{Decision: a.Action, Arguments: a.Arguments, Note: a.Note}
	case q.Kind == payload.QuestionChoice && a.Action == ActChoose:
		out = payload.Answer{Choice: a.Choice, Note: a.Note}
	case q.Kind == payload.QuestionReply && a.Action == ActReply:
		text := a.Text
		if text == "" {
			// The mailbox's reply control sends its words as a note.
			text = a.Note
		}
		out = payload.Answer{Text: text}
	default:
		return out, fmt.Errorf("%w: a %s question does not take %q", ErrAction, q.Kind, a.Action)
	}
	if err := runtime.CheckAnswer(questionOf(q), out); err != nil {
		return out, fmt.Errorf("%w: %s", ErrAction, strings.TrimPrefix(err.Error(), "runtime: invalid: "))
	}
	return out, nil
}

// answerer decides who is answering: the caller, or the person a surface
// answers for. A surface may only answer for a person its client maps, with
// the credential its client names. Anything else fails closed.
func (p *Platform) answerer(who auth.Principal, as *ActAs) (inbox.Reader, string, error) {
	if as == nil {
		return readerOf(who), "", nil
	}
	c, err := p.Clients.Get(who.Tenant, as.Client)
	if err != nil {
		return inbox.Reader{}, "", fmt.Errorf("%w: this credential does not answer for people of that client", ErrForbidden)
	}
	subject, ok := c.ActsFor(who.Subject, as.User)
	if !ok {
		return inbox.Reader{}, "", fmt.Errorf("%w: that person is not mapped to anyone in this workspace", ErrForbidden)
	}
	return p.mapped(who, subject), string(c.Surface) + ":" + as.User, nil
}

// mapped is who a person a surface answers for is: their subject, and the
// roles the workspace's policy grants it. A Slack click carries no sign-in,
// so those grants are all that is known of them.
func (p *Platform) mapped(who auth.Principal, subject string) inbox.Reader {
	var held []string
	for _, r := range who.Policy().Roles(authz.Identity{Subject: subject, Tenant: who.Tenant}) {
		held = append(held, string(r))
	}
	return inbox.Reader{Subject: subject, Roles: held}
}

// mayResume reports whether a person a surface answers for holds runs:write
// themselves. The credential holding it is not enough: a viewer is no
// approver because a machine relays their click.
func mayResume(who auth.Principal, person inbox.Reader) bool {
	return principalOf(who, person).Can(auth.RunsWrite)
}

// isSelfApproval reports whether the person answering, or the credential
// answering for them, asked for the run or the tree it is in, unless the
// run's pinned spec allows self-approval. A spec that cannot be read refuses.
func (p *Platform) isSelfApproval(ctx context.Context, tenant string, q inbox.QuestionPart, by, via string) (bool, error) {
	run, err := p.Runs.Get(tenant, q.Run)
	if err != nil {
		return false, err
	}
	rev, err := p.Agents.Get(ctx, tenant, run.Agent, run.Rev)
	if err != nil {
		return false, fmt.Errorf("%w: the rules of this run cannot be read, so nobody approves its calls", ErrForbidden)
	}
	// A question naming its own approvers carries its own self-approval
	// rule, as the runtime reads it.
	self := rev.Spec.SelfApproval
	if len(q.Approvers) > 0 {
		self = q.SelfApproval
	}
	names := p.selfApprovers(run, self)
	return slices.Contains(names, by) || slices.Contains(names, via), nil
}

// maySteer reports whether the person answering may say what they said to a
// question a run asks, and if not, why. What they say steers what the run
// does next, and what its callers do, since a called run's answer is its
// caller's tool result. So where that may reach a person's own credentials
// (steeredFor), only that person replies or picks an option, and anyone
// else approves or denies a call only as it was asked: with no note, which
// the model is shown on a denial, and no edited arguments, which it reads
// back with what the call found, even of a call that reaches only a shared
// credential. Otherwise their words, or their pick of its options, would
// reach the person's tools with the person's credential. Any other run is
// answered by whoever its rules allow.
func (p *Platform) maySteer(ctx context.Context, tenant string, q inbox.QuestionPart, a QuestionAnswer, by string) error {
	args := bytes.TrimSpace(a.Arguments)
	edited := len(args) > 0 && string(args) != "null"
	if q.Kind == payload.QuestionApproval && strings.TrimSpace(a.Note) == "" && !edited {
		return nil
	}
	run, err := p.Runs.Get(tenant, q.Run)
	if err != nil {
		return err
	}
	switch person := p.steeredFor(ctx, run); {
	case person == "" || by == person:
		return nil
	case person == nobody:
		return fmt.Errorf("%w: the run that called this one cannot be read, so nobody may answer what it asks", ErrForbidden)
	case q.Kind == payload.QuestionApproval:
		name := p.NameOf(tenant, person)
		return fmt.Errorf("%w: this run uses %s's own credentials, so only %s may edit its calls or add a note the agent reads; approve or deny it as asked", ErrForbidden, name, name)
	default:
		name := p.NameOf(tenant, person)
		return fmt.Errorf("%w: this run uses %s's own credentials, so only %s may answer what it asks", ErrForbidden, name, name)
	}
}

// AnswerQuestion answers a question a run is parked on, for a person. It is
// the one path every surface answers through: the inbox, the run, and Slack.
//
// The caller needs runs:write. The person answering is the caller, or the
// person a surface answers for (see ActAs). The question must be addressed to
// them, and the runtime must agree they may answer it; otherwise it is
// ErrForbidden and nothing changes. A question already answered settles its
// message with the answer that was recorded first, whoever asks.
func (p *Platform) AnswerQuestion(ctx context.Context, who auth.Principal, runID, questionID string, a QuestionAnswer) (inbox.Message, error) {
	run, err := p.Runs.Get(who.Tenant, runID)
	if err != nil {
		return inbox.Message{}, err
	}
	m, ok := p.Inbox.ByKey(who.Tenant, questionKey(runID, questionID))
	if !ok {
		// The question's event may not have been folded in yet. The
		// runtime's record of it is the authority.
		// The event announces the question a moment before the runtime's
		// record of it is written, so the run's own report is used first.
		q, pending := payload.Question{}, false
		if run.Question != nil && run.Question.ID == questionID {
			q, pending = *run.Question, true
		} else if got, ok, qerr := p.rt.PendingQuestion(ctx, who.Tenant, runID); qerr == nil && ok {
			q, pending = got, true
		}
		if !pending || q.ID != questionID {
			return inbox.Message{}, fmt.Errorf("%w: run %q has no question %q", inbox.ErrNotFound, runID, questionID)
		}
		if m, ok = p.raiseQuestion(ctx, run, q, ""); !ok {
			return inbox.Message{}, fmt.Errorf("%w: question %q is not one a person answers", inbox.ErrNotFound, questionID)
		}
	}
	return p.answer(ctx, who, m, a)
}

// checkAnswer reports whether reader may give answer a to the question q of
// message m, and if not, why: what answerFirst checks of the person
// answering before it asks the runtime, which has the last word. who is the
// caller, the reader's surface when it answers for them. logged says the
// answer is being given, so a refusal is logged.
func (p *Platform) checkAnswer(ctx context.Context, who auth.Principal, reader inbox.Reader, m inbox.Message, q inbox.QuestionPart, a QuestionAnswer, logged bool) error {
	refused := func(why string) {
		if logged {
			p.log.Info("platform: an answer was refused", "tenant", who.Tenant, "by", reader.Subject, "via", who.Subject, "run", q.Run, "question", q.ID, "why", why)
		}
	}
	// What the person answering may not read does not exist for them,
	// whichever route asks, before anything is recorded. A surface's
	// machine reads every thread on its surface wherever it is filed, so the
	// route's own check of the caller does not hold the person it answers
	// for to where they may read.
	if !p.readerSeer(who, reader).Message(m) {
		return fmt.Errorf("%w: message %q", inbox.ErrNotFound, m.ID)
	}
	if q.Kind == payload.QuestionApproval {
		// Checked here, from the run as it stands, not only from whom the
		// message was addressed to when it was raised: the runtime does not
		// know a requester by the names a client maps them to, nor who
		// started the tree.
		if self, err := p.isSelfApproval(ctx, who.Tenant, q, reader.Subject, who.Subject); err != nil {
			return err
		} else if self {
			refused("self-approval")
			return fmt.Errorf("%w: %s asked for this work, so someone else must approve its calls", ErrForbidden, p.NameOf(who.Tenant, reader.Subject))
		}
	}
	// Checked here, from the run and its callers as they stand, as well as
	// by whom the message was addressed to when it was raised.
	if err := p.maySteer(ctx, who.Tenant, q, a, reader.Subject); err != nil {
		refused("not the person the run acts for")
		return err
	}
	if !m.For(reader) {
		return fmt.Errorf("%w: %s may not answer this question", ErrForbidden, p.NameOf(who.Tenant, reader.Subject))
	}
	return nil
}

// answer is AnswerQuestion once the message is known.
func (p *Platform) answer(ctx context.Context, who auth.Principal, m inbox.Message, a QuestionAnswer) (inbox.Message, error) {
	got, _, err := p.answerFirst(ctx, who, m, a)
	return got, err
}

// answerFirst is answer, and also reports whether this answer is the one
// the run was given: not when the question no longer waited, because it was
// answered or its run stopped or ended, nor when someone answered it first.
// A message posted as an answer is posted only then (answerFromThread).
func (p *Platform) answerFirst(ctx context.Context, who auth.Principal, m inbox.Message, a QuestionAnswer) (inbox.Message, bool, error) {
	i := slices.IndexFunc(m.Parts, func(x inbox.Part) bool { return x.Question != nil })
	if i < 0 {
		return inbox.Message{}, false, fmt.Errorf("%w: message %q asks no question", ErrAction, m.ID)
	}
	part := m.Parts[i]
	q := *part.Question
	if !who.Can(auth.RunsWrite) {
		return inbox.Message{}, false, fmt.Errorf("%w: answering needs %s", ErrForbidden, auth.RunsWrite)
	}
	reader, via, err := p.answerer(who, a.ActAs)
	if err != nil {
		p.log.Warn("platform: an answer for someone a surface could not name was refused", "tenant", who.Tenant, "by", who.Subject, "run", q.Run, "question", q.ID)
		return inbox.Message{}, false, err
	}
	if a.ActAs != nil && !mayResume(who, reader) {
		p.log.Info("platform: an answer was refused", "tenant", who.Tenant, "by", reader.Subject, "via", who.Subject, "run", q.Run, "question", q.ID, "why", "no runs:write")
		return inbox.Message{}, false, fmt.Errorf("%w: %s may not resume runs (answering needs %s)", ErrForbidden, p.NameOf(who.Tenant, reader.Subject), auth.RunsWrite)
	}
	if err := p.checkAnswer(ctx, who, reader, m, q, a, true); err != nil {
		return inbox.Message{}, false, err
	}
	viewAs := func(recorded bool) (inbox.Message, bool, error) {
		got, err := p.inboxGetAs(who, reader, m.ID)
		return got, recorded && err == nil, err
	}
	if part.State != inbox.PartOpen {
		return viewAs(false)
	}
	ans, err := answerOf(q, a)
	if err != nil {
		return inbox.Message{}, false, err
	}
	actx := context.WithValue(ctx, answeringKey{}, answering{subject: reader.Subject, roles: reader.Roles})
	req := runtime.AnswerRequest{
		Tenant: who.Tenant, RunID: q.Run, QuestionID: q.ID, Answer: ans, By: reader.Subject, Via: who.Subject,
	}
	// Answer wakes the run before returning. Its resumed or terminal event
	// must not settle the message ahead of the surface attribution below.
	p.answers.Lock()
	defer p.answers.Unlock()
	// An approved call to an agent is noted before the runtime is told,
	// because the runtime wakes the run, and the call it lets through checks
	// for the note. It is dropped again unless this approval is the one
	// recorded.
	approved := ans.Decision == payload.DecisionApprove && p.noteApprovedCall(who.Tenant, q, ans)
	got, first, err := p.rt.Answer(actx, req)
	// A question answered the moment it is announced may not be recorded yet.
	// It is asked again for a short while, never past the request. The note
	// is kept while it is asked again, since a later attempt can still record
	// the approval and wake the run.
	for deadline := time.Now().Add(recordWait); errors.Is(err, runtime.ErrNoQuestion) && time.Now().Before(deadline); {
		select {
		case <-ctx.Done():
			err = ctx.Err()
		case <-time.After(20 * time.Millisecond):
			got, first, err = p.rt.Answer(actx, req)
		}
	}
	if approved && (err != nil || got.Decision != payload.DecisionApprove) {
		p.dropApprovedCall(who.Tenant, q)
	}
	switch {
	case errors.Is(err, runtime.ErrNotAllowed):
		p.log.Info("platform: an answer was refused", "tenant", who.Tenant, "by", reader.Subject, "via", who.Subject, "run", q.Run, "question", q.ID)
		return inbox.Message{}, false, fmt.Errorf("%w: %s may not answer this question", ErrForbidden, p.NameOf(who.Tenant, reader.Subject))
	case errors.Is(err, runtime.ErrInvalid):
		return inbox.Message{}, false, fmt.Errorf("%w: %s", ErrAction, strings.TrimPrefix(err.Error(), "runtime: invalid: "))
	case errors.Is(err, runtime.ErrNoQuestion):
		return inbox.Message{}, false, fmt.Errorf("%w: run %q has no question %q", inbox.ErrNotFound, q.Run, q.ID)
	case errors.Is(err, runtime.ErrQuestionClosed):
		// The run stopped or ended while it waited. Say so where it was asked.
		o := inbox.Outcome{Action: OutcomeClosed, By: "system", At: p.now(), Note: "The run ended without an answer"}
		if run, rerr := p.Runs.Get(who.Tenant, q.Run); rerr == nil && (run.Status == runs.Stopped || run.Stopping) {
			o.Action, o.Note = OutcomeStopped, run.Cause
		}
		p.settleQuestion(who.Tenant, q.Run, q.ID, o, "")
		return viewAs(false)
	case err != nil:
		return inbox.Message{}, false, err
	}
	o := outcomeOf(got, p.now())
	if first {
		p.settleQuestion(who.Tenant, q.Run, q.ID, o, via)
	} else {
		// Someone got there first. The message says what they decided.
		p.settleQuestion(who.Tenant, q.Run, q.ID, o, "")
	}
	p.audit(who, m, part.ID, a.Action, fmt.Sprintf("by=%s via=%s first=%t recorded=%s", reader.Subject, via, first, o.Action))
	return viewAs(first)
}
