// Package github composes transport capabilities with review and fix policy.
// API calls stay in fiber; presentation is supplied by humagen.
package github

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	presentation "github.com/urmzd/humagen/internal/presentation/github"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/saige/flow/address"
	"github.com/urmzd/saige/flow/connector"
	"github.com/urmzd/saige/flow/connectors"
	transport "github.com/urmzd/saige/flow/connectors/github"
	"github.com/urmzd/saige/flow/connectors/mention"
	"github.com/urmzd/saige/flow/deliverystate"
	"github.com/urmzd/saige/flow/event"
)

// Config combines transport configuration with explicit product policy.
type Config struct {
	FixPullRequests  bool
	AuthorizeFixPull func(context.Context, transport.Ref) error
	FixPullContext   func(context.Context, transport.Ref) (transport.FixPullContext, error)
	ObserveDelivery  connectors.DeliveryObserver
	// ReviewText verifies host provenance before native and comment rendering.
	ReviewText func(transport.View) (string, error)
	// AsksReview reports whether the run v answers for was asked for a
	// review: the shape the host saved on the run when it started, or a
	// review its agent started (start_review). A run asked anything else
	// posts no native review. An error is retried. Nil asks every run that
	// answers with a review for one.
	AsksReview func(transport.View) (bool, error)
	// ReviewPolicy is what the host says of the run a review answers for:
	// its agent's review policy narrowed by the repository's settings,
	// whether a signal that may approve started it, and the pull request's
	// author. With it the event is ReviewEvent's, decided again by the
	// transport's MayApprove hook before an approval posts. An error is
	// retried. Nil approves nothing.
	ReviewPolicy func(context.Context, transport.View) (ReviewGrant, error)
	// ApprovalClaims is where one approval of each commit is claimed, in a
	// store every replica shares, so two runs or two replicas never both
	// approve a commit. Nil approves nothing.
	ApprovalClaims state.Store
	// Log is where a review's side work that fails without stopping it,
	// such as a stale review that could not be dismissed, is told. Nil is
	// slog's default.
	Log *slog.Logger
	// SessionLink is the console's address of the session a run answers
	// in, linked under its comment. Nil, or "", links nothing.
	SessionLink        func(transport.View) string
	Feedback           connectors.FeedbackHandler
	ContinueThread     func(address.Address) bool
	StatusReactions    bool
	ActionRepositories []string
	API                transport.API
	Verifier           transport.Verifier
	Owners             []string
	ReadRepositories   []string
	// MayRead grants a cross-source read of a repository ReadRepositories
	// does not name, such as one the GitHub App installation covers. It is
	// the transport's Config.MayRead: it grants reads only (the read tools,
	// Connector.ReadComments, and a redirect of a read of the run's own
	// repository to this one), and it is given the owner and repository in
	// lower case. Nil grants nothing more.
	MayRead func(ctx context.Context, owner, repo string) bool
	// ReadRefused names a repository only its own runs may read: a
	// cross-source read of it, or a redirect to it from a read of another
	// repository, is refused whatever MayRead or ReadRepositories grant. It
	// is the transport's Config.ReadRefused, given the owner and repository
	// in lower case. Nil refuses nothing.
	ReadRefused func(ctx context.Context, owner, repo string) bool
	Bots        map[string]string
	Agents      mention.Set
	// AppID is the numeric ID of the GitHub App the connector acts as, 0
	// for a connector that is no app. Checks of the app's own are told
	// apart by it, and by Login.
	AppID int64
	// Login is the app's own login, which it comments as. With Triggers
	// set, it asks as a trigger does; without them, a comment that mentions
	// it and names no agent is raised naming none, and the host chooses who
	// answers.
	Login string
	// Triggers are the handles that ask, "humagen" and the app's login
	// without "[bot]" (pkg/trigger). Set, only a comment that holds one asks,
	// and the agent named right after it answers (trigger.go); a mention of
	// an agent's name alone asks nobody, and a review requested from a
	// trigger's account is the host's default's. Empty leaves the
	// transport's own reading, where any agent's mention asks.
	Triggers       []string
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

	// legacyPosts posts reviews in the layout from before reviews read as
	// prose, as a deploy of that code did. Tests only.
	legacyPosts bool
	// unmarkedPosts posts reviews in the prose layout without the agent's
	// mark, as a deploy from before stale reviews were dismissed did. Tests
	// only.
	unmarkedPosts bool
}

