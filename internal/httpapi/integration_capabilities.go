package httpapi

import (
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/clients"
	"net/http"
)

func (s *Server) getClientCapabilities(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	got, err := s.P.ClientCapabilities(who.Tenant, r.PathValue("id"))
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, 200, got)
}
func (s *Server) putClientCapabilities(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	if _, err := s.P.Clients.Get(who.Tenant, r.PathValue("id")); err != nil {
		s.problem(w, err)
		return
	}
	var body struct {
		Version int `json:"version"`
		clients.Capabilities
	}
	if !decode(w, r, &body) {
		return
	}
	got, err := s.P.SetClientCapabilities(r.Context(), who, r.PathValue("id"), body.Version, body.Capabilities)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, 200, got)
}

func (s *Server) clientBridgeDelivery(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	if _, err := s.P.Clients.Get(who.Tenant, r.PathValue("id")); err != nil {
		s.problem(w, err)
		return
	}
	run, channel := r.URL.Query().Get("run"), r.URL.Query().Get("channel")
	if run == "" || len(run) > 160 || channel == "" || len(channel) > 64 {
		fail(w, 400, "choose a bounded run and channel")
		return
	}
	allowed, err := s.P.BridgeDeliveryAllowed(r.Context(), who, r.PathValue("id"), channel, run)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, 200, map[string]bool{"allowed": allowed})
}
