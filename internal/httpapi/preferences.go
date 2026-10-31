package httpapi

import (
	"errors"
	"io"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/prefs"
	"github.com/urmzd/humagen/pkg/projects"
)

// Preferences are a person's own. Every route here acts on the caller and takes
// no subject, so there is no way to name someone else.
func (s *Server) preferenceRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/preferences", s.authed("", s.getPreferences))
	mux.HandleFunc("PUT /api/preferences", s.authed("", s.putPreferences))
	mux.HandleFunc("GET /api/preferences/photo", s.authed("", s.getPhoto))
	mux.HandleFunc("PUT /api/preferences/photo", s.authed("", s.putPhoto))
	mux.HandleFunc("DELETE /api/preferences/photo", s.authed("", s.deletePhoto))
}

func (s *Server) getPreferences(w http.ResponseWriter, _ *http.Request, who auth.Principal) {
	got, err := s.P.Prefs.Get(who.Tenant, who.Subject)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, got)
}

func (s *Server) putPreferences(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var body struct {
		DisplayName string      `json:"display_name"`
		Theme       prefs.Theme `json:"theme"`
		Accent      string      `json:"accent"`
		// Onboarded marks the first-run questions as done. It can be set
		// and never unset, and the time recorded is the server's.
		Onboarded bool `json:"onboarded"`
		// Pinned replaces the pinned conversations when present. Absent
		// leaves them as they are.
		Pinned *[]string `json:"pinned"`
		// Project shortcuts are independent of conversation pins.
		PinnedProjects *[]string `json:"pinned_projects"`
		// Muted replaces the projects whose announcements stay out of the
		// person's own inbox when present. They stay in each project's
		// inbox, and the workspace is never muted.
		Muted    *[]string       `json:"muted"`
		Personal *prefs.Personal `json:"personal"`
	}
	if !decode(w, r, &body) {
		return
	}
	// Everything is checked before anything is saved.
	if body.Personal != nil {
		clean, err := prefs.CleanPersonal(*body.Personal)
		if err != nil {
			s.problem(w, err)
			return
		}
		body.Personal = &clean
	}
	var pinned, pinnedProjects, muted []string
	if body.Pinned != nil {
		var err error
		if pinned, err = prefs.CleanPinned(*body.Pinned); err != nil {
			s.problem(w, err)
			return
		}
	}
	if body.Muted != nil {
		var err error
		if muted, err = prefs.CleanMuted(*body.Muted); err != nil {
			s.problem(w, err)
			return
		}
		for _, name := range muted {
			if _, _, err := s.P.ProjectRole(who, name, projects.Viewer); err != nil {
				s.problem(w, err)
				return
			}
		}
	}
	if body.PinnedProjects != nil {
		var err error
		if pinnedProjects, err = prefs.CleanPinnedProjects(*body.PinnedProjects); err != nil {
			s.problem(w, err)
			return
		}
		current, err := s.P.Prefs.Get(who.Tenant, who.Subject)
		if err != nil {
			s.problem(w, err)
			return
		}
		previous := current.PinnedProjects
		for _, name := range pinnedProjects {
			// A saved shortcut that became unreadable must not prevent
			// pinning another project or removing an unrelated shortcut.
			if slices.Contains(previous, name) {
				continue
			}
			if !who.Can(auth.ProjectsRead) {
				s.problem(w, auth.ErrForbidden)
				return
			}
			if _, _, err := s.P.ProjectRole(who, name, projects.Viewer); err != nil {
				s.problem(w, err)
				return
			}
		}
	}
	var personal []prefs.Personal
	if body.Personal != nil {
		personal = append(personal, *body.Personal)
	}
	next, err := s.P.SavePreferences(who, prefs.Preferences{
		DisplayName: body.DisplayName, Theme: body.Theme, Accent: body.Accent,
	}, personal...)
	if err != nil {
		s.problem(w, err)
		return
	}
	if body.Pinned != nil {
		if next, err = s.P.Prefs.SetPinned(who.Tenant, who.Subject, pinned); err != nil {
			s.problem(w, err)
			return
		}
	}
	if body.PinnedProjects != nil {
		if next, err = s.P.Prefs.SetPinnedProjects(who.Tenant, who.Subject, pinnedProjects); err != nil {
			s.problem(w, err)
			return
		}
	}
	if body.Muted != nil {
		if next, err = s.P.Prefs.SetMuted(who.Tenant, who.Subject, muted); err != nil {
			s.problem(w, err)
			return
		}
	}

	if body.Onboarded {
		if next, err = s.P.Prefs.MarkOnboarded(who.Tenant, who.Subject, time.Now()); err != nil {
			s.problem(w, err)
			return
		}
	}
	ok(w, http.StatusOK, next)
}

func (s *Server) putPhoto(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	// One byte over the limit is read so an oversized upload is told apart
	// from one that fits exactly.
	data, err := io.ReadAll(io.LimitReader(r.Body, prefs.MaxPhoto+1))
	if err != nil {
		fail(w, http.StatusBadRequest, "the picture could not be read")
		return
	}
	next, err := s.P.Prefs.SetPhoto(who.Tenant, who.Subject, data)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, next)
}

func (s *Server) deletePhoto(w http.ResponseWriter, _ *http.Request, who auth.Principal) {
	next, err := s.P.Prefs.ClearPhoto(who.Tenant, who.Subject)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, next)
}

func (s *Server) getPhoto(w http.ResponseWriter, _ *http.Request, who auth.Principal) {
	data, mime, err := s.P.Prefs.Photo(who.Tenant, who.Subject)
	if errors.Is(err, prefs.ErrNoPhoto) {
		fail(w, http.StatusNotFound, "not found")
		return
	}
	h := w.Header()
	// The type is the one the server determined from the bytes, and the
	// browser is told not to second-guess it or run anything from it.
	h.Set("Content-Type", mime)
	h.Set("Content-Length", strconv.Itoa(len(data)))
	h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	h.Set("Content-Disposition", "inline")
	h.Set("Cache-Control", "private, no-cache")
	_, _ = w.Write(data)
}
