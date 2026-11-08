package platform

import (
	"context"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/integrations/slack"
	"github.com/urmzd/orchagent/flow/event"
)

// conversationReader reads who can read a Slack conversation, with the
// installation's own token. The Slack connector is one.
type conversationReader interface {
	Conversation(ctx context.Context, channel string) (slack.Conversation, error)
}

// audienceTTL is how long a conversation's audience is reused before Slack
// is asked again. Membership changes reach a session within it.
const audienceTTL = time.Minute

type audienceEntry struct {
	aud *SurfaceAudience
	at  time.Time
}

// audiences caches each conversation's audience per client.
type audiences struct {
	mu   sync.Mutex
	seen map[string]audienceEntry
}

func (a *audiences) get(key string) (audienceEntry, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	e, ok := a.seen[key]
	return e, ok
}

func (a *audiences) put(key string, e audienceEntry) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.seen == nil {
		a.seen = map[string]audienceEntry{}
	}
	a.seen[key] = e
}

// sendSlackMessage sends a Slack message a person the client maps wrote to
// the root of its thread through the messages API (SendMessages): the entry
// agent owns the root, and a mentioned agent answers in its child session.
// It reports whether it took the message. A message with files still goes
// the connector's way, which hears voice notes and reads attachments, until
// the messages API takes them.
func (s *surface) sendSlackMessage(ctx context.Context, st *seat, c clients.Client, person string, tag event.Envelope) bool {
	if c.Surface != clients.Slack || person == "" || len(wire.AttachmentsOf(tag)) > 0 {
		return false
	}
	msg, ok := wire.SlackMessageOf(tag)
	if !ok || msg.Team == "" || msg.Channel == "" || msg.TS == "" {
		return false
	}
	content := make(Content, 0, len(msg.Mentions)+1)
	for _, name := range msg.Mentions {
		// The connector also lists mentions it could not resolve, such as
		// the app's own bot user: only an agent that exists is a mention.
		if _, err := s.p.Catalog().Get(ctx, c.Tenant, name, 0); err == nil {
			content = append(content, ContentBlock{Type: "mention", Agent: name})
		}
	}
	text, people := slackPeopleIn(c, wire.TextOf(tag))
	for _, person := range people {
		content = append(content, ContentBlock{Type: "mention", Person: person})
	}
	if text != "" {
		content = append(content, ContentBlock{Type: "text", Text: text})
	}
	if len(content) == 0 {
		return true
	}
	thread := msg.ThreadTS
	if thread == msg.TS {
		thread = ""
	}
	ref := &ExternalRef{
		Type: RefSlack, Client: c.ID, Team: msg.Team, Channel: msg.Channel, TS: msg.TS, ThreadTS: thread,
		Audience: s.audienceOf(ctx, st, c, msg),
	}
	in := MessagesInput{Messages: []Message{{Role: "user", Content: content}}, Ref: ref}
	if _, err := s.p.SendMessages(ctx, s.p.byPolicy(c.Tenant, person), in); err != nil {
		s.p.log.Warn("platform: a Slack message was not sent to its session", "tenant", c.Tenant, "client", c.ID, "event", tag.ID, "error", err)
	}
	return true
}

// audienceOf is who can read the conversation a message was said in, read
// from Slack, never guessed from the channel's id (private channels have C
// ids too). When Slack cannot be asked, it is a private channel with no
// members known, so nothing is shared beyond the person asking.
func (s *surface) audienceOf(ctx context.Context, st *seat, c clients.Client, msg wire.SlackMessage) *SurfaceAudience {
	if msg.ChannelType == "im" {
		return &SurfaceAudience{Type: ChannelDM}
	}
	key := c.Tenant + "/" + c.ID + "/" + msg.Channel
	if hit, ok := s.audiences.get(key); ok && s.p.now().Sub(hit.at) < audienceTTL {
		return hit.aud
	}
	closed := &SurfaceAudience{Type: ChannelPrivate}
	reader, ok := st.peer.(conversationReader)
	if !ok {
		return closed
	}
	conv, err := reader.Conversation(ctx, msg.Channel)
	if err != nil {
		s.p.log.Warn("platform: a Slack conversation's audience could not be read; it is treated as private", "tenant", c.Tenant, "client", c.ID, "channel", msg.Channel, "error", err)
		return closed
	}
	aud := &SurfaceAudience{Type: conv.Type, Members: conv.Members}
	s.audiences.put(key, audienceEntry{aud: aud, at: s.p.now()})
	return aud
}

// slackUserMention is a Slack user written as the connector leaves one it
// could not resolve to an agent: @U0123ABCD.
var slackUserMention = regexp.MustCompile(`@([UW][A-Z0-9]{6,20})\b`)

// slackPeopleIn takes the Slack users a message mentions out of its text: a
// person the client maps becomes a mention of them, and anyone else (the
// app's own bot user among them) is left out, as nobody Humagen knows.
func slackPeopleIn(c clients.Client, text string) (string, []string) {
	machine := ""
	if c.Slack != nil {
		machine = c.Slack.Machine
	}
	var people []string
	out := slackUserMention.ReplaceAllStringFunc(text, func(m string) string {
		if subject, ok := c.ActsFor(machine, m[1:]); ok && subject != "" {
			if !slices.Contains(people, subject) {
				people = append(people, subject)
			}
		}
		return ""
	})
	return strings.Join(strings.Fields(out), " "), people
}
