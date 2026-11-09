package platform

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/urmzd/humagen/pkg/clients"
)

// statusWriter remembers the status a connector answered a delivery with.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

// webhookFacts are what a delivery's log line names: its event and action,
// the repository or channel, and GitHub's delivery id.
type webhookFacts struct {
	event, action, place, delivery string
}

// githubDelivery is the part of a GitHub webhook payload a log line reads.
type githubDelivery struct {
	Action     string `json:"action"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
	Sender struct {
		Type string `json:"type"`
	} `json:"sender"`
	Comment struct {
		Body string `json:"body"`
	} `json:"comment"`
	Review struct {
		Body string `json:"body"`
	} `json:"review"`
	Issue struct {
		Title string `json:"title"`
		Body  string `json:"body"`
	} `json:"issue"`
	PullRequest struct {
		Title string `json:"title"`
		Body  string `json:"body"`
	} `json:"pull_request"`
}

func (s *surface) facts(r *http.Request, body []byte) (webhookFacts, githubDelivery) {
	var f webhookFacts
	var g githubDelivery
	if s.name == string(clients.GitHub) {
		f.event, f.delivery = r.Header.Get("X-GitHub-Event"), r.Header.Get("X-GitHub-Delivery")
		_ = json.Unmarshal(body, &g)
		f.action, f.place = g.Action, g.Repository.FullName
		return f, g
	}
	var slack struct {
		Type  string `json:"type"`
		Event struct {
			Type    string `json:"type"`
			Channel string `json:"channel"`
		} `json:"event"`
	}
	_ = json.Unmarshal(body, &slack)
	f.event, f.action, f.place = slack.Type, slack.Event.Type, slack.Event.Channel
	return f, g
}

// logIgnored says, once per delivery, that one was answered without reaching
// a client's connector, and why. c is the client it reached, if any.
func (s *surface) logIgnored(r *http.Request, body []byte, c *clients.Client, status int, why string) {
	f, _ := s.facts(r, body)
	attrs := []any{"surface", s.name, "event", f.event, "action", f.action, "place", f.place, "delivery", f.delivery, "status", status, "why", why}
	if c != nil {
		attrs = append(attrs, "tenant", c.Tenant, "client", c.ID)
	}
	s.p.log.Info("platform: a webhook delivery was ignored", attrs...)
}

// logDelivery says, once per delivery a client's connector handled, whether
// it raised a request and, when it did not, the likeliest reason. A request
// it raised is followed by stamp's own lines when it is refused there.
func (s *surface) logDelivery(r *http.Request, body []byte, c clients.Client, status int, raised int64) {
	f, g := s.facts(r, body)
	attrs := []any{"surface", s.name, "tenant", c.Tenant, "client", c.ID, "event", f.event, "action", f.action, "place", f.place, "delivery", f.delivery, "status", status}
	switch {
	case status >= http.StatusBadRequest:
		s.p.log.Info("platform: a webhook delivery was refused by its connector", attrs...)
	case raised > 0:
		s.p.log.Info("platform: a webhook delivery was accepted", append(attrs, "requests", raised)...)
	default:
		why := "the connector found no request in it"
		if s.name == string(clients.GitHub) {
			why = githubIgnoredWhy(c, f.event, g)
		}
		s.p.log.Info("platform: a webhook delivery was ignored", append(attrs, "why", why)...)
	}
}

// githubIgnoredWhy is the likeliest reason the GitHub connector raised no
// request for a delivery, read from the payload: the connector itself says
// only that it raised none.
func githubIgnoredWhy(c clients.Client, event string, g githubDelivery) string {
	if g.Sender.Type == "Bot" {
		return "a bot sent it"
	}
	text := ""
	switch event {
	case "issue_comment", "pull_request_review_comment":
		text = g.Comment.Body
	case "pull_request_review":
		text = g.Review.Body
	case "issues":
		text = g.Issue.Title + "\n" + g.Issue.Body
	case "pull_request":
		if g.Action == "review_requested" {
			return "the review was requested from someone who is not one of the client's agents"
		}
		text = g.PullRequest.Title + "\n" + g.PullRequest.Body
	default:
		return "the connector does not act on " + event + " events"
	}
	handle := ""
	if c.GitHub != nil {
		handle = strings.TrimSuffix(c.GitHub.Login, "[bot]")
	}
	for _, name := range append([]string{handle, c.DefaultAgent}, mentionNames(c)...) {
		if name != "" && strings.Contains(strings.ToLower(text), "@"+strings.ToLower(name)) {
			return "it mentions @" + name + ", but the connector raised nothing: check the client's accounts (owners) and the repository's access"
		}
	}
	if handle == "" {
		return "it mentions no agent, and the client has no login (HUMAGEN_GITHUB_LOGIN) for a mention of the app to reach it"
	}
	return "it mentions neither the app (@" + handle + ") nor one of the client's agents"
}

// mentionNames are the agents and bot logins a comment may mention to reach
// a client.
func mentionNames(c clients.Client) []string {
	var names []string
	for _, a := range c.Agents {
		if a != "*" {
			names = append(names, a)
		}
	}
	for login, agent := range c.Bots {
		names = append(names, strings.TrimSuffix(login, "[bot]"), agent)
	}
	return names
}
