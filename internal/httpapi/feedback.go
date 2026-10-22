package httpapi

import (
	"net/http"

	"github.com/urmzd/warp/internal/platform"
	"github.com/urmzd/warp/pkg/auth"
	"github.com/urmzd/warp/pkg/feedback"
)

func (s *Server) putFeedback(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var body struct {
		Rating  string          `json:"rating"`
		Reason  string          `json:"reason"`
		Comment string          `json:"comment"`
		ActAs   *platform.ActAs `json:"act_as"`
	}
	if !decode(w, r, &body) {
		return
	}
	actor := who
	if body.ActAs != nil {
		person, err := s.P.Acting(who, *body.ActAs)
		if err != nil {
			s.problem(w, err)
			return
		}
		actor = person
	}
	run, err := s.P.Runs.Get(who.Tenant, r.PathValue("id"))
	if err != nil || !actor.Can(auth.RunsRead) || !s.P.Seer(actor).Run(run) {
		notFound(w)
		return
	}
	if !run.Status.Terminal() {
		fail(w, http.StatusConflict, "feedback needs a finished run")
		return
	}
	if body.Rating != "up" && body.Rating != "down" || len(body.Comment) > 4000 || len(body.Reason) > 80 {
		fail(w, http.StatusBadRequest, "invalid feedback")
		return
	}
	record, err := s.P.Feedback.Save(who.Tenant, feedback.Record{Run: run.ID, Subject: actor.Subject, Rating: body.Rating, Reason: body.Reason, Comment: body.Comment})
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, record)
}

func (s *Server) listFeedback(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	if _, err := s.P.Runs.Get(who.Tenant, r.PathValue("id")); err != nil {
		notFound(w)
		return
	}
	records := s.P.Feedback.List(who.Tenant, r.PathValue("id"), r.URL.Query().Get("after"))
	next := ""
	if len(records) == 200 {
		next = records[len(records)-1].Subject
	}
	ok(w, http.StatusOK, map[string]any{"feedback": records, "next": next})
}
