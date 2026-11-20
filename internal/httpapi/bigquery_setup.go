package httpapi

import (
	"errors"
	"net/http"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/catalog"
)

func (s *Server) getBigQuerySetup(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	view, err := s.P.BigQuerySetup(who)
	if err != nil {
		s.bigQueryProblem(w, err)
		return
	}
	ok(w, http.StatusOK, view)
}

func (s *Server) createBigQueryConnector(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var in platform.BigQuerySetupInput
	if !decode(w, r, &in) {
		return
	}
	connector, err := s.P.CreateBigQueryConnector(r.Context(), who, in)
	if err != nil {
		s.bigQueryProblem(w, err)
		return
	}
	ok(w, http.StatusCreated, connector)
}

func (s *Server) reconnectBigQueryConnector(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	connector, err := s.P.ReconnectBigQueryConnector(r.Context(), who, r.PathValue("name"))
	if err != nil {
		s.bigQueryProblem(w, err)
		return
	}
	ok(w, http.StatusOK, connector)
}

func (s *Server) bigQueryProblem(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, catalog.ErrNotFound):
		fail(w, http.StatusNotFound, "not found")
	case errors.Is(err, catalog.ErrInvalid):
		fail(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, catalog.ErrExists):
		fail(w, http.StatusConflict, "a connector already uses that name")
	default:
		s.problem(w, err)
	}
}

func (s *Server) createSnowflakeConnector(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var in platform.SnowflakeSetupInput
	if !decode(w, r, &in) {
		return
	}
	connector, err := s.P.CreateSnowflakeConnector(who, in)
	if err != nil {
		s.bigQueryProblem(w, err)
		return
	}
	ok(w, http.StatusCreated, connector)
}

func (s *Server) putCatalogLogin(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var in platform.CatalogLoginInput
	if !decode(w, r, &in) {
		return
	}
	if err := s.P.SetCatalogLogin(who, r.PathValue("name"), in); err != nil {
		s.bigQueryProblem(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) deleteCatalogLogin(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	if err := s.P.DeleteCatalogLogin(who, r.PathValue("name")); err != nil {
		s.bigQueryProblem(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
