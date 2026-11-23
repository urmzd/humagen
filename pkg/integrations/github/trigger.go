package github

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/trigger"
	"github.com/urmzd/saige/flow/address"
	transport "github.com/urmzd/saige/flow/connectors/github"
	"github.com/urmzd/saige/flow/connectors/mention"
	"github.com/urmzd/saige/flow/event"
)

// maxTriggerBody bounds the delivery read for the trigger. A larger one is
// handed on unread, and the transport refuses it by its own limit.
const maxTriggerBody = 25 << 20

// asked is what a person's text on GitHub asked, read from the delivery
// before the transport reads it, and applied to the event it raises
// (prepare).
type asked struct {
	// review says a review was requested from a trigger's account, such as
	// the humagen user: the host's default reviews, as for "@humagen review".
	review bool
	// triggered says the text holds the trigger. Without it the text is a
	// reply in a conversation an agent already has there.
	triggered bool
	agent     string
	text      string
}

type askedKey struct{}

func askedOf(ctx context.Context) (asked, bool) {
	a, ok := ctx.Value(askedKey{}).(asked)
	return a, ok
}

// personText is the part of a comment, review, issue or pull request a person
// wrote, and where a reply to it would continue a conversation.
type personText struct {
	text string
	// command says the comment is "/stop" or "/feedback", which the
	// transport handles on its own.
	command bool
	// place is the conversation a reply continues, zero for an event that
	// opens one (a review, a new issue or pull request).
	place address.Address
}

// ServeHTTP reads who a person's comment asks before the transport does.
//
// With Config.Triggers set, only the trigger asks: a comment, a review, or a
// new issue or pull request starts a run when its text holds "@humagen" (or
// the app's login) outside code and quoted replies, and the agent named right
// after it answers, or the host's default when none is. A mention of an
// agent's name alone asks nobody, since on GitHub that name may be somebody's
// account. A reply without the trigger in a conversation an agent already has
// there continues it, whatever it mentions. Everything else, review requests
// and automations among them, is the transport's as before.
func (c *Connector) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if len(c.cfg.Triggers) == 0 || r.Method != http.MethodPost || r.Body == nil {
		c.Connector.ServeHTTP(w, r)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxTriggerBody+1))
	r.Body = io.NopCloser(io.MultiReader(bytes.NewReader(body), r.Body))
	if err != nil || len(body) > maxTriggerBody || c.cfg.Verifier == nil || c.cfg.Verifier.Verify(r.Header, body) != nil {
		// The transport answers what it cannot take.
		c.Connector.ServeHTTP(w, r)
		return
	}
	kind := r.Header.Get(transport.HeaderEvent)
	if c.reviewFromTrigger(kind, body) {
		c.Connector.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), askedKey{}, asked{review: true})))
		return
	}
	said, ok := personTextOf(kind, body)
	if !ok || said.command {
		// "/stop" and "/feedback" act on an answer already there, and
		// the transport answers them as before.
		c.Connector.ServeHTTP(w, r)
		return
	}
	var a asked
	if ask, found := trigger.Read(said.text, c.cfg.Triggers, c.lookup); found {
		a = asked{triggered: true, agent: ask.Agent, text: ask.Text}
	} else if said.place.Connector == "" || c.cfg.ContinueThread == nil || !c.cfg.ContinueThread(said.place) {
		// Nobody was asked: it is answered with nothing, as GitHub expects.
		w.WriteHeader(http.StatusOK)
		return
	} else {
		a = asked{text: strings.TrimSpace(said.text)}
	}
	c.Connector.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), askedKey{}, a)))
}

// lookup is the agent a word after the trigger names: one the connector may
// name (Config.Agents), or one a bot login stands for (Config.Bots).
func (c *Connector) lookup(word string) (string, bool) {
	if c.cfg.Agents != nil {
		if agent, ok := c.cfg.Agents.Lookup(word); ok {
			return agent, true
		}
	}
	for login, agent := range c.cfg.Bots {
		if strings.EqualFold(strings.TrimSuffix(login, "[bot]"), word) && !c.isTrigger(word) {
			return agent, true
		}
	}
	return "", false
}

func (c *Connector) isTrigger(word string) bool {
	for _, h := range c.cfg.Triggers {
		if strings.EqualFold(h, word) {
			return true
		}
	}
	return false
}

// applyAsked writes what the delivery asked over what the transport read
// from it: the agent named after the trigger, or none, and the text the
// agent reads. A request that holds the trigger and names no agent is
// marked (wire.MetaTriggered), so the host asks its default and not
// whichever agent answered there before.
func applyAsked(ctx context.Context, tag *event.Envelope) {
	a, ok := askedOf(ctx)
	if !ok {
		return
	}
	if a.review {
		if wire.ReviewerOf(*tag) != "" {
			wire.ClearReviewer(tag)
			wire.SetMeta(tag, wire.MetaTriggered, "true")
		}
		return
	}
	if _, isRequest := event.RequestOf(tag.Data); !isRequest {
		return
	}
	wire.SetAgent(tag, a.agent)
	wire.SetText(tag, a.text)
	if a.triggered && a.agent == "" {
		wire.SetMeta(tag, wire.MetaTriggered, "true")
	}
}

