package platform

import (
	"cmp"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/inbox"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/threads"
	"github.com/urmzd/saige/flow/event"
	"github.com/urmzd/saige/flow/session"
)

// SessionInput selects one agent's continuing conversation. Parent is a session
// ID; external places are accepted only by trusted connector ingress.
type SessionInput struct {
	Uploads     []Upload `json:"uploads,omitempty"`
	Text        string   `json:"text"`
	Agent       string   `json:"agent,omitempty"`
	Parent      string   `json:"parent,omitempty"`
	Label       string   `json:"label,omitempty"`
	Fresh       bool     `json:"fresh,omitempty"`
	Project     string   `json:"project,omitempty"`
	SourceAlert string   `json:"source_alert,omitempty"`
	// Environment and Repos are the place the session runs and the
	// repositories it works on, chosen from SessionOptions.
	Environment string   `json:"environment,omitempty"`
	Repos       []string `json:"repos,omitempty"`
	// Surface and ActAs are a chat surface's thread and the person a
	// message from it is posted for: only that client's machine sends them,
	// and the place's root session is found or started (surface_sessions.go).
	Surface     *SurfaceRef `json:"surface,omitempty"`
	ActAs       *ActAs      `json:"act_as,omitempty"`
	SurfaceKind string      `json:"surface_kind,omitempty"`
	// PostOnly posts a surface's message without starting anything, as a
	// channel the app follows quietly, or catching a thread up, does.
	PostOnly bool `json:"post_only,omitempty"`
	// spoken marks the first message as said in voice mode, for a
	// conversation the messages API starts (sendToNewConsoleRoot).
	spoken bool
}

// SessionMessageOptions choose a mentioned agent's continuing or fresh child.
type SessionMessageOptions struct {
	Agent string `json:"agent,omitempty"`
	Fresh bool   `json:"fresh,omitempty"`
	Label string `json:"label,omitempty"`
}
type SessionPostInput struct {
	MessageInput
	SessionMessageOptions
}
type SessionAuthor struct {
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
	// Display is the name people see when it differs from Name: the
	// workspace's for its entry agent.
	Display string `json:"display,omitempty"`
}
type SessionPart struct {
	Type    string `json:"type"`
	Text    string `json:"text,omitempty"`
	Session string `json:"session,omitempty"`
	Agent   string `json:"agent,omitempty"`
	Status  string `json:"status,omitempty"`
}
type SessionFeedback struct {
	FindingID string `json:"finding_id,omitempty"`
	Rating    string `json:"rating"`
	Reason    string `json:"reason,omitempty"`
	Comment   string `json:"comment,omitempty"`
}
type SessionMessage struct {
	ID              string            `json:"id"`
	Session         string            `json:"session"`
	Author          SessionAuthor     `json:"author"`
	Status          string            `json:"status"`
	Parts           []SessionPart     `json:"parts"`
	At              time.Time         `json:"at"`
	Message         *threads.Message  `json:"message,omitempty"`
	Run             *runs.Run         `json:"run,omitempty"`
	Ask             *Ask              `json:"ask,omitempty"`
	Feedback        *SessionFeedback  `json:"feedback,omitempty"`
	FindingFeedback []SessionFeedback `json:"finding_feedback,omitempty"`
}
type SessionSummary struct {
	Conversation ThreadSummary `json:"conversation"`
	ID           string        `json:"id"`
	Agent        string        `json:"agent"`
	Parent       string        `json:"parent"`
	Label        string        `json:"label,omitempty"`
	Project      string        `json:"project"`
	Title        string        `json:"title"`
	Status       string        `json:"status"`
	UpdatedAt    time.Time     `json:"updated_at"`
	// rank is how far the reader's open alerts raise the session in a
	// listing (prioritizeSessions): 0 for a session with none.
	rank int
}
type SessionView struct {
	SessionSummary
	Usage *event.RunUsage `json:"usage,omitempty"`
	// Voice is what voice mode has used in the session, when it was used.
	Voice    *VoiceUsage           `json:"voice,omitempty"`
	Thread   threads.Thread        `json:"thread"`
	Messages []SessionMessage      `json:"messages"`
	Children []SessionSummary      `json:"children"`
	Links    []session.Link        `json:"links"`
	People   []threads.Participant `json:"people"`
	Next     string                `json:"next,omitempty"`
	Cursor   string                `json:"cursor"`
	More     bool                  `json:"more,omitempty"`
	Summary  string                `json:"summary,omitempty"`
	// Posted is what the message just posted did, on a surface's post only.
	Posted *SessionPosted `json:"posted,omitempty"`
}

