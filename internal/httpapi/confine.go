package httpapi

import (
	"errors"
	"net/http"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/inbox"
	"github.com/urmzd/humagen/pkg/projects"
)

// Read confinement on the routes that name one object. Every route that names
// a run, a thread, a published file or an agent asks the platform's one read
// rule (platform.Seer) before its handler runs, and an object the caller may
// not read answers exactly as one that does not exist: 404 "not found". The
// list routes filter what they return with the same rule, each in its handler.

// notFound answers as a missing object does.
func notFound(w http.ResponseWriter) { fail(w, http.StatusNotFound, "not found") }

// errNotSeen is what a handler returns for an object the caller may not read.
// It answers as notFound does.
var errNotSeen = errors.New("not found")

// seenRun guards a route under /api/runs/{id}: the run must be one the caller
// may read. A run that does not exist is left to the handler, which says so
// in the same words.
func (s *Server) seenRun(h handler) handler {
	return func(w http.ResponseWriter, r *http.Request, who auth.Principal) {
		run, err := s.P.Runs.Get(who.Tenant, r.PathValue("id"))
		if err != nil || !s.P.Seer(who).Run(run) {
			notFound(w)
			return
		}
		h(w, r, who)
	}
}

// seenThread guards a route under /api/threads/{id}: its project, and in a
// private thread taking part in it.
func (s *Server) seenThread(h handler) handler {
	return func(w http.ResponseWriter, r *http.Request, who auth.Principal) {
		id := r.PathValue("id")
		th, err := s.P.ThreadRecord(who.Tenant, id)
		if err == nil && !s.P.Seer(who).Conversation(id, th.Project) {
			notFound(w)
			return
		}
		h(w, r, who)
	}
}

// seenArtifact guards a route under /api/artifacts/{id}: where the file was
// published.
func (s *Server) seenArtifact(h handler) handler {
	return func(w http.ResponseWriter, r *http.Request, who auth.Principal) {
		a, err := s.P.Artifacts.Get(who.Tenant, r.PathValue("id"))
		if err != nil || !s.P.Seer(who).Artifact(a) {
			notFound(w)
			return
		}
		h(w, r, who)
	}
}

// seenAgent guards a route under /api/agents/{name}: an agent that lives in a
// project is read only by those who see the project.
func (s *Server) seenAgent(h handler) handler {
	return func(w http.ResponseWriter, r *http.Request, who auth.Principal) {
		if !s.P.Seer(who).Agent(r.PathValue("name")) {
			notFound(w)
			return
		}
		h(w, r, who)
	}
}

// seenAlert guards a route under /api/alerts/{id}: the run that raised the
// alert, or its project, and its agent. An alert that does not exist is left
// to the handler, which says so in the same words.
func (s *Server) seenAlert(h handler) handler {
	return func(w http.ResponseWriter, r *http.Request, who auth.Principal) {
		id := r.PathValue("id")
		for _, a := range s.P.Alerts.Alerts(who.Tenant) {
			if a.ID == id && !s.P.Seer(who).Alert(a) {
				notFound(w)
				return
			}
		}
		h(w, r, who)
	}
}

// seenRule guards a route under /api/alert-rules/{name}: the project and the
// agent the rule watches. One that does not exist is left to the handler.
func (s *Server) seenRule(h handler) handler {
	return func(w http.ResponseWriter, r *http.Request, who auth.Principal) {
		rule, err := s.P.Alerts.Rule(who.Tenant, r.PathValue("name"))
		if err == nil && !s.P.Seer(who).Rule(rule) {
			notFound(w)
			return
		}
		h(w, r, who)
	}
}

// seenProject guards a route under /api/projects/{name}: a project the caller
// has no role in reads as one that does not exist. One that does not exist is
// left to the handler.
func (s *Server) seenProject(h handler) handler {
	return func(w http.ResponseWriter, r *http.Request, who auth.Principal) {
		name := projects.NameOf(r.PathValue("name"))
		if s.P.HasProject(who.Tenant, name) && !s.P.Seer(who).Project(name) {
			notFound(w)
			return
		}
		h(w, r, who)
	}
}

// seenSuite guards a route under /api/evals/{name}: a suite that tests an
// agent the caller may not read reads as one that does not exist.
func (s *Server) seenSuite(h handler) handler {
	return func(w http.ResponseWriter, r *http.Request, who auth.Principal) {
		suite, err := s.P.Evals.Get(who.Tenant, r.PathValue("name"))
		if err == nil && !s.P.Seer(who).Suite(suite) {
			notFound(w)
			return
		}
		h(w, r, who)
	}
}

// inboxNotice is an inbox notice on the event stream: to a client's machine,
// about a message addressed to a person it maps, it says for whom.
type inboxNotice struct {
	inbox.Notice
	For *platform.ActAs `json:"for,omitempty"`
}

// runGate rechecks the current audience for every event in a subscription.
type runGate struct {
	p   *platform.Platform
	who auth.Principal
}

func newRunGate(p *platform.Platform, who auth.Principal) *runGate {
	return &runGate{p: p, who: who}
}

func (g *runGate) seen(run string) bool {
	// Placement and private participants can change during a subscription. An
	// earlier verdict must never authorize an event after that audience changes.
	return g.p.Seer(g.who).RunID(run)
}
