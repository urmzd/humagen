package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/urmzd/warp/internal/platform"
	"github.com/urmzd/warp/pkg/auth"
	"github.com/urmzd/warp/pkg/inbox"
	"github.com/urmzd/warp/pkg/projects"
)

// The inbox routes ask only for runs:read, which every role has. What a caller
// sees in the inbox, and what they may do with each part, is decided per
// message by the platform: resolving a part needs the right for the thing it
// does, not the right to open an inbox.
func (s *Server) inboxRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/inbox", s.authed(auth.RunsRead, s.listInbox))
	mux.HandleFunc("GET /api/inbox/count", s.authed(auth.RunsRead, s.countInbox))
	mux.HandleFunc("GET /api/inbox/{id}", s.authed(auth.RunsRead, s.getInbox))
	mux.HandleFunc("POST /api/inbox/{id}/read", s.authed(auth.RunsRead, s.readInbox))
	mux.HandleFunc("POST /api/inbox/{id}/dismiss", s.authed(auth.RunsRead, s.dismissInbox))
	mux.HandleFunc("POST /api/inbox/{id}/snooze", s.authed(auth.RunsRead, s.snoozeInbox))
	mux.HandleFunc("POST /api/inbox/{id}/unhide", s.authed(auth.RunsRead, s.unhideInbox))
	mux.HandleFunc("POST /api/inbox/{id}/parts/{part}/resolve", s.authed(auth.RunsRead, s.resolveInbox))
	// A client's machine records where it posted a message. The platform
	// answers 404 to anyone else.
	mux.HandleFunc("PUT /api/inbox/{id}/deliveries/{client}", s.authed(auth.RunsWrite, s.deliverInbox))
}

// deliveryBody is where a client posted a message: the Slack channel and the
// ts of the post.
type deliveryBody struct {
	Channel string `json:"channel"`
	TS      string `json:"ts"`
}