// personTextOf is the text a person wrote in a delivery, for the events
// whose text asks: a comment, a review comment, a review, and a new issue or
// pull request. ok is false for anything else, a bot's included.
func personTextOf(kind string, body []byte) (personText, bool) {
	var p struct {
		Action     string `json:"action"`
		Repository struct {
			Name  string `json:"name"`
			Owner struct {
				Login string `json:"login"`
			} `json:"owner"`
		} `json:"repository"`
		Sender struct {
			Type string `json:"type"`
		} `json:"sender"`
		Issue *struct {
			Number      int             `json:"number"`
			Title       string          `json:"title"`
			Body        string          `json:"body"`
			PullRequest json.RawMessage `json:"pull_request"`
		} `json:"issue"`
		PullRequest *struct {
			Number int    `json:"number"`
			Title  string `json:"title"`
			Body   string `json:"body"`
			Head   struct {
				SHA string `json:"sha"`
			} `json:"head"`
		} `json:"pull_request"`
		Comment *struct {
			ID          int64  `json:"id"`
			InReplyToID int64  `json:"in_reply_to_id"`
			Body        string `json:"body"`
		} `json:"comment"`
		Review *struct {
			Body string `json:"body"`
		} `json:"review"`
	}
	if json.Unmarshal(body, &p) != nil || p.Sender.Type == "Bot" || p.Repository.Name == "" || p.Repository.Owner.Login == "" {
		return personText{}, false
	}
	place := func(kind string, number int) address.Address {
		return address.Address{Connector: transport.Name, Workspace: p.Repository.Owner.Login, Path: []string{p.Repository.Name, kind, strconv.Itoa(number)}}
	}
	switch kind {
	case "issue_comment":
		if p.Issue == nil || p.Comment == nil {
			return personText{}, false
		}
		where := "issues"
		if len(p.Issue.PullRequest) > 0 && string(p.Issue.PullRequest) != "null" {
			where = "pull"
		}
		return personText{text: p.Comment.Body, command: isCommand(p.Comment.Body), place: place(where, p.Issue.Number)}, true
	case "pull_request_review_comment":
		if p.PullRequest == nil || p.Comment == nil {
			return personText{}, false
		}
		root := p.Comment.ID
		if p.Comment.InReplyToID != 0 {
			root = p.Comment.InReplyToID
		}
		at := place("pull", p.PullRequest.Number)
		if p.PullRequest.Head.SHA != "" {
			at = at.WithParam("head_sha", p.PullRequest.Head.SHA)
		}
		return personText{text: p.Comment.Body, command: isCommand(p.Comment.Body), place: at.WithParam("review_comment", strconv.FormatInt(root, 10))}, true
	case "pull_request_review":
		if p.PullRequest == nil || p.Review == nil {
			return personText{}, false
		}
		return personText{text: p.Review.Body}, true
	case "issues":
		if p.Issue == nil || p.Action != "opened" {
			return personText{}, false
		}
		return personText{text: p.Issue.Title + "\n\n" + p.Issue.Body}, true
	case "pull_request":
		if p.PullRequest == nil || p.Action != "opened" && p.Action != "ready_for_review" {
			return personText{}, false
		}
		return personText{text: p.PullRequest.Title + "\n\n" + p.PullRequest.Body}, true
	}
	return personText{}, false
}

// triggerAware is the name set the transport reads mentions with: agents,
// and every trigger. The transport raises an event only for text that
// mentions a name it knows, so a trigger other than the app's own login
// ("@humagen" for an app called humagen-acme) must be one; what the event
// then asks is written over it (applyAsked).
func triggerAware(agents mention.Set, triggers []string) mention.Set {
	if len(triggers) == 0 {
		return agents
	}
	return triggerSet{agents: agents, triggers: mention.NewNames(triggers...)}
}

type triggerSet struct {
	agents   mention.Set
	triggers mention.Names
}

func (s triggerSet) Lookup(token string) (string, bool) {
	if s.agents != nil {
		if agent, ok := s.agents.Lookup(token); ok {
			return agent, true
		}
	}
	return s.triggers.Lookup(token)
}

// isCommand reports whether a comment is one the transport acts on itself:
// "/stop", or "/feedback up|down ...", on an answer already there.
func isCommand(body string) bool {
	fields := strings.Fields(body)
	return len(fields) > 0 && (fields[0] == "/stop" || fields[0] == "/feedback")
}

// reviewFromTrigger reports whether a delivery requests a pull request's
// review from a trigger's account, such as the humagen user people add as a
// collaborator.
func (c *Connector) reviewFromTrigger(kind string, body []byte) bool {
	if kind != "pull_request" {
		return false
	}
	var p struct {
		Action   string `json:"action"`
		Reviewer *struct {
			Login string `json:"login"`
		} `json:"requested_reviewer"`
	}
	if json.Unmarshal(body, &p) != nil || p.Action != "review_requested" || p.Reviewer == nil {
		return false
	}
	return c.isTrigger(strings.TrimSuffix(p.Reviewer.Login, "[bot]"))
}