// SeesSession checks the session and every parent: sharing a child never leaks
// context from a parent whose access has since been revoked.
func (p *Platform) SeesSession(who auth.Principal, id string) bool {
	return p.Seer(who).Session(id)
}
func (p *Platform) SessionRecord(ctx context.Context, who auth.Principal, id string) (threads.Thread, error) {
	if !p.SeesSession(who, id) {
		return threads.Thread{}, threads.ErrNotFound
	}
	return p.Threads.Get(who.Tenant, id)
}
func (p *Platform) sessionAgent(ctx context.Context, who auth.Principal, project, explicit, text string) (string, string, error) {
	mentions, err := threads.Parse(text, tagLookup{ctx: ctx, p: p, tenant: who.Tenant, sees: p.Seer(who).Agent, tree: p.Projects.Tree(who.Tenant), project: project, person: who.Subject, console: true})
	if err != nil {
		return "", "", err
	}
	for _, m := range mentions {
		if m.Refused != "" {
			return "", "", fmt.Errorf("%w: No agent or person named @%s", threads.ErrInvalid, m.Name)
		}
	}
	if explicit == "" {
		// The one agent a message mentions answers it. Several are each
		// asked for the entry agent, which answers with what they said
		// (carryOut); without it, the first mentioned answers.
		if agents := mentionedAgents(mentions, ""); len(agents) == 1 {
			explicit = agents[0]
		} else if len(agents) > 1 {
			if explicit = p.entryFor(ctx, who.Tenant, project, p.Seer(who).Agent); explicit == "" {
				explicit = agents[0]
			}
		}
	}
	picked := ""
	if explicit == "" {
		d, why, err := p.DefaultForAs(who, "", project, "")
		if err != nil {
			return "", "", err
		}
		if why != "" {
			return "", "", fmt.Errorf("%w: %s", threads.ErrInvalid, why)
		}
		explicit = d.Agent
	}
	if explicit == "" || explicit == Open {
		// Nobody named and no agent set to answer: the message lands (land),
		// on the entry agent, which owns the session and calls whoever fits,
		// so a message in the console always reaches someone.
		landed, err := p.land(ctx, who.Tenant, project, text, p.Seer(who).Agent)
		if errors.Is(err, ErrNoDefault) {
			return "", "", fmt.Errorf("%w: no agent is available here to answer; mention one with @", threads.ErrInvalid)
		}
		if err != nil {
			return "", "", err
		}
		explicit, picked = landed[0].Agent, landed[0].Picked
	}
	if err := p.checkDefault(ctx, who.Tenant, project, explicit, p.Seer(who).Agent); err != nil {
		return "", "", err
	}
	return explicit, picked, nil
}
func (p *Platform) StartSession(ctx context.Context, who auth.Principal, in SessionInput) (SessionView, error) {
	if err := p.checkSessionChoice(ctx, who, in); err != nil {
		return SessionView{}, err
	}
	if err := checkUploads(in.Uploads); err != nil {
		return SessionView{}, err
	}
	in.Text = uploadText(in.Text, in.Uploads)
	ctx = context.WithValue(ctx, sessionReaderKey{}, who)
	if in.Surface != nil {
		v, _, err := p.StartSurfaceSession(ctx, who, in)
		return v, err
	}
	if err := threads.CheckText(in.Text); err != nil {
		return SessionView{}, err
	}
	if _, _, _, _, err := p.author(who, nil); err != nil {
		return SessionView{}, err
	}
	if in.SourceAlert != "" {
		source, err := p.sourceAlertSession(ctx, who, in.SourceAlert)
		if err != nil {
			return SessionView{}, err
		}
		if in.Project != "" && projects.NameOf(in.Project) != source.Project {
			return SessionView{}, fmt.Errorf("%w: a fix session stays in its source alert's project", threads.ErrInvalid)
		}
		if in.Parent == "" {
			in.Parent = source.ID
		} else if !p.sessionDescendantOf(who.Tenant, in.Parent, source.ID) {
			return SessionView{}, fmt.Errorf("%w: a fix session stays in its source alert's session tree", threads.ErrInvalid)
		}
		if in.Label == "" {
			in.Label = "fix-" + in.SourceAlert
		}
	}
	project := in.Project
	var parent threads.Thread
	if in.Parent != "" {
		var err error
		parent, err = p.SessionRecord(ctx, who, in.Parent)
		if err != nil {
			return SessionView{}, err
		}
		if err = p.MayPost(who, Place{Project: parent.Project, Thread: parent.ID}); err != nil {
			return SessionView{}, err
		}
		if project != "" && projects.NameOf(project) != parent.Project {
			return SessionView{}, fmt.Errorf("%w: a child stays in its parent's project", threads.ErrInvalid)
		}
		project = parent.Project
		if in.SourceAlert == "" {
			in.SourceAlert = parent.Session.SourceAlert
		}
	}
	if project == "" && in.Parent == "" {
		pr, err := p.PersonalProject(who)
		if err != nil {
			return SessionView{}, err
		}
		project = pr.Name
	}
	project, ch, err := p.placeFor(who, project, "")
	if err != nil {
		return SessionView{}, err
	}
	agent, picked, err := p.sessionAgent(ctx, who, project, in.Agent, in.Text)
	if err != nil {
		return SessionView{}, err
	}
	ctx = withPicked(ctx, picked)
	par := in.Parent
	if par == "" {
		if strings.TrimSpace(in.Label) != "" {
			sum := sha256.Sum256([]byte(who.Subject + "\x00" + project))
			par = "console://" + hex.EncodeToString(sum[:16])
		} else {
			var b [16]byte
			if _, err := rand.Read(b[:]); err != nil {
				return SessionView{}, err
			}
			par = "console://" + hex.EncodeToString(b[:])
		}
	}
	rec, err := p.Sessions.Open(ctx, who.Tenant, session.Open{Parent: par, Agent: agent, Label: in.Label, Fresh: in.Fresh, Channel: wire.Channel(who.Tenant)})
	if err != nil {
		return SessionView{}, err
	}
	th := threads.Thread{ID: rec.ID, Project: project, Channel: ch.Name, Title: runs.SessionTitle(in.Text), Visibility: threads.Private, Default: agent, Agents: []string{agent}, CreatedBy: who.Subject, Session: &threads.SessionIdentity{Agent: agent, Parent: rec.Key.Parent, Label: rec.Key.Label, SourceAlert: in.SourceAlert, Environment: in.Environment, Repos: slices.Clone(in.Repos)}}
	if parent.ID != "" {
		th.Participants = slices.Clone(parent.Participants)
		th.Visibility = parent.Visibility
	}
	_, err = p.Threads.Create(who.Tenant, th, p.now())
	if err != nil && !errors.Is(err, threads.ErrExists) {
		return SessionView{}, err
	}
	existing, err := p.Threads.Get(who.Tenant, rec.ID)
	if err != nil {
		return SessionView{}, err
	}
	if existing.Session == nil || existing.Session.SourceAlert != in.SourceAlert {
		return SessionView{}, fmt.Errorf("%w: this session belongs to another source alert", threads.ErrInvalid)
	}
	if _, err := p.PostMessage(ctx, who, rec.ID, MessageInput{Text: in.Text, Spoken: in.spoken, Uploads: in.Uploads}); err != nil {
		return SessionView{}, err
	}
	return p.ReadSession(ctx, who, rec.ID, "", "", false)
}

