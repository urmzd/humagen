package httpapi

import (
	"errors"
	"net/http"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/threads"
)

// continueImport starts, as the caller, a private conversation that
// continues a document an import wrote from another tool's session
// (Platform.ContinueImport). The route checks runs:write, as a fork does,
// and seenSource answers 404 for a source the caller cannot see. The body
// names the revision the caller read, its document digest and, when one is
// kept, its structure digest, and the project the caller saw the source
// filed in, so a document changed, a structure replaced or a source
// detached since refuses it (409). It starts no agent: the caller posts the
// conversation's first message.
func (s *Server) continueImport(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var body struct {
		DocumentDigest  string   `json:"document_digest"`
		StructureDigest string   `json:"structure_digest"`
		Project         *string  `json:"project"`
		Title           string   `json:"title"`
		Agents          []string `json:"agents"`
		Mode            string   `json:"mode"`
	}
	if !decode(w, r, &body) {
		return
	}
	// The project is required here, though the platform can choose one:
	// without it, a source detached since the caller saw it would be
	// continued in the workspace itself rather than refused.
	if body.Project == nil {
		fail(w, http.StatusBadRequest, "name the project you saw the source filed in, or ~ for the workspace itself")
		return
	}
	th, err := s.P.ContinueImport(r.Context(), who, platform.ContinueImportInput{
		Source: r.PathValue("name"), Document: r.PathValue("id"),
		DocumentDigest: body.DocumentDigest, StructureDigest: body.StructureDigest,
		Project: body.Project, Title: body.Title, Agents: body.Agents, Mode: body.Mode,
	})
	switch {
	case err == nil:
	case errors.Is(err, platform.ErrImportChanged), errors.Is(err, platform.ErrImportDetached), errors.Is(err, platform.ErrImportNoSourceID):
		// Each says what to do already, so it is not told to reload too.
		fail(w, http.StatusConflict, detail(err, threads.ErrVersion))
		return
	default:
		s.problem(w, err)
		return
	}
	ok(w, http.StatusCreated, s.P.Seer(who).Thread(s.P.ShowThread(who.Tenant, th)))
}
