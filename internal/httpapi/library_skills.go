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

// skillView is a revision as a route answers with it: what its reader may
// see of it (LibraryView), and the hash of its package.
type skillView struct {
	library.Revision
	// PackageHash is the snapshot hash over every file's path and content,
	// the one a run that uses the revision is pinned with. A folder that
	// hashes the same holds the same package. Empty for a revision from
	// before packages, which holds instructions only.
	PackageHash string `json:"package_hash,omitempty"`
	// FailedTests are the tests of an unpublished revision, among those its
	// reader may see, that completed with a failed tool call, so do not count
	// as passing (platform.FailedSkillTests). Only a skill's history carries
	// them.
	FailedTests []string `json:"failed_tests,omitempty"`
}

// skillView hashes the package before a caller drops its files.
func (s *Server) skillView(who auth.Principal, r library.Revision) skillView {
	view := skillView{Revision: s.P.LibraryView(who, r)}
	if r.Files != nil {
		if pkg, err := r.Package(); err == nil {
			view.PackageHash = pkg.Hash
		}
	}
	return view
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
	// A skill this server cannot read, such as one a later release wrote
	// past its bounds, is named under unreadable and fails only itself.
	rows, unreadable, err := s.P.SkillLibrary.List(who.Tenant, scope)
	if err != nil {
		s.problem(w, err)
		return
	}
	views := make([]skillView, len(rows))
	for i := range rows {
		views[i] = s.skillView(who, rows[i])
		views[i].Files = nil // Package content is loaded only after opening a skill.
	}
	ok(w, 200, map[string]any{"skills": views, "unreadable": unreadable})
}
func (s *Server) draftLibrarySkill(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var body struct {
		Scope      library.Scope      `json:"scope"`
		Definition library.Definition `json:"definition"`
		Version    int                `json:"version"`
		// Via is "import" for a draft read from a skill folder, which the
		// revision records, or empty for one saved in the editor.
		Via string `json:"via"`
	}
	if !decode(w, r, &body) {
		return
	}
	if body.Via != "" && body.Via != library.ViaImport {
		fail(w, 400, "via is import, or left out")
		return
	}
	scope, err := personalLibraryScope(who, body.Scope)
	if err != nil {
		s.problem(w, err)
		return
	}
	var row library.Revision
	if body.Via == library.ViaImport {
		row, err = s.P.ImportSkill(who, scope, body.Definition, body.Version)
	} else {
		row, err = s.P.DraftSkill(who, scope, body.Definition, body.Version, nil)
	}
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, 201, s.skillView(who, row))
}
func (s *Server) getLibrarySkill(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	row, err := s.P.LibrarySkill(who, r.PathValue("id"))
	if err != nil {
		s.problem(w, err)
		return
	}
	rows, err := s.P.SkillLibrary.History(who.Tenant, row.Scope, row.Name)
	if err != nil {
		s.problem(w, err)
		return
	}
	views := make([]skillView, len(rows))
	for i := range rows {
		views[i] = s.skillView(who, rows[i])
		if views[i].FailedTests, err = s.P.FailedSkillTests(who, rows[i]); err != nil {
			s.problem(w, err)
			return
		}
	}
	ok(w, 200, map[string]any{"revisions": views})
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
	ok(w, 200, s.skillView(who, saved))
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
	ok(w, 201, s.skillView(who, saved))
}
