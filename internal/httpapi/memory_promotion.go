package httpapi

import (
	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
	"net/http"
)

func (s *Server) promoteMemory(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var input platform.MemoryPromotion
	if !decode(w, r, &input) {
		return
	}
	note, err := s.P.PromoteMemory(who, input)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, 201, s.viewNoteAs(who, note))
}
