package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/knowledge"
)

// The import routes write a session's document together with the session's
// structure (knowledge.Imported), under knowledge:import, which the
// ordinary document routes never take. The server cannot verify that a
// session came from the tool it names: the structure is what the caller
// asserts, and the store keeps the caller's subject and the time with it.
// An ordinary add or edit sends no structure, and an edit leaves the one
// kept untrusted.

// importedSession reads a structure as the contract requires it: a thin or
// loose one is refused (400), never read as zeros.
func importedSession(raw json.RawMessage) (knowledge.Imported, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return knowledge.Imported{}, fmt.Errorf("%w: session is required: the structure of the session the document is written from", knowledge.ErrInvalid)
	}
	return knowledge.ParseImported(raw)
}

// manualSource refuses a source whose documents an import may not write:
// a managed copy is changed by Refresh Copy.
func manualSource(src knowledge.Source) error {
	if src.Lifecycle != nil && (src.Lifecycle.Provider != "manual" || src.Lifecycle.Mode != knowledge.Indexed) {
		return fmt.Errorf("%w: managed copies are changed by Refresh Copy", knowledge.ErrInvalid)
	}
	return nil
}

// importDocument adds a session's document and its structure in one write.
func (s *Server) importDocument(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var body struct {
		Title   string          `json:"title"`
		URI     string          `json:"uri"`
		Text    string          `json:"text"`
		Session json.RawMessage `json:"session"`
	}
	if !decode(w, r, &body) {
		return
	}
	session, err := importedSession(body.Session)
	if err != nil {
		s.problem(w, err)
		return
	}
	src, err := s.sourceSnapshot(who, r)
	if err == nil {
		err = manualSource(src)
	}
	if err != nil {
		s.problem(w, err)
		return
	}
	doc, err := s.P.Knowledge.AddImportedForScope(who.Tenant, r.PathValue("name"), body.Title, body.URI, body.Text, who.Subject, time.Now(), sourceConfigOf(src), session)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusCreated, doc)
}

// reimportDocument replaces a session's document and its structure in one
// write, only if the document's title and text are still the ones digest
// names, as an ordinary replacement is.
func (s *Server) reimportDocument(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var body struct {
		Title   string          `json:"title"`
		Text    string          `json:"text"`
		Digest  *string         `json:"digest"`
		Session json.RawMessage `json:"session"`
	}
	if !decode(w, r, &body) {
		return
	}
	if body.Digest == nil {
		s.problem(w, fmt.Errorf("%w: digest is required: the digest the document was last read with", knowledge.ErrInvalid))
		return
	}
	session, err := importedSession(body.Session)
	if err != nil {
		s.problem(w, err)
		return
	}
	src, err := s.sourceSnapshot(who, r)
	if err == nil {
		err = manualSource(src)
	}
	if err != nil {
		s.problem(w, err)
		return
	}
	doc, err := s.P.Knowledge.ReplaceImportedForScope(who.Tenant, r.PathValue("name"), r.PathValue("id"), body.Title, body.Text, *body.Digest, who.Subject, time.Now(), sourceConfigOf(src), session)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, doc)
}
