// Package slack composes fiber's Slack transport with humagen's session
// lifecycle and command-discovery policy. Wire APIs and signed decoding stay upstream.
package slack

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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
}
type Connector struct {
	*transport.Connector
	cfg       Config
	offered   offered
	statusMu  sync.Mutex
	statusSeq map[string]uint64
	live      live
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
	base.Prepare = func(_ context.Context, tag *event.Envelope) error {
		if cfg.Agents != nil && wire.AgentOf(*tag) == "" {
			if agent, stripped, ok := mention.Find(wire.TextOf(*tag), cfg.Agents); ok {
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
