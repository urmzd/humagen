package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/threads"
	"github.com/urmzd/orchagent/flow/address"
	"github.com/urmzd/orchagent/flow/session"
)

// One way in for every surface: a batch of people's messages from one
// origin. The origin (ExternalRef) names the place the messages were asked
// in: a Slack thread, a GitHub pull request or issue, a scheduled run, a URL,
// or the console. A place has one root session, found by its address or
// started on its first message and owned by the entry agent, so work asked
// for in a place is picked up there later. Who answers is read from the
// message: an agent it mentions answers in its own child session under the
// root; otherwise the entry agent picks. Callers send people's messages;
// the platform writes the agents'.

// Origin kinds (ExternalRef.Type).
const (
	RefSlack    = "slack"
	RefGitHub   = "github"
	RefSchedule = "schedule"
	RefURL      = "url"
	RefWeb      = "web"
)

// Bounds on one request.
const (
	// MaxBatch is how many messages one request may carry.
	MaxBatch = 50
	// maxURL bounds a URL origin.
	maxURL = 2048
)

// ExternalRef is where a message came from: its origin, which the answer is
// delivered to and context is drawn from. It is a union on Type.
type ExternalRef struct {
	Type string `json:"type"`
	// Client is the Humagen client the origin is reached through (a Slack
	// app, a GitHub installation). It files the root where the client's
	// channel is bound and maps a Slack conversation's members to people.
	Client string `json:"client,omitempty"`
	// Slack: the team, channel and message ts, and the thread's ts for a
	// reply. Audience is the conversation's kind and members now.
	Team    string `json:"team,omitempty"`
	Channel string `json:"channel,omitempty"`
	// User is the Slack user who wrote the message, which runs it starts
	// carry as their actor's id. Only a surface built into the server, which
	// verified the user, sets it; the API ignores it.
	User     string           `json:"user,omitempty"`
	TS       string           `json:"ts,omitempty"`
	ThreadTS string           `json:"thread_ts,omitempty"`
	Audience *SurfaceAudience `json:"audience,omitempty"`
	// GitHub: owner/name, pull or issue (or comment, on either), its number
	// and the comment's id.
	Repo      string `json:"repo,omitempty"`
	Kind      string `json:"kind,omitempty"`
	Number    int    `json:"number,omitempty"`
	CommentID int64  `json:"comment_id,omitempty"`
	// Schedule: its name and which occurrence.
	Name       string `json:"name,omitempty"`
	Occurrence string `json:"occurrence,omitempty"`
	// URL: the page.
	URL string `json:"url,omitempty"`
}

// Place is the address of the root session messages from this origin land
// in, or "" for the console, whose every new conversation is a root of its
// own.
func (r ExternalRef) Place() (string, error) {
	bad := func(why string) (string, error) {
		return "", fmt.Errorf("%w: ref: %s", threads.ErrInvalid, why)
	}
	switch r.Type {
	case "", RefWeb:
		return "", nil
	case RefSlack:
		thread := r.ThreadTS
		if thread == "" {
			thread = r.TS
		}
		if !slackPart(r.Team) || !slackPart(r.Channel) || !slackTS(thread) {
			return bad("a slack ref names its team, channel and ts")
		}
		return surfacePlace(r.Team, r.Channel, thread), nil
	case RefGitHub:
		owner, name, ok := strings.Cut(r.Repo, "/")
		if !ok || !githubPart(owner) || !githubPart(name) || r.Number <= 0 {
			return bad("a github ref names its repo as owner/name and a number")
		}
		switch r.Kind {
		case "pull":
			return "github://" + owner + "/" + name + "/pull/" + strconv.Itoa(r.Number), nil
		case "issue", "comment":
			// A comment lands in its pull request's or issue's root, which
			// GitHub numbers alike; issues/N reads either.
			return "github://" + owner + "/" + name + "/issues/" + strconv.Itoa(r.Number), nil
		}
		return bad("a github ref's kind is pull, issue or comment")
	case RefSchedule:
		if r.Name == "" || len(r.Name) > 100 || strings.ContainsAny(r.Name, "/ ") || r.Occurrence == "" || len(r.Occurrence) > 100 {
			return bad("a schedule ref names the schedule and the occurrence")
		}
		return "schedule://" + r.Name + "/" + url.PathEscape(r.Occurrence), nil
	case RefURL:
		u, err := url.Parse(strings.TrimSpace(r.URL))
		if err != nil || len(r.URL) > maxURL || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			return bad("a url ref is an http or https URL")
		}
		u.Scheme, u.Host, u.Fragment = strings.ToLower(u.Scheme), strings.ToLower(u.Host), ""
		u.Path = strings.TrimSuffix(u.Path, "/")
		return u.String(), nil
	}
	return bad("type is slack, github, schedule, url or web")
}

