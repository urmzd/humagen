package httpapi

import (
	"net/http"

	"github.com/urmzd/warp/pkg/auth"
	"github.com/urmzd/warp/pkg/callpolicy"
)

// getCallPolicy answers an agent's call policy: the stored one, or the
// default an agent with none has (version 0).
func (s *Server) getCallPolicy(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	pol, err := s.P.CallPolicy(r.Context(), who.Tenant, r.PathValue("name"))
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, pol)
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
	pol, err := s.P.SetCallPolicy(r.Context(), who.Tenant, r.PathValue("name"), who.Subject, callpolicy.Policy{
		Callers: body.Callers, Allow: body.Allow, Approval: body.Approval, Trusted: body.Trusted,
		Approvers: body.Approvers, SelfApproval: body.SelfApproval, ExpiresSeconds: body.ExpiresSeconds,
	})
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, pol)
}

// reachable answers, for the agent in the path as a caller, every other agent
// and whether it may call it.
func (s *Server) reachable(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	agents, truncated, err := s.P.Reachable(r.Context(), who.Tenant, r.PathValue("name"))
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, map[string]any{"agents": agents, "truncated": truncated})
}
