package httpapi

import (
	"github.com/urmzd/humagen/pkg/auth"
	"net/http"
)

func (s *Server) listNativeNotifications(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	rows, err := s.P.NativeNotifications(who)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, 200, map[string]any{"notifications": rows})
}
func (s *Server) acknowledgeNativeNotification(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	if err := s.P.AcknowledgeNotification(who, r.PathValue("id")); err != nil {
		s.problem(w, err)
		return
	}
	w.WriteHeader(204)
}
