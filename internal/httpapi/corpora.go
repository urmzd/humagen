package httpapi

import (
	"net/http"
	"strconv"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/corpora"
)

// corpusRoutes are corpora: named bodies of knowledge, each holding sources
// and catalog connectors (docs/architecture/sources.md#corpora). A corpus another reader cannot read
// answers 404; only its owner changes it.
func (s *Server) corpusRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/corpora", s.authed(auth.KnowledgeRead, s.listCorpora))
	mux.HandleFunc("POST /api/corpora", s.authed(auth.KnowledgeWrite, s.createCorpus))
	mux.HandleFunc("GET /api/corpora/{name}", s.authed(auth.KnowledgeRead, s.getCorpus))
	mux.HandleFunc("PATCH /api/corpora/{name}", s.authed(auth.KnowledgeWrite, s.patchCorpus))
	mux.HandleFunc("DELETE /api/corpora/{name}", s.authed(auth.KnowledgeWrite, s.deleteCorpus))
	mux.HandleFunc("POST /api/corpora/{name}/members", s.authed(auth.KnowledgeWrite, s.addCorpusMember))
	mux.HandleFunc("DELETE /api/corpora/{name}/members/{kind}/{member}", s.authed(auth.KnowledgeWrite, s.removeCorpusMember))
}

func (s *Server) listCorpora(w http.ResponseWriter, _ *http.Request, who auth.Principal) {
	list, err := s.P.CorporaAs(who)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, map[string]any{"corpora": list})
}

func (s *Server) createCorpus(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var body platform.CorpusCreate
	if !decode(w, r, &body) {
		return
	}
	c, err := s.P.CreateCorpus(who, body)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusCreated, c)
}

func (s *Server) getCorpus(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	c, err := s.P.CorpusAs(who, r.PathValue("name"))
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, c)
}

func (s *Server) patchCorpus(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var body platform.CorpusChange
	if !decode(w, r, &body) {
		return
	}
	c, err := s.P.UpdateCorpus(who, r.PathValue("name"), body)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, c)
}

func (s *Server) deleteCorpus(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var version int64
	if v := r.URL.Query().Get("version"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 1 {
			fail(w, http.StatusBadRequest, "version is a positive integer")
			return
		}
		version = n
	}
	if err := s.P.DeleteCorpus(who, r.PathValue("name"), version); err != nil {
		s.problem(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) addCorpusMember(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var body corpora.Member
	if !decode(w, r, &body) {
		return
	}
	c, err := s.P.AddCorpusMember(who, r.PathValue("name"), body)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, c)
}

func (s *Server) removeCorpusMember(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	m := corpora.Member{Kind: corpora.MemberKind(r.PathValue("kind")), Name: r.PathValue("member")}
	c, err := s.P.RemoveCorpusMember(who, r.PathValue("name"), m)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, c)
}