// retitleAfterGreeting gives a session that began with only a greeting the
// title of its first message that asks for something. It renames only a title
// the system derived from that greeting, never one a person set.
func (p *Platform) retitleAfterGreeting(tenant string, th threads.Thread, text string) {
	if runs.Greeting(text) {
		return
	}
	head, err := p.Threads.Summary(tenant, th.ID)
	if err != nil || head.Messages == 0 || !runs.Greeting(head.First) || th.Title != runs.SessionTitle(head.First) {
		return
	}
	next := runs.SessionTitle(text)
	if next == "" || next == th.Title {
		return
	}
	// A failed rename leaves the greeting title, which is still true.
	_, _ = p.Threads.Update(tenant, th.ID, th.Version, p.now(), func(t *threads.Thread) error {
		t.Title = next
		return nil
	})
}
func firstWords(text string, n int) string {
	text = strings.Join(strings.Fields(text), " ")
	r := []rune(text)
	if len(r) > n {
		return string(r[:n])
	}
	return text
}
func (p *Platform) PostSession(ctx context.Context, who auth.Principal, id string, in MessageInput, options ...SessionMessageOptions) (SessionView, error) {
	ctx = context.WithValue(ctx, sessionReaderKey{}, who)
	var option SessionMessageOptions
	if len(options) > 0 {
		option = options[0]
	}
	if (in.PostOnly || in.Answers != nil) && (option.Agent != "" || option.Fresh || option.Label != "") {
		return SessionView{}, fmt.Errorf("%w: child routing is only for a new agent request", threads.ErrInvalid)
	}
	th, err := p.SessionRecord(ctx, who, id)
	if err != nil {
		return SessionView{}, err
	}
	// A chat surface's message is read in its own grammar, as the person it
	// is posted for, so its markup is never taken for a mention here.
	text, reader, err := p.sessionReading(who, in)
	if err != nil {
		return SessionView{}, err
	}
	if _, _, err = p.sessionAgent(ctx, reader, th.Project, th.Session.Agent, text); err != nil {
		return SessionView{}, err
	}
	p.retitleAfterGreeting(who.Tenant, th, text)
	// A message that mentions another agent is answered in this session, by
	// that agent or, for several, by the entry agent with what each said
	// (carryOut). Only a request that asks for a child session by option
	// (an agent with fresh or a label) opens one, for the agent it names.
	if !in.PostOnly && in.Answers == nil && (option.Fresh || option.Label != "" || (option.Agent != "" && option.Agent != th.Session.Agent)) {
		mentions, parseErr := threads.Parse(text, tagLookup{ctx: ctx, p: p, tenant: who.Tenant, sees: p.Seer(reader).Agent, tree: p.Projects.Tree(who.Tenant), project: th.Project, person: reader.Subject, console: true})
		if parseErr != nil {
			return SessionView{}, parseErr
		}
		target := ""
		for _, mention := range mentions {
			if mention.Kind != threads.Agent || (option.Agent != "" && mention.Name != option.Agent) {
				continue
			}
			target = mention.Name
			break
		}
		if target == "" {
			return SessionView{}, fmt.Errorf("%w: child routing must name an @agent in the message", threads.ErrInvalid)
		}
		if in.Surface != nil || in.ActAs != nil {
			if _, err := p.postSurfaceChild(ctx, who, th, in, text, target, option); err != nil {
				return SessionView{}, err
			}
			return p.ReadSession(ctx, who, id, "", "", false)
		}
		posted := in
		posted.PostOnly = true
		if _, err := p.PostMessage(ctx, who, id, posted); err != nil {
			return SessionView{}, err
		}
		if _, err := p.StartSession(ctx, who, SessionInput{Text: in.Text, Uploads: in.Uploads, Agent: target, Parent: id, Fresh: option.Fresh, Label: option.Label}); err != nil {
			return SessionView{}, err
		}
		return p.ReadSession(ctx, who, id, "", "", false)
	}
	if _, err = p.PostMessage(ctx, who, id, in); err != nil {
		return SessionView{}, err
	}
	return p.ReadSession(ctx, who, id, "", "", false)
}
func sessionStatus(rr []runs.Run) string {
	status := "done"
	var latest time.Time
	for _, r := range rr {
		if !r.Status.Terminal() {
			if r.Status == runs.Parked {
				return "waiting"
			}
			return "working"
		}
		if r.UpdatedAt.After(latest) {
			latest = r.UpdatedAt
			switch r.Status {
			case runs.Failed:
				status = "failed"
			case runs.Stopped:
				status = "stopped"
			default:
				status = "done"
			}
		}
	}
	return status
}

// waitingOn is what the latest run parked on a person waits for: the tool
// an approval asks about, or the first line of a question, and its kind.
// Only a question a person answers counts, as askOf and questionParts decide
// it: a parent waiting on its delegation to a child agent waits on nobody. A
// run's questions are read as Waits orders them. Whom it waits on is not
// read from the run: see openQuestions.
func waitingOn(rr []runs.Run) (what, kind string) {
	var at time.Time
	var q *event.Question
	for i := range rr {
		r := &rr[i]
		if r.Status != runs.Parked || (q != nil && r.UpdatedAt.Before(at)) {
			continue
		}
		for _, w := range r.Waits() {
			if _, _, ok := questionParts(partKind(w)); ok && w.ID != "" {
				q, at = &w, r.UpdatedAt
				break
			}
		}
	}
	if q == nil {
		return "", ""
	}
	return attentionOf(questionPartOf(*q))
}

// questionPartOf is the parts of a question that say what it is about.
func questionPartOf(q event.Question) inbox.QuestionPart {
	part := inbox.QuestionPart{ID: q.ID, Kind: partKind(q), Prompt: q.Prompt}
	if q.Tool != nil {
		part.Tool = &inbox.QuestionTool{Name: q.Tool.Name}
	}
	return part
}

// attentionOf is what a question waits for, in a few words, and its kind:
// the tool an approval asks about, else the first line of what it asks.
func attentionOf(q inbox.QuestionPart) (what, kind string) {
	part, _, _ := questionParts(q.Kind)
	if part == inbox.PartApproval {
		if q.Tool != nil && q.Tool.Name != "" {
			return firstWords(q.Tool.Name, MaxAttention), WaitingApproval
		}
		return firstWords(firstLineOf(q.Prompt), MaxAttention), WaitingApproval
	}
	return firstWords(firstLineOf(q.Prompt), MaxAttention), WaitingQuestion
}

