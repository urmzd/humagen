package httpapi

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/threads"
	"github.com/urmzd/orchagent/flow/event"
)

func (s *Server) sessionRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/sessions", s.authed(auth.RunsWrite, s.startSession))
	mux.HandleFunc("GET /api/sessions", s.authed(auth.RunsRead, s.listSessions))
	mux.HandleFunc("GET /api/sessions/{id}", s.authed(auth.RunsRead, s.seenSession(s.getSession)))
	mux.HandleFunc("POST /api/sessions/{id}/messages", s.authed(auth.RunsWrite, s.seenSession(s.postSession)))
	mux.HandleFunc("GET /api/sessions/{id}/stream", s.authed(auth.RunsRead, s.seenSession(s.sessionEvents)))
	mux.HandleFunc("POST /api/sessions/{id}/stop", s.authed(auth.RunsWrite, s.seenSession(s.stopSession)))
	mux.HandleFunc("POST /api/sessions/{id}/people", s.authed(auth.RunsWrite, s.seenSession(s.inviteSession)))
	mux.HandleFunc("POST /api/sessions/{id}/links", s.authed(auth.RunsWrite, s.seenSession(s.linkSession)))
	mux.HandleFunc("PUT /api/sessions/{id}/messages/{mid}/feedback", s.authed(auth.RunsRead, s.seenSession(s.sessionFeedback)))
	mux.HandleFunc("DELETE /api/sessions/{id}/messages/{mid}/feedback", s.authed(auth.RunsRead, s.seenSession(s.deleteSessionFeedback)))
	mux.HandleFunc("POST /api/sessions/{id}/alerts", s.authed(auth.AlertsWrite, s.seenSession(s.createSessionAlert)))
	mux.HandleFunc("PATCH /api/alerts/{id}", s.authed(auth.AlertsWrite, s.seenAlert(s.patchAlert)))
}
func (s *Server) seenSession(next handler) handler {
	return func(w http.ResponseWriter, r *http.Request, who auth.Principal) {
		if !s.P.SeesSession(who, r.PathValue("id")) {
			notFound(w)
			return
		}
		next(w, r, who)
	}
}
func (s *Server) startSession(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var in platform.SessionInput
	if !decode(w, r, &in) {
		return
	}
	v, err := s.P.StartSession(r.Context(), who, in)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusCreated, v)
}
func (s *Server) listSessions(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	q := r.URL.Query()
	limit := 50
	if q.Has("limit") {
		n, err := strconv.Atoi(q.Get("limit"))
		if err != nil || n < 1 || n > 200 {
			fail(w, 400, "limit is between 1 and 200")
			return
		}
		limit = n
	}
	all, err := s.P.ListSessions(r.Context(), who, q.Get("parent"), q.Get("agent"), q.Get("project"), q.Get("status"))
	if err != nil {
		s.problem(w, err)
		return
	}
	if q.Has("roots") && q.Get("roots") != "true" && q.Get("roots") != "false" {
		fail(w, 400, "roots is true or false")
		return
	}
	if q.Has("mine") && q.Get("mine") != "true" && q.Get("mine") != "false" {
		fail(w, 400, "mine is true or false")
		return
	}
	filtered := all[:0]
	for _, v := range all {
		if q.Get("roots") == "true" && strings.HasPrefix(v.Parent, "s_") {
			continue
		}
		if q.Get("mine") == "true" {
			th, err := s.P.SessionRecord(r.Context(), who, v.ID)
			if err != nil {
				continue
			}
			mine := th.CreatedBy == who.Subject
			for _, person := range th.Participants {
				if person.Kind == threads.Person && person.Name == who.Subject {
					mine = true
				}
			}
			if !mine {
				continue
			}
		}
		filtered = append(filtered, v)
	}
	all = filtered
	start := 0
	if after := q.Get("after"); after != "" {
		found := false
		for i, v := range all {
			if v.ID == after {
				start = i + 1
				found = true
				break
			}
		}
		if !found {
			fail(w, 400, "after is not a session cursor in this listing")
			return
		}
	}
	end := min(start+limit, len(all))
	next := ""
	if end < len(all) {
		next = all[end-1].ID
	}
	ok(w, 200, map[string]any{"sessions": all[start:end], "next": next})
}
func (s *Server) getSession(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	q := r.URL.Query()
	if q.Has("summary") && q.Get("summary") != "true" && q.Get("summary") != "false" {
		fail(w, 400, "summary is true or false")
		return
	}
	v, err := s.P.ReadSession(r.Context(), who, r.PathValue("id"), q.Get("since"), q.Get("before"), q.Get("summary") == "true", q.Get("around"))
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, 200, v)
}
func (s *Server) postSession(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var in platform.SessionPostInput
	if !decode(w, r, &in) {
		return
	}
	v, err := s.P.PostSession(r.Context(), who, r.PathValue("id"), in.MessageInput, in.SessionMessageOptions)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, 201, v)
}
func (s *Server) stopSession(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var in struct {
		Children bool `json:"children"`
	}
	if !decode(w, r, &in) {
		return
	}
	if err := s.P.StopSession(r.Context(), who, r.PathValue("id"), in.Children); err != nil {
		s.problem(w, err)
		return
	}
	w.WriteHeader(204)
}
func (s *Server) inviteSession(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var in struct {
		Person string `json:"person"`
	}
	if !decode(w, r, &in) {
		return
	}
	th, err := s.P.SessionRecord(r.Context(), who, r.PathValue("id"))
	if err != nil {
		s.problem(w, err)
		return
	}
	_, err = s.P.SetParticipants(r.Context(), who, th.ID, platform.ParticipantsUpdate{Version: th.Version, Add: []threads.Participant{{Kind: threads.Person, Name: in.Person}}})
	if err != nil {
		s.problem(w, err)
		return
	}
	v, err := s.P.ReadSession(r.Context(), who, th.ID, "", "", false)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, 200, v)
}
func (s *Server) linkSession(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var in struct {
		Target string `json:"target"`
		Kind   string `json:"kind"`
	}
	if !decode(w, r, &in) {
		return
	}
	if err := s.P.LinkSession(r.Context(), who, r.PathValue("id"), in.Target, in.Kind); err != nil {
		s.problem(w, err)
		return
	}
	w.WriteHeader(204)
}
func (s *Server) sessionFeedback(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	if !s.sessionFeedbackRun(w, r, who) {
		return
	}
	s.putFeedback(w, r, who)
}
func (s *Server) deleteSessionFeedback(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	if !s.sessionFeedbackRun(w, r, who) {
		return
	}
	s.deleteFeedback(w, r, who)
}
func (s *Server) sessionFeedbackRun(w http.ResponseWriter, r *http.Request, who auth.Principal) bool {
	run, err := s.P.Runs.Get(who.Tenant, r.PathValue("mid"))
	if err != nil || run.Thread != r.PathValue("id") || !s.P.Seer(who).Run(run) {
		notFound(w)
		return false
	}
	r.SetPathValue("id", run.ID)
	return true
}

