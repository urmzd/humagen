package httpapi

import (
	"net/http"
	"time"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/alerts"
	"github.com/urmzd/humagen/pkg/auth"
)

func (s *Server) createSessionAlert(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	th, err := s.P.SessionRecord(r.Context(), who, r.PathValue("id"))
	if err != nil {
		s.problem(w, err)
		return
	}
	if err := s.P.MayPost(who, platform.Place{Project: th.Project, Thread: th.ID}); err != nil {
		s.problem(w, err)
		return
	}
	var body struct {
		Title       string          `json:"title"`
		Body        string          `json:"body,omitempty"`
		Priority    alerts.Priority `json:"priority"`
		Message     string          `json:"message,omitempty"`
		Who         []string        `json:"who,omitempty"`
		Action      string          `json:"action,omitempty"`
		ResolveWhen string          `json:"resolve_when,omitempty"`
	}
	if !decode(w, r, &body) {
		return
	}
	if body.Message != "" {
		// The anchor must belong to this session, never another session's message.
		if _, err := s.P.Threads.Message(who.Tenant, th.ID, body.Message); err != nil {
			run, e := s.P.Runs.Get(who.Tenant, body.Message)
			if e != nil || run.Thread != th.ID || !s.P.Seer(who).Run(run) {
				fail(w, http.StatusBadRequest, "message does not belong to this session")
				return
			}
		}
	}
	body.Who, err = s.P.SessionAlertAudience(who, th.ID, body.Who)
	if err != nil {
		s.problem(w, err)
		return
	}
	a, err := s.P.CreateSessionAlert(who.Tenant, alerts.Alert{
		Session: th.ID, Project: th.Project, Title: body.Title, Body: body.Body, Priority: body.Priority,
		Message: body.Message, Who: body.Who, Action: body.Action, ResolveWhen: body.ResolveWhen, At: time.Now().UTC(),
	})
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusCreated, viewAlert(s.P.Names(who.Tenant), a))
}

func (s *Server) patchAlert(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	current, err := s.P.Alerts.Alert(who.Tenant, r.PathValue("id"))
	if err != nil {
		s.problem(w, err)
		return
	}
	if current.Session != "" {
		if err := s.P.MayPost(who, platform.Place{Project: current.Project, Thread: current.Session}); err != nil {
			s.problem(w, err)
			return
		}
	}
	var body struct {
		State        alerts.AlertState `json:"state"`
		SnoozedUntil time.Time         `json:"snoozed_until,omitzero"`
	}
	if !decode(w, r, &body) {
		return
	}
	var a alerts.Alert
	if body.State == alerts.Resolved && body.SnoozedUntil.IsZero() {
		a, err = s.P.AckAlert(who.Tenant, r.PathValue("id"), who.Subject)
	} else {
		a, err = s.P.Alerts.ChangeState(who.Tenant, r.PathValue("id"), who.Subject, body.State, body.SnoozedUntil, time.Now().UTC())
	}
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, viewAlert(s.P.Names(who.Tenant), a.View(time.Now().UTC())))
}

func alertFor(a alerts.Alert, who auth.Principal) bool {
	if len(a.Who) == 0 {
		return true
	}
	for _, to := range a.Who {
		if to == who.Subject {
			return true
		}
		for _, role := range who.Roles {
			if to == alerts.RolePrefix+string(role) {
				return true
			}
		}
	}
	return false
}
