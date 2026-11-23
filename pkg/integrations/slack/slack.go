// Package slack composes fiber's Slack transport with humagen's session
// lifecycle and command-discovery policy. Wire APIs and signed decoding stay upstream.
package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/urmzd/saige/flow/address"
	"github.com/urmzd/saige/flow/connector"
	"github.com/urmzd/saige/flow/connectors"
	"github.com/urmzd/saige/flow/connectors/mention"
	transport "github.com/urmzd/saige/flow/connectors/slack"
	"github.com/urmzd/saige/flow/deliverystate"
	"github.com/urmzd/saige/flow/event"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/trigger"
)

type Config struct {
	Feedback        connectors.FeedbackHandler
	ObserveDelivery connectors.DeliveryObserver
	ContinueThread  func(string, string, string) bool
	StatusReactions bool
	ActionChannels  []string
	Tenant          string
	Files           connectors.Files
	FileToken       string
	FileTokenSource func(context.Context) (string, error)
	AppID           string
	API             transport.API
	Verifier        transport.Verifier
	Team            string
	Bots            map[string]string
	// Agents is the set of names a plain "@name" in a message may name. A
	// message no bot mention addressed names the first of them it mentions.
	// Nil reads no plain names.
	Agents   mention.Set
	Interval time.Duration
	Clock    func() time.Time
	MaxBody  int64
	Sessions bool
	Commands []transport.Command
	Log      *slog.Logger
	// DeliveryState and DeliveryScope keep this installation's messages
	// through connector rebuilds. Replicas must share both.
	DeliveryState deliverystate.Store
	DeliveryScope string
	// Sender is who the client acts for. A message, mention, slash command
	// or interactive payload from anyone else is answered here and goes no
	// further (refuse). Nil leaves the decision to the platform alone.
	Sender SenderFunc
}
type Connector struct {
	*transport.Connector
	cfg       Config
	offered   offered
	statusMu  sync.Mutex
	statusSeq map[string]uint64
	live      live
	turned    turnAway
}

func New(cfg Config) (*Connector, error) {
	if err := validCommands(cfg.Commands); err != nil {
		return nil, err
	}
	if cfg.Sessions || len(cfg.Commands) > 0 {
		if _, ok := cfg.API.(transport.AgentAPI); !ok {
			return nil, fmt.Errorf("slack: session policy needs AgentAPI")
		}
	}
	cfg.Commands = append([]transport.Command(nil), cfg.Commands...)
	c := &Connector{cfg: cfg, offered: offered{seen: map[string]bool{}}, statusSeq: map[string]uint64{}}
	c.live.open, c.live.asked = map[string]session{}, map[string]bool{}
	base := transport.Config{Tenant: cfg.Tenant, Files: cfg.Files, FileToken: cfg.FileToken, AppID: cfg.AppID, API: cfg.API, Verifier: cfg.Verifier, Team: cfg.Team, Bots: cfg.Bots, Interval: cfg.Interval, Clock: cfg.Clock, MaxBody: cfg.MaxBody}
	base.FileTokenSource = cfg.FileTokenSource
	base.DeliveryState, base.DeliveryScope = cfg.DeliveryState, cfg.DeliveryScope
	base.Feedback, base.ObserveDelivery, base.ContinueThread, base.StatusReactions, base.ActionChannels = cfg.Feedback, cfg.ObserveDelivery, cfg.ContinueThread, cfg.StatusReactions, cfg.ActionChannels
	base.Prepare = func(ctx context.Context, tag *event.Envelope) error {
		if cfg.Agents != nil && wire.AgentOf(*tag) == "" {
			text := wire.TextOf(*tag)
			// The agent named right after the app's own mention that opens
			// the message is asked: "@Humagen ask docs-bot ...", "@Humagen
			// docs-bot: ...". Agents are not Slack users, so a person cannot
			// pick one from Slack's list. Only there: a thread reply, a
			// direct message, or a message that opens with someone else
			// reads its first word as words.
			at, opened := 0, ctx.Value(openedKey{}) != nil
			if opened {
				at = firstToken(text)
			}
			if agent, n := trigger.Leading(text[at:], cfg.Agents.Lookup); opened && agent != "" {
				wire.SetAgent(tag, agent)
				wire.SetText(tag, strings.TrimSpace(text[:at]+text[at+n:]))
			} else if agent, stripped, ok := mention.Find(text, cfg.Agents); ok {
				wire.SetAgent(tag, agent)
				wire.SetText(tag, stripped)
			}
		}
		from := wire.SurfaceOf(*tag)
		if from.SlackEvent != "slash_command" {
			return nil
		}
		for _, cmd := range cfg.Commands {
			if cmd.Name == strings.TrimPrefix(from.SlackCommand, "/") {
				return nil
			}
		}
		return fmt.Errorf("slack: command is not configured")
	}
	var err error
	c.Connector, err = transport.New(base)
	return c, err
}

