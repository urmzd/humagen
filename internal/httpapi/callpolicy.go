package httpapi

import (
	"net/http"
	"slices"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/callpolicy"
)

// getCallPolicy answers an agent's call policy: the stored one, or the
// default an agent with none has (version 0).
func (s *Server) getCallPolicy(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	pol, err := s.P.CallPolicy(r.Context(), who.Tenant, r.PathValue("name"))
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, callPolicyView{Policy: s.P.Seer(who).CallPolicy(pol), UpdatedByName: s.P.NameOf(who.Tenant, pol.UpdatedBy)})
}

// callPolicyBody is what a caller writes: a policy without the agent, which
// is the one in the path, or what the server records about the change.
type callPolicyBody struct {
	Callers        callpolicy.Callers  `json:"callers"`
	Allow          []string            `json:"allow"`
	Approval       callpolicy.Approval `json:"approval"`
	Trusted        []string            `json:"trusted"`
	Approvers      []string            `json:"approvers"`
	SelfApproval   bool                `json:"self_approval"`
	ExpiresSeconds int                 `json:"expires_seconds"`
}

// putCallPolicy replaces an agent's call policy, the agent in the path.
func (s *Server) putCallPolicy(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var body callPolicyBody
	if !decode(w, r, &body) {
		return
	}
	// An agent that lives in a project is that project's to change, and
	// what it may call is part of it.
	if err := s.P.OwnsHome(who, r.PathValue("name")); err != nil {
		s.problem(w, err)
		return
	}
	// An agent the caller may not read is not one, and the callers it lists
	// that they were not shown stay.
	pol, err := s.P.SetCallPolicyBy(r.Context(), who, r.PathValue("name"), callpolicy.Policy{
		Callers: body.Callers, Allow: body.Allow, Approval: body.Approval, Trusted: body.Trusted,
		Approvers: body.Approvers, SelfApproval: body.SelfApproval, ExpiresSeconds: body.ExpiresSeconds,
	})
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, callPolicyView{Policy: s.P.Seer(who).CallPolicy(pol), UpdatedByName: s.P.NameOf(who.Tenant, pol.UpdatedBy)})
}

// reachable answers, for the agent in the path as a caller, every other agent
// the caller may read and whether it may call it. An agent that lives in a
// project the caller cannot see is left out, as from the agents list.
func (s *Server) reachable(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	agents, truncated, err := s.P.Reachable(r.Context(), who.Tenant, r.PathValue("name"))
	if err != nil {
		s.problem(w, err)
		return
	}
	seer := s.P.Seer(who)
	agents = slices.DeleteFunc(agents, func(a platform.Reach) bool { return !seer.Agent(a.Agent) })
	ok(w, http.StatusOK, map[string]any{"agents": agents, "truncated": truncated})
}

// callPolicyView is an agent's call policy and what whoever last saved it is
// called. The policy names them by subject; the console shows the name.
type callPolicyView struct {
	callpolicy.Policy
	UpdatedByName string `json:"updated_by_name,omitempty"`
}
