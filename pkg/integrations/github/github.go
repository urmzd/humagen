// Package github composes transport capabilities with review and fix policy.
// API calls stay in fiber; presentation is supplied by humagen.
package github

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	presentation "github.com/urmzd/humagen/internal/presentation/github"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/orchagent/flow/address"
	"github.com/urmzd/orchagent/flow/connector"
	"github.com/urmzd/orchagent/flow/connectors"
	transport "github.com/urmzd/orchagent/flow/connectors/github"
	"github.com/urmzd/orchagent/flow/connectors/mention"
	"github.com/urmzd/orchagent/flow/deliverystate"
	"github.com/urmzd/orchagent/flow/event"
)

// Config combines transport configuration with explicit product policy.
type Config struct {
	FixPullRequests  bool
	AuthorizeFixPull func(context.Context, transport.Ref) error
	FixPullContext   func(context.Context, transport.Ref) (transport.FixPullContext, error)
	ObserveDelivery  connectors.DeliveryObserver
	// ReviewText verifies host provenance before native and comment rendering.
	ReviewText         func(transport.View) (string, error)
	Feedback           connectors.FeedbackHandler
	ContinueThread     func(address.Address) bool
	StatusReactions    bool
	ActionRepositories []string
	API                transport.API
	Verifier           transport.Verifier
	Owners             []string
	ReadRepositories   []string
	Bots               map[string]string
	Agents             mention.Set
	// Login is the app's own login. A comment that mentions it and names
	// no agent is raised naming none, and the host chooses who answers.
	Login          string
	Checks         bool
	Interval       time.Duration
	Clock          func() time.Time
	MaxBody        int64
	Guides         bool
	Mutations      bool
	MutationSecret []byte
	// Tenant is the workspace whose runs this connector's tags become. A fix
	// grant names the run it is good for, and the runtime derives a run's id
	// from the tenant and the tag id together, so the grant cannot be signed
	// without it. Mutations without a Tenant are refused by New.
	Tenant string
	// DeliveryState and DeliveryScope keep this installation's messages
	// through connector rebuilds. Replicas must share both.
	DeliveryState deliverystate.Store
	DeliveryScope string
}

// Connector decorates the transport without introducing another execution path.
type Connector struct {
	*transport.Connector
	cfg     Config
	guideMu sync.Mutex
}