// openQuestions is the open questions the runs of some sessions wait on
// that the reader may answer, newest first, by the session each is about.
// A question is the reader's as answering decides it from what it was raised
// with and what they hold: mayAnswer, the rule checkAnswer holds every
// answer to and the inbox offers a question's actions by, so a listing never
// says a run waits on someone answering would refuse. A budget hold is
// billing:write's, though it is addressed to everyone.
//
// It reports the run, not the reader's notifications: a reader who hid,
// snoozed or deleted their copy of the message is still whom the run waits
// on, so no reader's marks are read. A question a called agent's run raised
// is filed under the session that called it, so it counts there. Only
// sessions of the page, which the reader was already found to read, are
// asked about.
//
// The cost is one statement, whatever the inbox holds and however many
// sessions are asked about (inbox.Store.Asking: the tenant's newest
// inbox.MaxAsking live messages, in one list, with no lookup for any; the
// inbox keeps no index by session, so one session costs the same). known is
// false when the tenant holds more live messages than that: a session with
// no question here may still wait on the reader, and its row must say it
// waits without saying on whom (ThreadSummary.WaitingUnknown). A reader who
// may not write runs is asked nothing and nothing is read.
func (p *Platform) openQuestions(who auth.Principal, sessions []string) (asks map[string][]inbox.QuestionPart, known bool) {
	if !who.Can(auth.RunsWrite) || len(sessions) == 0 {
		return nil, true
	}
	msgs, complete, err := p.Inbox.Asking(who.Tenant, sessions)
	if err != nil {
		p.log.Warn("platform: the questions waiting on a reader could not be read; sessions are listed without saying whom they wait on", "tenant", who.Tenant, "error", err)
		return nil, false
	}
	reader := readerOf(who)
	asks = map[string][]inbox.QuestionPart{}
	for _, m := range msgs {
		for _, part := range m.Parts {
			if part.Question == nil || part.State != inbox.PartOpen {
				continue
			}
			if _, err := p.mayAnswer(who, reader, m, *part.Question); err == nil {
				asks[m.Source.Thread] = append(asks[m.Source.Thread], *part.Question)
			}
		}
	}
	return asks, complete
}

// latestFailure is why the latest turn failed, or "" when it did not: a turn
// still going or answered since leaves an earlier failure behind. Its error
// is read as shownRun shows it in th, so a run moved out of a private project
// does not name it.
func latestFailure(rr []runs.Run, th threads.Thread) string {
	var latest *runs.Run
	for i := range rr {
		r := &rr[i]
		if latest == nil || r.UpdatedAt.After(latest.UpdatedAt) {
			latest = r
		}
	}
	if latest == nil || latest.Status != runs.Failed {
		return ""
	}
	shown := shownRun(*latest, th, nil)
	return cmp.Or(firstWords(firstLineOf(cmp.Or(shown.Error, shown.Reason)), MaxAttention), "No reason was recorded.")
}

// firstLineOf is the first line of text that holds anything, trimmed.
func firstLineOf(text string) string {
	for line := range strings.Lines(text) {
		if t := strings.TrimSpace(line); t != "" {
			return t
		}
	}
	return ""
}

// sessionRow is a session placed in a listing before it is summarized: its
// record, what the run index keeps of it, when it last changed and how far
// the reader's open alerts raise it.
type sessionRow struct {
	head threads.Summary
	kept runs.ThreadSummary
	at   time.Time
	rank int
}

// SessionQuery selects the sessions a listing holds. Project "" is any
// project and "~" the workspace itself. Roots leaves out a session another
// session opened, and Mine keeps what the reader started or takes part in.
type SessionQuery struct {
	Parent, Agent, Project, Status string
	Roots, Mine                    bool
}

// sessionSummary is one session at a glance, read on its own.
func (p *Platform) sessionSummary(who auth.Principal, th threads.Thread) SessionSummary {
	row := sessionRow{head: threads.Summary{Thread: th}, at: th.UpdatedAt}
	if head, err := p.Threads.Summary(who.Tenant, th.ID); err == nil {
		row.head.Messages, row.head.First = head.Messages, head.First
	}
	if kept, err := p.Runs.ConversationSummary(who.Tenant, th.ID); err == nil && kept.ID != "" {
		row.kept = kept
		if kept.UpdatedAt.After(row.at) {
			row.at = kept.UpdatedAt
		}
	}
	out, err := p.summarizeSessions(who, []sessionRow{row}, false)
	if err != nil {
		p.log.Warn("platform: a session's runs could not be read; it is shown without them", "session", th.ID, "error", err)
		return p.shownSessions(who.Tenant, []SessionSummary{p.summarizeSession(who, row, nil, threads.Message{}, false, nil)})[0]
	}
	return out[0]
}

// shownSessions is the last step of every path that returns session
// summaries: the people in each are named as the directory names them
// (ShowSummaries), in one reading of the directory for all of them.
func (p *Platform) shownSessions(tenant string, out []SessionSummary) []SessionSummary {
	conversations := make([]ThreadSummary, len(out))
	for i := range out {
		conversations[i] = out[i].Conversation
	}
	for i, shown := range p.ShowSummaries(tenant, conversations) {
		out[i].Conversation = shown
	}
	return out
}

// summarizeSessions is each row at a glance, in the rows' order. The turns
// and the latest message of every row are read together, so a page costs a
// few statements and not several a session. placed is for a listing: each
// summary then shows the time its row was ordered by, which a cursor names,
// whatever a turn that changed since says.
func (p *Platform) summarizeSessions(who auth.Principal, rows []sessionRow, placed bool) ([]SessionSummary, error) {
	ids := make([]string, len(rows))
	heads := make([]threads.Summary, len(rows))
	for i, row := range rows {
		ids[i], heads[i] = row.head.ID, row.head
	}
	turns, err := p.Runs.Conversations(who.Tenant, ids)
	if err != nil {
		return nil, err
	}
	last, err := p.Threads.LastMessages(who.Tenant, heads)
	if err != nil {
		return nil, err
	}
	// Whom the rows that wait on a person wait on is read once for all of
	// them (openQuestions), and not at all when none waits.
	var waiting []string
	for _, row := range rows {
		if row.kept.Waiting > 0 || slices.ContainsFunc(turns[row.head.ID], func(r runs.Run) bool { return r.Status == runs.Parked }) {
			waiting = append(waiting, row.head.ID)
		}
	}
	asks, known := p.openQuestions(who, waiting)
	out := make([]SessionSummary, len(rows))
	for i, row := range rows {
		message, said := last[row.head.ID]
		out[i] = p.summarizeSession(who, row, turns[row.head.ID], message, said, asks[row.head.ID])
		if c := &out[i].Conversation; !known && !c.WaitingOnYou && (c.Waiting > 0 || c.WaitingFor != "") {
			// Not found among what was read of the inbox, which is not all
			// of it: it waits, and on whom is not known.
			c.WaitingUnknown = true
		}
		if placed {
			out[i].UpdatedAt, out[i].Conversation.UpdatedAt = row.at, row.at
		}
	}
	return p.shownSessions(who.Tenant, out), nil
}

