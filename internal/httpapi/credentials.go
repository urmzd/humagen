package httpapi

import (
	"net/http"

	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/credentials"
)

// A person's own credentials for the tools that act as them. Every route acts
// on the caller, or on the person a client's machine acts for through its
// people map (act_as_client and act_as_user), and takes no subject, so there
// is no way to name anyone else, and a value written is never answered back.

func (s *Server) listToolCredentials(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	person, allowed := s.actingFor(w, r, who, auth.RunsRead, "connections")
	if !allowed {
		return
	}
	who = person
	out, err := s.P.ToolCredentials(r.Context(), who)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, out)
}

func (s *Server) putToolCredential(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	person, allowed := s.actingFor(w, r, who, auth.RunsWrite, "connections")
	if !allowed {
		return
	}
	who = person
	var body credentials.Input
	if !decode(w, r, &body) {
		return
	}
	c, err := s.P.ConnectTool(r.Context(), who, r.PathValue("provider"), body)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, c)
}

func (s *Server) deleteToolCredential(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	person, allowed := s.actingFor(w, r, who, auth.RunsWrite, "connections")
	if !allowed {
		return
	}
	who = person
	if err := s.P.DisconnectTool(r.Context(), who, r.PathValue("provider")); err != nil {
		s.problem(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
