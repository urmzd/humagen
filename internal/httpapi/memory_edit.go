package httpapi

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/urmzd/humagen/pkg/audit"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/memory"
)

func (s *Server) editPersonalMemory(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	owner := memory.PersonalOwner(who.Subject)
	if !s.P.Seer(who).MemoryOwner(owner) {
		notFound(w)
		return
	}
	s.editMemory(w, r, who, owner, "PUT /api/me/memory/{id}", string(auth.RunsWrite))
}
func (s *Server) editAgentMemory(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	owner, found := s.agentToChange(w, r, who)
	if !found {
		return
	}
	s.editMemory(w, r, who, owner, "PUT /api/agents/{name}/memory/{id}", string(auth.AgentsWrite))
}
func (s *Server) editProjectMemory(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	owner, found := s.projectOwner(w, r, who, true)
	if !found {
		return
	}
	s.editMemory(w, r, who, owner, "PUT /api/projects/{name}/memory/{id}", string(auth.ProjectsWrite))
}
func (s *Server) editMemory(w http.ResponseWriter, r *http.Request, who auth.Principal, owner, route, rule string) {
	var before *memory.Note
	for _, note := range s.P.Memory.List(who.Tenant, owner) {
		if note.ID == r.PathValue("id") {
			copy := note
			before = &copy
			break
		}
	}
	if before == nil {
		notFound(w)
		return
	}
	var body struct {
		Text    string          `json:"text"`
		Kind    *memory.Kind    `json:"kind"`
		Expires json.RawMessage `json:"expires_at"`
	}
	if !decode(w, r, &body) {
		return
	}
	var details *memory.Details
	if body.Kind != nil || len(body.Expires) > 0 {
		next := before.Details
		if body.Kind != nil {
			next.Kind = *body.Kind
		}
		if len(body.Expires) > 0 {
			next.ExpiresAt = time.Time{}
			if err := json.Unmarshal(body.Expires, &next.ExpiresAt); err != nil {
				fail(w, 400, "invalid memory expiry")
				return
			}
		}
		details = &next
	}
	change := audit.Change{Action: "memory.edit", Resource: "memory", ID: before.ID, Route: route, Rule: rule, Before: map[string]string{"kind": string(before.Kind), "status": string(before.Status)}}
	if project, ok := memory.OwnerProject(owner); ok {
		change.Scope = "project"
		change.Project = project
	} else if subject, ok := memory.OwnerPerson(owner); ok {
		change.Scope = "personal"
		change.Subject = subject
	} else {
		change.Scope = "agent"
		change.Agent = owner
	}
	attempt, err := s.P.BeginAudit(who, change)
	if err != nil {
		s.problem(w, err)
		return
	}
	note, err := s.P.Memory.Edit(who.Tenant, owner, before.ID, body.Text, who.Subject, time.Now(), details)
	if completion := attempt.Finish(err); completion != nil {
		s.P.LogAuditCompletionFailure(completion)
	}
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, 200, s.viewNoteAs(who, note))
}
