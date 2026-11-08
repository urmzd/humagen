package httpapi

import (
	"net/http"
	"strconv"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
)

// messageRoutes are the one way in for every surface: people's messages from
// an origin, under the root session of its place.
func (s *Server) messageRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/messages", s.authed(auth.RunsWrite, s.sendMessages))
	mux.HandleFunc("GET /api/messages", s.authed(auth.RunsRead, s.listMessages))
	mux.HandleFunc("POST /api/messages/{id}", s.authed(auth.RunsWrite, s.seenMessage(s.sendMessagesUnder)))
	mux.HandleFunc("GET /api/messages/{id}", s.authed(auth.RunsRead, s.seenMessage(s.getMessage)))
	mux.HandleFunc("GET /api/messages/{id}/stream", s.authed(auth.RunsRead, s.seenMessage(s.messageEvents)))
	mux.HandleFunc("GET /api/messages/{id}/trace", s.authed(auth.RunsRead, s.seenMessage(s.messageTrace)))
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

// listMessages is the roots the caller may read, newest first. roots=true
// is the one listing it answers.
func (s *Server) listMessages(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	q := r.URL.Query()
	if q.Get("roots") != "true" {
		fail(w, http.StatusBadRequest, "roots=true is the one listing of messages")
		return
	}
	limit := 50
	if q.Has("limit") {
		n, err := strconv.Atoi(q.Get("limit"))
		if err != nil {
			fail(w, http.StatusBadRequest, "limit is a number")
			return
		}
		limit = n
	}
	page, err := s.P.ListRootMessages(r.Context(), who, limit, q.Get("after"))
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, page)
}

// getMessage reads a message as a tree with depth, else its session.
func (s *Server) getMessage(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	q := r.URL.Query()
	if q.Has("depth") {
		if q.Has("since") || q.Has("before") || q.Has("around") || q.Has("summary") {
			fail(w, http.StatusBadRequest, "depth reads a tree and takes no since, before, around or summary")
			return
		}
		depth := 1
		if raw := q.Get("depth"); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil {
				fail(w, http.StatusBadRequest, "depth is a number")
				return
			}
			depth = n
		}
		node, err := s.P.ReadMessage(r.Context(), who, r.PathValue("id"), depth)
		if err != nil {
			s.problem(w, err)
			return
		}
		ok(w, http.StatusOK, node)
		return
	}
	session, _ := platform.SplitMessageID(r.PathValue("id"))
	r.SetPathValue("id", session)
	s.getSession(w, r, who)
}

func (s *Server) messageEvents(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	session, _ := platform.SplitMessageID(r.PathValue("id"))
	r.SetPathValue("id", session)
	s.sessionEvents(w, r, who)
}

// messageTrace is what ran to answer a message, one node per run.
func (s *Server) messageTrace(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	out, err := s.P.MessageTrace(r.Context(), who, r.PathValue("id"))
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, out)
}
