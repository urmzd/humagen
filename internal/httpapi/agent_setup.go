package httpapi

import (
	"net/http"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/audit"
	"github.com/urmzd/humagen/pkg/auth"
)

func (s *Server) agentSetupRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/agent-setups", s.authed(auth.AgentsWrite, s.createAgentSetup))
	mux.HandleFunc("PUT /api/agent-setups/{name}", s.authed(auth.AgentsWrite, s.amendAgentSetup))
	mux.HandleFunc("GET /api/agent-setups/{name}", s.authed(auth.AgentsWrite, s.getAgentSetup))
	mux.HandleFunc("POST /api/agent-setups/{name}/resume", s.authed(auth.AgentsWrite, s.resumeAgentSetup))
	mux.HandleFunc("POST /api/agent-setups/{name}/test", s.authed(auth.AgentsWrite, s.testAgentSetup))
	mux.HandleFunc("POST /api/agent-setups/{name}/activate", s.authed(auth.AgentsWrite, s.activateAgentSetup))
}
func (s *Server) getAgentSetup(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	record, err := s.P.AgentSetup(who, r.PathValue("name"))
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, record)
}
func (s *Server) setupChange(w http.ResponseWriter, r *http.Request, who auth.Principal, name, action string, apply func() (platform.AgentSetup, error)) {
	route := r.Method + " /api/agent-setups" + map[bool]string{true: "/{name}/" + action, false: ""}[action != "create"]
	if action == "amend" {
		route = r.Method + " /api/agent-setups/{name}"
	}
	attempt, err := s.P.BeginAudit(who, audit.Change{Action: "agent.setup." + action, Resource: "agent_setup", ID: name, Scope: "personal", Subject: who.Subject, Agent: name, Route: route, Rule: string(auth.AgentsWrite)})
	if err != nil {
		s.problem(w, err)
		return
	}
	record, err := apply()
	domainErr := err
	if domainErr == nil && record.Problem != "" {
		domainErr = platform.ErrSettings
	}
	if finishErr := attempt.Finish(domainErr); finishErr != nil {
		s.P.LogAuditCompletionFailure(finishErr)
	}
	if err != nil {
		s.problem(w, err)
		return
	}
	visible, err := s.P.AgentSetup(who, record.Name)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, visible)
}
func (s *Server) createAgentSetup(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var plan platform.AgentSetupPlan
	if !decode(w, r, &plan) {
		return
	}
	s.setupChange(w, r, who, plan.Spec.Name, "create", func() (platform.AgentSetup, error) { return s.P.CreateAgentSetup(r.Context(), who, plan) })
}
func (s *Server) resumeAgentSetup(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	name := r.PathValue("name")
	if _, err := s.P.AgentSetup(who, name); err != nil {
		s.problem(w, err)
		return
	}
	s.setupChange(w, r, who, name, "resume", func() (platform.AgentSetup, error) { return s.P.ResumeAgentSetup(r.Context(), who, name) })
}
func (s *Server) testAgentSetup(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var body struct {
		Text string `json:"text"`
	}
	if !decode(w, r, &body) {
		return
	}
	name := r.PathValue("name")
	if _, err := s.P.AgentSetup(who, name); err != nil {
		s.problem(w, err)
		return
	}
	s.setupChange(w, r, who, name, "test", func() (platform.AgentSetup, error) { return s.P.TestAgentSetup(r.Context(), who, name, body.Text) })
}
func (s *Server) activateAgentSetup(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	name := r.PathValue("name")
	if _, err := s.P.AgentSetup(who, name); err != nil {
		s.problem(w, err)
		return
	}
	s.setupChange(w, r, who, name, "activate", func() (platform.AgentSetup, error) { return s.P.ActivateAgentSetup(r.Context(), who, name) })
}

func (s *Server) amendAgentSetup(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	name := r.PathValue("name")
	if _, err := s.P.AgentSetup(who, name); err != nil {
		s.problem(w, err)
		return
	}
	var body struct {
		Version int                     `json:"version"`
		Plan    platform.AgentSetupPlan `json:"plan"`
	}
	if !decode(w, r, &body) {
		return
	}
	s.setupChange(w, r, who, name, "amend", func() (platform.AgentSetup, error) {
		return s.P.AmendAgentSetup(r.Context(), who, name, body.Plan, body.Version)
	})
}
