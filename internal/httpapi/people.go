package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/urmzd/warp/pkg/auth"
	"github.com/urmzd/warp/pkg/people"
	"github.com/urmzd/warp/pkg/prefs"
)

// The directory: who is in the workspace, readable by everyone in it with
// people:read. A person is listed by the name they go by, their handle, the
// picture they uploaded themselves and the roles they held when last seen.
// No email address is shown: a subject that holds one is left out, and
// subjects are not searched. No picture is fetched from anywhere else, so
// reading the directory sends nobody else a request.
func (s *Server) peopleRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/people", s.authed(auth.PeopleRead, s.listPeople))
	mux.HandleFunc("GET /api/people/{handle}/photo", s.authed(auth.PeopleRead, s.personPhoto))
}

// personView is one person as the directory shows them.
type personView struct {
	// Subject is left out when it holds an address (people.ShownSubject).
	Subject string `json:"subject,omitempty"`
	// Name is the name they chose for themselves, or the one they signed in
	// with. Empty until one is known.
	Name   string   `json:"name,omitempty"`
	Handle string   `json:"handle"`
	Roles  []string `json:"roles"`
	// Picture is where their own photo is served, or null without one.
	Picture *string   `json:"picture"`
	SeenAt  time.Time `json:"seen_at,omitzero"`
}

func (s *Server) personOf(tenant string, p people.Person) personView {
	v := personView{Subject: p.ShownSubject(), Name: p.Name, Handle: p.Handle, Roles: orEmpty(p.Roles), SeenAt: p.SeenAt}
	own := s.P.Prefs.Get(tenant, p.Subject)
	if own.DisplayName != "" {
		v.Name = own.DisplayName
	}
	if own.HasPhoto {
		// The version changes with every upload, so a browser never shows
		// an old one.
		at := fmt.Sprintf("/api/people/%s/photo?v=%d", url.PathEscape(p.Handle), own.PhotoVersion)
		v.Picture = &at
	}
	return v
}

// listPeople pages the directory by handle. q searches names and handles;
// limit is at most 50, and before is the next of the page before.
func (s *Server) listPeople(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	q := r.URL.Query()
	query := people.Query{Q: q.Get("q"), Before: q.Get("before")}
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > people.MaxPage {
			fail(w, http.StatusBadRequest, fmt.Sprintf("limit is a number from 1 to %d", people.MaxPage))
			return
		}
		query.Limit = n
	}
	page, next, err := s.P.People.List(who.Tenant, query)
	if err != nil {
		s.problem(w, err)
		return
	}
	out := make([]personView, len(page))
	for i, p := range page {
		out[i] = s.personOf(who.Tenant, p)
	}
	var cursor *string
	if next != "" {
		cursor = &next
	}
	ok(w, http.StatusOK, map[string]any{"people": out, "next": cursor})
}

// personPhoto serves the photo a person uploaded for themselves.
func (s *Server) personPhoto(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	p, err := s.P.People.ByHandle(who.Tenant, r.PathValue("handle"))
	if err != nil {
		s.problem(w, err)
		return
	}
	data, mime, err := s.P.Prefs.Photo(who.Tenant, p.Subject)
	if errors.Is(err, prefs.ErrNoPhoto) {
		fail(w, http.StatusNotFound, "not found")
		return
	}
	h := w.Header()
	h.Set("Content-Type", mime)
	h.Set("Content-Length", strconv.Itoa(len(data)))
	h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	h.Set("Content-Disposition", "inline")
	h.Set("Cache-Control", "private, no-cache")
	_, _ = w.Write(data)
}
