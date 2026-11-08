package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
)

// messageRoutes are the one way in for every surface: people's messages from
// an origin, under the root session of its place.
func (s *Server) messageRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/messages", s.authed(auth.RunsWrite, s.sendMessages))
	mux.HandleFunc("POST /api/messages/{id}", s.authed(auth.RunsWrite, s.seenMessage(s.sendMessagesUnder)))
	mux.HandleFunc("GET /api/messages/{id}", s.authed(auth.RunsRead, s.seenMessage(s.getMessage)))
	mux.HandleFunc("GET /api/messages/{id}/stream", s.authed(auth.RunsRead, s.seenMessage(s.messageEvents)))
	mux.HandleFunc("POST /api/messages/{id}/feedback", s.authed(auth.RunsRead, s.seenMessage(s.messageFeedback)))
	mux.HandleFunc("POST /api/messages/{id}/stop", s.authed(auth.RunsWrite, s.seenMessage(s.stopMessage)))
	mux.HandleFunc("POST /api/messages/{id}/context", s.authed(auth.RunsRead, s.seenMessage(s.messageContext)))
}

// seenMessage answers 404 for a message whose session the caller cannot
// read, and hands the next handler the session's id as {id}.
func (s *Server) seenMessage(next handler) handler {
	return func(w http.ResponseWriter, r *http.Request, who auth.Principal) {
		session, _ := platform.SplitMessageID(r.PathValue("id"))
		if !s.P.SeesSession(who, session) {
			notFound(w)
			return
		}
		next(w, r, who)
	}
}

func (s *Server) sendMessages(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var in platform.MessagesInput
	if !decode(w, r, &in) {
		return
	}
	out, err := s.P.SendMessages(r.Context(), who, in)
	if err != nil {
		s.problem(w, err)
		return
	}
	status := http.StatusOK
	if out.Created {
		status = http.StatusCreated
	}
	ok(w, status, out)
}

func (s *Server) sendMessagesUnder(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var in platform.MessagesInput
	if !decode(w, r, &in) {
		return
	}
	out, err := s.P.SendMessagesUnder(r.Context(), who, r.PathValue("id"), in)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, out)
}

func (s *Server) getMessage(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	session, _ := platform.SplitMessageID(r.PathValue("id"))
	r.SetPathValue("id", session)
	s.getSession(w, r, who)
}

func (s *Server) messageEvents(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	session, _ := platform.SplitMessageID(r.PathValue("id"))
	r.SetPathValue("id", session)
	s.sessionEvents(w, r, who)
}

// messageFeedback rates an agent's message (id is session:run): one rating
// per person per message, saved again in place. It feeds evaluations and
// what routing learns an agent is good at.
func (s *Server) messageFeedback(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var in struct {
		Rating string `json:"rating"`
		Note   string `json:"note,omitempty"`
	}
	if !decode(w, r, &in) {
		return
	}
	session, run := platform.SplitMessageID(r.PathValue("id"))
	if run == "" {
		fail(w, http.StatusBadRequest, "feedback is on an agent's message: session:run")
		return
	}
	r.SetPathValue("id", session)
	r.SetPathValue("mid", run)
	if !s.sessionFeedbackRun(w, r, who) {
		return
	}
	body, _ := json.Marshal(map[string]string{"rating": in.Rating, "comment": in.Note})
	r.Body = io.NopCloser(bytes.NewReader(body))
	r.ContentLength = int64(len(body))
	s.putFeedback(w, r, who)
}

// stopMessage stops the work beneath a message's session, and with children
// the work of its child sessions too.
func (s *Server) stopMessage(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	session, _ := platform.SplitMessageID(r.PathValue("id"))
	r.SetPathValue("id", session)
	s.stopSession(w, r, who)
}

// messageContext pulls context from the place a message's session came from.
func (s *Server) messageContext(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var in struct {
		Request string `json:"request"`
	}
	if !decode(w, r, &in) {
		return
	}
	out, err := s.P.GetContext(r.Context(), who, r.PathValue("id"), in.Request)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, out)
}