// Ingest runs the Slack trigger. A person pressing stop on a session is
// handled here, not published: it stops the runs answering in the session.
func (c *Connector) Ingest(ctx context.Context, publish connector.Publish) error {
	return c.Connector.Ingest(ctx, func(ctx context.Context, e event.Envelope) error {
		if b, ok := e.Data.(event.SlackSessionStopped); ok {
			return c.stopped(ctx, stopOf(b))
		}
		return publish(ctx, e)
	})
}

func (c *Connector) Deliver(ctx context.Context, to address.Address, e event.Envelope) error {
	// Everything a run publishes is rendered, a stopped run's last event
	// included: the sink shows what the agent had said, marked stopped.
	stopping := false
	if to.Connector == transport.Name && len(to.Path) == 1 {
		stopping = c.live.track(to, e)
	}
	deliveryErr := c.Connector.Deliver(ctx, to, e)
	status, ok := statusFor(e.Type)
	if !ok {
		return deliveryErr
	}
	// A run someone stopped from Slack has had its session set to active.
	// Until it ends, a lifecycle event it was still publishing does not set
	// the session back to working.
	if stopping && status != StatusActive {
		return deliveryErr
	}
	// Keep replayed lifecycle events from regressing a completed session.
	// A failed API call does not advance the cursor, so a retry still runs.
	c.statusMu.Lock()
	defer c.statusMu.Unlock()
	key := e.Run + "\x00" + to.String()
	if seq, ok := c.statusSeq[key]; ok && e.Seq <= seq {
		return deliveryErr
	}
	if err := c.session(ctx, to, e); err != nil {
		return errors.Join(deliveryErr, err)
	}
	if len(c.statusSeq) >= maxChannels {
		clear(c.statusSeq)
	}
	c.statusSeq[key] = e.Seq
	return deliveryErr
}

type API = transport.API
type Command = transport.Command
type Status = transport.Status
type Session = transport.Session
type Fake = transport.Fake
type FakeMessage = transport.FakeMessage
type Error = transport.Error

const (
	StatusProcessing = transport.StatusProcessing
	StatusSuspended  = transport.StatusSuspended
	StatusActive     = transport.StatusActive
)

var NewFake = transport.NewFake
var NewHTTPAPI = transport.NewHTTPAPI

// firstToken is how many bytes of text its first "@name" takes: the app's
// own mention as the transport writes it ("@U08BOT", or "@humagen" from a
// label). 0 when the text does not open with one.
func firstToken(text string) int {
	if text == "" || text[0] != '@' {
		return 0
	}
	i := 1
	for i < len(text) && !strings.ContainsRune(" \t\n\r:,", rune(text[i])) {
		i++
	}
	return i
}

type openedKey struct{}

// maxOpenedBody bounds the delivery read for the app's mention. A larger one
// is handed on unread, and the transport refuses it by its own limit.
const maxOpenedBody = 25 << 20

// openedBySelf matches an event's text that opens with a user mention.
var openedBySelf = regexp.MustCompile(`^\s*<@([A-Z0-9]+)(?:\|[^<>]*)?>`)

// ServeHTTP refuses a request from a sender the client does not map
// (refuse), and notes whether a message opens with the app's own bot user,
// the one place a word after it names an agent (prepare). The transport
// reads and verifies the delivery as before.
func (c *Connector) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || r.Body == nil || (c.cfg.Agents == nil && c.cfg.Sender == nil) {
		c.Connector.ServeHTTP(w, r)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxOpenedBody+1))
	r.Body = io.NopCloser(io.MultiReader(bytes.NewReader(body), r.Body))
	if err == nil && len(body) <= maxOpenedBody && c.refuse(w, r, body) {
		return
	}
	if err == nil && len(body) <= maxOpenedBody && c.cfg.Agents != nil && openedByApp(body) {
		r = r.WithContext(context.WithValue(r.Context(), openedKey{}, true))
	}
	c.Connector.ServeHTTP(w, r)
}

// openedByApp reports whether an event's text opens with a mention of a bot
// user the delivery was made for (its authorizations): the app itself.
func openedByApp(body []byte) bool {
	var env struct {
		Authorizations []struct {
			UserID string `json:"user_id"`
			IsBot  bool   `json:"is_bot"`
		} `json:"authorizations"`
		Event struct {
			Text string `json:"text"`
		} `json:"event"`
	}
	if json.Unmarshal(body, &env) != nil {
		return false
	}
	m := openedBySelf.FindStringSubmatch(env.Event.Text)
	if m == nil {
		return false
	}
	for _, a := range env.Authorizations {
		if a.IsBot && a.UserID == m[1] {
			return true
		}
	}
	return false
}
