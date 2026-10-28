package wire

import (
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/urmzd/humagen/fiber/pkg/address"
	"github.com/urmzd/humagen/fiber/pkg/attachment"
	"github.com/urmzd/humagen/fiber/pkg/event"
)

// ChannelScheme is the scheme of the channel a run Humagen raises itself
// publishes on, when it has no surface to answer on.
const ChannelScheme = "humagen"

// Channel is the channel of a tenant's runs that answer on no surface.
func Channel(tenant string) string { return ChannelScheme + "://" + tenant }

// SourceOf is the source of an event Humagen raises through origin, such as
// "api" or "agent", in tenant. It is an opaque URI, not a surface address, so
// a run Humagen raises is never bound to a surface by its source.
func SourceOf(origin, tenant string) string { return origin + ":" + tenant }

// Raise is an event Humagen raises for one agent.
type Raise struct {
	ID     string
	Tenant string
	// Origin is the path it came through: "api", "agent", "assignment".
	Origin string
	// Agent is the one agent it is for.
	Agent string
	// Channel is where the run answers. Empty is Channel(Tenant).
	Channel     string
	Text        string
	Actor       event.Actor
	Context     map[string]string
	Attachments []attachment.Attachment
	At          time.Time
	Hops        int
}

// NewEvent is the message.posted.v1 event r describes. Every run Humagen
// raises itself, a call's child among them, is built here.
func NewEvent(r Raise) event.Envelope {
	req := event.Request{Text: r.Text, Actor: r.Actor, Attachments: r.Attachments}
	if r.Agent != "" {
		req.Mentions = []string{r.Agent}
	}
	if len(r.Context) > 0 {
		req.Context = maps.Clone(r.Context)
	}
	ch := r.Channel
	if ch == "" {
		ch = Channel(r.Tenant)
	}
	return event.Envelope{
		ID: r.ID, Source: SourceOf(r.Origin, r.Tenant), Type: event.TypeMessagePosted,
		Channel: ch, Time: r.At, Hops: r.Hops, Tenant: r.Tenant, Agent: r.Agent,
		Data: event.MessagePosted{Request: req},
	}
}

// Origin is the path an event came through: its source's scheme, such as
// "slack", "cron" or "api".
func Origin(e event.Envelope) string {
	if a, err := address.Parse(e.Source); err == nil {
		return a.Connector
	}
	scheme, _, _ := strings.Cut(e.Source, ":")
	return scheme
}

// SurfaceAddress is the address of the surface a run answers on: its
// channel, when that is one. A run Humagen raised on no surface has none.
func SurfaceAddress(e event.Envelope) address.Address {
	if strings.HasPrefix(e.Channel, ChannelScheme+":") {
		return address.Address{}
	}
	a, err := address.Parse(e.Channel)
	if err != nil {
		return address.Address{}
	}
	return a
}

// AgentOf is the agent an event is for: the one Humagen set on it, or else
// the first it addresses.
func AgentOf(e event.Envelope) string {
	if e.Agent != "" {
		return e.Agent
	}
	if e.Data == nil {
		return ""
	}
	if names := event.Addressees(e.Data); len(names) > 0 {
		return names[0]
	}
	return ""
}

// SetAgent makes agent the one an event is for: its request names only it,
// and so does its Agent field, which a run's own events carry too.
func SetAgent(e *event.Envelope, agent string) {
	e.Agent = agent
	update(e, func(r *event.Request) {
		r.Mentions = nil
		if agent != "" {
			r.Mentions = []string{agent}
		}
	})
}

// RequestOf is the request an event carries, zero for one that asks nothing.
func RequestOf(e event.Envelope) event.Request {
	if e.Data == nil {
		return event.Request{}
	}
	r, _ := event.RequestOf(e.Data)
	return r
}

// TextOf is the text a run was raised with.
func TextOf(e event.Envelope) string { return RequestOf(e).Text }

// ActorOf is who raised an event.
func ActorOf(e event.Envelope) event.Actor { return RequestOf(e).Actor }

// AttachmentsOf are the files that arrived with an event.
func AttachmentsOf(e event.Envelope) []attachment.Attachment { return RequestOf(e).Attachments }

// Meta is what an event says beside its text, as one map: the keys of the
// body's typed fields, its request's context, and its private keys. It is a
// copy: change it with SetMeta.
func Meta(e event.Envelope) map[string]string {
	out := map[string]string{}
	if e.Data != nil {
		if _, ctx, ok := event.Prompt(e.Data); ok {
			maps.Copy(out, ctx)
		} else if r, ok := event.RequestOf(e.Data); ok {
			maps.Copy(out, r.Context)
		}
	}
	maps.Copy(out, e.Private)
	return out
}

// update changes the request an event carries. An event that carries none is
// left as it is.
func update(e *event.Envelope, fn func(*event.Request)) {
	if e.Data == nil {
		return
	}
	r, ok := event.RequestOf(e.Data)
	if !ok {
		return
	}
	r.Context = maps.Clone(r.Context)
	fn(&r)
	if len(r.Context) == 0 {
		r.Context = nil
	}
	if b, ok := event.WithRequest(e.Data, r); ok {
		e.Data = b
	}
}

// SetText replaces the text a run is raised with.
func SetText(e *event.Envelope, text string) { update(e, func(r *event.Request) { r.Text = text }) }

// SetActor replaces who an event is from.
func SetActor(e *event.Envelope, a event.Actor) { update(e, func(r *event.Request) { r.Actor = a }) }

