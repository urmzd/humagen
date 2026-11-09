package httpapi

import (
	"net/http"

	"github.com/urmzd/humagen/pkg/auth"
)

// The agent store: the platform's optional agents as packages a workspace
// installs and removes (platform/agent_store.go). Whether a package is
// installed is read from the workspace's agents, never kept apart.
func (s *Server) storeRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/store/packages", s.authed(auth.AgentsRead, s.listStorePackages))
	mux.HandleFunc("POST /api/store/packages/{name}/install", s.authed(auth.AgentsWrite, s.installStorePackage))
	mux.HandleFunc("DELETE /api/store/packages/{name}", s.authed(auth.AgentsWrite, s.removeStorePackage))
}

func (s *Server) listStorePackages(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	list, err := s.P.StorePackages(r.Context(), who.Tenant)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, map[string]any{"packages": list})
}

func (s *Server) installStorePackage(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	out, err := s.P.InstallPackage(r.Context(), who, r.PathValue("name"))
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, out)
}

// removeStorePackage retires a package's agents. One changed in the
// workspace is retired only with force=true; without it the removal is
// refused with 409, naming them, and nothing is retired.
func (s *Server) removeStorePackage(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	force := false
	switch r.URL.Query().Get("force") {
	case "", "false":
	case "true":
		force = true
	default:
		fail(w, http.StatusBadRequest, "force is true or false")
		return
	}
	out, err := s.P.RemovePackage(r.Context(), who, r.PathValue("name"), force)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, out)
}
