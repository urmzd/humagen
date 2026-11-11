package httpapi

import (
	"net/http"
	"strconv"

	"github.com/urmzd/humagen/pkg/audit"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/evals"
)

// listEvalCandidates is a page of the review queue: the inputs of answers
// rated down with a reason that no reviewer has decided yet, newest first.
// A candidate from a run the caller may not read is left out.
func (s *Server) listEvalCandidates(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	q := r.URL.Query()
	limit := 50
	if q.Has("limit") {
		n, err := strconv.Atoi(q.Get("limit"))
		if err != nil || n < 1 || n > 200 {
			fail(w, http.StatusBadRequest, "limit is between 1 and 200")
			return
		}
		limit = n
	}
	page, err := s.P.ListEvalCandidates(who, q.Get("agent"), limit, q.Get("after"))
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, page)
}

// acceptEvalCandidate adds a candidate's question to a suite as a new case
// with the reviewer's checks, and takes it out of the queue. The answer the
// agent gave is never copied.
func (s *Server) acceptEvalCandidate(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	// A candidate that is not there is said so before the body is read, as
	// one the caller may not see is.
	c, err := s.P.EvalCandidate(who, r.PathValue("id"))
	if err != nil {
		s.problem(w, err)
		return
	}
	var body struct {
		Suite  string        `json:"suite"`
		Checks []evals.Check `json:"checks"`
	}
	if !decode(w, r, &body) {
		return
	}
	suite, added, err := s.P.EvalCandidateSuite(who, c, body.Suite, body.Checks)
	if err != nil {
		s.problem(w, err)
		return
	}
	attempt, err := s.P.BeginAudit(who, audit.Change{Action: "eval.candidate.accept", Resource: "eval_candidate", ID: c.ID, Subject: who.Subject, Project: c.Project, Agent: c.Agent, Route: "POST /api/eval-candidates/{id}/accept", Rule: string(auth.EvalsWrite), After: map[string]string{"suite": suite.Name}})
	if err != nil {
		s.problem(w, err)
		return
	}
	err = s.P.AcceptEvalCandidate(who, c, suite, added)
	if auditErr := attempt.Finish(err); auditErr != nil {
		s.P.LogAuditCompletionFailure(auditErr)
	}
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, s.P.EvalSuiteView(suite))
}

// dismissEvalCandidate takes a candidate out of the queue without a case.
func (s *Server) dismissEvalCandidate(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	c, err := s.P.EvalCandidate(who, r.PathValue("id"))
	if err != nil {
		s.problem(w, err)
		return
	}
	attempt, err := s.P.BeginAudit(who, audit.Change{Action: "eval.candidate.dismiss", Resource: "eval_candidate", ID: c.ID, Subject: who.Subject, Project: c.Project, Agent: c.Agent, Route: "DELETE /api/eval-candidates/{id}", Rule: string(auth.EvalsWrite)})
	if err != nil {
		s.problem(w, err)
		return
	}
	err = s.P.DismissEvalCandidate(who, c)
	if auditErr := attempt.Finish(err); auditErr != nil {
		s.P.LogAuditCompletionFailure(auditErr)
	}
	if err != nil {
		s.problem(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