func New(cfg Config) (*Connector, error) {
	c := &Connector{cfg: cfg}
	c.cfg.MutationSecret = append([]byte(nil), cfg.MutationSecret...)
	if cfg.Guides || cfg.Mutations {
		if _, ok := cfg.API.(transport.ContextAPI); !ok {
			return nil, fmt.Errorf("github: policy needs ContextAPI")
		}
	}
	if cfg.Guides {
		if _, ok := cfg.API.(transport.GuideAPI); !ok || len(cfg.Bots) == 0 {
			return nil, fmt.Errorf("github: guides need GuideAPI and bot mapping")
		}
	}
	if cfg.Mutations {
		if _, ok := cfg.API.(transport.MutationAPI); !ok || len(cfg.MutationSecret) < 16 {
			return nil, fmt.Errorf("github: mutations need MutationAPI and signing secret of at least 16 bytes")
		}
		if strings.TrimSpace(cfg.Tenant) == "" {
			return nil, fmt.Errorf("github: mutations need the tenant the connector's runs belong to")
		}
	}
	base := transport.Config{API: cfg.API, Verifier: cfg.Verifier, Owners: cfg.Owners, ReadRepositories: cfg.ReadRepositories, Bots: cfg.Bots, Agents: cfg.Agents, Login: cfg.Login, Checks: cfg.Checks, Tenant: cfg.Tenant, Interval: cfg.Interval, Clock: cfg.Clock, MaxBody: cfg.MaxBody}
	base.DeliveryState, base.DeliveryScope = cfg.DeliveryState, cfg.DeliveryScope
	base.FixPullRequests, base.AuthorizeFixPull = cfg.FixPullRequests, cfg.AuthorizeFixPull
	base.FixPullContext = cfg.FixPullContext
	base.Feedback, base.ContinueThread, base.StatusReactions, base.ActionRepositories = cfg.Feedback, cfg.ContinueThread, cfg.StatusReactions, cfg.ActionRepositories
	base.ObserveDelivery = cfg.ObserveDelivery
	base.Prepare = c.prepare
	base.Render = func(v transport.View) string {
		if cfg.ReviewText != nil && v.Terminal() {
			text, err := cfg.ReviewText(v)
			if err != nil {
				v.Text = "Review evidence is temporarily unavailable."
			} else {
				v.Text = text
			}
		}
		return presentation.RenderReview(v)
	}
	if _, ok := cfg.API.(transport.ReviewAPI); ok && cfg.Login != "" {
		if _, ok := cfg.API.(transport.ReviewHistoryAPI); ok {
			if _, ok := cfg.API.(transport.ContextAPI); ok {
				base.Review = func(v transport.View, files []transport.ContextFile) (transport.Review, bool, error) {
					if cfg.ReviewText != nil {
						text, err := cfg.ReviewText(v)
						if err != nil {
							return transport.Review{}, true, err
						}
						v.Text = text
					}
					var report presentation.ReviewReport
					if err := json.Unmarshal([]byte(presentation.ReviewJSON(v.Text)), &report); err != nil || report.Format != presentation.ReviewFormatV2 {
						return transport.Review{}, false, nil
					}
					review, err := presentation.NativeReview(report, files)
					return review, true, err
				}
				// The transport calls this only after verifying an existing app-authored
				// review's immutable payload digest. A historical approval reproduces its
				// body; it grants no new submission and keeps current evidence checks.
				base.RecoverReview = func(v transport.View, files []transport.ContextFile, disposition string) (transport.Review, bool, error) {
					if cfg.ReviewText != nil {
						text, err := cfg.ReviewText(v)
						if err != nil {
							return transport.Review{}, true, err
						}
						v.Text = text
					}
					var report presentation.ReviewReport
					if err := json.Unmarshal([]byte(presentation.ReviewJSON(v.Text)), &report); err != nil || report.Format != presentation.ReviewFormatV2 {
						return transport.Review{}, false, nil
					}
					review, err := presentation.RecoveredReview(report, files, disposition)
					return review, true, err
				}
			}
		}
	}
	base.Conclusion = conclusion
	var err error
	c.Connector, err = transport.New(base)
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Connector) Actions() []connector.Action {
	actions := c.Connector.Actions()
	if c.cfg.Mutations {
		actions = append(actions, c.mutationAction(), c.runChecksAction())
	}
	return actions
}

// prepare applies humagen's policy to an event before it is published. An
// event that names no agent is one whose agent the host has yet to choose,
// such as a comment addressed to the app alone: the guide names the agent
// that answers, so such an event posts none.
func (c *Connector) prepare(ctx context.Context, tag *event.Envelope) error {
	source, err := address.Parse(tag.Channel)
	if err != nil || len(source.Path) < 2 {
		return fmt.Errorf("github: event has no pull request or issue channel")
	}
	if source.Path[1] == "pull" {
		if c.cfg.Guides && wire.AgentOf(*tag) != "" {
			if err := c.prepareGuide(ctx, tag); err != nil {
				return err
			}
			source, _ = address.Parse(tag.Channel)
		}
		if c.cfg.Mutations && source.Params["head_sha"] == "" {
			ref, err := c.Reference(source)
			if err != nil {
				return err
			}
			evidence, err := c.cfg.API.(ContextAPI).ReadContext(ctx, ref, 1)
			if err != nil {
				return err
			}
			if !transport.ValidSHA(evidence.Subject.Head.SHA) {
				return fmt.Errorf("github: missing pinned head")
			}
			setSource(tag, source.WithParam("head_sha", evidence.Subject.Head.SHA))
			wire.SetGitHubHeadSHA(tag, evidence.Subject.Head.SHA)
		}
	}
	return c.grantMutation(ctx, tag)
}

// sourceOf is the address an event is about and answers on.
func sourceOf(tag event.Envelope) (address.Address, error) { return address.Parse(tag.Channel) }

// setSource moves an event to a, its source and channel both: the runtime
// binds the run's tools to its source and answers on its channel.
func setSource(tag *event.Envelope, a address.Address) {
	tag.Source, tag.Channel = a.String(), a.String()
}

func conclusion(v transport.View) string {
	var r presentation.ReviewReport
	if json.Unmarshal([]byte(presentation.ReviewJSON(v.Text)), &r) == nil && presentation.IsReviewFormat(r.Format) {
		if _, err := presentation.FormatReview(r); err != nil {
			return "action_required"
		}
		for _, f := range r.Findings {
			if f.Severity == "error" {
				return "failure"
			}
		}
	} else if presentation.NamesReviewFormat(v.Text) {
		return "action_required"
	}
	return "neutral"
}