// SetMeta sets one key of an event's request context. A key starting with "_"
// is kept with the event and never shown to a model.
func SetMeta(e *event.Envelope, key, value string) {
	update(e, func(r *event.Request) {
		if r.Context == nil {
			r.Context = map[string]string{}
		}
		r.Context[key] = value
	})
}

// DeleteMeta removes one key of an event's request context.
func DeleteMeta(e *event.Envelope, key string) {
	update(e, func(r *event.Request) { delete(r.Context, key) })
	delete(e.Private, key)
}

// EditMeta changes an event's meta through fn, which is handed a copy (Meta):
// a key fn sets or changes is written with SetMeta, and one it removes with
// DeleteMeta.
func EditMeta(e *event.Envelope, fn func(map[string]string)) {
	before := Meta(*e)
	after := maps.Clone(before)
	fn(after)
	for k := range before {
		if _, ok := after[k]; !ok {
			DeleteMeta(e, k)
		}
	}
	for k, v := range after {
		if old, ok := before[k]; !ok || old != v {
			SetMeta(e, k, v)
		}
	}
}

// SetContext replaces an event's request context.
func SetContext(e *event.Envelope, ctx map[string]string) {
	update(e, func(r *event.Request) { r.Context = maps.Clone(ctx) })
}

// ContextOf is an event's request context: the keys Humagen and a trigger
// wrote, without the body's typed fields. It is a copy.
func ContextOf(e event.Envelope) map[string]string {
	return maps.Clone(RequestOf(e).Context)
}

// The meta keys a connector's typed fields read as.
const (
	MetaSlackEvent     = "slack_event"
	MetaSlackChannel   = "slack_channel"
	MetaSlackCommand   = "slack_command"
	MetaGitHubEvent    = "github_event"
	MetaGitHubOwner    = "github_owner"
	MetaGitHubRepo     = "github_repo"
	MetaGitHubLabels   = "github_labels"
	MetaGitHubAuthor   = "github_author"
	MetaGitHubHeadSHA  = "github_head_sha"
	MetaCronSchedule   = "cron_schedule"
	MetaCronExpression = "cron_expression"
)

// Surface is what a connector said about where an event came from.
type Surface struct {
	SlackEvent    string
	SlackChannel  string
	SlackCommand  string
	GitHubEvent   string
	GitHubOwner   string
	GitHubRepo    string
	GitHubLabels  string
	GitHubAuthor  string
	GitHubHeadSHA string
	CronSchedule  string
}

// SurfaceOf reads the connector fields of an event. A field it does not
// carry is empty.
func SurfaceOf(e event.Envelope) Surface {
	m := Meta(e)
	return Surface{
		SlackEvent:    m[MetaSlackEvent],
		SlackChannel:  m[MetaSlackChannel],
		SlackCommand:  m[MetaSlackCommand],
		GitHubEvent:   m[MetaGitHubEvent],
		GitHubOwner:   m[MetaGitHubOwner],
		GitHubRepo:    m[MetaGitHubRepo],
		GitHubLabels:  m[MetaGitHubLabels],
		GitHubAuthor:  m[MetaGitHubAuthor],
		GitHubHeadSHA: m[MetaGitHubHeadSHA],
		CronSchedule:  m[MetaCronSchedule],
	}
}

// SetGitHubHeadSHA records the head commit a GitHub event is about.
func SetGitHubHeadSHA(e *event.Envelope, sha string) {
	switch b := e.Data.(type) {
	case event.GitHubIssueCommentCreated:
		b.HeadSHA = sha
		e.Data = b
	case event.GitHubReviewCommentCreated:
		b.HeadSHA = sha
		e.Data = b
	case event.GitHubReviewSubmitted:
		b.HeadSHA = sha
		e.Data = b
	case event.GitHubIssueOpened:
		b.HeadSHA = sha
		e.Data = b
	case event.GitHubIssueLabeled:
		b.HeadSHA = sha
		e.Data = b
	case event.GitHubPullOpened:
		b.HeadSHA = sha
		e.Data = b
	case event.GitHubPullReadyForReview:
		b.HeadSHA = sha
		e.Data = b
	case event.GitHubReviewRequested:
		b.HeadSHA = sha
		e.Data = b
	case event.GitHubPullSynchronize:
		b.HeadSHA = sha
		e.Data = b
	case event.GitHubPullLabeled:
		b.HeadSHA = sha
		e.Data = b
	default:
		SetMeta(e, MetaGitHubHeadSHA, sha)
	}
}

// PrivateMetaPrefix starts a context key that is kept with the event and
// never shown to a model.
const PrivateMetaPrefix = "_"

// PrivateMeta reports whether a context key is private.
func PrivateMeta(key string) bool { return strings.HasPrefix(key, PrivateMetaPrefix) }

// Clone is a deep copy of an event: changing the copy's body leaves the
// original as it was. Private keys are copied too.
func Clone(e event.Envelope) event.Envelope {
	priv := maps.Clone(e.Private)
	if e.Data != nil {
		if r, ok := event.RequestOf(e.Data); ok {
			r.Context = maps.Clone(r.Context)
			r.Mentions = slices.Clone(r.Mentions)
			r.Attachments = slices.Clone(r.Attachments)
			if b, ok := event.WithRequest(e.Data, r); ok {
				e.Data = b
			}
		}
	}
	e.Private = priv
	return e
}