// summarizeSession is one row at a glance, from its turns and its latest
// message, said when it has one. The people in it are not yet named as the
// directory names them: shownSessions does that, and nothing is returned
// without it.
func (p *Platform) summarizeSession(who auth.Principal, row sessionRow, rr []runs.Run, message threads.Message, said bool, asks []inbox.QuestionPart) SessionSummary {
	th := row.head.Thread
	at := row.at
	for _, r := range rr {
		if r.UpdatedAt.After(at) {
			at = r.UpdatedAt
		}
	}
	summary := row.kept
	if summary.ID == "" {
		summary = runs.ThreadSummary{ID: th.ID, Origin: runs.OriginAPI, Agents: []string{}, People: []string{}, CreatedAt: th.CreatedAt, UpdatedAt: th.UpdatedAt}
	}
	summary.Project, summary.Channel, summary.Title, summary.Named = th.Project, th.Channel, th.Title, th.Title != ""
	summary.CreatedAt = th.CreatedAt
	summary.UpdatedAt = at
	for _, person := range th.Participants {
		if person.Kind == threads.Person && !slices.Contains(summary.People, person.Name) {
			summary.People = append(summary.People, person.Name)
		}
	}
	conversation := ThreadSummary{ThreadSummary: summary, Visibility: th.Visibility}
	conversation.Messages = row.head.Messages
	conversation.Source = threadSource(th)
	conversation.Joined = joinedSession(th, who.Subject)
	conversation.WaitingFor, conversation.WaitingKind = waitingOn(rr)
	if len(asks) > 0 {
		// What waits on the reader is said over what else waits.
		conversation.WaitingFor, conversation.WaitingKind = attentionOf(asks[0])
		conversation.WaitingOnYou = true
	}
	conversation.Failure = latestFailure(rr, th)
	if said {
		lastAgent := time.Time{}
		for _, run := range rr {
			if run.Status == runs.Completed && run.Answer != "" && run.UpdatedAt.After(lastAgent) {
				lastAgent = run.UpdatedAt
			}
		}
		if !message.At.Before(lastAgent) {
			conversation.Last = firstWords(message.Text, runs.MaxLast)
			conversation.LastBy = message.By
			conversation.LastAgent = false
		}
	}
	return SessionSummary{Conversation: conversation, ID: th.ID, Agent: th.Session.Agent, Parent: th.Session.Parent, Label: th.Session.Label, Project: th.Project, Title: th.Title, Status: sessionStatus(rr), UpdatedAt: at, rank: row.rank}
}

// sessionRows is the sessions a query selects, ordered as a listing shows
// them: the reader's open alerts first, by how urgent, then the latest
// changed. Whether the reader may read each is not yet decided: the snapshot
// returned decides it (seenSession), and holds back the record of a read it
// grants by exception until the caller discloses the rows it returns
// (disclosedSessions), so a reader who reads by exception is recorded for
// the sessions a response shows them and for no session that was only
// examined (Seer.Holding states the rule). Nothing is read a
// session: the records come in one bounded read (threads.MaxThreads), what
// the run index keeps of them in one statement for every state.MaxList. A
// row's turns are not read here, so its place is by when its record or any
// run in it last changed.
func (p *Platform) sessionRows(who auth.Principal, q SessionQuery) ([]sessionRow, *Seer, error) {
	if q.Parent != "" && !p.seesParent(who, q.Parent) {
		return nil, nil, threads.ErrNotFound
	}
	inProject := q.Project != ""
	project := projects.NameOf(q.Project)
	heads, _, err := p.Threads.Summaries(who.Tenant, func(th threads.Thread) bool {
		if th.Session == nil || th.Archive != nil || (q.Parent != "" && th.Session.Parent != q.Parent) || (q.Agent != "" && th.Session.Agent != q.Agent) || (inProject && th.Project != project) {
			return false
		}
		if q.Roots && strings.HasPrefix(th.Session.Parent, "s_") {
			return false
		}
		return !q.Mine || sessionOf(th, who.Subject)
	})
	if err != nil {
		return nil, nil, err
	}
	seer := p.Seer(who).Snapshot().Holding()
	rows := make([]sessionRow, len(heads))
	ids := make([]string, len(heads))
	for i, h := range heads {
		seer.Hold(h.Thread)
		rows[i], ids[i] = sessionRow{head: h, at: h.UpdatedAt}, h.ID
	}
	kept, err := p.Runs.ConversationSummaries(who.Tenant, ids)
	if err != nil {
		return nil, nil, err
	}
	ranks, err := p.sessionRanks(seer)
	if err != nil {
		return nil, nil, err
	}
	for i := range rows {
		id := rows[i].head.ID
		if k, ok := kept[id]; ok {
			rows[i].kept = k
			if k.UpdatedAt.After(rows[i].at) {
				rows[i].at = k.UpdatedAt
			}
		}
		rows[i].rank = ranks[id]
	}
	slices.SortFunc(rows, func(a, b sessionRow) int {
		if a.rank != b.rank {
			return b.rank - a.rank
		}
		if n := b.at.Compare(a.at); n != 0 {
			return n
		}
		return strings.Compare(a.head.ID, b.head.ID)
	})
	return rows, seer, nil
}

// seenSession reports whether the seer's reader may read a row's session,
// as that row's decision: what it is granted by exception is recorded when
// the row is disclosed.
func seenSession(seer *Seer, id string) bool {
	return seer.deciding(id, func() bool { return seer.Session(id) })
}

// seenSessions is the rows the seer's reader may read, in order.
func seenSessions(seer *Seer, rows []sessionRow) []sessionRow {
	return slices.DeleteFunc(rows, func(r sessionRow) bool { return !seenSession(seer, r.head.ID) })
}

// errReadUnrecorded refuses a listing whose reader was to be shown a session
// by exception that could not be recorded. It answers as a session that is
// not there does, and says nothing of which.
var errReadUnrecorded = fmt.Errorf("%w: sessions", threads.ErrNotFound)

// disclosedSessions records the reads by exception the summaries a response
// is about to return rest on, together, and returns the summaries. When the
// records cannot be written none is, and the whole answer is refused: no
// summary is returned, and the caller returns no cursor either, since a
// cursor names a row's identity, time and rank.
func disclosedSessions(seer *Seer, out []SessionSummary) ([]SessionSummary, error) {
	ids := make([]string, len(out))
	for i, v := range out {
		ids[i] = v.ID
	}
	if !seer.Disclose(ids...) {
		return nil, errReadUnrecorded
	}
	return out, nil
}

