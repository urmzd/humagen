package httpapi

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/library"
	"github.com/urmzd/humagen/pkg/projects"
)

func personalLibraryScope(who auth.Principal, scope library.Scope) (library.Scope, error) {
	if scope.Kind == "personal" {
		if scope.Subject != "" && scope.Subject != who.Subject {
			return library.Scope{}, library.ErrNotFound
		}
		scope.Subject = who.Subject
	}
	return scope, nil
}
func (s *Server) listLibrarySkills(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	kind := r.URL.Query().Get("scope")
	if kind == "" {
		kind = "workspace"
	}
	scope, err := personalLibraryScope(who, library.Scope{Kind: kind, Project: projects.NameOf(r.URL.Query().Get("project"))})
	if err == nil {
		err = s.P.LibraryScope(who, scope, false)
	}
	if err != nil {
		s.problem(w, err)
		return
	}
	rows := s.P.SkillLibrary.List(who.Tenant, scope)
	for i := range rows {
		rows[i] = s.P.LibraryView(who, rows[i])
		rows[i].Files = nil // Package content is loaded only after opening a skill.
	}
	ok(w, 200, map[string]any{"skills": rows})
}
func (s *Server) draftLibrarySkill(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var body struct {
		Scope      library.Scope      `json:"scope"`
		Definition library.Definition `json:"definition"`
		Version    int                `json:"version"`
	}
	if !decode(w, r, &body) {
		return
	}
	scope, err := personalLibraryScope(who, body.Scope)
	if err != nil {
		s.problem(w, err)
		return
	}
	row, err := s.P.DraftSkill(who, scope, body.Definition, body.Version, nil)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, 201, s.P.LibraryView(who, row))
}
func (s *Server) getLibrarySkill(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	row, err := s.P.LibrarySkill(who, r.PathValue("id"))
	if err != nil {
		s.problem(w, err)
		return
	}
	rows := s.P.SkillLibrary.History(who.Tenant, row.Scope, row.Name)
	for i := range rows {
		rows[i] = s.P.LibraryView(who, rows[i])
	}
	ok(w, 200, map[string]any{"revisions": rows})
}
func (s *Server) skillRevision(w http.ResponseWriter, r *http.Request, who auth.Principal) (library.Revision, bool) {
	latest, err := s.P.LibrarySkill(who, r.PathValue("id"))
	if err != nil {
		s.problem(w, err)
		return library.Revision{}, false
	}
	version, err := strconv.Atoi(r.PathValue("version"))
	if err != nil || version < 1 {
		s.problem(w, library.ErrNotFound)
		return library.Revision{}, false
	}
	row, err := s.P.SkillLibrary.Get(who.Tenant, latest.Scope, latest.Name, version)
	if err != nil {
		s.problem(w, err)
		return library.Revision{}, false
	}
	return row, true
}
func (s *Server) testLibrarySkill(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	row, found := s.skillRevision(w, r, who)
	if !found {
		return
	}
	var body struct {
		Agent   string `json:"agent"`
		Project string `json:"project"`
		Text    string `json:"text"`
	}
	if !decode(w, r, &body) {
		return
	}
	if strings.TrimSpace(body.Text) == "" || len(body.Text) > 32000 {
		fail(w, 400, "a skill test needs a bounded request")
		return
	}
	run, err := s.P.TestSkill(r.Context(), who, row.ID, row.Version, body.Agent, body.Project, body.Text)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, 202, run)
}
func (s *Server) publishLibrarySkill(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	row, found := s.skillRevision(w, r, who)
	if !found {
		return
	}
	saved, err := s.P.PublishSkill(who, row.ID, row.Version)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, 200, s.P.LibraryView(who, saved))
}
func (s *Server) promoteLibrarySkill(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	row, found := s.skillRevision(w, r, who)
	if !found {
		return
	}
	var body struct {
		Scope   library.Scope `json:"scope"`
		Version int           `json:"version"`
	}
	if !decode(w, r, &body) {
		return
	}
	scope, err := personalLibraryScope(who, body.Scope)
	if err != nil {
		s.problem(w, err)
		return
	}
	if scope == row.Scope {
		s.problem(w, library.ErrInvalid)
		return
	}
	saved, err := s.P.DraftSkill(who, scope, row.Definition, body.Version, &library.Provenance{Scope: row.Scope, Name: row.Name, Version: row.Version})
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, 201, s.P.LibraryView(who, saved))
}
