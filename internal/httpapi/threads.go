package httpapi

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/urmzd/warp/internal/platform"
	"github.com/urmzd/warp/pkg/auth"
	"github.com/urmzd/warp/pkg/projects"
	"github.com/urmzd/warp/pkg/threads"
)

// threadRoutes are shared threads and the channels they live in. A thread's
// routes take runs:*, as conversations do; a project's channels and experts
// take projects:*, as the rest of the project does. Each then holds the
// caller's role on the project: seeing it reads its channels and experts,
// owning it changes them, and posting in a thread takes a member.
func (s *Server) threadRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/projects/{name}/channels", s.authed(auth.ProjectsRead, s.listChannels))
	mux.HandleFunc("POST /api/projects/{name}/channels", s.authed(auth.ProjectsWrite, s.createChannel))
	mux.HandleFunc("PUT /api/projects/{name}/channels/{channel}", s.authed(auth.ProjectsWrite, s.updateChannel))
	mux.HandleFunc("DELETE /api/projects/{name}/channels/{channel}", s.authed(auth.ProjectsWrite, s.archiveChannel))
	mux.HandleFunc("GET /api/projects/{name}/experts", s.authed(auth.ProjectsRead, s.getExperts))
	mux.HandleFunc("PUT /api/projects/{name}/experts", s.authed(auth.ProjectsWrite, s.putExperts))
	mux.HandleFunc("POST /api/threads", s.authed(auth.RunsWrite, s.startThread))
	mux.HandleFunc("PUT /api/threads/{id}", s.authed(auth.RunsWrite, s.updateThread))
	mux.HandleFunc("POST /api/threads/{id}/messages", s.authed(auth.RunsWrite, s.postMessage))
	mux.HandleFunc("PUT /api/threads/{id}/participants", s.authed(auth.RunsWrite, s.putParticipants))
}

// answers is who answers an untagged message in a place, and where that is
// set. Unavailable says why it cannot, when it has stopped being available.
type answers struct {
	Agent       string `json:"agent"`
	From        string `json:"from"`
	Unavailable string `json:"unavailable,omitempty"`
}

// channelView is a channel and who answers untagged messages in it.
type channelView struct {
	projects.Channel
	Answers answers `json:"answers"`
}

func (s *Server) channelView(tenant string, c projects.Channel) channelView {
	d, why, _ := s.P.DefaultFor(tenant, "", c.Project, c.Name)
	return channelView{Channel: c, Answers: answers{Agent: d.Agent, From: d.From, Unavailable: why}}
}

func (s *Server) listChannels(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	pr, _, found := s.visible(w, who, r.PathValue("name"), projects.Viewer)
	if !found {
		return
	}
	name := pr.Name
	list, err := s.P.Channels(who.Tenant, name)
	if err != nil {
		s.problem(w, err)
		return
	}
	out := make([]channelView, 0, len(list))
	for _, c := range list {
		out = append(out, s.channelView(who.Tenant, c))
	}
	ok(w, http.StatusOK, map[string]any{"channels": out})
}

func (s *Server) createChannel(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var in platform.ChannelInput
	if !decode(w, r, &in) {
		return
	}
	pr, _, found := s.visible(w, who, r.PathValue("name"), projects.Owner)
	if !found {
		return
	}
	name := pr.Name
	c, err := s.P.CreateChannel(r.Context(), who.Tenant, name, writer(who), in)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusCreated, s.channelView(who.Tenant, c))
}

func (s *Server) updateChannel(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var in platform.ChannelUpdate
	if !decode(w, r, &in) {
		return
	}
	pr, _, found := s.visible(w, who, r.PathValue("name"), projects.Owner)
	if !found {
		return
	}
	name := pr.Name
	c, err := s.P.UpdateChannel(r.Context(), who.Tenant, name, r.PathValue("channel"), writer(who), in)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, s.channelView(who.Tenant, c))
}

// archiveChannel archives a channel. It and its threads stay, and general
// is never archived.
func (s *Server) archiveChannel(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	pr, _, found := s.visible(w, who, r.PathValue("name"), projects.Owner)
	if !found {
		return
	}
	name := pr.Name
	if _, err := s.P.ArchiveChannel(who.Tenant, name, r.PathValue("channel"), writer(who)); err != nil {
		s.problem(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) getExperts(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	pr, _, found := s.visible(w, who, r.PathValue("name"), projects.Viewer)
	if !found {
		return
	}
	name := pr.Name
	got, err := s.P.ProjectExperts(who.Tenant, name)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, got)
}

func (s *Server) putExperts(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var in platform.Experts
	if !decode(w, r, &in) {
		return
	}
	pr, _, found := s.visible(w, who, r.PathValue("name"), projects.Owner)
	if !found {
		return
	}
	name := pr.Name
	if in.Experts == nil {
		fail(w, http.StatusBadRequest, "experts is a list, [] for none")
		return
	}
	got, err := s.P.SetProjectExperts(who.Tenant, name, writer(who), in)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, got)
}