// reviewDecisionTimeout bounds the reads a review's event is decided on.
const reviewDecisionTimeout = 30 * time.Second

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
	base := transport.Config{API: cfg.API, Verifier: cfg.Verifier, Owners: cfg.Owners, ReadRepositories: cfg.ReadRepositories, MayRead: cfg.MayRead, ReadRefused: cfg.ReadRefused, Bots: cfg.Bots, Agents: triggerAware(cfg.Agents, cfg.Triggers), Login: cfg.Login, AppID: cfg.AppID, Checks: cfg.Checks, Tenant: cfg.Tenant, Interval: cfg.Interval, Clock: cfg.Clock, MaxBody: cfg.MaxBody}
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
		return presentation.RenderComment(v, sessionLink(cfg, v))
	}
	if _, ok := cfg.API.(transport.ReviewAPI); ok && cfg.Login != "" {
		if _, ok := cfg.API.(transport.ReviewHistoryAPI); ok {
			if _, ok := cfg.API.(transport.ContextAPI); ok {
				layouts := newLayouts(cfg)
				pending := newApprovals(cfg.Clock)
				pending.host = func() reviewHost { return c.Connector }
				// The transport asks the hook only of an API that can also
				// dismiss reviews; without one nothing approves.
				_, dismisses := cfg.API.(transport.ReviewDismissAPI)
				if cfg.ReviewPolicy != nil && dismisses {
					base.MayApprove = pending.mayApprove(cfg)
				}
				base.Review = func(v transport.View, files []transport.ContextFile) (transport.Review, bool, error) {
					// A run the host saved as a reply posts no review, whatever
					// its agent wrote: the host, not the model, chose the shape.
					if cfg.AsksReview != nil {
						asked, err := cfg.AsksReview(v)
						if err != nil {
							return transport.Review{}, true, err
						}
						if !asked {
							return transport.Review{}, false, nil
						}
					}
					report, ok, err := reviewOf(cfg, v)
					if !ok || err != nil {
						return transport.Review{}, err != nil, err
					}
					if cfg.legacyPosts {
						review, err := presentation.LegacyRecoveredReview(report, files, "")
						return review, true, err
					}
					// The transport gives this no context; the decision's reads
					// are bounded on their own.
					ctx, cancel := context.WithTimeout(context.Background(), reviewDecisionTimeout)
					defer cancel()
					policy, facts, err := reviewDecision(ctx, cfg, v, report)
					if err != nil {
						return transport.Review{}, true, err
					}
					if base.MayApprove == nil {
						policy.Approve = "never"
					}
					review, err := presentation.NativeReview(report, files, presentation.TargetOf(v.Channel), policy, facts)
					if err != nil {
						return transport.Review{}, true, err
					}
					if review.Event == "APPROVE" {
						pending.plan(v, report)
					}
					// The agent's approvals of another head are stale unless this
					// review approves (the hook dismisses them if it then says
					// no). Its change requests are dismissed only once this
					// review has posted (Deliver). A dismissal that fails is
					// logged and never stops this review.
					if owner, repo, number, ok := pullOf(v.Channel); ok {
						dismissStale(ctx, cfg, c.Connector, transport.Ref{Owner: owner, Repo: repo, Number: number}, v.Agent, review.CommitID, review.Event)
					}
					// Say which layout this review is posted in before posting it,
					// so its recovery reproduces it.
					if cfg.unmarkedPosts {
						if err := layouts.record(v, proseLayout); err != nil {
							return transport.Review{}, true, err
						}
						review.Body += presentation.SessionFooter(sessionLink(cfg, v))
						return review, true, nil
					}
					if err := layouts.mark(v); err != nil {
						return transport.Review{}, true, err
					}
					// Which agent wrote it is kept before it posts, by the
					// marker the sink posts it with.
					if err := recordReviewAgent(ctx, cfg, v, review.CommitID); err != nil {
						return transport.Review{}, true, err
					}
					review.Body = presentation.WithAgentMark(review.Body, v.Agent) + presentation.SessionFooter(sessionLink(cfg, v))
					return review, true, nil
				}
				// The transport calls this only after verifying an existing app-authored
				// review's immutable payload digest. A historical approval reproduces its
				// body; it grants no new submission and keeps current evidence checks.
				// A review posted before reviews read as prose is reproduced in that
				// layout, so it is receipted and its findings stay mapped.
				base.RecoverReview = func(v transport.View, files []transport.ContextFile, disposition string) (transport.Review, bool, error) {
					report, ok, err := reviewOf(cfg, v)
					if !ok || err != nil {
						return transport.Review{}, err != nil, err
					}
					layout, err := layouts.posted(v)
					if err != nil {
						return transport.Review{}, true, err
					}
					if layout == "" {
						review, err := presentation.LegacyRecoveredReview(report, files, disposition)
						return review, true, err
					}
					review, err := presentation.RecoveredReview(report, files, presentation.TargetOf(v.Channel), disposition)
					if layout == markedLayout {
						review.Body = presentation.WithAgentMark(review.Body, v.Agent)
					}
					review.Body += presentation.SessionFooter(sessionLink(cfg, v))
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

// Deliver delivers an event, and once a run's final answer is delivered on a
// pull request, dismisses its agent's change requests that the agent's
// latest posted review, a comment or an approval, replaces. Nothing is
// dismissed before the review that replaces it has posted.
func (c *Connector) Deliver(ctx context.Context, target address.Address, e event.Envelope) error {
	if err := c.Connector.Deliver(ctx, target, e); err != nil {
		return err
	}
	if _, ok := c.cfg.API.(transport.ReviewHistoryAPI); !ok || e.Type != event.TypeCompleted || e.Agent == "" {
		return nil
	}
	if owner, repo, number, ok := pullOf(target.String()); ok {
		dismissReplaced(ctx, c.cfg, c.Connector, transport.Ref{Owner: owner, Repo: repo, Number: number}, e.Agent)
	}
	return nil
}

func (c *Connector) Actions() []connector.Action {
	actions := c.Connector.Actions()
	for i, a := range actions {
		if a.Name == createReviewAction {
			actions[i] = withoutMarks(a)
		}
	}
	if c.cfg.Mutations {
		actions = append(actions, c.mutationAction(), c.runChecksAction())
	}
	return actions
}

// createReviewAction is the transport's tool that posts a review a model
// wrote.
const createReviewAction = "github_create_review"

// withoutMarks has a review a model posts through the tool written with
// WithoutMarks: its body and each inline comment's body, so nothing in it
// reads as a line the host or the transport writes. Input that is not the
// tool's is passed on as it is, for the tool to refuse.
func withoutMarks(a connector.Action) connector.Action {
	invoke := a.Invoke
	a.Invoke = func(ctx context.Context, target address.Address, raw json.RawMessage) (connector.Result, error) {
		var input map[string]json.RawMessage
		if json.Unmarshal(raw, &input) != nil {
			return invoke(ctx, target, raw)
		}
		var body string
		if json.Unmarshal(input["body"], &body) == nil {
			input["body"], _ = json.Marshal(presentation.WithoutMarks(body))
		}
		var comments []map[string]json.RawMessage
		if json.Unmarshal(input["comments"], &comments) == nil && comments != nil {
			for _, comment := range comments {
				var text string
				if json.Unmarshal(comment["body"], &text) == nil {
					comment["body"], _ = json.Marshal(presentation.WithoutMarks(text))
				}
			}
			input["comments"], _ = json.Marshal(comments)
		}
		out, err := json.Marshal(input)
		if err != nil {
			return invoke(ctx, target, raw)
		}
		return invoke(ctx, target, out)
	}
	return a
}

// prepare applies humagen's policy to an event before it is published. An
// event that names no agent is one whose agent the host has yet to choose,
// such as a comment addressed to the app alone: the guide names the agent
// that answers, so such an event posts none.
func (c *Connector) prepare(ctx context.Context, tag *event.Envelope) error {
	applyAsked(ctx, tag)
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

// reviewOf is a run's answer as a v2 review, after the host's provenance
// check, scoped to the head the run pinned. ok is false for anything else.
func reviewOf(cfg Config, v transport.View) (presentation.ReviewReport, bool, error) {
	if cfg.ReviewText != nil {
		text, err := cfg.ReviewText(v)
		if err != nil {
			return presentation.ReviewReport{}, true, err
		}
		v.Text = text
	}
	report, ok := presentation.ParseReview(v.Text, presentation.TargetOf(v.Channel).Head)
	if !ok || report.Format != presentation.ReviewFormatV2 {
		return presentation.ReviewReport{}, false, nil
	}
	return report, true, nil
}

// sessionLink is the console's address of the session v answers in, or "".
func sessionLink(cfg Config, v transport.View) string {
	if cfg.SessionLink == nil {
		return ""
	}
	return cfg.SessionLink(v)
}

func conclusion(v transport.View) string {
	if r, ok := presentation.ParseReview(v.Text, presentation.TargetOf(v.Channel).Head); ok {
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
