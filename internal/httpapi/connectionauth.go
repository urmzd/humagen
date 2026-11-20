package httpapi

import (
	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
	"net/http"
)

type ConnectionCallback struct {
	State          string `json:"state"`
	Code           string `json:"code"`
	InstallationID string `json:"installation_id,omitempty"`
}

func (s *Server) listConnectionPresets(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	out, err := s.P.ConnectionPresets(who)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, out)
}
func (s *Server) authorizeConnection(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var input platform.ConnectionAuthorizationInput
	if r.ContentLength != 0 {
		if !decode(w, r, &input) {
			return
		}
	}
	out, err := s.P.AuthorizeConnection(r.Context(), who, r.PathValue("provider"), input)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, out)
}
func (s *Server) completeConnection(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var body ConnectionCallback
	if !decode(w, r, &body) {
		return
	}
	var err error
	if r.PathValue("provider") == "github" {
		err = s.P.CompleteGitHubInstall(r.Context(), who, body.State, body.Code, body.InstallationID)
	} else {
		err = s.P.CompleteConnection(r.Context(), who, r.PathValue("provider"), body.State, body.Code)
	}
	if err != nil {
		s.problem(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (s *Server) disconnectConnection(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	if err := s.P.DisconnectConnection(who, r.PathValue("provider")); err != nil {
		s.problem(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) checkConnection(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	out, err := s.P.CheckConnection(r.Context(), who, r.PathValue("provider"))
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, out)
}
func (s *Server) checkClientConnection(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	out, err := s.P.CheckClientConnection(r.Context(), who, r.PathValue("id"))
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, out)
}

func (s *Server) getConnection(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	out, err := s.P.GetConnection(r.Context(), who, r.PathValue("provider"))
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, out)
}