// startThread starts a shared thread with its first message. The person
// posting is the caller, or the person a surface's credential acts for: no
// field names an author. Every thread route names people by handle
// (platform.ShowThread), never by a subject, which can hold an address.
func (s *Server) startThread(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var in platform.ThreadInput
	if !decode(w, r, &in) {
		return
	}
	if in.Visibility == "" {
		fail(w, http.StatusBadRequest, "visibility is required: shared")
		return
	}
	got, err := s.P.StartThread(r.Context(), who, in)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusCreated, s.P.ShowPosted(who.Tenant, got))
}

func (s *Server) postMessage(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var in platform.MessageInput
	if !decode(w, r, &in) {
		return
	}
	got, err := s.P.PostMessage(r.Context(), who, r.PathValue("id"), in)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusCreated, s.P.ShowPosted(who.Tenant, got))
}

func (s *Server) updateThread(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var in platform.ThreadUpdate
	if !decode(w, r, &in) {
		return
	}
	got, err := s.P.UpdateThread(r.Context(), who, r.PathValue("id"), in)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, s.P.ShowThread(who.Tenant, got))
}

func (s *Server) putParticipants(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var in platform.ParticipantsUpdate
	if !decode(w, r, &in) {
		return
	}
	got, err := s.P.SetParticipants(r.Context(), who, r.PathValue("id"), in)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, s.P.ShowThread(who.Tenant, got))
}

// getThread reads a thread: its record, who answers untagged messages in
// it, and one page of its turns, its people's messages and its agents' runs
// in the order they were made. The first page is the newest; next is the
// cursor for the turns before it, null at the start.
func (s *Server) getThread(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	q := r.URL.Query()
	limit := 0
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > platform.MaxTurnPage {
			fail(w, http.StatusBadRequest, "limit is a number from 1 to 200")
			return
		}
		limit = n
	}
	id := r.PathValue("id")
	th, turns, next, err := s.P.ThreadTurns(who.Tenant, id, q.Get("before"), limit)
	if err != nil {
		s.problem(w, err)
		return
	}
	d, why, _ := s.P.DefaultFor(who.Tenant, id, "", "")
	th, turns = s.P.ShowTurns(who.Tenant, th, turns)
	var cursor *string
	if next != "" {
		cursor = &next
	}
	ok(w, http.StatusOK, map[string]any{
		"thread": th, "turns": turns, "next": cursor,
		"answers": answers{Agent: d.Agent, From: d.From, Unavailable: why},
	})
}

// threadProblem answers the errors of threads, and reports whether err was
// one.
func threadProblem(w http.ResponseWriter, err error) bool {
	switch {
	case errors.Is(err, threads.ErrNotFound):
		fail(w, http.StatusNotFound, "not found")
	case errors.Is(err, threads.ErrExists):
		fail(w, http.StatusConflict, "already exists")
	case errors.Is(err, threads.ErrFull):
		fail(w, http.StatusConflict, "this thread is full; start a new thread")
	case errors.Is(err, threads.ErrPrivate):
		fail(w, http.StatusConflict, "private threads need read confinement, not built")
	case errors.Is(err, threads.ErrVersion):
		fail(w, http.StatusConflict, detail(err, threads.ErrVersion)+"; reload and try again")
	case errors.Is(err, threads.ErrInvalid):
		fail(w, http.StatusBadRequest, detail(err, threads.ErrInvalid))
	case errors.Is(err, threads.ErrCursor), errors.Is(err, platform.ErrTurnCursor):
		fail(w, http.StatusBadRequest, "before is not a cursor this server issued")
	case errors.Is(err, platform.ErrThreadAuthor):
		fail(w, http.StatusForbidden, detail(err, platform.ErrThreadAuthor))
	case errors.Is(err, platform.ErrAnswered):
		fail(w, http.StatusConflict, detail(err, platform.ErrAnswered))
	case errors.Is(err, threads.ErrLimit):
		fail(w, http.StatusTooManyRequests, detail(err, threads.ErrLimit))
	case errors.Is(err, platform.ErrPostRate):
		fail(w, http.StatusTooManyRequests, detail(err, platform.ErrPostRate))
	default:
		return false
	}
	return true
}