// key is what makes one message from this origin post once: its Slack ts,
// its GitHub comment, its schedule occurrence.
func (r ExternalRef) key() string {
	switch r.Type {
	case RefSlack:
		return "slack:" + r.Channel + ":" + r.TS
	case RefGitHub:
		if r.CommentID > 0 {
			return "github:comment:" + strconv.FormatInt(r.CommentID, 10)
		}
	case RefSchedule:
		return "schedule:" + r.Name + ":" + r.Occurrence
	}
	return ""
}

func slackPart(s string) bool {
	return s != "" && len(s) <= 32 && strings.IndexFunc(s, func(r rune) bool { return (r < 'A' || r > 'Z') && (r < '0' || r > '9') }) < 0
}

func slackTS(s string) bool {
	whole, frac, ok := strings.Cut(s, ".")
	digits := func(x string) bool {
		return x != "" && strings.IndexFunc(x, func(r rune) bool { return r < '0' || r > '9' }) < 0
	}
	return ok && digits(whole) && digits(frac) && len(s) <= 32
}

func githubPart(s string) bool {
	return s != "" && len(s) <= 100 && strings.IndexFunc(s, func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '-' && r != '_' && r != '.'
	}) < 0
}

// ContentBlock is one block of a message's content: text, a mention of an
// agent or a person a surface already resolved, or a link to another place.
type ContentBlock struct {
	Type   string       `json:"type"`
	Text   string       `json:"text,omitempty"`
	Agent  string       `json:"agent,omitempty"`
	Person string       `json:"person,omitempty"`
	Ref    *ExternalRef `json:"ref,omitempty"`
}

// Content is a message's content: a string, or blocks.
type Content []ContentBlock

// UnmarshalJSON reads a string as one text block.
func (c *Content) UnmarshalJSON(b []byte) error {
	if t := bytes.TrimSpace(b); len(t) > 0 && t[0] == '"' {
		var s string
		if err := json.Unmarshal(t, &s); err != nil {
			return err
		}
		*c = Content{{Type: "text", Text: s}}
		return nil
	}
	var blocks []ContentBlock
	if err := json.Unmarshal(b, &blocks); err != nil {
		return err
	}
	*c = blocks
	return nil
}

