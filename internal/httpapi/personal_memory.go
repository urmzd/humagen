package httpapi

import (
	"net/http"
	"time"

	"github.com/urmzd/humagen/pkg/audit"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/memory"
)

func (s *Server) listPersonalMemory(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	owner := memory.PersonalOwner(who.Subject)
	if !s.P.Seer(who).MemoryOwner(owner) {
		fail(w, 403, "personal memory requires a person")
		return
	}
	ok(w, 200, map[string]any{"notes": orEmpty(s.P.Memory.List(who.Tenant, owner))})
}
func (s *Server) addPersonalMemory(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	owner := memory.PersonalOwner(who.Subject)
	if !s.P.Seer(who).MemoryOwner(owner) {
		fail(w, 403, "personal memory requires a person")
		return
	}
	var body struct {
		Text string `json:"text"`
		memory.Details
	}
	if !decode(w, r, &body) {
		return
	}
	attempt, err := s.P.BeginAudit(who, audit.Change{Action: "memory.remember", Resource: "memory", ID: owner, Subject: who.Subject, Route: "POST /api/me/memory", Rule: string(auth.RunsWrite)})
	if err != nil {
		s.problem(w, err)
		return
	}
	note, err := s.P.Memory.AddDetailed(who.Tenant, owner, body.Text, memory.Person, memory.Approved, who.Subject, time.Now(), body.Details)
	if auditErr := attempt.Finish(err); auditErr != nil {
		s.P.LogAuditCompletionFailure(auditErr)
	}
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, 201, note)
}
func (s *Server) forgetPersonalMemory(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	owner := memory.PersonalOwner(who.Subject)
	if !s.P.Seer(who).MemoryOwner(owner) {
		notFound(w)
		return
	}
	id := r.PathValue("id")
	exists := false
	for _, note := range s.P.Memory.List(who.Tenant, owner) {
		exists = exists || note.ID == id
	}
	if !exists {
		fail(w, 404, "not found")
		return
	}
	attempt, err := s.P.BeginAudit(who, audit.Change{Action: "memory.forget", Resource: "memory", ID: id, Subject: who.Subject, Route: "DELETE /api/me/memory/{id}", Rule: string(auth.RunsWrite)})
	if err != nil {
		s.problem(w, err)
		return
	}
	err = s.P.RemoveMemory(who.Tenant, owner, id)
	if auditErr := attempt.Finish(err); auditErr != nil {
		s.P.LogAuditCompletionFailure(auditErr)
	}
	if err != nil {
		s.problem(w, err)
		return
	}
	w.WriteHeader(204)
}

// Forget records who removed a memory, without retaining the forgotten text.
// Permission and owner checks must have completed before entering this helper.
func (s *Server) forgetOwnedMemory(w http.ResponseWriter, r *http.Request, who auth.Principal, owner, route, rule string) {
	id := r.PathValue("id")
	exists := false
	for _, note := range s.P.Memory.List(who.Tenant, owner) {
		exists = exists || note.ID == id
	}
	if !exists {
		notFound(w)
		return
	}
	change := audit.Change{Action: "memory.forget", Resource: "memory", ID: id, Route: route, Rule: rule}
	if project, ok := memory.OwnerProject(owner); ok {
		change.Scope = "project"
		change.Project = project
	} else if person, ok := memory.OwnerPerson(owner); ok {
		change.Scope = "personal"
		change.Subject = person
	} else {
		change.Scope = "agent"
		change.Agent = owner
	}
	attempt, err := s.P.BeginAudit(who, change)
	if err != nil {
		s.problem(w, err)
		return
	}
	err = s.P.RemoveMemory(who.Tenant, owner, id)
	if auditErr := attempt.Finish(err); auditErr != nil {
		s.P.LogAuditCompletionFailure(auditErr)
	}
	if err != nil {
		s.problem(w, err)
		return
	}
	w.WriteHeader(204)
}