// sessionOf reports whether a person started a session or takes part in it.
func sessionOf(th threads.Thread, subject string) bool {
	if th.CreatedBy == subject {
		return true
	}
	for _, person := range th.Participants {
		if person.Kind == threads.Person && person.Name == subject {
			return true
		}
	}
	return false
}

// joinedSession reports that subject takes part in a session someone else
// started: the sidebar's Participated.
func joinedSession(th threads.Thread, subject string) bool {
	return subject != "" && th.CreatedBy != subject && sessionOf(th, subject)
}

// ListSessions is every session a query selects that the reader may read, at
// a glance, in a listing's order.
func (p *Platform) ListSessions(ctx context.Context, who auth.Principal, parent, agent, project, status string) ([]SessionSummary, error) {
	rows, seer, err := p.sessionRows(who, SessionQuery{Parent: parent, Agent: agent, Project: project})
	if err != nil {
		return nil, err
	}
	all, err := p.summarizeSessions(who, seenSessions(seer, rows), true)
	if err != nil {
		return nil, err
	}
	out := []SessionSummary{}
	for _, v := range all {
		if status == "" || status == v.Status {
			out = append(out, v)
		}
	}
	return disclosedSessions(seer, out)
}

// SessionsPage is one page of the sessions a query selects that the reader
// may read: at most limit of them, after the place the previous page's
// cursor names, and the cursor of the page that follows ("" on the last).
// Only the page is summarized: the sessions before it are passed over by
// their place, and those after it are neither read nor, but for the one
// that says a page follows, decided. A status is known from a session's
// turns, so a query that names one reads the turns of every session it
// could hold. Either way only the page is disclosed: a reader who reads by
// exception is recorded for the sessions it returns (Seer.Holding), and
// when one of those records cannot be written the page is refused whole,
// with no rows and no cursor.
func (p *Platform) SessionsPage(ctx context.Context, who auth.Principal, q SessionQuery, limit int, after string) ([]SessionSummary, string, error) {
	rows, seer, err := p.sessionRows(who, q)
	if err != nil {
		return nil, "", err
	}
	if q.Status != "" {
		// A session in flight has a run that has not ended, and a failed one
		// a run that failed: what the index counts leaves the rest out
		// before their turns are read.
		rows = slices.DeleteFunc(rows, func(r sessionRow) bool {
			switch q.Status {
			case "working", "waiting":
				return r.kept.Active == 0
			case "failed":
				return r.kept.Failed == 0
			}
			return false
		})
		all, err := p.summarizeSessions(who, seenSessions(seer, rows), true)
		if err != nil {
			return nil, "", err
		}
		// The turns of every session the filter could hold were read to
		// know its status. That is the platform examining them: only the
		// page that is returned is disclosed, and recorded.
		page, next, err := PageSessions(slices.DeleteFunc(all, func(v SessionSummary) bool { return v.Status != q.Status }), limit, after)
		if err != nil {
			return nil, "", err
		}
		if page, err = disclosedSessions(seer, page); err != nil {
			return nil, "", err
		}
		return page, next, nil
	}
	start := 0
	if after != "" {
		rank, at, id, err := decodeSessionListCursor(after)
		if err != nil {
			return nil, "", err
		}
		start = len(rows)
		if i := slices.IndexFunc(rows, func(r sessionRow) bool { return afterSessionPlace(r.rank, r.at, r.head.ID, rank, at, id) }); i >= 0 {
			start = i
		}
	}
	picked := make([]sessionRow, 0, min(max(limit, 0), len(rows)-start))
	more := false
	for _, row := range rows[start:] {
		if !seenSession(seer, row.head.ID) {
			continue
		}
		if len(picked) >= limit {
			more = true
			break
		}
		picked = append(picked, row)
	}
	out, err := p.summarizeSessions(who, picked, true)
	if err != nil {
		return nil, "", err
	}
	next := ""
	if more && len(picked) > 0 {
		last := picked[len(picked)-1]
		next = sessionListCursor(last.rank, last.at, last.head.ID)
	}
	if out, err = disclosedSessions(seer, out); err != nil {
		return nil, "", err
	}
	return out, next, nil
}

// afterSessionPlace reports whether a session's place in a listing is
// strictly after the place a cursor names: lower in rank, or at its rank
// changed earlier, or at the same moment with a later id.
func afterSessionPlace(rank int, changed time.Time, id string, cursorRank int, cursorAt int64, cursorID string) bool {
	if rank != cursorRank {
		return rank < cursorRank
	}
	at := changed.UnixNano()
	return at < cursorAt || (at == cursorAt && id > cursorID)
}

// sessionListCursor names a session's place in a listing.
func sessionListCursor(rank int, changed time.Time, id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(rank) + ":" + strconv.FormatInt(changed.UnixNano(), 10) + ":" + id))
}

// PageSessions cuts one page from a listing ListSessions ordered: at most
// limit sessions, after the place the previous page's cursor names, and the
// cursor of the page that follows ("" on the last).
//
// A cursor is the place of the last session on a page: how far alerts raise
// it, when it last changed, and its id to break ties. The next page is every
// session strictly after that place, wherever the session that set it
// stands by then. So a session that changes while a reader pages, which
// moves it to the top, makes no later page repeat the sessions already
// read: it is absent from the pages still to come and present the next
// time the first page is read.
func PageSessions(rows []SessionSummary, limit int, after string) ([]SessionSummary, string, error) {
	start := 0
	if after != "" {
		rank, at, id, err := decodeSessionListCursor(after)
		if err != nil {
			return nil, "", err
		}
		start = len(rows)
		if i := slices.IndexFunc(rows, func(v SessionSummary) bool { return afterSessionPlace(v.rank, v.UpdatedAt, v.ID, rank, at, id) }); i >= 0 {
			start = i
		}
	}
	end := min(start+max(limit, 0), len(rows))
	next := ""
	if end < len(rows) && end > start {
		last := rows[end-1]
		next = sessionListCursor(last.rank, last.UpdatedAt, last.ID)
	}
	return rows[start:end], next, nil
}

