package platform

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/threads"
	"github.com/urmzd/orchagent/flow/event"
	"github.com/urmzd/orchagent/flow/session"
)

// SessionInput selects one agent's continuing conversation. Parent is a session
// ID; external places are accepted only by trusted connector ingress.
type SessionInput struct {
	Text        string `json:"text"`
	Agent       string `json:"agent,omitempty"`
	Parent      string `json:"parent,omitempty"`
	Label       string `json:"label,omitempty"`
	Fresh       bool   `json:"fresh,omitempty"`
	Project     string `json:"project,omitempty"`
	SourceAlert string `json:"source_alert,omitempty"`
	// Surface and ActAs are a chat surface's thread and the person a
	// message from it is posted for: only that client's machine sends them,
	// and the place's root session is found or started (surface_sessions.go).
	Surface     *SurfaceRef `json:"surface,omitempty"`
	ActAs       *ActAs      `json:"act_as,omitempty"`
	SurfaceKind string      `json:"surface_kind,omitempty"`
	// PostOnly posts a surface's message without starting anything, as a
	// channel the app follows quietly, or catching a thread up, does.
	PostOnly bool `json:"post_only,omitempty"`
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
}
type SessionView struct {
	SessionSummary
	Usage    *event.RunUsage       `json:"usage,omitempty"`
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
	for depth := 0; depth <= 64; depth++ {
		th, err := p.Threads.Get(who.Tenant, id)
		if err != nil || th.Session == nil || !p.SeesThread(who, id) || !p.Seer(who).Agent(th.Session.Agent) {
			return false
		}
		if !strings.HasPrefix(th.Session.Parent, "s_") {
			return true
		}
		id = th.Session.Parent
	}
	return false
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
		if explicit == "" && m.Kind == threads.Agent {
			explicit = m.Name
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
		// Nobody named and no agent set to answer: the entry agent owns the
		// session and calls whoever fits. Without one, routing picks, so a
		// message in the console always reaches someone.
		if entry := p.entryFor(ctx, who.Tenant, project, p.Seer(who).Agent); entry != "" {
			explicit = entry
		} else {
			var err error
			if explicit, picked, err = p.routeSession(ctx, who, project, text); err != nil {
				return "", "", err
			}
		}
	}
	if err := p.checkDefault(ctx, who.Tenant, project, explicit, p.Seer(who).Agent); err != nil {
		return "", "", err
	}
	return explicit, picked, nil
}
func (p *Platform) StartSession(ctx context.Context, who auth.Principal, in SessionInput) (SessionView, error) {
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
	th := threads.Thread{ID: rec.ID, Project: project, Channel: ch.Name, Title: firstWords(in.Text, threads.MaxTitle), Visibility: threads.Private, Default: agent, Agents: []string{agent}, CreatedBy: who.Subject, Session: &threads.SessionIdentity{Agent: agent, Parent: rec.Key.Parent, Label: rec.Key.Label, SourceAlert: in.SourceAlert}}
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
	if _, err := p.PostMessage(ctx, who, rec.ID, MessageInput{Text: in.Text}); err != nil {
		return SessionView{}, err
	}
	return p.ReadSession(ctx, who, rec.ID, "", "", false)
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
	// A different named agent gets its continuing child session. The parent
	// keeps the person's request and a visible child reference, never a second owner.
	if !in.PostOnly && in.Answers == nil {
		mentions, parseErr := threads.Parse(text, tagLookup{ctx: ctx, p: p, tenant: who.Tenant, sees: p.Seer(reader).Agent, tree: p.Projects.Tree(who.Tenant), project: th.Project, person: reader.Subject, console: true})
		if parseErr != nil {
			return SessionView{}, parseErr
		}
		target := ""
		for _, mention := range mentions {
			if mention.Kind != threads.Agent || (option.Agent != "" && mention.Name != option.Agent) {
				continue
			}
			if mention.Name == th.Session.Agent && !option.Fresh && option.Label == "" {
				continue
			}
			target = mention.Name
			break
		}
		if target == "" && (option.Fresh || option.Label != "" || (option.Agent != "" && option.Agent != th.Session.Agent)) {
			return SessionView{}, fmt.Errorf("%w: child routing must name an @agent in the message", threads.ErrInvalid)
		}
		if target != "" && (in.Surface != nil || in.ActAs != nil) {
			if _, err := p.postSurfaceChild(ctx, who, th, in, text, target, option); err != nil {
				return SessionView{}, err
			}
			return p.ReadSession(ctx, who, id, "", "", false)
		}
		if target != "" {
			posted := in
			posted.PostOnly = true
			if _, err := p.PostMessage(ctx, who, id, posted); err != nil {
				return SessionView{}, err
			}
			if _, err := p.StartSession(ctx, who, SessionInput{Text: in.Text, Agent: target, Parent: id, Fresh: option.Fresh, Label: option.Label}); err != nil {
				return SessionView{}, err
			}
			return p.ReadSession(ctx, who, id, "", "", false)
		}
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
func (p *Platform) sessionSummary(who auth.Principal, th threads.Thread) SessionSummary {
	rr, _ := p.Runs.Conversation(who.Tenant, th.ID)
	at := th.UpdatedAt
	for _, r := range rr {
		if r.UpdatedAt.After(at) {
			at = r.UpdatedAt
		}
	}
	summary, err := p.Runs.ConversationSummary(who.Tenant, th.ID)
	if err != nil || summary.ID == "" {
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
	if head, err := p.Threads.Summary(who.Tenant, th.ID); err == nil {
		conversation.Messages = head.Messages
		if head.Messages > 0 {
			if message, err := p.Threads.Message(who.Tenant, th.ID, fmt.Sprintf("m-%d", head.Messages)); err == nil {
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
		}
	}
	conversation = p.ShowSummaries(who.Tenant, []ThreadSummary{conversation})[0]
	return SessionSummary{Conversation: conversation, ID: th.ID, Agent: th.Session.Agent, Parent: th.Session.Parent, Label: th.Session.Label, Project: th.Project, Title: th.Title, Status: sessionStatus(rr), UpdatedAt: at}
}
func (p *Platform) ListSessions(ctx context.Context, who auth.Principal, parent, agent, project, status string) ([]SessionSummary, error) {
	if parent != "" && !p.seesParent(who, parent) {
		return nil, threads.ErrNotFound
	}
	// project "" is any project; "~" names the workspace itself.
	inProject := project != ""
	project = projects.NameOf(project)
	records, err := p.Threads.Records(who.Tenant, func(th threads.Thread) bool {
		return th.Session != nil && th.Archive == nil && (parent == "" || th.Session.Parent == parent) && (agent == "" || th.Session.Agent == agent) && (!inProject || th.Project == project)
	})
	if err != nil {
		return nil, err
	}
	out := []SessionSummary{}
	for _, th := range records {
		if !p.SeesSession(who, th.ID) {
			continue
		}
		v := p.sessionSummary(who, th)
		if status == "" || status == v.Status {
			out = append(out, v)
		}
	}
	slices.SortFunc(out, func(a, b SessionSummary) int {
		if n := b.UpdatedAt.Compare(a.UpdatedAt); n != 0 {
			return n
		}
		return strings.Compare(a.ID, b.ID)
	})
	if err := p.prioritizeSessions(who, out); err != nil {
		return nil, err
	}
	return out, nil
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
		if t.Message != nil {
			// What the entry agent picked follows the message it was for,
			// with each answer it led to inline.
			if pm, ok := p.pickMessage(who, th, *t.Message); ok {
				at := pm.At
				if at.After(last) {
					last = at
				}
				if since == "" {
					view.Messages = append(view.Messages, pm)
				} else if at.After(after.At) {
					changes = append(changes, changedMessage{message: pm, at: at})
				}
			}
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