// text is the content as the platform reads a message: its text, an agent
// or person a block mentions written as their @mention when the text does
// not already say it, and a link as its address.
func (c Content) text() (string, error) {
	var parts, mentions []string
	for _, b := range c {
		switch b.Type {
		case "text":
			parts = append(parts, b.Text)
		case "mention":
			name := strings.TrimPrefix(strings.TrimSpace(cmpOr(b.Agent, b.Person)), "@")
			if name == "" || (b.Agent != "" && b.Person != "") {
				return "", fmt.Errorf("%w: a mention names an agent or a person", threads.ErrInvalid)
			}
			mentions = append(mentions, "@"+name)
		case "link":
			if b.Ref == nil {
				return "", fmt.Errorf("%w: a link block carries its ref", threads.ErrInvalid)
			}
			at, err := b.Ref.Place()
			if err != nil {
				return "", err
			}
			parts = append(parts, at)
		case "image", "document", "audio", "answer":
			return "", fmt.Errorf("%w: %s blocks are not taken here yet", threads.ErrInvalid, b.Type)
		default:
			return "", fmt.Errorf("%w: a content block is text, mention, link, image, document, audio or answer", threads.ErrInvalid)
		}
	}
	text := strings.Join(parts, "\n")
	for _, m := range mentions {
		if !strings.Contains(text, m) {
			text = m + " " + text
		}
	}
	return strings.TrimSpace(text), nil
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// Message is one message in a request: who said it is the caller, never a
// field. Ref is its own origin when it differs from the batch's (a Slack
// reply's ts), which also makes it post once.
type Message struct {
	Role    string       `json:"role"`
	Content Content      `json:"content"`
	Ref     *ExternalRef `json:"ref,omitempty"`
}

// MessagesInput is a batch of people's messages from one origin. The last
// one is what is asked; those before it are posted as they were said and
// start nothing.
type MessagesInput struct {
	Messages []Message    `json:"messages"`
	Ref      *ExternalRef `json:"ref,omitempty"`
	Stream   bool         `json:"stream,omitempty"`
	// surface is set only by a surface built into the server, which verified
	// the person and the place (SendSurfaceMessages): its runs answer on the
	// surface, and its refs' User is believed. No request can set it.
	surface bool
}

// MessageView is the message a request created: its id
// (<session>:<message>), the root it is under, and its content. Created
// reports a root this request started.
type MessageView struct {
	ID      string        `json:"id"`
	Root    string        `json:"root"`
	Role    string        `json:"role"`
	Content Content       `json:"content"`
	Created bool          `json:"created"`
	Posted  SessionPosted `json:"posted"`
}

// messageID is a message's id across the platform.
func messageID(session, message string) string { return session + ":" + message }

// SplitMessageID reads a message id, or a session id alone, as the session
// and the message ("" for a session).
func SplitMessageID(id string) (session, message string) {
	session, message, _ = strings.Cut(id, ":")
	return session, message
}

func (in MessagesInput) check() error {
	if len(in.Messages) == 0 || len(in.Messages) > MaxBatch {
		return fmt.Errorf("%w: a request carries 1 to %d messages", threads.ErrInvalid, MaxBatch)
	}
	for _, m := range in.Messages {
		if m.Role != "user" {
			return fmt.Errorf("%w: callers send people's messages, role user; the platform writes the agents'", threads.ErrInvalid)
		}
	}
	return nil
}

// SendMessages posts a batch of who's messages from one origin: under the
// root session of the place it names (found, or started and owned by the
// entry agent), or for the console as a new root. Surfaces built into the
// server call it with the person they verified; the API with the caller.
func (p *Platform) SendMessages(ctx context.Context, who auth.Principal, in MessagesInput) (MessageView, error) {
	if err := in.check(); err != nil {
		return MessageView{}, err
	}
	if !in.surface {
		in.dropUnverified()
	}
	origin := ExternalRef{Type: RefWeb}
	if in.Ref != nil {
		origin = *in.Ref
	}
	place, err := origin.Place()
	if err != nil {
		return MessageView{}, err
	}
	if place == "" {
		return p.sendToNewConsoleRoot(ctx, who, in)
	}
	root, created, err := p.rootAt(ctx, who, place, origin, p.firstMentioned(ctx, who, in.Messages[len(in.Messages)-1]))
	if err != nil {
		return MessageView{}, err
	}
	if in.surface && origin.Type == RefSlack && origin.Client != "" {
		// Answers go where they were asked: runs started here answer on the
		// Slack thread, through the origin's client, for the user who wrote.
		thread := cmpOr(origin.ThreadTS, origin.TS)
		at := address.Address{Connector: "slack", Workspace: origin.Team, Path: []string{origin.Channel}}.WithParam("thread", thread)
		ctx = withDelivery(ctx, delivery{channel: at.String(), client: origin.Client, user: origin.User, slackChannel: origin.Channel})
	}
	out, err := p.sendUnder(ctx, who, root, origin, in.Messages)
	out.Created = created
	return out, err
}

// SendSurfaceMessages is SendMessages for a surface built into the server
// (the Slack and GitHub ingress), which verified who wrote the messages and
// where: runs they start answer on the surface, through the origin's client,
// and carry the surface user who wrote as their actor's id.
func (p *Platform) SendSurfaceMessages(ctx context.Context, who auth.Principal, in MessagesInput) (MessageView, error) {
	in.surface = true
	return p.SendMessages(ctx, who, in)
}

// dropUnverified forgets what only a surface built into the server may say:
// the surface user who wrote a message. Who speaks is the caller.
func (in *MessagesInput) dropUnverified() {
	if in.Ref != nil {
		r := *in.Ref
		r.User = ""
		in.Ref = &r
	}
	for i := range in.Messages {
		if in.Messages[i].Ref != nil {
			r := *in.Messages[i].Ref
			r.User = ""
			in.Messages[i].Ref = &r
		}
	}
}

// SendMessagesUnder posts a batch beneath a session (or one of its
// messages), as a follow-up in that conversation.
func (p *Platform) SendMessagesUnder(ctx context.Context, who auth.Principal, id string, in MessagesInput) (MessageView, error) {
	if err := in.check(); err != nil {
		return MessageView{}, err
	}
	in.dropUnverified()
	sessionID, _ := SplitMessageID(id)
	th, err := p.SessionRecord(ctx, who, sessionID)
	if err != nil {
		return MessageView{}, err
	}
	origin := ExternalRef{Type: RefWeb}
	if in.Ref != nil {
		origin = *in.Ref
		if _, err := origin.Place(); err != nil {
			return MessageView{}, err
		}
	}
	return p.sendUnder(ctx, who, th, origin, in.Messages)
}

// sendToNewConsoleRoot starts a console conversation with the request's one
// message, answered as a new console session is: the agent it mentions, else
// the entry agent.
func (p *Platform) sendToNewConsoleRoot(ctx context.Context, who auth.Principal, in MessagesInput) (MessageView, error) {
	if len(in.Messages) != 1 {
		return MessageView{}, fmt.Errorf("%w: a new console conversation starts with one message", threads.ErrInvalid)
	}
	text, err := in.Messages[0].Content.text()
	if err != nil {
		return MessageView{}, err
	}
	v, err := p.StartSession(ctx, who, SessionInput{Text: text})
	if err != nil {
		return MessageView{}, err
	}
	out := MessageView{Root: v.ID, Role: "user", Content: in.Messages[0].Content, Created: true}
	for _, m := range v.Messages {
		if m.Message != nil {
			out.ID = messageID(v.ID, m.Message.ID)
			out.Posted.Message = SessionPostedMessage{ID: out.ID, Text: m.Message.Text}
			break
		}
	}
	out.Posted.Runs, out.Posted.Notified, out.Posted.Refused = nonNil(out.Posted.Runs), []string{}, nonNil(out.Posted.Refused)
	for _, m := range v.Messages {
		if m.Run != nil {
			out.Posted.Runs = append(out.Posted.Runs, *m.Run)
		}
	}
	return out, nil
}

// rootAt is the root session of place, found, or started for who and owned
// by the entry agent (with no built-in agents, the client's default),
// filed where the origin's client files it: a Slack channel where it is
// bound, else the client's home; with no client, who's own project. Its
// audience follows the origin.
func (p *Platform) rootAt(ctx context.Context, who auth.Principal, place string, origin ExternalRef, mentioned string) (threads.Thread, bool, error) {
	var c *clients.Client
	if origin.Client != "" {
		got, err := p.Clients.Get(who.Tenant, origin.Client)
		if err != nil {
			return threads.Thread{}, false, fmt.Errorf("%w: ref: no client %q", threads.ErrInvalid, origin.Client)
		}
		if origin.Type == RefSlack && got.Identity.Team != origin.Team {
			return threads.Thread{}, false, fmt.Errorf("%w: ref: client %q is not team %s's", threads.ErrInvalid, got.ID, origin.Team)
		}
		c = &got
	}
	vis, people, aud, err := p.originAudience(who, c, origin)
	if err != nil {
		return threads.Thread{}, false, err
	}
	project, channel := "", ""
	switch {
	case c != nil && origin.Type == RefSlack:
		at, err := p.filedAt(who.Tenant, *c, origin.Channel)
		if err != nil {
			return threads.Thread{}, false, err
		}
		project, channel = at.project, at.channel
	case c != nil:
		h := c.HomeOf()
		project, channel = h.Project, h.Channel
	default:
		pr, err := p.PersonalProject(who)
		if err != nil {
			return threads.Thread{}, false, err
		}
		project = pr.Name
		if vis == threads.Shared {
			vis, people = threads.Private, []threads.Participant{{Kind: threads.Person, Name: humanSubject(who), Owner: true, At: p.now()}}
		}
	}
	project, ch, err := p.placeFor(who, project, channel)
	if err != nil {
		return threads.Thread{}, false, err
	}
	owner := p.entryFor(ctx, who.Tenant, project, p.Seer(who).Agent)
	if owner == "" && c != nil && origin.Type == RefSlack {
		// Without the entry agent, the Slack channel's binding answers first:
		// the bound channel's own default, then the client's.
		if bound, ok := p.Projects.BoundTo(who.Tenant, c.ID, origin.Channel); ok && bound.Default != "" && bound.Default != Open {
			owner = bound.Default
		}
	}
	if owner == "" && c != nil && c.DefaultAgent != Open {
		owner = c.DefaultAgent
	}
	if owner == "" {
		// With no entry agent and no client default, the agent the message
		// mentions owns the place's root.
		owner = mentioned
	}
	if owner == "" {
		return threads.Thread{}, false, fmt.Errorf("%w: nobody answers here: the server has no entry agent and the origin's client names no default agent", ErrNoDefault)
	}
	rec, err := p.Sessions.Open(ctx, who.Tenant, session.Open{Parent: place, Agent: owner, Channel: wire.Channel(who.Tenant)})
	if err != nil {
		return threads.Thread{}, false, err
	}
	th := threads.Thread{ID: rec.ID, Project: project, Channel: ch.Name, Visibility: vis, Participants: people, Title: place, Default: owner, Agents: []string{owner}, CreatedBy: humanSubject(who), Session: &threads.SessionIdentity{Agent: owner, Parent: rec.Key.Parent, Label: rec.Key.Label}}
	_, err = p.Threads.Create(who.Tenant, th, p.now())
	created := err == nil
	if err != nil && !errors.Is(err, threads.ErrExists) {
		return threads.Thread{}, false, err
	}
	if th, err = p.Threads.Get(who.Tenant, rec.ID); err != nil {
		return threads.Thread{}, false, err
	}
	if !created && vis == threads.Private && origin.Audience != nil && len(origin.Audience.Members) > 0 {
		if err := p.keepAudience(who.Tenant, th, people); err != nil {
			return threads.Thread{}, false, err
		}
	}
	if aud.Kind != "" {
		if err := p.audiences.Put(who.Tenant, th.ID, aud); err != nil {
			return threads.Thread{}, false, err
		}
	}
	if !created && !p.SeesSession(who, th.ID) {
		return threads.Thread{}, false, threads.ErrNotFound
	}
	return th, created, nil
}

// originAudience is who a root started from origin is for. A Slack origin's
// follows its conversation (surfaceAudience); a GitHub origin through a
// client is shared where the client files it; anything else is its
// person's alone.
func (p *Platform) originAudience(who auth.Principal, c *clients.Client, origin ExternalRef) (threads.Visibility, []threads.Participant, SessionAudience, error) {
	owner := threads.Participant{Kind: threads.Person, Name: humanSubject(who), Owner: true, At: p.now()}
	switch {
	case origin.Type == RefSlack && c != nil:
		ref := SurfaceRef{Client: c.ID, Channel: origin.Channel, TS: origin.ThreadTS, Audience: origin.Audience}
		return p.audienceOf(*c, ref, who)
	case origin.Type == RefGitHub && c != nil:
		return threads.Shared, nil, SessionAudience{Kind: "public", At: p.now()}, nil
	}
	return threads.Private, []threads.Participant{owner}, SessionAudience{Kind: "direct", Members: []string{owner.Name}, At: p.now()}, nil
}

// sendUnder posts messages under the root th: each but the last as said,
// starting nothing; the last to the agents it mentions, each answering in
// its child session, else to the root, where the entry agent picks.
func (p *Platform) sendUnder(ctx context.Context, who auth.Principal, th threads.Thread, origin ExternalRef, msgs []Message) (MessageView, error) {
	var out MessageView
	for i, m := range msgs {
		text, err := m.Content.text()
		if err != nil {
			return MessageView{}, err
		}
		ref := origin
		if m.Ref != nil {
			ref = *m.Ref
		}
		in := MessageInput{Text: text, PostOnly: i < len(msgs)-1, messageKey: ref.key()}
		v, err := p.surfacePost(ctx, who, th, in)
		if err != nil {
			return MessageView{}, err
		}
		out = MessageView{Root: th.ID, Role: "user", Content: m.Content, Posted: *v.Posted}
		out.ID = messageID(th.ID, v.Posted.Message.ID)
		out.Posted.Message.ID = out.ID
	}
	return out, nil
}

// mentionedAgents are the agents a message's text mentions, other than its
// session's owner, in the order written.
func mentionedAgents(ms []threads.Mention, owner string) []string {
	var out []string
	for _, m := range ms {
		if m.Kind == threads.Agent && m.Refused == "" && m.Name != owner && !slices.Contains(out, m.Name) {
			out = append(out, m.Name)
		}
	}
	return out
}

// firstMentioned is the first agent m mentions, as who reads it, or "".
func (p *Platform) firstMentioned(ctx context.Context, who auth.Principal, m Message) string {
	text, err := m.Content.text()
	if err != nil {
		return ""
	}
	mentions, err := threads.Parse(text, tagLookup{ctx: ctx, p: p, tenant: who.Tenant, sees: p.Seer(who).Agent, tree: p.Projects.Tree(who.Tenant), project: "", person: who.Subject, console: true})
	if err != nil {
		return ""
	}
	if agents := mentionedAgents(mentions, ""); len(agents) > 0 {
		return agents[0]
	}
	return ""
}

// delivery is where runs a surface's message starts answer: the surface's
// channel (a Slack thread's address), the client that reaches it, and the
// surface's user who wrote the message.
type delivery struct{ channel, client, user, slackChannel string }

type deliveryKey struct{}

func withDelivery(ctx context.Context, d delivery) context.Context {
	return context.WithValue(ctx, deliveryKey{}, d)
}

func deliveryOf(ctx context.Context) (delivery, bool) {
	d, ok := ctx.Value(deliveryKey{}).(delivery)
	return d, ok
}
