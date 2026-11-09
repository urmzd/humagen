package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

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
	mux.HandleFunc("POST /api/messages/{id}/feedback", s.authed(auth.RunsRead, s.seenMessage(s.messageFeedback)))
	mux.HandleFunc("POST /api/messages/{id}/stop", s.authed(auth.RunsWrite, s.seenMessage(s.stopMessage)))
	mux.HandleFunc("POST /api/messages/{id}/answer", s.authed(auth.RunsWrite, s.seenMessage(s.answerToolUse)))
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

// answerToolUse answers what waits on a person at one tool call of the
// conversation, named by its tool_use_id: approving or denying the call, or
// answering the question it asked. It is the same as answering the question
// on its run, and returns the question's message.
func (s *Server) answerToolUse(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var body struct {
		ToolUseID string `json:"tool_use_id"`
		Answer    *struct {
			Action string `json:"action"`
			Note   string `json:"note"`
			answerBody
		} `json:"answer"`
	}
	if !decode(w, r, &body) {
		return
	}
	if strings.TrimSpace(body.ToolUseID) == "" || body.Answer == nil {
		fail(w, http.StatusBadRequest, "an answer names the tool call it answers (tool_use_id) and says what (answer)")
		return
	}
	if !body.Answer.valid(w, body.Answer.Note) {
		return
	}
	a := body.Answer
	m, err := s.P.AnswerToolUse(r.Context(), who, r.PathValue("id"), body.ToolUseID, platform.QuestionAnswer{
		Action: a.Action, Arguments: a.Arguments, Choice: a.Choice, Text: a.Text, Note: a.Note, ActAs: a.ActAs,
	})
	s.inboxAnswer(w, m, err)
}
