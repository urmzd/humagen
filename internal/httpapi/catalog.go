package httpapi

import (
	"errors"
	"net/http"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/catalog"
)

// getCatalog lists the connectors the caller sees and their entries,
// ranked against q when it is given.
func (s *Server) getCatalog(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	limit, valid := queryInt(r, "limit")
	if !valid || limit > catalog.MaxHits {
		fail(w, 400, "limit is at most 50")
		return
	}
	page, err := s.P.CatalogAs(who, r.URL.Query().Get("q"), int(limit))
	if err != nil {
		s.catalogProblem(w, err)
		return
	}
	ok(w, 200, page)
}

func (s *Server) publishConnector(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	c, err := s.P.PublishConnector(who, r.PathValue("name"))
	s.connectorAnswer(w, c, err)
}

func (s *Server) withdrawConnector(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	c, err := s.P.WithdrawConnector(who, r.PathValue("name"))
	s.connectorAnswer(w, c, err)
}

func (s *Server) crawlConnector(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	c, err := s.P.CrawlConnector(r.Context(), who, r.PathValue("name"))
	s.connectorAnswer(w, c, err)
}

func (s *Server) connectorAnswer(w http.ResponseWriter, c catalog.Connector, err error) {
	if err != nil {
		s.catalogProblem(w, err)
		return
	}
	ok(w, 200, c)
}

func (s *Server) catalogProblem(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, catalog.ErrNotFound):
		fail(w, http.StatusNotFound, "not found")
	case errors.Is(err, catalog.ErrInvalid):
		fail(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, platform.ErrCrawlRunning):
		fail(w, http.StatusConflict, err.Error())
	case errors.Is(err, platform.ErrForbidden):
		fail(w, http.StatusForbidden, err.Error())
	default:
		// The connected system failed: say so, without its details.
		s.Log.Warn("catalog: crawl failed", "err", err)
		fail(w, http.StatusBadGateway, "the connected system could not be listed")
	}
}
