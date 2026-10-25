package httpapi

import (
	"errors"
	"net/http"
	"slices"
	"strconv"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/inbox"
)

// inboxKinds are the kinds a project's inbox can be narrowed to.
var inboxKinds = []inbox.Kind{
	inbox.KindAlert, inbox.KindFailure, inbox.KindMemory, inbox.KindBudget, inbox.KindApproval,
	inbox.KindClarification, inbox.KindResult, inbox.KindArtifact, inbox.KindMention, inbox.KindAnnouncement,
}

// projectInbox pages a project's inbox, newest first: every message filed in
// the project, and with children=true in the projects below it too. kind,
// repeated, keeps those kinds; state is open (the default) or all; limit is at
// most 200 (30 when left out) and before is the next of the page before. An
// announcement there reaches anyone who can see the project.
func (s *Server) projectInbox(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	q := r.URL.Query()
	query := inbox.Query{State: inbox.State(q.Get("state")), Before: q.Get("before"), Limit: 30}
	switch query.State {
	case "", inbox.Open, inbox.All:
	default:
		fail(w, http.StatusBadRequest, "state is open or all")
		return
	}
	for _, k := range q["kind"] {
		if !slices.Contains(inboxKinds, inbox.Kind(k)) {
			fail(w, http.StatusBadRequest, "kind is one of the inbox's kinds")
			return
		}
		query.Kinds = append(query.Kinds, inbox.Kind(k))
	}
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > inbox.MaxPage {
			fail(w, http.StatusBadRequest, "limit is a number from 1 to 200")
			return
		}
		query.Limit = n
	}
	children := false
	switch q.Get("children") {
	case "", "false":
	case "true":
		children = true
	default:
		fail(w, http.StatusBadRequest, "children is true or false")
		return
	}
	page, next, err := s.P.ProjectInbox(who, r.PathValue("name"), children, query)
	if err != nil {
		if errors.Is(err, inbox.ErrCursor) {
			fail(w, http.StatusBadRequest, "before is not a cursor this server issued")
			return
		}
		s.problem(w, err)
		return
	}
	var cursor *string
	if next != "" {
		cursor = &next
	}
	ok(w, http.StatusOK, map[string]any{"messages": page, "next": cursor})
}

// postAnnouncement posts an announcement in a project's inbox as the caller,
// who needs the role the project lets announce: owner unless it says
// otherwise. It reaches the members of the project and of every project
// below it. A project posts at most 20 a day and a workspace 200 (429).
func (s *Server) postAnnouncement(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var body struct {
		Subject  string         `json:"subject"`
		Text     string         `json:"text"`
		Severity inbox.Severity `json:"severity"`
	}
	if !decode(w, r, &body) {
		return
	}
	m, err := s.P.PostAnnouncement(who, r.PathValue("name"), platform.Announcement{Subject: body.Subject, Text: body.Text, Severity: body.Severity})
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusCreated, m)
}

// withdrawAnnouncement withdraws an announcement for everyone, as its author
// or an owner of the project.
func (s *Server) withdrawAnnouncement(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	if err := s.P.WithdrawAnnouncement(who, r.PathValue("name"), r.PathValue("id")); err != nil {
		s.problem(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