// deliverInbox records where a client posted a message.
func (s *Server) deliverInbox(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var body deliveryBody
	if !decode(w, r, &body) {
		return
	}
	if err := s.P.RecordDelivery(who, r.PathValue("id"), r.PathValue("client"), inbox.Delivery{Channel: body.Channel, TS: body.TS}); err != nil {
		s.problem(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// actingFor is who a read is for: the caller, or the person a client's
// machine reads for (act_as_client and act_as_user), who must hold the
// route's permission themselves. what names the read in the refusal.
func (s *Server) actingFor(w http.ResponseWriter, r *http.Request, who auth.Principal, need auth.Permission, what string) (auth.Principal, bool) {
	q := r.URL.Query()
	client, user := q.Get("act_as_client"), q.Get("act_as_user")
	if client == "" && user == "" {
		return who, true
	}
	if client == "" || user == "" {
		fail(w, http.StatusBadRequest, "act_as_client and act_as_user go together")
		return auth.Principal{}, false
	}
	person, err := s.P.ActingFor(who, platform.ActAs{Client: client, User: user})
	if err != nil {
		s.problem(w, err)
		return auth.Principal{}, false
	}
	if !person.Can(need) {
		fail(w, http.StatusForbidden, "that person cannot read "+what)
		return auth.Principal{}, false
	}
	return person, true
}

// readingInbox is who an inbox read is for: the caller, or a person of a
// client, who must hold runs:read.
func (s *Server) readingInbox(w http.ResponseWriter, r *http.Request, who auth.Principal) (auth.Principal, bool) {
	return s.actingFor(w, r, who, auth.RunsRead, "an inbox")
}

// listInbox pages the caller's inbox, newest first.
//
// state is open (the default), resolved, dismissed, snoozed or all. kind,
// unread, needs, news, alert, agent, project, limit and before are optional.
// needs keeps what open counts: every message but a posted announcement, an
// agent's draft included. news keeps only posted announcements, what updates
// counts. alert finds the message about one alert, in any
// state. agent keeps the messages an agent
// raised or whose call would reach it: with kind=approval, that agent's
// approval queue. project keeps what is filed in one project, "~" for the
// workspace, of what the caller's own inbox holds. The answer carries next,
// the cursor for the page after this one, or null at the end.
func (s *Server) listInbox(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	who, acting := s.readingInbox(w, r, who)
	if !acting {
		return
	}
	q := r.URL.Query()
	query := inbox.Query{State: inbox.State(q.Get("state")), Kind: inbox.Kind(q.Get("kind")), Before: q.Get("before"), Unread: q.Get("unread") == "true", Needs: q.Get("needs") == "true", News: q.Get("news") == "true", Alert: q.Get("alert"), Agent: q.Get("agent")}
	// Asking after one alert means its message, wherever it stands.
	if query.Alert != "" && query.State == "" {
		query.State = inbox.All
	}
	switch query.State {
	case "", inbox.Open, inbox.Resolved, inbox.Dismissed, inbox.Snoozed, inbox.All:
	default:
		fail(w, http.StatusBadRequest, "state is open, resolved, dismissed, snoozed or all")
		return
	}
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			fail(w, http.StatusBadRequest, "limit is a number above zero")
			return
		}
		query.Limit = n
	}
	if q.Has("project") {
		pr, _, err := s.P.ProjectRole(who, q.Get("project"), projects.Viewer)
		if err != nil {
			s.problem(w, err)
			return
		}
		query.Projects = []string{pr.Name}
	}
	page, next, err := s.P.InboxPage(who, query)
	if err != nil {
		fail(w, http.StatusBadRequest, "before is not a cursor this server issued")
		return
	}
	var cursor *string
	if next != "" {
		cursor = &next
	}
	c := s.P.InboxCount(who)
	ok(w, http.StatusOK, map[string]any{"messages": page, "next": cursor, "open": c.Open, "unread": c.Unread, "updates": c.Updates})
}

// countInbox counts the caller's inbox. open and unread are what may need
// them; updates are the announcements they have not read, which never grow
// the badge.
func (s *Server) countInbox(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	who, acting := s.readingInbox(w, r, who)
	if !acting {
		return
	}
	ok(w, http.StatusOK, s.P.InboxCount(who))
}

func (s *Server) getInbox(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	who, acting := s.readingInbox(w, r, who)
	if !acting {
		return
	}
	m, err := s.P.InboxGet(who, r.PathValue("id"))
	if err != nil {
		s.problem(w, err)
		return
	}
	// Each reader is shown what changed the message for everyone and their
	// own marks, never another reader's, nor another client's receipts.
	ok(w, http.StatusOK, map[string]any{"message": m, "history": s.P.InboxHistory(who, m.ID)})
}

func (s *Server) readInbox(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	body := struct {
		Read *bool `json:"read"`
	}{}
	if !decode(w, r, &body) {
		return
	}
	m, err := s.P.InboxRead(who, r.PathValue("id"), body.Read == nil || *body.Read)
	s.inboxAnswer(w, m, err)
}

func (s *Server) dismissInbox(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	m, err := s.P.InboxDismiss(who, r.PathValue("id"))
	s.inboxAnswer(w, m, err)
}

// unhideInbox brings back an announcement the caller hid or snoozed for
// themselves.
func (s *Server) unhideInbox(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	m, err := s.P.InboxUnhide(who, r.PathValue("id"))
	s.inboxAnswer(w, m, err)
}

func (s *Server) snoozeInbox(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var body struct {
		Until time.Time `json:"until"`
	}
	if !decode(w, r, &body) {
		return
	}
	m, err := s.P.InboxSnooze(who, r.PathValue("id"), body.Until)
	s.inboxAnswer(w, m, err)
}

func (s *Server) resolveInbox(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var body struct {
		Action string       `json:"action"`
		IDs    []string     `json:"ids"`
		Note   string       `json:"note"`
		Limits inbox.Limits `json:"limits"`
		answerBody
	}
	if !decode(w, r, &body) {
		return
	}
	if !body.valid(w, body.Note) {
		return
	}
	m, err := s.P.InboxResolve(r.Context(), who, r.PathValue("id"), r.PathValue("part"),
		platform.Resolution{
			Action: body.Action, IDs: body.IDs, Note: body.Note, Limits: body.Limits,
			Arguments: body.Arguments, Choice: body.Choice, Text: body.Text, ActAs: body.ActAs,
		})
	s.inboxAnswer(w, m, err)
}

// answerBody is what answers a question, besides the action and a note.
type answerBody struct {
	// Arguments replace the model's on an approval, as a JSON object that
	// satisfies the tool's schema.
	Arguments json.RawMessage `json:"arguments"`
	Choice    string          `json:"choice"`
	Text      string          `json:"text"`
	// ActAs is the person a surface answers for.
	ActAs *platform.ActAs `json:"act_as"`
}

// Bounds on an answer. The runtime clips what it records further.
const (
	maxNote      = 500
	maxReply     = 4000
	maxArguments = 64 << 10
)

func (b answerBody) valid(w http.ResponseWriter, note string) bool {
	switch {
	case len(note) > maxNote:
		fail(w, http.StatusBadRequest, "a note is at most 500 characters")
	case len(b.Text) > maxReply || len(b.Choice) > maxReply:
		fail(w, http.StatusBadRequest, "an answer is at most 4000 characters")
	case len(b.Arguments) > maxArguments:
		fail(w, http.StatusBadRequest, "edited arguments are at most 64 KB")
	default:
		return true
	}
	return false
}

// answerQuestion answers the question a run is parked on, as the inbox's
// resolve does for its message. It is how a surface showing the run answers
// it in place.
func (s *Server) answerQuestion(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var body struct {
		Action string `json:"action"`
		Note   string `json:"note"`
		answerBody
	}
	if !decode(w, r, &body) {
		return
	}
	if !body.valid(w, body.Note) {
		return
	}
	m, err := s.P.AnswerQuestion(r.Context(), who, r.PathValue("id"), r.PathValue("question"), platform.QuestionAnswer{
		Action: body.Action, Arguments: body.Arguments, Choice: body.Choice, Text: body.Text, Note: body.Note, ActAs: body.ActAs,
	})
	s.inboxAnswer(w, m, err)
}

func (s *Server) inboxAnswer(w http.ResponseWriter, m inbox.Message, err error) {
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, m)
}
