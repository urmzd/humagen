package httpapi

import (
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