func decodeSessionListCursor(c string) (rank int, at int64, id string, err error) {
	raw, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil || len(c) > 2048 {
		return 0, 0, "", threads.ErrCursor
	}
	part := strings.SplitN(string(raw), ":", 3)
	if len(part) != 3 || part[2] == "" {
		return 0, 0, "", threads.ErrCursor
	}
	if rank, err = strconv.Atoi(part[0]); err != nil {
		return 0, 0, "", threads.ErrCursor
	}
	if at, err = strconv.ParseInt(part[1], 10, 64); err != nil {
		return 0, 0, "", threads.ErrCursor
	}
	return rank, at, part[2], nil
}

func sessionCursor(at time.Time) string {
	return base64.RawURLEncoding.EncodeToString([]byte(at.UTC().Format(time.RFC3339Nano)))
}

type sessionPosition struct {
	At time.Time `json:"at"`
	ID string    `json:"id"`
}

func readSessionCursor(raw string) (sessionPosition, error) {
	if raw == "" {
		return sessionPosition{}, nil
	}
	if len(raw) > 2048 {
		return sessionPosition{}, threads.ErrCursor
	}
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return sessionPosition{}, threads.ErrCursor
	}
	at, err := time.Parse(time.RFC3339Nano, string(b))
	if err == nil {
		return sessionPosition{At: at}, nil
	}
	var pos sessionPosition
	if err = json.Unmarshal(b, &pos); err != nil || pos.At.IsZero() || pos.ID == "" || len(pos.ID) > 200 {
		return sessionPosition{}, threads.ErrCursor
	}
	return pos, nil
}
func (p *Platform) ReadSession(ctx context.Context, who auth.Principal, id, since, before string, summary bool, around ...string) (SessionView, error) {
	th, err := p.SessionRecord(ctx, who, id)
	if err != nil {
		return SessionView{}, err
	}
	after, err := readSessionCursor(since)
	if err != nil {
		return SessionView{}, err
	}
	anchor := ""
	if len(around) > 0 {
		anchor = around[0]
	}
	turns, next, err := p.sessionTurns(who.Tenant, th, since, before, anchor)
	if err != nil {
		return SessionView{}, err
	}
	seer := p.Seer(who)
	turns = seer.Turns(th, turns)
	_, turns = p.ShowTurns(who.Tenant, seer.Thread(th), turns)
	turns = p.AsksIn(ctx, who, turns)
	view := SessionView{SessionSummary: p.sessionSummary(who, th), Messages: []SessionMessage{}, Children: []SessionSummary{}, Links: []session.Link{}, People: []threads.Participant{}, Next: next}
	shown := p.ShowThread(who.Tenant, seer.Thread(th))
	view.Thread = shown
	if store, ok := p.Sessions.(session.UsageStore); ok {
		usage, e := store.Usage(ctx, who.Tenant, id)
		if e != nil {
			return view, e
		}
		view.Usage = &usage
	}
	view.Voice = p.SessionVoiceUsage(who.Tenant, id)
	for _, pt := range shown.Participants {
		if pt.Kind == threads.Person {
			view.People = append(view.People, pt)
		}
	}
	last := th.UpdatedAt
	type changedMessage struct {
		message SessionMessage
		at      time.Time
	}
	var changes []changedMessage
	for _, t := range turns {
		m, updated, _ := sessionMessageOf(id, t)
		if t.Run != nil {
			r := *t.Run
			changed, err := p.Feedback.ChangedAt(who.Tenant, r.ID, who.Subject)
			if err != nil {
				return view, err
			}
			if changed.After(updated) {
				updated = changed
			}
			if f, err := p.Feedback.Own(who.Tenant, r.ID, who.Subject); err == nil {
				m.Feedback = &SessionFeedback{Rating: f.Rating, Reason: f.Reason, Comment: f.Comment}
			}
			if ratings, err := p.Feedback.OwnAll(who.Tenant, r.ID, who.Subject); err == nil {
				for _, f := range ratings {
					if f.FindingID != "" {
						m.FindingFeedback = append(m.FindingFeedback, SessionFeedback{FindingID: f.FindingID, Rating: f.Rating, Reason: f.Reason, Comment: f.Comment})
					}
				}
			}
		}
		if updated.After(last) {
			last = updated
		}
		if m.ID != "" && since == "" {
			view.Messages = append(view.Messages, m)
		} else if m.ID != "" && (updated.After(after.At) || (updated.Equal(after.At) && (after.ID == "" || m.ID > after.ID))) {
			changes = append(changes, changedMessage{message: m, at: updated})
		}
	}
	view.Cursor = sessionCursor(last)
	if since != "" {
		// A delta page advances only through returned changes. Timestamp ties
		// use message identity, so bursts larger than one page cannot be lost.
		slices.SortFunc(changes, func(a, b changedMessage) int {
			if n := a.at.Compare(b.at); n != 0 {
				return n
			}
			return strings.Compare(a.message.ID, b.message.ID)
		})
		view.More = len(changes) > MaxTurnPage
		changes = changes[:min(len(changes), MaxTurnPage)]
		view.Cursor = since
		for _, change := range changes {
			view.Messages = append(view.Messages, change.message)
		}
		if len(changes) > 0 {
			last := changes[len(changes)-1]
			b, _ := json.Marshal(sessionPosition{At: last.at, ID: last.message.ID})
			view.Cursor = base64.RawURLEncoding.EncodeToString(b)
		}
	}
	view.Children, err = p.ListSessions(ctx, who, id, "", "", "")
	if err != nil {
		return view, err
	}
	if len(view.Children) > session.MaxPage {
		view.Children = view.Children[:session.MaxPage]
	}
	links, err := p.Sessions.Links(ctx, who.Tenant, id, session.MaxPage)
	if err != nil {
		return view, err
	}
	for _, l := range links {
		if p.SeesSession(who, l.Target) {
			view.Links = append(view.Links, l)
		}
	}
	if summary {
		var lines []string
		for _, m := range view.Messages {
			for _, part := range m.Parts {
				if part.Text != "" {
					lines = append(lines, m.Author.Name+": "+firstWords(part.Text, 400))
				}
			}
		}
		view.Summary = strings.Join(lines, "\n")
		if len(view.Summary) > 8000 {
			view.Summary = firstWords(view.Summary, 8000)
		}
	}
	return view, nil
}

