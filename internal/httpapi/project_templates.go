package httpapi

import (
	"github.com/urmzd/humagen/pkg/auth"
	"net/http"
)

func (s *Server) listProjectTemplates(w http.ResponseWriter, _ *http.Request, _ auth.Principal) {
	type view struct {
		Name        string `json:"name"`
		Title       string `json:"title"`
		Description string `json:"description"`
		Kind        string `json:"kind"`
		Initiative  bool   `json:"initiative"`
	}
	out := []view{}
	for _, template := range s.P.ProjectTemplates() {
		out = append(out, view{Name: template.Name, Title: template.Title, Description: template.Description, Kind: string(template.Kind), Initiative: template.Initiative})
	}
	ok(w, http.StatusOK, map[string]any{"templates": out})
}
