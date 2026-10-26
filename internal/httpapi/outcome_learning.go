package httpapi

import (
	"errors"
	"net/http"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/library"
	"github.com/urmzd/humagen/pkg/memory"
	"github.com/urmzd/humagen/pkg/runs"
)

func (s *Server) outcomeLearningRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/runs/{id}/learning", s.authed(auth.RunsRead, s.seenRun(s.outcomeLearning)))
	mux.HandleFunc("POST /api/runs/{id}/skill-draft", s.authed(auth.ProjectsWrite, s.seenRun(s.draftOutcomeSkill)))
	mux.HandleFunc("POST /api/runs/{id}/decision-memory", s.authed(auth.ProjectsWrite, s.seenRun(s.rememberOutcomeDecision)))
}
func (s *Server) learningProblem(w http.ResponseWriter, err error) {
	if errors.Is(err, runs.ErrLearningConflict) || errors.Is(err, memory.ErrReviewConflict) {
		fail(w, http.StatusConflict, err.Error())
		return
	}
	s.problem(w, err)
}
func (s *Server) outcomeLearning(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	preview, err := s.P.OutcomeLearning(who, r.PathValue("id"), library.Scope{Kind: r.URL.Query().Get("scope")})
	if err != nil {
		s.learningProblem(w, err)
		return
	}
	ok(w, http.StatusOK, preview)
}
func (s *Server) draftOutcomeSkill(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var body platform.OutcomeSkillDraft
	if !decode(w, r, &body) {
		return
	}
	row, err := s.P.DraftOutcomeSkill(who, r.PathValue("id"), body)
	if err != nil {
		s.learningProblem(w, err)
		return
	}
	ok(w, http.StatusCreated, row)
}
func (s *Server) rememberOutcomeDecision(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var body struct {
		Snapshot string        `json:"snapshot"`
		Scope    library.Scope `json:"scope,omitempty"`
		Decision *int          `json:"decision"`
		Text     string        `json:"text"`
	}
	if !decode(w, r, &body) {
		return
	}
	if body.Decision == nil {
		fail(w, http.StatusBadRequest, "select the recorded decision being reviewed")
		return
	}
	note, err := s.P.RememberOutcomeDecision(who, r.PathValue("id"), platform.OutcomeDecisionMemory{Snapshot: body.Snapshot, Scope: body.Scope, Decision: *body.Decision, Text: body.Text})
	if err != nil {
		s.learningProblem(w, err)
		return
	}
	ok(w, http.StatusCreated, s.viewNoteAs(who, note))
}