// sessionMessageOf is a turn of session as a session message, and when it
// last changed: an agent's run, or a person's message. It is false for any
// other turn.
func sessionMessageOf(session string, t ThreadTurn) (SessionMessage, time.Time, bool) {
	if t.Run != nil {
		r := *t.Run
		return SessionMessage{ID: r.ID, Session: session, Author: SessionAuthor{Kind: "agent", Name: r.Agent, Version: fmt.Sprintf("%d.0.0", r.Rev)}, Status: sessionStatus([]runs.Run{r}), Parts: []SessionPart{{Type: "text", Text: r.Answer}}, At: r.CreatedAt, Run: &r, Ask: t.Ask}, r.UpdatedAt, true
	}
	if t.Message != nil {
		return SessionMessage{ID: t.Message.ID, Session: session, Author: SessionAuthor{Kind: "person", Name: t.Message.Name}, Status: "done", Parts: []SessionPart{{Type: "text", Text: t.Message.Text}}, At: t.Message.At, Message: t.Message}, t.Message.At, true
	}
	return SessionMessage{}, time.Time{}, false
}

func (p *Platform) LinkSession(ctx context.Context, who auth.Principal, id, target, kind string) error {
	th, err := p.SessionRecord(ctx, who, id)
	if err != nil {
		return err
	}
	if err = p.MayPost(who, Place{Project: th.Project, Thread: id}); err != nil {
		return err
	}
	if !p.SeesSession(who, target) {
		return threads.ErrNotFound
	}
	if kind == "" {
		kind = "context"
	}
	return p.Sessions.Link(ctx, who.Tenant, id, session.Link{Target: target, Kind: kind})
}
func (p *Platform) StopSession(ctx context.Context, who auth.Principal, id string, children bool) error {
	if !p.SeesSession(who, id) {
		return threads.ErrNotFound
	}
	rr, err := p.Runs.Conversation(who.Tenant, id)
	if err != nil && !errors.Is(err, runs.ErrNotFound) {
		return err
	}
	for _, r := range rr {
		if !r.Status.Terminal() {
			if _, err = p.StopRun(ctx, who, r.ID); err != nil {
				return err
			}
		}
	}
	if children {
		cc, err := p.ListSessions(ctx, who, id, "", "", "")
		if err != nil {
			return err
		}
		for _, c := range cc {
			if err = p.StopSession(ctx, who, c.ID, true); err != nil {
				return err
			}
		}
	}
	return nil
}

func (p *Platform) sharedState() state.Store {
	if p.cfg.Backends.Shared != nil {
		return p.cfg.Backends.Shared
	}
	return p.cfg.Backends.State
}

// EnsureAlertSession gives a system problem a durable place where a person can
// reply. It does not call a model or grant anyone access to an existing session.
func (p *Platform) EnsureAlertSession(ctx context.Context, tenant, agent, project, owner, title string) (string, error) {
	if agent == "" || agent == Open {
		return "", fmt.Errorf("%w: an alert session needs one agent", threads.ErrInvalid)
	}
	visibility := threads.Private
	var ch projects.Channel
	var err error
	if owner == "" {
		visibility = threads.Shared
		project = projects.NameOf(project)
		if !p.HasProject(tenant, project) {
			return "", projects.ErrNotFound
		}
		ch, err = p.Projects.Channel(tenant, project, projects.General)
		if err != nil {
			return "", err
		}
		if ch.Archived {
			return "", projects.ErrInvalid
		}
		if err = p.checkDefault(ctx, tenant, project, agent, nil); err != nil {
			return "", err
		}
	} else {
		who := p.byPolicy(tenant, owner)
		if project == "" {
			pr, e := p.PersonalProject(who)
			if e != nil {
				return "", e
			}
			project = pr.Name
		}
		project, ch, err = p.placeFor(who, project, "")
		if err != nil {
			return "", err
		}
		if err = p.checkDefault(ctx, tenant, project, agent, p.Seer(who).Agent); err != nil {
			return "", err
		}
	}
	rec, err := p.Sessions.Open(ctx, tenant, session.Open{Parent: "system://alerts/" + owner + "/" + project, Agent: agent, Channel: wire.Channel(tenant)})
	if err != nil {
		return "", err
	}
	_, err = p.Threads.Create(tenant, threads.Thread{ID: rec.ID, Project: project, Channel: ch.Name, Title: firstWords(title, threads.MaxTitle), Visibility: visibility, CreatedBy: owner, Default: agent, Agents: []string{agent}, Session: &threads.SessionIdentity{Agent: agent, Parent: rec.Key.Parent, Label: rec.Key.Label}}, p.now())
	if errors.Is(err, threads.ErrExists) {
		err = nil
	}
	return rec.ID, err
}

// EnsureAlertSessionFromThread gives a legacy conversation a canonical reply
// place with the same audience. Only trusted attention projection calls it;
// an API caller cannot nominate another thread or manufacture participants.
func (p *Platform) EnsureAlertSessionFromThread(ctx context.Context, tenant, threadID, agent, title string) (string, error) {
	th, err := p.ThreadRecord(tenant, threadID)
	if err != nil {
		return "", err
	}
	if th.Session != nil {
		return th.ID, nil
	}
	if agent == "" || agent == Open {
		return "", threads.ErrInvalid
	}
	if err = p.checkDefault(ctx, tenant, th.Project, agent, nil); err != nil {
		return "", err
	}
	rec, err := p.Sessions.Open(ctx, tenant, session.Open{Parent: "legacy://" + th.ID, Agent: agent, Channel: wire.Channel(tenant)})
	if err != nil {
		return "", err
	}
	_, err = p.Threads.Create(tenant, threads.Thread{ID: rec.ID, Project: th.Project, Channel: th.Channel, Title: firstWords(title, threads.MaxTitle), Visibility: th.Visibility, CreatedBy: th.CreatedBy, Participants: slices.Clone(th.Participants), Default: agent, Agents: []string{agent}, Session: &threads.SessionIdentity{Agent: agent, Parent: rec.Key.Parent, Label: rec.Key.Label}}, p.now())
	if errors.Is(err, threads.ErrExists) {
		err = nil
	}
	return rec.ID, err
}

// seesParent reports whether a reader may list the sessions under a parent:
// a session they may read, or a schedule (ScheduleParent) filed where they
// may read. Each session listed is still one they may read.
func (p *Platform) seesParent(who auth.Principal, parent string) bool {
	if name, ok := strings.CutPrefix(parent, ScheduleParent("")); ok {
		sc, _, err := p.Schedules.Get(who.Tenant, name)
		return err == nil && p.Seer(who).Project(sc.Context[runs.MetaProject])
	}
	return p.SeesSession(who, parent)
}