// A stream cursor tracks every run independently. Sequence numbers are per run,
// so a single numeric cursor would lose events when a child starts at sequence 1.
type sessionStreamCursor struct {
	Runs     map[string]uint64 `json:"r"`
	Snapshot string            `json:"s,omitempty"`
	Retired  time.Time         `json:"t,omitzero"`
	Before   time.Time         `json:"b,omitzero"`
}

func (s *Server) sessionEvents(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	cur := sessionStreamCursor{Runs: map[string]uint64{}}
	if raw := r.Header.Get("Last-Event-ID"); raw != "" {
		b, err := base64.RawURLEncoding.DecodeString(raw)
		if err != nil || len(b) > 32768 || json.Unmarshal(b, &cur) != nil || len(cur.Runs) > 200 {
			fail(w, 400, "Last-Event-ID is not a session cursor")
			return
		}
		if cur.Runs == nil {
			cur.Runs = map[string]uint64{}
		}
	}
	id := r.PathValue("id")
	stream, err := s.P.Subscribe(r.Context(), who.Tenant, platform.EventFilter{})
	if err != nil {
		s.problem(w, err)
		return
	}
	defer func() { _ = stream.Close() }()
	posts, unwatch := s.P.WatchThreads(who.Tenant)
	defer unwatch()
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Time{})
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	emit := func(kind string, v any) error {
		data, err := json.Marshal(v)
		if err != nil {
			return err
		}
		cursor, _ := json.Marshal(cur)
		_, err = fmt.Fprintf(w, "id: %s\nevent: %s\ndata: %s\n\n", base64.RawURLEncoding.EncodeToString(cursor), kind, data)
		if err != nil {
			return err
		}
		return rc.Flush()
	}
	snapshot := func(kind string, force bool) error {
		v, err := s.P.ReadSession(r.Context(), who, id, "", "", false)
		if err != nil {
			return err
		}
		b, _ := json.Marshal(v)
		h := sha256.Sum256(b)
		hash := base64.RawURLEncoding.EncodeToString(h[:])
		if hash == cur.Snapshot && !force {
			return nil
		}
		cur.Snapshot = hash
		return emit(kind, v)
	}
	if err = snapshot("session", false); err != nil {
		return
	}
	if err = rc.Flush(); err != nil {
		return
	}
	events := make(chan event.Envelope)
	go func() {
		defer close(events)
		for {
			e, err := stream.Recv(r.Context())
			if err != nil {
				return
			}
			select {
			case events <- e:
			case <-r.Context().Done():
				return
			}
		}
	}()
	tick := time.NewTicker(heartbeat)
	defer tick.Stop()
	gate := newRunGate(r.Context(), s.P, who)
	for {
		select {
		case <-r.Context().Done():
			return
		case <-tick.C:
			if !s.P.SeesSession(who, id) {
				return
			}
			if _, err = fmt.Fprint(w, ": keep-alive\n\n"); err != nil {
				return
			}
			if err = rc.Flush(); err != nil {
				return
			}
		case _, ok := <-posts:
			if !ok {
				return
			}
			if err = snapshot("session", false); err != nil {
				return
			}
		case e, ok := <-events:
			if !ok {
				return
			}
			if !gate.seen(e.Run) {
				continue
			}
			run, err := s.P.Runs.Get(who.Tenant, e.Run)
			if err != nil {
				// This replica may authorize an in-flight admission before
				// its durable projection exists. Preserve its first event too.
				if !s.P.AwaitRunRecord(r.Context(), who.Tenant, e.Run, runRecordWait) {
					continue
				}
				run, err = s.P.Runs.Get(who.Tenant, e.Run)
				if err != nil || !gate.seen(e.Run) {
					continue
				}
			}
			belongs := run.Thread == id
			if !belongs {
				th, err := s.P.Threads.Get(who.Tenant, run.Thread)
				belongs = err == nil && th.Session != nil && th.Session.Parent == id
			}
			if !belongs || !s.P.SeesSession(who, id) || !s.P.CitationEventVisible(who, e) {
				continue
			}
			if e.Seq <= cur.Runs[e.Run] {
				continue
			}
			if len(cur.Runs) >= 200 && cur.Runs[e.Run] == 0 {
				// Retire completed runs behind a bounded watermark. A reset
				// snapshot replaces their deltas, including history pages that
				// a client may need to reload. Active run positions survive.
				for id := range cur.Runs {
					old, err := s.P.Runs.Get(who.Tenant, id)
					if err == nil && old.Status.Terminal() {
						if old.UpdatedAt.After(cur.Retired) {
							cur.Retired = old.UpdatedAt
						}
						delete(cur.Runs, id)
					}
				}
				if len(cur.Runs) >= 200 {
					// More concurrent runs than fit the header: explicitly
					// replace transient deltas rather than disconnect forever.
					cur.Before = time.Now().UTC()
					cur.Runs = map[string]uint64{}
				}
				if err = snapshot("session-reset", true); err != nil {
					return
				}
			}
			if !e.Time.After(cur.Before) || (run.Status.Terminal() && !run.UpdatedAt.After(cur.Retired) && cur.Runs[e.Run] == 0) {
				// Projection may have settled after the reset snapshot.
				if run.Status.Terminal() {
					if err = snapshot("session", false); err != nil {
						return
					}
				}
				continue
			}
			v, shown := s.P.ShownAPI(who, e)
			if !shown {
				continue
			}
			cur.Runs[e.Run] = e.Seq
			if err = emit("run", shownEvent(s.P.Names(who.Tenant), v)); err != nil {
				return
			}
			if e.Type == event.TypeCompleted || e.Type == event.TypeFailed || e.Type == event.TypeParked {
				if err = snapshot("session", false); err != nil {
					return
				}
			}
		}
	}
}
